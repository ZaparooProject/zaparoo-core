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
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// liveBrowseLaunchers mirrors the MiSTer shape that matters here: a generic
// launcher with no folders that accepts any MGL, and a system launcher bound
// to the _Arcade folder.
func liveBrowseLaunchers() []platforms.Launcher {
	return []platforms.Launcher{
		{ID: "Generic", Extensions: []string{".mgl", ".mra", ".rbf"}},
		{ID: "Arcade", SystemID: "Arcade", Folders: []string{"_Arcade"}, Extensions: []string{".mra", ".mgl"}},
	}
}

// newLiveBrowseFixture builds a temp root holding a menu collection folder,
// an Arcade Organizer style folder of symlinked MRAs, and a folder outside
// the root, then returns the root and a platform that reports it.
func newLiveBrowseFixture(t *testing.T) (root, outside string, pl *mocks.MockPlatform) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires Windows privileges")
	}
	base := t.TempDir()
	root = filepath.ToSlash(filepath.Join(base, "fat"))
	outside = filepath.ToSlash(filepath.Join(base, "outside"))

	write := func(path string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	}

	chrono := filepath.Join(root, "_Chronological")
	write(filepath.Join(chrono, "10 Later Game.mgl"))
	write(filepath.Join(chrono, "2 Early Game.mgl"))
	write(filepath.Join(chrono, "1 First Game.mgl"))
	write(filepath.Join(chrono, "notes.txt"))
	write(filepath.Join(chrono, "Odd, Name || [x].mgl"))
	write(filepath.Join(chrono, ".hidden.mgl"))
	require.NoError(t, os.MkdirAll(filepath.Join(chrono, "1980s"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(chrono, "10 Nineties"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(chrono, "2 Eighties"), 0o750))

	write(filepath.Join(outside, "secret.mgl"))
	require.NoError(t, os.Symlink(outside, filepath.Join(chrono, "escape")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.mgl"), filepath.Join(chrono, "escape.mgl")))
	require.NoError(t, os.Symlink(filepath.Join(chrono, "missing.mgl"), filepath.Join(chrono, "broken.mgl")))

	arcade := filepath.Join(root, "_Arcade")
	write(filepath.Join(arcade, "Pac-Man.mra"))
	write(filepath.Join(arcade, "Galaga.mra"))
	organized := filepath.Join(arcade, "_Organized", "_1 A-E")
	require.NoError(t, os.MkdirAll(organized, 0o750))
	require.NoError(t, os.Symlink(filepath.Join("..", "..", "Pac-Man.mra"), filepath.Join(organized, "Pac-Man.mra")))
	require.NoError(t, os.Symlink(filepath.Join(arcade, "Galaga.mra"), filepath.Join(organized, "Galaga.mra")))

	pl = mocks.NewMockPlatform()
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{root})
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(liveBrowseLaunchers())
	pl.On("Settings").Return(platforms.Settings{}).Maybe()
	return root, outside, pl
}

func runLiveBrowse(
	t *testing.T, db *helpers.MockMediaDBI, pl *mocks.MockPlatform, params map[string]any,
) (models.BrowseResults, error) {
	t.Helper()
	params["live"] = true
	env := newBrowseEnv(t, db, pl, params)
	result, err := HandleMediaBrowse(env)
	if err != nil {
		return models.BrowseResults{}, err
	}
	results, ok := result.(models.BrowseResults)
	require.True(t, ok)
	return results, nil
}

func liveEntryNames(entries []models.BrowseEntry) []string {
	names := make([]string, len(entries))
	for i := range entries {
		names[i] = entries[i].Name
	}
	return names
}

func noLiveMatches(db *helpers.MockMediaDBI) {
	db.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).Return([]database.MediaPathID(nil), nil)
}

func TestHandleMediaBrowse_LiveListsCollectionFolder(t *testing.T) {
	t.Parallel()
	root, _, pl := newLiveBrowseFixture(t)
	db := helpers.NewMockMediaDBI()
	noLiveMatches(db)

	dir := root + "/_Chronological"
	results, err := runLiveBrowse(t, db, pl, map[string]any{"path": dir})
	require.NoError(t, err)

	// Folders first, then launchable files, both in natural order. Dot
	// files, non-launchable files, broken links and links leaving the root
	// are hidden.
	assert.Equal(t, []string{
		"2 Eighties", "10 Nineties", "1980s",
		"1 First Game", "2 Early Game", "10 Later Game", "Odd, Name || [x]",
	}, liveEntryNames(results.Entries))
	assert.Equal(t, 3, results.TotalDirs)
	assert.Equal(t, 4, results.TotalFiles)
	assert.Equal(t, dir, results.Path)
	assert.Nil(t, results.Pagination)

	assert.Equal(t, "directory", results.Entries[0].Type)
	assert.Equal(t, dir+"/2 Eighties", results.Entries[0].Path)

	game := results.Entries[3]
	assert.Equal(t, "media", game.Type)
	assert.Equal(t, dir+"/1 First Game.mgl", game.Path)
	assert.Nil(t, game.SystemID, "a generic MGL has no system")
	assert.Zero(t, game.MediaID)
	assertLaunchesPath(t, game.ZapScript, dir+"/1 First Game.mgl")

	// ZapScript syntax in a file name survives the launch command.
	odd := results.Entries[6]
	assert.Equal(t, "Odd, Name || [x]", odd.Name)
	assertLaunchesPath(t, odd.ZapScript, dir+"/Odd, Name || [x].mgl")
}

func TestHandleMediaBrowse_LiveMatchesOrganizerLinksToIndexedMedia(t *testing.T) {
	t.Parallel()
	root, _, pl := newLiveBrowseFixture(t)
	db := helpers.NewMockMediaDBI()

	realArcade, err := filepath.EvalSymlinks(filepath.Join(root, "_Arcade"))
	require.NoError(t, err)
	pacMan := filepath.ToSlash(filepath.Join(realArcade, "Pac-Man.mra"))
	db.On("FindMediaIDsByPaths", mock.Anything, mock.MatchedBy(func(paths []string) bool {
		for _, p := range paths {
			if p == pacMan {
				return true
			}
		}
		return false
	})).Return([]database.MediaPathID{
		{SystemID: "Arcade", Path: pacMan, DBID: 42, MediaTitleDBID: 7},
	}, nil)
	db.On("GetMediaCoverStatus", mock.Anything, []database.MediaRef{{MediaDBID: 42, MediaTitleDBID: 7}}).
		Return(map[int64]bool{42: true}, nil)
	db.On("GetMediaCoverColors", mock.Anything, []int64{42}).
		Return(map[int64]uint32{42: 0x112233}, nil)

	dir := root + "/_Arcade/_Organized/_1 A-E"
	results, err := runLiveBrowse(t, db, pl, map[string]any{"path": dir})
	require.NoError(t, err)
	require.Equal(t, []string{"Galaga", "Pac-Man"}, liveEntryNames(results.Entries))

	galaga := results.Entries[0]
	assert.Zero(t, galaga.MediaID, "an unindexed target stays a plain entry")
	assert.False(t, galaga.HasCover)
	require.NotNil(t, galaga.SystemID)
	assert.Equal(t, "Arcade", *galaga.SystemID, "the folder-bound launcher names the system")

	pac := results.Entries[1]
	assert.Equal(t, int64(42), pac.MediaID)
	assert.True(t, pac.HasCover)
	assert.Equal(t, "#112233", pac.CoverColor)
	require.NotNil(t, pac.SystemID)
	assert.Equal(t, "Arcade", *pac.SystemID)
	assert.Equal(t, dir+"/Pac-Man.mra", pac.Path, "the entry keeps the path the user picked")
	assertLaunchesPath(t, pac.ZapScript, dir+"/Pac-Man.mra")
	db.AssertExpectations(t)
}

func TestHandleMediaBrowse_LiveAmbiguousPathStaysUnmatched(t *testing.T) {
	t.Parallel()
	root, _, pl := newLiveBrowseFixture(t)
	db := helpers.NewMockMediaDBI()
	path := root + "/_Arcade/Galaga.mra"
	db.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).Return([]database.MediaPathID{
		{SystemID: "Arcade", Path: path, DBID: 1},
		{SystemID: "Other", Path: path, DBID: 2},
	}, nil)

	results, err := runLiveBrowse(t, db, pl, map[string]any{"path": root + "/_Arcade"})
	require.NoError(t, err)
	for _, entry := range results.Entries {
		assert.Zero(t, entry.MediaID, entry.Name)
	}
}

