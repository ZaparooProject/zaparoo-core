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

package userdb

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	goose "github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"
)

// Queries go here to keep the interface clean

//go:embed migrations/*.sql
var migrationFiles embed.FS

func sqlMigrateUp(db *sql.DB) error {
	goose.SetBaseFS(migrationFiles)

	if err := goose.SetDialect("sqlite"); err != nil {
		return fmt.Errorf("error setting goose dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("error running migrations up: %w", err)
	}

	return nil
}

func sqlAllocate(db *sql.DB) error {
	return sqlMigrateUp(db)
}

//goland:noinspection SqlWithoutWhere
func sqlTruncate(ctx context.Context, db *sql.DB) error {
	sqlStmt := `
	delete from History;
	delete from Mappings;
	vacuum;
	`
	_, err := db.ExecContext(ctx, sqlStmt)
	if err != nil {
		return fmt.Errorf("failed to truncate database: %w", err)
	}
	return nil
}

func sqlVacuum(ctx context.Context, db *sql.DB) error {
	sqlStmt := `
	vacuum;
	`
	_, err := db.ExecContext(ctx, sqlStmt)
	if err != nil {
		return fmt.Errorf("failed to vacuum database: %w", err)
	}
	return nil
}

//nolint:gocritic // struct passed for DB insertion
func sqlAddHistory(ctx context.Context, db *sql.DB, entry database.HistoryEntry) error {
	stmt, err := db.PrepareContext(ctx, `
		insert into History(
			Time, Type, TokenID, TokenValue, TokenData, Success
		) values (?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare history insert statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	_, err = stmt.ExecContext(ctx,
		entry.Time.Unix(),
		entry.Type,
		entry.TokenID,
		entry.TokenValue,
		entry.TokenData,
		entry.Success,
	)
	if err != nil {
		return fmt.Errorf("failed to execute history insert: %w", err)
	}
	return nil
}

func sqlGetHistoryWithOffset(ctx context.Context, db *sql.DB, lastID int) ([]database.HistoryEntry, error) {
	list := make([]database.HistoryEntry, 0, 25)
	// Instead of offset, use token-based
	if lastID == 0 {
		lastID = 2147483646
	}

	q, err := db.PrepareContext(ctx, `
		select 
		DBID, Time, Type, TokenID, TokenValue, TokenData, Success
		from History
		where DBID < ?
		order by DBID DESC
		limit 25;
	`)
	if err != nil {
		return list, fmt.Errorf("failed to prepare history query statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx, lastID)
	if err != nil {
		return list, fmt.Errorf("failed to query history: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		row := database.HistoryEntry{}
		var timeInt int64
		scanErr := rows.Scan(
			&row.DBID,
			&timeInt,
			&row.Type,
			&row.TokenID,
			&row.TokenValue,
			&row.TokenData,
			&row.Success,
		)
		if scanErr != nil {
			return list, fmt.Errorf("failed to scan history row: %w", scanErr)
		}
		row.Time = time.Unix(timeInt, 0)
		list = append(list, row)
	}
	if err = rows.Err(); err != nil {
		return list, fmt.Errorf("error iterating history rows: %w", err)
	}
	return list, nil
}

//nolint:gocritic // struct passed for DB insertion
func sqlAddMapping(ctx context.Context, db *sql.DB, m database.Mapping) error {
	stmt, err := db.PrepareContext(ctx, `
		insert into Mappings(
			Added, Label, Enabled, Type, Match, Pattern, Override
		) values (?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare mapping insert statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	_, err = stmt.ExecContext(ctx,
		m.Added,
		m.Label,
		m.Enabled,
		m.Type,
		m.Match,
		m.Pattern,
		m.Override,
	)
	if err != nil {
		return fmt.Errorf("failed to execute mapping insert: %w", err)
	}
	return nil
}

func sqlGetMapping(ctx context.Context, db *sql.DB, id int64) (database.Mapping, error) {
	var row database.Mapping
	q, err := db.PrepareContext(ctx, `
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where DBID = ?;
	`)
	if err != nil {
		return row, fmt.Errorf("failed to prepare mapping select statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	err = q.QueryRowContext(ctx, id).Scan(
		&row.DBID,
		&row.Added,
		&row.Label,
		&row.Enabled,
		&row.Type,
		&row.Match,
		&row.Pattern,
		&row.Override,
	)
	if err != nil {
		return row, fmt.Errorf("failed to scan mapping row: %w", err)
	}
	return row, nil
}

func sqlDeleteMapping(ctx context.Context, db *sql.DB, id int64) error {
	stmt, err := db.PrepareContext(ctx, `
		delete from Mappings where DBID = ?;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare mapping delete statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	_, err = stmt.ExecContext(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to execute mapping delete: %w", err)
	}
	return nil
}

//nolint:gocritic // struct passed for DB update
func sqlUpdateMapping(ctx context.Context, db *sql.DB, id int64, m database.Mapping) error {
	stmt, err := db.PrepareContext(ctx, `
		update Mappings set
			Added = ?,
			Label = ?,
			Enabled = ?,
			Type = ?,
			Match = ?,
			Pattern = ?,
			Override = ?
		where
			DBID = ?;
	`)
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	if err != nil {
		return err
	}
	_, err = stmt.ExecContext(ctx,
		m.Added,
		m.Label,
		m.Enabled,
		m.Type,
		m.Match,
		m.Pattern,
		m.Override,
		id,
	)
	return err
}

func sqlGetAllMappings(ctx context.Context, db *sql.DB) ([]database.Mapping, error) {
	list := make([]database.Mapping, 0)

	q, err := db.PrepareContext(ctx, `
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings;
	`)
	if err != nil {
		return list, err
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx)
	if err != nil {
		return list, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		row := database.Mapping{}
		scanErr := rows.Scan(
			&row.DBID,
			&row.Added,
			&row.Label,
			&row.Enabled,
			&row.Type,
			&row.Match,
			&row.Pattern,
			&row.Override,
		)
		if scanErr != nil {
			return list, scanErr
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlGetEnabledMappings(ctx context.Context, db *sql.DB) ([]database.Mapping, error) {
	list := make([]database.Mapping, 0)

	q, err := db.PrepareContext(ctx, `
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings
		where Enabled = ?
	`)
	if err != nil {
		return list, err
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx, true)
	if err != nil {
		return list, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		row := database.Mapping{}
		scanErr := rows.Scan(
			&row.DBID,
			&row.Added,
			&row.Label,
			&row.Enabled,
			&row.Type,
			&row.Match,
			&row.Pattern,
			&row.Override,
		)
		if scanErr != nil {
			return list, scanErr
		}
		list = append(list, row)
	}
	err = rows.Err()
	return list, err
}

func sqlUpdateZapLinkHost(ctx context.Context, db *sql.DB, host string, zapscript int) error {
	stmt, err := db.PrepareContext(ctx, `
		INSERT INTO ZapLinkHosts (Host, ZapScript, CheckedAt)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(Host) DO UPDATE SET
			ZapScript = excluded.ZapScript,
			CheckedAt = CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	_, err = stmt.ExecContext(ctx, host, zapscript)
	return err
}

func sqlGetZapLinkHost(ctx context.Context, db *sql.DB, host string) (supported, ok bool, err error) {
	row := db.QueryRowContext(ctx, `
		SELECT ZapScript FROM ZapLinkHosts WHERE Host = ?;
	`, host)

	var zapscript int
	err = row.Scan(&zapscript)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	} else if err != nil {
		return false, false, err
	}

	return zapscript != 0, true, nil
}

func sqlUpdateZapLinkCache(ctx context.Context, db *sql.DB, url, zapscript string) error {
	stmt, err := db.PrepareContext(ctx, `
		INSERT INTO ZapLinkCache (URL, ZapScript, UpdatedAt)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(URL) DO UPDATE SET
			ZapScript = excluded.ZapScript,
			UpdatedAt = CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	_, err = stmt.ExecContext(ctx, url, zapscript)
	return err
}

func sqlGetZapLinkCache(ctx context.Context, db *sql.DB, url string) (string, error) {
	var zapscript string
	err := db.QueryRowContext(ctx,
		`SELECT ZapScript FROM ZapLinkCache WHERE URL = ?;`,
		url,
	).Scan(&zapscript)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return zapscript, nil
}
