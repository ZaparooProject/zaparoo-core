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
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb/boltmigration"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const zapLinkHostExpiration = 30 * 24 * time.Hour

const userDBBackupMaxAge = 24 * time.Hour

// mediaDBSchemaReset reports that the media database was discarded because its
// schema was newer than this build supports.
type mediaDBSchemaReset struct {
	// userDataLost is set when favorites or launcher overrides that existed only in
	// the discarded database could not be read out of it, or could not all be written
	// to the user database. No reindex can rebuild them and nothing else holds a copy,
	// so the user is told rather than left to notice on their own.
	userDataLost bool
}

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

// makeDatabase opens both databases. A non-nil reset reports that the media database
// was discarded because its schema was newer than this build supports, so the caller
// can tell the user why a full reindex is starting.
func makeDatabase(
	ctx context.Context, pl platforms.Platform,
) (*database.Database, *mediaDBSchemaReset, error) {
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

	// The user database goes first because it is the one that can end startup: if
	// its schema is newer than this build understands there is nothing to be done
	// about it, and the media database must not have been thrown away by then.
	log.Debug().Msg("opening user database")
	userDB, err := openAndRecoverUserDB(ctx, pl)
	// Assign before the error check: openAndRecoverUserDB can return a non-nil
	// handle alongside an error, and the deferred closeDatabase only closes what
	// is stored on db. Assigning here ensures that handle is not leaked.
	db.UserDB = userDB
	if err != nil {
		return db, nil, err
	}

	log.Debug().Msg("opening media database")
	mediaDB, err := mediadb.OpenMediaDB(ctx, pl)
	if err != nil {
		return db, nil, fmt.Errorf("failed to open media database: %w", err)
	}
	db.MediaDB = mediaDB

	log.Debug().Msg("running media database migrations")
	var reset *mediaDBSchemaReset
	err = mediaDB.MigrateUp()
	switch {
	case errors.Is(err, database.ErrSchemaAhead):
		// A newer build migrated this file and the device has since gone back to
		// an older one. Everything in the media database can be rebuilt by a
		// reindex, so discarding it is better than refusing to start: a device
		// that will not boot is a far worse outcome than a rebuild. The user
		// database holds data nothing can reconstruct, so it stays fatal.
		//
		// No recovery gate is taken around the rebuild: nothing else has a handle
		// on this database yet, so there is no background work to lock out.
		log.Warn().Err(err).Msg("media database schema is newer than this build supports, rebuilding it")
		reset = &mediaDBSchemaReset{userDataLost: false}
		// Favorites and launcher overrides written before UserDB became their
		// home exist only in this file. Import them now rather than carrying them
		// to the backfill below: once the file is gone the rescue is the only copy
		// there is, and anything that ends startup in between would take it.
		rescued, rescueErr := rescueMediaUserData(ctx, mediaDB)
		switch {
		case rescueErr != nil:
			// Nothing later can say what was in there, so assume the worst.
			log.Error().Err(rescueErr).
				Msg("could not read media user data out of the newer media database")
			reset.userDataLost = true
		case len(rescued) > 0:
			if importErr := backfillMediaUserData(ctx, db, rescued); importErr != nil {
				log.Error().Err(importErr).
					Msg("could not import media user data rescued from the newer media database")
				reset.userDataLost = true
			}
		}
		if resetErr := resetMediaDBForNewerSchema(mediaDB); resetErr != nil {
			return db, nil, fmt.Errorf("rebuilding media database with a newer schema: %w", resetErr)
		}
	case err != nil:
		return db, nil, fmt.Errorf("error migrating mediadb: %w", err)
	}

	// migrate old boltdb mappings if required
	log.Debug().Msg("checking for boltdb migration")
	err = boltmigration.MaybeMigrate(pl, userDB)
	if err != nil {
		log.Error().Err(err).Msg("error migrating old boltdb mappings")
	}

	// One-time import of favorites/launcher overrides that older versions wrote
	// only to media.db, so they live in UserDB (the source of truth) and survive a
	// future media.db rebuild. media.db is still there to retry from next boot, so a
	// failure here is only logged.
	if err := backfillMediaUserData(ctx, db, nil); err != nil {
		log.Warn().Err(err).Msg("failed to backfill media user data into the user database")
	}

	success = true
	return db, reset, nil
}

