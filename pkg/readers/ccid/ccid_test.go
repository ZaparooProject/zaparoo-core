/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package ccid

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag/tagsim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testInfo = readerInfo{Path: "usb:1-2:0", Name: "Test Reader", Known: true}

func TestReader_Metadata(t *testing.T) {
	t.Parallel()
	r := NewReader(nil)

	assert.Equal(t, readers.DriverMetadata{
		ID:                "ccid",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
		Description:       "USB CCID NFC readers",
	}, r.Metadata())
	assert.Equal(t, []string{"ccid", "acr122pcsc", "acr122_pcsc"}, r.IDs(),
		"connection entries written for the driver this replaced must keep working")
	assert.Equal(t,
		[]readers.Capability{readers.CapabilityWrite, readers.CapabilityRemovable}, r.Capabilities())
	assert.False(t, r.Connected())
}

// The same tag must give the same token on every reader, although the three
// readers accept different transfer sizes and report different error codes.
func TestReader_SameTokenOnEveryReader(t *testing.T) {
	t.Parallel()

	tags := map[string]struct {
		tag      func() simTag
		wantType string
		wantText string
		wantData string
	}{
		"NTAG": {
			tag:      func() simTag { return ntagWithText("@NES/Lode Runner") },
			wantType: tokens.TypeNTAG,
			wantText: "@NES/Lode Runner",
			wantData: "0317d1011354" + "02656e404e45532f4c6f6465205275" + "6e6e6572fe000000000000",
		},
		"MIFARE Classic": {
			tag:      func() simTag { return classicWithText("test4") },
			wantType: tokens.TypeMifare,
			wantText: "test4",
			wantData: "030cd1010854" + "02656e7465737434fe00",
		},
	}

	for name, tt := range tags {
		for _, profile := range simProfiles {
			t.Run(name+" on "+profile.name, func(t *testing.T) {
				t.Parallel()
				dev := newSimDevice(profile)
				tag := tt.tag()
				dev.place(tag)
				r, iq := attach(t, dev, &testInfo)

				ticks(t, r, iq, 3)

				scans := drain(iq)
				require.Len(t, scans, 1, "a tag that stays on the reader is reported once")
				token := scans[0].Token
				require.NotNil(t, token)
				assert.Equal(t, tt.wantType, token.Type)
				assert.Equal(t, hex.EncodeToString(tag.UID()), token.UID)
				assert.Equal(t, tt.wantText, token.Text)
				assert.Equal(t, tt.wantData, token.Data)
				assert.Equal(t, tokens.SourceReader, token.Source)
				assert.Equal(t, r.ReaderID(), token.ReaderID)
				assert.Equal(t, r.ReaderID(), scans[0].ReaderID)
			})
		}
	}
}

func TestReader_NeverSendsALengthOfZero(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileBR500)
	dev.place(ntagWithText("hello"))
	r, iq := attach(t, dev, &testInfo)

	ticks(t, r, iq, 1)

	for _, apdu := range dev.transmits {
		if apdu[1] == insReadBinary {
			assert.NotZero(t, apdu[4], "a zero length reads as success with no data on some readers")
		}
	}
}

func TestReader_RemovalNeedsTwoMissedPolls(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileACR122U)
	dev.place(ntagWithText("hello"))
	r, iq := attach(t, dev, &testInfo)
	ticks(t, r, iq, 1)
	require.Len(t, drain(iq), 1)

	dev.remove()
	ticks(t, r, iq, 1)
	assert.Empty(t, drain(iq), "one missed poll is not a removal")

	ticks(t, r, iq, 1)
	scans := drain(iq)
	require.Len(t, scans, 1)
	assert.Nil(t, scans[0].Token)
	assert.False(t, scans[0].ReaderError)
	assert.False(t, scans[0].WrittenTagRemoved)

	ticks(t, r, iq, 3)
	assert.Empty(t, drain(iq), "an empty reader stays quiet")
}

