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

package database

import (
	"database/sql"
	"embed"
	"fmt"

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

	log.Debug().Str("migration_dir", migrationDir).Msg("running goose up migrations")
	if err := goose.Up(db, migrationDir); err != nil {
		return fmt.Errorf("error running migrations up: %w", err)
	}

	return nil
}
