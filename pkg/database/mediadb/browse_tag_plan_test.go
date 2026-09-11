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
	"testing"

	zapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tagPlanBigFiles   = 200
	tagPlanSmallFiles = 5
	tagPlanRareFiles  = 3
)

// tagPlanFixture is one big directory and one small one under the same system,
// with a tag on nearly every file and a tag on a handful, which is the pair the
// plan has to choose between.
type tagPlanFixture struct {
	mediaDB  *MediaDB
	bigDir   string
	smallDir string
	system   systemdefs.System
}

func setupTagPlanFixture(t *testing.T) (fixture *tagPlanFixture, cleanup func()) {
	t.Helper()
	mediaDB, cleanup := setupTempMediaDB(t)

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	bigDir := browseTestDir("big", "NES")
	smallDir := browseTestDir("small", "NES")

	require.NoError(t, mediaDB.BeginTransaction(false))
	regionType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)
	common, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: regionType.DBID, Tag: "common"})
	require.NoError(t, err)
	rare, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: regionType.DBID, Tag: "rare"})
	require.NoError(t, err)

	insert := func(dir, name, file string, tagDBIDs ...int64) {
		t.Helper()
		title, titleErr := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: system.DBID,
			Slug:       slugs.Slugify(nesSystem.GetMediaType(), name+dir+file),
			Name:       name,
		})
		require.NoError(t, titleErr)
		row, mediaErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     system.DBID,
			MediaTitleDBID: title.DBID,
			Path:           dir + file,
			ParentDir:      dir,
			SortName:       name,
		})
		require.NoError(t, mediaErr)
		for _, tagDBID := range tagDBIDs {
			_, tagErr := mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: row.DBID, TagDBID: tagDBID})
			require.NoError(t, tagErr)
		}
	}

	for i := range tagPlanBigFiles {
		name := fmt.Sprintf("Big %03d", i)
		tagIDs := []int64{common.DBID}
		if i < tagPlanRareFiles {
			tagIDs = append(tagIDs, rare.DBID)
		}
		insert(bigDir, name, fmt.Sprintf("big-%03d.nes", i), tagIDs...)
	}
	for i := range tagPlanSmallFiles {
		insert(smallDir, fmt.Sprintf("Small %02d", i), fmt.Sprintf("small-%02d.nes", i), common.DBID)
	}
	require.NoError(t, mediaDB.CommitTransaction())
	require.NoError(t, sqlPopulateBrowseCache(context.Background(), mediaDB.sql.Load()))

	return &tagPlanFixture{
		mediaDB: mediaDB, system: *nesSystem, bigDir: bigDir, smallDir: smallDir,
	}, cleanup
}

// TestBrowseTagPlan_PicksTheSmallerSide pins the rule: resolve a tag from the
// tag side only when it carries fewer rows than the browse would read.
func TestBrowseTagPlan_PicksTheSmallerSide(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f, cleanup := setupTagPlanFixture(t)
	defer cleanup()
	db := f.mediaDB.sql.Load()
	systems := []systemdefs.System{f.system}
	rare := []zapscript.TagFilter{{Type: "region", Value: "rare"}}
	common := []zapscript.TagFilter{{Type: "region", Value: "common"}}

	bigScope, known, err := sqlBrowseScopeRows(ctx, db, []string{f.bigDir}, systems)
	require.NoError(t, err)
	require.True(t, known)
	assert.Equal(t, tagPlanBigFiles, bigScope)

	plan := sqlBrowseTagPlan(ctx, db, rare, bigScope, true)
	require.NotNil(t, plan.driveFromTag, "3 rows against a 200-file directory must drive from the tag")
	assert.Equal(t, "rare", plan.driveFromTag.Value)

	plan = sqlBrowseTagPlan(ctx, db, common, bigScope, true)
	assert.Nil(t, plan.driveFromTag,
		"a tag on every row of the directory is not the smaller side")

	smallScope, known, err := sqlBrowseScopeRows(ctx, db, []string{f.smallDir}, systems)
	require.NoError(t, err)
	require.True(t, known)
	plan = sqlBrowseTagPlan(ctx, db, common, smallScope, true)
	assert.Nil(t, plan.driveFromTag,
		"a common tag against a small directory must stay on candidate probes, which is "+
			"the case that gets slower when driven from the tag side")

	// Without a scope there is nothing to compare against, so the browse keeps
	// the behaviour it had.
	plan = sqlBrowseTagPlan(ctx, db, rare, 0, false)
	assert.Nil(t, plan.driveFromTag)
}

