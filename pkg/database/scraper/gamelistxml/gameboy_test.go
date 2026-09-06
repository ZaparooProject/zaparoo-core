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
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGameboySiblingGamelistPaths(t *testing.T) {
	t.Parallel()
	for _, companion := range []bool{false, true} {
		for _, allowed := range []bool{false, true} {
			name := "regular"
			if companion {
				name = "companion"
			}
			if allowed {
				name += "/same-system-root"
			} else {
				name += "/outside-system-roots"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				gbRoot := filepath.Join(root, "GAMEBOY")
				gbcRoot := filepath.Join(root, "GBC")
				rom := filepath.Join(gbRoot, "Game.gbc")
				fs := afero.NewMemMapFs()
				require.NoError(t, fs.MkdirAll(gbcRoot, 0o755))
				xml := `<gameList><game><path>../GAMEBOY/Game.gbc</path><name>Display Name</name>
<image>./covers/image.png</image><box2d>./covers/box.png</box2d></game></gameList>`
				if companion {
					xml = `<gameList><game source="ZaparooCompanion" id="123">
<name>Game</name><image>./covers/image.png</image></game>
<game source="ZaparooCompanion" parentid="123"><path>../GAMEBOY/Game.gbc</path></game></gameList>`
				}
				require.NoError(t, afero.WriteFile(fs, filepath.Join(gbcRoot, "gamelist.xml"), []byte(xml), 0o600))
				roots := []string{gbcRoot}
				if allowed {
					roots = append(roots, gbRoot)
				}
				system := scraper.ScrapeSystem{ID: systemdefs.SystemGameboyColor, ROMPaths: roots}
				s := &GamelistXMLScraper{fs: fs}
				if companion {
					_, children := s.loadCompanionEntries(context.Background(), system)
					if !allowed {
						assert.Empty(t, children)
						return
					}
					require.Len(t, children, 1)
					assert.Equal(t, rom, children[0].ResolvedPath)
					return
				}
				indexes := mediaByPath(database.Media{DBID: 2, MediaTitleDBID: 1, Path: rom})
				records, err := s.LoadRecords(context.Background(), system, indexes)
				require.NoError(t, err)
				if !allowed {
					assert.Empty(t, records)
					return
				}
				require.Len(t, records, 1)
				assert.Equal(t, int64(2), records[0].MatchedMediaDBID)
				assert.True(t, records[0].MediaLevelWriteSafe)
				mapped := s.MapToDB(records[0])
				for property, filename := range map[tags.TagValue]string{
					tags.TagPropertyImageImage: "image.png", tags.TagPropertyImageBoxart: "box.png",
				} {
					prop, ok := propertyByType(mapped.MediaProps, tags.PropertyTypeTag(property))
					require.True(t, ok)
					assert.Equal(t, filepath.ToSlash(filepath.Join(gbcRoot, "covers", filename)), prop.Text)
				}
			})
		}
	}
}

func TestGameboySiblingArtworkFallback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gbRoot, gbcRoot := filepath.Join(root, "GAMEBOY"), filepath.Join(root, "GBC")
	rom := filepath.Join(gbRoot, "Sub", "Game.gbc")
	fs := afero.NewMemMapFs()
	art := filepath.Join(gbcRoot, "media", "box2d", "Sub", "Game.png")
	require.NoError(t, fs.MkdirAll(filepath.Dir(art), 0o755))
	require.NoError(t, afero.WriteFile(fs, art, nil, 0o600))
	xml := `<gameList><game><path>../GAMEBOY/Sub/Game.gbc</path><name>Game</name>
<image>../GAMEBOY/outside.png</image></game></gameList>`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(gbcRoot, "gamelist.xml"), []byte(xml), 0o600))
	s := &GamelistXMLScraper{fs: fs}
	records, err := s.LoadRecords(context.Background(), scraper.ScrapeSystem{
		ID: systemdefs.SystemGameboyColor, ROMPaths: []string{gbRoot, gbcRoot},
	}, mediaByPath(database.Media{DBID: 2, MediaTitleDBID: 1, Path: rom}))
	require.NoError(t, err)
	require.Len(t, records, 1)
	mapped := s.MapToDB(records[0])
	box, ok := propertyByType(mapped.MediaProps, tags.PropertyTypeTag(tags.TagPropertyImageBoxart))
	require.True(t, ok)
	assert.Equal(t, filepath.ToSlash(art), box.Text)
	_, imageOK := propertyByType(mapped.MediaProps, tags.PropertyTypeTag(tags.TagPropertyImageImage))
	assert.False(t, imageOK, "ROM root expansion must not relax the asset path boundary")
}

