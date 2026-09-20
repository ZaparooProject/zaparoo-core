/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package userdb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog/log"
)

// schemaMetaAppVersion is the key holding the version of the build that last
// migrated this database.
const schemaMetaAppVersion = "app_version"

// recordSchemaProvenance notes which build just migrated this database.
//
// An older binary refuses a schema newer than it understands, which is right —
// nothing can reconstruct history, mappings or profiles — but until this was
// recorded the refusal could only name goose timestamps, which do not tell a
// user which version to reinstall.
//
// Failure is not fatal: this is diagnostic detail, and a database that
// migrated successfully must not be rejected because a note about it could not
// be written.
func recordSchemaProvenance(ctx context.Context, db *sql.DB) {
	_, err := db.ExecContext(
		ctx,
		`insert into SchemaMeta (Key, Value, UpdatedAt) values (?, ?, ?)
		 on conflict(Key) do update set Value = excluded.Value, UpdatedAt = excluded.UpdatedAt`,
		schemaMetaAppVersion, config.AppVersion, time.Now().Unix(),
	)
	if err != nil {
		log.Debug().Err(err).Msg("could not record which build migrated the user database")
	}
}

// SchemaProvenance reports the version of Zaparoo that last migrated the user
// database at dbPath, without going through the normal open path.
//
// It is meant for the case where the normal open path has already refused: the
// schema is newer than this build understands, so nothing here may migrate,
// write or otherwise touch the file. The connection is read-only for that
// reason.
//
// ok is false when the file, the table or the row is absent, which is the
// normal answer for a database last written by a build from before this was
// recorded. Callers have to read well without it.
func SchemaProvenance(dbPath string) (version string, ok bool) {
	if dbPath == "" {
		return "", false
	}
	if _, err := os.Stat(dbPath); err != nil {
		return "", false
	}

	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro&_busy_timeout=2000")
	if err != nil {
		log.Debug().Err(err).Msg("could not open user database to read its provenance")
		return "", false
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("could not close provenance connection")
		}
	}()

	var value string
	queryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = db.QueryRowContext(
		queryCtx, `select Value from SchemaMeta where Key = ?`, schemaMetaAppVersion,
	).Scan(&value)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Debug().Err(err).Msg("could not read user database provenance")
		}
		return "", false
	}
	if value == "" {
		return "", false
	}

	return value, true
}
