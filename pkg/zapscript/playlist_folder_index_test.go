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

package zapscript

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A folder playlist is the folder's browse listing: the same query, the
// same filename order, hidden media left out, browse's names. Item n of
// the playlist is entry n of the listing a client browsed.
func TestFolderPlaylistItems_FollowTheBrowseListing(t *testing.T) {
	t.Parallel()

	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("BrowseFiles", mock.Anything, mock.MatchedBy(func(opts *database.BrowseFilesOptions) bool {
		return opts.PathPrefix == "/music/Mix/" && opts.Sort == "filename-asc" &&
			opts.ExcludeHidden && opts.Cursor == nil && len(opts.Systems) == 0
	})).Return([]database.SearchResultWithCursor{
		{Name: "Departure", Path: "/music/Mix/01 Departure.mp3", MediaID: 1},
		{Name: "Coastline", Path: "/music/Mix/02 Coastline.mp3", MediaID: 2},
	}, nil).Once()
	env := &platforms.CmdEnv{Database: &database.Database{MediaDB: mediaDB}}

	items, err := folderPlaylistItems(env, mocks.NewMockPlatform(), "/music/Mix", nil)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Departure", items[0].Name)
	assert.Equal(t, "/music/Mix/01 Departure.mp3", items[0].ZapScript)
	assert.Equal(t, "/music/Mix/02 Coastline.mp3", items[1].ZapScript)
	mediaDB.AssertExpectations(t)
}

func TestFolderPlaylistItems_PagesThroughALargeFolder(t *testing.T) {
	t.Parallel()

	page := func(from, n int) []database.SearchResultWithCursor {
		rows := make([]database.SearchResultWithCursor, 0, n)
		for i := from; i < from+n; i++ {
			rows = append(rows, database.SearchResultWithCursor{
				Name: fmt.Sprintf("Track %04d", i), Path: fmt.Sprintf("/m/%04d.mp3", i),
				MediaID: int64(i + 1), SortValue: fmt.Sprintf("%04d.mp3", i), SortMode: "path",
			})
		}
		return rows
	}
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("BrowseFiles", mock.Anything, mock.MatchedBy(func(opts *database.BrowseFilesOptions) bool {
		return opts.Cursor == nil
	})).Return(page(0, folderPageSize), nil).Once()
	mediaDB.On("BrowseFiles", mock.Anything, mock.MatchedBy(func(opts *database.BrowseFilesOptions) bool {
		return opts.Cursor != nil && opts.Cursor.LastID == int64(folderPageSize) &&
			opts.Cursor.SortValue == fmt.Sprintf("%04d.mp3", folderPageSize-1)
	})).Return(page(folderPageSize, 3), nil).Once()
	env := &platforms.CmdEnv{Database: &database.Database{MediaDB: mediaDB}}

	items, err := folderPlaylistItems(env, mocks.NewMockPlatform(), "/m", nil)
	require.NoError(t, err)
	require.Len(t, items, folderPageSize+3)
	assert.Equal(t, "/m/0502.mp3", items[len(items)-1].ZapScript)
	mediaDB.AssertExpectations(t)
}

// A folder the index does not cover is read from disk, as before.
func TestFolderPlaylistItems_ReadsAnUnindexedFolderFromDisk(t *testing.T) {
	initTestLauncherCache(t, []string{".nes"})

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.nes"), []byte{}, 0o600))
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{}, nil).Once()
	env := &platforms.CmdEnv{
		Cfg:      &config.Instance{},
		Database: &database.Database{MediaDB: mediaDB},
	}

	items, err := folderPlaylistItems(env, mocks.NewMockPlatform(), dir, nil)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, filepath.Join(dir, "alpha.nes"), items[0].ZapScript)
}

// With a system, the index is read as a browse filtered by that system.
func TestFolderPlaylistItems_SystemFiltersTheIndex(t *testing.T) {
	t.Parallel()

	audio, err := systemdefs.LookupSystem("Audio")
	require.NoError(t, err)
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("BrowseFiles", mock.Anything, mock.MatchedBy(func(opts *database.BrowseFilesOptions) bool {
		return len(opts.Systems) == 1 && opts.Systems[0].ID == audio.ID
	})).Return([]database.SearchResultWithCursor{
		{Name: "Departure", Path: "/music/Mix/01 Departure.mp3", MediaID: 1},
	}, nil).Once()
	env := &platforms.CmdEnv{Database: &database.Database{MediaDB: mediaDB}}

	items, err := folderPlaylistItems(env, mocks.NewMockPlatform(), "/music/Mix", audio)
	require.NoError(t, err)
	require.Len(t, items, 1)
	mediaDB.AssertExpectations(t)
}

// With a system, a folder read from disk keeps to that system's launchers.
func TestReadPlaylistFolder_SystemKeepsToItsLaunchers(t *testing.T) {
	helpers.GlobalLauncherCache.InitializeFromSlice([]platforms.Launcher{
		{ID: "audio", SystemID: "Audio", Extensions: []string{".mp3"}},
		{ID: "nes", SystemID: "NES", Extensions: []string{".nes"}},
	})
	t.Cleanup(func() { helpers.GlobalLauncherCache.InitializeFromSlice(nil) })

	dir := t.TempDir()
	for _, name := range []string{"a.mp3", "b.nes", "c.mp3"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte{}, 0o600))
	}
	audio, err := systemdefs.LookupSystem("Audio")
	require.NoError(t, err)

	mp := mocks.NewMockPlatform()
	mp.On("RootDirs", mock.Anything).Return([]string{}).Maybe()
	mp.On("Settings").Return(platforms.Settings{}).Maybe()
	items, err := readPlaylistFolder(&config.Instance{}, mp, dir, audio)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, filepath.Join(dir, "a.mp3"), items[0].ZapScript)
	assert.Equal(t, filepath.Join(dir, "c.mp3"), items[1].ZapScript)

	all, err := readPlaylistFolder(&config.Instance{}, mp, dir, nil)
	require.NoError(t, err)
	assert.Len(t, all, 3, "without a system every launcher counts")
}
