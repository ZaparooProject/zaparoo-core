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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
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

	successSoundPath := filepath.Join(
		helpers.DataDir(pl),
		config.AssetsDir,
		config.SuccessSoundFilename,
	)
	if _, err := os.Stat(successSoundPath); err != nil {
		// copy success sound to temp
		//nolint:gosec // Safe: creates audio files in controlled application directories
		sf, err := os.Create(successSoundPath)
		if err != nil {
			log.Error().Msgf("error creating success sound file: %s", err)
		}
		_, err = sf.Write(assets.SuccessSound)
		if err != nil {
			log.Error().Msgf("error writing success sound file: %s", err)
		}
		_ = sf.Close()
	}

	failSoundPath := filepath.Join(
		helpers.DataDir(pl),
		config.AssetsDir,
		config.FailSoundFilename,
	)
	if _, err := os.Stat(failSoundPath); err != nil {
		// copy fail sound to temp
		//nolint:gosec // Safe: creates audio files in controlled application directories
		ff, err := os.Create(failSoundPath)
		if err != nil {
			log.Error().Msgf("error creating fail sound file: %s", err)
		}
		_, err = ff.Write(assets.FailSound)
		if err != nil {
			log.Error().Msgf("error writing fail sound file: %s", err)
		}
		_ = ff.Close()
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

func Start(
	pl platforms.Platform,
	cfg *config.Instance,
) (func() error, error) {
	log.Info().Msgf("version: %s", config.AppVersion)

	// TODO: define the notifications chan here instead of in state
	st, ns := state.NewState(pl) // global state, notification queue
	// TODO: convert this to a *token channel
	itq := make(chan tokens.Token)        // input token queue
	lsq := make(chan *tokens.Token)       // launch software queue
	plq := make(chan *playlists.Playlist) // playlist event queue

	err := setupEnvironment(pl)
	if err != nil {
		log.Error().Err(err).Msg("error setting up environment")
		return nil, err
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
	go api.Start(pl, cfg, st, itq, db, ns)

	log.Info().Msg("starting publishers")
	activePublishers := startPublishers(cfg, ns)

	if cfg.GmcProxyEnabled() {
		log.Info().Msg("starting GroovyMiSTer GMC Proxy service")
		go groovyproxy.Start(cfg, st, itq)
	}

	log.Info().Msg("starting reader manager")
	go readerManager(pl, cfg, st, db, itq, lsq, plq)

	log.Info().Msg("starting input token queue manager")
	go processTokenQueue(pl, cfg, st, itq, db, lsq, plq)

	log.Info().Msg("running platform post start")
	err = pl.StartPost(cfg, st.ActiveMedia, st.SetActiveMedia)
	if err != nil {
		log.Error().Err(err).Msg("platform post start error")
		return nil, fmt.Errorf("platform start post failed: %w", err)
	}
	log.Info().Msg("platform post start completed, service fully initialized")

	return func() error {
		// Stop publishers gracefully
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
		return nil
	}, nil
}

// startPublishers initializes and starts all configured publishers.
// Returns a slice of active publishers for graceful shutdown.
func startPublishers(cfg *config.Instance, notifChan <-chan models.Notification) []*publishers.MQTTPublisher {
	mqttConfigs := cfg.GetMQTTPublishers()
	if len(mqttConfigs) == 0 {
		return nil
	}

	activePublishers := make([]*publishers.MQTTPublisher, 0, len(mqttConfigs))
	publisherChans := make([]chan models.Notification, 0, len(mqttConfigs))

	for _, mqttCfg := range mqttConfigs {
		// Skip if explicitly disabled (nil = enabled by default)
		if mqttCfg.Enabled != nil && !*mqttCfg.Enabled {
			continue
		}

		log.Info().Msgf("starting MQTT publisher: %s (topic: %s)", mqttCfg.Broker, mqttCfg.Topic)

		// Create a dedicated channel for this publisher
		pChan := make(chan models.Notification, 10) // Use a buffered channel

		publisher := publishers.NewMQTTPublisher(mqttCfg.Broker, mqttCfg.Topic, mqttCfg.Filter)
		if err := publisher.Start(pChan); err != nil {
			log.Error().Err(err).Msgf("failed to start MQTT publisher for %s", mqttCfg.Broker)
			continue
		}

		activePublishers = append(activePublishers, publisher)
		publisherChans = append(publisherChans, pChan)
	}

	// Start a fan-out goroutine to broadcast notifications to all active publishers
	go func() {
		for notif := range notifChan {
			for _, pChan := range publisherChans {
				// Use a non-blocking send to prevent a slow publisher from blocking others
				select {
				case pChan <- notif:
				default:
					log.Warn().Msg("MQTT publisher channel full, dropping notification.")
				}
			}
		}
		// When the main notification channel closes, close all publisher channels
		for _, pChan := range publisherChans {
			close(pChan)
		}
	}()

	if len(activePublishers) > 0 {
		log.Info().Msgf("started %d MQTT publisher(s)", len(activePublishers))
	}

	return activePublishers
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
