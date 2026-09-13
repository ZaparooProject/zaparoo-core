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