// TestBrowseTagPlan_ShapesAgree is the correctness guard. The plan only changes
// which side a filter is resolved from, so both shapes have to return the same
// rows and the same total for every filter, whichever side the plan picks.
func TestBrowseTagPlan_ShapesAgree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	f, cleanup := setupTagPlanFixture(t)
	t.Cleanup(cleanup)
	db := f.mediaDB.sql.Load()
	systems := []systemdefs.System{f.system}

	for _, testCase := range []struct {
		name string
		tags []zapscript.TagFilter
		want int
	}{
		{name: "rare", tags: []zapscript.TagFilter{{Type: "region", Value: "rare"}}, want: tagPlanRareFiles},
		{name: "common", tags: []zapscript.TagFilter{{Type: "region", Value: "common"}}, want: tagPlanBigFiles},
		{name: "absent", tags: []zapscript.TagFilter{{Type: "region", Value: "nosuch"}}, want: 0},
		{name: "both", tags: []zapscript.TagFilter{
			{Type: "region", Value: "rare"}, {Type: "region", Value: "common"},
		}, want: tagPlanRareFiles},
		{name: "not", tags: []zapscript.TagFilter{
			{Type: "region", Value: "rare", Operator: zapscript.TagOperatorNOT},
		}, want: tagPlanBigFiles - tagPlanRareFiles},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			countOpts := database.BrowseFileCountOptions{
				PathPrefix: f.bigDir,
				Systems:    systems,
				Tags:       testCase.tags,
			}
			filesOpts := &database.BrowseFilesOptions{
				PathPrefix: f.bigDir,
				Systems:    systems,
				Tags:       testCase.tags,
				Limit:      1000,
			}

			probed := browseTagCountWithPlan(ctx, t, db, countOpts, browseTagPlan{})
			assert.Equal(t, testCase.want, probed, "candidate-probe total")

			for _, filter := range browseTagDriverCandidates(testCase.tags) {
				driven := browseTagCountWithPlan(ctx, t, db, countOpts,
					browseTagPlan{driveFromTag: &filter})
				assert.Equal(t, probed, driven,
					"driving from %s:%s must not change the total", filter.Type, filter.Value)

				probedRows := browseTagPathsWithPlan(ctx, t, db, filesOpts, browseTagPlan{})
				drivenRows := browseTagPathsWithPlan(ctx, t, db, filesOpts,
					browseTagPlan{driveFromTag: &filter})
				assert.Equal(t, probedRows, drivenRows,
					"driving from %s:%s must not change the rows", filter.Type, filter.Value)
				assert.Len(t, probedRows, probed, "the total must match the rows returned")
			}
		})
	}
}

func browseTagCountWithPlan(
	ctx context.Context, t *testing.T, db sqlQueryable,
	opts database.BrowseFileCountOptions, //nolint:gocritic // mirrors the production call
	plan browseTagPlan,
) int {
	t.Helper()
	where, args := browseFilesBaseCondition(&database.BrowseFilesOptions{
		PathPrefix: opts.PathPrefix,
		Letter:     opts.Letter,
		Systems:    opts.Systems,
		Tags:       opts.Tags,
	}, plan)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM Media m
		INNER JOIN Systems s ON m.SystemDBID = s.DBID
		WHERE `+where, args...).Scan(&count))
	return count
}

func browseTagPathsWithPlan(
	ctx context.Context, t *testing.T, db sqlQueryable,
	opts *database.BrowseFilesOptions, plan browseTagPlan,
) []string {
	t.Helper()
	where, args := browseFilesBaseCondition(opts, plan)
	rows, err := db.QueryContext(ctx, `SELECT m.Path
		FROM Media m
		INNER JOIN Systems s ON m.SystemDBID = s.DBID
		WHERE `+where+` ORDER BY m.Path`, args...)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var paths []string
	for rows.Next() {
		var path string
		require.NoError(t, rows.Scan(&path))
		paths = append(paths, path)
	}
	require.NoError(t, rows.Err())
	return paths
}