func TestResolveGamelistROMPathBoundaries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gbRoot, gbcRoot := filepath.Join(root, "GAMEBOY"), filepath.Join(root, "GBC")
	for _, path := range []string{
		filepath.Join("..", "..", "outside.gbc"),
		filepath.Join("..", "SNES", "Game.gbc"),
		filepath.Join("..", "GAMEBOY-other", "Game.gbc"),
		filepath.Join(root, "outside.gbc"),
	} {
		resolved, matchedRoot := resolveGamelistROMPath(path, gbcRoot, []string{gbRoot, gbcRoot})
		assert.Empty(t, resolved, path)
		assert.Empty(t, matchedRoot, path)
	}
}

func scrapeGameboyFixture(
	t *testing.T, s *GamelistXMLScraper, db database.MediaDBI, systems []scraper.ScrapeSystem, force bool,
) {
	t.Helper()
	ch := make(chan scraper.ScrapeUpdate, 128)
	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{
		Force: force, Pauser: syncutil.NewPauser(),
	}, systems, db, ch)
	var done bool
	for update := range ch {
		require.NoError(t, update.FatalErr)
		require.NoError(t, update.Err)
		done = done || update.Done
	}
	require.True(t, done)
}

func TestGameboyScrapeLayouts(t *testing.T) {
	t.Parallel()
	for _, layout := range []string{"shared", "separate", "sibling-reference"} {
		for _, companion := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/companion=%t", layout, companion), func(t *testing.T) {
				t.Parallel()
				db, cleanup := helpers.NewInMemoryMediaDB(t)
				t.Cleanup(cleanup)
				fs := afero.NewMemMapFs()
				root := t.TempDir()
				gbRoot, gbcRoot := filepath.Join(root, "GAMEBOY"), filepath.Join(root, "GBC")
				gbcROMRoot, gbcXMLRoot := gbcRoot, gbcRoot
				gbcXMLPath := "./Game.gbc"
				if layout != "separate" {
					gbcROMRoot = gbRoot
				}
				if layout == "shared" {
					gbcXMLRoot = gbRoot
				}
				if layout == "sibling-reference" {
					gbcXMLPath = "../GAMEBOY/Game.gbc"
				}
				gbPath, gbcPath := filepath.Join(gbRoot, "Game.gb"), filepath.Join(gbcROMRoot, "Game.gbc")
				scantest.IndexMediaPaths(t, db, systemdefs.SystemGameboy, gbPath)
				scantest.IndexMediaPaths(t, db, systemdefs.SystemGameboyColor, gbcPath)
				entries := func(path, id, prefix string) string {
					if companion {
						return fmt.Sprintf(`<game source="ZaparooCompanion" id="%s">
<name>Game</name><desc>%s description</desc><image>./covers/%s-image.png</image>
<box2d>./covers/%s-box.png</box2d></game>
<game source="ZaparooCompanion" parentid="%s"><path>%s</path></game>`, id, prefix, prefix, prefix, id, path)
					}
					return fmt.Sprintf(`<game><path>%s</path><name>Game</name><desc>%s description</desc>
<image>./covers/%s-image.png</image><box2d>./covers/%s-box.png</box2d></game>`, path, prefix, prefix, prefix)
				}
				gbXML := entries("./Game.gb", "1", "gb")
				gbcXML := entries(gbcXMLPath, "2", "gbc")
				files := map[string]string{gbRoot: gbXML, gbcXMLRoot: gbcXML}
				if layout == "shared" {
					files[gbRoot] = gbXML + gbcXML
				}
				for dir, contents := range files {
					require.NoError(t, fs.MkdirAll(dir, 0o755))
					require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "gamelist.xml"),
						[]byte("<gameList>"+contents+"</gameList>"), 0o600))
				}
				gb, err := db.FindSystemBySystemID(systemdefs.SystemGameboy)
				require.NoError(t, err)
				gbc, err := db.FindSystemBySystemID(systemdefs.SystemGameboyColor)
				require.NoError(t, err)
				systems := []scraper.ScrapeSystem{
					{ID: gb.SystemID, DBID: gb.DBID, ROMPaths: []string{gbRoot}},
					{ID: gbc.SystemID, DBID: gbc.DBID, ROMPaths: []string{gbRoot, gbcRoot}},
				}
				s := &GamelistXMLScraper{fs: fs, db: db}
				for _, force := range []bool{false, false, true} {
					scrapeGameboyFixture(t, s, db, systems, force)
					for _, tc := range []struct{ system, prefix, assetRoot string }{
						{gb.SystemID, "gb", gbRoot}, {gbc.SystemID, "gbc", gbcXMLRoot},
					} {
						rows, rowsErr := db.GetMediaBySystemID(tc.system)
						require.NoError(t, rowsErr)
						require.Len(t, rows, 1)
						props, propsErr := db.GetMediaProperties(context.Background(), rows[0].DBID)
						require.NoError(t, propsErr)
						titleProps, propsErr := db.GetMediaTitleProperties(context.Background(), rows[0].MediaTitleDBID)
						require.NoError(t, propsErr)
						if companion {
							// Companion parent artwork is shared at title scope.
							assert.Empty(t, props)
							props = titleProps
						}
						for property, suffix := range map[tags.TagValue]string{
							tags.TagPropertyImageImage: "-image.png", tags.TagPropertyImageBoxart: "-box.png",
						} {
							prop, ok := propertyByType(props, tags.PropertyTypeTag(property))
							require.True(t, ok, "%s property %s, force=%t", tc.system, property, force)
							want := filepath.ToSlash(filepath.Join(tc.assetRoot, "covers", tc.prefix+suffix))
							assert.Equal(t, want, prop.Text)
						}
						desc, ok := propertyByType(titleProps, tags.PropertyTypeTag(tags.TagPropertyDescription))
						require.True(t, ok)
						assert.Equal(t, tc.prefix+" description", desc.Text)
					}
				}
			})
		}
	}
}

