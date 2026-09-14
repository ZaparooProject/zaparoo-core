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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/rs/zerolog/log"
)

const mediaUserDataColumns = `
	DBID, SystemID, Path, IsFavorite, IsHidden, IsLiked, IsDisliked, IsPlayLater,
	LauncherOverride, MediaName, Slug, Tags, CreatedAt, UpdatedAt`

// mediaUserNoIntent is the predicate for a row that records nothing and is
// pruned rather than kept.
const mediaUserNoIntent = `IsFavorite = 0 and IsHidden = 0 and IsLiked = 0 and IsDisliked = 0` +
	` and IsPlayLater = 0 and LauncherOverride = ''`

// mediaUserFlagColumn maps a preference flag to its column and the columns a
// true value clears, so an exclusive pair can never be written together.
type mediaUserFlagColumn struct {
	column string
	clears []string
}

var mediaUserFlagColumns = map[database.MediaUserFlag]mediaUserFlagColumn{
	database.MediaUserFlagFavorite:  {column: "IsFavorite", clears: []string{"IsDisliked"}},
	database.MediaUserFlagHidden:    {column: "IsHidden"},
	database.MediaUserFlagLiked:     {column: "IsLiked", clears: []string{"IsDisliked"}},
	database.MediaUserFlagDisliked:  {column: "IsDisliked", clears: []string{"IsLiked", "IsFavorite"}},
	database.MediaUserFlagPlayLater: {column: "IsPlayLater"},
}

// GetMediaUserData returns the user-data row for a media path. The bool is false
// when no row exists for the (systemID, path) key, in which case the media has no
// user preferences recorded.
func (db *UserDB) GetMediaUserData(systemID, path string) (database.MediaUserData, bool, error) {
	return sqlGetMediaUserData(db.ctx, db.sql.Load(), systemID, pathutil.CanonicalMediaPath(path))
}

// UpsertMediaUserData inserts or updates the user-data row for (SystemID, Path).
// CreatedAt is set on insert only; UpdatedAt is set on every write. A row with no
// flag set and no launcher override carries no user intent, so it is deleted
// rather than persisted. A row holding a forbidden flag pair is refused.
func (db *UserDB) UpsertMediaUserData(data *database.MediaUserData) error {
	conn := db.sql.Load()
	normalized := *data
	normalized.Path = pathutil.CanonicalMediaPath(data.Path)
	if err := normalized.ValidateFlags(); err != nil {
		return fmt.Errorf("media user data for %q: %w", normalized.Path, err)
	}
	if !normalized.HasIntent() {
		return sqlDeleteMediaUserData(db.ctx, conn, normalized.SystemID, normalized.Path)
	}
	return sqlUpsertMediaUserData(db.ctx, conn, &normalized, time.Now().Unix())
}

// SetMediaUserFlag records (or clears) one preference flag for a media path
// without disturbing the other columns on the same row, except for the flags
// the model forbids beside it: liked clears disliked and the reverse, and
// disliked clears favorite. The write is a column-scoped upsert plus a
// conditional delete, run in one transaction so two concurrent edits to the
// same path cannot read-modify-write over each other.
func (db *UserDB) SetMediaUserFlag(systemID, path string, flag database.MediaUserFlag, value bool) error {
	spec, ok := mediaUserFlagColumns[flag]
	if !ok {
		return fmt.Errorf("unknown media user flag %q", flag)
	}
	var sb strings.Builder
	_, _ = sb.WriteString("insert into MediaUserData(SystemID, Path, ")
	_, _ = sb.WriteString(spec.column)
	_, _ = sb.WriteString(", LauncherOverride, CreatedAt, UpdatedAt) values (?, ?, ?, '', ?, ?)\n")
	_, _ = sb.WriteString("on conflict(SystemID, Path) do update set\n\t")
	_, _ = sb.WriteString(spec.column + " = excluded." + spec.column + ",\n")
	for _, cleared := range spec.clears {
		_, _ = sb.WriteString("\t" + cleared + " = case when excluded." + spec.column +
			" = 1 then 0 else MediaUserData." + cleared + " end,\n")
	}
	_, _ = sb.WriteString("\tUpdatedAt = excluded.UpdatedAt;")
	return mediaUserDataColumnWrite(
		db.ctx, db.sql.Load(), sb.String(), systemID, pathutil.CanonicalMediaPath(path), value, time.Now().Unix(),
	)
}

