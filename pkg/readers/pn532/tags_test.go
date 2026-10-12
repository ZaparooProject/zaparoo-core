// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package pn532

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag/tagsim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadTag_NoTagOps(t *testing.T) {
	t.Parallel()

	reader := &Reader{tagOps: nil}
	detectedTag := &pn532.DetectedTag{
		UID:        "test-uid",
		Type:       pn532.TagTypeNTAG,
		TargetData: []byte{0x01, 0x02, 0x03},
	}

	tokenType, text, data := reader.readTag(context.Background(), detectedTag)

	assert.Equal(t, tokens.TypeNTAG, tokenType)
	assert.Empty(t, text)
	assert.Empty(t, data, "detection data is not tag memory and is not reported as it")
}

// pagedTag answers go-pn532's block read from an in-memory NTAG, stopping at
// the last page as go-pn532 does.
type pagedTag struct {
	sim *tagsim.Type2
}

func (p *pagedTag) ReadBlocks(_ context.Context, start, end byte) ([]byte, error) {
	pages := len(p.sim.Mem) / 4
	if int(start) >= pages {
		return nil, errors.New("page out of range")
	}
	last := min(int(end), pages-1)
	return append([]byte(nil), p.sim.Mem[int(start)*4:(last+1)*4]...), nil
}

// classicTag answers go-pn532's block read from an in-memory MIFARE Classic
// tag, authenticating each sector with whichever key works.
type classicTag struct {
	sim *tagsim.Classic
}

func (c *classicTag) ReadBlocks(ctx context.Context, start, end byte) ([]byte, error) {
	var out []byte
	for block := int(start); block <= int(end); block++ {
		authErr := c.sim.Authenticate(ctx, block, nfctag.KeyA, nfctag.KeyNDEF)
		if authErr != nil {
			authErr = c.sim.Authenticate(ctx, block, nfctag.KeyA, nfctag.KeyFactory)
		}
		if authErr != nil {
			return nil, fmt.Errorf("authenticate block %d: %w", block, authErr)
		}
		data, err := c.sim.ReadBlock(ctx, block)
		if err != nil {
			return nil, fmt.Errorf("read block %d: %w", block, err)
		}
		out = append(out, data...)
	}
	return out, nil
}

// textTLV is the NDEF data a tag holds for a short text record.
//
//nolint:gosec // test text is far shorter than 255 bytes
func textTLV(text string) []byte {
	record := append([]byte{0xD1, 0x01, byte(len(text) + 3), 'T', 0x02, 'e', 'n'}, text...)
	out := append([]byte{0x03, byte(len(record))}, record...)
	return append(out, 0xFE)
}

// The expected values are the ones the ccid and libnfc drivers' tests use for
// the same tags: every driver must give the same token.
func TestReadMemory_SameDataAsOtherDrivers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("NTAG", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewType2(tagsim.PagesNTAG215)
		sim.SetUser(textTLV("@NES/Lode Runner"))

		res, err := readMemory(ctx, &pagedTag{sim: sim}, nfctag.KindType2, sim.UID())

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

		res, err := readMemory(ctx, &classicTag{sim: sim}, nfctag.KindClassic1K, sim.UID())

		require.NoError(t, err)
		assert.Equal(t, tokens.TypeMifare, res.Type)
		assert.Equal(t, "030cd101085402656e7465737434fe00", hex.EncodeToString(res.Data))
	})

	t.Run("Amiibo", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewType2(tagsim.PagesNTAG215)
		copy(sim.Mem[9:16], []byte{0x48, 0x0F, 0xE0, 0xF1, 0x10, 0xFF, 0xEE})
		copy(sim.Mem[21*4:], []byte{1, 2, 3, 4, 5, 6, 7, 8})

		res, err := readMemory(ctx, &pagedTag{sim: sim}, nfctag.KindType2, sim.UID())

		require.NoError(t, err)
		assert.Equal(t, tokens.TypeAmiibo, res.Type)
		assert.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, res.Data)
	})

	t.Run("message that fills the tag", func(t *testing.T) {
		t.Parallel()
		// The last read runs past the tag's final page and comes back short.
		sim := tagsim.NewType2(tagsim.PagesNTAG213)
		text := strings.Repeat("z", 144-10)
		_, err := nfctag.Write(ctx, &nfctag.Tag{IO: sim, UID: sim.UID(), Kind: nfctag.KindType2}, text)
		require.NoError(t, err)

		res, err := readMemory(ctx, &pagedTag{sim: sim}, nfctag.KindType2, sim.UID())

		require.NoError(t, err)
		assert.Equal(t, text, res.Text)
	})

	t.Run("read failure", func(t *testing.T) {
		t.Parallel()
		sim := tagsim.NewClassic(16)
		sim.SetKeys(1, nfctag.Key{1, 2, 3, 4, 5, 6})

		res, err := readMemory(ctx, &classicTag{sim: sim}, nfctag.KindClassic1K, sim.UID())

		require.Error(t, err)
		assert.Equal(t, tokens.TypeMifare, res.Type)
		assert.Empty(t, res.Data)
	})
}

func TestBlockIO_IsReadOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	io := &blockIO{ops: &pagedTag{sim: tagsim.NewType2(tagsim.PagesNTAG213)}, type2: true}

	require.ErrorIs(t, io.WriteBlock(ctx, 4, []byte{1, 2, 3, 4}), errReadOnly)
	require.NoError(t, io.Authenticate(ctx, 4, nfctag.KeyA, nfctag.KeyNDEF))
	_, err := io.ReadBlock(ctx, 256)
	require.Error(t, err)
	_, err = io.ReadBlock(ctx, -1)
	require.Error(t, err)
	_, err = io.ReadBlock(ctx, 200)
	require.Error(t, err, "a page past the end of the tag")
}

func TestClassifyWriteError(t *testing.T) {
	t.Parallel()
	errFault := errors.New("transport fault")

	noTag := classifyWriteError(errors.New("timeout waiting for tag"))
	require.ErrorIs(t, noTag, readers.ErrTagNotDetected)

	midPoll := classifyWriteError(fmt.Errorf("tag detection failed: %w", context.DeadlineExceeded))
	require.ErrorIs(t, midPoll, readers.ErrTagNotDetected, "the wait can run out during a poll")

	unsupported := classifyWriteError(fmt.Errorf("failed to create tag: %w", pn532.ErrInvalidTag))
	require.ErrorIs(t, unsupported, readers.ErrUnsupportedTagType)
	require.ErrorIs(t, unsupported, pn532.ErrInvalidTag)

	other := classifyWriteError(errFault)
	require.ErrorIs(t, other, errFault)
	require.NotErrorIs(t, other, readers.ErrTagNotDetected)
	require.NotErrorIs(t, other, readers.ErrUnsupportedTagType)

	require.ErrorIs(t, classifyWriteError(context.Canceled), context.Canceled)
}
