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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// Queries go here to keep the interface clean

//go:embed migrations/*.sql
var migrationFiles embed.FS

func sqlMigrateUp(db *sql.DB) error {
	if err := database.MigrateUp(db, migrationFiles, "migrations"); err != nil {
		return fmt.Errorf("failed to run user database migrations: %w", err)
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

func sqlCleanupHistory(ctx context.Context, db *sql.DB, retentionDays int) (int64, error) {
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays).Unix()

	stmt, err := db.PrepareContext(ctx, `DELETE FROM History WHERE Time < ?;`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare history cleanup statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	result, err := stmt.ExecContext(ctx, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to execute history cleanup: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	// Vacuum to reclaim disk space after cleanup
	if rowsAffected > 0 {
		if err := sqlVacuum(ctx, db); err != nil {
			return rowsAffected, fmt.Errorf("cleanup succeeded but vacuum failed: %w", err)
		}
	}

	return rowsAffected, nil
}

//nolint:gocritic // struct passed for DB insertion
func sqlAddHistory(ctx context.Context, db *sql.DB, entry database.HistoryEntry) error {
	stmt, err := db.PrepareContext(ctx, `
		insert into History(
			ID, Time, Type, TokenID, TokenValue, TokenData, Success,
			ClockReliable, BootUUID, MonotonicStart, CreatedAt, DeviceID
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare history insert statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	var deviceID any
	if entry.DeviceID != nil {
		deviceID = *entry.DeviceID
	}

	_, err = stmt.ExecContext(ctx,
		entry.ID,
		entry.Time.Unix(),
		entry.Type,
		entry.TokenID,
		entry.TokenValue,
		entry.TokenData,
		entry.Success,
		entry.ClockReliable,
		entry.BootUUID,
		entry.MonotonicStart,
		entry.CreatedAt.Unix(),
		deviceID,
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
		DBID, ID, Time, Type, TokenID, TokenValue, TokenData, Success,
		ClockReliable, BootUUID, MonotonicStart, CreatedAt, DeviceID
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
		var timeInt, createdAtInt int64
		var id sql.NullString
		var deviceID sql.NullString

		scanErr := rows.Scan(
			&row.DBID,
			&id,
			&timeInt,
			&row.Type,
			&row.TokenID,
			&row.TokenValue,
			&row.TokenData,
			&row.Success,
			&row.ClockReliable,
			&row.BootUUID,
			&row.MonotonicStart,
			&createdAtInt,
			&deviceID,
		)
		if scanErr != nil {
			return list, fmt.Errorf("failed to scan history row: %w", scanErr)
		}

		if id.Valid {
			row.ID = id.String
		}
		if deviceID.Valid {
			deviceStr := deviceID.String
			row.DeviceID = &deviceStr
		}

		row.Time = time.Unix(timeInt, 0)
		row.CreatedAt = time.Unix(createdAtInt, 0)
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
		return fmt.Errorf("failed to prepare update mapping statement: %w", err)
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
	if err != nil {
		return fmt.Errorf("failed to execute update mapping statement: %w", err)
	}
	return nil
}

func sqlGetAllMappings(ctx context.Context, db *sql.DB) ([]database.Mapping, error) {
	list := make([]database.Mapping, 0)

	q, err := db.PrepareContext(ctx, `
		select
		DBID, Added, Label, Enabled, Type, Match, Pattern, Override
		from Mappings;
	`)
	if err != nil {
		return list, fmt.Errorf("failed to prepare get all mappings statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx)
	if err != nil {
		return list, fmt.Errorf("failed to execute get all mappings query: %w", err)
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
			return list, fmt.Errorf("failed to scan mapping row: %w", scanErr)
		}
		list = append(list, row)
	}
	err = rows.Err()
	if err != nil {
		return list, fmt.Errorf("failed to iterate over mapping rows: %w", err)
	}
	return list, nil
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
		return list, fmt.Errorf("failed to prepare enabled mappings statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx, true)
	if err != nil {
		return list, fmt.Errorf("failed to execute enabled mappings query: %w", err)
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
			return list, fmt.Errorf("failed to scan enabled mapping row: %w", scanErr)
		}
		list = append(list, row)
	}
	err = rows.Err()
	if err != nil {
		return list, fmt.Errorf("failed to iterate over enabled mapping rows: %w", err)
	}
	return list, nil
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
		return fmt.Errorf("failed to prepare update zap link host statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	_, err = stmt.ExecContext(ctx, host, zapscript)
	if err != nil {
		return fmt.Errorf("failed to execute update zap link host statement: %w", err)
	}
	return nil
}

func sqlGetZapLinkHost(ctx context.Context, db *sql.DB, host string) (found, zapScript bool, err error) {
	row := db.QueryRowContext(ctx, `
		SELECT ZapScript FROM ZapLinkHosts WHERE Host = ?;
	`, host)

	var zapscript int
	err = row.Scan(&zapscript)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	} else if err != nil {
		return false, false, fmt.Errorf("failed to scan zap link host row: %w", err)
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
		return fmt.Errorf("failed to prepare update zap link cache statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	_, err = stmt.ExecContext(ctx, url, zapscript)
	if err != nil {
		return fmt.Errorf("failed to execute update zap link cache statement: %w", err)
	}
	return nil
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
		return "", fmt.Errorf("failed to scan zap link cache row: %w", err)
	}
	return zapscript, nil
}

func sqlGetSupportedZapLinkHosts(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT Host FROM ZapLinkHosts WHERE ZapScript > 0;`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query supported zap link hosts: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close rows")
		}
	}()

	hosts := make([]string, 0)
	for rows.Next() {
		var host string
		if scanErr := rows.Scan(&host); scanErr != nil {
			return nil, fmt.Errorf("failed to scan zap link host row: %w", scanErr)
		}
		hosts = append(hosts, host)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating zap link host rows: %w", err)
	}

	return hosts, nil
}

func sqlPruneExpiredZapLinkHosts(ctx context.Context, db *sql.DB, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan).Format(time.RFC3339)
	result, err := db.ExecContext(ctx,
		`DELETE FROM ZapLinkHosts WHERE ZapScript = 0 AND datetime(CheckedAt) < datetime(?);`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to prune expired zap link hosts: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rowsAffected, nil
}

func sqlAddInboxMessage(ctx context.Context, db *sql.DB, msg *database.InboxMessage) (*database.InboxMessage, error) {
	var dbid int64

	if msg.Category != "" {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() {
			if err != nil {
				_ = tx.Rollback()
			}
		}()

		// Check if a message with this category+profile already exists
		var existingID int64
		err = tx.QueryRowContext(ctx,
			`SELECT DBID FROM Inbox WHERE Category = ? AND ProfileID = ?`,
			msg.Category, msg.ProfileID,
		).Scan(&existingID)

		switch {
		case err == nil:
			// Message exists, update it
			_, err = tx.ExecContext(ctx, `
				UPDATE Inbox SET Title = ?, Body = ?, Severity = ?, CreatedAt = ?
				WHERE DBID = ?
			`, msg.Title, msg.Body, msg.Severity, msg.CreatedAt.Unix(), existingID)
			if err != nil {
				return nil, fmt.Errorf("failed to update inbox message: %w", err)
			}
			dbid = existingID
		case errors.Is(err, sql.ErrNoRows):
			// Message doesn't exist, insert new
			err = tx.QueryRowContext(ctx, `
				INSERT INTO Inbox (Title, Body, Severity, Category, ProfileID, CreatedAt)
				VALUES (?, ?, ?, ?, ?, ?)
				RETURNING DBID;
			`, msg.Title, msg.Body, msg.Severity, msg.Category, msg.ProfileID, msg.CreatedAt.Unix()).Scan(&dbid)
			if err != nil {
				return nil, fmt.Errorf("failed to insert inbox message: %w", err)
			}
		default:
			return nil, fmt.Errorf("failed to check existing inbox message: %w", err)
		}

		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}
	} else {
		// No category: always insert new message
		err := db.QueryRowContext(ctx, `
			INSERT INTO Inbox (Title, Body, Severity, Category, ProfileID, CreatedAt)
			VALUES (?, ?, ?, NULL, ?, ?)
			RETURNING DBID;
		`, msg.Title, msg.Body, msg.Severity, msg.ProfileID, msg.CreatedAt.Unix()).Scan(&dbid)
		if err != nil {
			return nil, fmt.Errorf("failed to insert inbox message: %w", err)
		}
	}

	return &database.InboxMessage{
		DBID:      dbid,
		Title:     msg.Title,
		Body:      msg.Body,
		Severity:  msg.Severity,
		Category:  msg.Category,
		ProfileID: msg.ProfileID,
		CreatedAt: msg.CreatedAt,
	}, nil
}

func sqlGetInboxMessages(ctx context.Context, db *sql.DB) ([]database.InboxMessage, error) {
	list := make([]database.InboxMessage, 0)

	q, err := db.PrepareContext(ctx, `
		SELECT DBID, Title, Body, Severity, Category, ProfileID, CreatedAt
		FROM Inbox
		ORDER BY CreatedAt DESC
		LIMIT 100;
	`)
	if err != nil {
		return list, fmt.Errorf("failed to prepare inbox query statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx)
	if err != nil {
		return list, fmt.Errorf("failed to execute inbox query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	for rows.Next() {
		row := database.InboxMessage{}
		var createdAtSec int64
		var body, category sql.NullString

		scanErr := rows.Scan(&row.DBID, &row.Title, &body, &row.Severity, &category, &row.ProfileID, &createdAtSec)
		if scanErr != nil {
			return list, fmt.Errorf("failed to scan inbox row: %w", scanErr)
		}

		if body.Valid {
			row.Body = body.String
		}
		if category.Valid {
			row.Category = category.String
		}
		row.CreatedAt = time.Unix(createdAtSec, 0)
		list = append(list, row)
	}

	if err = rows.Err(); err != nil {
		return list, fmt.Errorf("error iterating inbox rows: %w", err)
	}

	return list, nil
}

func sqlDeleteInboxMessage(ctx context.Context, db *sql.DB, id int64) error {
	result, err := db.ExecContext(ctx, `DELETE FROM Inbox WHERE DBID = ?;`, id)
	if err != nil {
		return fmt.Errorf("failed to execute inbox delete: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("inbox message not found: %d", id)
	}

	return nil
}

//goland:noinspection SqlWithoutWhere
func sqlDeleteAllInboxMessages(ctx context.Context, db *sql.DB) (int64, error) {
	result, err := db.ExecContext(ctx, `DELETE FROM Inbox;`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete all inbox messages: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}
