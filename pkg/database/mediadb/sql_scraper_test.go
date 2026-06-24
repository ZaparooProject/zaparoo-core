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

package mediadb

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupScraperTestDB creates a minimal MediaDB with:
//   - Systems: "NES" (DBID=1)
//   - TagTypes: "scraper.test" (additive, DBID=1), "developer" (exclusive, DBID=2), "property" (additive, DBID=3)
//   - MediaTitles: "mario" (DBID=1)
//   - Media: "roms/mario.nes" (DBID=1) linked to MediaTitle 1
//   - Tags: "property:description" seeded (DBID=1)
func setupScraperTestDB(t *testing.T) (mediaDB *MediaDB, cleanup func()) {
	t.Helper()
	mediaDB, cleanup = setupTempMediaDB(t)
	ctx := context.Background()
	db := mediaDB.sql.Load()

	mediaPath := filepath.ToSlash(filepath.Join("roms", "mario.nes"))
	_, err := db.ExecContext(ctx, `
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES
		    (1, 'scraper.test', 0),
		    (2, 'developer',    1),
		    (3, 'property',     0);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES
		    (1, 3, 'description'),
		    (2, 3, 'image-boxart');
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'mario', 'Mario');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (1, 1, 1, ?);
	`, mediaPath)
	require.NoError(t, err)

	return mediaDB, cleanup
}

// --- FindMediaBySystemAndPath ---

func TestFindMediaBySystemAndPath_Found(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	mediaPath := filepath.ToSlash(filepath.Join("roms", "mario.nes"))
	m, err := mediaDB.FindMediaBySystemAndPath(context.Background(), 1, mediaPath)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, int64(1), m.DBID)
	assert.Equal(t, int64(1), m.MediaTitleDBID)
	assert.Equal(t, filepath.ToSlash(filepath.Join("roms", "mario.nes")), m.Path)
}

func TestFindMediaBySystemAndPath_NotFound(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	mediaPath := filepath.Join("roms", "nonexistent.nes")
	m, err := mediaDB.FindMediaBySystemAndPath(context.Background(), 1, mediaPath)
	require.NoError(t, err)
	assert.Nil(t, m)
}

func TestFindMediaBySystemAndPath_WrongSystem(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	mediaPath := filepath.Join("roms", "mario.nes")
	m, err := mediaDB.FindMediaBySystemAndPath(context.Background(), 99, mediaPath)
	require.NoError(t, err)
	assert.Nil(t, m, "path exists but systemDBID doesn't match")
}