// rescueMediaUserData reads the favorites and launcher overrides out of a media
// database that is about to be discarded, for the caller to hand straight to the
// backfill. The file was written by a newer build, so these queries may not fit its
// schema at all; an error means the caller is about to delete rows it could not read
// and cannot say what they were, which is worth telling the user about.
func rescueMediaUserData(ctx context.Context, mediaDB *mediadb.MediaDB) ([]database.MediaUserData, error) {
	rows, err := mediaDB.GetExistingMediaUserData(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading media user data from the newer media database: %w", err)
	}
	if len(rows) > 0 {
		log.Info().Int("found", len(rows)).
			Msg("preserving media user data from the discarded media database")
	}
	return rows, nil
}

// resetMediaDBForNewerSchema replaces a media database this build cannot read
// with an empty one at this build's schema, left marked pending so the startup
// resume check reindexes it. No forensic copy is kept: the cause is known, the
// contents are reproducible, and the platforms most likely to hit this are the
// ones with the least free space.
func resetMediaDBForNewerSchema(mediaDB *mediadb.MediaDB) error {
	if err := mediaDB.Recreate(false); err != nil {
		return fmt.Errorf("recreating media database: %w", err)
	}
	// Recreate's reopen allocates the schema, but the extra work MigrateUp does
	// on top of that — seeding planner statistics so the reindex about to start
	// has sane query plans — only happens on this path.
	if err := mediaDB.MigrateUp(); err != nil {
		return fmt.Errorf("migrating recreated media database: %w", err)
	}
	return nil
}

// notifyMediaDBSchemaReset tells the user why their media is being indexed
// again. makeDatabase discards the database before the inbox service exists, so
// the message is posted from Start once it does.
func notifyMediaDBSchemaReset(st *state.State, userDataLost bool) {
	if st == nil {
		return
	}
	inboxSvc := st.Inbox()
	if inboxSvc == nil {
		log.Warn().Msg("inbox unavailable, cannot report media database rebuild")
		return
	}
	body := "This version of Zaparoo is older than the one that last ran and could not read " +
		"the media database it left behind. The database has been rebuilt and your media is being " +
		"indexed again. Re-scrape your library to restore box art and metadata."
	if userDataLost {
		// The rescue is the only thing that could have saved these, and it did not.
		// A reindex will not bring them back, so say so plainly.
		// A failed read cannot tell an empty database from one full of favorites, so
		// this hedges rather than telling someone who had none that they lost some.
		body += " Favorites and launcher overrides set before this device last updated may " +
			"not have been carried across, and may need to be set again."
	}
	if err := inboxSvc.Add("Media database was rebuilt after a version change",
		inbox.WithBody(body),
		inbox.WithSeverity(inbox.SeverityWarning),
		inbox.WithCategory(inbox.CategoryMediaDBSchemaReset),
	); err != nil {
		log.Warn().Err(err).Msg("failed to add inbox message about media database rebuild")
	}
}

// backfillMediaHistoryUUIDs assigns stable IDs to history written by older
// versions. Idempotent; the caller retries on error (the backfill watcher
// re-attempts on every sweep trigger until one clean success).
func backfillMediaHistoryUUIDs(userDB database.UserDBI) (int64, error) {
	if userDB == nil {
		return 0, nil
	}

	startedAt := time.Now()
	backfilled, err := userDB.BackfillMediaHistoryUUIDs()
	duration := time.Since(startedAt)
	if err != nil {
		log.Error().Err(err).Dur("duration", duration).Msg("failed to backfill media history UUIDs")
		return 0, fmt.Errorf("backfill media history UUIDs: %w", err)
	}
	if backfilled > 0 {
		log.Info().Int64("backfilled", backfilled).Dur("duration", duration).
			Msg("backfilled media history UUIDs")
		return backfilled, nil
	}
	log.Debug().Dur("duration", duration).Msg("media history UUID backfill completed")
	return 0, nil
}

