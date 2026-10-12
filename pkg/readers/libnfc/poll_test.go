//go:build linux && !android

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

package libnfc

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag/tagsim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/clausecker/nfc/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errNoAnswer = errors.New("RF transmission error")

// fakeDevice is a libnfc device with at most one tag in its field. It answers
// the tag's native commands the way the tag would.
type fakeDevice struct {
	pollErr error
	type2   *tagsim.Type2
	classic *tagsim.Classic
	// failReads is how many more reads fail, as they do while a tag is still
	// being placed.
	failReads int
	desfire   bool
	halted    bool
	closed    bool
}

var _ device = (*fakeDevice)(nil)

func (*fakeDevice) String() string     { return "fake reader" }
func (*fakeDevice) Connection() string { return "acr122_usb:001:002" }

func (d *fakeDevice) Close() error {
	d.closed = true
	return nil
}

func (d *fakeDevice) clear() {
	d.type2, d.classic, d.desfire = nil, nil, false
}

func (d *fakeDevice) InitiatorPollTarget([]nfc.Modulation, int, time.Duration) (int, nfc.Target, error) {
	if d.pollErr != nil {
		return 0, nil, d.pollErr
	}
	card := &nfc.ISO14443aTarget{}
	var uid []byte
	switch {
	case d.type2 != nil:
		card.Atqa, card.Sak, uid = [2]byte{0x00, 0x44}, 0x00, d.type2.UID()
	case d.classic != nil:
		card.Atqa, card.Sak, uid = [2]byte{0x00, 0x04}, 0x08, d.classic.UID()
	case d.desfire:
		card.Atqa, card.Sak, uid = [2]byte{0x03, 0x44}, 0x20, []byte{4, 1, 2, 3, 4, 5, 6}
	default:
		return 0, nil, nil
	}
	card.UIDLen = len(uid)
	copy(card.UID[:], uid)
	d.halted = false
	return 1, card, nil
}

func (d *fakeDevice) InitiatorSelectPassiveTarget(nfc.Modulation, []byte) (nfc.Target, error) {
	d.halted = false
	return nil, nil //nolint:nilnil // only the wake-up matters
}

func (d *fakeDevice) InitiatorTransceiveBytes(tx, rx []byte, _ int) (int, error) {
	if d.halted {
		return 0, errNoAnswer
	}
	ctx := context.Background()
	addr := int(tx[1])
	switch tx[0] {
	case 0x30:
		if d.failReads > 0 {
			d.failReads--
			return 0, errNoAnswer
		}
		var block []byte
		var err error
		if d.type2 != nil {
			block, err = d.type2.ReadBlock(ctx, addr)
		} else if d.classic != nil {
			block, err = d.classic.ReadBlock(ctx, addr)
		}
		if err != nil || block == nil {
			return 0, errNoAnswer
		}
		return copy(rx, block), nil
	case 0xA2:
		if d.type2 == nil || d.type2.WriteBlock(ctx, addr, tx[2:]) != nil {
			return 0, errNoAnswer
		}
	case 0xA0:
		if d.classic == nil || d.classic.WriteBlock(ctx, addr, tx[2:]) != nil {
			return 0, errNoAnswer
		}
	case 0x60, 0x61:
		var key nfctag.Key
		copy(key[:], tx[2:8])
		if d.classic == nil || d.classic.Authenticate(ctx, addr, nfctag.KeyType(tx[0]), key) != nil {
			d.halted = true
			return 0, errNoAnswer
		}
	default:
		return 0, errNoAnswer
	}
	return 0, nil
}

// textTLV is the NDEF data a tag holds for a short text record.
//
//nolint:gosec // test text is far shorter than 255 bytes
func textTLV(text string) []byte {
	record := append([]byte{0xD1, 0x01, byte(len(text) + 3), 'T', 0x02, 'e', 'n'}, text...)
	out := append([]byte{0x03, byte(len(record))}, record...)
	return append(out, 0xFE)
}

func ntagWithText(text string) *tagsim.Type2 {
	tag := tagsim.NewType2(tagsim.PagesNTAG215)
	tag.SetUser(textTLV(text))
	return tag
}

func testReader(dev *fakeDevice) *Reader {
	r := NewACR122Reader(&config.Instance{})
	r.pnd = dev
	return r
}

// poll polls once, carrying the token over the way the reader's loop does.
func poll(t *testing.T, r *Reader, dev *fakeDevice) (token *tokens.Token, removed bool, err error) {
	t.Helper()
	token, removed, err = r.pollDevice(dev, r.prevToken, 1, 0)
	if err == nil {
		r.prevToken = token
	}
	return token, removed, err
}

