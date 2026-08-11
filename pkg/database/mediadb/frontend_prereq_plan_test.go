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
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

func TestFrontendPrerequisiteQueryPlansUseExistingIndexes(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	rootDir := seedRandomGameBenchmark(t, mediaDB, 1_000)
	folderDir := mediaRecursivePathPrefix(filepath.ToSlash(filepath.Join(rootDir, "folder-000")))
	favorite := []zapscript.TagFilter{{Type: "user", Value: "favorite"}}

	t.Run("untagged system counts stream covering index", func(t *testing.T) {
		plan := randomQueryPlan(t, mediaDB.sql.Load(), `
			SELECT Media.SystemDBID, COUNT(*)
			FROM Media INDEXED BY media_system_present_path_idx
			WHERE Media.IsMissing = 0
			GROUP BY Media.SystemDBID`)
		assertRandomPlanContains(t, plan, "media_system_present_path_idx")
	})

	t.Run("cached system counts use root and system indexes", func(t *testing.T) {
		plan := randomQueryPlan(t, mediaDB.sql.Load(), `
			SELECT Systems.SystemID, SUM(BrowseDirCounts.FileCount)
			FROM BrowseDirs
			INNER JOIN BrowseDirCounts ON BrowseDirCounts.ParentDirDBID = BrowseDirs.DBID
			INNER JOIN Systems ON Systems.DBID = BrowseDirCounts.SystemDBID
			WHERE BrowseDirs.Path = '/'
			GROUP BY BrowseDirCounts.SystemDBID, Systems.SystemID`)
		assertRandomPlanContains(t, plan, "sqlite_autoindex_BrowseDirs_1")
		assertRandomPlanContains(t, plan, "idx_browsedircounts_parent_system")
	})

	t.Run("tagged system facets use reverse tag indexes", func(t *testing.T) {
		tagClauses, args := BuildTagFilterSQL(favorite)
		plan := randomQueryPlan(t, mediaDB.sql.Load(), `
			SELECT Systems.SystemID, matched.Count
			FROM (
				SELECT Media.SystemDBID, COUNT(*) AS Count
				FROM Media
				WHERE Media.IsMissing = 0 AND `+strings.Join(tagClauses, " AND ")+`
				GROUP BY Media.SystemDBID
			) matched
			INNER JOIN Systems ON Systems.DBID = matched.SystemDBID`, args...)
		assertRandomPlanContains(t, plan, "mediatags_tag_media_idx")
		assertRandomPlanContains(t, plan, "mediatitletags_tag_idx")
	})

	t.Run("negative system facets subtract reverse-index exclusions", func(t *testing.T) {
		query, args := buildNegativeOnlySystemMediaCountsQuery([]zapscript.TagFilter{{
			Type: "user", Value: "favorite", Operator: zapscript.TagOperatorNOT,
		}})
		plan := randomQueryPlan(t, mediaDB.sql.Load(), query, args...)
		assertRandomPlanContains(t, plan, "media_system_present_path_idx")
		assertRandomPlanContains(t, plan, "mediatags_tag_media_idx")
		assertRandomPlanContains(t, plan, "mediatitletags_tag_idx")
	})

	t.Run("search path prefix uses media path index", func(t *testing.T) {
		pathClause, args := browsePathPrefixCondition("Media.Path", folderDir)
		plan := randomQueryPlan(t, mediaDB.sql.Load(), `
			SELECT Media.DBID
			FROM Media
			WHERE Media.IsMissing = 0 AND `+pathClause+`
			ORDER BY Media.Path`, args...)
		assertRandomPlanContains(t, plan, "media_path_idx")
	})

	t.Run("favorite browse uses reverse tag indexes", func(t *testing.T) {
		opts := &database.BrowseFilesOptions{
			PathPrefix: folderDir,
			Sort:       "name",
			Tags:       favorite,
			Limit:      100,
		}
		whereClause, args := browseFilesBaseCondition(opts)
		args = append(args, opts.Limit)
		plan := randomQueryPlan(t, mediaDB.sql.Load(), `
			SELECT m.DBID
			FROM Media m
			WHERE `+whereClause+`
			ORDER BY m.SortName ASC, m.DBID ASC
			LIMIT ?`, args...)
		assertRandomPlanContains(t, plan, "mediatags_tag_media_idx")
		assertRandomPlanContains(t, plan, "mediatitletags_tag_idx")
	})

	t.Run("sorted favorite search uses reverse tag indexes", func(t *testing.T) {
		tagClauses, tagArgs := BuildTagFilterSQL(favorite)
		args := append([]any{"NES"}, tagArgs...)
		args = append(args, 100)
		plan := randomQueryPlan(t, mediaDB.sql.Load(), `
			SELECT Systems.SystemID, MediaTitles.Name, Media.Path, Media.DBID
			FROM Systems
			INNER JOIN MediaTitles ON Systems.DBID = MediaTitles.SystemDBID
			INNER JOIN Media ON MediaTitles.DBID = Media.MediaTitleDBID
			WHERE Systems.SystemID = ?
			AND Media.IsMissing = 0
			AND `+strings.Join(tagClauses, " AND ")+`
			ORDER BY MediaTitles.Name COLLATE NOCASE ASC, Media.DBID ASC
			LIMIT ?`, args...)
		assertRandomPlanContains(t, plan, "mediatags_tag_media_idx")
		assertRandomPlanContains(t, plan, "mediatitletags_tag_idx")
	})
}
