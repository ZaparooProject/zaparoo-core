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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestScrapeScopedGamelist(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "force", "resume", "scraped", "uri"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.ToSlash(filepath.Join(root, "foo", "Game.nes"))
			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll(root, 0o750))
			xmlData := `<gameList>
<game><path>./foobar/Game.nes</path><name>Game</name><desc>outside</desc></game>
<game><path>./foo/Game.nes</path><name>Game</name><desc>selected</desc></game>
<game><path>./foo/Game.nes</path><name>Game</name><desc>duplicate</desc></game>
<folder><path>./foo</path><name>Game</name><desc>ambiguous folder</desc></folder>
</gameList>`
			if mode == "uri" {
				path = "steam://123"
				xmlData = `<gameList>
<game id="42" source="ZaparooCompanion"><name>Game</name><desc>selected</desc></game>
<game parentid="42" source="ZaparooCompanion"><path>./game.slug</path></game>
</gameList>`
			}
			require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "gamelist.xml"), []byte(xmlData), 0o600))
			mdb := helpers.NewMockMediaDBI()
			scope := &database.ScrapeScope{SystemID: "NES", Path: path, MediaID: 1}
			opts := scraper.ScrapeOptions{Scope: scope, Force: mode == "force" || mode == "resume"}
			runID := ""
			if opts.Force {
				runID, opts.RunID = "run", "run"
			}
			mdb.On("GetScrapeMedia", mock.Anything, *scope).Return([]database.MediaFullRow{{
				Media:  database.Media{DBID: 1, MediaTitleDBID: 10, Path: path},
				Title:  database.MediaTitle{DBID: 10, SystemDBID: 100, Name: "Game", Slug: "game"},
				System: database.System{DBID: 100, SystemID: "NES"},
			}}, nil).Once()
			completed := make(map[int64]struct{})
			alreadyDone := mode == "resume" || mode == "scraped"
			if alreadyDone {
				completed[1] = struct{}{}
			}
			mdb.On("GetScopedScrapeMediaIDs", mock.Anything, *scope, "gamelist.xml", runID).
				Return(completed, nil).Once()
			if !alreadyDone {
				if mode != "uri" {
					mdb.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "NES", mock.Anything).
						Return(nil, nil).Once()
				}
				matchesWrite := mock.MatchedBy(func(w *database.ScrapeWrite) bool {
					for _, prop := range w.TitleProps {
						if prop.TypeTag == tags.PropertyTypeTag(tags.TagPropertyDescription) &&
							prop.Text == "selected" {
							return true
						}
					}
					return false
				})
				mdb.On("ApplyScrapeResult", mock.Anything, int64(1), int64(10), matchesWrite).Return(nil).Once()
			}
			g := &GamelistXMLScraper{db: mdb, fs: fs}
			ch := make(chan scraper.ScrapeUpdate, 32)
			systems := []scraper.ScrapeSystem{{ID: "NES", DBID: 100, ROMPaths: []string{root}}}
			g.scrapeLoop(context.Background(), opts, systems, mdb, ch)
			var final scraper.ScrapeUpdate
			for update := range ch {
				require.NoError(t, update.FatalErr)
				final = update
			}
			require.True(t, final.Done)
			require.Equal(t, 1, final.Total)
			require.Equal(t, 1, final.Processed)
			if alreadyDone {
				require.Equal(t, 1, final.Skipped)
			} else {
				require.Equal(t, 1, final.Matched)
			}
			mdb.AssertExpectations(t)
		})
	}
}
