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

package nfctag_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag/tagsim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tlv wraps an NDEF message as a tag stores it.
//
//nolint:gosec // test messages are far shorter than 255 bytes
func tlv(message []byte) []byte {
	out := append([]byte{0x03, byte(len(message))}, message...)
	return append(out, 0xFE)
}

// textTLV is the NDEF data a tag holds for a short text record.
//
//nolint:gosec // test text is far shorter than 255 bytes
func textTLV(text string) []byte {
	return tlv(append([]byte{0xD1, 0x01, byte(len(text) + 3), 'T', 0x02, 'e', 'n'}, text...))
}

func type2Tag(sim *tagsim.Type2) *nfctag.Tag {
	return &nfctag.Tag{IO: sim, UID: sim.UID(), Kind: nfctag.KindType2}
}

func classicTag(sim *tagsim.Classic, kind nfctag.Kind) *nfctag.Tag {
	return &nfctag.Tag{IO: sim, UID: sim.UID(), Kind: kind}
}

func TestReadType2_Text(t *testing.T) {
	t.Parallel()

	sizes := map[string]int{
		"NTAG213": tagsim.PagesNTAG213,
		"NTAG215": tagsim.PagesNTAG215,
		"NTAG216": tagsim.PagesNTAG216,
	}
	for name, pages := range sizes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sim := tagsim.NewType2(pages)
			sim.SetUser(textTLV("@NES/Lode Runner"))

			res, err := nfctag.Read(context.Background(), type2Tag(sim))

			require.NoError(t, err)
			assert.Equal(t, tokens.TypeNTAG, res.Type)
			assert.Equal(t, "@NES/Lode Runner", res.Text)
		})
	}
}

func TestReadType2_DataIsMemoryThroughTerminatorInWholeBlocks(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG215)
	stored := textTLV("@NES/Lode Runner")
	sim.SetUser(stored)

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	require.Len(t, stored, 26)
	assert.Equal(t, sim.User()[:32], res.Data, "26 bytes of NDEF data round up to two blocks")
}

func TestReadType2_LongMessageUsesLongTLVLength(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("a", 400)
	sim := tagsim.NewType2(tagsim.PagesNTAG215)
	_, err := nfctag.Write(context.Background(), type2Tag(sim), text)
	require.NoError(t, err)
	require.Equal(t, []byte{0x03, 0xFF}, sim.User()[:2], "messages of 255 bytes and over use the 3-byte length")

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Equal(t, text, res.Text)
}

func TestReadType2_SkipsLockControlTLV(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG216)
	sim.SetUser(append([]byte{0x01, 0x03, 0xA0, 0x0C, 0x34}, textTLV("**launch.random:snes")...))

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Equal(t, "**launch.random:snes", res.Text)
}

func TestReadType2_FirstRecordOfMultiRecordMessage(t *testing.T) {
	t.Parallel()
	first := append([]byte{0x91, 0x01, 0x06, 'T', 0x02, 'e', 'n'}, "one"...)
	second := append([]byte{0x51, 0x01, 0x06, 'T', 0x02, 'e', 'n'}, "two"...)
	message := make([]byte, 0, len(first)+len(second))
	message = append(message, first...)
	message = append(message, second...)
	sim := tagsim.NewType2(tagsim.PagesNTAG213)
	sim.SetUser(tlv(message))

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Equal(t, "one", res.Text)
}

func TestReadType2_URIRecord(t *testing.T) {
	t.Parallel()
	record := append([]byte{0xD1, 0x01, 0x0C, 'U', 0x04}, "zaparoo.org"...)
	sim := tagsim.NewType2(tagsim.PagesNTAG213)
	sim.SetUser(tlv(record))

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Equal(t, "https://zaparoo.org", res.Text)
}

func TestReadType2_EmptyTextRecordIsEmptyText(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG213)
	sim.SetUser(textTLV(""))

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Empty(t, res.Text, "an empty record must not come back as a JSON description")
}

func TestReadType2_BlankTag(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG215)

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Equal(t, tokens.TypeNTAG, res.Type)
	assert.Empty(t, res.Text)
	assert.LessOrEqual(t, sim.Reads, 3, "a blank tag is not read end to end")
}

func TestReadType2_NoCapabilityContainerIsNotRead(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG215)
	sim.SetUser(textTLV("ignored"))
	sim.ClearCC()

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Equal(t, tokens.TypeNTAG, res.Type)
	assert.Empty(t, res.Text)
}

