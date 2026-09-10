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

package methods

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestResolveScrapeScope(t *testing.T) {
	t.Parallel()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "game.nes"))
	for _, kind := range []string{"id", "file", "subtree", "uri"} {
		t.Run(kind, func(t *testing.T) {
			mdb := helpers.NewMockMediaDBI()
			env := requests.RequestEnv{Context: context.Background(), Database: &database.Database{MediaDB: mdb}}
			id := int64(42)
			mediaPath := path
			if kind == "uri" {
				mediaPath = "steam://123"
			}
			row := database.MediaFullRow{
				Media: database.Media{DBID: id, Path: mediaPath}, System: database.System{DBID: 1, SystemID: "NES"},
			}
			input := &models.MediaScrapeScope{MediaID: &id}
			if kind == "file" || kind == "subtree" || kind == "uri" {
				input.MediaID = nil
				selector := &models.MediaScrapePath{System: "nes", Path: mediaPath}
				mdb.On("FindSystemBySystemID", "NES").Return(row.System, nil).Once()
				if kind == "subtree" {
					input.Subtree = selector
				} else {
					input.File = selector
					mdb.On("FindMediaBySystemAndPath", mock.Anything, int64(1), mediaPath).
						Return(&row.Media, nil).Once()
				}
			}
			if kind != "subtree" {
				mdb.On("GetMediaWithTitleAndSystem", mock.Anything, id).Return(&row, nil).Once()
			}
			scope, err := resolveScrapeScope(&env, models.MediaScrapeParams{Scope: input})
			require.NoError(t, err)
			require.Equal(t, "NES", scope.SystemID)
			require.Equal(t, mediaPath, scope.Path)
			require.Equal(t, kind == "subtree", scope.Subtree)
			if !scope.Subtree {
				require.Equal(t, id, scope.MediaID)
			}
			mdb.AssertExpectations(t)
		})
	}
}

func TestResolveScrapeScopeRejectsInvalidForms(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"scope":{}}`,
		`{"scope":{"mediaId":0}}`,
		`{"scope":{"mediaId":-1}}`,
		`{"scope":{"mediaId":1,"file":{"system":"NES","path":"/game"}}}`,
		`{"scope":{"mediaId":1},"systems":[]}`,
		`{"scope":{"file":{"system":"NES","path":"relative.nes"}}}`,
		`{"scope":{"file":{"system":"NES","path":"/games/../game.nes"}}}`,
		`{"scope":{"subtree":{"system":"NES","path":"steam://"}}}`,
		`{"scope":{"subtree":{"system":"NES","path":""}}}`,
		`{"scope":{"subtree":{"system":"unknown-system","path":"/"}}}`,
		`{"scope":{"subtree":{"system":"NES","path":"/games\u0000"}}}`,
	} {
		t.Run(input, func(t *testing.T) {
			var params models.MediaScrapeParams
			require.NoError(t, json.Unmarshal([]byte(input), &params))
			_, err := resolveScrapeScope(&requests.RequestEnv{}, params)
			require.Error(t, err)
		})
	}
	for _, input := range []string{
		`{"scope":{"directory":"/games"}}`,
		`{"scope":{"file":{"system":"NES","path":"/games","recursive":true}}}`,
	} {
		var params models.MediaScrapeParams
		require.Error(t, json.Unmarshal([]byte(input), &params))
	}
}

func TestResolveScrapeScopeMissingID(t *testing.T) {
	t.Parallel()
	mdb := helpers.NewMockMediaDBI()
	mdb.On("GetMediaWithTitleAndSystem", mock.Anything, int64(42)).Return(nil, nil).Once()
	env := requests.RequestEnv{Context: context.Background(), Database: &database.Database{MediaDB: mdb}}
	id := int64(42)
	_, err := resolveScrapeScope(&env, models.MediaScrapeParams{Scope: &models.MediaScrapeScope{MediaID: &id}})
	require.ErrorContains(t, err, "not found")
	mdb.AssertExpectations(t)
}

func TestResolveScrapeScopeMissingFile(t *testing.T) {
	t.Parallel()
	mdb := helpers.NewMockMediaDBI()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "absent.nes"))
	mdb.On("FindSystemBySystemID", "NES").Return(database.System{DBID: 1, SystemID: "NES"}, nil).Once()
	mdb.On("FindMediaBySystemAndPath", mock.Anything, int64(1), path).Return(nil, nil).Once()
	if filepath.FromSlash(path) != path {
		mdb.On("FindMediaBySystemAndPath", mock.Anything, int64(1), filepath.FromSlash(path)).Return(nil, nil).Once()
	}
	env := requests.RequestEnv{Context: context.Background(), Database: &database.Database{MediaDB: mdb}}
	_, err := resolveScrapeScope(&env, models.MediaScrapeParams{Scope: &models.MediaScrapeScope{
		File: &models.MediaScrapePath{System: "NES", Path: path},
	}})
	require.ErrorContains(t, err, "not found")
	mdb.AssertExpectations(t)
}

func TestResumeScrapeScopeFailsClosed(t *testing.T) {
	// Shared operation status prevents parallel execution.
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid", true: "stale identity"}[stale], func(t *testing.T) {
			ClearScrapingStatus()
			statusInstance.clear()
			mdb := helpers.NewMockMediaDBI()
			scope := &database.ScrapeScope{}
			if stale {
				scope = &database.ScrapeScope{
					SystemID: "NES", MediaID: 42, Path: filepath.ToSlash(filepath.Join(t.TempDir(), "game.nes")),
				}
				mdb.On("GetScrapeMedia", mock.Anything, *scope).Return([]database.MediaFullRow{}, nil).Once()
			}
			env := makeScrapeEnv(t, map[string]platforms.Scraper{
				"test": emptyPlatformScraper("test", "Test"),
			}, mdb, nil)
			err := ResumeMediaScrape(&env, database.ScrapingOperation{ScraperID: "test", Scope: scope})
			require.Error(t, err)
			require.False(t, IsScrapingRunning())
			mdb.AssertNotCalled(t, "SetScrapingOperation", mock.Anything)
			mdb.AssertExpectations(t)
		})
	}
}