func TestFindMediaBySystemAndPaths_ReturnsMatchesByPath(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	marioPath := filepath.ToSlash(filepath.Join("roms", "mario.nes"))
	zeldaPath := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	missingPath := filepath.ToSlash(filepath.Join("roms", "missing.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, zeldaPath)
	require.NoError(t, err)

	results, err := mediaDB.FindMediaBySystemAndPaths(ctx, 1, []string{marioPath, zeldaPath, missingPath})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, int64(1), results[marioPath].DBID)
	assert.Equal(t, int64(2), results[zeldaPath].DBID)
	assert.NotContains(t, results, missingPath)
}

func TestFindMediaBySystemAndPaths_EmptyInput(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	results, err := mediaDB.FindMediaBySystemAndPaths(context.Background(), 1, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestFindMediaIDsByPaths_EmptyInput(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	results, err := mediaDB.FindMediaIDsByPaths(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestFindMediaIDsByPaths_ReturnsSamePathAcrossSystems(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath := filepath.ToSlash(filepath.Join("roms", "mario.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (2, 'SNES', 'Super Nintendo');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 2, 'mario', 'Mario');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 2, ?);
	`, mediaPath)
	require.NoError(t, err)

	results, err := mediaDB.FindMediaIDsByPaths(ctx, []string{mediaPath})
	require.NoError(t, err)
	assert.ElementsMatch(t, []database.MediaPathID{
		{SystemID: "NES", Path: mediaPath, DBID: 1},
		{SystemID: "SNES", Path: mediaPath, DBID: 2},
	}, results)
}

func TestFindMediaIDsByPaths_ChunksLargeInput(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	marioPath := filepath.ToSlash(filepath.Join("roms", "mario.nes"))
	zeldaPath := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, zeldaPath)
	require.NoError(t, err)

	paths := make([]string, 0, sqliteMaxParams+2)
	paths = append(paths, marioPath)
	for i := range sqliteMaxParams {
		paths = append(paths, filepath.ToSlash(filepath.Join("roms", "missing", fmt.Sprintf("game-%04d.nes", i))))
	}
	paths = append(paths, zeldaPath)

	results, err := mediaDB.FindMediaIDsByPaths(ctx, paths)
	require.NoError(t, err)
	assert.ElementsMatch(t, []database.MediaPathID{
		{SystemID: "NES", Path: marioPath, DBID: 1},
		{SystemID: "NES", Path: zeldaPath, DBID: 2},
	}, results)
}

func TestFindSingleContainerLaunchMedia_ReturnsOnlyDirectChild(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	container := filepath.ToSlash(filepath.Join("roms", "Zelda"))
	childPath := filepath.ToSlash(filepath.Join(container, "zelda.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES (2, 2, 1, ?, ?);
	`, childPath, container+"/")
	require.NoError(t, err)

	media, err := mediaDB.FindSingleContainerLaunchMedia(ctx, 1, container)
	require.NoError(t, err)
	require.NotNil(t, media)
	assert.Equal(t, int64(2), media.DBID)
	assert.Equal(t, childPath, media.Path)
}

func TestFindSingleContainerLaunchMedia_ReturnsCueForCueBinFolder(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	container := filepath.ToSlash(filepath.Join("roms", "PSX", "Game"))
	parentDir := container + "/"
	cuePath := filepath.ToSlash(filepath.Join(container, "Game.cue"))
	binPath := filepath.ToSlash(filepath.Join(container, "Game.bin"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(2, 1, 'game-cue', 'Game Cue'),
			(3, 1, 'game-bin', 'Game Bin');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(2, 2, 1, ?, ?),
			(3, 3, 1, ?, ?);
	`, cuePath, parentDir, binPath, parentDir)
	require.NoError(t, err)

	media, err := mediaDB.FindSingleContainerLaunchMedia(ctx, 1, container)
	require.NoError(t, err)
	require.NotNil(t, media)
	assert.Equal(t, int64(2), media.DBID)
	assert.Equal(t, cuePath, media.Path)
}

func TestFindSingleContainerLaunchMedia_ReturnsM3UForDiscFolder(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	container := filepath.ToSlash(filepath.Join("roms", "PSX", "Multi Disc"))
	parentDir := container + "/"
	m3uPath := filepath.ToSlash(filepath.Join(container, "Game.m3u"))
	cuePath := filepath.ToSlash(filepath.Join(container, "Disc 1.cue"))
	binPath := filepath.ToSlash(filepath.Join(container, "Disc 1.bin"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(2, 1, 'game-m3u', 'Game M3U'),
			(3, 1, 'disc-cue', 'Disc Cue'),
			(4, 1, 'disc-bin', 'Disc Bin');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(2, 2, 1, ?, ?),
			(3, 3, 1, ?, ?),
			(4, 4, 1, ?, ?);
	`, m3uPath, parentDir, cuePath, parentDir, binPath, parentDir)
	require.NoError(t, err)

	media, err := mediaDB.FindSingleContainerLaunchMedia(ctx, 1, container)
	require.NoError(t, err)
	require.NotNil(t, media)
	assert.Equal(t, int64(2), media.DBID)
	assert.Equal(t, m3uPath, media.Path)
}

func TestFindSingleContainerLaunchMedia_RejectsAmbiguousDirectChildren(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	container := filepath.ToSlash(filepath.Join("roms", "Collection"))
	parentDir := container + "/"
	onePath := filepath.ToSlash(filepath.Join(container, "one.cue"))
	twoPath := filepath.ToSlash(filepath.Join(container, "two.cue"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(2, 1, 'one', 'One'),
			(3, 1, 'two', 'Two');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(2, 2, 1, ?, ?),
			(3, 3, 1, ?, ?);
	`, onePath, parentDir, twoPath, parentDir)
	require.NoError(t, err)

	media, err := mediaDB.FindSingleContainerLaunchMedia(ctx, 1, container)
	require.NoError(t, err)
	assert.Nil(t, media)
}

func TestFindSingleContainerLaunchMedia_RejectsNestedOnlyOrMixedNestedMedia(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "Parent"))
	childDir := filepath.ToSlash(filepath.Join(parent, "Child"))
	directPath := filepath.ToSlash(filepath.Join(parent, "direct.nes"))
	nestedPath := filepath.ToSlash(filepath.Join(childDir, "nested.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(2, 1, 'direct', 'Direct'),
			(3, 1, 'nested', 'Nested');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(2, 2, 1, ?, ?),
			(3, 3, 1, ?, ?);
	`, directPath, parent+"/", nestedPath, childDir+"/")
	require.NoError(t, err)

	media, err := mediaDB.FindSingleContainerLaunchMedia(ctx, 1, parent)
	require.NoError(t, err)
	assert.Nil(t, media)

	media, err = mediaDB.FindSingleContainerLaunchMedia(ctx, 1, childDir)
	require.NoError(t, err)
	require.NotNil(t, media)
	assert.Equal(t, int64(3), media.DBID)
}

func TestFindSingleContainerLaunchMedia_IgnoresMissingAndOtherSystems(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	container := filepath.ToSlash(filepath.Join("roms", "Shared"))
	parentDir := container + "/"
	nesPath := filepath.ToSlash(filepath.Join(container, "nes.nes"))
	snesPath := filepath.ToSlash(filepath.Join(container, "snes.sfc"))
	missingPath := filepath.ToSlash(filepath.Join(container, "missing.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (2, 'SNES', 'Super Nintendo');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(2, 1, 'nes-game', 'NES Game'),
			(3, 2, 'snes-game', 'SNES Game'),
			(4, 1, 'missing-game', 'Missing Game');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing) VALUES
			(2, 2, 1, ?, ?, 0),
			(3, 3, 2, ?, ?, 0),
			(4, 4, 1, ?, ?, 1);
	`, nesPath, parentDir, snesPath, parentDir, missingPath, parentDir)
	require.NoError(t, err)

	media, err := mediaDB.FindSingleContainerLaunchMedia(ctx, 1, container)
	require.NoError(t, err)
	require.NotNil(t, media)
	assert.Equal(t, int64(2), media.DBID)
}

func TestFindSingleContainerLaunchMedia_UsesByteExactPrefix(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	gameUnderscore := filepath.ToSlash(filepath.Join("roms", "Game_1"))
	gameA := filepath.ToSlash(filepath.Join("roms", "GameA1"))
	gamePercent := filepath.ToSlash(filepath.Join("roms", "Game%1"))
	gameXYZ := filepath.ToSlash(filepath.Join("roms", "GameXYZ1"))
	caseUpper := filepath.ToSlash(filepath.Join("roms", "CaseGame"))
	caseLower := filepath.ToSlash(filepath.Join("roms", "casegame"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(2, 1, 'underscore', 'Underscore'),
			(3, 1, 'wildcard', 'Wildcard'),
			(4, 1, 'percent', 'Percent'),
			(5, 1, 'percent-wildcard', 'Percent Wildcard'),
			(6, 1, 'case-upper', 'Case Upper'),
			(7, 1, 'case-lower', 'Case Lower');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing) VALUES
			(2, 2, 1, ?, ?, 0),
			(3, 3, 1, ?, ?, 0),
			(4, 4, 1, ?, ?, 0),
			(5, 5, 1, ?, ?, 0),
			(6, 6, 1, ?, ?, 0),
			(7, 7, 1, ?, ?, 0);
	`,
		filepath.ToSlash(filepath.Join(gameUnderscore, "game.nes")), gameUnderscore+"/",
		filepath.ToSlash(filepath.Join(gameA, "game.nes")), gameA+"/",
		filepath.ToSlash(filepath.Join(gamePercent, "game.nes")), gamePercent+"/",
		filepath.ToSlash(filepath.Join(gameXYZ, "game.nes")), gameXYZ+"/",
		filepath.ToSlash(filepath.Join(caseUpper, "game.nes")), caseUpper+"/",
		filepath.ToSlash(filepath.Join(caseLower, "game.nes")), caseLower+"/",
	)
	require.NoError(t, err)

	underscore, err := mediaDB.FindSingleContainerLaunchMedia(ctx, 1, gameUnderscore)
	require.NoError(t, err)
	require.NotNil(t, underscore)
	assert.Equal(t, int64(2), underscore.DBID)

	percent, err := mediaDB.FindSingleContainerLaunchMedia(ctx, 1, gamePercent)
	require.NoError(t, err)
	require.NotNil(t, percent)
	assert.Equal(t, int64(4), percent.DBID)

	caseExact, err := mediaDB.FindSingleContainerLaunchMedia(ctx, 1, caseUpper)
	require.NoError(t, err)
	require.NotNil(t, caseExact)
	assert.Equal(t, int64(6), caseExact.DBID)
}

func TestStringPrefixUpperBound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{name: "empty", prefix: "", want: ""},
		{name: "ascii", prefix: "roms/Game/", want: "roms/Game0"},
		{name: "carry", prefix: string([]byte{'a', 0xff}), want: "b"},
		{name: "no bound", prefix: string([]byte{0xff, 0xff}), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, stringPrefixUpperBound(tt.prefix))
		})
	}
}

// --- FindMediaBySystemAndPathFold ---

func TestFindMediaBySystemAndPathFold_ExactMatch(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	mediaPath := filepath.ToSlash(filepath.Join("roms", "mario.nes"))
	m, err := mediaDB.FindMediaBySystemAndPathFold(context.Background(), 1, mediaPath)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, int64(1), m.DBID)
}

func TestFindMediaBySystemAndPathFold_CaseInsensitive(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	// DB has "roms/mario.nes"; query with mixed-case path components as a
	// Windows scraper would produce when the system directory casing in the
	// resolver differs from the on-disk casing the indexer recorded.
	mediaPath := filepath.ToSlash(filepath.Join("ROMS", "Mario.nes"))
	m, err := mediaDB.FindMediaBySystemAndPathFold(context.Background(), 1, mediaPath)
	require.NoError(t, err)
	require.NotNil(t, m, "case-insensitive query must find the row")
	assert.Equal(t, int64(1), m.DBID)
}

func TestFindMediaBySystemAndPathFold_NotFound(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	mediaPath := filepath.Join("roms", "nonexistent.nes")
	m, err := mediaDB.FindMediaBySystemAndPathFold(context.Background(), 1, mediaPath)
	require.NoError(t, err)
	assert.Nil(t, m)
}

func TestFindMediaBySystemAndPathFold_WrongSystem(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	mediaPath := filepath.Join("roms", "mario.nes")
	m, err := mediaDB.FindMediaBySystemAndPathFold(context.Background(), 99, mediaPath)
	require.NoError(t, err)
	assert.Nil(t, m, "path exists but systemDBID doesn't match")
}

// --- FindMediaBySystemAndPathSuffix ---

func TestFindMediaBySystemAndPathSuffix_Found(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	results, err := mediaDB.FindMediaBySystemAndPathSuffix(context.Background(), 1, "mario.nes")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int64(1), results[0].DBID)
	assert.Equal(t, filepath.ToSlash(filepath.Join("roms", "mario.nes")), results[0].Path)
}

func TestFindMediaBySystemAndPathSuffix_NotFound(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	results, err := mediaDB.FindMediaBySystemAndPathSuffix(context.Background(), 1, "nonexistent.nes")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestFindMediaBySystemAndPathSuffix_WrongSystem(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	results, err := mediaDB.FindMediaBySystemAndPathSuffix(context.Background(), 99, "mario.nes")
	require.NoError(t, err)
	assert.Empty(t, results, "path exists but systemDBID doesn't match")
}

func TestFindMediaBySystemAndPathSuffix_MultipleMatches(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	deepPath := filepath.ToSlash(filepath.Join("roms", "sub", "mario.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'mario2', 'Mario 2');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, deepPath)
	require.NoError(t, err)

	results, err := mediaDB.FindMediaBySystemAndPathSuffix(ctx, 1, "mario.nes")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestFindMediaBySystemAndPathSuffix_PartialFilenamNoMatch(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	// "ario.nes" is a suffix of "mario.nes" but not a full filename component.
	// Pattern "%/ario.nes" must not match "roms/mario.nes".
	results, err := mediaDB.FindMediaBySystemAndPathSuffix(context.Background(), 1, "ario.nes")
	require.NoError(t, err)
	assert.Empty(t, results, "partial filename must not match")
}

func TestFindMediaBySystemAndPathSuffix_PercentEscaped(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	specialPath := filepath.ToSlash(filepath.Join("roms", "100% game.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, '100pct', '100% Game');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, specialPath)
	require.NoError(t, err)

	// Querying the literal filename must match exactly — "%" must not act as LIKE wildcard.
	results, err := mediaDB.FindMediaBySystemAndPathSuffix(ctx, 1, "100% game.nes")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int64(2), results[0].DBID)

	// A pattern that would only match if "%" were a wildcard must not return the row.
	results, err = mediaDB.FindMediaBySystemAndPathSuffix(ctx, 1, "100.nes")
	require.NoError(t, err)
	assert.Empty(t, results, "unescaped %% would make this match; escaped it must not")
}

func TestFindMediaBySystemAndPathSuffix_UnderscoreEscaped(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	specialPath := filepath.ToSlash(filepath.Join("roms", "game_one.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'game-one', 'Game One');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, specialPath)
	require.NoError(t, err)

	// "gameXone.nes" would match if "_" were a LIKE wildcard; it must not.
	results, err := mediaDB.FindMediaBySystemAndPathSuffix(ctx, 1, "gameXone.nes")
	require.NoError(t, err)
	assert.Empty(t, results, "unescaped _ would make this match; escaped it must not")

	// The exact filename must match.
	results, err = mediaDB.FindMediaBySystemAndPathSuffix(ctx, 1, "game_one.nes")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int64(2), results[0].DBID)
}

// --- MediaHasTag ---

func TestMediaHasTag_True(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Insert tag DBID=1 (property:description) on media DBID=1.
	_, err := mediaDB.sql.Load().ExecContext(ctx,
		"INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES (1, 1)")
	require.NoError(t, err)

	// MediaHasTag splits on the first colon: "property" → TagTypes.Type, "description" → Tags.Tag.
	has, err := mediaDB.MediaHasTag(ctx, 1, "property:description")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestMediaHasTag_True_Sentinel(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Write the sentinel tag via UpsertMediaTags (Type="scraper.test", Tag="scraped").
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
		{Type: "scraper.test", Tag: "scraped"},
	}))

	// MediaHasTag must find it using the "type:value" combined string.
	has, err := mediaDB.MediaHasTag(ctx, 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestMediaHasTag_False(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	has, err := mediaDB.MediaHasTag(context.Background(), 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestGetScrapedMediaCount(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath2 := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, mediaPath2)
	require.NoError(t, err)

	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 2, []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 2, []database.TagInfo{{Type: "scraper.other", Tag: "scraped"}}))

	count, err := mediaDB.GetScrapedMediaCount(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	otherCount, err := mediaDB.GetScrapedMediaCount(ctx, "other")
	require.NoError(t, err)
	assert.Equal(t, 1, otherCount)
}

func TestGetScrapedMediaCount_MissingSentinelReturnsZero(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	count, err := mediaDB.GetScrapedMediaCount(context.Background(), "missing")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetTotalScrapedMediaCount_DistinctMedia(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath2 := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, mediaPath2)
	require.NoError(t, err)

	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{{Type: "scraper.other", Tag: "scraped"}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 2, []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 2, []database.TagInfo{{Type: "genre", Tag: "platform"}}))

	count, err := mediaDB.GetTotalScrapedMediaCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestGetTotalScrapedMediaCount_TitlePropertyOnlyDoesNotCount(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath2 := filepath.ToSlash(filepath.Join("roms", "zelda-rev-a.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 1, 1, ?);
	`, mediaPath2)
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, []database.MediaProperty{
		{TypeTag: "property:description", Text: "scraped metadata"},
	}))

	count, err := mediaDB.GetTotalScrapedMediaCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetTotalScrapedMediaCount_MediaPropertyOnlyDoesNotCount(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath2 := filepath.ToSlash(filepath.Join("roms", "mario-rev-a.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 1, 1, ?);
	`, mediaPath2)
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, []database.MediaProperty{
		{TypeTag: "property:description", Text: "media-only scraped metadata"},
	}))

	count, err := mediaDB.GetTotalScrapedMediaCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetTotalScrapedMediaCount_MissingSentinelsReturnsZero(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	count, err := mediaDB.GetTotalScrapedMediaCount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetScrapedMediaIDs(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath2 := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	mediaPathOtherSystem := filepath.ToSlash(filepath.Join("roms", "sonic.md"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (2, 'Genesis', 'Genesis');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
		    (2, 1, 'zelda', 'Zelda'),
		    (3, 2, 'sonic', 'Sonic');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES
		    (2, 2, 1, ?),
		    (3, 3, 2, ?);
	`, mediaPath2, mediaPathOtherSystem)
	require.NoError(t, err)

	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 2, []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 3, []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 2, []database.TagInfo{{Type: "scraper.other", Tag: "scraped"}}))

	ids, err := mediaDB.GetScrapedMediaIDs(ctx, "test", 1)
	require.NoError(t, err)
	assert.Equal(t, map[int64]struct{}{1: {}, 2: {}}, ids)
}

func TestGetScrapedMediaIDs_MissingSentinelReturnsEmptySet(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	ids, err := mediaDB.GetScrapedMediaIDs(context.Background(), "missing", 1)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestGetScrapeRunMediaIDs(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath2 := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, mediaPath2)
	require.NoError(t, err)

	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{{
		Type: string(tags.ScraperRunType("test")),
		Tag:  "run-1",
	}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 2, []database.TagInfo{{
		Type: string(tags.ScraperRunType("test")),
		Tag:  "run-2",
	}}))

	ids, err := mediaDB.GetScrapeRunMediaIDs(ctx, "test", "run-1", 1)
	require.NoError(t, err)
	assert.Equal(t, map[int64]struct{}{1: {}}, ids)

	require.NoError(t, mediaDB.ClearScrapeRunMarkers(ctx, "test", "run-1"))
	ids, err = mediaDB.GetScrapeRunMediaIDs(ctx, "test", "run-1", 1)
	require.NoError(t, err)
	assert.Empty(t, ids)

	var remainingRunTags int
	err = mediaDB.sql.Load().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM Tags t
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type = 'scraper-run.test'
	`).Scan(&remainingRunTags)
	require.NoError(t, err)
	assert.Equal(t, 1, remainingRunTags, "only the unrelated run marker should remain")
}

func TestScrapeRunMarkers_EmptyRunIDIsNoop(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	ids, err := mediaDB.GetScrapeRunMediaIDs(ctx, "test", "", 1)
	require.NoError(t, err)
	assert.Empty(t, ids)
	require.NoError(t, mediaDB.ClearScrapeRunMarkers(ctx, "test", ""))
}

func TestClearScrapeRunMarkers_MissingRunIsNoop(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, mediaDB.ClearScrapeRunMarkers(ctx, "test", "missing-run"))
}

func TestApplyScrapeResult_WritesSentinelLastPayload(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := mediaDB.ApplyScrapeResult(ctx, 1, 1, &database.ScrapeWrite{
		Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
		MediaTags: []database.TagInfo{{Type: "developer", Tag: "nintendo", Label: "Nintendo"}},
		TitleTags: []database.TagInfo{{Type: "publisher", Tag: "nintendo", Label: "Nintendo"}},
		TitleProps: []database.MediaProperty{{
			TypeTag: "property:description",
			Text:    "A classic",
		}},
		MediaProps: []database.MediaProperty{{
			TypeTag: "property:image-boxart",
			Text:    filepath.ToSlash(filepath.Join("media", "boxart", "mario.png")),
		}},
	})
	require.NoError(t, err)

	hasSentinel, err := mediaDB.MediaHasTag(ctx, 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.True(t, hasSentinel)

	titleProps, err := mediaDB.GetMediaTitleProperties(ctx, 1)
	require.NoError(t, err)
	assert.Condition(t, func() bool {
		for _, prop := range titleProps {
			if prop.TypeTag == "property:description" && prop.Text == "A classic" {
				return true
			}
		}
		return false
	})

	usedTags, err := mediaDB.GetAllUsedTags(ctx)
	require.NoError(t, err)
	assert.Contains(t, usedTags, database.TagInfo{Type: "developer", Tag: "nintendo", Label: "Nintendo", Count: 1})
	assert.Contains(t, usedTags, database.TagInfo{Type: "publisher", Tag: "nintendo", Label: "Nintendo", Count: 1})

	mediaProps, err := mediaDB.GetMediaProperties(ctx, 1)
	require.NoError(t, err)
	boxartPath := filepath.ToSlash(filepath.Join("media", "boxart", "mario.png"))
	assert.Condition(t, func() bool {
		for _, prop := range mediaProps {
			if prop.TypeTag == "property:image-boxart" && prop.Text == boxartPath {
				return true
			}
		}
		return false
	})
}

func TestApplyScrapeResult_RollsBackBeforeSentinel(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := mediaDB.ApplyScrapeResult(ctx, 1, 1, &database.ScrapeWrite{
		Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
		MediaTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
		TitleProps: []database.MediaProperty{{
			TypeTag: "property:missing-type-tag",
			Text:    "should roll back",
		}},
	})
	require.Error(t, err)

	hasDeveloper, err := mediaDB.MediaHasTag(ctx, 1, "developer:nintendo")
	require.NoError(t, err)
	assert.False(t, hasDeveloper, "metadata written before the failure should be rolled back")

	hasSentinel, err := mediaDB.MediaHasTag(ctx, 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.False(t, hasSentinel, "failed scrape writes must not mark the record as scraped")
}

func TestApplyScrapeResults_WritesMultipleTargets(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, mediaPath)
	require.NoError(t, err)

	targets := []database.ScrapeWriteTarget{
		{
			MediaDBID: 1, MediaTitleDBID: 1,
			Write: &database.ScrapeWrite{
				Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				MediaTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
				TitleProps: []database.MediaProperty{{
					TypeTag: "property:description",
					Text:    "Mario description",
				}},
			},
		},
		{
			MediaDBID: 2, MediaTitleDBID: 2,
			Write: &database.ScrapeWrite{
				Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				TitleTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
				MediaProps: []database.MediaProperty{{
					TypeTag: "property:image-boxart",
					Text:    filepath.ToSlash(filepath.Join("media", "boxart", "zelda.png")),
				}},
			},
		},
	}

	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, targets))

	for _, mediaDBID := range []int64{1, 2} {
		hasSentinel, tagErr := mediaDB.MediaHasTag(ctx, mediaDBID, "scraper.test:scraped")
		require.NoError(t, tagErr)
		assert.True(t, hasSentinel)
	}

	titleProps, err := mediaDB.GetMediaTitleProperties(ctx, 1)
	require.NoError(t, err)
	assert.Condition(t, func() bool {
		for _, prop := range titleProps {
			if prop.TypeTag == "property:description" && prop.Text == "Mario description" {
				return true
			}
		}
		return false
	})

	mediaProps, err := mediaDB.GetMediaProperties(ctx, 2)
	require.NoError(t, err)
	boxartPath := filepath.ToSlash(filepath.Join("media", "boxart", "zelda.png"))
	assert.Condition(t, func() bool {
		for _, prop := range mediaProps {
			if prop.TypeTag == "property:image-boxart" && prop.Text == boxartPath {
				return true
			}
		}
		return false
	})
}

func TestApplyScrapeResults_SkipsUnchangedTitleMetadataAndStillWritesSentinel(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	write := &database.ScrapeWrite{
		Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
		TitleTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
		TitleProps: []database.MediaProperty{{
			TypeTag: "property:description",
			Text:    "A classic",
		}},
	}
	target := database.ScrapeWriteTarget{MediaDBID: 1, MediaTitleDBID: 1, Write: write}
	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{target}))
	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{target}))

	var titleTagLinks int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM MediaTitleTags mtt
		JOIN Tags t ON mtt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mtt.MediaTitleDBID = 1 AND tt.Type = 'developer' AND t.Tag = 'nintendo'
	`).Scan(&titleTagLinks))
	assert.Equal(t, 1, titleTagLinks)

	var titlePropRows int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM MediaTitleProperties mtp
		JOIN Tags t ON mtp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mtp.MediaTitleDBID = 1
		  AND tt.Type = 'property'
		  AND t.Tag = 'description'
		  AND mtp.Text = 'A classic'
	`).Scan(&titlePropRows))
	assert.Equal(t, 1, titlePropRows)

	hasSentinel, err := mediaDB.MediaHasTag(ctx, 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.True(t, hasSentinel)
}

func TestApplyScrapeResults_ReplacesChangedExclusiveTitleTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	first := database.ScrapeWriteTarget{
		MediaDBID: 1, MediaTitleDBID: 1,
		Write: &database.ScrapeWrite{
			Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
			TitleTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
		},
	}
	second := database.ScrapeWriteTarget{
		MediaDBID: 1, MediaTitleDBID: 1,
		Write: &database.ScrapeWrite{
			Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
			TitleTags: []database.TagInfo{{Type: "developer", Tag: "capcom"}},
		},
	}
	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{first}))
	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{second}))

	var developerTags []string
	rows, err := mediaDB.sql.Load().QueryContext(ctx, `
		SELECT t.Tag
		FROM MediaTitleTags mtt
		JOIN Tags t ON mtt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mtt.MediaTitleDBID = 1 AND tt.Type = 'developer'
		ORDER BY t.Tag
	`)
	require.NoError(t, err)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Fatalf("failed to close rows: %v", closeErr)
		}
	}()
	for rows.Next() {
		var tag string
		require.NoError(t, rows.Scan(&tag))
		developerTags = append(developerTags, tag)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"capcom"}, developerTags)
}