func TestGameboyRenameReindexForceScrape(t *testing.T) {
	t.Parallel()
	for _, changeTitle := range []bool{false, true} {
		t.Run(fmt.Sprintf("change-title=%t", changeTitle), func(t *testing.T) {
			t.Parallel()
			db, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			fs := afero.NewMemMapFs()
			root := filepath.Join(t.TempDir(), "GAMEBOY")
			require.NoError(t, fs.MkdirAll(root, 0o755))
			oldName, newName := "Game (USA).gbc", "Game (Europe).gbc"
			if changeTitle {
				newName = "Renamed Game.gbc"
			}
			oldPath, newPath := filepath.Join(root, oldName), filepath.Join(root, newName)
			require.NoError(t, afero.WriteFile(fs, oldPath, nil, 0o600))
			scantest.IndexMediaPaths(t, db, systemdefs.SystemGameboyColor, oldPath)
			sys, err := db.FindSystemBySystemID(systemdefs.SystemGameboyColor)
			require.NoError(t, err)
			systems := []scraper.ScrapeSystem{{ID: sys.SystemID, DBID: sys.DBID, ROMPaths: []string{root}}}
			writeXML := func(name, image string) {
				xml := fmt.Sprintf(`<gameList><game><path>./%s</path><name>Game</name>
<image>./covers/%s.png</image></game></gameList>`, name, image)
				require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "gamelist.xml"), []byte(xml), 0o600))
			}
			writeXML(oldName, "original")
			s := &GamelistXMLScraper{fs: fs, db: db}
			scrapeGameboyFixture(t, s, db, systems, false)
			require.NoError(t, fs.Rename(oldPath, newPath))
			scantest.IndexMediaPaths(t, db, sys.SystemID, newPath)
			xmlName := oldName
			if changeTitle {
				xmlName = newName
			}
			// A same-title rename can recover through the old XML slug. If the
			// title changes, the refreshed path must win over the missing title.
			writeXML(xmlName, "refreshed")
			scrapeGameboyFixture(t, s, db, systems, true)
			rows, err := db.GetMediaBySystemID(sys.SystemID)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			for _, row := range rows {
				props, propsErr := db.GetMediaProperties(context.Background(), row.DBID)
				require.NoError(t, propsErr)
				image, ok := propertyByType(props, tags.PropertyTypeTag(tags.TagPropertyImageImage))
				require.True(t, ok, "artwork missing for %s", row.Path)
				want := "refreshed.png"
				if row.Path == filepath.ToSlash(oldPath) {
					assert.True(t, row.IsMissing)
					want = "original.png"
				} else {
					assert.Equal(t, filepath.ToSlash(newPath), row.Path)
					assert.False(t, row.IsMissing)
				}
				assert.Equal(t, filepath.ToSlash(filepath.Join(root, "covers", want)), image.Text)
			}
		})
	}
}

