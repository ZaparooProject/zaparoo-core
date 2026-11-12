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

package tags_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/libnfc/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag/tagsim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/clausecker/nfc/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errNoAnswer = errors.New("RF transmission error")

// fakeDevice is a libnfc device with one tag in its field. It answers the
// tag's native commands the way the tag would, including going silent after
// a failed authentication until it is selected again.
type fakeDevice struct {
	type2   *tagsim.Type2
	classic *tagsim.Classic
	sent    [][]byte
	selects int
	short   bool
	halted  bool
}

func (d *fakeDevice) InitiatorSelectPassiveTarget(nfc.Modulation, []byte) (nfc.Target, error) {
	d.selects++
	d.halted = false
	return nil, nil //nolint:nilnil // the adapter only needs the tag woken up
}

func (d *fakeDevice) InitiatorTransceiveBytes(tx, rx []byte, _ int) (int, error) {
	d.sent = append(d.sent, append([]byte(nil), tx...))
	if d.halted {
		return 0, errNoAnswer
	}
	ctx := context.Background()
	addr := int(tx[1])

	switch tx[0] {
	case 0x30:
		var block []byte
		var err error
		if d.type2 != nil {
			block, err = d.type2.ReadBlock(ctx, addr)
		} else {
			block, err = d.classic.ReadBlock(ctx, addr)
		}
		if err != nil {
			return 0, errNoAnswer
		}
		if d.short {
			block = block[:8]
		}
		return copy(rx, block), nil
	case 0xA2:
		if d.type2 == nil || d.type2.WriteBlock(ctx, addr, tx[2:]) != nil {
			return 0, errNoAnswer
		}
		return 0, nil
	case 0xA0:
		if d.classic == nil || d.classic.WriteBlock(ctx, addr, tx[2:]) != nil {
			return 0, errNoAnswer
		}
		return 0, nil
	case 0x60, 0x61:
		var key nfctag.Key
		copy(key[:], tx[2:8])
		if d.classic == nil || d.classic.Authenticate(ctx, addr, nfctag.KeyType(tx[0]), key) != nil {
			d.halted = true
			return 0, errNoAnswer
		}
		return 0, nil
	default:
		return 0, errNoAnswer
	}
}

// textTLV is the NDEF data a tag holds for a short text record.
//
//nolint:gosec // test text is far shorter than 255 bytes
func textTLV(text string) []byte {
	record := append([]byte{0xD1, 0x01, byte(len(text) + 3), 'T', 0x02, 'e', 'n'}, text...)
	out := append([]byte{0x03, byte(len(record))}, record...)
	return append(out, 0xFE)
}

// The expected values here are the same ones the ccid driver's tests use for
// the same tags: every driver must give the same token.
func TestIO_SameTokenAsOtherDrivers(t *testing.T) {
	t.Parallel()

	t.Run("NTAG", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewType2(tagsim.PagesNTAG215)
		sim.SetUser(textTLV("@NES/Lode Runner"))
		dev := &fakeDevice{type2: sim}

		res, err := nfctag.Read(context.Background(),
			&nfctag.Tag{IO: tags.NewIO(dev, sim.UID()), UID: sim.UID(), Kind: nfctag.KindType2})

		require.NoError(t, err)
		assert.Equal(t, tokens.TypeNTAG, res.Type)
		assert.Equal(t, "@NES/Lode Runner", res.Text)
		assert.Equal(t, "0317d101135402656e404e45532f4c6f64652052756e6e6572fe000000000000",
			hex.EncodeToString(res.Data))
	})

	t.Run("MIFARE Classic", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewClassic(16)
		sim.Format()
		sim.SetData(textTLV("test4"))
		dev := &fakeDevice{classic: sim}

		res, err := nfctag.Read(context.Background(),
			&nfctag.Tag{IO: tags.NewIO(dev, sim.UID()), UID: sim.UID(), Kind: nfctag.KindClassic1K})

		require.NoError(t, err)
		assert.Equal(t, tokens.TypeMifare, res.Type)
		assert.Equal(t, "test4", res.Text)
		assert.Equal(t, "030cd101085402656e7465737434fe00", hex.EncodeToString(res.Data))
	})
}

func TestIO_WritesTheSameBytesAsOtherDrivers(t *testing.T) {
	t.Parallel()
	want := textTLV("**launch.system:nes")

	t.Run("NTAG", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewType2(tagsim.PagesNTAG215)
		dev := &fakeDevice{type2: sim}

		_, err := nfctag.Write(context.Background(),
			&nfctag.Tag{IO: tags.NewIO(dev, sim.UID()), UID: sim.UID(), Kind: nfctag.KindType2}, "**launch.system:nes")

		require.NoError(t, err)
		assert.Equal(t, want, sim.User()[:len(want)])
	})

	t.Run("blank MIFARE Classic", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewClassic(16)
		dev := &fakeDevice{classic: sim}

		_, err := nfctag.Write(context.Background(),
			&nfctag.Tag{IO: tags.NewIO(dev, sim.UID()), UID: sim.UID(), Kind: nfctag.KindClassic1K},
			"**launch.system:nes")

		require.NoError(t, err)
		assert.Equal(t, want, sim.Data()[:len(want)])
		assert.Equal(t, nfctag.KeyNDEF, sim.KeyA(1))
		assert.Positive(t, dev.selects, "the tag is selected again after the NDEF key is refused")
	})
}

