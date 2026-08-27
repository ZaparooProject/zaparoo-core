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

package userdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

func (db *UserDB) ClaimRemoteCommand(command *database.RemoteCommand) (*database.RemoteCommand, bool, error) {
	return sqlClaimRemoteCommand(db.ctx, db.sql.Load(), command)
}

func (db *UserDB) TransitionRemoteCommand(
	commandID, fromState, toState string, executionExpiresAt *time.Time,
) (bool, error) {
	return sqlTransitionRemoteCommand(db.ctx, db.sql.Load(), commandID, fromState, toState, executionExpiresAt)
}

func (db *UserDB) StoreRemoteCommandResult(
	commandID, fromState, status string, result json.RawMessage, errorCode string,
) (bool, error) {
	return sqlStoreRemoteCommandResult(db.ctx, db.sql.Load(), commandID, fromState, status, result, errorCode)
}

func (db *UserDB) MarkRemoteCommandResultReported(commandID string) error {
	return sqlMarkRemoteCommandResultReported(db.ctx, db.sql.Load(), commandID)
}

// ListUnreportedRemoteCommands returns terminal commands whose result has
// not yet been posted, oldest first. limit bounds a single call so the
// caller can post the batch a little at a time; see remote.replayStoredResults.
func (db *UserDB) ListUnreportedRemoteCommands(limit int) ([]database.RemoteCommand, error) {
	return sqlListUnreportedRemoteCommands(db.ctx, db.sql.Load(), limit)
}

// ListRecentRemoteCommands returns the most recently created remote commands,
// newest first, for display as an owner-facing activity log. limit is
// clamped to at least 1 by the caller.
func (db *UserDB) ListRecentRemoteCommands(limit int) ([]database.RemoteCommand, error) {
	return sqlListRecentRemoteCommands(db.ctx, db.sql.Load(), limit)
}

func (db *UserDB) PruneRemoteCommands(before time.Time) (int64, error) {
	return sqlPruneRemoteCommands(db.ctx, db.sql.Load(), before)
}

func sqlClaimRemoteCommand(
	ctx context.Context, db *sql.DB, command *database.RemoteCommand,
) (*database.RemoteCommand, bool, error) {
	now := time.Now().UTC()
	if command.CreatedAt.IsZero() {
		command.CreatedAt = now
	}
	command.UpdatedAt = now
	result, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO RemoteCommands (
			CommandID, OperationID, OperationType, ProtocolVersion, ParamsDigest,
			Origin, DeadlineAt, State, CreatedAt, UpdatedAt
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`,
		command.CommandID, command.OperationID, command.OperationType, command.ProtocolVersion,
		command.ParamsDigest, string(command.Origin), command.DeadlineAt.UnixNano(), command.State,
		command.CreatedAt.UnixNano(), command.UpdatedAt.UnixNano(),
	)
	if err != nil {
		return nil, false, fmt.Errorf("claim remote command: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("inspect remote command claim: %w", err)
	}
	stored, err := sqlGetRemoteCommand(ctx, db, command.CommandID)
	if err != nil {
		return nil, false, err
	}
	return stored, rows == 1, nil
}

func sqlGetRemoteCommand(ctx context.Context, db *sql.DB, commandID string) (*database.RemoteCommand, error) {
	row := db.QueryRowContext(ctx, `
		SELECT CommandID, OperationID, OperationType, ProtocolVersion, ParamsDigest,
			Origin, DeadlineAt, ExecutionExpiresAt, State, ResultStatus, Result,
			ErrorCode, ResultReported, CreatedAt, UpdatedAt
		FROM RemoteCommands WHERE CommandID = ?;`, commandID)
	command, err := scanRemoteCommand(row)
	if err != nil {
		return nil, fmt.Errorf("get remote command: %w", err)
	}
	return &command, nil
}

func sqlTransitionRemoteCommand(
	ctx context.Context, db *sql.DB, commandID, fromState, toState string, executionExpiresAt *time.Time,
) (bool, error) {
	var expires any
	if executionExpiresAt != nil {
		expires = executionExpiresAt.UnixNano()
	}
	result, err := db.ExecContext(ctx, `
		UPDATE RemoteCommands
		SET State = ?, ExecutionExpiresAt = COALESCE(?, ExecutionExpiresAt), UpdatedAt = ?
		WHERE CommandID = ? AND State = ?;`,
		toState, expires, time.Now().UTC().UnixNano(), commandID, fromState)
	if err != nil {
		return false, fmt.Errorf("transition remote command: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect remote command transition: %w", err)
	}
	return rows == 1, nil
}

func sqlStoreRemoteCommandResult(
	ctx context.Context, db *sql.DB, commandID, fromState, status string,
	resultJSON json.RawMessage, errorCode string,
) (bool, error) {
	var resultValue any
	if len(resultJSON) > 0 {
		resultValue = string(resultJSON)
	}
	var errorValue any
	if errorCode != "" {
		errorValue = errorCode
	}
	result, err := db.ExecContext(ctx, `
		UPDATE RemoteCommands
		SET State = 'terminal', ResultStatus = ?, Result = ?, ErrorCode = ?,
			ResultReported = 0, UpdatedAt = ?
		WHERE CommandID = ? AND State = ?;`,
		status, resultValue, errorValue, time.Now().UTC().UnixNano(), commandID, fromState)
	if err != nil {
		return false, fmt.Errorf("store remote command result: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect remote command result update: %w", err)
	}
	return rows == 1, nil
}

func sqlMarkRemoteCommandResultReported(ctx context.Context, db *sql.DB, commandID string) error {
	result, err := db.ExecContext(ctx, `
		UPDATE RemoteCommands SET ResultReported = 1, UpdatedAt = ?
		WHERE CommandID = ? AND State = 'terminal';`, time.Now().UTC().UnixNano(), commandID)
	if err != nil {
		return fmt.Errorf("mark remote command result reported: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect remote command result report: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("remote command not terminal: %s", commandID)
	}
	return nil
}

func sqlListUnreportedRemoteCommands(ctx context.Context, db *sql.DB, limit int) ([]database.RemoteCommand, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT CommandID, OperationID, OperationType, ProtocolVersion, ParamsDigest,
			Origin, DeadlineAt, ExecutionExpiresAt, State, ResultStatus, Result,
			ErrorCode, ResultReported, CreatedAt, UpdatedAt
		FROM RemoteCommands
		WHERE State = 'terminal' AND ResultReported = 0
		ORDER BY CreatedAt
		LIMIT ?;`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unreported remote commands: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("failed to close remote command rows")
		}
	}()
	commands := make([]database.RemoteCommand, 0)
	for rows.Next() {
		command, scanErr := scanRemoteCommand(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan unreported remote command: %w", scanErr)
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unreported remote commands: %w", err)
	}
	return commands, nil
}

