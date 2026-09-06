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
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestScopedLocalMedia(t *testing.T) {
	t.Parallel()
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "force"}[force], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.ToSlash(filepath.Join(root, "Game.nes"))
			fs := afero.NewMemMapFs()
			art := filepath.Join(root, "media", "boxart", "Game.png")
			require.NoError(t, fs.MkdirAll(filepath.Dir(art), 0o750))
			require.NoError(t, afero.WriteFile(fs, art, []byte("image"), 0o600))
			mdb := helpers.NewMockMediaDBI()
			scope := &database.ScrapeScope{SystemID: "NES", Path: path, MediaID: 1}
			opts := scraper.ScrapeOptions{Scope: scope, Force: force}
			mdb.On("GetScrapeMedia", mock.Anything, *scope).Return([]database.MediaFullRow{{
				Media:  database.Media{DBID: 1, MediaTitleDBID: 10, Path: path},
				Title:  database.MediaTitle{DBID: 10, Name: "Game", Slug: "game"},
				System: database.System{DBID: 100, SystemID: "NES"},
			}}, nil).Once()
			if force {
				mdb.On("GetMediaPropertyMetadata", mock.Anything, int64(1)).
					Return([]database.MediaProperty{}, nil).Once()
			} else {
				mdb.On("GetScopedScrapeMediaIDs", mock.Anything, *scope, scraperID, "").
					Return(map[int64]struct{}{}, nil).Once()
			}
			mdb.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "NES", mock.Anything).
				Return(nil, nil).Once()
			mdb.On("ApplyScrapeResult", mock.Anything, int64(1), int64(10), mock.Anything).Return(nil).Once()
			s := &scraperImpl{db: mdb, fs: fs}
			ch := make(chan scraper.ScrapeUpdate, 16)
			s.scrapeLoop(context.Background(), opts, []scraper.ScrapeSystem{{ID: "NES", ROMPaths: []string{root}}}, ch)
			var final scraper.ScrapeUpdate
			for update := range ch {
				require.NoError(t, update.FatalErr)
				final = update
			}
			require.True(t, final.Done)
			require.Equal(t, 1, final.Total)
			require.Equal(t, 1, final.Matched)
			mdb.AssertExpectations(t)
		})
	}
}