func TestReader_DroppedPollDoesNotRemoveTag(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileACR122U)
	tag := ntagWithText("hello")
	dev.place(tag)
	r, iq := attach(t, dev, &testInfo)
	ticks(t, r, iq, 1)
	require.Len(t, drain(iq), 1)

	dev.remove()
	ticks(t, r, iq, 1)
	dev.place(tag)
	ticks(t, r, iq, 2)

	assert.Empty(t, drain(iq))
}

func TestReader_TagSwapWithoutAGap(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileACR122U)
	dev.place(ntagWithText("first"))
	r, iq := attach(t, dev, &testInfo)
	ticks(t, r, iq, 1)
	require.Len(t, drain(iq), 1)

	dev.place(classicWithText("second"))
	ticks(t, r, iq, 2)

	scans := drain(iq)
	require.Len(t, scans, 1, "the new tag is reported without a removal in between")
	require.NotNil(t, scans[0].Token)
	assert.Equal(t, "second", scans[0].Token.Text)
}

func TestReader_WaitsForAReaderThatHasNotIdentifiedTheTag(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileBR500)
	tag := ntagWithText("hello")
	dev.place(tag)
	dev.atr = atrPlaceholder
	dev.notReady = 2
	r, iq := attach(t, dev, &testInfo)

	ticks(t, r, iq, 2)
	assert.Empty(t, drain(iq), "an empty UID must never become a token")
	assert.Equal(t, 2, dev.reactivations)

	dev.atr = atrNTAG
	ticks(t, r, iq, 1)
	scans := drain(iq)
	require.Len(t, scans, 1)
	assert.Equal(t, hex.EncodeToString(tag.UID()), scans[0].Token.UID)
}

func TestReader_NeverTalksToACardThatIsNotContactless(t *testing.T) {
	t.Parallel()

	for name, attached := range map[string]bool{"device declined it": false, "device offered it": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dev := newSimDevice(profileACR1552)
			dev.placeCard(atrSecurityKey, attached)
			r, iq := attach(t, dev, &testInfo)

			ticks(t, r, iq, 3)

			assert.Empty(t, drain(iq))
			assert.Empty(t, dev.transmits, "no command may reach a contact card or security key")
		})
	}
}

func TestReader_UnreadableContactlessCardIsReportedByUID(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileACR1552)
	dev.placeCard(atrDESFire, true)
	r, iq := attach(t, dev, &testInfo)

	ticks(t, r, iq, 2)

	scans := drain(iq)
	require.Len(t, scans, 1)
	assert.Equal(t, tokens.TypeUnknown, scans[0].Token.Type)
	assert.Equal(t, "04112233445566", scans[0].Token.UID)
	assert.Empty(t, scans[0].Token.Text)
	assert.Empty(t, scans[0].Token.Data)
}

func TestReader_ReadFailureStillReportsTheTag(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileBR500)
	tag := ntagWithText("a message long enough to need several reads from the tag")
	dev.place(tag)
	dev.failReadsFrom = 3
	r, iq := attach(t, dev, &testInfo)

	ticks(t, r, iq, 1)

	scans := drain(iq)
	require.Len(t, scans, 1)
	assert.Equal(t, tokens.TypeNTAG, scans[0].Token.Type)
	assert.Equal(t, hex.EncodeToString(tag.UID()), scans[0].Token.UID)
	assert.Empty(t, scans[0].Token.Text)
	assert.NoError(t, scans[0].Error)
}

