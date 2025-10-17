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
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

const insertSystemSQL = `INSERT INTO Systems (DBID, SystemID, Name) VALUES (?, ?, ?)`

func sqlFindSystem(ctx context.Context, db *sql.DB, system database.System) (database.System, error) {
	var row database.System
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, SystemID, Name
		from Systems
		where DBID = ?
		or SystemID = ?
		limit 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find system statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx,
		system.DBID,
		system.SystemID,
	).Scan(
		&row.DBID,
		&row.SystemID,
		&row.Name,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan system row: %w", err)
	}
	return row, nil
}

func sqlFindSystemBySystemID(ctx context.Context, db *sql.DB, systemID string) (database.System, error) {
	var row database.System
	stmt, err := db.PrepareContext(ctx, `
		select
		DBID, SystemID, Name
		from Systems
		where SystemID = ?
		limit 1;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare find system by system ID statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = stmt.QueryRowContext(ctx, systemID).Scan(
		&row.DBID,
		&row.SystemID,
		&row.Name,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan system row: %w", err)
	}
	return row, nil
}

func sqlInsertSystemWithPreparedStmt(
	ctx context.Context, stmt *sql.Stmt, row database.System,
) (database.System, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	res, err := stmt.ExecContext(ctx, dbID, row.SystemID, row.Name)
	if err != nil {
		return row, fmt.Errorf("failed to execute prepared insert system statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for system: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlInsertSystem(ctx context.Context, db *sql.DB, row database.System) (database.System, error) {
	var dbID any
	if row.DBID != 0 {
		dbID = row.DBID
	}

	stmt, err := db.PrepareContext(ctx, insertSystemSQL)
	if err != nil {
		return row, fmt.Errorf("failed to prepare insert system statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	res, err := stmt.ExecContext(ctx, dbID, row.SystemID, row.Name)
	if err != nil {
		return row, fmt.Errorf("failed to execute insert system statement: %w", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return row, fmt.Errorf("failed to get last insert ID for system: %w", err)
	}

	row.DBID = lastID
	return row, nil
}

func sqlSystemIndexed(ctx context.Context, db *sql.DB, system *systemdefs.System) bool {
	systemID := ""
	q, err := db.PrepareContext(ctx, `
		select
		SystemID
		from Systems
		where SystemID = ?;
	`)
	if err != nil {
		return false
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = q.QueryRowContext(ctx, system.ID).Scan(&systemID)
	if err != nil {
		return false
	}
	return systemID == system.ID
}

func sqlIndexedSystems(ctx context.Context, db *sql.DB) ([]string, error) {
	list := make([]string, 0)

	q, err := db.PrepareContext(ctx, `
		select SystemID from Systems;
	`)
	if err != nil {
		return list, fmt.Errorf("failed to prepare indexed systems query: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx)
	if err != nil {
		return list, fmt.Errorf("failed to execute indexed systems query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		row := ""
		if scanErr := rows.Scan(&row); scanErr != nil {
			return list, fmt.Errorf("failed to scan indexed systems result: %w", scanErr)
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlGetAllSystems(ctx context.Context, db *sql.DB) ([]database.System, error) {
	rows, err := db.QueryContext(ctx, "SELECT DBID, SystemID, Name FROM Systems ORDER BY DBID")
	if err != nil {
		return nil, fmt.Errorf("failed to query systems: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	systems := make([]database.System, 0)
	for rows.Next() {
		var system database.System
		if err := rows.Scan(&system.DBID, &system.SystemID, &system.Name); err != nil {
			return nil, fmt.Errorf("failed to scan system: %w", err)
		}
		systems = append(systems, system)
	}
	return systems, rows.Err()
}

// sqlGetSystemsExcluding retrieves all systems except those in the excludeSystemIDs list
func sqlGetSystemsExcluding(ctx context.Context, db *sql.DB, excludeSystemIDs []string) ([]database.System, error) {
	if len(excludeSystemIDs) == 0 {
		return sqlGetAllSystems(ctx, db)
	}

	// Build placeholders for the IN clause
	placeholders := make([]string, len(excludeSystemIDs))
	args := make([]any, len(excludeSystemIDs))
	for i, systemID := range excludeSystemIDs {
		placeholders[i] = "?"
		args[i] = systemID
	}

	//nolint:gosec // using parameterized placeholders, not user input
	query := fmt.Sprintf(
		"SELECT DBID, SystemID, Name FROM Systems WHERE SystemID NOT IN (%s) ORDER BY DBID",
		strings.Join(placeholders, ","),
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query systems excluding %v: %w", excludeSystemIDs, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	systems := make([]database.System, 0)
	for rows.Next() {
		var system database.System
		if err := rows.Scan(&system.DBID, &system.SystemID, &system.Name); err != nil {
			return nil, fmt.Errorf("failed to scan system: %w", err)
		}
		systems = append(systems, system)
	}
	return systems, rows.Err()
}
