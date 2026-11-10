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
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gamelistDirsScraper(fs afero.Fs, dirs ...string) *GamelistXMLScraper {
	g := &GamelistXMLScraper{fs: fs}
	WithGamelistDirs(dirs...)(g)
	return g
}

func TestLoadRecords_FallsBackToGamelistDirs(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	roms := filepath.Join(base, "RetroPie", "roms", "snes")
	esDir := filepath.Join(base, ".emulationstation")
	gamelists := filepath.Join(esDir, "gamelists")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(roms, 0o755))
	xml := `<gameList><game><path>./Game.sfc</path><name>Home List</name>` +
		`<image>` + filepath.Join(esDir, "downloaded_media", "snes", "Game.png") + `</image></game></gameList>`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(gamelists, "snes", "gamelist.xml"), []byte(xml), 0o600))

	newIndexes := func() loadRecordIndexes {
		return mediaBySlugAndPath("game", &database.MediaTitle{DBID: 1, Slug: "game"},
			database.Media{DBID: 2, MediaTitleDBID: 1, Path: filepath.Join(roms, "Game.sfc")})
	}
	system := scraper.ScrapeSystem{ID: "SNES", ROMPaths: []string{roms}, Extensions: []string{".sfc"}}

	records, err := (&GamelistXMLScraper{fs: fs}).LoadRecords(context.Background(), system, newIndexes())
	require.NoError(t, err)
	assert.Empty(t, records, "without the option only the ROM folder is read")

	g := gamelistDirsScraper(fs, gamelists, filepath.Join(base, "etc", "gamelists"))
	records, err = g.LoadRecords(context.Background(), system, newIndexes())
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "Home List", records[0].Game.Name)
	assert.Equal(t, roms, records[0].SystemRootPath, "relative paths stay bound to the ROM folder")
	assert.Equal(t,
		filepath.Join(esDir, "downloaded_media", "snes", "Game.png"),
		resolveESAssetPath(records[0].Game.Image, roms, g.externalAssetRoots),
		"downloaded media beside the gamelists folder is allowed")
	assert.Empty(t, resolveESAssetPath(filepath.Join(base, "elsewhere.png"), roms, g.externalAssetRoots))

	// ES prefers the ROM folder's own list.
	require.NoError(t, afero.WriteFile(fs, filepath.Join(roms, "gamelist.xml"),
		[]byte(`<gameList><game><path>./Game.sfc</path><name>ROM List</name></game></gameList>`), 0o600))
	records, err = g.LoadRecords(context.Background(), system, newIndexes())
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "ROM List", records[0].Game.Name)
}
