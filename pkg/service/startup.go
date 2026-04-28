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

package service

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb/boltmigration"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const zapLinkHostExpiration = 30 * 24 * time.Hour

func setupEnvironment(pl platforms.Platform) error {
	return setupEnvironmentFS(afero.NewOsFs(), pl)
}

func setupEnvironmentFS(fs afero.Fs, pl platforms.Platform) error {
	if _, ok := helpers.HasUserDir(); ok {
		log.Info().Msg("using 'user' directory for storage")
	}

	log.Info().Msg("creating platform directories")
	dirs := []string{
		helpers.ConfigDir(pl),
		pl.Settings().TempDir,
		helpers.DataDir(pl),
		filepath.Join(helpers.DataDir(pl), config.MappingsDir),
		filepath.Join(helpers.DataDir(pl), config.AssetsDir),
		filepath.Join(helpers.DataDir(pl), config.LaunchersDir),
		filepath.Join(helpers.DataDir(pl), config.MediaDir),
	}
	for _, dir := range dirs {
		err := fs.MkdirAll(dir, 0o750)
		if err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

func makeDatabase(ctx context.Context, pl platforms.Platform) (*database.Database, error) {
	db := &database.Database{
		MediaDB: nil,
		UserDB:  nil,
	}
	success := false
	defer func() {
		if !success {
			closeDatabase(db)
		}
	}()

	log.Debug().Msg("opening media database")
	mediaDB, err := mediadb.OpenMediaDB(ctx, pl)
	if err != nil {
		return db, fmt.Errorf("failed to open media database: %w", err)
	}
	db.MediaDB = mediaDB

	log.Debug().Msg("running media database migrations")
	err = mediaDB.MigrateUp()
	if err != nil {
		return db, fmt.Errorf("error migrating mediadb: %w", err)
	}

	log.Debug().Msg("opening user database")
	userDB, err := userdb.OpenUserDB(ctx, pl)
	if err != nil {
		return db, fmt.Errorf("failed to open user database: %w", err)
	}
	db.UserDB = userDB

	log.Debug().Msg("running user database migrations")
	err = userDB.MigrateUp()
	if err != nil {
		return db, fmt.Errorf("error migrating userdb: %w", err)
	}

	// migrate old boltdb mappings if required
	log.Debug().Msg("checking for boltdb migration")
	err = boltmigration.MaybeMigrate(pl, userDB)
	if err != nil {
		log.Error().Err(err).Msg("error migrating old boltdb mappings")
	}

	success = true
	return db, nil
}

func closeDatabase(db *database.Database) {
	if db == nil {
		return
	}
	if db.UserDB != nil {
		if err := db.UserDB.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing user database")
		}
	}
	if db.MediaDB != nil {
		if err := db.MediaDB.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing media database")
		}
	}
}

func startupMaintenanceCancelled(ctx context.Context, message string) bool {
	if err := ctx.Err(); err != nil {
		log.Debug().Err(err).Msg(message)
		return true
	}
	return false
}