func TestApplyScrapeResults_ExclusiveTitleTagReplaceKeepsOtherTypes(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	first := database.ScrapeWriteTarget{
		MediaDBID: 1, MediaTitleDBID: 1,
		Write: &database.ScrapeWrite{
			Sentinel: database.TagInfo{Type: "scraper.test", Tag: "scraped"},
			TitleTags: []database.TagInfo{
				{Type: "developer", Tag: "nintendo"},
				{Type: "genre", Tag: "platformer"},
			},
		},
	}
	second := database.ScrapeWriteTarget{
		MediaDBID: 1, MediaTitleDBID: 1,
		Write: &database.ScrapeWrite{
			Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
			TitleTags: []database.TagInfo{{Type: "developer", Tag: "capcom"}},
		},
	}
	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{first}))
	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{second}))

	assert.Equal(t, []string{"capcom"}, getTitleTagValuesForType(ctx, t, mediaDB, "developer"))
	assert.Equal(t, []string{"platformer"}, getTitleTagValuesForType(ctx, t, mediaDB, "genre"))
}

func getTitleTagValuesForType(ctx context.Context, t *testing.T, mediaDB *MediaDB, typeName string) []string {
	t.Helper()
	rows, err := mediaDB.sql.Load().QueryContext(ctx, `
		SELECT t.Tag
		FROM MediaTitleTags mtt
		JOIN Tags t ON mtt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mtt.MediaTitleDBID = 1 AND tt.Type = ?
		ORDER BY t.Tag
	`, typeName)
	require.NoError(t, err)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Fatalf("failed to close rows: %v", closeErr)
		}
	}()

	var values []string
	for rows.Next() {
		var tag string
		require.NoError(t, rows.Scan(&tag))
		values = append(values, tag)
	}
	require.NoError(t, rows.Err())
	return values
}