// SetMediaUserFavorite records (or clears) the favourite flag for a media path.
func (db *UserDB) SetMediaUserFavorite(systemID, path string, favorite bool) error {
	return db.SetMediaUserFlag(systemID, path, database.MediaUserFlagFavorite, favorite)
}

// SetMediaUserHidden changes visibility without disturbing other preferences.
func (db *UserDB) SetMediaUserHidden(systemID, path string, hidden bool) error {
	return db.SetMediaUserFlag(systemID, path, database.MediaUserFlagHidden, hidden)
}

// SetMediaUserLauncherOverride records (or clears, when launcherID is empty) the
// launcher-override intent for a media path without disturbing the flags on the
// same row. See SetMediaUserFlag for the concurrency guarantee.
func (db *UserDB) SetMediaUserLauncherOverride(systemID, path, launcherID string) error {
	return mediaUserDataColumnWrite(db.ctx, db.sql.Load(), `
		insert into MediaUserData(
			SystemID, Path, IsFavorite, LauncherOverride, CreatedAt, UpdatedAt
		) values (?, ?, 0, ?, ?, ?)
		on conflict(SystemID, Path) do update set
			LauncherOverride = excluded.LauncherOverride,
			UpdatedAt = excluded.UpdatedAt;
	`, systemID, pathutil.CanonicalMediaPath(path), launcherID, time.Now().Unix())
}

// SetMediaUserSnapshot records a successfully resolved scanner identity
// snapshot on an existing user-data row. It never inserts: a snapshot without
// user intent is meaningless. Empty tags are significant and replace stale
// tags; callers must skip this method when lookup fails.
func (db *UserDB) SetMediaUserSnapshot(systemID, path, mediaName, slug string, tags []string) error {
	return sqlSetMediaUserSnapshot(
		db.ctx, db.sql.Load(), systemID, pathutil.CanonicalMediaPath(path),
		mediaName, slug, database.EncodeTagStrings(tags),
	)
}

// DeleteMediaUserData removes the user-data row for (SystemID, Path). Deleting a
// row that does not exist is not an error.
func (db *UserDB) DeleteMediaUserData(systemID, path string) error {
	return sqlDeleteMediaUserData(db.ctx, db.sql.Load(), systemID, pathutil.CanonicalMediaPath(path))
}

// ListMediaUserData returns every user-data row, used by the reindex re-apply
// step to re-materialize the media.db projection.
func (db *UserDB) ListMediaUserData() ([]database.MediaUserData, error) {
	return sqlListMediaUserData(db.ctx, db.sql.Load())
}

func scanMediaUserData(scan func(dest ...any) error) (database.MediaUserData, error) {
	var row database.MediaUserData
	var rawTags string
	err := scan(
		&row.DBID,
		&row.SystemID,
		&row.Path,
		&row.IsFavorite,
		&row.IsHidden,
		&row.IsLiked,
		&row.IsDisliked,
		&row.IsPlayLater,
		&row.LauncherOverride,
		&row.MediaName,
		&row.Slug,
		&rawTags,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		return row, err
	}
	row.Path = pathutil.CanonicalMediaPath(row.Path)
	row.Tags = database.DecodeTagStrings(rawTags)
	return row, nil
}