func TestGameboySharedFolderDoesNotMatchOtherSystemBySlug(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ system, extension, otherExtension string }{
		{systemdefs.SystemGameboy, ".gb", ".gbc"},
		{systemdefs.SystemGameboyColor, ".gbc", ".gb"},
	} {
		t.Run(tc.system, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), "GAMEBOY")
			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll(root, 0o755))
			// An XML entry for the other system must not claim a same-named title,
			// including when that system's own entry is absent from this gamelist.
			xml := `<gameList><game><path>./Game` + tc.otherExtension + `</path>
<name>Game</name><image>./wrong.png</image></game></gameList>`
			require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "gamelist.xml"), []byte(xml), 0o600))
			indexes := mediaBySlugAndPath("game", &database.MediaTitle{DBID: 1, Slug: "game"},
				database.Media{DBID: 2, MediaTitleDBID: 1, Path: filepath.Join(root, "Game"+tc.extension)})
			records, err := (&GamelistXMLScraper{fs: fs}).LoadRecords(context.Background(),
				scraper.ScrapeSystem{ID: tc.system, ROMPaths: []string{root}}, indexes)
			require.NoError(t, err)
			assert.Empty(t, records, "another system's entry must not write metadata or a scrape sentinel")

			// Rejecting an incompatible entry must not consume the title: a
			// subsequent valid slug-only entry can still enrich its moved ROM.
			xml = `<gameList><game><path>./old/Game` + tc.extension + `</path><name>Game</name></game></gameList>`
			require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "gamelist.xml"), []byte(xml), 0o600))
			records, err = (&GamelistXMLScraper{fs: fs}).LoadRecords(context.Background(),
				scraper.ScrapeSystem{ID: tc.system, ROMPaths: []string{root}}, indexes)
			require.NoError(t, err)
			require.Len(t, records, 1)
			assert.Equal(t, gamelistMatchSlugOnly, records[0].MatchKind)
			assert.Equal(t, int64(2), records[0].MatchedMediaDBID)
		})
	}
}