func TestReadType2_Amiibo(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG215)
	copy(sim.Mem[9:16], []byte{0x48, 0x0F, 0xE0, 0xF1, 0x10, 0xFF, 0xEE})
	id := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	copy(sim.Mem[21*4:], id)

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Equal(t, tokens.TypeAmiibo, res.Type)
	assert.Equal(t, id, res.Data)
	assert.Empty(t, res.Text)
}

func TestReadType2_LegoDimensionsKeepsPayloadOutOfText(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG213)
	user := make([]byte, 0, 27)
	user = append(user, 0x01, 0x03, 0xA0, 0x0C, 0x34, 0x03, 0x13, 0xD1, 0x01, 0x0F, 0x54, 0x02, 0x65, 0x6E)
	user = append(user, "ABCDEFGHIJKL"...)
	user = append(user, 0xFE)
	sim.SetUser(user)

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Equal(t, tokens.TypeLegoDimensions, res.Type)
	assert.Empty(t, res.Text, "a toy's text must not be run as ZapScript")
	assert.Contains(t, string(res.Data), "ABCDEFGHIJKL")
}

func TestReadType2_OversizedLengthStopsAtEndOfMemory(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG213)
	sim.SetUser([]byte{0x03, 0xFF, 0xFF, 0xFF, 0xD1, 0x01})

	res, err := nfctag.Read(context.Background(), type2Tag(sim))

	require.NoError(t, err)
	assert.Empty(t, res.Text)
	assert.Len(t, res.Data, 144)
}

type failingIO struct {
	nfctag.BlockIO
	failRead  int
	corrupt   bool
	readCalls int
}

var errIO = errors.New("io failure")

func (f *failingIO) ReadBlock(ctx context.Context, addr int) ([]byte, error) {
	f.readCalls++
	if f.failRead != 0 && f.readCalls >= f.failRead {
		return nil, errIO
	}
	data, err := f.BlockIO.ReadBlock(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if f.corrupt && addr >= 4 {
		data[0] ^= 0xFF
	}
	return data, nil
}

func TestReadType2_ReadFailureStillReportsType(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG215)
	sim.SetUser(textTLV(strings.Repeat("b", 100)))
	tag := &nfctag.Tag{IO: &failingIO{BlockIO: sim, failRead: 3}, UID: sim.UID(), Kind: nfctag.KindType2}

	res, err := nfctag.Read(context.Background(), tag)

	require.ErrorIs(t, err, errIO)
	assert.Equal(t, tokens.TypeNTAG, res.Type)
	assert.Empty(t, res.Text)
}

func TestWriteType2_BytesOnTag(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG213)
	sim.SetUser(textTLV("an older and much longer message"))

	res, err := nfctag.Write(context.Background(), type2Tag(sim), "**stop")

	require.NoError(t, err)
	want := []byte{0x03, 0x0D, 0xD1, 0x01, 0x09, 0x54, 0x02, 0x65, 0x6E, '*', '*', 's', 't', 'o', 'p', 0xFE}
	assert.Equal(t, want, sim.User()[:16])
	assert.Equal(t, tokens.TypeNTAG, res.Type)
	assert.Equal(t, "**stop", res.Text)
	assert.Equal(t, want, res.Data)
}

func TestWriteType2_Capacity(t *testing.T) {
	t.Parallel()

	// A text record costs 7 bytes of record header, a TLV header and the
	// terminator on top of the text.
	tests := []struct {
		name    string
		pages   int
		fits    int
		tooMany int
	}{
		{name: "NTAG213", pages: tagsim.PagesNTAG213, fits: 144 - 10, tooMany: 144 - 9},
		{name: "NTAG215", pages: tagsim.PagesNTAG215, fits: 504 - 15, tooMany: 504 - 14},
		{name: "NTAG216", pages: tagsim.PagesNTAG216, fits: 888 - 15, tooMany: 888 - 14},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sim := tagsim.NewType2(tt.pages)

			_, err := nfctag.Write(context.Background(), type2Tag(sim), strings.Repeat("x", tt.fits))
			require.NoError(t, err)

			_, err = nfctag.Write(context.Background(), type2Tag(sim), strings.Repeat("x", tt.tooMany))
			require.ErrorIs(t, err, nfctag.ErrTooLarge)
		})
	}
}