func sqlListRecentRemoteCommands(ctx context.Context, db *sql.DB, limit int) ([]database.RemoteCommand, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT CommandID, OperationID, OperationType, ProtocolVersion, ParamsDigest,
			Origin, DeadlineAt, ExecutionExpiresAt, State, ResultStatus, Result,
			ErrorCode, ResultReported, CreatedAt, UpdatedAt
		FROM RemoteCommands
		ORDER BY CreatedAt DESC
		LIMIT ?;`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent remote commands: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("failed to close remote command rows")
		}
	}()
	commands := make([]database.RemoteCommand, 0)
	for rows.Next() {
		command, scanErr := scanRemoteCommand(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan recent remote command: %w", scanErr)
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent remote commands: %w", err)
	}
	return commands, nil
}

func sqlPruneRemoteCommands(ctx context.Context, db *sql.DB, before time.Time) (int64, error) {
	result, err := db.ExecContext(ctx, `DELETE FROM RemoteCommands WHERE DeadlineAt < ?;`, before.UnixNano())
	if err != nil {
		return 0, fmt.Errorf("prune remote commands: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("inspect remote command prune: %w", err)
	}
	return rows, nil
}

type remoteCommandScanner interface {
	Scan(dest ...any) error
}

func scanRemoteCommand(scanner remoteCommandScanner) (database.RemoteCommand, error) {
	var command database.RemoteCommand
	var origin string
	var deadlineAt, createdAt, updatedAt int64
	var executionExpiresAt sql.NullInt64
	var resultStatus, resultJSON, errorCode sql.NullString
	var reported bool
	if err := scanner.Scan(
		&command.CommandID, &command.OperationID, &command.OperationType, &command.ProtocolVersion,
		&command.ParamsDigest, &origin, &deadlineAt, &executionExpiresAt, &command.State,
		&resultStatus, &resultJSON, &errorCode, &reported, &createdAt, &updatedAt,
	); err != nil {
		return command, fmt.Errorf("scan remote command row: %w", err)
	}
	command.Origin = json.RawMessage(origin)
	command.DeadlineAt = time.Unix(0, deadlineAt).UTC()
	command.CreatedAt = time.Unix(0, createdAt).UTC()
	command.UpdatedAt = time.Unix(0, updatedAt).UTC()
	command.ResultReported = reported
	if executionExpiresAt.Valid {
		expires := time.Unix(0, executionExpiresAt.Int64).UTC()
		command.ExecutionExpiresAt = &expires
	}
	if resultStatus.Valid {
		command.ResultStatus = resultStatus.String
	}
	if resultJSON.Valid {
		command.Result = json.RawMessage(resultJSON.String)
	}
	if errorCode.Valid {
		command.ErrorCode = errorCode.String
	}
	return command, nil
}