func TestReader_DisconnectIsNotARemoval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err       error
		name      string
		wantTicks int
	}{
		{name: "reader gone", err: errReaderGone, wantTicks: 1},
		{name: "repeated failures", err: errors.New("transfer failed"), wantTicks: pollFailureLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dev := newSimDevice(profileACR122U)
			dev.place(ntagWithText("hello"))
			r, iq := attach(t, dev, &testInfo)
			ticks(t, r, iq, 1)
			require.Len(t, drain(iq), 1)

			dev.pollErr = tt.err
			for i := range tt.wantTicks - 1 {
				require.False(t, r.tick(context.Background(), iq), "tick %d", i)
				require.Empty(t, drain(iq))
				require.True(t, r.Connected())
			}
			require.True(t, r.tick(context.Background(), iq), "polling stops once the reader is gone")

			scans := drain(iq)
			require.Len(t, scans, 1, "a disconnect with a tag present must not also report a removal")
			assert.True(t, scans[0].ReaderError)
			assert.Nil(t, scans[0].Token)
			assert.False(t, r.Connected())
		})
	}
}

func TestReader_OnePollFailureIsForgiven(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileACR122U)
	dev.place(ntagWithText("hello"))
	r, iq := attach(t, dev, &testInfo)
	ticks(t, r, iq, 1)
	require.Len(t, drain(iq), 1)

	dev.pollErr = errors.New("transfer failed")
	ticks(t, r, iq, pollFailureLimit-1)
	dev.pollErr = nil
	ticks(t, r, iq, pollFailureLimit)

	assert.Empty(t, drain(iq))
	assert.True(t, r.Connected())
}

func TestReader_WritesOnEveryReader(t *testing.T) {
	t.Parallel()

	tags := map[string]struct {
		tag      func() simTag
		stored   func(simTag) []byte
		wantType string
	}{
		"NTAG": {
			tag:      func() simTag { return tagsim.NewType2(tagsim.PagesNTAG215) },
			stored:   func(tag simTag) []byte { return tag.(*tagsim.Type2).User() },
			wantType: tokens.TypeNTAG,
		},
		"blank MIFARE Classic": {
			tag:      func() simTag { return tagsim.NewClassic(16) },
			stored:   func(tag simTag) []byte { return tag.(*tagsim.Classic).Data() },
			wantType: tokens.TypeMifare,
		},
	}

	for name, tt := range tags {
		for _, profile := range simProfiles {
			t.Run(name+" on "+profile.name, func(t *testing.T) {
				t.Parallel()
				dev := newSimDevice(profile)
				tag := tt.tag()
				dev.place(tag)
				r, _ := attach(t, dev, &testInfo)

				token, err := r.Write("**launch.system:nes")

				require.NoError(t, err)
				assert.Equal(t, tt.wantType, token.Type)
				assert.Equal(t, hex.EncodeToString(tag.UID()), token.UID)
				assert.Equal(t, "**launch.system:nes", token.Text)
				want := textTLV("**launch.system:nes")
				assert.Equal(t, want, tt.stored(tag)[:len(want)], "every reader must leave the same bytes on the tag")
			})
		}
	}
}

func TestReader_WrittenTagStaysQuietUntilItLeaves(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileACR122U)
	dev.place(tagsim.NewType2(tagsim.PagesNTAG215))
	r, iq := attach(t, dev, &testInfo)

	_, err := r.Write("hello")
	require.NoError(t, err)

	ticks(t, r, iq, 3)
	assert.Empty(t, drain(iq), "the tag just written must not be scanned as a new tag")

	dev.remove()
	ticks(t, r, iq, removalMisses)
	scans := drain(iq)
	require.Len(t, scans, 1)
	assert.Nil(t, scans[0].Token)
	assert.True(t, scans[0].WrittenTagRemoved)

	dev.place(ntagWithText("hello"))
	ticks(t, r, iq, 1)
	scans = drain(iq)
	require.Len(t, scans, 1, "presenting the tag again is a normal scan")
	assert.Equal(t, "hello", scans[0].Token.Text)
}

