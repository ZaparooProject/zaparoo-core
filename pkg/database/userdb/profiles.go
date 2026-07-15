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
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

var (
	// ErrProfileNotFound is returned when a profile lookup matches no row.
	ErrProfileNotFound = errors.New("profile not found")
	// ErrLastProfileAdmin is returned when an operation would remove the
	// final administrator profile.
	ErrLastProfileAdmin = errors.New("cannot remove the last admin profile")
)

func (db *UserDB) CreateProfile(p *database.Profile) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlCreateProfile(db.ctx, db.sql.Load(), p)
}

func (db *UserDB) GetProfile(profileID string) (*database.Profile, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	return sqlGetProfile(db.ctx, db.sql.Load(), "ProfileID", profileID)
}

func (db *UserDB) GetProfileBySwitchID(switchID string) (*database.Profile, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	return sqlGetProfile(db.ctx, db.sql.Load(), "SwitchID", switchID)
}

func (db *UserDB) ListProfiles() ([]database.Profile, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	return sqlListProfiles(db.ctx, db.sql.Load())
}

func (db *UserDB) UpdateProfile(p *database.Profile) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlUpdateProfile(db.ctx, db.sql.Load(), p)
}

// ActivateProfile atomically records profile use and persists it as active.
func (db *UserDB) ActivateProfile(profileID string, lastUsedAt int64) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlActivateProfile(db.ctx, db.sql.Load(), profileID, lastUsedAt)
}

// DeleteProfile removes a profile. If the profile is the device's active
// profile, the active-profile device state is cleared in the same
// transaction.
func (db *UserDB) DeleteProfile(profileID string) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlDeleteProfile(db.ctx, db.sql.Load(), profileID)
}

func (db *UserDB) SetDeviceState(key, value string) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlSetDeviceState(db.ctx, db.sql.Load(), key, value)
}

// GetDeviceState returns the value for key and whether it exists.
func (db *UserDB) GetDeviceState(key string) (value string, found bool, err error) {
	if db.sql.Load() == nil {
		return "", false, ErrNullSQL
	}
	return sqlGetDeviceState(db.ctx, db.sql.Load(), key)
}

func (db *UserDB) DeleteDeviceState(key string) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return sqlDeleteDeviceState(db.ctx, db.sql.Load(), key)
}

/*
 * Internal SQL functions
 */

const profileColumns = `DBID, ProfileID, Name, Role, SwitchID, PINHash, LimitsEnabled,
	DailyLimit, SessionLimit, CreatedAt, UpdatedAt, LastUsedAt`

func sqlCreateProfile(ctx context.Context, db *sql.DB, p *database.Profile) error {
	var dbid int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO Profiles (ProfileID, Name, Role, SwitchID, PINHash, LimitsEnabled,
			DailyLimit, SessionLimit, CreatedAt, UpdatedAt)
		VALUES (?, ?,
			CASE WHEN NOT EXISTS (SELECT 1 FROM Profiles) THEN 'admin' ELSE ? END,
			?, ?, ?, ?, ?, ?, ?)
		RETURNING DBID, Role;
	`, p.ProfileID, p.Name, p.Role, p.SwitchID, nullableString(p.PINHash),
		nullableBool(p.LimitsEnabled), p.DailyLimit, p.SessionLimit,
		p.CreatedAt, p.UpdatedAt).Scan(&dbid, &p.Role)
	if err != nil {
		return fmt.Errorf("failed to insert profile: %w", err)
	}
	p.DBID = dbid
	return nil
}

func sqlGetProfile(ctx context.Context, db *sql.DB, column, value string) (*database.Profile, error) {
	//nolint:gosec // column is a hardcoded column name, not user input
	row := db.QueryRowContext(ctx, `
		SELECT `+profileColumns+`
		FROM Profiles
		WHERE `+column+` = ?;
	`, value)

	p, err := scanProfile(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s=%s", ErrProfileNotFound, column, value)
		}
		return nil, fmt.Errorf("failed to scan profile row: %w", err)
	}
	return p, nil
}

func sqlListProfiles(ctx context.Context, db *sql.DB) ([]database.Profile, error) {
	list := make([]database.Profile, 0)

	rows, err := db.QueryContext(ctx, `
		SELECT `+profileColumns+`
		FROM Profiles
		ORDER BY CreatedAt ASC;
	`)
	if err != nil {
		return list, fmt.Errorf("failed to query profiles: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()

	for rows.Next() {
		p, scanErr := scanProfile(rows.Scan)
		if scanErr != nil {
			return list, fmt.Errorf("failed to scan profile row: %w", scanErr)
		}
		list = append(list, *p)
	}

	if err = rows.Err(); err != nil {
		return list, fmt.Errorf("error iterating profile rows: %w", err)
	}
	return list, nil
}

func sqlUpdateProfile(ctx context.Context, db *sql.DB, p *database.Profile) error {
	result, err := db.ExecContext(ctx, `
		UPDATE Profiles
		SET Name = ?, Role = ?, SwitchID = ?, PINHash = ?, LimitsEnabled = ?,
		    DailyLimit = ?, SessionLimit = ?, UpdatedAt = ?
		WHERE ProfileID = ?
		  AND (Role <> 'admin' OR ? = 'admin' OR
		       (SELECT COUNT(*) FROM Profiles WHERE Role = 'admin') > 1);
	`, p.Name, p.Role, p.SwitchID, nullableString(p.PINHash), nullableBool(p.LimitsEnabled),
		p.DailyLimit, p.SessionLimit, p.UpdatedAt, p.ProfileID, p.Role)
	if err != nil {
		return fmt.Errorf("failed to execute profile update: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		var role string
		queryErr := db.QueryRowContext(ctx, `SELECT Role FROM Profiles WHERE ProfileID = ?;`, p.ProfileID).Scan(&role)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, p.ProfileID)
		}
		if queryErr != nil {
			return fmt.Errorf("failed to inspect rejected profile update: %w", queryErr)
		}
		return ErrLastProfileAdmin
	}
	return nil
}

func sqlActivateProfile(ctx context.Context, db *sql.DB, profileID string, lastUsedAt int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin profile activation transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			log.Warn().Err(rollbackErr).Msg("failed to rollback profile activation transaction")
		}
	}()

	result, err := tx.ExecContext(ctx, `
		UPDATE Profiles SET LastUsedAt = ? WHERE ProfileID = ?;
	`, lastUsedAt, profileID)
	if err != nil {
		return fmt.Errorf("failed to update profile last used time: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get profile activation rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO DeviceState (Key, Value, UpdatedAt)
		VALUES (?, ?, ?)
		ON CONFLICT(Key) DO UPDATE SET Value = excluded.Value, UpdatedAt = excluded.UpdatedAt;
	`, database.DeviceStateKeyActiveProfile, profileID, lastUsedAt)
	if err != nil {
		return fmt.Errorf("failed to persist active profile state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit profile activation transaction: %w", err)
	}
	return nil
}

