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
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/require"
)

func BenchmarkRandomGame_Query_1k(b *testing.B) {
	benchmarkRandomGameQuery(b, 1_000)
}

func BenchmarkRandomGame_Query_50k(b *testing.B) {
	benchmarkRandomGameQuery(b, 50_000)
}

func BenchmarkRandomGame_Query_500k(b *testing.B) {
	benchmarkRandomGameQuery(b, 500_000)
}

func benchmarkRandomGameQuery(b *testing.B, rows int) {
	b.Helper()
	ctx := context.Background()
	mediaDB, cleanup := setupBrowseBenchMediaDB(b)
	defer cleanup()
	rootDir := seedRandomGameBenchmark(b, mediaDB, rows)
	seedRandomGameBrowseCounts(b, mediaDB, rows)
	broadSystems := []string{
		"3DO", "AdventureVision", "AmigaCD32", "Arcadia", "Atari2600", "Atari5200", "Atari7800", "Astrocade",
		"CasioPV1000", "CDI", "ChannelF", "ColecoVision", "FDS", "Genesis", "Sega32X", "Intellivision",
		"Jaguar", "JaguarCD", "Odyssey2", "MasterSystem", "NeoGeo", "NeoGeoCD", "NES", "Nintendo64", "PSX",
		"Saturn", "MegaCD", "SG1000", "SNES", "SuperGameboy", "SuperGrafx", "TurboGrafx16", "TurboGrafx16CD",
		"VC4000", "Vectrex", "VirtualBoy", "CreatiVision",
	}

	queries := []struct {
		name  string
		query database.MediaQuery
		cold  bool
	}{
		{name: "all", query: database.MediaQuery{Systems: []string{"NES"}}},
		{name: "all-cold", cold: true, query: database.MediaQuery{Systems: []string{"NES"}}},
		{name: "broad-systems-cold", cold: true, query: database.MediaQuery{Systems: broadSystems}},
		{name: "favorite-sparse", query: database.MediaQuery{
			Systems: []string{"NES"},
			Tags:    []zapscript.TagFilter{{Type: "user", Value: "favorite"}},
		}},
		{name: "favorite-sparse-cold", cold: true, query: database.MediaQuery{
			Systems: []string{"NES"},
			Tags:    []zapscript.TagFilter{{Type: "user", Value: "favorite"}},
		}},
		{name: "action-dense", query: database.MediaQuery{
			Systems: []string{"NES"},
			Tags:    []zapscript.TagFilter{{Type: "genre", Value: "action"}},
		}},
		{name: "not-favorite-dense", query: database.MediaQuery{
			Systems: []string{"NES"},
			Tags: []zapscript.TagFilter{{
				Type:     "user",
				Value:    "favorite",
				Operator: zapscript.TagOperatorNOT,
			}},
		}},
		{name: "recursive-path", query: database.MediaQuery{PathPrefix: rootDir}},
	}

	for i := range queries {
		testCase := &queries[i]
		b.Run(testCase.name, func(b *testing.B) {
			b.ReportAllocs()
			_, err := mediaDB.RandomGameWithQuery(ctx, &testCase.query)
			require.NoError(b, err)

			b.ResetTimer()
			for b.Loop() {
				if testCase.cold {
					if err = mediaDB.InvalidateCountCache(); err != nil {
						b.Fatal(err)
					}
				}
				_, err = mediaDB.RandomGameWithQuery(ctx, &testCase.query)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func seedRandomGameBenchmark(b testing.TB, mediaDB *MediaDB, rows int) string {
	b.Helper()
	ctx := context.Background()
	rootDir := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "random-bench"))

	tx, err := mediaDB.sql.Load().BeginTx(ctx, nil)
	require.NoError(b, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'NES');
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES
			(1, 'user', 0),
			(2, 'genre', 0);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES
			(1, 1, 'favorite'),
			(2, 2, 'action');
	`)
	require.NoError(b, err)

	titleStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, 1, ?, ?)`)
	require.NoError(b, err)
	defer func() { require.NoError(b, titleStmt.Close()) }()
	mediaStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName)
		VALUES (?, ?, 1, ?, ?, ?)
	`)
	require.NoError(b, err)
	defer func() { require.NoError(b, mediaStmt.Close()) }()
	tagStmt, err := tx.PrepareContext(ctx, `INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`)
	require.NoError(b, err)
	defer func() { require.NoError(b, tagStmt.Close()) }()

	for i := 1; i <= rows; i++ {
		mediaID := int64(i)
		name := fmt.Sprintf("Random Game %06d", i)
		folderNumber := (i - 1) / 100 % 100
		folder := mediaRecursivePathPrefix(filepath.ToSlash(filepath.Join(
			rootDir, fmt.Sprintf("folder-%03d", folderNumber))))
		mediaPath := filepath.ToSlash(filepath.Join(folder, fmt.Sprintf("game-%06d.rom", i)))
		_, err = titleStmt.ExecContext(ctx, mediaID, fmt.Sprintf("random-game-%06d", i), name)
		require.NoError(b, err)
		_, err = mediaStmt.ExecContext(ctx, mediaID, mediaID, mediaPath, folder, name)
		require.NoError(b, err)
		if i <= 5 || i%10_007 == 0 {
			_, err = tagStmt.ExecContext(ctx, mediaID, int64(1))
			require.NoError(b, err)
		}
		if i%5 != 0 {
			_, err = tagStmt.ExecContext(ctx, mediaID, int64(2))
			require.NoError(b, err)
		}
	}

	require.NoError(b, tx.Commit())
	_, err = mediaDB.sql.Load().ExecContext(ctx, "ANALYZE")
	require.NoError(b, err)
	return rootDir
}

func seedRandomGameBrowseCounts(b testing.TB, mediaDB *MediaDB, rows int) {
	b.Helper()
	ctx := context.Background()
	db := mediaDB.sql.Load()

	_, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO BrowseDirs (Path, Name, IsVirtual) VALUES ('/', '/', 0)`)
	require.NoError(b, err)
	var rootDirDBID int64
	require.NoError(b, db.QueryRowContext(ctx,
		"SELECT DBID FROM BrowseDirs WHERE Path = '/'",
	).Scan(&rootDirDBID))
	_, err = db.ExecContext(ctx, `
		INSERT OR REPLACE INTO BrowseDirCounts
			(ParentDirDBID, ChildDirDBID, SystemDBID, FileCount)
		VALUES (?, ?, 1, ?)`, rootDirDBID, rootDirDBID, rows)
	require.NoError(b, err)
	_, err = db.ExecContext(ctx, `
		INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)`,
		DBConfigBrowseIndexVersion, browseCacheSchemaVersion)
	require.NoError(b, err)
}