func TestReader_DifferentTagAfterWriteIsScanned(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileACR122U)
	dev.place(tagsim.NewType2(tagsim.PagesNTAG215))
	r, iq := attach(t, dev, &testInfo)
	_, err := r.Write("hello")
	require.NoError(t, err)

	dev.place(classicWithText("other"))
	ticks(t, r, iq, 1)

	scans := drain(iq)
	require.Len(t, scans, 1)
	assert.Equal(t, "other", scans[0].Token.Text)

	dev.remove()
	ticks(t, r, iq, removalMisses)
	scans = drain(iq)
	require.Len(t, scans, 1)
	assert.False(t, scans[0].WrittenTagRemoved, "the tag that left is not the one that was written")
}

func TestReader_PollingIsSilentWhileAWriteWaits(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileACR122U)
	r, iq := attach(t, dev, &testInfo)
	_, err := r.beginWrite(context.Background())
	require.NoError(t, err)

	dev.place(ntagWithText("launch me"))
	ticks(t, r, iq, 3)

	assert.Empty(t, drain(iq), "the tag presented for a write must not be launched first")
	assert.Empty(t, dev.transmits)
	r.endWrite()
}

func TestReader_WriteTargetChecksTheTag(t *testing.T) {
	t.Parallel()
	tag := tagsim.NewType2(tagsim.PagesNTAG215)
	uid := hex.EncodeToString(tag.UID())

	tests := []struct {
		name    string
		wantErr string
		opts    readers.WriteOptions
	}{
		{
			name:    "excluded",
			opts:    readers.WriteOptions{ExcludeUID: uid},
			wantErr: "refusing to write excluded tag: " + uid,
		},
		{
			name:    "wrong target",
			opts:    readers.WriteOptions{TargetUID: "aabbccdd"},
			wantErr: "refusing to write wrong tag: got " + uid + " want aabbccdd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dev := newSimDevice(profileACR122U)
			dev.place(tagsim.NewType2(tagsim.PagesNTAG215))
			r, _ := attach(t, dev, &testInfo)

			_, err := r.WriteTarget(context.Background(), "hello", tt.opts)

			require.EqualError(t, err, tt.wantErr)
			for _, apdu := range dev.transmits {
				assert.NotEqual(t, byte(insUpdateBinary), apdu[1], "nothing may be written to the wrong tag")
			}
		})
	}

	t.Run("matching target", func(t *testing.T) {
		t.Parallel()
		dev := newSimDevice(profileACR122U)
		dev.place(tagsim.NewType2(tagsim.PagesNTAG215))
		r, _ := attach(t, dev, &testInfo)

		token, err := r.WriteTarget(context.Background(), "hello", readers.WriteOptions{TargetUID: uid})

		require.NoError(t, err)
		assert.Equal(t, uid, token.UID)
	})
}

func TestReader_WriteErrors(t *testing.T) {
	t.Parallel()

	t.Run("no tag presented", func(t *testing.T) {
		t.Parallel()
		r, _ := attach(t, newSimDevice(profileACR122U), &testInfo)
		r.writeWait = 20 * time.Millisecond

		_, err := r.Write("hello")

		require.ErrorIs(t, err, readers.ErrTagNotDetected)
	})

	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()
		r, _ := attach(t, newSimDevice(profileACR122U), &testInfo)
		errs := make(chan error, 1)
		go func() {
			_, err := r.Write("hello")
			errs <- err
		}()

		require.Eventually(t, r.isWriting, time.Second, time.Millisecond)
		r.CancelWrite()

		require.ErrorIs(t, <-errs, context.Canceled)
		assert.False(t, r.isWriting())
	})

	t.Run("unsupported tag", func(t *testing.T) {
		t.Parallel()
		dev := newSimDevice(profileACR1552)
		dev.placeCard(atrDESFire, true)
		r, _ := attach(t, dev, &testInfo)

		_, err := r.Write("hello")

		require.ErrorIs(t, err, readers.ErrUnsupportedTagType)
	})

	t.Run("empty text", func(t *testing.T) {
		t.Parallel()
		r, _ := attach(t, newSimDevice(profileACR122U), &testInfo)
		_, err := r.Write("")
		require.Error(t, err)
	})

	t.Run("not open", func(t *testing.T) {
		t.Parallel()
		_, err := newTestReader().Write("hello")
		require.ErrorIs(t, err, errNotOpen)
	})

	t.Run("second write while one is waiting", func(t *testing.T) {
		t.Parallel()
		r, _ := attach(t, newSimDevice(profileACR122U), &testInfo)
		_, err := r.beginWrite(context.Background())
		require.NoError(t, err)

		_, err = r.Write("hello")

		require.Error(t, err)
		r.endWrite()
	})
}

