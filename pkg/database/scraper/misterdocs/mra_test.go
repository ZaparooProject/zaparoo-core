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

package misterdocs

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMRASetName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		wantName string
		wantOK   bool
	}{
		{
			name: "setname element",
			content: `<misterromdescription>
	<name>Street Fighter Alpha 3</name>
	<setname>sfa3</setname>
	<rbf>cps2</rbf>
</misterromdescription>`,
			wantName: "sfa3",
			wantOK:   true,
		},
		{
			name:     "surrounding whitespace is trimmed",
			content:  "<misterromdescription><setname>\n  shocktro \n</setname></misterromdescription>",
			wantName: "shocktro",
			wantOK:   true,
		},
		{
			name:     "case is preserved for the caller to fold",
			content:  "<misterromdescription><setname>CPS1GAME</setname></misterromdescription>",
			wantName: "CPS1GAME",
			wantOK:   true,
		},
		{
			name: "legacy encoding declaration is not fatal",
			content: `<?xml version="1.0" encoding="ISO-8859-1"?>
<misterromdescription><setname>pacman</setname></misterromdescription>`,
			wantName: "pacman",
			wantOK:   true,
		},
		{
			name:     "unclosed tags before the setname are tolerated",
			content:  "<misterromdescription><rbf>cps1<setname>ffight</setname></misterromdescription>",
			wantName: "ffight",
			wantOK:   true,
		},
		{
			name:    "no setname",
			content: "<misterromdescription><name>Game</name></misterromdescription>",
		},
		{
			name:    "empty setname",
			content: "<misterromdescription><setname></setname></misterromdescription>",
		},
		{
			name:    "not xml",
			content: "this is not an mra",
		},
		{
			name:    "empty file",
			content: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			path := filepath.Join("games", "_Arcade", "Game.mra")
			require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
			require.NoError(t, afero.WriteFile(fs, path, []byte(tt.content), 0o600))

			gotName, gotOK := readMRASetName(fs, path)
			assert.Equal(t, tt.wantOK, gotOK)
			assert.Equal(t, tt.wantName, gotName)
		})
	}
}

func TestReadMRASetName_RejectsMissingDirectoryAndOversized(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join("games", "_Arcade")
	require.NoError(t, fs.MkdirAll(filepath.Join(dir, "Folder.mra"), 0o750))

	_, ok := readMRASetName(fs, filepath.Join(dir, "Missing.mra"))
	assert.False(t, ok)
	_, ok = readMRASetName(fs, filepath.Join(dir, "Folder.mra"))
	assert.False(t, ok)

	oversized := append(
		[]byte("<misterromdescription><setname>big</setname>"),
		bytes.Repeat([]byte(" "), int(maxMRABytes))...,
	)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "Big.mra"), oversized, 0o600))
	_, ok = readMRASetName(fs, filepath.Join(dir, "Big.mra"))
	assert.False(t, ok)
}
