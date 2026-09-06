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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSharedFolderSiblingSystemsBySlug covers the MiSTer folders shared by
// systems that use different ROM formats. Game Boy is the reported case, but
// SMS, WonderSwan, NES and TGFX16 have the same shape, so the guard is driven by
// each system's launcher extensions rather than a Game Boy special case.
func TestSharedFolderSiblingSystemsBySlug(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ folder, system, extension, sibling string }{
		{"GAMEBOY", systemdefs.SystemGameboy, ".gb", ".gbc"},
		{"GAMEBOY", systemdefs.SystemGameboyColor, ".gbc", ".gb"},
		{"SMS", systemdefs.SystemMasterSystem, ".sms", ".gg"},
		{"SMS", systemdefs.SystemGameGear, ".gg", ".sg"},
		{"WonderSwan", systemdefs.SystemWonderSwan, ".ws", ".wsc"},
		{"NES", systemdefs.SystemNES, ".nes", ".fds"},
		{"TGFX16", systemdefs.SystemSuperGrafx, ".sgx", ".pce"},
	} {
		t.Run(tc.folder+"/"+tc.system, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), tc.folder)
			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll(root, 0o755))
			system := scraper.ScrapeSystem{
				ID: tc.system, ROMPaths: []string{root},
				Extensions: misterExtensions(t, tc.system),
			}
			indexes := mediaBySlugAndPath("game", &database.MediaTitle{DBID: 1, Slug: "game"},
				database.Media{DBID: 2, MediaTitleDBID: 1, Path: filepath.Join(root, "Game"+tc.extension)})

			xml := `<gameList><game><path>./Game` + tc.sibling + `</path>
<name>Game</name><image>./wrong.png</image></game></gameList>`
			require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "gamelist.xml"), []byte(xml), 0o600))
			records, err := (&GamelistXMLScraper{fs: fs}).LoadRecords(context.Background(), system, indexes)
			require.NoError(t, err)
			assert.Empty(t, records, "a sibling system's entry must not claim this system's title")

			// The rejection must not consume the title: this system's own entry
			// still recovers its moved ROM through the slug.
			xml = `<gameList><game><path>./old/Game` + tc.extension + `</path><name>Game</name></game></gameList>`
			require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "gamelist.xml"), []byte(xml), 0o600))
			records, err = (&GamelistXMLScraper{fs: fs}).LoadRecords(context.Background(), system, indexes)
			require.NoError(t, err)
			require.Len(t, records, 1)
			assert.Equal(t, gamelistMatchSlugOnly, records[0].MatchKind)
			assert.Equal(t, int64(2), records[0].MatchedMediaDBID)
		})
	}
}

// TestSystemIndexesExtensionUnknownSet keeps the guard fail-open. A system whose
// launchers accept files through a Test function has no exact extension list, so
// filtering on a partial list would drop entries the scanner did index.
func TestSystemIndexesExtensionUnknownSet(t *testing.T) {
	t.Parallel()
	assert.True(t, systemIndexesExtension(nil, "/roms/GAMEBOY/Game.gbc"))
	assert.True(t, systemIndexesExtension([]string{".gb"}, "/roms/GAMEBOY/DiscFolder"))
	assert.True(t, systemIndexesExtension([]string{".gb", ".mgl"}, "/roms/GAMEBOY/Game.GB"))
	assert.False(t, systemIndexesExtension([]string{".gb", ".mgl"}, "/roms/GAMEBOY/Game.gbc"))
}

func TestIndexedExtensionsBySystem(t *testing.T) {
	t.Parallel()
	launchers := []platforms.Launcher{
		{ID: "Gameboy", SystemID: systemdefs.SystemGameboy, Extensions: []string{".GB", ".mgl"}},
		{ID: "RAGameboy", SystemID: systemdefs.SystemGameboy},
		{ID: "GameboyColor", SystemID: systemdefs.SystemGameboyColor, Extensions: []string{".gbc"}},
		{ID: "Custom", SystemID: systemdefs.SystemGameboyColor, Extensions: []string{".mgl"}},
		{
			ID: "Arcade", SystemID: systemdefs.SystemArcade, Extensions: []string{".mra"},
			Test: func(*config.Instance, string) bool { return true },
		},
		{ID: "NoSystem", Extensions: []string{".zip"}},
	}
	got := indexedExtensionsBySystem(launchers)
	assert.Equal(t, []string{".gb", ".mgl"}, got[systemdefs.SystemGameboy])
	assert.Equal(t, []string{".gbc", ".mgl"}, got[systemdefs.SystemGameboyColor])
	assert.NotContains(t, got, systemdefs.SystemArcade, "a Test function makes the set unknown")
	assert.NotContains(t, got, "")
}

// TestSlugWithoutMediaRowsIsReleased covers the title with no media rows at all:
// the slug is dropped so a later entry naming the same title can still match by
// path instead of being shadowed.
func TestSlugWithoutMediaRowsIsReleased(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "GAMEBOY")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(root, 0o755))
	xml := `<gameList><game><path>./Missing.gb</path><name>Game</name></game>
<game><path>./Other.gb</path><name>Game</name><image>./art.png</image></game></gameList>`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "gamelist.xml"), []byte(xml), 0o600))

	// The slug resolves to title 1, which owns no media rows. The only indexed
	// row belongs to a different title.
	indexes := mediaBySlugAndPath("game", &database.MediaTitle{DBID: 1, Slug: "game"},
		database.Media{DBID: 5, MediaTitleDBID: 9, Path: filepath.Join(root, "Other.gb")})

	records, err := (&GamelistXMLScraper{fs: fs}).LoadRecords(context.Background(), scraper.ScrapeSystem{
		ID: systemdefs.SystemGameboy, ROMPaths: []string{root},
		Extensions: misterExtensions(t, systemdefs.SystemGameboy),
	}, indexes)
	require.NoError(t, err)
	assert.NotContains(t, indexes.TitlesBySlug, "game", "a title with no media rows must release its slug")
	require.Len(t, records, 1)
	assert.Equal(t, gamelistMatchPathOnly, records[0].MatchKind)
	assert.Equal(t, int64(5), records[0].MatchedMediaDBID)
}
