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
	"fmt"

	"github.com/rs/zerolog/log"
)

// sqlSeedPlannerStats installs the captured sqlite_stat1 rows (see
// stat1_seed_data.go) on a database that has no real Media statistics yet.
// A fresh database otherwise has an empty sqlite_stat1 until the first
// ANALYZE at the end of indexing, and mid-index queries can pick
// catastrophic plans without statistics. This follows the "fixed results of
// ANALYZE" pattern documented at sqlite.org/lang_analyze.html: seed captured
// rows, then ANALYZE sqlite_schema so the planner reloads them. The seed is
// replaced by a real approximate ANALYZE after the first system commits
// during indexing.
func sqlSeedPlannerStats(ctx context.Context, db *sql.DB) error {
	// Analyzing just the schema table creates the statistics tables if they
	// don't exist yet, without scanning any data tables.
	if _, err := db.ExecContext(ctx, "ANALYZE sqlite_schema"); err != nil {
		return fmt.Errorf("planner stats seed: failed to create statistics tables: %w", err)
	}

	var existing int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_stat1 WHERE tbl = 'Media'").Scan(&existing)
	if err != nil {
		return fmt.Errorf("planner stats seed: failed to check existing statistics: %w", err)
	}
	if existing > 0 {
		// Real (or previously seeded) statistics present; never overwrite.
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("planner stats seed: failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, row := range stat1SeedRows {
		var idx any
		if row.idx != "" {
			idx = row.idx
		}
		if _, insertErr := tx.ExecContext(ctx,
			"INSERT INTO sqlite_stat1 (tbl, idx, stat) VALUES (?, ?, ?)",
			row.tbl, idx, row.stat,
		); insertErr != nil {
			return fmt.Errorf("planner stats seed: failed to insert row for %s: %w", row.tbl, insertErr)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("planner stats seed: failed to commit: %w", err)
	}

	// Force the query planner to reload the statistics tables.
	if _, err := db.ExecContext(ctx, "ANALYZE sqlite_schema"); err != nil {
		return fmt.Errorf("planner stats seed: failed to reload statistics: %w", err)
	}

	log.Debug().Int("rows", len(stat1SeedRows)).Msg("seeded planner statistics on fresh media database")
	return nil
}
