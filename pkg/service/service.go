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
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/groovyproxy"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/discovery"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// StartResult holds the return values from Start.
type StartResult struct {
	Stop             func() error
	Done             <-chan struct{}
	RestartRequested func() bool
}

func rebuildStartupSlugSearchCache(mediaDB database.MediaDBI, slugCacheLoaded bool) {
	if mediaDB == nil || slugCacheLoaded {
		return
	}
	indexingStatus, statusErr := mediaDB.GetIndexingStatus()
	if statusErr != nil {
		log.Warn().Err(statusErr).Msg("failed to get indexing status before slug cache rebuild")
		return
	}
	if indexingStatus == mediadb.IndexingStatusRunning || indexingStatus == mediadb.IndexingStatusPending {
		log.Debug().Str("status", indexingStatus).Msg("skipping slug search cache rebuild during indexing")
		return
	}

	mediaDB.TrackBackgroundOperation()
	defer mediaDB.BackgroundOperationDone()
	if cacheErr := mediaDB.RebuildSlugSearchCache(); cacheErr != nil {
		log.Warn().Err(cacheErr).Msg("failed to build slug search cache")
		return
	}
	if persistErr := mediaDB.PersistSlugSearchCache(); persistErr != nil {
		log.Warn().Err(persistErr).
			Msg("failed to persist slug search cache after startup rebuild")
	}
}

type drainCallbackRegistrar interface {
	SetDrainCallback(slot string, fn func(natural bool))
}

func wireNativeAudioDrainCallbacks(pm drainCallbackRegistrar, svc *ServiceContext) {
	pm.SetDrainCallback(mediaslot.Primary, func(_ bool) {
		svc.State.SetActiveMedia(nil)
	})
	pm.SetDrainCallback(mediaslot.Background, func(natural bool) {
		if !natural {
			return
		}
		advanceBackgroundPlaylist(svc)
	})
}

// advanceBackgroundPlaylist is called when a background track ends naturally.
// It advances to the next track according to the playlist's repeat mode, or clears
// the background state when the playlist has finished and repeat is off.
func advanceBackgroundPlaylist(svc *ServiceContext) {
	pls := svc.State.GetBackgroundPlaylist()
	if pls == nil {
		// Single-track background (not a playlist) — just clear media state.
		svc.State.SetBackgroundMedia(nil)
		return
	}

	var next *playlists.Playlist
	switch {
	case pls.LoopOne:
		// Repeat the same track. ForceRelaunch bypasses the playlistNeedsUpdate dedup.
		next = &playlists.Playlist{
			ID:            pls.ID,
			Name:          pls.Name,
			Slot:          pls.Slot,
			Items:         pls.Items,
			Index:         pls.Index,
			Playing:       true,
			Loop:          pls.Loop,
			LoopOne:       pls.LoopOne,
			ForceRelaunch: true,
		}
	case pls.Index+1 < len(pls.Items):
		// More tracks remain — advance normally.
		next = playlists.Next(*pls)
		next.Playing = true
	case pls.Loop:
		// Last track finished and repeat=all — wrap back to the start.
		next = playlists.Next(*pls) // Next already wraps to 0
		next.Playing = true
		if len(pls.Items) <= 1 {
			// Single-item loop: same index after wrap, need ForceRelaunch.
			next.ForceRelaunch = true
		}
	default:
		// repeat=off, end of playlist — clear state and stop.
		svc.State.SetBackgroundPlaylist(nil)
		svc.State.SetBackgroundMedia(nil)
		return
	}

	select {
	case svc.PlaylistQueue <- next:
	case <-svc.State.GetContext().Done():
	}
}

