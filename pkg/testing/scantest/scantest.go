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

// Package scantest populates media databases in tests through the production
// staging pipeline. It lives outside pkg/testing/helpers because it imports
// mediascanner, which the mediascanner package's own tests (which import
// helpers) must not see transitively.
package scantest

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/stretchr/testify/require"
)

// IndexMediaPaths runs the given paths through the production staging pipeline
// for one system — canonical tag seeding, staging, set-based reconcile — and
// commits the result. It is the test-suite replacement for hand-driving the
// old AddMediaPath/ScanState flow. This helper uses ReconcileStagedSystem with
// default ScanReconcileOpts, so it is a full scan and marks unstaged media for
// the same system missing. Use IndexMediaPathsWithOpts with IncompleteScan:
// true for incremental updates.
func IndexMediaPaths(
	tb testing.TB, db database.MediaDBI, systemID string, paths ...string,
) database.ScanReconcileStats {
	tb.Helper()
	return IndexMediaPathsWithOpts(tb, db, systemID, database.ScanReconcileOpts{}, paths...)
}

// IndexMediaPathsWithOpts runs IndexMediaPaths with explicit ReconcileStagedSystem
// options, for callers that need incremental scans with IncompleteScan: true.
func IndexMediaPathsWithOpts(
	tb testing.TB,
	db database.MediaDBI,
	systemID string,
	opts database.ScanReconcileOpts,
	paths ...string,
) database.ScanReconcileStats {
	tb.Helper()
	ctx := context.Background()

	require.NoError(tb, mediascanner.SeedCanonicalTags(ctx, db))
	require.NoError(tb, db.BeginTransaction(true))
	require.NoError(tb, db.ClearScanStage())
	for _, path := range paths {
		require.NoError(tb, mediascanner.StageMediaPath(&mediascanner.StageMediaPathParams{
			DB:       db,
			SystemID: systemID,
			Path:     path,
		}))
	}
	stats, err := db.ReconcileStagedSystem(ctx, systemID, opts)
	require.NoError(tb, err)
	require.NoError(tb, db.CommitTransaction())
	return stats
}
