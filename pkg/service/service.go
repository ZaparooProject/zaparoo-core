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
	"os"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb/boltmigration"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/groovyproxy"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/discovery"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/publishers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"github.com/mackerelio/go-osstat/uptime"
	"github.com/rs/zerolog/log"
)

const zapLinkHostExpiration = 30 * 24 * time.Hour

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

	log.Debug().Msg("opening media database")
	mediaDB, err := mediadb.OpenMediaDB(ctx, pl)
	if err != nil {
		return db, fmt.Errorf("failed to open media database: %w", err)
	}

	log.Debug().Msg("running media database migrations")
	err = mediaDB.MigrateUp()
	if err != nil {
		return db, fmt.Errorf("error migrating mediadb: %w", err)
	}

	db.MediaDB = mediaDB

	log.Debug().Msg("opening user database")
	userDB, err := userdb.OpenUserDB(ctx, pl)
	if err != nil {
		return db, fmt.Errorf("failed to open user database: %w", err)
	}

	log.Debug().Msg("running user database migrations")
	err = userDB.MigrateUp()
	if err != nil {
		return db, fmt.Errorf("error migrating userdb: %w", err)
	}

	db.UserDB = userDB

	// migrate old boltdb mappings if required
	log.Debug().Msg("checking for boltdb migration")
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