func sqlGetMediaUserData(
	ctx context.Context, db *sql.DB, systemID, path string,
) (database.MediaUserData, bool, error) {
	q, err := db.PrepareContext(ctx, `select`+mediaUserDataColumns+`
		from MediaUserData
		where SystemID = ? and Path = ?;
	`)
	if err != nil {
		return database.MediaUserData{}, false,
			fmt.Errorf("failed to prepare media user data select statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	row, err := scanMediaUserData(q.QueryRowContext(ctx, systemID, path).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return database.MediaUserData{}, false, nil
	}
	if err != nil {
		return row, false, fmt.Errorf("failed to scan media user data row: %w", err)
	}
	return row, true, nil
}

func sqlUpsertMediaUserData(
	ctx context.Context, db *sql.DB, data *database.MediaUserData, now int64,
) error {
	stmt, err := db.PrepareContext(ctx, `
		insert into MediaUserData(
			SystemID, Path, IsFavorite, IsHidden, IsLiked, IsDisliked, IsPlayLater,
			LauncherOverride, MediaName, Slug, Tags, CreatedAt, UpdatedAt
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(SystemID, Path) do update set
			IsFavorite = excluded.IsFavorite,
			IsHidden = excluded.IsHidden,
			IsLiked = excluded.IsLiked,
			IsDisliked = excluded.IsDisliked,
			IsPlayLater = excluded.IsPlayLater,
			LauncherOverride = excluded.LauncherOverride,
			MediaName = case when excluded.MediaName != '' then excluded.MediaName else MediaUserData.MediaName end,
			Slug = case when excluded.Slug != '' then excluded.Slug else MediaUserData.Slug end,
			Tags = case when excluded.Tags != '' then excluded.Tags else MediaUserData.Tags end,
			UpdatedAt = excluded.UpdatedAt;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare media user data upsert statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()
	_, err = stmt.ExecContext(ctx,
		data.SystemID,
		data.Path,
		data.IsFavorite,
		data.IsHidden,
		data.IsLiked,
		data.IsDisliked,
		data.IsPlayLater,
		data.LauncherOverride,
		data.MediaName,
		data.Slug,
		database.EncodeTagStrings(data.Tags),
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("failed to execute media user data upsert: %w", err)
	}
	return nil
}

func sqlSetMediaUserSnapshot(
	ctx context.Context, db *sql.DB, systemID, path, mediaName, slug, encodedTags string,
) error {
	_, err := db.ExecContext(ctx, `
		update MediaUserData set MediaName = ?, Slug = ?, Tags = ?
		where SystemID = ? and Path = ?;
	`, mediaName, slug, encodedTags, systemID, path)
	if err != nil {
		return fmt.Errorf("failed to update media user data snapshot: %w", err)
	}
	return nil
}

// mediaUserDataColumnWrite applies a single-column upsert and then deletes the
// row if no user intent remains, both inside one transaction so the pair is
// atomic against concurrent writers. value is the column-specific bind
// (preference bool or launcher ID string).
func mediaUserDataColumnWrite(
	ctx context.Context, db *sql.DB, upsert, systemID, path string, value any, now int64,
) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin media user data transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, upsert, systemID, path, value, now, now); err != nil {
		return fmt.Errorf("failed to upsert media user data column: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`delete from MediaUserData where SystemID = ? and Path = ? and `+mediaUserNoIntent+`;`,
		systemID, path,
	); err != nil {
		return fmt.Errorf("failed to prune empty media user data row: %w", err)
	}
	if revErr := sqlAdvanceMediaPreferencesRevision(ctx, tx, now); revErr != nil {
		err = revErr
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit media user data transaction: %w", err)
	}
	return nil
}

// sqlAdvanceMediaPreferencesRevision bumps the counter that invalidates cached
// browse cursor totals whenever a listing-affecting preference changes.
func sqlAdvanceMediaPreferencesRevision(ctx context.Context, tx *sql.Tx, now int64) error {
	if _, err := tx.ExecContext(ctx, `
		insert into DeviceState(Key, Value, UpdatedAt) values (?, '1', ?)
		on conflict(Key) do update set Value = cast(DeviceState.Value as integer) + 1,
			UpdatedAt = excluded.UpdatedAt;
	`, database.DeviceStateKeyMediaPreferencesRevision, now); err != nil {
		return fmt.Errorf("failed to advance media preferences revision: %w", err)
	}
	return nil
}

func sqlDeleteMediaUserData(ctx context.Context, db *sql.DB, systemID, path string) error {
	_, err := db.ExecContext(ctx,
		`delete from MediaUserData where SystemID = ? and Path = ?;`, systemID, path)
	if err != nil {
		return fmt.Errorf("failed to execute media user data delete: %w", err)
	}
	return nil
}

func sqlListMediaUserData(ctx context.Context, db *sql.DB) ([]database.MediaUserData, error) {
	list := make([]database.MediaUserData, 0)

	q, err := db.PrepareContext(ctx, `select`+mediaUserDataColumns+` from MediaUserData;`)
	if err != nil {
		return list, fmt.Errorf("failed to prepare list media user data statement: %w", err)
	}
	defer func() {
		if closeErr := q.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql statement")
		}
	}()

	rows, err := q.QueryContext(ctx)
	if err != nil {
		return list, fmt.Errorf("failed to execute list media user data query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	for rows.Next() {
		row, scanErr := scanMediaUserData(rows.Scan)
		if scanErr != nil {
			return list, fmt.Errorf("failed to scan media user data row: %w", scanErr)
		}
		list = append(list, row)
	}
	if err = rows.Err(); err != nil {
		return list, fmt.Errorf("failed to iterate over media user data rows: %w", err)
	}
	return list, nil
}
