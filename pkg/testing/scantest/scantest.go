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
	"errors"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
	results := make([]platforms.ScanResult, 0, len(paths))
	for _, path := range paths {
		results = append(results, platforms.ScanResult{Path: path})
	}
	return IndexScanResults(tb, db, systemID, database.ScanReconcileOpts{}, results...)
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
	results := make([]platforms.ScanResult, 0, len(paths))
	for _, path := range paths {
		results = append(results, platforms.ScanResult{Path: path})
	}
	return IndexScanResults(tb, db, systemID, opts, results...)
}

// IndexScanResults stages complete scanner results, including optional source
// provenance, through the production reconciliation pipeline.
func IndexScanResults(
	tb testing.TB,
	db database.MediaDBI,
	systemID string,
	opts database.ScanReconcileOpts,
	results ...platforms.ScanResult,
) database.ScanReconcileStats {
	tb.Helper()
	stats, err := Index(context.Background(), db, systemID, opts, results...)
	require.NoError(tb, err)
	return stats
}

// IndexBatchSize is how many staged paths Index writes per transaction. The
// production indexer commits mid-system on a file limit too; staged rows live
// in the scan scratch database, so they survive the commit until reconcile.
const IndexBatchSize = 10_000

// Index is IndexScanResults without a testing.TB, for callers outside tests
// such as the simulator's fixture generator. It seeds canonical tags, stages
// every result, reconciles the system with opts and commits. On error the open
// transaction is rolled back.
func Index(
	ctx context.Context,
	db database.MediaDBI,
	systemID string,
	opts database.ScanReconcileOpts,
	results ...platforms.ScanResult,
) (stats database.ScanReconcileStats, err error) {
	if err = mediascanner.SeedCanonicalTags(ctx, db); err != nil {
		return stats, fmt.Errorf("seed canonical tags: %w", err)
	}
	if err = db.BeginTransaction(true); err != nil {
		return stats, fmt.Errorf("begin transaction: %w", err)
	}
	inTx := true
	defer func() {
		if err != nil && inTx {
			err = errors.Join(err, db.RollbackTransaction())
		}
	}()
	if err = db.ClearScanStage(); err != nil {
		return stats, fmt.Errorf("clear scan stage: %w", err)
	}
	for i := range results {
		if i > 0 && i%IndexBatchSize == 0 {
			if err = ctx.Err(); err != nil {
				return stats, fmt.Errorf("index %s: %w", systemID, err)
			}
			inTx = false
			if err = db.CommitTransaction(); err != nil {
				return stats, fmt.Errorf("commit staging batch: %w", err)
			}
			if err = db.BeginTransaction(true); err != nil {
				return stats, fmt.Errorf("begin transaction: %w", err)
			}
			inTx = true
		}
		result := &results[i]
		if err = mediascanner.StageMediaPath(&mediascanner.StageMediaPathParams{
			DB: db, SystemID: systemID, Path: result.Path, ProvidedName: result.Name,
			Source: result.Source, LinkTarget: result.LinkTarget, NoExt: result.NoExt,
		}); err != nil {
			return stats, fmt.Errorf("stage %q: %w", result.Path, err)
		}
	}
	if stats, err = db.ReconcileStagedSystem(ctx, systemID, opts); err != nil {
		return stats, fmt.Errorf("reconcile %s: %w", systemID, err)
	}
	inTx = false
	if err = db.CommitTransaction(); err != nil {
		return stats, fmt.Errorf("commit: %w", err)
	}
	return stats, nil
}