func Start(
	pl platforms.Platform,
	cfg *config.Instance,
) (stop func() error, done <-chan struct{}, err error) {
	log.Info().Msgf("version: %s", config.AppVersion)

	// Generate boot UUID for this session (for timestamp healing on MiSTer)
	bootUUID := uuid.New().String()
	log.Info().Msgf("boot session UUID: %s", bootUUID)

	// TODO: define the notifications chan here instead of in state
	st, ns := state.NewState(pl, bootUUID) // global state, notification queue (source)

	// Create and start notification broker to broadcast to all consumers
	notifBroker := broker.NewBroker(st.GetContext(), ns)
	notifBroker.Start()

	// TODO: convert this to a *token channel
	itq := make(chan tokens.Token)        // input token queue
	lsq := make(chan *tokens.Token)       // launch software queue
	plq := make(chan *playlists.Playlist) // playlist event queue

	err = setupEnvironment(pl)
	if err != nil {
		log.Error().Err(err).Msg("error setting up environment")
		return nil, nil, err
	}

	log.Info().Msg("running platform pre start")
	err = pl.StartPre(cfg)
	if err != nil {
		log.Error().Err(err).Msg("platform start pre error")
		return nil, nil, fmt.Errorf("platform start pre failed: %w", err)
	}

	log.Info().Msg("opening databases")
	db, err := makeDatabase(st.GetContext(), pl)
	if err != nil {
		log.Error().Err(err).Msgf("error opening databases")
		return nil, nil, err
	}

	// Perform all history cleanup operations
	cleanupHistoryOnStartup(cfg, db)

	pruneExpiredZapLinkHosts(db)
	go zapscript.PreWarmZapLinkHosts(db, pl.ID(), helpers.WaitForInternet)

	// Initialize inbox service for system notifications
	log.Info().Msg("initializing inbox service")
	st.SetInbox(inbox.NewService(db.UserDB, st.Notifications))

	// Initialize playtime limits system (always create for runtime enable/disable)
	log.Info().Msg("initializing playtime limits")
	limitsManager := playtime.NewLimitsManager(db, pl, cfg, clockwork.NewRealClock())
	limitsManager.Start(notifBroker, st.Notifications)
	if cfg.PlaytimeLimitsEnabled() {
		limitsManager.SetEnabled(true)
	}

	// Set up the OnMediaStart hook
	st.SetOnMediaStartHook(func(_ *models.ActiveMedia) {
		if script := cfg.LaunchersOnMediaStart(); script != "" {
			if hookErr := runHook(pl, cfg, st, db, lsq, plq, "on_media_start", script, nil); hookErr != nil {
				log.Error().Err(hookErr).Msg("error running on_media_start script")
			}
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

	log.Info().Msg("starting mDNS discovery service")
	discoveryService := discovery.New(cfg, pl.ID())
	if discoveryErr := discoveryService.Start(); discoveryErr != nil {
		log.Error().Err(discoveryErr).Msg("mDNS discovery failed to start (continuing without discovery)")
	}

	log.Info().Msg("starting API service")
	apiNotifications, _ := notifBroker.Subscribe(100)
	go api.Start(pl, cfg, st, itq, db, limitsManager, apiNotifications, discoveryService.InstanceName())

	log.Info().Msg("starting publishers")
	publisherNotifications, _ := notifBroker.Subscribe(100)
	activePublishers, cancelPublisherFanOut := startPublishers(st, cfg, publisherNotifications)

	// Start media history tracking
	log.Info().Msg("starting media history listener")
	historyTracker := &mediaHistoryTracker{
		st:    st,
		db:    db,
		clock: clockwork.NewRealClock(),
	}
	historyNotifications, _ := notifBroker.Subscribe(100)
	go historyTracker.listen(historyNotifications)
	log.Info().Msg("starting media history PlayTime updater")
	go historyTracker.updatePlayTime(st.GetContext())

	// Start clock reliability monitor for timestamp healing (MiSTer NTP sync)
	log.Info().Msg("starting clock reliability monitor")
	go monitorClockAndHealTimestamps(st.GetContext(), db, bootUUID)

	if cfg.GmcProxyEnabled() {
		log.Info().Msg("starting GroovyMiSTer GMC Proxy service")
		go groovyproxy.Start(cfg, st, itq)
	}

	log.Info().Msg("starting reader manager")
	go readerManager(pl, cfg, st, db, itq, lsq, plq)

	log.Info().Msg("starting input token queue manager")
	go processTokenQueue(pl, cfg, st, itq, db, lsq, plq, limitsManager)

	log.Info().Msg("running platform post start")
	err = pl.StartPost(cfg, st.LauncherManager(), st.ActiveMedia, st.SetActiveMedia, db)
	if err != nil {
		log.Error().Err(err).Msg("platform post start error")
		return nil, nil, fmt.Errorf("platform start post failed: %w", err)
	}
	log.Info().Msg("platform post start completed, service fully initialized")

	doneCh := make(chan struct{})
	go func() {
		<-st.GetContext().Done()
		log.Info().Msg("service context cancelled, running cleanup")

		discoveryService.Stop()
		cancelPublisherFanOut()
		for _, publisher := range activePublishers {
			publisher.Stop()
		}
		if stopErr := pl.Stop(); stopErr != nil {
			log.Warn().Msgf("error stopping platform: %s", stopErr)
		}
		notifBroker.Stop()
		close(plq)
		close(lsq)
		close(itq)

		log.Info().Msg("service cleanup completed")
		close(doneCh)
	}()

	stop = func() error {
		st.StopService()
		<-doneCh
		return nil
	}
	done = doneCh
	return stop, done, nil
}

// monitorClockAndHealTimestamps monitors the system clock and heals timestamps when NTP syncs.
// This is critical for MiSTer devices that boot without RTC and initially show 1970 epoch time.
// Once NTP syncs, we can mathematically reconstruct correct timestamps using monotonic uptime.
func monitorClockAndHealTimestamps(ctx context.Context, db *database.Database, bootUUID string) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	healed := false
	wasReliable := helpers.IsClockReliable(time.Now())

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			isReliable := helpers.IsClockReliable(now)

			// Detect transition from unreliable → reliable (NTP sync event)
			if !wasReliable && isReliable && !healed {
				log.Info().Msg("clock became reliable (NTP sync detected), healing timestamps")

				// Calculate true boot time: Current Time - System Uptime
				systemUptime, err := uptime.Get()
				if err != nil {
					log.Error().Err(err).Msg("failed to get system uptime for timestamp healing")
					wasReliable = isReliable
					continue
				}

				trueBootTime := now.Add(-systemUptime)
				log.Info().
					Time("true_boot_time", trueBootTime).
					Dur("uptime", systemUptime).
					Msg("calculated true boot time")

				// Heal all timestamps for this boot session
				rowsHealed, healErr := db.UserDB.HealTimestamps(bootUUID, trueBootTime)
				if healErr != nil {
					log.Error().Err(healErr).Msg("failed to heal timestamps")
				} else if rowsHealed > 0 {
					log.Info().Int64("rows", rowsHealed).Msg("successfully healed timestamps")
				}

				healed = true
			}

			wasReliable = isReliable

		case <-ctx.Done():
			return
		}
	}
}

// mediaHistoryTracker encapsulates the state and logic for tracking media history.
// It coordinates between the notification listener and the periodic PlayTime updater.
type mediaHistoryTracker struct {
	clock                     clockwork.Clock
	currentMediaStartTime     time.Time
	currentMediaStartTimeMono time.Time
	st                        *state.State
	db                        *database.Database
	currentHistoryDBID        int64
	mu                        syncutil.RWMutex
}