func TestWriteType2_WritesMissingCapabilityContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		pages  int
		ccSize byte
	}{
		{name: "NTAG213", pages: tagsim.PagesNTAG213, ccSize: 0x12},
		{name: "NTAG215", pages: tagsim.PagesNTAG215, ccSize: 0x3E},
		{name: "NTAG216", pages: tagsim.PagesNTAG216, ccSize: 0x6D},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sim := tagsim.NewType2(tt.pages)
			sim.ClearCC()

			_, err := nfctag.Write(context.Background(), type2Tag(sim), "hello")
			require.NoError(t, err)
			assert.Equal(t, []byte{0xE1, 0x10, tt.ccSize, 0x00}, sim.Mem[12:16])

			res, err := nfctag.Read(context.Background(), type2Tag(sim))
			require.NoError(t, err)
			assert.Equal(t, "hello", res.Text, "a tag this package wrote must read back")
		})
	}
}

func TestWriteType2_RefusesTagsItCannotFormat(t *testing.T) {
	t.Parallel()

	t.Run("amiibo", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewType2(tagsim.PagesNTAG215)
		copy(sim.Mem[9:16], []byte{0x48, 0x0F, 0xE0, 0xF1, 0x10, 0xFF, 0xEE})
		before := append([]byte(nil), sim.Mem...)

		_, err := nfctag.Write(context.Background(), type2Tag(sim), "hello")

		require.ErrorIs(t, err, readers.ErrUnsupportedTagType)
		assert.Equal(t, before, sim.Mem)
	})

	t.Run("foreign capability container", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewType2(tagsim.PagesNTAG215)
		copy(sim.Mem[12:16], []byte{0xF2, 0x00, 0x00, 0x00})

		_, err := nfctag.Write(context.Background(), type2Tag(sim), "hello")

		require.ErrorIs(t, err, readers.ErrUnsupportedTagType)
	})

	t.Run("too small to identify", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewType2(tagsim.PagesUltralight)
		sim.ClearCC()

		_, err := nfctag.Write(context.Background(), type2Tag(sim), "hello")

		require.ErrorIs(t, err, readers.ErrUnsupportedTagType)
	})
}

func TestWriteType2_VerifyCatchesBadReadBack(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewType2(tagsim.PagesNTAG215)
	tag := &nfctag.Tag{IO: &failingIO{BlockIO: sim, corrupt: true}, UID: sim.UID(), Kind: nfctag.KindType2}

	_, err := nfctag.Write(context.Background(), tag, "hello")

	require.ErrorIs(t, err, nfctag.ErrVerify)
}

func TestWrite_EmptyText(t *testing.T) {
	t.Parallel()
	_, err := nfctag.Write(context.Background(), type2Tag(tagsim.NewType2(tagsim.PagesNTAG213)), "")
	require.ErrorIs(t, err, nfctag.ErrEmptyText)
}

func TestUnknownKind(t *testing.T) {
	t.Parallel()
	tag := &nfctag.Tag{UID: []byte{1, 2, 3, 4}, Kind: nfctag.KindUnknown}

	res, err := nfctag.Read(context.Background(), tag)
	require.NoError(t, err)
	assert.Equal(t, tokens.TypeUnknown, res.Type)

	_, err = nfctag.Write(context.Background(), tag, "hello")
	require.ErrorIs(t, err, readers.ErrUnsupportedTagType)
}

func TestReadClassic_FormattedTag(t *testing.T) {
	t.Parallel()

	kinds := map[string]struct {
		kind    nfctag.Kind
		sectors int
	}{
		"1K": {kind: nfctag.KindClassic1K, sectors: 16},
		"4K": {kind: nfctag.KindClassic4K, sectors: 40},
	}
	for name, tt := range kinds {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sim := tagsim.NewClassic(tt.sectors)
			sim.Format()
			sim.SetData(textTLV("test4"))

			res, err := nfctag.Read(context.Background(), classicTag(sim, tt.kind))

			require.NoError(t, err)
			assert.Equal(t, tokens.TypeMifare, res.Type)
			assert.Equal(t, "test4", res.Text)
			assert.Equal(t, sim.Data()[:16], res.Data)
		})
	}
}

func TestReadClassic_BlankTagIsUIDOnly(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewClassic(16)

	res, err := nfctag.Read(context.Background(), classicTag(sim, nfctag.KindClassic1K))

	require.NoError(t, err)
	assert.Equal(t, tokens.TypeMifare, res.Type)
	assert.Empty(t, res.Text)
	assert.Empty(t, res.Data)
}

func TestReadClassic_MessageAcrossSectors(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("c", 150)
	sim := tagsim.NewClassic(16)
	_, err := nfctag.Write(context.Background(), classicTag(sim, nfctag.KindClassic1K), text)
	require.NoError(t, err)

	res, err := nfctag.Read(context.Background(), classicTag(sim, nfctag.KindClassic1K))

	require.NoError(t, err)
	assert.Equal(t, text, res.Text)
}

