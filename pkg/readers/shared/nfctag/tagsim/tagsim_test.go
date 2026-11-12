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

package tagsim_test

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag/tagsim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The simulated tags stand in for hardware in every driver's tests, so the
// ways they behave like the real thing are pinned here.

func TestType2_FactoryLayout(t *testing.T) {
	t.Parallel()

	sizes := []struct {
		name   string
		pages  int
		ccSize byte
	}{
		{name: "NTAG213", pages: tagsim.PagesNTAG213, ccSize: 0x12},
		{name: "NTAG215", pages: tagsim.PagesNTAG215, ccSize: 0x3E},
		{name: "NTAG216", pages: tagsim.PagesNTAG216, ccSize: 0x6D},
		{name: "Ultralight", pages: tagsim.PagesUltralight, ccSize: 0x06},
	}
	for _, tt := range sizes {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tag := tagsim.NewType2(tt.pages)

			assert.Len(t, tag.Mem, tt.pages*4)
			assert.Len(t, tag.UID(), 7)
			assert.Equal(t, []byte{0xE1, 0x10, tt.ccSize, 0x00}, tag.Mem[12:16])
			assert.Len(t, tag.User(), (tt.pages-4)*4)
			if tt.pages >= tagsim.PagesNTAG213 {
				config := tag.Mem[(tt.pages-4)*4:]
				assert.Equal(t, byte(0xFF), config[3], "AUTH0 as delivered")
			}
		})
	}
}

func TestType2_ReadRollsOverAtTheEnd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tag := tagsim.NewType2(tagsim.PagesNTAG213)
	tag.SetUser([]byte{1, 2, 3, 4, 5, 6, 7, 8})

	block, err := tag.ReadBlock(ctx, 4)
	require.NoError(t, err)
	assert.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 0, 0, 0, 0}, block)
	assert.Equal(t, 1, tag.Reads)

	last, err := tag.ReadBlock(ctx, tagsim.PagesNTAG213-1)
	require.NoError(t, err)
	assert.Equal(t, tag.Mem[0:12], last[4:], "a read past the last page continues from page 0")

	_, err = tag.ReadBlock(ctx, tagsim.PagesNTAG213)
	require.ErrorIs(t, err, tagsim.ErrOutOfRange)
	_, err = tag.ReadBlock(ctx, -1)
	require.ErrorIs(t, err, tagsim.ErrOutOfRange)
}

func TestType2_WriteRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tag := tagsim.NewType2(tagsim.PagesNTAG213)

	require.NoError(t, tag.WriteBlock(ctx, 4, []byte{9, 8, 7, 6}))
	assert.Equal(t, []byte{9, 8, 7, 6}, tag.User()[:4])

	require.ErrorIs(t, tag.WriteBlock(ctx, 4, []byte{1, 2, 3}), tagsim.ErrBadLength)
	require.ErrorIs(t, tag.WriteBlock(ctx, 0, []byte{1, 2, 3, 4}), tagsim.ErrReadOnly)
	require.ErrorIs(t, tag.WriteBlock(ctx, 1, []byte{1, 2, 3, 4}), tagsim.ErrReadOnly)
	require.ErrorIs(t, tag.WriteBlock(ctx, tagsim.PagesNTAG213, []byte{1, 2, 3, 4}), tagsim.ErrOutOfRange)
}

func TestType2_CapabilityContainerIsOneTimeProgrammable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tag := tagsim.NewType2(tagsim.PagesNTAG215)
	tag.ClearCC()
	require.Equal(t, []byte{0, 0, 0, 0}, tag.Mem[12:16])

	require.NoError(t, tag.WriteBlock(ctx, 3, []byte{0xE1, 0x10, 0x3E, 0x00}))
	require.NoError(t, tag.WriteBlock(ctx, 3, []byte{0x00, 0x00, 0x01, 0x00}))

	assert.Equal(t, []byte{0xE1, 0x10, 0x3F, 0x00}, tag.Mem[12:16], "bits can be set and never cleared")
}

