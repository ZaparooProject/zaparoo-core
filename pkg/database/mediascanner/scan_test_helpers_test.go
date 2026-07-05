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
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/browseprefix"
	"github.com/stretchr/testify/require"
)

// indexMediaPaths runs paths through the production staging pipeline for one
// system — canonical tag seeding, staging, set-based reconcile — and commits.
// Mirror of pkg/testing/scantest.IndexMediaPaths for use inside this package
// (which cannot import a helper package that imports mediascanner back).
func indexMediaPaths(
	tb testing.TB, db database.MediaDBI, systemID string, paths ...string,
) database.ScanReconcileStats {
	tb.Helper()
	stats, err := indexMediaPathsErr(db, systemID, paths...)
	require.NoError(tb, err)
	return stats
}

// indexMediaPathsErr is indexMediaPaths without assertions, for tests that
// exercise failure paths themselves.
func indexMediaPathsErr(
	db database.MediaDBI, systemID string, paths ...string,
) (database.ScanReconcileStats, error) {
	ctx := context.Background()
	if err := SeedCanonicalTags(ctx, db); err != nil {
		return database.ScanReconcileStats{}, err
	}
	if err := db.BeginTransaction(true); err != nil {
		return database.ScanReconcileStats{}, fmt.Errorf("begin transaction: %w", err)
	}
	if err := db.ClearScanStage(); err != nil {
		return database.ScanReconcileStats{}, fmt.Errorf("clear scan stage: %w", err)
	}
	for _, path := range paths {
		if err := StageMediaPath(db, systemID, path, "", false, browseprefix.Policy{}, nil, ""); err != nil {
			return database.ScanReconcileStats{}, err
		}
	}
	stats, err := db.ReconcileStagedSystem(ctx, systemID, database.ScanReconcileOpts{})
	if err != nil {
		return stats, fmt.Errorf("reconcile staged system: %w", err)
	}
	if err := db.CommitTransaction(); err != nil {
		return stats, fmt.Errorf("commit transaction: %w", err)
	}
	return stats, nil
}
