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
package pinup

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func FuzzParseTablePath(f *testing.F) {
	for _, seed := range []string{
		"popper://1/Table", "popper://0/Table", "popper://2147483648/Table", "", "steam://1/Table",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		id, err := ParseTablePath(value)
		if err != nil {
			return
		}
		require.Positive(t, id)
		require.LessOrEqual(t, id, maxGameID)
		parsed, err := ParseTablePath(TablePath(id, "renamed table"))
		require.NoError(t, err)
		require.Equal(t, id, parsed)
	})
}

func TestTablePathRoundTrip(t *testing.T) {
	t.Parallel()

	path := TablePath(42, "Attack from Mars (Bally 1995)")
	assert.True(t, strings.HasPrefix(path, "popper://42/"), path)
	assert.Contains(t, path, "Attack%20from%20Mars")

	gameID, err := ParseTablePath(path)
	require.NoError(t, err)
	assert.Equal(t, 42, gameID)
}

func TestParseTablePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		want    int
		wantErr bool
	}{
		{name: "id and name", path: "popper://7/Medieval%20Madness", want: 7},
		{name: "id only", path: "popper://7", want: 7},
		{name: "id with trailing slash", path: "popper://7/", want: 7},
		{name: "uppercase scheme", path: "POPPER://7/x", want: 7},
		{name: "wrong scheme", path: "steam://7/x", wantErr: true},
		{name: "non numeric", path: "popper://abc/x", wantErr: true},
		{name: "zero", path: "popper://0/x", wantErr: true},
		{name: "negative", path: "popper://-5/x", wantErr: true},
		{name: "too large", path: "popper://99999999999/x", wantErr: true},
		{name: "empty id", path: "popper:///x", wantErr: true},
		{name: "not a virtual path", path: "C:/tables/afm.vpx", wantErr: true},
		{name: "empty", path: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseTablePath(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLibraryTable(t *testing.T) {
	t.Parallel()

	lib := Library{
		Emulators: map[int]Emulator{1: {ID: 1, Name: "Visual Pinball X"}},
		Tables: []Table{
			{ID: 10, EmulatorID: 1, Name: "afm"},
			{ID: 11, EmulatorID: 9, Name: "orphan"},
		},
	}

	table, emu, ok := lib.Table(10)
	require.True(t, ok)
	assert.Equal(t, "afm", table.Name)
	assert.Equal(t, 1, emu.ID)

	_, _, ok = lib.Table(11)
	assert.False(t, ok, "a table whose emulator is not in the library is not launchable")

	_, _, ok = lib.Table(99)
	assert.False(t, ok)
}

func TestTableDisplayName(t *testing.T) {
	t.Parallel()

	withDisplay := Table{Name: "afm", Display: "Attack from Mars"}
	assert.Equal(t, "Attack from Mars", withDisplay.DisplayName())
	stemOnly := Table{Name: "afm"}
	assert.Equal(t, "afm", stemOnly.DisplayName())
}
