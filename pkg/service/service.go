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
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/groovyproxy"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/discovery"
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
		LaunchSoftwareQueue: lsq,
		PlaylistQueue:       plq,
		ConfirmQueue:        cfq,
		BackgroundWG:        backgroundWG,
	}

	// Set up the OnMediaStart hook
	st.SetOnMediaStartHook(func(_ *models.ActiveMedia) {
		if script := cfg.LaunchersOnMediaStart(); script != "" {
			if hookErr := runHook(svc, "on_media_start", script, nil, nil); hookErr != nil {
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

	// Create index pauser to pause media indexing while a game is running.
	indexPauser := syncutil.NewPauser()

	discoveryService := discovery.New(cfg)

	log.Info().Msg("starting API service")
	apiReady := make(chan error, 1)
	apiDone := make(chan error, 1)
	go func() {
		apiDone <- api.StartWithReady(
			pl, cfg, st, itq, cfq, db, limitsManager,
			notifBroker, discoveryService.InstanceName(), player, indexPauser,
			apiReady,
		)
	}()

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

	log.Info().Msg("starting mDNS discovery service")
	if discoveryErr := discoveryService.Start(); discoveryErr != nil {
		log.Warn().Err(discoveryErr).Msg("mDNS discovery initialization failed")
	}

	backgroundWG.Add(1)
	go func() {
		defer backgroundWG.Done()
		runStartupMaintenance(st.GetContext(), cfg, db)
	}()

	backgroundWG.Add(1)
	go func() {
		defer backgroundWG.Done()
		zapscript.PreWarmZapLinkHostsContext(st.GetContext(), db, helpers.WaitForInternetContext)
	}()
	backgroundWG.Add(1)
	go func() {
		defer backgroundWG.Done()
		updater.CheckAndNotify(
			st.GetContext(), cfg, pl.ID(), st.Inbox(),
			helpers.WaitForInternetContext, updater.Check,
			pl.ManagedByPackageManager(),
		)
	}()
	go watchGameForIndexPause(st.GetContext(), notifBroker, st, st.Notifications, indexPauser)

	log.Info().Msg("checking for interrupted media indexing")
	backgroundWG.Add(1)
	go func() {
		defer backgroundWG.Done()
		checkAndResumeIndexing(pl, cfg, db, st, indexPauser)
	}()

	log.Info().Msg("checking for interrupted media optimization")
	backgroundWG.Add(1)
	go func() {
		defer backgroundWG.Done()
		checkAndResumeOptimization(db, st.Notifications, indexPauser)
	}()

	// Build slug search cache after API is listening to avoid blocking startup
	if db.MediaDB != nil {
		db.MediaDB.TrackBackgroundOperation()
		go func() {
			defer db.MediaDB.BackgroundOperationDone()
			if cacheErr := db.MediaDB.RebuildSlugSearchCache(); cacheErr != nil {
				log.Warn().Err(cacheErr).Msg("failed to build slug search cache")
			}
		}()
	}

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

		discoveryService.Stop()
		cancelPublisherFanOut()
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
		close(plq)
		close(lsq)
		close(itq)
		close(cfq)
		closeDatabase(db)

		log.Info().Msg("service cleanup completed")
		close(doneCh)
	}()

	log.Info().Msg("running platform post start")
	err = pl.StartPost(cfg, st.LauncherManager(), st.ActiveMedia, st.SetActiveMedia, db)
	if err != nil {
		log.Error().Err(err).Msg("platform post start error")
		st.StopService()
		<-doneCh
		return nil, fmt.Errorf("platform start post failed: %w", err)
	}
	log.Info().Msg("platform post start completed, service fully initialized")

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
