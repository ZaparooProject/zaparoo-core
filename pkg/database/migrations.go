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

package database

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"
)

var migrationMutex syncutil.Mutex

// gooseZerologAdapter implements goose.Logger interface to redirect
// goose output to zerolog instead of stdout
type gooseZerologAdapter struct{}

func (*gooseZerologAdapter) Printf(format string, v ...any) {
	log.Info().Msgf(format, v...)
}

func (*gooseZerologAdapter) Fatalf(format string, v ...any) {
	log.Fatal().Msgf(format, v...)
}

// MigrateUp provides thread-safe database migration using goose.
// It locks access to goose's global state to prevent race conditions
// between multiple databases setting their migration filesystems.
func MigrateUp(db *sql.DB, migrationFiles embed.FS, migrationDir string) error {
	log.Debug().Msg("waiting for migration mutex")
	migrationMutex.Lock()
	log.Debug().Msg("migration mutex acquired")
	defer func() {
		migrationMutex.Unlock()
		log.Debug().Msg("migration mutex released")
	}()

	log.Debug().Msg("setting up goose logger")
	// Set custom logger to redirect goose output to zerolog
	goose.SetLogger(&gooseZerologAdapter{})

	log.Debug().Msg("setting goose base filesystem")
	goose.SetBaseFS(migrationFiles)

	log.Debug().Msg("setting goose dialect to sqlite")
	if err := goose.SetDialect("sqlite"); err != nil {
		return fmt.Errorf("error setting goose dialect: %w", err)
	}

	// Check if the database schema is ahead of this binary. This happens
	// when switching from a newer binary (e.g. beta) to an older one.
	if err := CheckSchemaVersion(db, migrationFiles, migrationDir); err != nil {
		return err
	}

	log.Debug().Str("migration_dir", migrationDir).Msg("running goose up migrations")
	if err := goose.Up(db, migrationDir); err != nil {
		return fmt.Errorf("error running migrations up: %w", err)
	}

	return nil
}

// ErrSchemaAhead is returned when the database schema version is newer than
// the binary supports. This happens when switching from a newer binary
// (e.g. beta) to an older one (e.g. stable).
var ErrSchemaAhead = errors.New("database schema is newer than this binary supports")

// CheckSchemaVersion compares the database's migration version against the
// latest migration embedded in the current binary. Returns ErrSchemaAhead
// if the database is ahead, preventing the older binary from running against
// an incompatible schema.
func CheckSchemaVersion(db *sql.DB, migrationFiles embed.FS, migrationDir string) error {
	dbVersion, err := goose.GetDBVersion(db)
	if err != nil {
		return fmt.Errorf("checking database schema version: %w", err)
	}
	if dbVersion == 0 {
		return nil
	}

	latest, err := latestEmbeddedVersion(migrationFiles, migrationDir)
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	if dbVersion > latest {
		return fmt.Errorf(
			"%w: database is at version %d but this binary only supports up to %d, "+
				"update to a newer version or reinstall the previous version",
			ErrSchemaAhead, dbVersion, latest,
		)
	}

	return nil
}

// latestEmbeddedVersion returns the highest goose version number from the
// embedded migration filenames (e.g. 20250605021915_init.sql).
func latestEmbeddedVersion(migrationFiles embed.FS, migrationDir string) (int64, error) {
	entries, err := fs.ReadDir(migrationFiles, migrationDir)
	if err != nil {
		return 0, fmt.Errorf("reading embedded migrations: %w", err)
	}

	var maxVersion int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) == 0 {
			continue
		}

		version, parseErr := strconv.ParseInt(parts[0], 10, 64)
		if parseErr != nil {
			continue
		}

		if version > maxVersion {
			maxVersion = version
		}
	}

	if maxVersion == 0 {
		return 0, errors.New("no valid migration versions found in embedded files")
	}

	return maxVersion, nil
}