func TestApplyScrapeResults_RollsBackWholeBatchBeforeSentinel(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, mediaPath)
	require.NoError(t, err)

	err = mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{
		{
			MediaDBID: 1, MediaTitleDBID: 1,
			Write: &database.ScrapeWrite{
				Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				MediaTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
			},
		},
		{
			MediaDBID: 2, MediaTitleDBID: 2,
			Write: &database.ScrapeWrite{
				Sentinel: database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				TitleProps: []database.MediaProperty{{
					TypeTag: "property:missing-type-tag",
					Text:    "should fail",
				}},
			},
		},
	})
	require.Error(t, err)

	for _, mediaDBID := range []int64{1, 2} {
		hasSentinel, tagErr := mediaDB.MediaHasTag(ctx, mediaDBID, "scraper.test:scraped")
		require.NoError(t, tagErr)
		assert.False(t, hasSentinel)
	}
	hasDeveloper, err := mediaDB.MediaHasTag(ctx, 1, "developer:nintendo")
	require.NoError(t, err)
	assert.False(t, hasDeveloper)
}

func TestApplyScrapeResults_DoesNotDuplicateSharedTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, mediaPath)
	require.NoError(t, err)

	sharedWrite := func() *database.ScrapeWrite {
		return &database.ScrapeWrite{
			Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
			TitleTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
		}
	}
	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{
		{MediaDBID: 1, MediaTitleDBID: 1, Write: sharedWrite()},
		{MediaDBID: 2, MediaTitleDBID: 2, Write: sharedWrite()},
	}))

	var tagCount int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM Tags t JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type = 'developer' AND t.Tag = 'nintendo'
	`).Scan(&tagCount))
	assert.Equal(t, 1, tagCount)
}

func TestApplyScrapeResults_BulkExclusiveTitleTagLaterTargetWins(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath := filepath.ToSlash(filepath.Join("roms", "mario-alt.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 1, 1, ?);
	`, mediaPath)
	require.NoError(t, err)

	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{
		{
			MediaDBID: 1, MediaTitleDBID: 1,
			Write: &database.ScrapeWrite{
				Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				TitleTags: []database.TagInfo{{Type: "developer", Tag: "nintendo"}},
			},
		},
		{
			MediaDBID: 2, MediaTitleDBID: 1,
			Write: &database.ScrapeWrite{
				Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				TitleTags: []database.TagInfo{{Type: "developer", Tag: "capcom"}},
			},
		},
	}))

	var developerTags []string
	rows, err := mediaDB.sql.Load().QueryContext(ctx, `
		SELECT t.Tag
		FROM MediaTitleTags mtt
		JOIN Tags t ON mtt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mtt.MediaTitleDBID = 1 AND tt.Type = 'developer'
		ORDER BY t.Tag
	`)
	require.NoError(t, err)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Fatalf("failed to close rows: %v", closeErr)
		}
	}()
	for rows.Next() {
		var tag string
		require.NoError(t, rows.Scan(&tag))
		developerTags = append(developerTags, tag)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"capcom"}, developerTags)
}

func TestApplyScrapeResults_BulkAdditiveTitleTagsAccumulate(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{
		{
			MediaDBID: 1, MediaTitleDBID: 1,
			Write: &database.ScrapeWrite{
				Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				TitleTags: []database.TagInfo{{Type: "genre", Tag: "action"}},
			},
		},
		{
			MediaDBID: 1, MediaTitleDBID: 1,
			Write: &database.ScrapeWrite{
				Sentinel:  database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				TitleTags: []database.TagInfo{{Type: "genre", Tag: "platformer"}},
			},
		},
	}))

	var genreTags []string
	rows, err := mediaDB.sql.Load().QueryContext(ctx, `
		SELECT t.Tag
		FROM MediaTitleTags mtt
		JOIN Tags t ON mtt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mtt.MediaTitleDBID = 1 AND tt.Type = 'genre'
		ORDER BY t.Tag
	`)
	require.NoError(t, err)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Fatalf("failed to close rows: %v", closeErr)
		}
	}()
	for rows.Next() {
		var tag string
		require.NoError(t, rows.Scan(&tag))
		genreTags = append(genreTags, tag)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"action", "platformer"}, genreTags)
}