func TestHandleMediaBrowse_LiveDatabaseErrorKeepsEntries(t *testing.T) {
	t.Parallel()
	root, _, pl := newLiveBrowseFixture(t)
	db := helpers.NewMockMediaDBI()
	db.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).Return(nil, assert.AnError)

	results, err := runLiveBrowse(t, db, pl, map[string]any{"path": root + "/_Arcade"})
	require.NoError(t, err)
	assert.Equal(t, []string{"_Organized", "Galaga", "Pac-Man"}, liveEntryNames(results.Entries))
}

func TestHandleMediaBrowse_LivePaginates(t *testing.T) {
	t.Parallel()
	root, _, pl := newLiveBrowseFixture(t)
	db := helpers.NewMockMediaDBI()
	noLiveMatches(db)
	dir := root + "/_Chronological"

	var names []string
	params := map[string]any{"path": dir, "maxResults": 2}
	for range 5 {
		results, err := runLiveBrowse(t, db, pl, params)
		require.NoError(t, err)
		names = append(names, liveEntryNames(results.Entries)...)
		assert.Equal(t, 3, results.TotalDirs)
		assert.Equal(t, 4, results.TotalFiles)
		if results.Pagination == nil || !results.Pagination.HasNextPage {
			break
		}
		require.NotNil(t, results.Pagination.NextCursor)
		params = map[string]any{"path": dir, "maxResults": 2, "cursor": *results.Pagination.NextCursor}
	}
	assert.Equal(t, []string{
		"2 Eighties", "10 Nineties", "1980s",
		"1 First Game", "2 Early Game", "10 Later Game", "Odd, Name || [x]",
	}, names)
}