func TestReader_OpenAndClose(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	dev := b.add(&testInfo, profileACR122U)
	dev.place(ntagWithText("hello"))
	r := newTestReader(b)
	iq := make(chan readers.Scan, 16)

	err := r.Open(config.ReadersConnect{Driver: "ccid", Path: testInfo.Path}, iq, readers.OpenOpts{})
	require.NoError(t, err)

	assert.True(t, r.Connected())
	assert.Equal(t, testInfo.Path, r.Path())
	assert.Equal(t, testInfo.Name, r.Info())
	assert.Equal(t, readers.GenerateReaderID("ccid", testInfo.Path), r.ReaderID())

	select {
	case scan := <-iq:
		require.NotNil(t, scan.Token)
		assert.Equal(t, "hello", scan.Token.Text)
	case <-time.After(5 * time.Second):
		t.Fatal("no scan from the opened reader")
	}

	require.NoError(t, r.Close())
	assert.False(t, r.Connected())
	assert.True(t, dev.closed)
	assert.Equal(t, 1, b.closed)
	require.NoError(t, r.Close(), "closing twice is harmless")
}

func TestReader_OpenAcceptsTheReplacedDriversName(t *testing.T) {
	t.Parallel()

	for _, driver := range []string{"acr122pcsc", "acr122_pcsc", "ACR122PCSC"} {
		t.Run(driver, func(t *testing.T) {
			t.Parallel()
			b := newFakeBackend("pcsc")
			name := "ACS ACR122 0"
			b.add(&readerInfo{Path: name, Name: name, Known: true}, profileACR122U)
			r := newTestReader(b)

			err := r.Open(
				config.ReadersConnect{Driver: driver, Path: name}, make(chan readers.Scan, 1), readers.OpenOpts{})

			require.NoError(t, err)
			assert.Equal(t, name, r.Path())
			require.NoError(t, r.Close())
		})
	}
}

func TestReader_OpenFailures(t *testing.T) {
	t.Parallel()
	iq := make(chan readers.Scan, 1)

	t.Run("wrong driver", func(t *testing.T) {
		t.Parallel()
		r := newTestReader(newFakeBackend("usb"))
		err := r.Open(config.ReadersConnect{Driver: "pn532", Path: "x"}, iq, readers.OpenOpts{})
		require.Error(t, err)
	})

	t.Run("reader not present", func(t *testing.T) {
		t.Parallel()
		r := newTestReader(newFakeBackend("usb"))
		err := r.Open(config.ReadersConnect{Driver: "ccid", Path: "usb:9-9:0"}, iq, readers.OpenOpts{})
		require.ErrorIs(t, err, errReaderMissing)
		assert.False(t, r.Connected())
	})

	t.Run("no backends", func(t *testing.T) {
		t.Parallel()
		err := newTestReader().Open(config.ReadersConnect{Driver: "ccid"}, iq, readers.OpenOpts{})
		require.ErrorIs(t, err, errReaderMissing)
	})
}

func TestReader_OpenWithoutAPathPrefersAKnownReader(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	b.add(&readerInfo{Path: "usb:1-1:0", Name: "Unknown"}, profileBR500)
	b.add(&testInfo, profileACR122U)
	r := newTestReader(b)

	require.NoError(t, r.Open(config.ReadersConnect{Driver: "ccid"}, make(chan readers.Scan, 1), readers.OpenOpts{}))

	assert.Equal(t, testInfo.Path, r.Path())
	require.NoError(t, r.Close())
}