func TestApplyScrapeResults_BulkTitlePropertyLaterTargetWins(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaPath := filepath.ToSlash(filepath.Join("roms", "mario-alt.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 1, 1, ?);
	`, mediaPath)
	require.NoError(t, err)

	require.NoError(t, mediaDB.ApplyScrapeResults(ctx, []database.ScrapeWriteTarget{
		{
			MediaDBID: 1, MediaTitleDBID: 1,
			Write: &database.ScrapeWrite{
				Sentinel: database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				TitleProps: []database.MediaProperty{{
					TypeTag: "property:description",
					Text:    "first",
				}},
			},
		},
		{
			MediaDBID: 2, MediaTitleDBID: 1,
			Write: &database.ScrapeWrite{
				Sentinel: database.TagInfo{Type: "scraper.test", Tag: "scraped"},
				TitleProps: []database.MediaProperty{{
					TypeTag: "property:description",
					Text:    "second",
				}},
			},
		},
	}))

	var text string
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx, `
		SELECT mtp.Text
		FROM MediaTitleProperties mtp
		JOIN Tags t ON mtp.TypeTagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE mtp.MediaTitleDBID = 1 AND tt.Type = 'property' AND t.Tag = 'description'
	`).Scan(&text))
	assert.Equal(t, "second", text)
}

func TestApplyScrapeResults_RejectsNilWrite(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	err := mediaDB.ApplyScrapeResults(context.Background(), []database.ScrapeWriteTarget{
		{MediaDBID: 1, MediaTitleDBID: 1},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write is nil")
}

// --- UpsertMediaTags ---

func TestUpsertMediaTags_AdditiveType_AccumulatesTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// "scraper.test" is additive (IsExclusive=0).
	tags1 := []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}
	err := mediaDB.UpsertMediaTags(ctx, 1, tags1)
	require.NoError(t, err)

	// Insert a second different tag of the same type.
	tags2 := []database.TagInfo{{Type: "scraper.test", Tag: "extra"}}
	err = mediaDB.UpsertMediaTags(ctx, 1, tags2)
	require.NoError(t, err)

	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaTags WHERE MediaDBID = 1").Scan(&count))
	assert.Equal(t, 2, count, "additive type should keep both tags")
}

func TestUpsertMediaTags_ExclusiveType_ReplacesExisting(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// "developer" is exclusive (IsExclusive=1).
	err := mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{{Type: "developer", Tag: "nintendo"}})
	require.NoError(t, err)

	err = mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{{Type: "developer", Tag: "konami"}})
	require.NoError(t, err)

	// Only "konami" should remain.
	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaTags mt
		 JOIN Tags t ON mt.TagDBID = t.DBID
		 JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		 WHERE tt.Type = 'developer' AND mt.MediaDBID = 1`).Scan(&count))
	assert.Equal(t, 1, count, "exclusive type should have exactly one tag")

	var tagVal string
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT t.Tag FROM MediaTags mt
		 JOIN Tags t ON mt.TagDBID = t.DBID
		 JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		 WHERE tt.Type = 'developer' AND mt.MediaDBID = 1`).Scan(&tagVal))
	assert.Equal(t, tags.PadTagValue("konami"), tagVal)
}

func TestUpsertMediaTags_Idempotent(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	ti := []database.TagInfo{{Type: "scraper.test", Tag: "scraped"}}
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, ti))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, ti)) // insert same tag again

	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaTags WHERE MediaDBID = 1").Scan(&count))
	assert.Equal(t, 1, count, "duplicate additive insert should be idempotent")
}

// --- UpsertMediaTitleTags ---

func TestUpsertMediaTitleTags_ExclusiveType_Replaces(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, mediaDB.UpsertMediaTitleTags(ctx, 1, []database.TagInfo{{Type: "developer", Tag: "nintendo"}}))
	require.NoError(t, mediaDB.UpsertMediaTitleTags(ctx, 1, []database.TagInfo{{Type: "developer", Tag: "sega"}}))

	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaTitleTags mtt
		 JOIN Tags t ON mtt.TagDBID = t.DBID
		 JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		 WHERE tt.Type = 'developer' AND mtt.MediaTitleDBID = 1`).Scan(&count))
	assert.Equal(t, 1, count, "exclusive type should replace old value")
}

// --- UpsertMediaTitleProperties ---

func TestUpsertMediaTitleProperties_Insert(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	props := []database.MediaProperty{
		{TypeTag: "property:description", Text: "A plumber's adventure."},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props))

	var text string
	var blobDBID *int64
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT Text, BlobDBID FROM MediaTitleProperties WHERE MediaTitleDBID = 1").Scan(&text, &blobDBID))
	assert.Equal(t, "A plumber's adventure.", text)
	assert.Nil(t, blobDBID, "text-only property should have nil BlobDBID")
}

func TestUpsertMediaTitleProperties_Update(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	props1 := []database.MediaProperty{
		{TypeTag: "property:description", Text: "First version.", ContentType: "text/plain"},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props1))

	props2 := []database.MediaProperty{
		{TypeTag: "property:description", Text: "Updated version.", ContentType: "text/plain"},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props2))

	var text string
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT Text FROM MediaTitleProperties WHERE MediaTitleDBID = 1").Scan(&text))
	assert.Equal(t, "Updated version.", text, "second upsert should update existing row")

	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaTitleProperties WHERE MediaTitleDBID = 1").Scan(&count))
	assert.Equal(t, 1, count, "upsert must not create duplicate rows")
}

func TestUpsertMediaTitleProperties_UnknownTypeTag_ReturnsError(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	props := []database.MediaProperty{
		{TypeTag: "property:nonexistent", Text: "nope", ContentType: "text/plain"},
	}
	err := mediaDB.UpsertMediaTitleProperties(context.Background(), 1, props)
	assert.Error(t, err, "unknown type tag should return an error")
}

func TestUpsertMediaTitleProperties_RollsBackOnError(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := mediaDB.UpsertMediaTitleProperties(ctx, 1, []database.MediaProperty{
		{TypeTag: "property:description", Text: "should roll back"},
		{TypeTag: "property:missing", Text: "invalid"},
	})
	require.Error(t, err)

	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaTitleProperties WHERE MediaTitleDBID = 1").Scan(&count))
	assert.Equal(t, 0, count)
}

// --- UpsertMediaProperties ---

func TestUpsertMediaProperties_Insert(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	boxartPath := filepath.Join("roms", "nes", "mario-box.png")
	props := []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: boxartPath},
	}
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, props))

	var text string
	var blobDBID *int64
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT Text, BlobDBID FROM MediaProperties WHERE MediaDBID = 1").Scan(&text, &blobDBID))
	assert.Equal(t, boxartPath, text)
	assert.Nil(t, blobDBID, "path-only property should have nil BlobDBID")
}

func TestUpsertMediaProperties_RollsBackOnError(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := mediaDB.UpsertMediaProperties(ctx, 1, []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: filepath.Join("roms", "nes", "mario-box.png")},
		{TypeTag: "property:missing", Text: "invalid"},
	})
	require.Error(t, err)

	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaProperties WHERE MediaDBID = 1").Scan(&count))
	assert.Equal(t, 0, count)
}

// --- GetMediaTitleProperties / GetMediaProperties ---

func TestGetMediaTitleProperties_Empty(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	props, err := mediaDB.GetMediaTitleProperties(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, props)
}

func TestGetMediaTitleProperties_RoundTrip(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	artPath := filepath.Join("art", "mario.png")
	in := []database.MediaProperty{
		{TypeTag: "property:description", Text: "Hello world."},
		{TypeTag: "property:image-boxart", Text: artPath},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, in))

	got, err := mediaDB.GetMediaTitleProperties(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Fix 1: TypeTag must be populated from the JOIN, not left as "".
	for _, p := range got {
		assert.NotEmpty(t, p.TypeTag, "TypeTag must be populated (Fix 1)")
	}
}

func TestGetMediaProperties_RoundTrip(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	artPath := filepath.Join("art", "mario.png")
	in := []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: artPath},
	}
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, in))

	got, err := mediaDB.GetMediaProperties(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, artPath, got[0].Text)
	assert.Nil(t, got[0].BlobDBID, "path-only property has no blob")
	assert.Empty(t, got[0].ContentType)
	assert.Nil(t, got[0].Binary)
}