// The expected values are the ones the ccid and PN532 drivers' tests use for
// the same tags: every driver must give the same token.
func TestPollDevice_SameTokenAsOtherDrivers(t *testing.T) {
	t.Parallel()

	t.Run("NTAG", func(t *testing.T) {
		t.Parallel()
		tag := ntagWithText("@NES/Lode Runner")
		dev := &fakeDevice{type2: tag}
		r := testReader(dev)

		token, removed, err := poll(t, r, dev)

		require.NoError(t, err)
		require.NotNil(t, token)
		assert.False(t, removed)
		assert.Equal(t, tokens.TypeNTAG, token.Type)
		assert.Equal(t, hex.EncodeToString(tag.UID()), token.UID)
		assert.Equal(t, "@NES/Lode Runner", token.Text)
		assert.Equal(t, "0317d101135402656e404e45532f4c6f64652052756e6e6572fe000000000000", token.Data)
		assert.Equal(t, tokens.SourceReader, token.Source)
		assert.Equal(t, r.ReaderID(), token.ReaderID)
	})

	t.Run("MIFARE Classic", func(t *testing.T) {
		t.Parallel()
		tag := tagsim.NewClassic(16)
		tag.Format()
		tag.SetData(textTLV("test4"))
		dev := &fakeDevice{classic: tag}

		token, _, err := poll(t, testReader(dev), dev)

		require.NoError(t, err)
		assert.Equal(t, tokens.TypeMifare, token.Type)
		assert.Equal(t, "test4", token.Text)
		assert.Equal(t, "030cd101085402656e7465737434fe00", token.Data)
	})

	t.Run("tag this driver cannot read", func(t *testing.T) {
		t.Parallel()
		dev := &fakeDevice{desfire: true}

		token, _, err := poll(t, testReader(dev), dev)

		require.NoError(t, err)
		assert.Equal(t, tokens.TypeUnknown, token.Type)
		assert.Equal(t, "04010203040506", token.UID)
		assert.Empty(t, token.Text)
	})
}

func TestPollDevice_RemovalNeedsTwoMissedPolls(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{type2: ntagWithText("hello")}
	r := testReader(dev)
	first, _, err := poll(t, r, dev)
	require.NoError(t, err)

	same, removed, err := poll(t, r, dev)
	require.NoError(t, err)
	assert.Same(t, first, same, "a tag that stays is not read again")
	assert.False(t, removed)

	dev.clear()
	token, removed, err := poll(t, r, dev)
	require.NoError(t, err)
	assert.False(t, removed, "one missed poll is not a removal")
	assert.NotNil(t, token)

	token, removed, err = poll(t, r, dev)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.Nil(t, token)

	_, removed, err = poll(t, r, dev)
	require.NoError(t, err)
	assert.False(t, removed, "an empty reader reports nothing")
}

func TestPollDevice_ReadFailures(t *testing.T) {
	t.Parallel()

	t.Run("retried, then reported by UID", func(t *testing.T) {
		t.Parallel()
		tag := ntagWithText("hello")
		dev := &fakeDevice{type2: tag, failReads: 100}
		r := testReader(dev)

		for i := range readAttempts - 1 {
			token, _, err := poll(t, r, dev)
			require.Error(t, err, "attempt %d", i)
			assert.Nil(t, token)
		}

		token, _, err := poll(t, r, dev)
		require.NoError(t, err)
		require.NotNil(t, token, "a tag that cannot be read is still reported")
		assert.Equal(t, hex.EncodeToString(tag.UID()), token.UID)
		assert.Equal(t, tokens.TypeNTAG, token.Type)
		assert.Empty(t, token.Text)
	})

	t.Run("a tag still being placed reads on the next poll", func(t *testing.T) {
		t.Parallel()
		dev := &fakeDevice{type2: ntagWithText("hello"), failReads: 1}
		r := testReader(dev)

		_, _, err := poll(t, r, dev)
		require.Error(t, err)

		token, _, err := poll(t, r, dev)
		require.NoError(t, err)
		assert.Equal(t, "hello", token.Text)
	})

	// A tag that failed twice and left must get all its attempts again when
	// it comes back. Otherwise one more failed read reports it by UID alone,
	// and a UID mapping could run in place of the tag's own text.
	t.Run("failures do not carry over after the tag leaves", func(t *testing.T) {
		t.Parallel()
		tag := ntagWithText("hello")
		dev := &fakeDevice{type2: tag, failReads: 100}
		r := testReader(dev)
		for range readAttempts - 1 {
			_, _, err := poll(t, r, dev)
			require.Error(t, err)
		}

		dev.clear()
		_, _, err := poll(t, r, dev)
		require.NoError(t, err)

		dev.type2 = tag
		token, _, err := poll(t, r, dev)
		require.Error(t, err, "the returning tag is retried, not given up on")
		assert.Nil(t, token)
	})
}