func cleanupHistoryRetention(ctx context.Context, cfg *config.Instance, db *database.Database) {
	if startupMaintenanceCancelled(ctx, "skipping history retention cleanup: startup maintenance cancelled") {
		return
	}
	if db == nil {
		log.Warn().Msg("skipping history retention cleanup: database is nil")
		return
	}
	if db.UserDB == nil {
		log.Warn().Msg("skipping history retention cleanup: user database is nil")
		return
	}

	scanHistoryDays := cfg.ScanHistory()
	if scanHistoryDays > 0 {
		log.Info().Msgf("cleaning up scan history older than %d days", scanHistoryDays)
		rowsDeleted, cleanupErr := db.UserDB.CleanupHistory(scanHistoryDays)
		switch {
		case cleanupErr != nil:
			log.Error().Err(cleanupErr).Msg("error cleaning up scan history")
		case rowsDeleted > 0:
			log.Info().Msgf("deleted %d old scan history entries", rowsDeleted)
		default:
			log.Debug().Msg("no old scan history entries to clean up")
		}
	} else {
		log.Debug().Msg("scan history cleanup disabled (retention set to 0)")
	}

	if startupMaintenanceCancelled(ctx, "skipping media history retention cleanup: startup maintenance cancelled") {
		return
	}

	// Cleanup old media history entries if retention is configured
	playtimeRetention := cfg.PlaytimeRetention()
	if playtimeRetention > 0 {
		log.Info().Msgf("cleaning up media history older than %d days", playtimeRetention)
		rowsDeleted, cleanupErr := db.UserDB.CleanupMediaHistory(playtimeRetention)
		switch {
		case cleanupErr != nil:
			log.Error().Err(cleanupErr).Msg("error cleaning up media history")
		case rowsDeleted > 0:
			log.Info().Msgf("deleted %d old media history entries", rowsDeleted)
		default:
			log.Debug().Msg("no old media history entries to clean up")
		}
	} else {
		log.Debug().Msg("media history cleanup disabled (retention set to 0)")
	}
}

func closeHangingMediaHistoryOnStartup(db *database.Database) {
	log.Info().Msg("closing hanging media history entries")
	if hangingErr := db.UserDB.CloseHangingMediaHistory(); hangingErr != nil {
		log.Error().Err(hangingErr).Msg("error closing hanging media history entries")
	}
}

// pruneExpiredZapLinkHosts removes non-supporting zaplink hosts older than 30 days.
// This allows hosts that may have added zaplink support to be re-checked.
func pruneExpiredZapLinkHosts(db *database.Database) {
	log.Info().Msg("pruning expired non-supporting zaplink hosts")
	rowsDeleted, err := db.UserDB.PruneExpiredZapLinkHosts(zapLinkHostExpiration)
	switch {
	case err != nil:
		log.Error().Err(err).Msg("error pruning expired zaplink hosts")
	case rowsDeleted > 0:
		log.Info().Msgf("pruned %d expired non-supporting zaplink hosts", rowsDeleted)
	default:
		log.Debug().Msg("no expired zaplink hosts to prune")
	}
}

func runMediaDBStartupMaintenance(ctx context.Context, db database.MediaDBI) {
	if db == nil {
		log.Warn().Msg("skipping media database startup maintenance: media database is nil")
		return
	}

	db.TrackBackgroundOperation()
	defer db.BackgroundOperationDone()

	if sqlDB := db.UnsafeGetSQLDb(); sqlDB != nil {
		log.Debug().Msg("running media database PRAGMA optimize")
		if _, err := sqlDB.ExecContext(ctx, "PRAGMA optimize;"); err != nil {
			log.Warn().Err(err).Msg("failed to run PRAGMA optimize")
		}

		log.Debug().Msg("running media database WAL checkpoint")
		if _, err := sqlDB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
			log.Warn().Err(err).Msg("failed to run WAL checkpoint on startup")
		}
	} else {
		log.Warn().Msg("skipping media database PRAGMA maintenance: SQL database is nil")
	}

	if startupMaintenanceCancelled(ctx, "skipping tag cache warmup: startup maintenance cancelled") {
		return
	}

	if err := db.RebuildTagCache(); err != nil {
		log.Warn().Err(err).Msg("failed to warm tag cache on startup")
	}
}

func runStartupMaintenance(ctx context.Context, cfg *config.Instance, db *database.Database) {
	if db == nil {
		log.Warn().Msg("skipping startup maintenance: database is nil")
		return
	}

	runMediaDBStartupMaintenance(ctx, db.MediaDB)
	cleanupHistoryRetention(ctx, cfg, db)
	if startupMaintenanceCancelled(ctx, "skipping zaplink host pruning: startup maintenance cancelled") {
		return
	}
	pruneExpiredZapLinkHosts(db)
}