func TestGetMediaBatchMetadata_RoundTrip(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	zeldaPath := filepath.ToSlash(filepath.Join("roms", "zelda.nes"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 1, 'zelda', 'Zelda');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (2, 2, 1, ?);
	`, zeldaPath)
	require.NoError(t, err)

	nintendoTag := []database.TagInfo{{Type: "developer", Tag: "nintendo", Label: "Nintendo"}}
	capcomTag := []database.TagInfo{{Type: "developer", Tag: "capcom", Label: "Capcom"}}
	titleOneTag := []database.TagInfo{{Type: "developer", Tag: "title-one", Label: "Title One"}}
	titleTwoTag := []database.TagInfo{{Type: "developer", Tag: "title-two", Label: "Title Two"}}
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, nintendoTag))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 2, capcomTag))
	require.NoError(t, mediaDB.UpsertMediaTitleTags(ctx, 1, titleOneTag))
	require.NoError(t, mediaDB.UpsertMediaTitleTags(ctx, 2, titleTwoTag))
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, []database.MediaProperty{{
		TypeTag: "property:image-boxart",
		Text:    filepath.Join("art", "mario.png"),
	}}))
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 2, []database.MediaProperty{{
		TypeTag: "property:image-boxart",
		Text:    filepath.Join("art", "zelda.png"),
	}}))
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, []database.MediaProperty{{
		TypeTag: "property:description",
		Text:    "Mario description",
	}}))
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 2, []database.MediaProperty{{
		TypeTag: "property:description",
		Text:    "Zelda description",
	}}))

	rows, err := mediaDB.GetMediaWithTitleAndSystemByIDs(ctx, []int64{1, 2, 999})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, filepath.ToSlash(filepath.Join("roms", "mario.nes")), rows[1].Path)
	assert.Equal(t, "Mario", rows[1].Title.Name)
	assert.Equal(t, "NES", rows[1].System.SystemID)
	assert.Equal(t, zeldaPath, rows[2].Path)
	assert.Equal(t, "Zelda", rows[2].Title.Name)

	singleMediaTags, err := mediaDB.GetMediaTagsByMediaDBID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, []database.TagInfo{{Tag: "nintendo", Type: "developer", Label: "Nintendo"}}, singleMediaTags)

	singleTitleTags, err := mediaDB.GetMediaTitleTagsByMediaTitleDBID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, []database.TagInfo{{Tag: "title-one", Type: "developer", Label: "Title One"}}, singleTitleTags)

	mediaTags, err := mediaDB.GetMediaTagsByMediaDBIDs(ctx, []int64{1, 2})
	require.NoError(t, err)
	assert.Equal(t, []database.TagInfo{{Tag: "nintendo", Type: "developer", Label: "Nintendo"}}, mediaTags[1])
	assert.Equal(t, []database.TagInfo{{Tag: "capcom", Type: "developer", Label: "Capcom"}}, mediaTags[2])

	titleTags, err := mediaDB.GetMediaTitleTagsByMediaTitleDBIDs(ctx, []int64{1, 2})
	require.NoError(t, err)
	assert.Equal(t, []database.TagInfo{{Tag: "title-one", Type: "developer", Label: "Title One"}}, titleTags[1])
	assert.Equal(t, []database.TagInfo{{Tag: "title-two", Type: "developer", Label: "Title Two"}}, titleTags[2])

	mediaProps, err := mediaDB.GetMediaPropertiesByMediaDBIDs(ctx, []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, mediaProps[1], 1)
	require.Len(t, mediaProps[2], 1)
	assert.Equal(t, filepath.Join("art", "mario.png"), mediaProps[1][0].Text)
	assert.Equal(t, filepath.Join("art", "zelda.png"), mediaProps[2][0].Text)

	titleProps, err := mediaDB.GetMediaTitlePropertiesByMediaTitleDBIDs(ctx, []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, titleProps[1], 1)
	require.Len(t, titleProps[2], 1)
	assert.Equal(t, "Mario description", titleProps[1][0].Text)
	assert.Equal(t, "Zelda description", titleProps[2][0].Text)
}

func TestGetMediaBatchMetadata_EmptyIDsReturnEmptyMaps(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	mediaRows, err := mediaDB.GetMediaWithTitleAndSystemByIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, mediaRows)

	mediaTags, err := mediaDB.GetMediaTagsByMediaDBIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, mediaTags)

	titleTags, err := mediaDB.GetMediaTitleTagsByMediaTitleDBIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, titleTags)

	mediaProps, err := mediaDB.GetMediaPropertiesByMediaDBIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, mediaProps)

	titleProps, err := mediaDB.GetMediaTitlePropertiesByMediaTitleDBIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, titleProps)
}

// --- FindMediaTitlesWithoutSentinel ---

func TestFindMediaTitlesWithoutSentinel_AllUnseen(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	titles, err := mediaDB.FindMediaTitlesWithoutSentinel(context.Background(), 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.Len(t, titles, 1, "mario title has no sentinel → should be returned")
}

func TestFindMediaTitlesWithoutSentinel_MissingSentinelReturnsAllSystemTitles(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	titles, err := mediaDB.FindMediaTitlesWithoutSentinel(context.Background(), 1, "scraper.missing:scraped")
	require.NoError(t, err)
	assert.Len(t, titles, 1)
	assert.Equal(t, "mario", titles[0].Slug)
}

func TestFindMediaTitlesWithoutSentinel_AfterSentinelWritten(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Write the sentinel tag on media DBID=1.
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
		{Type: "scraper.test", Tag: "scraped"},
	}))

	titles, err := mediaDB.FindMediaTitlesWithoutSentinel(ctx, 1, "scraper.test:scraped")
	require.NoError(t, err)
	assert.Empty(t, titles, "media has sentinel → title should be excluded")
}

func TestFindMediaTitlesWithoutSentinel_UsesRequestedTagValue(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
		{Type: "scraper.test", Tag: "scraped"},
	}))

	titles, err := mediaDB.FindMediaTitlesWithoutSentinel(ctx, 1, "scraper.test:other")
	require.NoError(t, err)
	require.Len(t, titles, 1)
	assert.Equal(t, "mario", titles[0].Slug)
}

func TestFindMediaTitlesWithoutSentinel_WrongSystem(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	titles, err := mediaDB.FindMediaTitlesWithoutSentinel(context.Background(), 99, "scraper.test:scraped")
	require.NoError(t, err)
	assert.Empty(t, titles, "system 99 has no titles")
}

// --- FindMediaTitleByDBID ---

func TestFindMediaTitleByDBID_Found(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	title, err := mediaDB.FindMediaTitleByDBID(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, title)
	assert.Equal(t, "Mario", title.Name)
	assert.Equal(t, "mario", title.Slug)
}

func TestFindMediaTitleByDBID_NotFound(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	title, err := mediaDB.FindMediaTitleByDBID(context.Background(), 999)
	require.NoError(t, err)
	assert.Nil(t, title)
}

// --- FindMediaTitleBySystemAndSlug ---

func TestFindMediaTitleBySystemAndSlug_Found(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	title, err := mediaDB.FindMediaTitleBySystemAndSlug(context.Background(), 1, "mario")
	require.NoError(t, err)
	require.NotNil(t, title)
	assert.Equal(t, "Mario", title.Name)
	assert.Equal(t, "mario", title.Slug)
	assert.Equal(t, int64(1), title.SystemDBID)
}

func TestFindMediaTitleBySystemAndSlug_NotFound(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	title, err := mediaDB.FindMediaTitleBySystemAndSlug(context.Background(), 1, "missing-game")
	require.NoError(t, err)
	assert.Nil(t, title)
}

func TestFindMediaTitleBySystemAndSlug_WrongSystem(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	title, err := mediaDB.FindMediaTitleBySystemAndSlug(context.Background(), 999, "mario")
	require.NoError(t, err)
	assert.Nil(t, title)
}

// --- upsertTags exclusive-type single-call rejection ---

// TestUpsertMediaTags_ExclusiveType_MultipleDistinctInOneCall exercises the
// len(seen) > 1 guard on line 293: when a single UpsertMediaTags call
// supplies two *different* values for the same exclusive type the function
// must return an error before touching the DB.
func TestUpsertMediaTags_ExclusiveType_MultipleDistinctInOneCall(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
		{Type: "developer", Tag: "nintendo"},
		{Type: "developer", Tag: "sega"},
	})
	require.Error(t, err, "two distinct values for an exclusive type must be rejected")

	// No MediaTags rows should have been written (transaction must be rolled back).
	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaTags WHERE MediaDBID = 1`).Scan(&count))
	assert.Equal(t, 0, count, "no tags should be persisted when the call is rejected")
}

// TestUpsertMediaTags_ExclusiveType_DuplicateValueInOneCall verifies that
// duplicate entries with the same value are harmless for an exclusive type.
func TestUpsertMediaTags_ExclusiveType_DuplicateValueInOneCall(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	err := mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
		{Type: "developer", Tag: "nintendo"},
		{Type: "developer", Tag: "nintendo"},
	})
	require.NoError(t, err)

	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaTags WHERE MediaDBID = 1`).Scan(&count))
	assert.Equal(t, 1, count, "duplicate identical tags should persist once")
}

// --- Fix 2: upsertTags auto-creates missing tag types ---

func TestUpsertMediaTags_AutoCreatesTagType(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// "scraper.gamelist.xml" is not pre-seeded in the test DB.
	err := mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
		{Type: "scraper.gamelist.xml", Tag: "scraped"},
	})
	require.NoError(t, err, "upsertTags must auto-create missing tag type")

	// The TagTypes row must now exist.
	var count int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM TagTypes WHERE Type = 'scraper.gamelist.xml'`).Scan(&count))
	assert.Equal(t, 1, count, "auto-created TagTypes row must exist")

	// The sentinel tag must be reachable.
	has, err := mediaDB.MediaHasTag(ctx, 1, "scraper.gamelist.xml:scraped")
	require.NoError(t, err)
	assert.True(t, has, "sentinel tag must be retrievable after auto-creation")
}

// --- Fix 5: concurrent writes to the same tag must not error ---

