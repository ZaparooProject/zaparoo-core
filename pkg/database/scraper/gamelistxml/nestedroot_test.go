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

package gamelistxml

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveGamelistROMPathNestedRoots covers configured roots that nest. The
// ROM belongs to the most specific root, so artwork fallback names stay relative
// to the media/ directory beside it instead of gaining the nested prefix and
// matching a same-named file in the outer root.
func TestResolveGamelistROMPathNestedRoots(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	outer := filepath.Join(base, "GAMEBOY")
	inner := filepath.Join(outer, "Subset")
	gbcRoot := filepath.Join(base, "GBC")

	for _, order := range [][]string{
		{gbcRoot, outer, inner},
		{gbcRoot, inner, outer},
	} {
		resolved, matchedRoot := resolveGamelistROMPath("../GAMEBOY/Subset/Game.gbc", gbcRoot, order)
		assert.Equal(t, filepath.Join(inner, "Game.gbc"), resolved)
		assert.Equal(t, inner, matchedRoot, "most specific root must win, order %v", order)
	}

	resolved, matchedRoot := resolveGamelistROMPath("", gbcRoot, []string{outer})
	assert.Empty(t, resolved)
	assert.Empty(t, matchedRoot)
}

func TestNestedRootArtworkFallback(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	outer := filepath.Join(base, "GAMEBOY")
	inner := filepath.Join(outer, "Subset")
	gbcRoot := filepath.Join(base, "GBC")
	rom := filepath.Join(inner, "Game.gbc")

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(gbcRoot, 0o755))
	wanted := filepath.Join(inner, "media", "box2d", "Game.png")
	require.NoError(t, fs.MkdirAll(filepath.Dir(wanted), 0o755))
	require.NoError(t, afero.WriteFile(fs, wanted, nil, 0o600))
	// Treating the outer root as the ROM's root names the artwork
	// Subset/Game.png, which matches a different game's cover here.
	decoy := filepath.Join(outer, "media", "box2d", "Subset", "Game.png")
	require.NoError(t, fs.MkdirAll(filepath.Dir(decoy), 0o755))
	require.NoError(t, afero.WriteFile(fs, decoy, nil, 0o600))

	xml := `<gameList><game><path>../GAMEBOY/Subset/Game.gbc</path><name>Game</name></game></gameList>`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(gbcRoot, "gamelist.xml"), []byte(xml), 0o600))

	s := &GamelistXMLScraper{fs: fs}
	records, err := s.LoadRecords(context.Background(), scraper.ScrapeSystem{
		ID: systemdefs.SystemGameboyColor, ROMPaths: []string{gbcRoot, outer, inner},
		Extensions: misterExtensions(t, systemdefs.SystemGameboyColor),
	}, mediaByPath(database.Media{DBID: 2, MediaTitleDBID: 1, Path: rom}))
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, inner, records[0].ROMRootPath)

	mapped := s.MapToDB(records[0])
	box, ok := propertyByType(mapped.MediaProps, tags.PropertyTypeTag(tags.TagPropertyImageBoxart))
	require.True(t, ok)
	assert.Equal(t, filepath.ToSlash(wanted), box.Text)
}
