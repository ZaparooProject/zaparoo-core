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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

const (
	DBConfigLastGeneratedAt                 = "LastGeneratedAt"
	DBConfigOptimizationStatus              = "OptimizationStatus"
	DBConfigOptimizationStep                = "OptimizationStep"
	DBConfigIndexingStatus                  = "IndexingStatus"
	DBConfigScrapingStatus                  = "ScrapingStatus"
	DBConfigScrapingOperation               = "ScrapingOperation"
	DBConfigLastIndexedSystem               = "LastIndexedSystem"
	DBConfigIndexingSystems                 = "IndexingSystems"
	DBConfigIndexingPlanSystems             = "IndexingPlanSystems"
	DBConfigBrowseIndexVersion              = "BrowseIndexVersion"
	DBConfigMediaTotalCount                 = "MediaTotalCount"
	DBConfigMediaMissingCount               = "MediaMissingCount"
	DBConfigTemporaryRepairParentDirVersion = "TemporaryRepairParentDirVersion"
	DBConfigIndexGeneration                 = "IndexGeneration"
	DBConfigIndexResumeAttempts             = "IndexResumeAttempts"
	DBConfigDisambiguationVersion           = "DisambiguationVersion"

	temporaryRepairParentDirVersion = "1"

	// disambiguationAlgoVersion identifies the current title-disambiguation
	// algorithm (the recompute query plus the ZapScriptTagTypes mapping feeding
	// it). Indexing only recomputes DisambiguationTypes for titles whose media or
	// tags changed, so values stored by an older algorithm are never revisited by
	// reindexing alone; a stamp mismatch triggers a one-time background backfill
	// instead (see runDisambiguationBackfill). Bump this whenever the recompute
	// query or the ZapScriptTagTypes mapping changes.
	disambiguationAlgoVersion = "1"
)

func sqlUpdateLastGenerated(ctx context.Context, db sqlQueryable) error {
	_, err := db.ExecContext(ctx,
		fmt.Sprintf(
			"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES ('%s', ?)",
			DBConfigLastGeneratedAt,
		),
		strconv.FormatInt(time.Now().Unix(), 10),
	)
	if err != nil {
		return fmt.Errorf("failed to set last generated timestamp: %w", err)
	}
	return nil
}