func TestIO_UnknownKeyLeavesTheTagAlone(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewClassic(16)
	sim.SetKeys(1, nfctag.Key{1, 2, 3, 4, 5, 6})
	dev := &fakeDevice{classic: sim}

	_, err := nfctag.Write(context.Background(),
		&nfctag.Tag{IO: tags.NewIO(dev, sim.UID()), UID: sim.UID(), Kind: nfctag.KindClassic1K}, "hello")

	require.ErrorIs(t, err, readers.ErrUnsupportedTagType)
	assert.Zero(t, sim.Writes)
	assert.False(t, dev.halted, "the tag is left selected, not silent")
}

func TestIO_Commands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sim := tagsim.NewClassic(16)
	uid := sim.UID()
	dev := &fakeDevice{classic: sim}
	io := tags.NewIO(dev, uid)

	require.NoError(t, io.Authenticate(ctx, 4, nfctag.KeyA, nfctag.KeyFactory))
	block := make([]byte, 16)
	require.NoError(t, io.WriteBlock(ctx, 4, block))
	_, err := io.ReadBlock(ctx, 4)
	require.NoError(t, err)

	type2 := &fakeDevice{type2: tagsim.NewType2(tagsim.PagesNTAG213)}
	require.NoError(t, tags.NewIO(type2, nil).WriteBlock(ctx, 4, []byte{1, 2, 3, 4}))

	wantAuth := append([]byte{0x60, 0x04, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, uid...)
	assert.Equal(t, [][]byte{wantAuth, append([]byte{0xA0, 0x04}, block...), {0x30, 0x04}}, dev.sent)
	assert.Equal(t, [][]byte{{0xA2, 0x04, 1, 2, 3, 4}}, type2.sent)
}

func TestIO_RejectsBadArgumentsAndReplies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dev := &fakeDevice{type2: tagsim.NewType2(tagsim.PagesNTAG213)}
	io := tags.NewIO(dev, []byte{1, 2, 3, 4})

	_, err := io.ReadBlock(ctx, 256)
	require.Error(t, err)
	require.Error(t, io.WriteBlock(ctx, -1, []byte{1, 2, 3, 4}))
	require.Error(t, io.WriteBlock(ctx, 4, []byte{1, 2, 3}))
	require.Error(t, io.Authenticate(ctx, 256, nfctag.KeyA, nfctag.KeyNDEF))
	require.Error(t, tags.NewIO(dev, nil).Authenticate(ctx, 4, nfctag.KeyA, nfctag.KeyNDEF))
	assert.Empty(t, dev.sent)

	_, err = io.ReadBlock(ctx, 200)
	require.ErrorIs(t, err, errNoAnswer, "a read past the end of the tag")

	dev.short = true
	_, err = nfctag.Read(ctx, &nfctag.Tag{IO: io, UID: []byte{1, 2, 3, 4}, Kind: nfctag.KindType2})
	require.Error(t, err, "a short reply is not taken for a block")
}

func target(atqa [2]byte, sak byte, uid ...byte) *nfc.ISO14443aTarget {
	card := &nfc.ISO14443aTarget{Atqa: atqa, Sak: sak, UIDLen: len(uid)}
	copy(card.UID[:], uid)
	return card
}

func TestKindAndUID(t *testing.T) {
	t.Parallel()

	ntag := target([2]byte{0x00, 0x44}, 0x00, 0x04, 0x7A, 0x7E, 0x0A, 0xC0, 0x1C, 0x91)
	assert.Equal(t, nfctag.KindType2, tags.Kind(ntag))
	assert.Equal(t, []byte{0x04, 0x7A, 0x7E, 0x0A, 0xC0, 0x1C, 0x91}, tags.UID(ntag))
	assert.Equal(t, "047a7e0ac01c91", tags.GetTagUID(ntag))

	assert.Equal(t, nfctag.KindClassic1K, tags.Kind(target([2]byte{0x00, 0x04}, 0x08, 1, 2, 3, 4)))
	assert.Equal(t, nfctag.KindClassic4K, tags.Kind(target([2]byte{0x00, 0x02}, 0x18, 1, 2, 3, 4)))
	assert.Equal(t, nfctag.KindUnknown, tags.Kind(target([2]byte{0x03, 0x44}, 0x20, 1, 2, 3, 4, 5, 6, 7)),
		"a DESFire card is reported by UID alone")

	assert.Equal(t, nfctag.KindUnknown, tags.Kind(&nfc.FelicaTarget{}))
	assert.Nil(t, tags.UID(&nfc.FelicaTarget{}))
	assert.Empty(t, tags.GetTagUID(&nfc.FelicaTarget{}))

	bad := target([2]byte{0x00, 0x44}, 0x00)
	bad.UIDLen = 11
	assert.Nil(t, tags.UID(bad), "a UID length from the device is not trusted past the buffer")
}