// listen processes media start/stop notifications and records them in the database.
func (t *mediaHistoryTracker) listen(notificationChan <-chan models.Notification) {
	for notif := range notificationChan {
		switch notif.Method {
		case models.NotificationStarted:
			// Media started - create new history entry
			activeMedia := t.st.ActiveMedia()
			if activeMedia != nil {
				now := t.clock.Now()
				nowMono := time.Now() // Monotonic clock for duration calculation

				// Calculate system uptime for timestamp healing on MiSTer
				systemUptime, uptimeErr := uptime.Get()
				if uptimeErr != nil {
					log.Warn().Err(uptimeErr).Msg("failed to get system uptime, using 0")
					systemUptime = 0
				}
				monotonicStart := int64(systemUptime.Seconds())

				// Determine clock reliability and source
				clockReliable := helpers.IsClockReliable(now)
				var clockSource string
				if clockReliable {
					clockSource = helpers.ClockSourceSystem
				} else {
					clockSource = helpers.ClockSourceEpoch
				}

				entry := &database.MediaHistoryEntry{
					ID:             uuid.New().String(),
					StartTime:      activeMedia.Started,
					SystemID:       activeMedia.SystemID,
					SystemName:     activeMedia.SystemName,
					MediaPath:      activeMedia.Path,
					MediaName:      activeMedia.Name,
					LauncherID:     activeMedia.LauncherID,
					PlayTime:       0,
					BootUUID:       t.st.BootUUID(),
					MonotonicStart: monotonicStart,
					DurationSec:    0,
					WallDuration:   0,
					TimeSkewFlag:   false,
					ClockReliable:  clockReliable,
					ClockSource:    clockSource,
					CreatedAt:      now,
					UpdatedAt:      now,
				}
				dbid, addErr := t.db.UserDB.AddMediaHistory(entry)
				if addErr != nil {
					log.Error().Err(addErr).Msg("failed to add media history entry")
				} else {
					t.mu.Lock()
					t.currentHistoryDBID = dbid
					t.currentMediaStartTime = activeMedia.Started
					t.currentMediaStartTimeMono = nowMono
					t.mu.Unlock()
					log.Debug().Int64("dbid", dbid).Msg("created media history entry")
				}
			}

		case models.NotificationStopped:
			// Media stopped - close history entry
			t.mu.Lock()
			dbid := t.currentHistoryDBID
			startTime := t.currentMediaStartTime
			startTimeMono := t.currentMediaStartTimeMono
			t.currentHistoryDBID = 0
			t.currentMediaStartTime = time.Time{}
			t.currentMediaStartTimeMono = time.Time{}
			t.mu.Unlock()

			if dbid != 0 {
				endTime := t.clock.Now()

				// Calculate duration - prefer monotonic if available, fall back to wall-clock
				var playTime int
				if !startTimeMono.IsZero() {
					// Use monotonic clock (more accurate, handles sleep)
					endTimeMono := time.Now()
					playTime = int(endTimeMono.Sub(startTimeMono).Seconds())
				} else {
					// Fall back to wall-clock (for tests or if mono not initialized)
					playTime = int(endTime.Sub(startTime).Seconds())
				}

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
			startTimeMono := t.currentMediaStartTimeMono
			t.mu.RUnlock()

			if dbid != 0 {
				// Calculate duration - prefer monotonic if available, fall back to wall-clock
				var playTime int
				switch {
				case !startTimeMono.IsZero():
					// Use monotonic clock (more accurate, handles sleep/hibernate)
					nowMono := time.Now()
					playTime = int(nowMono.Sub(startTimeMono).Seconds())
				case !startTime.IsZero():
					// Fall back to wall-clock (for tests or if mono not initialized)
					playTime = int(t.clock.Since(startTime).Seconds())
				default:
					// No valid start time - skip update
					continue
				}

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
	activePublishers := make([]*publishers.MQTTPublisher, 0)

	mqttConfigs := cfg.GetMQTTPublishers()
	if len(mqttConfigs) > 0 {
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
	}

	if len(activePublishers) > 0 {
		log.Info().Msgf("started %d MQTT publisher(s)", len(activePublishers))
	}

	// CRITICAL: Always start the drain goroutine, even if there are no active publishers.
	// The notifChan MUST be consumed or it will fill up and block the notification system.
	// If there are no publishers, notifications are simply discarded after being consumed.
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
				// If no publishers, notification is simply discarded
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
