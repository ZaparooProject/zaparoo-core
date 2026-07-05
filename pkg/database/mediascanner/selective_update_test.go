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

package mediascanner

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner/testdata"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func indexBatchSystems(t *testing.T, db database.MediaDBI, batch testdata.TestBatch, systems ...string) {
	t.Helper()
	for _, systemID := range systems {
		paths := make([]string, 0, len(batch.Entries[systemID]))
		for _, entry := range batch.Entries[systemID] {
			paths = append(paths, entry.Path)
		}
		indexMediaPaths(t, db, systemID, paths...)
	}
}

// TestSelectiveUpdate_DuplicateTagPrevention verifies that truncating one
// system and re-indexing it never duplicates tag types, tag values, or titles.
func TestSelectiveUpdate_DuplicateTagPrevention(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	testSystems := []string{"nes", "snes"}
	batch := testdata.CreateReproducibleBatch(testSystems, 10)
	indexBatchSystems(t, db, batch, testSystems...)

	countRows := func(query string) int {
		var n int
		require.NoError(t, db.UnsafeGetSQLDb().QueryRowContext(ctx, query).Scan(&n))
		return n
	}
	tagsBefore := countRows("SELECT COUNT(*) FROM Tags")
	tagTypesBefore := countRows("SELECT COUNT(*) FROM TagTypes")

	// Selective reindex: truncate NES (orphan tags cleaned), then re-index it.
	require.NoError(t, db.TruncateSystems([]string{"nes"}))
	indexBatchSystems(t, db, batch, "nes")

	// Tag/TagType counts must not increase — increases would mean duplicates.
	assert.LessOrEqual(t, countRows("SELECT COUNT(*) FROM Tags"), tagsBefore,
		"Tag count should not increase across truncate + reindex")
	assert.Equal(t, tagTypesBefore, countRows("SELECT COUNT(*) FROM TagTypes"),
		"TagType count should be unchanged across truncate + reindex")

	duplicateMediaTitles, err := db.CheckForDuplicateMediaTitles()
	require.NoError(t, err)
	assert.Empty(t, duplicateMediaTitles, "ZERO duplicate MediaTitles allowed")
}

// TestSelectiveUpdate_MediaTagAssociationsPreserved verifies that media entries
// actually have tag associations after selective reindexing (regression test for #504).
func TestSelectiveUpdate_MediaTagAssociationsPreserved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	testSystems := []string{"nes", "snes", "genesis"}
	batch := testdata.CreateReproducibleBatch(testSystems, 10)
	indexBatchSystems(t, db, batch, testSystems...)

	nesMediaTagCount := func() int {
		var n int
		require.NoError(t, db.UnsafeGetSQLDb().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM MediaTags mt
			 JOIN Media m ON mt.MediaDBID = m.DBID
			 JOIN Systems s ON m.SystemDBID = s.DBID
			 WHERE s.SystemID = ?`, "nes").Scan(&n))
		return n
	}

	mediaTagCountBefore := nesMediaTagCount()
	require.Positive(t, mediaTagCountBefore, "NES media should have tag associations from full index")

	require.NoError(t, db.TruncateSystems([]string{"nes"}))
	indexBatchSystems(t, db, batch, "nes")

	assert.Equal(t, mediaTagCountBefore, nesMediaTagCount(),
		"MediaTag count should match after selective reindex (tags must not be silently dropped)")
}