func sqlDeleteProfile(ctx context.Context, db *sql.DB, profileID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin profile delete transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			log.Warn().Err(rollbackErr).Msg("failed to rollback profile delete transaction")
		}
	}()

	result, err := tx.ExecContext(ctx, `
		DELETE FROM Profiles
		WHERE ProfileID = ?
		  AND (Role <> 'admin' OR
		       (SELECT COUNT(*) FROM Profiles WHERE Role = 'admin') > 1);
	`, profileID)
	if err != nil {
		return fmt.Errorf("failed to execute profile delete: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		var role string
		queryErr := tx.QueryRowContext(ctx, `SELECT Role FROM Profiles WHERE ProfileID = ?;`, profileID).Scan(&role)
		if errors.Is(queryErr, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrProfileNotFound, profileID)
		}
		if queryErr != nil {
			return fmt.Errorf("failed to inspect rejected profile delete: %w", queryErr)
		}
		return ErrLastProfileAdmin
	}

	_, err = tx.ExecContext(ctx, `
		DELETE FROM DeviceState WHERE Key = ? AND Value = ?;
	`, database.DeviceStateKeyActiveProfile, profileID)
	if err != nil {
		return fmt.Errorf("failed to clear active profile device state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit profile delete transaction: %w", err)
	}
	return nil
}

func sqlSetDeviceState(ctx context.Context, db *sql.DB, key, value string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO DeviceState (Key, Value, UpdatedAt)
		VALUES (?, ?, ?)
		ON CONFLICT(Key) DO UPDATE SET Value = excluded.Value, UpdatedAt = excluded.UpdatedAt;
	`, key, value, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to set device state: %w", err)
	}
	return nil
}

func sqlGetDeviceState(ctx context.Context, db *sql.DB, key string) (value string, found bool, err error) {
	err = db.QueryRowContext(ctx, `SELECT Value FROM DeviceState WHERE Key = ?;`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to query device state: %w", err)
	}
	return value, true, nil
}

func sqlDeleteDeviceState(ctx context.Context, db *sql.DB, key string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM DeviceState WHERE Key = ?;`, key)
	if err != nil {
		return fmt.Errorf("failed to delete device state: %w", err)
	}
	return nil
}

// scanProfile reads a profile row using the given scan function, converting
// nullable columns to their pointer/empty-string Go representations.
func scanProfile(scan func(dest ...any) error) (*database.Profile, error) {
	var p database.Profile
	var pinHash sql.NullString
	var limitsEnabled sql.NullBool
	var dailyLimit, sessionLimit sql.NullString
	var lastUsedAt sql.NullInt64

	err := scan(
		&p.DBID, &p.ProfileID, &p.Name, &p.Role, &p.SwitchID, &pinHash,
		&limitsEnabled, &dailyLimit, &sessionLimit, &p.CreatedAt, &p.UpdatedAt, &lastUsedAt,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // callers wrap with query context
	}

	if pinHash.Valid {
		p.PINHash = pinHash.String
	}
	if limitsEnabled.Valid {
		p.LimitsEnabled = &limitsEnabled.Bool
	}
	if dailyLimit.Valid {
		p.DailyLimit = &dailyLimit.String
	}
	if sessionLimit.Valid {
		p.SessionLimit = &sessionLimit.String
	}
	if lastUsedAt.Valid {
		p.LastUsedAt = &lastUsedAt.Int64
	}
	return &p, nil
}

// nullableString stores empty strings as NULL.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableBool stores nil as NULL and otherwise 0/1.
func nullableBool(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}
