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
	"sync/atomic"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

// mediaDBRecovering serializes media database recovery so the startup check and the
// runtime watcher can never run a close/reopen rebuild concurrently.
var mediaDBRecovering atomic.Bool

// checkAndResumeIndexing checks if media indexing was interrupted and automatically resumes it
func checkAndResumeIndexing(
	pl platforms.Platform,
	cfg *config.Instance,
	db *database.Database,
	st *state.State,
	pauser *syncutil.Pauser,
) {
	// Check if indexing was interrupted
	indexingStatus, err := db.MediaDB.GetIndexingStatus()
	if err != nil {
		log.Debug().Err(err).Msg("failed to get indexing status during startup check")
		return
	}

	// Only resume if indexing was interrupted (running or pending states)
	if indexingStatus != mediadb.IndexingStatusRunning && indexingStatus != mediadb.IndexingStatusPending {
		log.Debug().Msgf("indexing status is '%s', no auto-resume needed", indexingStatus)
		return
	}

	log.Info().Msg("detected interrupted media indexing, automatically resuming")

	// Get the systems that were being indexed from the database
	// If not available, fall back to all systems
	var systems []systemdefs.System
	storedSystemIDs, err := db.MediaDB.GetIndexingSystems()
	if err != nil || len(storedSystemIDs) == 0 {
		log.Debug().Msgf("no stored systems found (err=%v, len=%d), defaulting to all systems",
			err, len(storedSystemIDs))
		systems = systemdefs.AllSystems()
	} else {
		// Convert system IDs to System objects
		systems = make([]systemdefs.System, 0, len(storedSystemIDs))
		for _, systemID := range storedSystemIDs {
			if system, exists := systemdefs.Systems[systemID]; exists {
				systems = append(systems, system)
			} else {
				log.Warn().Msgf("stored system ID '%s' not found in system definitions, skipping", systemID)
			}
		}
		// If we couldn't resolve any systems, fall back to all systems
		if len(systems) == 0 {
			log.Warn().Msg("could not resolve any stored systems, falling back to all systems")
			systems = systemdefs.AllSystems()
		}
	}

	// Resume using the proper function with full notification support
	// GenerateMediaDB spawns its own goroutine and returns immediately
	err = methods.GenerateMediaDB(st.GetContext(), pl, cfg, st.Notifications, systems, db, pauser)
	if err != nil {
		// An expected operational state (e.g. indexing/scraping already running)
		// means auto-resume isn't needed — not a failure worth reporting.
		var clientErr *models.ClientError
		if errors.As(err, &clientErr) {
			log.Warn().Err(err).Msg("skipping auto-resume of media indexing")
		} else {
			log.Error().Err(err).Msg("failed to start auto-resume of media indexing")
		}
	}
}

