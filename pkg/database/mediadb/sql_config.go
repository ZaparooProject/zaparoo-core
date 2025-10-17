// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
)

const (
	DBConfigLastGeneratedAt    = "LastGeneratedAt"
	DBConfigOptimizationStatus = "OptimizationStatus"
	DBConfigOptimizationStep   = "OptimizationStep"
	DBConfigIndexingStatus     = "IndexingStatus"
	DBConfigLastIndexedSystem  = "LastIndexedSystem"
	DBConfigIndexingSystems    = "IndexingSystems"
)

func sqlUpdateLastGenerated(ctx context.Context, db *sql.DB) error {
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

func sqlSetOptimizationStatus(ctx context.Context, db *sql.DB, status string) error {
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

func sqlSetOptimizationStep(ctx context.Context, db *sql.DB, step string) error {
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

func sqlSetIndexingStatus(ctx context.Context, db *sql.DB, status string) error {
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

func sqlSetLastIndexedSystem(ctx context.Context, db *sql.DB, systemID string) error {
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

func sqlSetIndexingSystems(ctx context.Context, db *sql.DB, systemIDs []string) error {
	systemsJSON, err := json.Marshal(systemIDs)
	if err != nil {
		return fmt.Errorf("failed to marshal systems to JSON: %w", err)
	}
	_, err = db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigIndexingSystems,
		string(systemsJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to set indexing systems: %w", err)
	}
	return nil
}

func sqlGetIndexingSystems(ctx context.Context, db *sql.DB) ([]string, error) {
	var systemsJSON string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigIndexingSystems,
	).Scan(&systemsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get indexing systems: %w", err)
	}

	var systemIDs []string
	err = json.Unmarshal([]byte(systemsJSON), &systemIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal indexing systems: %w", err)
	}
	return systemIDs, nil
}