func TestReader_UsesTheFirstBackendThatHasTheReader(t *testing.T) {
	t.Parallel()
	pcsc := newFakeBackend("pcsc")
	pcsc.listErr = errors.New("service not running")
	usb := newFakeBackend("usb")
	usb.add(&testInfo, profileACR122U)
	r := newTestReader(pcsc, usb)

	assert.Equal(t, "ccid:"+testInfo.Path, r.Detect(nil), "a backend that cannot list does not stop the others")
	require.NoError(t, r.Close())
	assert.Equal(t, 1, pcsc.closed)
	assert.Equal(t, 1, usb.closed)
}

// With no PC/SC service, a reader some other program holds would otherwise go
// unexplained. It is reported once, not on every detect pass.
func TestReader_ReportsUnusableReaderOnce(t *testing.T) {
	t.Parallel()
	pcsc := newFakeBackend("pcsc")
	pcsc.listErr = errors.New("service not running")
	usb := newFakeBackend("usb")
	busy := readerInfo{
		Path: "usb:1-5:0", Name: "Busy Reader", Unusable: "in use by another program", Busy: true,
		Details: map[string]string{"usbID": "1234:5678"},
	}
	usb.add(&busy, profileACR122U)
	tpdu := readerInfo{Path: "usb:1-6:0", Name: "Contact Reader", Unusable: "reader does not exchange whole APDUs"}
	usb.add(&tpdu, profileACR122U)
	r := newTestReader(pcsc, usb)

	assert.Empty(t, r.Detect(nil))

	assert.False(t, r.registry.firstTime(logKey("unusable", busy.Path)), "the busy reader was reported")
	assert.False(t, r.registry.firstTime(logKey("unusable", tpdu.Path)))
	assert.Empty(t, usb.opened)
	assert.Empty(t, r.Detect(nil), "a second pass has nothing new to say")
}

// While every backend works, a busy reader is the PC/SC service doing its job.
func TestReader_BusyReaderIsNotReportedWhilePCSCWorks(t *testing.T) {
	t.Parallel()
	usb := newFakeBackend("usb")
	busy := readerInfo{Path: "usb:1-5:0", Name: "Busy Reader", Unusable: "in use by another program", Busy: true}
	usb.add(&busy, profileACR122U)
	r := newTestReader(newFakeBackend("pcsc"), usb)

	assert.Empty(t, r.Detect(nil))

	assert.True(t, r.registry.firstTime(logKey("unusable", busy.Path)))
}

func TestReader_FirstTagOnAnUntestedReaderIsRecordedOnce(t *testing.T) {
	t.Parallel()
	unknown := readerInfo{Path: "usb:1-7:0", Name: "Generic NFC Reader"}

	t.Run("read succeeds", func(t *testing.T) {
		t.Parallel()
		dev := newSimDevice(profileBR500)
		dev.place(ntagWithText("hello"))
		r, iq := attach(t, dev, &unknown)

		ticks(t, r, iq, 1)

		require.Len(t, drain(iq), 1)
		assert.False(t, r.registry.firstTime(logKey("first-tag", unknown.Path)))
		assert.True(t, r.diagnosed)
	})

	t.Run("read fails", func(t *testing.T) {
		t.Parallel()
		dev := newSimDevice(profileBR500)
		dev.place(ntagWithText("a message long enough to need several reads from the tag"))
		dev.failReadsFrom = 2
		r, iq := attach(t, dev, &unknown)

		ticks(t, r, iq, 1)

		scans := drain(iq)
		require.Len(t, scans, 1, "the tag is still reported, by UID")
		assert.Empty(t, scans[0].Token.Text)
		assert.False(t, r.registry.firstTime(logKey("first-tag", unknown.Path)))
	})

	t.Run("known reader", func(t *testing.T) {
		t.Parallel()
		dev := newSimDevice(profileACR122U)
		dev.place(ntagWithText("hello"))
		r, iq := attach(t, dev, &testInfo)

		ticks(t, r, iq, 1)

		assert.True(t, r.registry.firstTime(logKey("first-tag", testInfo.Path)),
			"nothing to record for a tested reader")
	})
}

