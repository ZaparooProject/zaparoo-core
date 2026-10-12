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
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	unknownPath = "usb:1-3:0"
	unknownName = "Generic Smart Card Reader"
)

func unknownInfo() *readerInfo {
	return &readerInfo{Path: unknownPath, Name: unknownName}
}

func probe(r *Reader, path string) error {
	return r.Open(config.ReadersConnect{Driver: DriverID, Path: path}, make(chan readers.Scan, 4),
		readers.OpenOpts{Probing: true})
}

func TestDetect_KnownReaderAttachesWithNoTag(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	b.add(&testInfo, profileACR122U)

	assert.Equal(t, "ccid:"+testInfo.Path, newTestReader(b).Detect(nil))
}

func TestDetect_SkipsConnectedAndFailedReaders(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	b.add(&testInfo, profileACR122U)
	second := readerInfo{Path: "usb:1-4:0", Name: "Second", Known: true}
	b.add(&second, profileACR122U)
	r := newTestReader(b)

	assert.Equal(t, "ccid:"+second.Path, r.Detect([]string{"ccid:" + testInfo.Path}))
	assert.Empty(t, r.Detect([]string{"ccid:" + testInfo.Path, "ccid:" + second.Path}))
	assert.Equal(t, "ccid:"+testInfo.Path, r.Detect([]string{"pn532:" + testInfo.Path}),
		"another driver's reader with the same path text is not this one")
}

func TestDetect_UnknownReaderWaitsForACard(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	b.add(unknownInfo(), profileBR500)
	r := newTestReader(b)

	assert.Empty(t, r.Detect(nil), "an untested reader with nothing on it is left alone")

	b.cards[unknownPath] = true
	assert.Equal(t, "ccid:"+unknownPath, r.Detect(nil))
}

func TestDetect_UnknownReaderWithVisibleATR(t *testing.T) {
	t.Parallel()

	t.Run("contactless tag", func(t *testing.T) {
		t.Parallel()
		b := newFakeBackend("pcsc")
		info := unknownInfo()
		info.ATR = atrNTAG
		b.add(info, profileBR500)

		assert.Equal(t, "ccid:"+unknownPath, newTestReader(b).Detect(nil))
	})

	t.Run("security key", func(t *testing.T) {
		t.Parallel()
		b := newFakeBackend("pcsc")
		info := unknownInfo()
		info.ATR = atrSecurityKey
		dev := b.add(info, profileBR500)
		r := newTestReader(b)

		assert.Empty(t, r.Detect(nil))
		assert.True(t, r.registry.isIgnored(unknownPath))
		assert.Empty(t, b.opened, "the reader is ruled out without being opened")
		assert.Empty(t, dev.transmits)
	})

	t.Run("card slot holding an unresponsive card", func(t *testing.T) {
		t.Parallel()
		b := newFakeBackend("pcsc")
		info := unknownInfo()
		info.ATR = []byte{}
		b.add(info, profileBR500)
		r := newTestReader(b)

		assert.Empty(t, r.Detect(nil))
		assert.True(t, r.registry.isIgnored(unknownPath))
	})
}

func TestDetect_IgnoredReaderIsLookedAtAgainAfterReplugging(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("pcsc")
	info := unknownInfo()
	info.ATR = atrSecurityKey
	b.add(info, profileBR500)
	r := newTestReader(b)
	require.Empty(t, r.Detect(nil))
	require.True(t, r.registry.isIgnored(unknownPath))

	b.infos = nil
	require.Empty(t, r.Detect(nil))

	assert.False(t, r.registry.isIgnored(unknownPath))
}

func TestDetect_UnusableReadersAreNotOffered(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	busy := testInfo
	busy.Unusable = "in use by another program"
	busy.Busy = true
	b.add(&busy, profileACR122U)

	assert.Empty(t, newTestReader(b).Detect(nil))
}

func TestOpen_ProbeAttachesAnUnknownReaderOnAContactlessTag(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	dev := b.add(unknownInfo(), profileBR500)
	dev.place(ntagWithText("hello"))
	r := newTestReader(b)

	require.NoError(t, probe(r, unknownPath))

	assert.True(t, r.Connected())
	assert.True(t, r.registry.hasSeen(unknownPath))
	require.NoError(t, r.Close())

	dev.remove()
	later := newTestReader(b)
	later.registry = r.registry
	assert.Equal(t, "ccid:"+unknownPath, later.Detect(nil),
		"once it has shown a contactless tag the reader attaches with nothing on it")
}

func TestOpen_ProbeRejectsAnUnknownReaderHoldingAContactCard(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	dev := b.add(unknownInfo(), profileBR500)
	dev.placeCard(atrSecurityKey, true)
	r := newTestReader(b)

	err := probe(r, unknownPath)

	require.ErrorIs(t, err, errNotContactless)
	assert.False(t, r.Connected())
	assert.True(t, r.registry.isIgnored(unknownPath))
	assert.True(t, dev.closed)
	assert.Empty(t, dev.transmits, "only the answer to reset is looked at")
}