func sqlRefreshMediaCounts(ctx context.Context, db sqlQueryable) error {
	var total, missing int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN IsMissing != 0 THEN 1 ELSE 0 END), 0)
		FROM Media`,
	).Scan(&total, &missing); err != nil {
		return fmt.Errorf("failed to count media rows: %w", err)
	}
	for _, count := range []struct {
		name  string
		value int
	}{
		{name: DBConfigMediaTotalCount, value: total},
		{name: DBConfigMediaMissingCount, value: missing},
	} {
		if _, err := db.ExecContext(ctx,
			"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
			count.name, strconv.Itoa(count.value),
		); err != nil {
			return fmt.Errorf("failed to set %s: %w", count.name, err)
		}
	}
	return nil
}

func sqlInvalidateMediaCountCache(ctx context.Context, db sqlQueryable, names ...string) error {
	for _, name := range names {
		if _, err := db.ExecContext(ctx, "DELETE FROM DBConfig WHERE Name = ?", name); err != nil {
			return fmt.Errorf("failed to invalidate %s: %w", name, err)
		}
	}
	return nil
}

func sqlGetLastGenerated(ctx context.Context, db *sql.DB) (time.Time, error) {
	var rawTimestamp string
	err := db.QueryRowContext(ctx,
		fmt.Sprintf(
			"SELECT Value FROM DBConfig WHERE Name = '%s'",
			DBConfigLastGeneratedAt,
		),
	).Scan(&rawTimestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	} else if err != nil {
		return time.Time{}, fmt.Errorf("failed to scan timestamp: %w", err)
	}

	timestamp, err := strconv.Atoi(rawTimestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse timestamp: %w", err)
	}

	return time.Unix(int64(timestamp), 0), nil
}

func sqlSetOptimizationStatus(ctx context.Context, db sqlQueryable, status string) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigOptimizationStatus,
		status,
	)
	if err != nil {
		return fmt.Errorf("failed to set optimization status: %w", err)
	}
	return nil
}

func sqlGetOptimizationStatus(ctx context.Context, db *sql.DB) (string, error) {
	var status string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigOptimizationStatus,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get optimization status: %w", err)
	}
	return status, nil
}

func sqlSetOptimizationStep(ctx context.Context, db sqlQueryable, step string) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigOptimizationStep,
		step,
	)
	if err != nil {
		return fmt.Errorf("failed to set optimization step: %w", err)
	}
	return nil
}

func sqlGetOptimizationStep(ctx context.Context, db *sql.DB) (string, error) {
	var step string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigOptimizationStep,
	).Scan(&step)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get optimization step: %w", err)
	}
	return step, nil
}

func sqlSetIndexResumeAttempts(ctx context.Context, db sqlQueryable, attempts int) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigIndexResumeAttempts,
		strconv.Itoa(attempts),
	)
	if err != nil {
		return fmt.Errorf("failed to set index resume attempts: %w", err)
	}
	return nil
}

func sqlGetIndexResumeAttempts(ctx context.Context, db sqlQueryable) (int, error) {
	var raw string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigIndexResumeAttempts,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("failed to get index resume attempts: %w", err)
	}
	attempts, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("failed to parse index resume attempts: %w", err)
	}
	return attempts, nil
}

func sqlSetIndexingStatus(ctx context.Context, db sqlQueryable, status string) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigIndexingStatus,
		status,
	)
	if err != nil {
		return fmt.Errorf("failed to set indexing status: %w", err)
	}
	return nil
}

func sqlGetIndexingStatus(ctx context.Context, db *sql.DB) (string, error) {
	var status string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigIndexingStatus,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get indexing status: %w", err)
	}
	return status, nil
}

func sqlSetScrapingStatus(ctx context.Context, db sqlQueryable, status string) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigScrapingStatus,
		status,
	)
	if err != nil {
		return fmt.Errorf("failed to set scraping status: %w", err)
	}
	return nil
}

func sqlGetScrapingStatus(ctx context.Context, db *sql.DB) (string, error) {
	var status string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigScrapingStatus,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get scraping status: %w", err)
	}
	return status, nil
}

func sqlSetScrapingOperation(ctx context.Context, db sqlQueryable, operation database.ScrapingOperation) error {
	operationJSON, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("failed to marshal scraping operation: %w", err)
	}
	_, err = db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigScrapingOperation,
		string(operationJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to set scraping operation: %w", err)
	}
	return nil
}

func sqlGetScrapingOperation(ctx context.Context, db *sql.DB) (database.ScrapingOperation, bool, error) {
	var operationJSON string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigScrapingOperation,
	).Scan(&operationJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return database.ScrapingOperation{}, false, nil
	} else if err != nil {
		return database.ScrapingOperation{}, false, fmt.Errorf("failed to get scraping operation: %w", err)
	}

	var operation database.ScrapingOperation
	if err := json.Unmarshal([]byte(operationJSON), &operation); err != nil {
		return database.ScrapingOperation{}, false, fmt.Errorf("failed to unmarshal scraping operation: %w", err)
	}
	return operation, true, nil
}

func sqlClearScrapingOperation(ctx context.Context, db sqlQueryable) error {
	_, err := db.ExecContext(ctx, "DELETE FROM DBConfig WHERE Name = ?", DBConfigScrapingOperation)
	if err != nil {
		return fmt.Errorf("failed to clear scraping operation: %w", err)
	}
	return nil
}

func sqlSetLastIndexedSystem(ctx context.Context, db sqlQueryable, systemID string) error {
	_, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigLastIndexedSystem,
		systemID,
	)
	if err != nil {
		return fmt.Errorf("failed to set last indexed system: %w", err)
	}
	return nil
}

func sqlGetLastIndexedSystem(ctx context.Context, db *sql.DB) (string, error) {
	var systemID string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigLastIndexedSystem,
	).Scan(&systemID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get last indexed system: %w", err)
	}
	return systemID, nil
}

// sqlGetIndexGeneration reads the monotonic counter that's bumped at the end
// of every successful indexing run. Persisted on-disk caches (tag cache, slug
// search cache) embed this value in their header so a stale cache file from
// a previous run can be detected and rebuilt.
func sqlGetIndexGeneration(ctx context.Context, db *sql.DB) (int64, error) {
	var raw string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigIndexGeneration,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("failed to get index generation: %w", err)
	}
	gen, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse index generation: %w", err)
	}
	return gen, nil
}

// sqlBumpIndexGeneration atomically increments the generation counter and
// returns the new value via a single INSERT ... ON CONFLICT DO UPDATE ...
// RETURNING statement. Not transactional with the cache file writes or
// status flip that follow it; see BumpIndexGeneration for the crash-recovery
// contract.
func sqlBumpIndexGeneration(ctx context.Context, db sqlQueryable) (int64, error) {
	var next int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO DBConfig (Name, Value) VALUES (?, '1')
		 ON CONFLICT(Name) DO UPDATE SET Value = CAST(CAST(Value AS INTEGER) + 1 AS TEXT)
		 RETURNING CAST(Value AS INTEGER)`,
		DBConfigIndexGeneration,
	).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("failed to bump index generation: %w", err)
	}
	return next, nil
}

func sqlSetSystemListConfig(ctx context.Context, db sqlQueryable, name string, systemIDs []string) error {
	systemsJSON, err := json.Marshal(systemIDs)
	if err != nil {
		return fmt.Errorf("failed to marshal %s to JSON: %w", name, err)
	}
	_, err = db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		name,
		string(systemsJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to set %s: %w", name, err)
	}
	return nil
}

func sqlGetSystemListConfig(ctx context.Context, db *sql.DB, name string) ([]string, error) {
	var systemsJSON string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		name,
	).Scan(&systemsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get %s: %w", name, err)
	}

	var systemIDs []string
	err = json.Unmarshal([]byte(systemsJSON), &systemIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s: %w", name, err)
	}
	return systemIDs, nil
}

func sqlSetIndexingSystems(ctx context.Context, db sqlQueryable, systemIDs []string) error {
	return sqlSetSystemListConfig(ctx, db, DBConfigIndexingSystems, systemIDs)
}

func sqlGetIndexingSystems(ctx context.Context, db *sql.DB) ([]string, error) {
	return sqlGetSystemListConfig(ctx, db, DBConfigIndexingSystems)
}

func sqlSetIndexingPlanSystems(ctx context.Context, db sqlQueryable, systemIDs []string) error {
	return sqlSetSystemListConfig(ctx, db, DBConfigIndexingPlanSystems, systemIDs)
}

func sqlGetIndexingPlanSystems(ctx context.Context, db *sql.DB) ([]string, error) {
	return sqlGetSystemListConfig(ctx, db, DBConfigIndexingPlanSystems)
}
