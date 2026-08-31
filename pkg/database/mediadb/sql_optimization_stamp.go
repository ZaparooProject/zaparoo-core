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
	"database/sql"
	"errors"
	"fmt"
)

// sqlMediaExists reports whether the database holds any media rows at all.
func sqlMediaExists(ctx context.Context, db sqlQueryable) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM Media LIMIT 1`).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check for media rows: %w", err)
	}
	return true, nil
}

// sqlClearOptimizationStamp removes the pending optimization markers.
func sqlClearOptimizationStamp(ctx context.Context, db sqlQueryable) error {
	_, err := db.ExecContext(ctx,
		"DELETE FROM DBConfig WHERE Name IN (?, ?)",
		DBConfigOptimizationStatus,
		DBConfigOptimizationStep,
	)
	if err != nil {
		return fmt.Errorf("failed to clear optimization stamp: %w", err)
	}
	return nil
}

// clearOptimizationStampIfEmpty drops a pending optimization stamp from a
// database that has no media to optimize.
//
// Two migrations stamp OptimizationStatus=pending and OptimizationStep=browse_cache
// unconditionally so that *existing* databases rebuild their browse cache on
// upgrade (20260429142159_system_browse_cache.sql and 20260430054834_browse_v2.sql).
// A brand-new database runs the same migration chain, so it is born pending too,
// and checkAndResumeOptimization then "resumes" a browse cache rebuild over zero
// media on first start.
//
// The stamp itself must stay — the upgrade path depends on it. This only clears
// it where there is provably nothing to do. Mirrors disambiguationBackfillPending:
// stamp-on-empty rather than skip-on-empty, so the state is corrected once at
// migration time instead of being re-evaluated on every start.
//
// A rebuild via Recreate also lands here with an empty database, which is
// correct: its legitimate pending stamp is written later, after indexing
// finishes, so clearing here cannot race it.
func (db *MediaDB) clearOptimizationStampIfEmpty(ctx context.Context) error {
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return ErrNullSQL
	}

	status, err := sqlGetOptimizationStatus(ctx, sqlDB)
	if err != nil {
		return err
	}
	if status == "" {
		return nil
	}

	hasMedia, err := sqlMediaExists(ctx, sqlDB)
	if err != nil {
		return err
	}
	if hasMedia {
		return nil
	}
	return sqlClearOptimizationStamp(ctx, sqlDB)
}