func TestHandleMediaBrowse_LiveSortAndLetter(t *testing.T) {
	t.Parallel()
	root, _, pl := newLiveBrowseFixture(t)
	db := helpers.NewMockMediaDBI()
	noLiveMatches(db)
	dir := root + "/_Chronological"

	results, err := runLiveBrowse(t, db, pl, map[string]any{"path": dir, "sort": "name-desc"})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"2 Eighties", "10 Nineties", "1980s",
		"Odd, Name || [x]", "10 Later Game", "2 Early Game", "1 First Game",
	}, liveEntryNames(results.Entries), "sort orders files; folders stay ascending")

	results, err = runLiveBrowse(t, db, pl, map[string]any{"path": root + "/_Arcade", "letter": "p"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Pac-Man"}, liveEntryNames(results.Entries))
	assert.Equal(t, 0, results.TotalDirs)
	assert.Equal(t, 1, results.TotalFiles)
}

func TestHandleMediaBrowse_LiveRejectsBadRequests(t *testing.T) {
	t.Parallel()
	root, outside, pl := newLiveBrowseFixture(t)
	dbCursor, err := encodeDirCursor("x", 0, 0, nil)
	require.NoError(t, err)
	liveCursor := base64.StdEncoding.EncodeToString(mustJSON(t, browseCursorData{
		Phase: browsePhaseLiveFiles, DirName: "x",
	}))

	tests := []struct {
		params map[string]any
		name   string
		want   string
	}{
		{name: "no path", params: map[string]any{}, want: "requires a path"},
		{name: "traversal", params: map[string]any{"path": root + "/_Chronological/../../outside"}, want: "disallowed"},
		{name: "outside root", params: map[string]any{"path": outside}, want: "not within an allowed root"},
		{
			name:   "symlink escape",
			params: map[string]any{"path": root + "/_Chronological/escape"},
			want:   "not within an allowed root",
		},
		{name: "missing", params: map[string]any{"path": root + "/_Nope"}, want: "path not found"},
		{name: "file", params: map[string]any{"path": root + "/_Arcade/Galaga.mra"}, want: "not a directory"},
		{name: "virtual", params: map[string]any{"path": "steam://"}, want: "filesystem path"},
		{name: "tags", params: map[string]any{"path": root, "tags": []string{"region:us"}}, want: "tag filters"},
		{name: "systems", params: map[string]any{"path": root, "systems": []string{"NES"}}, want: "system filters"},
		{name: "root view", params: map[string]any{"path": root, "rootView": "routes"}, want: "rootView"},
		{
			name:   "indexed cursor",
			params: map[string]any{"path": root, "cursor": dbCursor},
			want:   "not a live browse cursor",
		},
		{
			name:   "sort mismatch",
			params: map[string]any{"path": root, "cursor": liveCursor, "sort": "name-desc"},
			want:   "does not match sort",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := runLiveBrowse(t, helpers.NewMockMediaDBI(), pl, tt.params)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestHandleMediaBrowse_IndexedRejectsLiveCursor(t *testing.T) {
	t.Parallel()
	_, _, pl := newLiveBrowseFixture(t)
	liveCursor := base64.StdEncoding.EncodeToString(mustJSON(t, browseCursorData{
		Phase: browsePhaseLiveDirs, DirName: "x",
	}))
	env := newBrowseEnv(t, helpers.NewMockMediaDBI(), pl, map[string]any{"path": "/x", "cursor": liveCursor})
	_, err := HandleMediaBrowse(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cursor phase")
}

func assertLaunchesPath(t *testing.T, script *string, path string) {
	t.Helper()
	require.NotNil(t, script)
	parsed, err := zapscript.NewParser(*script).ParseScript()
	require.NoError(t, err)
	require.Len(t, parsed.Cmds, 1)
	assert.Equal(t, zapscript.ZapScriptCmdLaunch, parsed.Cmds[0].Name)
	assert.Equal(t, []string{path}, parsed.Cmds[0].Args)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