func TestReader_TickStopsWhenClosedOrCancelled(t *testing.T) {
	t.Parallel()
	iq := make(chan readers.Scan, 1)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	r, _ := attach(t, newSimDevice(profileACR122U), &testInfo)
	assert.True(t, r.tick(cancelled, iq))

	assert.True(t, newTestReader().tick(context.Background(), iq), "a reader that is not open has nothing to poll")
	require.NoError(t, r.OnMediaChange(nil))
}

func TestReader_ScanIsDroppedWhenNobodyIsListening(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileACR122U)
	dev.place(ntagWithText("hello"))
	r, _ := attach(t, dev, &testInfo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		// An unbuffered queue with no reader: the send must give up with the
		// context instead of blocking the reader for good.
		r.send(ctx, make(chan readers.Scan), &readers.Scan{})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("send blocked after its context ended")
	}
}

func TestReader_WriteWaitsOutATagThatIsNotReady(t *testing.T) {
	t.Parallel()
	dev := newSimDevice(profileBR500)
	tag := tagsim.NewType2(tagsim.PagesNTAG215)
	dev.place(tag)
	dev.notReady = 1
	r, _ := attach(t, dev, &testInfo)

	token, err := r.Write("hello")

	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(tag.UID()), token.UID)
	assert.Positive(t, dev.reactivations, "the reader was asked to identify the tag again")
}

func TestReader_WriteFailsWhenTheReaderGoes(t *testing.T) {
	t.Parallel()

	t.Run("unplugged", func(t *testing.T) {
		t.Parallel()
		dev := newSimDevice(profileACR122U)
		dev.pollErr = errReaderGone
		r, _ := attach(t, dev, &testInfo)

		_, err := r.Write("hello")

		require.ErrorIs(t, err, errReaderGone)
	})

	t.Run("a failed poll is retried", func(t *testing.T) {
		t.Parallel()
		dev := newSimDevice(profileACR122U)
		dev.pollErr = errors.New("transfer failed")
		r, _ := attach(t, dev, &testInfo)
		r.writeWait = 20 * time.Millisecond

		_, err := r.Write("hello")

		require.ErrorIs(t, err, readers.ErrTagNotDetected)
	})

	t.Run("write fails on the tag", func(t *testing.T) {
		t.Parallel()
		dev := newSimDevice(profileACR122U)
		dev.place(tagsim.NewType2(tagsim.PagesNTAG215))
		dev.failReadsFrom = 1
		r, _ := attach(t, dev, &testInfo)

		_, err := r.Write("hello")

		require.ErrorContains(t, err, "failed to write to tag")
	})
}

func TestReader_OpenReportsBackendFailure(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	b.infos = append(b.infos, testInfo)
	r := newTestReader(b)

	err := r.Open(config.ReadersConnect{Driver: DriverID, Path: testInfo.Path}, make(chan readers.Scan, 1),
		readers.OpenOpts{})

	require.ErrorIs(t, err, errReaderGone, "the reader was listed and then could not be opened")
	assert.False(t, r.Connected())
}

func TestOpen_ProbeFailsWhenThePollFails(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	dev := b.add(&readerInfo{Path: "usb:1-8:0", Name: "Flaky Reader"}, profileBR500)
	dev.pollErr = errors.New("transfer failed")
	r := newTestReader(b)

	err := r.Open(config.ReadersConnect{Driver: DriverID, Path: "usb:1-8:0"}, make(chan readers.Scan, 1),
		readers.OpenOpts{Probing: true})

	require.ErrorContains(t, err, "transfer failed")
	assert.True(t, dev.closed)
	assert.False(t, r.registry.isIgnored("usb:1-8:0"), "a failed transfer says nothing about what the reader is")
}
