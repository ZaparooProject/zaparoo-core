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
//

package mmb

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type entry struct {
	title  string
	status byte
}

func buildCatalog(header byte, entries map[int]entry) []byte {
	cat := make([]byte, catalogSize)
	cat[headerFlagOff] = header
	for slot, e := range entries {
		o := (slot + 1) * entrySize
		copy(cat[o:o+12], e.title)
		cat[o+15] = e.status
	}
	return cat
}

func writeImage(t *testing.T, fs afero.Fs, data []byte) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, "BEEB.MMB", data, 0o600))
}

func TestReadCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries map[int]entry
		want    []Disk
	}{
		{
			name: "read only and read write",
			entries: map[int]entry{
				0: {"Elite", 0x00},
				3: {"Repton", 0x0F},
			},
			want: []Disk{
				{Title: "Elite", Number: 0, ReadOnly: true},
				{Title: "Repton", Number: 3},
			},
		},
		{
			name: "unformatted invalid and blank skipped",
			entries: map[int]entry{
				0: {"Gone", 0xF0},
				1: {"Bad", 0xFF},
				2: {"", 0x0F},
				3: {"            ", 0x00},
				4: {"Kept", 0x0F},
			},
			want: []Disk{{Title: "Kept", Number: 4}},
		},
		{
			name: "title sanitizing",
			entries: map[int]entry{
				0: {"A/B\\C", 0x0F},
				1: {"  X  Y ", 0x0F},
				5: {"Pad\x00\x00", 0x0F},
			},
			want: []Disk{
				{Title: "A B C", Number: 0},
				{Title: "X Y", Number: 1},
				{Title: "Pad", Number: 5},
			},
		},
		{
			name: "binary titles are not discs",
			entries: map[int]entry{
				0: {"X\x01Y", 0x0F},
				1: {"\xff\xfe\x80", 0x0F},
				2: {"\x01\x02\x7f", 0x0F},
				3: {"Real", 0x0F},
			},
			want: []Disk{{Title: "Real", Number: 3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			writeImage(t, fs, buildCatalog(0, tt.entries))
			got, err := ReadCatalog(fs, "BEEB.MMB")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReadCatalog_ExtendedChunks(t *testing.T) {
	t.Parallel()

	data := make([]byte, 2*chunkSize+catalogSize)
	copy(data, buildCatalog(0xA2, map[int]entry{0: {"One", 0x0F}}))
	copy(data[chunkSize:], buildCatalog(0, map[int]entry{
		0:   {"Two", 0x00},
		510: {"TwoLast", 0x0F},
	}))
	copy(data[2*chunkSize:], buildCatalog(0, map[int]entry{7: {"Three", 0x0F}}))

	fs := afero.NewMemMapFs()
	writeImage(t, fs, data)
	got, err := ReadCatalog(fs, "BEEB.MMB")
	require.NoError(t, err)
	assert.Equal(t, []Disk{
		{Title: "One", Number: 0},
		{Title: "Two", Number: 511, ReadOnly: true},
		{Title: "TwoLast", Number: 1021},
		{Title: "Three", Number: 1022 + 7},
	}, got)
}

func TestReadCatalog_TruncatedMidChunk(t *testing.T) {
	t.Parallel()

	data := make([]byte, chunkSize+catalogSize-1)
	copy(data, buildCatalog(0xA2, map[int]entry{0: {"One", 0x0F}}))
	copy(data[chunkSize:], buildCatalog(0, map[int]entry{0: {"Two", 0x0F}})[:catalogSize-1])

	fs := afero.NewMemMapFs()
	writeImage(t, fs, data)
	got, err := ReadCatalog(fs, "BEEB.MMB")
	require.NoError(t, err)
	assert.Equal(t, []Disk{{Title: "One", Number: 0}}, got)
}

func TestReadCatalog_NotMMB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{"too short", make([]byte, catalogSize-1)},
		{"empty", nil},
		{"no valid entries", buildCatalog(0, map[int]entry{0: {"X", 0xF0}, 1: {"", 0x0F}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			writeImage(t, fs, tt.data)
			_, err := ReadCatalog(fs, "BEEB.MMB")
			require.ErrorIs(t, err, ErrNotMMB)
		})
	}
}

func TestReadCatalog_Missing(t *testing.T) {
	t.Parallel()
	_, err := ReadCatalog(afero.NewMemMapFs(), "nope.mmb")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotMMB)
}

func TestReadCatalog_DiskImageThatIsNotAnMMB(t *testing.T) {
	t.Parallel()

	data := make([]byte, catalogSize)
	state := uint32(1)
	for i := range data {
		state = state*1664525 + 1013904223
		data[i] = byte(state >> 24)
	}

	fs := afero.NewMemMapFs()
	writeImage(t, fs, data)
	_, err := ReadCatalog(fs, "BEEB.MMB")

	require.ErrorIs(t, err, ErrNotMMB)
}
