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
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestScummVMMarkerSourceUsesIndexedTarget(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := filepath.Join(string(filepath.Separator), "games")
	sourcePath := filepath.Join(root, "Monkey")
	source := database.MediaSource{
		MediaDBID: 1, MediaPath: "scummvm://monkey/Monkey%20Island", SourcePath: sourcePath,
		SourceKey: helpers.NormalizePathForComparison(sourcePath), SourceRoot: root,
		SourceKind: "directory", Unique: true,
	}
	index := scraper.NewSourceIndex([]database.MediaSource{source})
	impl := &GamelistXMLScraper{fs: fs}

	for _, tc := range []struct {
		name string
		path string
		data string
		want bool
	}{
		{name: "indexed target", path: filepath.Join(root, "game.scummvm"), data: "\xef\xbb\xbfmonkey\r\n", want: true},
		{name: "not marker", path: filepath.Join(root, "game.txt"), data: "monkey"},
		{name: "missing marker", path: filepath.Join(root, "missing.scummvm")},
		{name: "empty target", path: filepath.Join(root, "empty.scummvm"), data: " \r\n"},
		{name: "control character", path: filepath.Join(root, "control.scummvm"), data: "mon\x00key"},
		{name: "oversized", path: filepath.Join(root, "large.scummvm"), data: strings.Repeat("x", 1025)},
		{name: "unknown target", path: filepath.Join(root, "unknown.scummvm"), data: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.data != "" {
				require.NoError(t, fs.MkdirAll(filepath.Dir(tc.path), 0o750))
				require.NoError(t, afero.WriteFile(fs, tc.path, []byte(tc.data), 0o600))
			}
			got, ok := impl.scummVMMarkerSource(index, tc.path)
			require.Equal(t, tc.want, ok)
			if tc.want {
				require.Equal(t, source, got)
			}
		})
	}
}

func TestMatchSourceRecordFailsClosedWithoutUsableOwnership(t *testing.T) {
	t.Parallel()
	impl := &GamelistXMLScraper{}
	file := &parsedGamelistFile{}
	game := &esapi.Game{Path: "scummvm://monkey/Title"}
	require.Nil(t, impl.matchSourceRecord(loadRecordIndexes{}, nil, file, game, ""))

	source := database.MediaSource{
		MediaDBID: 2, MediaPath: game.Path, SourcePath: "/games/Monkey",
		SourceKey: "/games/monkey", SourceRoot: "/games", SourceKind: "directory", Unique: true,
	}
	records := &sourceRecordIndex{sources: scraper.NewSourceIndex([]database.MediaSource{source})}
	indexes := loadRecordIndexes{MediaByPathFold: map[string]database.Media{
		pathFoldKey(game.Path): {DBID: 1},
	}}
	require.Nil(t, impl.matchSourceRecord(indexes, records, file, game, ""),
		"source MediaDBID must own the matching virtual media row")
}

func TestInheritGivesFolderEntriesToUnnamedChildren(t *testing.T) {
	t.Parallel()
	root := filepath.Join(string(filepath.Separator), "games")
	gameFolder := filepath.Join(root, "kyra3")
	src := func(dbid int64, target, dir string, unique bool) database.MediaSource {
		path := filepath.Join(gameFolder, dir)
		return database.MediaSource{
			MediaDBID: dbid, MediaPath: "scummvm://" + target + "/Title", SourcePath: path,
			SourceKey: helpers.NormalizePathForComparison(path), SourceRoot: root,
			SourceKind: "directory", Unique: unique,
		}
	}
	english, french := src(1, "en", "dos-english", true), src(2, "fr", "dos-french", true)
	macOne, macTwo := src(3, "mac-en", "macintosh", false), src(4, "mac-de", "macintosh", false)
	all := []database.MediaSource{english, french, macOne, macTwo}
	records := &sourceRecordIndex{
		sources: scraper.NewSourceIndex(all),
		dirs:    map[string]map[string]string{root: {"boxart": filepath.Join(root, "media", "boxart")}},
	}
	indexes := loadRecordIndexes{MediaByPathFold: make(map[string]database.Media, len(all))}
	for _, source := range all {
		indexes.MediaByPathFold[pathFoldKey(source.MediaPath)] = database.Media{
			DBID: source.MediaDBID, MediaTitleDBID: source.MediaDBID + 10,
		}
	}
	file := &parsedGamelistFile{RootPath: root}
	folder := esapi.Game{Path: "./kyra3", Image: "./kyra3.png"}

	got := records.claimFolders(indexes, []folderEntry{
		{file: file, directory: gameFolder, game: folder},
		{file: file, directory: gameFolder, game: esapi.Game{Path: "./kyra3", Image: "./later.png"}},
	}, records.sources.UnderParent)

	require.Len(t, got, 2, "a second entry for the same folder finds every child already claimed")
	for i, want := range []database.MediaSource{english, french} {
		require.Equal(t, folder, got[i].Game, "the first entry for a folder wins its children")
		require.Equal(t, gameFolder, got[i].SourceDirectory,
			"artwork falls back to the name of the folder the entry describes")
		require.Equal(t, root, got[i].ROMRootPath)
		require.Equal(t, want.MediaDBID, got[i].MatchedMediaDBID)
		require.Equal(t, gamelistMatchSource, got[i].MatchKind)
		require.True(t, got[i].MediaLevelWriteSafe)
	}
	require.NotContains(t, indexes.MediaByPathFold, pathFoldKey(english.MediaPath),
		"a claimed row leaves the path index so no later fallback guesses at it")
	require.Contains(t, indexes.MediaByPathFold, pathFoldKey(macOne.MediaPath),
		"targets sharing a directory of another scheme are never claimed by the folder holding them")
	require.Contains(t, indexes.MediaByPathFold, pathFoldKey(macTwo.MediaPath))

	require.Empty(t, records.claimFolders(indexes, []folderEntry{{file: file, directory: root, game: folder}},
		records.sources.UnderParent), "the collection root is not a game folder")
}