func TestUpsertMediaTags_Concurrent(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	const goroutines = 5
	errs := make(chan error, goroutines)
	for range goroutines {
		go func() {
			errs <- mediaDB.UpsertMediaTags(ctx, 1, []database.TagInfo{
				{Type: "scraper.test", Tag: "concurrent"},
			})
		}()
	}
	for range goroutines {
		require.NoError(t, <-errs, "concurrent tag write must not return an error")
	}

	// Exactly one Tags row and one MediaTags link should exist.
	var tagCount int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM Tags t
		 JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		 WHERE tt.Type = 'scraper.test' AND t.Tag LIKE '%concurrent%'`).Scan(&tagCount))
	assert.Equal(t, 1, tagCount, "concurrent writes must produce exactly one Tags row")

	var mediaTagCount int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaTags mt
		 JOIN Tags t ON mt.TagDBID = t.DBID
		 JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		 WHERE tt.Type = 'scraper.test' AND t.Tag LIKE '%concurrent%'`).Scan(&mediaTagCount))
	assert.Equal(t, 1, mediaTagCount, "concurrent writes must produce exactly one MediaTags link")
}

// --- Blob round-trip via property upserts ---

func TestUpsertMediaTitleProperties_WithBlob(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	blobDBID, err := mediaDB.UpsertMediaBlob(ctx, "image/png", data)
	require.NoError(t, err)
	require.Positive(t, blobDBID)

	props := []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: "", BlobDBID: &blobDBID},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props))

	got, err := mediaDB.GetMediaTitleProperties(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].BlobDBID, "BlobDBID must be set after upsert with blob")
	assert.Equal(t, blobDBID, *got[0].BlobDBID)
	assert.Equal(t, "image/png", got[0].ContentType)
	assert.Equal(t, data, got[0].Binary)
}

func TestGetMediaTitlePropertyMetadata_WithBlobOmitsBinary(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte{0x89, 0x50, 0x4E, 0x47}
	blobDBID, err := mediaDB.UpsertMediaBlob(ctx, "image/png", data)
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, []database.MediaProperty{
		{TypeTag: "property:image-boxart", BlobDBID: &blobDBID},
	}))

	got, err := mediaDB.GetMediaTitlePropertyMetadata(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].BlobDBID)
	assert.Equal(t, blobDBID, *got[0].BlobDBID)
	assert.Equal(t, int64(len(data)), got[0].BlobSize)
	assert.Equal(t, "image/png", got[0].ContentType)
	assert.Nil(t, got[0].Binary)
}

func TestGetMediaPropertyMetadata_WithBlobOmitsBinary(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte{0x89, 0x50, 0x4E, 0x47}
	blobDBID, err := mediaDB.UpsertMediaBlob(ctx, "image/png", data)
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, []database.MediaProperty{
		{TypeTag: "property:image-boxart", BlobDBID: &blobDBID},
	}))

	got, err := mediaDB.GetMediaPropertyMetadata(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].BlobDBID)
	assert.Equal(t, blobDBID, *got[0].BlobDBID)
	assert.Equal(t, int64(len(data)), got[0].BlobSize)
	assert.Equal(t, "image/png", got[0].ContentType)
	assert.Nil(t, got[0].Binary)
}

func TestGetMediaPropertyMetadataGrouped_WithBlobOmitsBinary(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte{0x89, 0x50, 0x4E, 0x47}
	blobDBID, err := mediaDB.UpsertMediaBlob(ctx, "image/png", data)
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, []database.MediaProperty{
		{TypeTag: "property:image-boxart", BlobDBID: &blobDBID},
	}))

	got, err := mediaDB.GetMediaPropertyMetadataByMediaDBIDs(ctx, []int64{1})
	require.NoError(t, err)
	require.Len(t, got[1], 1)
	require.NotNil(t, got[1][0].BlobDBID)
	assert.Equal(t, blobDBID, *got[1][0].BlobDBID)
	assert.Equal(t, int64(len(data)), got[1][0].BlobSize)
	assert.Equal(t, "image/png", got[1][0].ContentType)
	assert.Nil(t, got[1][0].Binary)
}

func TestGetMediaTitlePropertyMetadataGrouped_WithBlobOmitsBinary(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte{0x89, 0x50, 0x4E, 0x47}
	blobDBID, err := mediaDB.UpsertMediaBlob(ctx, "image/png", data)
	require.NoError(t, err)
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, []database.MediaProperty{
		{TypeTag: "property:image-boxart", BlobDBID: &blobDBID},
	}))

	got, err := mediaDB.GetMediaTitlePropertyMetadataByMediaTitleDBIDs(ctx, []int64{1})
	require.NoError(t, err)
	require.Len(t, got[1], 1)
	require.NotNil(t, got[1][0].BlobDBID)
	assert.Equal(t, blobDBID, *got[1][0].BlobDBID)
	assert.Equal(t, int64(len(data)), got[1][0].BlobSize)
	assert.Equal(t, "image/png", got[1][0].ContentType)
	assert.Nil(t, got[1][0].Binary)
}

func TestUpsertMediaProperties_WithBlob(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte("fake jpeg bytes")
	blobDBID, err := mediaDB.UpsertMediaBlob(ctx, "image/jpeg", data)
	require.NoError(t, err)
	require.Positive(t, blobDBID)

	props := []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: "", BlobDBID: &blobDBID},
	}
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, props))

	got, err := mediaDB.GetMediaProperties(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].BlobDBID, "BlobDBID must be set after upsert with blob")
	assert.Equal(t, blobDBID, *got[0].BlobDBID)
	assert.Equal(t, "image/jpeg", got[0].ContentType)
	assert.Equal(t, int64(len(data)), got[0].BlobSize)
	assert.Equal(t, data, got[0].Binary)
}

func TestGetMediaProperties_OversizedBlobOmitsBinary(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	blobDBID, err := mediaDB.UpsertMediaBlob(ctx, "image/png", []byte("small"))
	require.NoError(t, err)
	var largeSize int64 = database.MaxMediaPropertyBinaryBytes + 1
	res, err := mediaDB.sql.Load().ExecContext(ctx,
		`UPDATE MediaBlobs SET Data = zeroblob(?) WHERE DBID = ?`, largeSize, blobDBID)
	require.NoError(t, err)
	rowsAffected, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), rowsAffected)
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, []database.MediaProperty{
		{TypeTag: "property:image-boxart", BlobDBID: &blobDBID},
	}))

	got, err := mediaDB.GetMediaProperties(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].BlobDBID)
	assert.Equal(t, blobDBID, *got[0].BlobDBID)
	assert.Equal(t, largeSize, got[0].BlobSize)
	assert.Nil(t, got[0].Binary)
}

func TestGetMediaTitleProperties_NoBlobIsNilBlobDBID(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	props := []database.MediaProperty{
		{TypeTag: "property:description", Text: "No binary here."},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props))

	got, err := mediaDB.GetMediaTitleProperties(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Nil(t, got[0].BlobDBID)
	assert.Empty(t, got[0].ContentType)
	assert.Nil(t, got[0].Binary)
}

// --- ResolveSingletonContainerAliases ---

// setupAliasTestDB returns a MediaDB seeded with:
//   - Systems: NES (1), PSX (2)
//   - TagTypes: "favorite" (additive, 10)
//   - Tags: "favorite:true" (10)
//
// Callers insert their own MediaTitles and Media.
func setupAliasTestDB(t *testing.T) (mediaDB *MediaDB, cleanup func()) {
	t.Helper()
	mediaDB, cleanup = setupTempMediaDB(t)
	ctx := context.Background()
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES
			(1, 'NES', 'Nintendo'),
			(2, 'PSX', 'PlayStation');
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES
			(10, 'favorite', 0);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES
			(10, 10, 'true');
	`)
	require.NoError(t, err)
	return mediaDB, cleanup
}

func aliasTestDir(parent string, parts ...string) string {
	return filepath.ToSlash(filepath.Join(append([]string{parent}, parts...)...)) + "/"
}

func TestResolveSingletonContainerAliases_SingleFileIsAliased(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))
	gameDir := aliasTestDir(parent, "Game")
	gamePath := filepath.ToSlash(filepath.Join(parent, "Game", "Game.chd"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 2, 'game', 'Game');
		INSERT INTO Media (
			DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName
		) VALUES (1, 1, 2, ?, ?, 'Game (Disc 1)');
	`, gamePath, gameDir)
	require.NoError(t, err)

	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: gameDir, FileCount: 1},
	})
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, gameDir, aliases[0].ChildDir)
	assert.Equal(t, int64(1), aliases[0].Row.DBID)
	assert.Equal(t, "Game", aliases[0].Row.Title.Name)
	assert.Equal(t, "Game (Disc 1)", aliases[0].Row.SortName)
	assert.Equal(t, "PSX", aliases[0].Row.System.SystemID)
	assert.Empty(t, aliases[0].ZapScriptTags)
}

