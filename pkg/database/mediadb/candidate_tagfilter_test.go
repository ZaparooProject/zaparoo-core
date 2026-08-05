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
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCandidateTagFilterSQL(t *testing.T) {
	t.Parallel()

	t.Run("regular AND filters use candidate-correlated lookups", func(t *testing.T) {
		t.Parallel()
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
		}

		clauses, args := buildCandidateTagFilterSQL(filters)

		require.Len(t, clauses, 2)
		require.Len(t, args, 8)
		for _, clause := range clauses {
			assert.Equal(t, 2, strings.Count(clause, "EXISTS ("))
			assert.Contains(t, clause, "MediaTags.MediaDBID = Media.DBID")
			assert.Contains(t, clause, "MediaTitleTags.MediaTitleDBID = Media.MediaTitleDBID")
			assert.NotContains(t, clause, "Media.DBID IN (")
		}
		assert.Equal(t, []any{"region", "us", "region", "us", "lang", "en", "lang", "en"}, args)
	})

	t.Run("NOT filter checks both candidate tag sources", func(t *testing.T) {
		t.Parallel()
		filters := []zapscript.TagFilter{
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
		}

		clauses, args := buildCandidateTagFilterSQL(filters)

		require.Len(t, clauses, 1)
		assert.Contains(t, clauses[0], "NOT (")
		assert.Equal(t, 2, strings.Count(clauses[0], "EXISTS ("))
		assert.NotContains(t, clauses[0], "NOT IN")
		assert.Equal(t, []any{"unfinished", "demo", "unfinished", "demo"}, args)
	})

	t.Run("OR filters share candidate-correlated lookup", func(t *testing.T) {
		t.Parallel()
		filters := []zapscript.TagFilter{
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "ja", Operator: zapscript.TagOperatorOR},
		}

		clauses, args := buildCandidateTagFilterSQL(filters)

		require.Len(t, clauses, 1)
		assert.Equal(t, 2, strings.Count(clauses[0], "EXISTS ("))
		assert.Equal(t, 4, strings.Count(clauses[0], "TagTypes.Type = ?"))
		assert.Equal(t, []any{"lang", "en", "lang", "ja", "lang", "en", "lang", "ja"}, args)
	})

	t.Run("AND credit searches all credit roles", func(t *testing.T) {
		t.Parallel()
		filters := []zapscript.TagFilter{
			{Type: "credit", Value: "nintendo", Operator: zapscript.TagOperatorAND},
		}

		clauses, args := buildCandidateTagFilterSQL(filters)

		require.Len(t, clauses, 1)
		assert.Equal(t, 2, strings.Count(clauses[0], "TagTypes.Type IN (?, ?, ?)"))
		assert.Equal(t, []any{
			"nintendo", "developer", "publisher", "credit",
			"nintendo", "developer", "publisher", "credit",
		}, args)
	})
}

func TestSqlSearchMediaByTitleDBIDs_UsesCandidateTagFilters(t *testing.T) {
	t.Parallel()

	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)WHERE MediaTitles\.DBID IN \(\?\).*AND \(EXISTS \(.*`+
		`MediaTags\.MediaDBID = Media\.DBID.*MediaTitleTags\.MediaTitleDBID = Media\.MediaTitleDBID`).
		WithArgs(int64(10), "region", "us", "region", "us", 100).
		WillReturnRows(sqlmock.NewRows([]string{
			"SystemID", "Name", "Path", "DBID", "DisambiguationTypes",
		}))

	results, err := sqlSearchMediaByTitleDBIDs(
		context.Background(),
		db,
		[]int64{10},
		[]zapscript.TagFilter{{Type: "region", Value: "us"}},
		nil,
		nil,
		100,
	)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSqlSearchMediaByTitleDBIDs_CandidateTagFilterSemantics(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	regionType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)
	require.NoError(t, mediaDB.BeginTransaction(false))

	nes, err := systemdefs.GetSystem(systemdefs.SystemNES)
	require.NoError(t, err)
	system, err := mediaDB.InsertSystem(database.System{SystemID: nes.ID, Name: nes.ID})
	require.NoError(t, err)

	type fixture struct {
		path    string
		region  string
		titleID int64
		title   bool
	}
	fixtures := []fixture{
		{path: filepath.Join("games", "us-file.nes"), region: "us"},
		{path: filepath.Join("games", "us-title.nes"), region: "us", title: true},
		{path: filepath.Join("games", "jp-file.nes"), region: "jp"},
		{path: filepath.Join("games", "untagged.nes")},
	}
	for i := range fixtures {
		title, insertErr := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: system.DBID,
			Slug:       "candidate" + fixtures[i].region,
			Name:       fixtures[i].path,
		})
		require.NoError(t, insertErr)
		fixtures[i].titleID = title.DBID
		media, insertErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     system.DBID,
			MediaTitleDBID: title.DBID,
			Path:           fixtures[i].path,
		})
		require.NoError(t, insertErr)
		if fixtures[i].region == "" || fixtures[i].title {
			continue
		}
		tag, tagErr := mediaDB.FindOrInsertTag(database.Tag{
			TypeDBID: regionType.DBID,
			Tag:      fixtures[i].region,
		})
		require.NoError(t, tagErr)
		_, tagErr = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: media.DBID, TagDBID: tag.DBID})
		require.NoError(t, tagErr)
	}
	require.NoError(t, mediaDB.CommitTransaction())
	require.NoError(t, mediaDB.UpsertMediaTitleTags(context.Background(), fixtures[1].titleID, []database.TagInfo{{
		Type: "region",
		Tag:  "us",
	}}))

	candidateIDs := make([]int64, len(fixtures))
	for i := range fixtures {
		candidateIDs[i] = fixtures[i].titleID
	}
	tests := []struct {
		name      string
		filters   []zapscript.TagFilter
		wantPaths []string
	}{
		{
			name:      "AND matches file and title tags",
			filters:   []zapscript.TagFilter{{Type: "region", Value: "us"}},
			wantPaths: []string{fixtures[0].path, fixtures[1].path},
		},
		{
			name: "NOT excludes file and title tags",
			filters: []zapscript.TagFilter{{
				Type: "region", Value: "us", Operator: zapscript.TagOperatorNOT,
			}},
			wantPaths: []string{fixtures[2].path, fixtures[3].path},
		},
		{
			name: "OR matches either value across both sources",
			filters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorOR},
				{Type: "region", Value: "jp", Operator: zapscript.TagOperatorOR},
			},
			wantPaths: []string{fixtures[0].path, fixtures[1].path, fixtures[2].path},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, searchErr := sqlSearchMediaByTitleDBIDs(
				context.Background(), mediaDB.sql.Load(), candidateIDs, tt.filters, nil, nil, 10,
			)
			require.NoError(t, searchErr)
			paths := make([]string, len(results))
			for i := range results {
				paths[i] = results[i].Path
			}
			assert.ElementsMatch(t, tt.wantPaths, paths)
		})
	}
}