func Start(
	pl platforms.Platform,
	cfg *config.Instance,
) (*StartResult, error) {
	log.Info().Msgf("version: %s", config.AppVersion)

	// Generate boot UUID for this session (for timestamp healing on MiSTer)
	bootUUID := uuid.New().String()
	log.Info().Msgf("boot session UUID: %s", bootUUID)

	player := audio.NewMalgoPlayer()
	player.SetVolume(float64(cfg.AudioVolume()) / 100.0)
	playbackManager := audio.NewLongformPlaybackManager()

	// TODO: define the notifications chan here instead of in state
	st, ns := state.NewState(pl, bootUUID) // global state, notification queue (source)

	// Create and start notification broker to broadcast to all consumers.
	// media.indexing is coalesceable: bursts during index/resume collapse to
	// latest-wins so slow WebSocket consumers don't drop discrete events.
	notifBroker := broker.NewBroker(st.GetContext(), ns, models.NotificationMediaIndexing)
	notifBroker.Start()

	// TODO: convert this to a *token channel
	itq := make(chan tokens.Token)        // input token queue
	lsq := make(chan *tokens.Token)       // launch software queue
	plq := make(chan *playlists.Playlist) // playlist event queue
	cfq := make(chan chan error)          // launch guard confirm queue
	backgroundWG := &sync.WaitGroup{}

	setupStarted := time.Now()
	err := setupEnvironment(pl)
	if err != nil {
		log.Error().Err(err).Msg("error setting up environment")
		return nil, err
	}
	log.Debug().Dur("duration", time.Since(setupStarted)).Msg("setup environment completed")

	log.Info().Msg("running platform pre start")
	preStartStarted := time.Now()
	err = pl.StartPre(cfg)
	if err != nil {
		log.Error().Err(err).Msg("platform start pre error")
		return nil, fmt.Errorf("platform start pre failed: %w", err)
	}
	log.Debug().Dur("duration", time.Since(preStartStarted)).Msg("platform pre start completed")

	log.Info().Msg("opening databases")
	databaseStarted := time.Now()
	db, err := makeDatabase(st.GetContext(), pl)
	if err != nil {
		log.Error().Err(err).Msgf("error opening databases")
		return nil, err
	}
	log.Debug().Dur("duration", time.Since(databaseStarted)).Msg("databases opened")
	closeHangingMediaHistoryOnStartup(db)

	// Initialize inbox service for system notifications
	log.Info().Msg("initializing inbox service")
	st.SetInbox(inbox.NewService(db.UserDB, st.Notifications))

	// Initialize playtime limits system (always create for runtime enable/disable)
	log.Info().Msg("initializing playtime limits")
	limitsManager := playtime.NewLimitsManager(db, pl, cfg, clockwork.NewRealClock(), player)
	limitsManager.Start(notifBroker, st.Notifications)
	if cfg.PlaytimeLimitsEnabled() {
		limitsManager.SetEnabled(true)
	}

	svc := &ServiceContext{
		Platform:            pl,
		Config:              cfg,
		State:               st,
		DB:                  db,
		PlaybackManager:     playbackManager,
		LaunchSoftwareQueue: lsq,
		PlaylistQueue:       plq,
		ConfirmQueue:        cfq,
		BackgroundWG:        backgroundWG,
	}
	wireNativeAudioDrainCallbacks(playbackManager, svc)

	// Set up media readiness and the OnMediaStart hook.
	st.SetOnMediaStartHook(func(media *models.ActiveMedia, gen uint64) {
		startMediaReadyProbe(svc, media, gen)
		if script := cfg.LaunchersOnMediaStart(); script != "" {
			if hookErr := runHook(svc, "on_media_start", script, nil, nil); hookErr != nil {
				log.Error().Err(hookErr).Msg("error running on_media_start script")
			}
		}
	})

	log.Info().Msg("loading mapping files")
	mappingsStarted := time.Now()
	err = cfg.LoadMappings(filepath.Join(helpers.DataDir(pl), config.MappingsDir))
	if err != nil {
		log.Error().Err(err).Msgf("error loading mapping files")
	}
	log.Debug().Dur("duration", time.Since(mappingsStarted)).Msg("mapping files loaded")

	log.Info().Msg("loading custom launchers")
	launchersStarted := time.Now()
	err = cfg.LoadCustomLaunchers(filepath.Join(helpers.DataDir(pl), config.LaunchersDir))
	if err != nil {
		log.Error().Err(err).Msgf("error loading custom launchers")
	}
	log.Debug().Dur("duration", time.Since(launchersStarted)).Msg("custom launchers loaded")

	log.Info().Msg("initializing launcher cache")
	launcherCacheStarted := time.Now()
	helpers.GlobalLauncherCache.Initialize(
		pl, cfg,
		platforms.NativeAudioLauncher(playbackManager, st.SetBackgroundMedia),
	)
	log.Debug().Dur("duration", time.Since(launcherCacheStarted)).Msg("launcher cache initialized")

	// Create pausers to pause heavy background media work while a game is running.
	indexPauser := syncutil.NewPauser()
	scrapePauser := syncutil.NewPauser()

	discoveryService := discovery.New(cfg)

	// Set up the idle scheduler before API startup so the in-flight
	// counter is wired through the very first request.
	idleSched := idle.New()

	log.Info().Msg("starting API service")
	apiReady := make(chan error, 1)
	apiDone := make(chan error, 1)
	go func() {
		apiDone <- api.StartWithReady(
			pl, cfg, st, itq, cfq, db, limitsManager,
			notifBroker, discoveryService.InstanceName(), player, indexPauser, scrapePauser,
			idleSched, apiReady,
		)
	}()

	apiReadyStarted := time.Now()
	if apiErr := <-apiReady; apiErr != nil {
		discoveryService.Stop()
		if stopErr := pl.Stop(); stopErr != nil {
			log.Warn().Msgf("error stopping platform after API startup failure: %s", stopErr)
		}
		if apiDoneErr := <-apiDone; apiDoneErr != nil {
			log.Debug().Err(apiDoneErr).Msg("API service returned after startup failure")
		}
		limitsManager.Stop()
		notifBroker.Stop()
		closeDatabase(db)
		return nil, fmt.Errorf("api startup failed: %w", apiErr)
	}
	log.Debug().Dur("duration", time.Since(apiReadyStarted)).Msg("API service reported ready")

	log.Info().Msg("starting mDNS discovery service")
	if discoveryErr := discoveryService.Start(); discoveryErr != nil {
		log.Warn().Err(discoveryErr).Msg("mDNS discovery initialization failed")
	}

	checkAndResumeScraping(pl, cfg, db, st, scrapePauser)

	idleSched.Schedule(
		st.GetContext(), "startup-media-work",
		5*time.Second, 300*time.Second,
		func(_ context.Context) {
			if db == nil {
				log.Warn().Msg("skipping startup media work: database is nil")
				return
			}

			var tagCacheLoaded, slugCacheLoaded bool
			if db.MediaDB != nil {
				tagCacheStarted := time.Now()
				loaded, loadErr := db.MediaDB.LoadCachedTagCache()
				if loadErr != nil {
					log.Warn().Err(loadErr).Msg("failed to load cached tag cache from disk")
				}
				log.Debug().
					Bool("loaded", loaded).
					Dur("duration", time.Since(tagCacheStarted)).
					Msg("cached tag cache load completed")
				tagCacheLoaded = loaded

				slugCacheStarted := time.Now()
				loaded, loadErr = db.MediaDB.LoadCachedSlugSearchCache()
				if loadErr != nil {
					log.Warn().Err(loadErr).Msg("failed to load cached slug search cache from disk")
				}
				log.Debug().
					Bool("loaded", loaded).
					Dur("duration", time.Since(slugCacheStarted)).
					Msg("cached slug search cache load completed")
				slugCacheLoaded = loaded
			}

			runMediaDBStartupMaintenance(st.GetContext(), db.MediaDB, indexPauser, tagCacheLoaded)
			checkAndResumeIndexing(pl, cfg, db, st, indexPauser)
			checkAndResumeOptimization(db, st.Notifications, indexPauser)

			rebuildStartupSlugSearchCache(db.MediaDB, slugCacheLoaded)
		},
	)

	// Defer non-critical "run eventually" work to the idle scheduler so it
	// doesn't compete with the launcher's first request for the single
	// ARM core, the SQLite file lock, or the network. Each of these has a
	// 300 s hard cap so they still run on a saturated system.
	idleSched.Schedule(
		st.GetContext(), "zaplink-prewarm",
		5*time.Second, 300*time.Second,
		func(ctx context.Context) {
			zapscript.PreWarmZapLinkHostsContext(ctx, db, helpers.WaitForInternetContext)
		},
	)
	idleSched.Schedule(
		st.GetContext(), "updater-check",
		5*time.Second, 300*time.Second,
		func(ctx context.Context) {
			updater.CheckAndNotify(
				ctx, cfg, pl.ID(), st.Inbox(),
				helpers.WaitForInternetContext, updater.Check,
				pl.ManagedByPackageManager(),
			)
		},
	)
	idleSched.Schedule(
		st.GetContext(), "history-retention-cleanup",
		5*time.Second, 300*time.Second,
		func(ctx context.Context) {
			cleanupHistoryRetention(ctx, cfg, db)
		},
	)
	idleSched.Schedule(
		st.GetContext(), "zaplink-host-prune",
		5*time.Second, 300*time.Second,
		func(_ context.Context) {
			pruneExpiredZapLinkHosts(db)
		},
	)
	go watchGameForIndexPause(st.GetContext(), notifBroker, st, st.Notifications, indexPauser)
	go watchGameForScrapePause(st.GetContext(), notifBroker, st, st.Notifications, scrapePauser)

	log.Info().Msg("starting publishers")
	publisherNotifications, _ := notifBroker.Subscribe(100)
	activePublishers, cancelPublisherFanOut, publisherFanOutDone := startPublishers(st, cfg, publisherNotifications)

	// Start media history tracking
	log.Info().Msg("starting media history listener")
	historyTracker := &mediaHistoryTracker{
		st:    st,
		db:    db,
		clock: clockwork.NewRealClock(),
	}
	historyNotifications, _ := notifBroker.Subscribe(100)
	historyListenDone := make(chan struct{})
	go func() {
		defer close(historyListenDone)
		historyTracker.listen(historyNotifications)
	}()
	log.Info().Msg("starting media history PlayTime updater")
	historyUpdateDone := make(chan struct{})
	go func() {
		defer close(historyUpdateDone)
		historyTracker.updatePlayTime(st.GetContext())
	}()

	// Start clock reliability monitor for timestamp healing (MiSTer NTP sync)
	log.Info().Msg("starting clock reliability monitor")
	clockMonitorDone := make(chan struct{})
	go func() {
		defer close(clockMonitorDone)
		monitorClockAndHealTimestamps(st.GetContext(), db, bootUUID)
	}()

	if cfg.GmcProxyEnabled() {
		log.Info().Msg("starting GroovyMiSTer GMC Proxy service")
		backgroundWG.Add(1)
		go func() {
			defer backgroundWG.Done()
			groovyproxy.Start(cfg, st, itq)
		}()
	}

	log.Info().Msg("starting reader manager")
	readerManagerDone := make(chan struct{})
	go func() {
		defer close(readerManagerDone)
		readerManager(svc, itq, make(chan readers.Scan), player, nil)
	}()

	log.Info().Msg("starting input token queue manager")
	processTokenQueueDone := make(chan struct{})
	go func() {
		defer close(processTokenQueueDone)
		processTokenQueue(svc, itq, limitsManager, player)
	}()

	doneCh := make(chan struct{})
	go func() {
		<-st.GetContext().Done()
		log.Info().Msg("service context cancelled, running cleanup")
		indexPauser.Resume()
		scrapePauser.Resume()

		if stopErr := playbackManager.Stop(mediaslot.Primary); stopErr != nil {
			log.Warn().Err(stopErr).Msg("error stopping primary playback during cleanup")
		}
		if stopErr := playbackManager.Stop(mediaslot.Background); stopErr != nil {
			log.Warn().Err(stopErr).Msg("error stopping background playback during cleanup")
		}

		discoveryService.Stop()
		cancelPublisherFanOut()
		<-publisherFanOutDone
		for _, publisher := range activePublishers {
			publisher.Stop()
		}
		if stopErr := pl.Stop(); stopErr != nil {
			log.Warn().Msgf("error stopping platform: %s", stopErr)
		}
		if apiErr := <-apiDone; apiErr != nil {
			log.Error().Err(apiErr).Msg("API service stopped with error")
		}
		limitsManager.Stop()
		notifBroker.Stop()
		<-historyListenDone
		<-historyUpdateDone
		<-clockMonitorDone
		<-processTokenQueueDone
		<-readerManagerDone
		backgroundWG.Wait()
		// Wait for idle-scheduled tasks (history retention, zaplink prune,
		// etc.) before closing the database so an in-flight task can't
		// race with closeDatabase.
		idleSched.Wait()
		close(plq)
		close(lsq)
		close(itq)
		close(cfq)
		closeDatabase(db)

		log.Info().Msg("service cleanup completed")
		close(doneCh)
	}()

	log.Info().Msg("running platform post start")
	err = pl.StartPost(st.GetContext(), cfg, st.LauncherManager(), st.ActiveMedia, st.SetActiveMedia, db, idleSched)
	if err != nil {
		log.Error().Err(err).Msg("platform post start error")
		st.StopService()
		<-doneCh
		return nil, fmt.Errorf("platform start post failed: %w", err)
	}
	log.Info().Msg("platform post start completed, service fully initialized")

	if cfg.ServiceOnBoot() != "" || cfg.ServiceOnReady() != "" {
		backgroundWG.Add(1)
		go func() {
			defer backgroundWG.Done()
			runConfiguredServiceHooks(svc)
		}()
	}

	return &StartResult{
		Stop: func() error {
			st.StopService()
			<-doneCh
			return nil
		},
		Done:             doneCh,
		RestartRequested: st.RestartRequested,
	}, nil
}