func TestResolveSingletonContainerAliases_CueBinIsAliasedToCue(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))
	gameDir := aliasTestDir(parent, "Disc")
	cuePath := filepath.ToSlash(filepath.Join(parent, "Disc", "Game.cue"))
	binPath := filepath.ToSlash(filepath.Join(parent, "Disc", "Game.bin"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(1, 2, 'game-cue', 'Game'),
			(2, 2, 'game-bin', 'Game Bin');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(1, 1, 2, ?, ?),
			(2, 2, 2, ?, ?);
	`, cuePath, gameDir, binPath, gameDir)
	require.NoError(t, err)

	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: gameDir, FileCount: 2},
	})
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, gameDir, aliases[0].ChildDir)
	assert.Equal(t, cuePath, aliases[0].Row.Path)
}

func TestResolveSingletonContainerAliases_NestedSubdirIsNotAliased(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "NES"))
	collDir := aliasTestDir(parent, "Collection")
	subDir := aliasTestDir(parent, "Collection", "Sub")
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'game', 'Game');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(1, 1, 1, ?, ?);
	`,
		filepath.ToSlash(filepath.Join(parent, "Collection", "Sub", "game.nes")), subDir)
	require.NoError(t, err)

	// Collection's recursive FileCount is 1 but it has no direct media rows —
	// the count mismatch marks it as nested and it must not be aliased.
	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 1, []database.SingletonAliasCandidate{
		{ChildDir: collDir, FileCount: 1},
	})
	require.NoError(t, err)
	assert.Empty(t, aliases)
}

func TestResolveSingletonContainerAliases_M3UAliasedForMultiDisc(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))
	gameDir := aliasTestDir(parent, "MultiDisc")
	m3uPath := filepath.ToSlash(filepath.Join(parent, "MultiDisc", "Game.m3u"))
	cuePath := filepath.ToSlash(filepath.Join(parent, "MultiDisc", "Disc1.cue"))
	binPath := filepath.ToSlash(filepath.Join(parent, "MultiDisc", "Disc1.bin"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(1, 2, 'game-m3u', 'Game'),
			(2, 2, 'disc1-cue', 'Disc1 Cue'),
			(3, 2, 'disc1-bin', 'Disc1 Bin');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(1, 1, 2, ?, ?),
			(2, 2, 2, ?, ?),
			(3, 3, 2, ?, ?);
	`, m3uPath, gameDir, cuePath, gameDir, binPath, gameDir)
	require.NoError(t, err)

	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: gameDir, FileCount: 3},
	})
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, m3uPath, aliases[0].Row.Path)
}

func TestResolveSingletonContainerAliases_TagsAttachedOnAlias(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))
	gameDir := aliasTestDir(parent, "Tagged")
	gamePath := filepath.ToSlash(filepath.Join(parent, "Tagged", "game.chd"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 2, 'tagged-game', 'Tagged Game');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES (1, 1, 2, ?, ?);
		INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES (1, 10);
	`, gamePath, gameDir)
	require.NoError(t, err)

	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: gameDir, FileCount: 1},
	})
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	require.Len(t, aliases[0].Tags, 1)
	assert.Equal(t, "true", aliases[0].Tags[0].Tag)
	assert.Equal(t, "favorite", aliases[0].Tags[0].Type)
}

// TestResolveSingletonContainerAliases_DisambiguatingTagsAttached verifies that a
// singleton container alias whose title has sibling variants gets its disambiguating
// ZapScriptTags populated. The aliased USA disc lives in its own directory while the
// Japan variant of the same title lives elsewhere; the title therefore disambiguates
// on "release" and the alias must surface release=USA.
func TestResolveSingletonContainerAliases_DisambiguatingTagsAttached(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	usaPath := filepath.ToSlash(filepath.Join("roms", "PSX", "USA Disc", "game.chd"))
	jpnPath := filepath.ToSlash(filepath.Join("roms", "PSX", "Game (Japan).chd"))
	systemDBID, _, mediaIDs := setupDisambTitle(t, mediaDB, "PSX", "Game", []disambTitleMedia{
		{path: usaPath, tags: map[string]string{"release": "USA"}},
		{path: jpnPath, tags: map[string]string{"release": "Japan"}},
	})
	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))

	aliasDir := ParentDirForMediaPath(usaPath)
	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, systemDBID, []database.SingletonAliasCandidate{
		{ChildDir: aliasDir, FileCount: 1},
	})
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, mediaIDs[0], aliases[0].Row.DBID)
	require.Len(t, aliases[0].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "release", Tag: "USA"}, aliases[0].ZapScriptTags[0])
}

func TestResolveSingletonContainerAliases_MultipleDirsInOneScan(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))
	gameADir := aliasTestDir(parent, "GameA")
	gameBDir := aliasTestDir(parent, "GameB")
	gameAPath := filepath.ToSlash(filepath.Join(parent, "GameA", "GameA.chd"))
	gameBPath := filepath.ToSlash(filepath.Join(parent, "GameB", "GameB.chd"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(1, 2, 'game-a', 'Game A'),
			(2, 2, 'game-b', 'Game B');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(1, 1, 2, ?, ?),
			(2, 2, 2, ?, ?);
	`, gameAPath, gameADir, gameBPath, gameBDir)
	require.NoError(t, err)

	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: gameADir, FileCount: 1},
		{ChildDir: gameBDir, FileCount: 1},
	})
	require.NoError(t, err)
	require.Len(t, aliases, 2)

	byDir := make(map[string]database.SingletonContainerAlias, 2)
	for _, a := range aliases {
		byDir[a.ChildDir] = a
	}
	if a, ok := byDir[gameADir]; assert.True(t, ok, "missing GameA alias") {
		assert.Equal(t, gameAPath, a.Row.Path)
	}
	if b, ok := byDir[gameBDir]; assert.True(t, ok, "missing GameB alias") {
		assert.Equal(t, gameBPath, b.Row.Path)
	}

	// A dir not passed as a candidate must not be resolved even though its
	// media rows exist in the table.
	aliases, err = mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: gameADir, FileCount: 1},
	})
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, gameADir, aliases[0].ChildDir)
}

func TestResolveSingletonContainerAliases_CountMismatchSkipsDir(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))
	gameDir := aliasTestDir(parent, "Game")
	subDir := aliasTestDir(parent, "Game", "Extras")
	gamePath := filepath.ToSlash(filepath.Join(parent, "Game", "Game.chd"))
	extraPath := filepath.ToSlash(filepath.Join(parent, "Game", "Extras", "bonus.chd"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(1, 2, 'game', 'Game'),
			(2, 2, 'bonus', 'Bonus');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(1, 1, 2, ?, ?),
			(2, 2, 2, ?, ?);
	`, gamePath, gameDir, extraPath, subDir)
	require.NoError(t, err)

	// Game has one direct row but a recursive FileCount of 2 (the nested
	// Extras file), so it must be skipped.
	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: gameDir, FileCount: 2},
	})
	require.NoError(t, err)
	assert.Empty(t, aliases)
}

func TestResolveSingletonContainerAliases_RootPrefixIsHandled(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	gameDir := aliasTestDir("", "Game")
	gamePath := filepath.ToSlash(filepath.Join("Game", "Game.chd"))
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 2, 'game', 'Game');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES (1, 1, 2, ?, ?);
	`, gamePath, gameDir)
	require.NoError(t, err)

	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: gameDir, FileCount: 1},
	})
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, gameDir, aliases[0].ChildDir)
	assert.Equal(t, gamePath, aliases[0].Row.Path)
	assert.Equal(t, int64(1), aliases[0].Row.DBID)
}

func TestResolveSingletonContainerAliases_NoCandidatesReturnsNil(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()

	aliases, err := mediaDB.ResolveSingletonContainerAliases(context.Background(), 2, nil)
	require.NoError(t, err)
	assert.Empty(t, aliases)
}

// TestResolveSingletonContainerAliases_HasCoverSet verifies that HasCover is
// true when the aliased media's title has a scraped image property, and false
// when it does not.
func TestResolveSingletonContainerAliases_HasCoverSet(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Seed the minimal tag rows needed by fetchAndAttachCoverFlags.
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT OR IGNORE INTO TagTypes (DBID, Type, IsExclusive) VALUES (900, 'property', 0);
		INSERT OR IGNORE INTO Tags    (DBID, TypeDBID, Tag)      VALUES (901, 900, 'image-boxart');
	`)
	require.NoError(t, err)

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))

	// gameWithCover — media-level image property inserted below.
	coverDir := aliasTestDir(parent, "WithCover")
	coverPath := filepath.ToSlash(filepath.Join(parent, "WithCover", "cover.chd"))
	// gameNoCover — no property.
	noCoverDir := aliasTestDir(parent, "NoCover")
	noCoverPath := filepath.ToSlash(filepath.Join(parent, "NoCover", "nocov.chd"))

	_, err = mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(1, 2, 'with-cover', 'With Cover'),
			(2, 2, 'no-cover',   'No Cover');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir) VALUES
			(1, 1, 2, ?, ?),
			(2, 2, 2, ?, ?);
	`, coverPath, coverDir, noCoverPath, noCoverDir)
	require.NoError(t, err)

	// MediaProperties row for media DBID=1.
	_, err = mediaDB.sql.Load().ExecContext(ctx,
		`INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text) VALUES (1, 901, 'cover.jpg')`)
	require.NoError(t, err)

	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: coverDir, FileCount: 1},
		{ChildDir: noCoverDir, FileCount: 1},
	})
	require.NoError(t, err)
	require.Len(t, aliases, 2)

	byDir := make(map[string]database.SingletonContainerAlias, 2)
	for _, a := range aliases {
		byDir[a.ChildDir] = a
	}
	assert.True(t, byDir[coverDir].HasCover, "aliased dir with image property should have HasCover=true")
	assert.False(t, byDir[noCoverDir].HasCover, "aliased dir without image property should have HasCover=false")
}
