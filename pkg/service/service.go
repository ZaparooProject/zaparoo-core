/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb/boltmigration"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/groovyproxy"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/publishers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

func setupEnvironment(pl platforms.Platform) error {
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
		err := os.MkdirAll(dir, 0o750)
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

	mediaDB, err := mediadb.OpenMediaDB(ctx, pl)
	if err != nil {
		return db, fmt.Errorf("failed to open media database: %w", err)
	}

	err = mediaDB.MigrateUp()
	if err != nil {
		return db, fmt.Errorf("error migrating mediadb: %w", err)
	}

	db.MediaDB = mediaDB

	userDB, err := userdb.OpenUserDB(ctx, pl)
	if err != nil {
		return db, fmt.Errorf("failed to open user database: %w", err)
	}

	err = userDB.MigrateUp()
	if err != nil {
		return db, fmt.Errorf("error migrating userdb: %w", err)
	}

	db.UserDB = userDB

	// migrate old boltdb mappings if required
	err = boltmigration.MaybeMigrate(pl, userDB)
	if err != nil {
		log.Error().Err(err).Msg("error migrating old boltdb mappings")
	}

	return db, nil
}

// cleanupHistoryOnStartup performs all history cleanup operations at service startup
func cleanupHistoryOnStartup(cfg *config.Instance, db *database.Database) {
	// Cleanup old scan history entries if retention is configured
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

	// Close any hanging media history entries from unclean shutdown
	log.Info().Msg("closing hanging media history entries")
	if hangingErr := db.UserDB.CloseHangingMediaHistory(); hangingErr != nil {
		log.Error().Err(hangingErr).Msg("error closing hanging media history entries")
	}

	// Cleanup old media history entries if retention is configured
	mediaHistoryDays := cfg.MediaHistory()
	if mediaHistoryDays > 0 {
		log.Info().Msgf("cleaning up media history older than %d days", mediaHistoryDays)
		rowsDeleted, cleanupErr := db.UserDB.CleanupMediaHistory(mediaHistoryDays)
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

func Start(
	pl platforms.Platform,
	cfg *config.Instance,
) (func() error, error) {
	log.Info().Msgf("version: %s", config.AppVersion)

	// TODO: define the notifications chan here instead of in state
	st, ns := state.NewState(pl) // global state, notification queue (source)

	// Create separate notification channels for API, publishers, and history to avoid race conditions
	apiNotifications := make(chan models.Notification, 100)
	publisherNotifications := make(chan models.Notification, 100)
	historyNotifications := make(chan models.Notification, 100)

	// Start main fan-out goroutine to broadcast notifications to all consumers
	go func() {
		for notif := range ns {
			select {
			case apiNotifications <- notif:
			case <-st.GetContext().Done():
				return
			}
			select {
			case publisherNotifications <- notif:
			case <-st.GetContext().Done():
				return
			}
			select {
			case historyNotifications <- notif:
			case <-st.GetContext().Done():
				return
			}
		}
		close(apiNotifications)
		close(publisherNotifications)
		close(historyNotifications)
	}()

	// TODO: convert this to a *token channel
	itq := make(chan tokens.Token)        // input token queue
	lsq := make(chan *tokens.Token)       // launch software queue
	plq := make(chan *playlists.Playlist) // playlist event queue

	err := setupEnvironment(pl)
	if err != nil {
		log.Error().Err(err).Msg("error setting up environment")
		return nil, err
	}

	log.Info().Msg("initializing audio system")
	if audioErr := audio.Initialize(); audioErr != nil {
		log.Warn().Err(audioErr).Msg("failed to initialize audio - audio feedback will be disabled")
	}

	log.Info().Msg("running platform pre start")
	err = pl.StartPre(cfg)
	if err != nil {
		log.Error().Err(err).Msg("platform start pre error")
		return nil, fmt.Errorf("platform start pre failed: %w", err)
	}

	log.Info().Msg("opening databases")
	db, err := makeDatabase(st.GetContext(), pl)
	if err != nil {
		log.Error().Err(err).Msgf("error opening databases")
		return nil, err
	}

	// Perform all history cleanup operations
	cleanupHistoryOnStartup(cfg, db)

	// Set up the OnMediaStart hook
	st.SetOnMediaStartHook(func(_ *models.ActiveMedia) {
		onMediaStartScript := cfg.LaunchersOnMediaStart()
		if onMediaStartScript == "" {
			return
		}

		log.Info().Msgf("running on_media_start script: %s", onMediaStartScript)
		plsc := playlists.PlaylistController{
			Active: st.GetActivePlaylist(),
			Queue:  plq,
		}
		t := tokens.Token{
			ScanTime: time.Now(),
			Text:     onMediaStartScript,
		}

		if scriptErr := runTokenZapScript(pl, cfg, st, t, db, lsq, plsc); scriptErr != nil {
			log.Error().Err(scriptErr).Msg("Error running on_media_start script")
		}
	})

	log.Info().Msg("loading mapping files")
	err = cfg.LoadMappings(filepath.Join(helpers.DataDir(pl), config.MappingsDir))
	if err != nil {
		log.Error().Err(err).Msgf("error loading mapping files")
	}

	log.Info().Msg("loading custom launchers")
	err = cfg.LoadCustomLaunchers(filepath.Join(helpers.DataDir(pl), config.LaunchersDir))
	if err != nil {
		log.Error().Err(err).Msgf("error loading custom launchers")
	}

	log.Info().Msg("initializing launcher cache")
	helpers.GlobalLauncherCache.Initialize(pl, cfg)

	log.Info().Msg("checking for interrupted media indexing")
	go checkAndResumeIndexing(pl, cfg, db, st)

	log.Info().Msg("checking for interrupted media optimization")
	go checkAndResumeOptimization(db, st.Notifications)

	log.Info().Msg("starting API service")
	go api.Start(pl, cfg, st, itq, db, apiNotifications)

	log.Info().Msg("starting publishers")
	activePublishers, cancelPublisherFanOut := startPublishers(st, cfg, publisherNotifications)

	// Start media history tracking
	log.Info().Msg("starting media history listener")
	historyTracker := &mediaHistoryTracker{
		st:    st,
		db:    db,
		clock: clockwork.NewRealClock(),
	}
	go historyTracker.listen(historyNotifications)
	log.Info().Msg("starting media history PlayTime updater")
	go historyTracker.updatePlayTime(st.GetContext())

	if cfg.GmcProxyEnabled() {
		log.Info().Msg("starting GroovyMiSTer GMC Proxy service")
		go groovyproxy.Start(cfg, st, itq)
	}

	log.Info().Msg("starting reader manager")
	go readerManager(pl, cfg, st, db, itq, lsq, plq)

	log.Info().Msg("starting input token queue manager")
	go processTokenQueue(pl, cfg, st, itq, db, lsq, plq)

	log.Info().Msg("running platform post start")
	err = pl.StartPost(cfg, st.LauncherManager(), st.ActiveMedia, st.SetActiveMedia)
	if err != nil {
		log.Error().Err(err).Msg("platform post start error")
		return nil, fmt.Errorf("platform start post failed: %w", err)
	}
	log.Info().Msg("platform post start completed, service fully initialized")

	return func() error {
		cancelPublisherFanOut()
		for _, publisher := range activePublishers {
			publisher.Stop()
		}
		err = pl.Stop()
		if err != nil {
			log.Warn().Msgf("error stopping platform: %s", err)
		}
		st.StopService()
		close(plq)
		close(lsq)
		close(itq)
		if err := audio.Shutdown(); err != nil {
			log.Warn().Err(err).Msg("error shutting down audio")
		}
		return nil
	}, nil
}

// mediaHistoryTracker encapsulates the state and logic for tracking media history.
// It coordinates between the notification listener and the periodic PlayTime updater.
type mediaHistoryTracker struct {
	clock                 clockwork.Clock
	currentMediaStartTime time.Time
	st                    *state.State
	db                    *database.Database
	currentHistoryDBID    int64
	mu                    sync.RWMutex
}

// listen processes media start/stop notifications and records them in the database.
func (t *mediaHistoryTracker) listen(notificationChan <-chan models.Notification) {
	for notif := range notificationChan {
		switch notif.Method {
		case models.NotificationStarted:
			// Media started - create new history entry
			activeMedia := t.st.ActiveMedia()
			if activeMedia != nil {
				entry := &database.MediaHistoryEntry{
					StartTime:  activeMedia.Started,
					SystemID:   activeMedia.SystemID,
					SystemName: activeMedia.SystemName,
					MediaPath:  activeMedia.Path,
					MediaName:  activeMedia.Name,
					LauncherID: activeMedia.LauncherID,
					PlayTime:   0,
				}
				dbid, addErr := t.db.UserDB.AddMediaHistory(entry)
				if addErr != nil {
					log.Error().Err(addErr).Msg("failed to add media history entry")
				} else {
					t.mu.Lock()
					t.currentHistoryDBID = dbid
					t.currentMediaStartTime = activeMedia.Started
					t.mu.Unlock()
					log.Debug().Int64("dbid", dbid).Msg("created media history entry")
				}
			}

		case models.NotificationStopped:
			// Media stopped - close history entry
			t.mu.Lock()
			dbid := t.currentHistoryDBID
			startTime := t.currentMediaStartTime
			t.currentHistoryDBID = 0
			t.currentMediaStartTime = time.Time{}
			t.mu.Unlock()

			if dbid != 0 {
				endTime := t.clock.Now()
				playTime := int(endTime.Sub(startTime).Seconds())
				closeErr := t.db.UserDB.CloseMediaHistory(dbid, endTime, playTime)
				if closeErr != nil {
					log.Error().Err(closeErr).Int64("dbid", dbid).Msg("failed to close media history entry")
				} else {
					log.Debug().Int64("dbid", dbid).Int("playTime", playTime).Msg("closed media history entry")
				}
			}
		}
	}
}

// updatePlayTime periodically updates the PlayTime for the currently active media
// history entry every minute.
func (t *mediaHistoryTracker) updatePlayTime(ctx context.Context) {
	ticker := t.clock.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.Chan():
			t.mu.RLock()
			dbid := t.currentHistoryDBID
			startTime := t.currentMediaStartTime
			t.mu.RUnlock()

			if dbid != 0 {
				playTime := int(t.clock.Since(startTime).Seconds())
				updateErr := t.db.UserDB.UpdateMediaHistoryTime(dbid, playTime)
				if updateErr != nil {
					log.Warn().Err(updateErr).Msg("failed to update media history play time")
				} else {
					log.Debug().Int64("dbid", dbid).Int("playTime", playTime).Msg("updated media history play time")
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// startPublishers initializes and starts all configured publishers.
// Returns a slice of active publishers and a cancel function for graceful shutdown.
func startPublishers(
	st *state.State,
	cfg *config.Instance,
	notifChan <-chan models.Notification,
) ([]*publishers.MQTTPublisher, context.CancelFunc) {
	mqttConfigs := cfg.GetMQTTPublishers()
	if len(mqttConfigs) == 0 {
		return nil, func() {}
	}

	activePublishers := make([]*publishers.MQTTPublisher, 0, len(mqttConfigs))

	for _, mqttCfg := range mqttConfigs {
		// Skip if explicitly disabled (nil = enabled by default)
		if mqttCfg.Enabled != nil && !*mqttCfg.Enabled {
			continue
		}

		log.Info().Msgf("starting MQTT publisher: %s (topic: %s)", mqttCfg.Broker, mqttCfg.Topic)

		publisher := publishers.NewMQTTPublisher(mqttCfg.Broker, mqttCfg.Topic, mqttCfg.Filter)
		if err := publisher.Start(); err != nil {
			log.Error().Err(err).Msgf("failed to start MQTT publisher for %s", mqttCfg.Broker)
			continue
		}

		activePublishers = append(activePublishers, publisher)
	}

	if len(activePublishers) == 0 {
		return nil, func() {}
	}

	log.Info().Msgf("started %d MQTT publisher(s)", len(activePublishers))

	// Start a single fan-out goroutine to broadcast notifications to all publishers
	// Derived from service context so it automatically stops when service stops
	ctx, cancel := context.WithCancel(st.GetContext())
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("mqtt publisher fan-out: stopping")
				return
			case notif, ok := <-notifChan:
				if !ok {
					log.Debug().Msg("mqtt publisher fan-out: notification channel closed")
					return
				}
				// Publish to all active publishers sequentially
				// Timeout in Publish() prevents blocking indefinitely
				for _, pub := range activePublishers {
					if err := pub.Publish(notif); err != nil {
						log.Warn().Err(err).Msgf("failed to publish %s notification", notif.Method)
					}
				}
			}
		}
	}()

	return activePublishers, cancel
}

// checkAndResumeIndexing checks if media indexing was interrupted and automatically resumes it
func checkAndResumeIndexing(
	pl platforms.Platform,
	cfg *config.Instance,
	db *database.Database,
	st *state.State,
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
	err = methods.GenerateMediaDB(st.GetContext(), pl, cfg, st.Notifications, systems, db)
	if err != nil {
		log.Error().Err(err).Msg("failed to start auto-resume of media indexing")
	}
}

// checkAndResumeOptimization checks if optimization was interrupted and automatically resumes it
func checkAndResumeOptimization(db *database.Database, ns chan<- models.Notification) {
	status, err := db.MediaDB.GetOptimizationStatus()
	if err != nil {
		log.Debug().Err(err).Msg("failed to get optimization status during startup check")
		return
	}

	// Resume if optimization was interrupted or failed
	if status == mediadb.IndexingStatusPending ||
		status == mediadb.IndexingStatusRunning ||
		status == mediadb.IndexingStatusFailed {
		log.Info().Msgf("detected incomplete optimization (status: %s), automatically resuming", status)
		go db.MediaDB.RunBackgroundOptimization(func(optimizing bool) {
			notifications.MediaIndexing(ns, models.IndexingStatusResponse{
				Exists:     true,
				Indexing:   false,
				Optimizing: optimizing,
			})
		})
	} else {
		log.Debug().Msgf("optimization status is '%s', no auto-resume needed", status)
	}
}
