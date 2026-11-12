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

package trs80cas

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func leader(n int) []byte { return make([]byte, n) }

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func TestProgramName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		errIs error
		name  string
		data  []byte
		want  Program
	}{
		{
			name: "system",
			data: cat(leader(255), []byte{0xA5, 0x55}, []byte("GALAXY"), []byte{0x3C, 0x01}),
			want: Program{Kind: System, Name: "GALAXY"},
		},
		{
			name: "system padded",
			data: cat(leader(10), []byte{0xA5, 0x55}, []byte("PONG  "), []byte{0x3C}),
			want: Program{Kind: System, Name: "PONG"},
		},
		{
			name: "system no leader",
			data: cat([]byte{0xA5, 0x55}, []byte("AB    ")),
			want: Program{Kind: System, Name: "AB"},
		},
		{
			name: "basic",
			data: cat(leader(255), []byte{0xA5, 0xD3, 0xD3, 0xD3, 'A', 0x00}),
			want: Program{Kind: BASIC, Name: "A"},
		},
		{
			name: "non printable name",
			data: cat([]byte{0xA5, 0xD3, 0xD3, 0xD3, 0x01}),
			want: Program{Kind: BASIC, Name: "?"},
		},
		{
			name:  "empty",
			data:  nil,
			errIs: ErrUnknownFormat,
		},
		{
			name:  "only leader",
			data:  leader(5000),
			errIs: ErrUnknownFormat,
		},
		{
			name:  "leader past scan limit",
			data:  cat(leader(5000), []byte{0xA5, 0x55}, []byte("LATE  ")),
			errIs: ErrUnknownFormat,
		},
		{
			name:  "garbage before sync",
			data:  cat([]byte{0x12, 0xA5, 0x55}, []byte("NOPE  ")),
			errIs: ErrUnknownFormat,
		},
		{
			name:  "unknown marker",
			data:  cat([]byte{0xA5, 0x99, 0x00}),
			errIs: ErrUnknownFormat,
		},
		{
			name:  "truncated system",
			data:  cat([]byte{0xA5, 0x55}, []byte("AB")),
			errIs: ErrUnknownFormat,
		},
		{
			name:  "truncated basic",
			data:  []byte{0xA5, 0xD3, 0xD3},
			errIs: ErrUnknownFormat,
		},
		{
			name:  "bad basic marker",
			data:  []byte{0xA5, 0xD3, 0x00, 0xD3, 'A'},
			errIs: ErrUnknownFormat,
		},
		{
			name:  "sync at end",
			data:  []byte{0xA5},
			errIs: ErrUnknownFormat,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			p := filepath.Join("tapes", "x.cas")
			require.NoError(t, afero.WriteFile(fs, p, tt.data, 0o644))
			got, err := ProgramName(fs, p)
			if tt.errIs != nil {
				require.ErrorIs(t, err, tt.errIs)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProgramNameMissingFile(t *testing.T) {
	t.Parallel()
	_, err := ProgramName(afero.NewMemMapFs(), filepath.Join("tapes", "none.cas"))
	require.Error(t, err)
}