func TestPollDevice_PollError(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{pollErr: nfc.Error(nfc.EIO)}

	_, _, err := testReader(dev).pollDevice(dev, nil, 1, 0)
	require.ErrorIs(t, err, nfc.Error(nfc.EIO))

	dev.pollErr = nfc.Error(nfc.ETIMEOUT)
	token, removed, err := testReader(dev).pollDevice(dev, nil, 1, 0)
	require.NoError(t, err, "a poll that times out found no tag")
	assert.Nil(t, token)
	assert.False(t, removed)
}

func TestReadRetries(t *testing.T) {
	t.Parallel()
	var retries readRetries

	for i := range readAttempts - 1 {
		assert.False(t, retries.failed("aa"), "attempt %d", i)
	}
	assert.True(t, retries.failed("aa"))

	assert.False(t, retries.failed("bb"), "a different tag starts its own count")
	retries.reset()
	assert.False(t, retries.failed("bb"))
}

// write runs one write request the way the reader's loop does.
func write(r *Reader, req *WriteRequest) WriteRequestResult {
	req.Result = make(chan WriteRequestResult, 1)
	if req.Cancel == nil {
		req.Cancel = make(chan bool, 1)
	}
	if req.Ctx == nil {
		req.Ctx = context.Background()
	}
	r.writeTag(req)
	return <-req.Result
}

func TestWriteTag(t *testing.T) {
	t.Parallel()

	t.Run("NTAG", func(t *testing.T) {
		t.Parallel()
		tag := tagsim.NewType2(tagsim.PagesNTAG215)
		dev := &fakeDevice{type2: tag}
		r := testReader(dev)

		res := write(r, &WriteRequest{Text: "**launch.system:nes"})

		require.NoError(t, res.Err)
		require.NotNil(t, res.Token)
		assert.Equal(t, tokens.TypeNTAG, res.Token.Type)
		assert.Equal(t, "**launch.system:nes", res.Token.Text)
		assert.Equal(t, hex.EncodeToString(tag.UID()), res.Token.UID)
		want := textTLV("**launch.system:nes")
		assert.Equal(t, want, tag.User()[:len(want)])
		assert.Same(t, res.Token, r.prevToken, "the written tag is not scanned again while it stays")
		assert.True(t, r.writtenTag, "its removal is reported as the written tag leaving")
		assert.Nil(t, r.activeWrite)
	})

	t.Run("blank MIFARE Classic is formatted", func(t *testing.T) {
		t.Parallel()
		tag := tagsim.NewClassic(16)
		dev := &fakeDevice{classic: tag}

		res := write(testReader(dev), &WriteRequest{Text: "hello"})

		require.NoError(t, res.Err)
		assert.Equal(t, tokens.TypeMifare, res.Token.Type)
		assert.Equal(t, nfctag.KeyNDEF, tag.KeyA(1))
	})

	t.Run("unsupported tag", func(t *testing.T) {
		t.Parallel()
		dev := &fakeDevice{desfire: true}

		res := write(testReader(dev), &WriteRequest{Text: "hello"})

		require.ErrorIs(t, res.Err, readers.ErrUnsupportedTagType)
	})

	t.Run("excluded tag", func(t *testing.T) {
		t.Parallel()
		tag := tagsim.NewType2(tagsim.PagesNTAG215)
		dev := &fakeDevice{type2: tag}
		uid := hex.EncodeToString(tag.UID())

		res := write(testReader(dev), &WriteRequest{Text: "hello", Options: readers.WriteOptions{ExcludeUID: uid}})

		require.EqualError(t, res.Err, "refusing to write excluded tag: "+uid)
		assert.Equal(t, make([]byte, 16), tag.User()[:16])
	})

	t.Run("wrong target", func(t *testing.T) {
		t.Parallel()
		dev := &fakeDevice{type2: tagsim.NewType2(tagsim.PagesNTAG215)}

		res := write(testReader(dev), &WriteRequest{Text: "hello", Options: readers.WriteOptions{TargetUID: "aabb"}})

		require.ErrorContains(t, res.Err, "refusing to write wrong tag")
	})

	t.Run("cancelled while waiting", func(t *testing.T) {
		t.Parallel()
		cancel := make(chan bool, 1)
		cancel <- true

		res := write(testReader(&fakeDevice{}), &WriteRequest{Text: "hello", Cancel: cancel})

		assert.True(t, res.Cancelled)
	})

	t.Run("context ended while waiting", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		res := write(testReader(&fakeDevice{}), &WriteRequest{Text: "hello", Ctx: ctx})

		require.ErrorIs(t, res.Err, context.Canceled)
	})

	t.Run("device closed", func(t *testing.T) {
		t.Parallel()
		r := NewACR122Reader(&config.Instance{})

		res := write(r, &WriteRequest{Text: "hello"})

		require.Error(t, res.Err)
	})

	t.Run("write already in progress", func(t *testing.T) {
		t.Parallel()
		r := testReader(&fakeDevice{})
		r.activeWrite = &WriteRequest{}

		res := write(r, &WriteRequest{Text: "hello"})

		require.ErrorContains(t, res.Err, "write already in progress")
	})
}