func TestClassic_FactoryLayout(t *testing.T) {
	t.Parallel()

	oneK := tagsim.NewClassic(16)
	assert.Len(t, oneK.Blocks, 64)
	assert.Len(t, oneK.UID(), 4)
	assert.Len(t, oneK.Data(), 15*3*16)
	assert.Equal(t, nfctag.KeyFactory, oneK.KeyA(0))
	assert.Equal(t, nfctag.KeyFactory, oneK.KeyA(15))
	assert.Equal(t, []byte{0xFF, 0x07, 0x80, 0x69}, oneK.Blocks[7][6:10])

	fourK := tagsim.NewClassic(40)
	assert.Len(t, fourK.Blocks, 32*4+8*16)
	assert.Len(t, fourK.Data(), (31*3+8*15)*16)
	assert.Equal(t, []byte{0xFF, 0x07, 0x80, 0x69}, fourK.Blocks[143][6:10], "sector 32's trailer is its 16th block")
}

func TestClassic_Authentication(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tag := tagsim.NewClassic(16)

	_, err := tag.ReadBlock(ctx, 4)
	require.ErrorIs(t, err, tagsim.ErrNotAuthenticated)
	require.ErrorIs(t, tag.WriteBlock(ctx, 4, make([]byte, 16)), tagsim.ErrNotAuthenticated)

	require.ErrorIs(t, tag.Authenticate(ctx, 4, nfctag.KeyA, nfctag.KeyNDEF), tagsim.ErrAuthFailed)
	require.NoError(t, tag.Authenticate(ctx, 4, nfctag.KeyA, nfctag.KeyFactory))
	_, err = tag.ReadBlock(ctx, 6)
	require.NoError(t, err)
	_, err = tag.ReadBlock(ctx, 8)
	require.ErrorIs(t, err, tagsim.ErrNotAuthenticated, "authentication covers one sector")

	require.ErrorIs(t, tag.Authenticate(ctx, 4, nfctag.KeyB, nfctag.KeyNDEF), tagsim.ErrAuthFailed)
	_, err = tag.ReadBlock(ctx, 4)
	require.ErrorIs(t, err, tagsim.ErrNotAuthenticated, "a failed attempt drops the earlier one")

	require.ErrorIs(t, tag.Authenticate(ctx, 64, nfctag.KeyA, nfctag.KeyFactory), tagsim.ErrOutOfRange)
	_, err = tag.ReadBlock(ctx, 64)
	require.ErrorIs(t, err, tagsim.ErrOutOfRange)
}

func TestClassic_TrailerHoldsTheKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tag := tagsim.NewClassic(16)
	require.NoError(t, tag.Authenticate(ctx, 7, nfctag.KeyA, nfctag.KeyFactory))

	trailer, err := tag.ReadBlock(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, make([]byte, 6), trailer[0:6], "key A never reads back")
	assert.Equal(t, nfctag.KeyFactory[:], trailer[10:16])

	formatted := make([]byte, 0, 16)
	formatted = append(formatted, nfctag.KeyNDEF[:]...)
	formatted = append(formatted, trailer[6:10]...)
	formatted = append(formatted, nfctag.KeyNDEF[:]...)
	require.NoError(t, tag.WriteBlock(ctx, 7, formatted))
	assert.Equal(t, 1, tag.Writes)
	assert.Equal(t, nfctag.KeyNDEF, tag.KeyA(1))

	require.ErrorIs(t, tag.Authenticate(ctx, 4, nfctag.KeyA, nfctag.KeyFactory), tagsim.ErrAuthFailed)
	require.NoError(t, tag.Authenticate(ctx, 4, nfctag.KeyB, nfctag.KeyNDEF))
	require.ErrorIs(t, tag.WriteBlock(ctx, 4, []byte{1, 2, 3, 4}), tagsim.ErrBadLength)
}

func TestClassic_DataSkipsTrailers(t *testing.T) {
	t.Parallel()
	tag := tagsim.NewClassic(16)
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i + 1)
	}

	tag.Format()
	tag.SetData(data)

	assert.Equal(t, nfctag.KeyNDEF, tag.KeyA(1))
	assert.Equal(t, nfctag.KeyFactory, tag.KeyA(0), "sector 0 is never formatted")
	assert.Equal(t, data, tag.Data()[:100])
	assert.Equal(t, data[:16], tag.Blocks[4])
	assert.Equal(t, data[48:64], tag.Blocks[8], "the fourth block of data lands after sector 1's trailer")
	assert.Equal(t, []byte{0xFF, 0x07, 0x80, 0x69}, tag.Blocks[7][6:10])

	tag.SetKeys(2, nfctag.Key{1, 2, 3, 4, 5, 6})
	assert.Equal(t, nfctag.Key{1, 2, 3, 4, 5, 6}, tag.KeyA(2))
}