func TestWriteClassic_FormatsOnlyTheSectorsItNeeds(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewClassic(16)

	res, err := nfctag.Write(context.Background(), classicTag(sim, nfctag.KindClassic1K), "test4")

	require.NoError(t, err)
	assert.Equal(t, "test4", res.Text)
	assert.Equal(t, nfctag.KeyNDEF, sim.KeyA(1))
	assert.Equal(t, nfctag.KeyFactory, sim.KeyA(2), "untouched sectors keep their key")
	assert.Equal(t, nfctag.KeyFactory, sim.KeyA(0), "sector 0 is never formatted")
	assert.Equal(t,
		[]byte{0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7, 0xFF, 0x07, 0x80, 0x69, 0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7},
		sim.Blocks[7], "the trailer keeps the tag's own access bits")
	assert.Equal(t, textTLV("test4"), sim.Data()[:len(textTLV("test4"))])
}

func TestWriteClassic_ClearsLeftoversOfALongerMessage(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewClassic(16)
	tag := classicTag(sim, nfctag.KindClassic1K)
	_, err := nfctag.Write(context.Background(), tag, strings.Repeat("d", 120))
	require.NoError(t, err)

	_, err = nfctag.Write(context.Background(), tag, "short")
	require.NoError(t, err)

	data := sim.Data()
	assert.Equal(t, make([]byte, len(data)-16), data[16:], "nothing of the old message is left")
}

func TestWriteClassic_4KUsesLargeSectors(t *testing.T) {
	t.Parallel()
	// Sectors 1 to 31 hold 1488 bytes, so this reaches the 16-block sectors.
	text := strings.Repeat("e", 1600)
	sim := tagsim.NewClassic(40)
	tag := classicTag(sim, nfctag.KindClassic4K)

	_, err := nfctag.Write(context.Background(), tag, text)
	require.NoError(t, err)
	assert.Equal(t, nfctag.KeyNDEF, sim.KeyA(32))
	assert.Equal(t, nfctag.KeyFactory, sim.KeyA(33))

	res, err := nfctag.Read(context.Background(), tag)
	require.NoError(t, err)
	assert.Equal(t, text, res.Text)
}

func TestWriteClassic_Capacity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     nfctag.Kind
		sectors  int
		capacity int
	}{
		{name: "1K", kind: nfctag.KindClassic1K, sectors: 16, capacity: 720},
		{name: "4K", kind: nfctag.KindClassic4K, sectors: 40, capacity: 3408},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Long-form TLV and record headers cost 15 bytes with the terminator.
			sim := tagsim.NewClassic(tt.sectors)
			tag := classicTag(sim, tt.kind)

			_, err := nfctag.Write(context.Background(), tag, strings.Repeat("f", tt.capacity-15))
			require.NoError(t, err)

			_, err = nfctag.Write(context.Background(), tag, strings.Repeat("f", tt.capacity-14))
			require.ErrorIs(t, err, nfctag.ErrTooLarge)
		})
	}
}

func TestWriteClassic_UnknownKeyIsUnsupported(t *testing.T) {
	t.Parallel()
	sim := tagsim.NewClassic(16)
	sim.SetKeys(1, nfctag.Key{1, 2, 3, 4, 5, 6})

	_, err := nfctag.Write(context.Background(), classicTag(sim, nfctag.KindClassic1K), "hello")

	require.ErrorIs(t, err, readers.ErrUnsupportedTagType)
	assert.Zero(t, sim.Writes)
}

func TestClassic_NeedsAuthenticator(t *testing.T) {
	t.Parallel()
	tag := &nfctag.Tag{IO: tagsim.NewType2(tagsim.PagesNTAG213), UID: []byte{1, 2, 3, 4}, Kind: nfctag.KindClassic1K}

	res, err := nfctag.Read(context.Background(), tag)
	require.Error(t, err)
	assert.Equal(t, tokens.TypeMifare, res.Type)

	_, err = nfctag.Write(context.Background(), tag, "hello")
	require.Error(t, err)
}

func TestTextFromMemory_NoNDEF(t *testing.T) {
	t.Parallel()
	assert.Empty(t, nfctag.TextFromMemory(nil))
	assert.Empty(t, nfctag.TextFromMemory(make([]byte, 32)))
	assert.Empty(t, nfctag.TextFromMemory([]byte{0xFE, 0x00, 0x00}))
}