func TestOpen_ProbeGivesUpOnASlotThatNeverActivates(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	b.add(unknownInfo(), profileBR500)
	b.cards[unknownPath] = true
	r := newTestReader(b)

	for i := range powerFailureLimit {
		require.ErrorIs(t, probe(r, unknownPath), errNoTagPresent)
		assert.Equal(t, i == powerFailureLimit-1, r.registry.isIgnored(unknownPath), "attempt %d", i)
	}
	assert.Empty(t, r.Detect(nil))
}

func TestOpen_ManualConnectionAttachesAnUnknownReader(t *testing.T) {
	t.Parallel()
	b := newFakeBackend("usb")
	b.add(unknownInfo(), profileBR500)
	r := newTestReader(b)

	err := r.Open(config.ReadersConnect{Driver: DriverID, Path: unknownPath}, make(chan readers.Scan, 1),
		readers.OpenOpts{})

	require.NoError(t, err, "a reader the user names is attached without waiting for a tag")
	require.NoError(t, r.Close())
}

func TestRegistry(t *testing.T) {
	t.Parallel()
	reg := newRegistry()

	assert.True(t, reg.firstTime(logKey("seen", "a")))
	assert.False(t, reg.firstTime(logKey("seen", "a")))
	assert.True(t, reg.firstTime(logKey("seen", "b")))
	assert.True(t, reg.firstTime("not tied to a reader"))

	reg.ignore("a", "reason")
	reg.markSeen("b")
	assert.False(t, reg.powerFailed("c"))

	reg.prune(map[string]bool{"b": true})

	assert.False(t, reg.isIgnored("a"))
	assert.True(t, reg.firstTime(logKey("seen", "a")), "a reader that comes back is reported again")
	assert.False(t, reg.firstTime(logKey("seen", "b")))
	assert.False(t, reg.firstTime("not tied to a reader"))
	assert.True(t, reg.hasSeen("b"))
	assert.False(t, reg.powerFailed("c"), "the failure count starts again for a replugged reader")
}

func TestKnownReaders(t *testing.T) {
	t.Parallel()

	assert.True(t, isKnownName("ACS ACR122U PICC Interface 00 00"))
	assert.True(t, isKnownName("ACS ACR122 0"))
	assert.True(t, isKnownName("ACS ACR1552 1S CL Reader [ACR1552 1S CL Reader PICC] (RR620-032040) 01 00"))
	assert.False(t, isKnownName("ACS ACR1552 1S CL Reader [ACR1552 1S CL Reader SAM] (RR620-032040) 02 00"))
	assert.True(t, isKnownName("Feitian bR500 (VER03) 02 00"))
	assert.False(t, isKnownName("Yubico YubiKey OTP+FIDO+CCID 00 00"))

	assert.True(t, isKnownUSB(0x072F, 0x2200, 0))
	assert.True(t, isKnownUSB(0x072F, 0x2303, 0))
	assert.False(t, isKnownUSB(0x072F, 0x2303, 1), "the ACR1552 SAM interface is not a contactless reader")
	assert.True(t, isKnownUSB(0x096E, 0x0623, 0))
	assert.False(t, isKnownUSB(0x1050, 0x0407, 0))
}

// With no PC/SC service a busy reader is worth reporting, because nothing
// else explains why it is not picked up. The reader this driver has open is
// busy for that reason alone and must not be reported as someone else's.
func TestRegistry_HeldReaderIsNotReportedAsBusy(t *testing.T) {
	t.Parallel()
	pcsc := newFakeBackend("pcsc")
	pcsc.listErr = errors.New("service not running")
	usb := newFakeBackend("usb")
	usb.add(&testInfo, profileACR122U)
	r := newTestReader(pcsc, usb)

	require.NoError(t, r.Open(config.ReadersConnect{Driver: DriverID, Path: testInfo.Path},
		make(chan readers.Scan, 1), readers.OpenOpts{}))
	assert.True(t, r.registry.isHeld(testInfo.Path))

	// Once open, the reader is listed as claimed.
	usb.infos[0].Unusable = "in use by another program"
	usb.infos[0].Busy = true
	later := newTestReader(pcsc, usb)
	later.registry = r.registry
	assert.Empty(t, later.Detect([]string{"ccid:" + testInfo.Path}))
	assert.True(t, r.registry.firstTime(logKey("unusable", testInfo.Path)),
		"the driver's own reader was not logged as unusable")

	require.NoError(t, r.Close())
	assert.False(t, r.registry.isHeld(testInfo.Path))
}