func TestRandomGameQueryPlansUseExistingIndexes(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	rootDir := seedRandomGameBenchmark(t, mediaDB, 1_000)

	pathWhere, pathArgs := buildMediaQueryWhereClause(&database.MediaQuery{PathPrefix: rootDir})
	pathPlan := randomQueryPlan(t, mediaDB.sql.Load(),
		"SELECT COUNT(*), MIN(Media.DBID), MAX(Media.DBID) FROM Media "+pathWhere, pathArgs...)
	assertRandomPlanContains(t, pathPlan, "path_idx")

	broadSystems := &database.MediaQuery{Systems: []string{
		"NES", "SNES", "Genesis", "Saturn", "PSX", "Nintendo64", "Atari2600", "AmigaCD32",
	}}
	systemWhere, systemArgs := buildMediaQueryWhereClause(broadSystems)
	systemPlan := randomQueryPlan(t, mediaDB.sql.Load(),
		"SELECT COUNT(*), MIN(Media.DBID), MAX(Media.DBID) FROM Media "+systemWhere, systemArgs...)
	assertRandomPlanContains(t, systemPlan, "media_system_present_path_idx")

	tagQuery := &database.MediaQuery{
		Systems: []string{"NES"},
		Tags:    []zapscript.TagFilter{{Type: "user", Value: "favorite"}},
	}
	tagWhere, tagArgs := buildMediaQueryWhereClause(tagQuery)
	tagPlan := randomQueryPlan(t, mediaDB.sql.Load(), `
		SELECT COUNT(*), MIN(Media.DBID), MAX(Media.DBID)
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
	`+tagWhere, tagArgs...)
	assertRandomPlanContains(t, tagPlan, "mediatags_tag_media_idx")
	assertRandomPlanContains(t, tagPlan, "mediatitletags_tag_idx")

	candidateWhere, candidateArgs := buildMediaCandidateQueryWhereClause(tagQuery)
	candidateArgs = append(candidateArgs, int64(100))
	candidatePlan := randomQueryPlan(t, mediaDB.sql.Load(), `
		SELECT Systems.SystemID, Media.Path, Media.DBID
		FROM Media
		INNER JOIN MediaTitles ON MediaTitles.DBID = Media.MediaTitleDBID
		INNER JOIN Systems ON Systems.DBID = MediaTitles.SystemDBID
	`+candidateWhere+" AND Media.DBID = ? LIMIT 1", candidateArgs...)
	assertRandomPlanContains(t, candidatePlan, "SEARCH Media USING INTEGER PRIMARY KEY")
}

func randomQueryPlan(t testing.TB, db sqlQueryable, query string, args ...any) string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var details []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		details = append(details, detail)
	}
	require.NoError(t, rows.Err())
	plan := strings.Join(details, "\n")
	t.Logf("query plan:\n%s", plan)
	return plan
}

func assertRandomPlanContains(t testing.TB, plan, index string) {
	t.Helper()
	require.Contains(t, strings.ToLower(plan), strings.ToLower(index))
}
