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

package localmedia

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/stretchr/testify/require"
)

func TestMediaArtworkNamesUsesIndexedSourceKind(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "games")
	mediaPath := "test://one/Virtual"

	for _, tc := range []struct {
		name       string
		kind       string
		sourcePath string
		unique     bool
		missing    bool
		wantNames  bool
	}{
		{name: "directory", kind: "directory", sourcePath: filepath.Join(root, "Game"), unique: true, wantNames: true},
		{name: "file", kind: "file", sourcePath: filepath.Join(root, "Game.rom"), unique: true, wantNames: true},
		{name: "ambiguous", kind: "directory", sourcePath: filepath.Join(root, "Shared"), unique: false},
		{
			name: "missing media", kind: "directory", sourcePath: filepath.Join(root, "Missing"),
			unique: true, missing: true,
		},
		{name: "unknown kind", kind: "unknown", sourcePath: filepath.Join(root, "Game"), unique: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := database.MediaSource{
				MediaDBID: 1, MediaPath: mediaPath, SourcePath: tc.sourcePath,
				SourceKey: helpers.NormalizePathForComparison(tc.sourcePath), SourceRoot: root,
				SourceKind: tc.kind, Unique: tc.unique,
			}
			lookupRoots, names, cleanupNames := mediaArtworkNames(
				&database.MediaWithFullPath{Path: mediaPath, IsMissing: tc.missing}, nil, nil,
				scraper.NewSourceIndex([]database.MediaSource{source}), true,
			)
			if !tc.wantNames {
				require.Empty(t, lookupRoots)
				require.Empty(t, names)
				require.Empty(t, cleanupNames)
				return
			}
			require.Equal(t, []string{root}, lookupRoots)
			require.NotEmpty(t, names)
			require.Equal(t, names, cleanupNames)
		})
	}
}
