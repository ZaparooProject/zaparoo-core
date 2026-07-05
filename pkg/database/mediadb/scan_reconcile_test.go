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

// Package mediadb_test drives the scan-staging/reconcile pipeline
// (sqlReconcileStagedSystem, sqlSeedCanonicalTags, and their MediaDB
// wrappers) through MediaDB's public API. It exists as an external test
// package specifically so its coverage is attributed to package mediadb: the
// same pipeline is already exercised end-to-end, scenario by scenario, in
// pkg/database/mediascanner's test suite, but per-package coverage (what CI
// and codecov measure) never credits mediadb for tests that live in
// mediascanner. These tests pin the contract at the mediadb boundary rather
// than re-deriving every mediascanner behavioral case.
package mediadb_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mediaDBIDsBySystem(t *testing.T, db *mediadb.MediaDB, systemID string) map[string]database.MediaWithFullPath {
	t.Helper()
	rows, err := db.GetMediaBySystemID(systemID)
	require.NoError(t, err)
	byPath := make(map[string]database.MediaWithFullPath, len(rows))
	for _, row := range rows {
		byPath[row.Path] = row
	}
	return byPath
}

func TestReconcileStagedSystem_FullScanInsertsTitlesMediaAndTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Super Game (USA) (Rev 2).sfc")
	stats := scantest.IndexMediaPaths(t, mediaDB, "SNES", gamePath)

	assert.True(t, stats.SystemKnown)
	assert.Positive(t, stats.SystemDBID)
	assert.Equal(t, int64(1), stats.TitlesInserted)
	assert.Equal(t, int64(1), stats.MediaUpserted)
	assert.Equal(t, int64(0), stats.MediaMissing)

	total, err := mediaDB.GetTotalMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 1, total)

	byPath := mediaDBIDsBySystem(t, mediaDB, "SNES")
	require.Contains(t, byPath, gamePath)
	assert.False(t, byPath[gamePath].IsMissing)
}

func TestReconcileStagedSystem_IdempotentRescanIsNoOp(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.Join(string(filepath.Separator), "roms", "Genesis", "Game.md")
	scantest.IndexMediaPaths(t, mediaDB, "Genesis", gamePath)

	stats := scantest.IndexMediaPaths(t, mediaDB, "Genesis", gamePath)
	assert.Equal(t, int64(0), stats.TitlesInserted)
	assert.Equal(t, int64(0), stats.MediaUpserted)
	assert.Equal(t, int64(0), stats.MediaMissing)
	assert.Equal(t, int64(0), stats.TouchedTitles)
}

// TestReconcileStagedSystem_IncompleteScanPreservesMissingState pins
// ScanReconcileOpts.IncompleteScan: a scan known to have only partially
// collected a system's files must not flag the unstaged remainder missing,
// unlike an ordinary full scan of the same reduced set.
func TestReconcileStagedSystem_IncompleteScanPreservesMissingState(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	fileA := filepath.Join(string(filepath.Separator), "roms", "SNES", "Game A.sfc")
	fileB := filepath.Join(string(filepath.Separator), "roms", "SNES", "Game B.sfc")
	scantest.IndexMediaPaths(t, mediaDB, "SNES", fileA, fileB)

	stats := scantest.IndexMediaPathsWithOpts(
		t, mediaDB, "SNES", database.ScanReconcileOpts{IncompleteScan: true}, fileA,
	)
	assert.Equal(t, int64(0), stats.MediaMissing, "incomplete scan must not flag the omitted file missing")

	missing, err := mediaDB.GetMissingMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 0, missing, "fileB must remain present after a known-incomplete scan omitted it")

	// A full (non-incomplete) rescan of the same reduced set does flag the
	// omitted file missing, confirming the difference is IncompleteScan and
	// not some other property of the reduced staged set.
	stats = scantest.IndexMediaPaths(t, mediaDB, "SNES", fileA)
	assert.Equal(t, int64(1), stats.MediaMissing)
	missing, err = mediaDB.GetMissingMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 1, missing)
}

// TestReconcileStagedSystem_UnknownSystemNoStagedIsNoop pins
// sqlResolveScanSystem's early-out: reconciling a system with no existing
// Systems row and nothing staged for it must not create one.
func TestReconcileStagedSystem_UnknownSystemNoStagedIsNoop(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, mediaDB.BeginTransaction(true))
	stats, err := mediaDB.ReconcileStagedSystem(context.Background(), "NeverSeen", database.ScanReconcileOpts{})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	assert.False(t, stats.SystemKnown)
	assert.Zero(t, stats.SystemDBID)

	systems, err := mediaDB.GetAllSystems()
	require.NoError(t, err)
	assert.Empty(t, systems)
}

// TestReconcileStagedSystem_RequiresOpenTransaction pins the guard that
// reconcile must run inside the scanner's open batch transaction.
func TestReconcileStagedSystem_RequiresOpenTransaction(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	_, err := mediaDB.ReconcileStagedSystem(context.Background(), "SNES", database.ScanReconcileOpts{})
	require.ErrorIs(t, err, mediadb.ErrTransactionRequired)
}

// TestSeedCanonicalTagDefinitions_IdempotentNoDuplicates pins the anti-join
// dedup in sqlSeedCanonicalTags: seeding twice must not create duplicate
// TagTypes or Tags rows.
func TestSeedCanonicalTagDefinitions_IdempotentNoDuplicates(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))

	var typesAfterFirst, tagsAfterFirst int
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM TagTypes").Scan(&typesAfterFirst))
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM Tags").Scan(&tagsAfterFirst))
	require.Positive(t, typesAfterFirst)
	require.Positive(t, tagsAfterFirst)

	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))

	var typesAfterSecond, tagsAfterSecond int
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM TagTypes").Scan(&typesAfterSecond))
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM Tags").Scan(&tagsAfterSecond))
	assert.Equal(t, typesAfterFirst, typesAfterSecond)
	assert.Equal(t, tagsAfterFirst, tagsAfterSecond)
}
