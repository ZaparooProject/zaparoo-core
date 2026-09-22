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

package helpers_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSourceIdentityNamesAreDecoded covers every name a client reads back from
// a host media identity. The identity escapes each of its components, so a
// helper that reads the leaf off the raw path reports "Super%20Mario%20Bros."
// as the game's name.
func TestSourceIdentityNamesAreDecoded(t *testing.T) {
	t.Parallel()
	id := sourcepath.ID("content://com.android.externalstorage.documents/tree/primary%3ARoms")

	tests := []struct {
		name     string
		wantName string
		wantExt  string
		parts    []string
	}{
		{
			name:     "spaces and brackets",
			parts:    []string{"Super Mario Bros. (World).nes"},
			wantName: "Super Mario Bros. (World)",
			wantExt:  ".nes",
		},
		{
			name:     "nested directory keeps only the leaf",
			parts:    []string{"NES", "Mega Man 2 (USA).nes"},
			wantName: "Mega Man 2 (USA)",
			wantExt:  ".nes",
		},
		{
			name:     "a literal percent survives round-tripping",
			parts:    []string{"100% Orange Juice.zip"},
			wantName: "100% Orange Juice",
			wantExt:  ".zip",
		},
		{
			name:     "reserved URI characters are not structural",
			parts:    []string{"Game #3 & Friends? [!].nes"},
			wantName: "Game #3 & Friends? [!]",
			wantExt:  ".nes",
		},
		{
			name:     "no extension",
			parts:    []string{"Some Folder Game"},
			wantName: "Some Folder Game",
			wantExt:  "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			uri, err := sourcepath.Format(id, test.parts)
			require.NoError(t, err)
			require.Contains(t, uri, "%", "the identity under test must actually be escaped")

			leaf := test.parts[len(test.parts)-1]
			assert.Equal(t, leaf, helpers.SourceIdentityLeaf(uri))
			assert.Equal(t, test.wantName, helpers.FilenameFromPath(uri),
				"a search or random-media name comes from FilenameFromPath")

			info := helpers.GetPathInfo(uri)
			assert.Equal(t, leaf, info.Filename)
			assert.Equal(t, test.wantExt, info.Extension)
			assert.Equal(t, test.wantName, info.Name)
			assert.Equal(t, test.wantName, helpers.GetPathName(uri))

			// The identity itself must never be rewritten: sourcepath.Parse
			// only accepts the canonical escaping.
			assert.Equal(t, uri, helpers.DecodeURIIfNeeded(uri))
			_, _, err = sourcepath.Parse(helpers.DecodeURIIfNeeded(uri))
			assert.NoError(t, err)
		})
	}
}

// TestSourceIdentityLeafRejectsNonIdentities keeps the new branch from
// swallowing paths it does not own.
func TestSourceIdentityLeafRejectsNonIdentities(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"",
		"/media/fat/games/NES/Game.nes",
		"steam://123/Portal 2",
		"https://example.com/Game%20Name.zip",
		"source://not-a-valid-id/Game.nes",
		"source://" + sourcepath.ID("x"),
		"source://" + sourcepath.ID("x") + "/Game%2Fwith%2Fslashes.nes",
	} {
		assert.Empty(t, helpers.SourceIdentityLeaf(value), "value %q", value)
	}
}