// checkAndRecoverCorruptMediaDB rebuilds the media database from scratch when corruption
// has been detected. MediaDB is a disposable, rebuildable cache (user-owned data lives in
// UserDB), so recovery discards the corrupt file rather than attempting an unreliable
// in-process repair, then triggers a full reindex. The durable sidecar marker is the
// authoritative signal because the in-DB status write can itself fail on a malformed file.
func checkAndRecoverCorruptMediaDB(
	pl platforms.Platform,
	cfg *config.Instance,
	db *database.Database,
	st *state.State,
	pauser *syncutil.Pauser,
) {
	if db == nil || db.MediaDB == nil {
		return
	}

	// Only one recovery at a time: the startup check and the runtime watcher both call
	// this, and a concurrent close/reopen rebuild would race.
	if !mediaDBRecovering.CompareAndSwap(false, true) {
		return
	}
	defer mediaDBRecovering.Store(false)

	corrupt := db.MediaDB.IsMarkedCorrupt()
	if !corrupt {
		// Backstop: trust a persisted corrupt status even if the marker is missing.
		if status, err := db.MediaDB.GetIndexingStatus(); err == nil && status == mediadb.IndexingStatusCorrupt {
			corrupt = true
		}
	}
	if !corrupt {
		return
	}

	// Never rebuild on top of an in-flight operation; the marker keeps recovery pending
	// until the next safe point (this check runs again on the next startup pass).
	if status, err := db.MediaDB.GetIndexingStatus(); err == nil &&
		(status == mediadb.IndexingStatusRunning || status == mediadb.IndexingStatusPending) {
		log.Warn().Msg("media database flagged corrupt but indexing is in flight; deferring recovery")
		return
	}
	if status, err := db.MediaDB.GetScrapingStatus(); err == nil && status == mediadb.IndexingStatusRunning {
		log.Warn().Msg("media database flagged corrupt but scraping is in flight; deferring recovery")
		return
	}

	log.Error().Strs("integrity", db.MediaDB.IntegrityReport()).
		Msg("media database is corrupt; rebuilding from scratch")
	notifications.MediaIndexing(st.Notifications, models.IndexingStatusResponse{
		Exists:   false,
		Indexing: true,
	})

	if err := db.MediaDB.RecreateAfterCorruption(config.IsDevelopmentVersion()); err != nil {
		log.Error().Err(err).Msg("failed to recreate media database after corruption")
		return
	}
	log.Info().Msg("media database recreated after corruption; starting full reindex")

	// Tell the user persistently: the rebuild discards scraped metadata (it lived in the
	// corrupt cache), so artwork returns only after a re-scrape. The inbox lives in UserDB,
	// which is unaffected by the media database corruption. Category dedups repeat events.
	if inbox := st.Inbox(); inbox != nil {
		if inboxErr := inbox.Add("Media database was corrupted and rebuilt",
			inboxservice.WithBody("Your media database was corrupted (likely a storage write "+
				"error) and has been rebuilt automatically. Re-scrape your library to restore "+
				"box art and metadata."),
			inboxservice.WithSeverity(inboxservice.SeverityWarning),
			inboxservice.WithCategory(inboxservice.CategoryMediaDBCorruptionRecovery),
		); inboxErr != nil {
			log.Warn().Err(inboxErr).Msg("failed to add inbox message about media database recovery")
		}
	}

	if err := methods.GenerateMediaDB(st.GetContext(), pl, cfg, st.Notifications,
		systemdefs.AllSystems(), db, pauser); err != nil {
		var clientErr *models.ClientError
		if errors.As(err, &clientErr) {
			log.Warn().Err(err).Msg("skipping reindex after media database recovery")
		} else {
			log.Error().Err(err).Msg("failed to start reindex after media database recovery")
		}
	}
}

// watchForCorruptMediaDBRecovery triggers recovery at runtime once an indexing or
// optimization operation completes. Detection points set the durable corrupt marker
// mid-operation; this watcher re-checks it at each operation boundary (a media-indexing
// notification) so corruption found during a session self-heals without waiting for the
// next service start. checkAndRecoverCorruptMediaDB is a cheap no-op when the marker is
// absent or an operation is still in flight, and its CAS guard makes re-entry safe.
func watchForCorruptMediaDBRecovery(
	ctx context.Context,
	b *broker.Broker,
	pl platforms.Platform,
	cfg *config.Instance,
	db *database.Database,
	st *state.State,
	pauser *syncutil.Pauser,
) {
	notifChan, subID := b.Subscribe(32, models.NotificationMediaIndexing)
	defer b.Unsubscribe(subID)

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-notifChan:
			if !ok {
				return
			}
			checkAndRecoverCorruptMediaDB(pl, cfg, db, st, pauser)
		}
	}
}