// backfillMediaUserData seeds UserDB from favorites/launcher overrides that older
// versions stored only in media.db. It runs only while UserDB has no media user
// data yet: once any row exists, UserDB is authoritative and media.db's copy is
// never re-read (re-reading could resurrect a favorite the user removed if a prior
// projection write had failed).
//
// rescued carries rows read out of a media database that has since been discarded
// for having a newer schema; when it is set, it stands in for the read that can no
// longer happen. The same UserDB-is-authoritative guard applies to it.
//
// Never fatal — startup carries on either way. An error means at least one row did
// not make it, which matters for rescued rows because nothing else holds them.
// Having nothing to do is not an error.
func backfillMediaUserData(ctx context.Context, db *database.Database, rescued []database.MediaUserData) error {
	if db == nil || db.UserDB == nil {
		if len(rescued) > 0 {
			return errors.New("no user database to import media user data into")
		}
		return nil
	}

	existing, err := db.UserDB.ListMediaUserData()
	if err != nil {
		return fmt.Errorf("reading media user data from user database: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}

	rows := rescued
	if len(rows) == 0 {
		if db.MediaDB == nil {
			return nil
		}
		rows, err = db.MediaDB.GetExistingMediaUserData(ctx)
		if err != nil {
			return fmt.Errorf("reading media user data from media database: %w", err)
		}
	}
	if len(rows) == 0 {
		return nil
	}

	migrated := 0
	var firstErr error
	for i := range rows {
		row := rows[i]
		if upErr := db.UserDB.UpsertMediaUserData(&row); upErr != nil {
			log.Warn().Err(upErr).
				Str("system", row.SystemID).Str("path", row.Path).
				Msg("failed to backfill media user data row")
			if firstErr == nil {
				firstErr = upErr
			}
			continue
		}
		migrated++
	}
	log.Info().Int("migrated", migrated).Int("found", len(rows)).
		Msg("backfilled media user data into user database")
	if firstErr != nil {
		return fmt.Errorf("imported %d of %d media user data rows: %w", migrated, len(rows), firstErr)
	}
	return nil
}

func openAndRecoverUserDB(ctx context.Context, pl platforms.Platform) (*userdb.UserDB, error) {
	userDB, err := userdb.OpenUserDB(ctx, pl)
	if err != nil {
		if userDB != nil && userDB.NoteCorruption(err) {
			logUserDBIntegrityReport(userDB)
			if _, recoverErr := userDB.RecoverFromCorruption(); recoverErr != nil {
				return userDB, fmt.Errorf("failed to recover corrupt user database after open error: %w", recoverErr)
			}
			return userDB, nil
		}
		return userDB, fmt.Errorf("failed to open user database: %w", err)
	}
	if userDB.IsMarkedCorrupt() {
		logUserDBIntegrityReport(userDB)
		if _, recoverErr := userDB.RecoverFromCorruption(); recoverErr != nil {
			return userDB, fmt.Errorf("failed to recover marked corrupt user database: %w", recoverErr)
		}
		return userDB, nil
	}

	log.Debug().Msg("running user database migrations")
	if err = userDB.MigrateUp(); err != nil {
		if userDB.NoteCorruption(err) {
			logUserDBIntegrityReport(userDB)
			if _, recoverErr := userDB.RecoverFromCorruption(); recoverErr != nil {
				return userDB, fmt.Errorf(
					"failed to recover corrupt user database after migration error: %w", recoverErr,
				)
			}
			return userDB, nil
		}
		return userDB, fmt.Errorf("error migrating userdb: %w", err)
	}

	if backup, created, backupErr := userDB.EnsureRecentBackup(userDBBackupMaxAge); backupErr != nil {
		log.Warn().Err(backupErr).Msg("failed to ensure recent user database backup")
	} else if created {
		log.Info().Str("path", backup.Path).Msg("created scheduled user database backup")
	}
	return userDB, nil
}

func logUserDBIntegrityReport(userDB *userdb.UserDB) {
	for _, line := range userDB.IntegrityReport() {
		log.Warn().Str("report", line).Msg("user database integrity report")
	}
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

func cleanupHistoryRetention(
	ctx context.Context, cfg *config.Instance, db *database.Database, protectUnsyncedPlayHistory bool,
) {
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
		rowsDeleted, cleanupErr := db.UserDB.CleanupMediaHistory(playtimeRetention, protectUnsyncedPlayHistory)
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

func runMediaDBStartupMaintenance(
	ctx context.Context, db database.MediaDBI, pauser *syncutil.Pauser, tagCacheLoaded bool,
) {
	if db == nil {
		log.Warn().Msg("skipping media database startup maintenance: media database is nil")
		return
	}

	db.TrackBackgroundOperation()
	defer db.BackgroundOperationDone()

	// Boot here intentionally does NOT issue PRAGMA optimize or
	// wal_checkpoint(TRUNCATE). SQLite's auto-checkpoint runs PASSIVE
	// inline with COMMITs and keeps the WAL bounded without blocking
	// readers; TRUNCATE takes the EXCLUSIVE writer lock and contends with
	// the launcher's first query. Optimize is documented as run-on-close
	// or "every few hours" and is similarly expensive on cold boot. WAL
	// mode auto-recovers on next open after a hard power-off, so neither
	// is needed for correctness.

	if startupMaintenanceCancelled(ctx, "skipping tag cache warmup: startup maintenance cancelled") {
		return
	}

	indexingStatus, indexingStatusErr := db.GetIndexingStatus()
	if indexingStatusErr != nil {
		log.Warn().Err(indexingStatusErr).Msg("failed to check indexing status before tag cache warmup")
	}
	interruptedIndexing := indexingStatusErr == nil &&
		(indexingStatus == mediadb.IndexingStatusRunning || indexingStatus == mediadb.IndexingStatusPending)

	// Only rebuild the tag cache if LoadCachedTagCache didn't populate it
	// from disk. Skipping the rebuild on a warm boot is the whole point of
	// persisting the cache. Also skip when an interrupted index is waiting to
	// resume; routine cache work must not delay visible media-update progress.
	if interruptedIndexing {
		log.Debug().Str("status", indexingStatus).Msg("skipping startup media maintenance before index resume")
		return
	} else if !tagCacheLoaded {
		if err := db.RebuildTagCache(); err != nil {
			log.Warn().Err(err).Msg("failed to warm tag cache on startup")
		} else if persistErr := db.PersistTagCache(); persistErr != nil {
			// Best-effort: the rebuild succeeded so the running process is
			// fine, but skipping persistence means the next cold boot will
			// pay the rebuild cost again.
			log.Warn().Err(persistErr).Msg("failed to persist tag cache after startup rebuild")
		}
	}

	if startupMaintenanceCancelled(ctx, "skipping temporary media repair jobs: startup maintenance cancelled") {
		return
	}

	pending, err := db.TemporaryRepairJobsPending(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to check temporary media repair jobs")
		return
	}
	if !pending {
		return
	}

	indexingStatus, err = db.GetIndexingStatus()
	if err != nil {
		log.Warn().Err(err).Msg("failed to check indexing status before temporary media repair jobs")
		return
	}
	if indexingStatus == mediadb.IndexingStatusRunning || indexingStatus == mediadb.IndexingStatusPending {
		log.Info().Str("indexingStatus", indexingStatus).
			Msg("temporary media repair jobs pending; deferring until indexing completes")
		return
	}

	optimizationStatus, err := db.GetOptimizationStatus()
	if err != nil {
		log.Warn().Err(err).Msg("failed to check optimization status before temporary media repair jobs")
		return
	}
	if optimizationStatus == mediadb.IndexingStatusRunning {
		log.Info().Msg("temporary media repair jobs pending; optimization already running")
		return
	}

	log.Info().Msg("temporary media repair jobs pending; starting background optimization")
	db.RunBackgroundOptimization(nil, pauser)
}