// checkAndResumeOptimization checks if optimization was interrupted and automatically resumes it
func checkAndResumeScraping(
	pl platforms.Platform,
	cfg *config.Instance,
	db *database.Database,
	st *state.State,
	pauser *syncutil.Pauser,
) {
	status, err := db.MediaDB.GetScrapingStatus()
	if err != nil {
		log.Debug().Err(err).Msg("failed to get scraping status during startup check")
		return
	}
	if status != mediadb.IndexingStatusRunning && status != mediadb.IndexingStatusPending {
		log.Debug().Msgf("scraping status is '%s', no auto-resume needed", status)
		return
	}

	operation, found, err := db.MediaDB.GetScrapingOperation()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get scraping operation during startup check")
		return
	}
	if !found || operation.ScraperID == "" {
		log.Warn().Msg("scraping marked incomplete but no scraping operation was stored")
		if setErr := db.MediaDB.SetScrapingStatus(mediadb.IndexingStatusFailed); setErr != nil {
			log.Warn().Err(setErr).Msg("failed to mark incomplete scraping as failed")
		}
		return
	}

	if _, ok := pl.Scrapers(cfg)[operation.ScraperID]; !ok {
		log.Warn().Str("scraper", operation.ScraperID).Msg("stored scraper not available; marking scrape failed")
		if setErr := db.MediaDB.SetScrapingStatus(mediadb.IndexingStatusFailed); setErr != nil {
			log.Warn().Err(setErr).Msg("failed to mark unavailable scraper as failed")
		}
		return
	}

	log.Info().Str("scraper", operation.ScraperID).Msg("detected interrupted media scraping, automatically resuming")
	env := requests.RequestEnv{
		Context:      st.GetContext(),
		Platform:     pl,
		Config:       cfg,
		State:        st,
		Database:     db,
		ScrapePauser: pauser,
	}
	err = methods.ResumeMediaScrape(&env, operation)
	if err != nil {
		if setErr := db.MediaDB.SetScrapingStatus(mediadb.IndexingStatusFailed); setErr != nil {
			log.Warn().Err(setErr).Msg("failed to persist scraping auto-resume failure status")
		}
		if clearErr := db.MediaDB.ClearScrapingOperation(); clearErr != nil {
			log.Warn().Err(clearErr).Msg("failed to clear scraping operation after auto-resume failure")
		}
		log.Error().Err(err).Str("scraper", operation.ScraperID).Msg("failed to start auto-resume of media scraping")
	}
}

// checkAndResumeOptimization resumes an interrupted optimization, or flags the database
// corrupt when a failed optimization turns out to be a malformed file. It returns true
// when it marked the database corrupt, so the caller can trigger recovery immediately
// rather than waiting for the next startup.
func checkAndResumeOptimization(db *database.Database, ns chan<- models.Notification, pauser *syncutil.Pauser) bool {
	status, err := db.MediaDB.GetOptimizationStatus()
	if err != nil {
		log.Debug().Err(err).Msg("failed to get optimization status during startup check")
		return false
	}

	// Resume if optimization was interrupted or failed
	if status == mediadb.IndexingStatusPending ||
		status == mediadb.IndexingStatusRunning ||
		status == mediadb.IndexingStatusFailed {
		// A failed optimization is often the symptom of a corrupt database — e.g. a
		// PRAGMA optimize that hit a malformed page. Resuming would just fail again
		// on every boot, so confirm integrity first and route confirmed corruption
		// to the rebuild flow (IndexingStatusCorrupt) instead of looping.
		if status == mediadb.IndexingStatusFailed {
			switch ok, checkErr := db.MediaDB.QuickCheck(); {
			case checkErr != nil:
				log.Warn().Err(checkErr).Msg("failed to run quick_check before resuming optimization")
			case !ok:
				log.Error().Strs("integrity", db.MediaDB.IntegrityReport()).
					Msg("media database failed integrity check; marking corrupt, skipping optimization resume")
				db.MediaDB.MarkCorrupt("quick_check failed before optimization resume")
				if setErr := db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusCorrupt); setErr != nil {
					log.Error().Err(setErr).Msg("failed to mark media database as corrupt")
				}
				return true
			}
		}
		log.Info().Msgf("detected incomplete optimization (status: %s), automatically resuming", status)
		db.MediaDB.RunBackgroundOptimization(func(optimizing bool) {
			notifications.MediaIndexing(ns, models.IndexingStatusResponse{
				Exists:     true,
				Indexing:   false,
				Optimizing: optimizing,
			})
		}, pauser)
	} else {
		log.Debug().Msgf("optimization status is '%s', no auto-resume needed", status)
	}
	return false
}
