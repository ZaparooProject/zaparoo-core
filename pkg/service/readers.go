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

package service

import (
	"errors"
	"fmt"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// ErrNoStagedToken is returned when a confirm request is received but no
// token is currently staged by the launch guard.
var ErrNoStagedToken = errors.New("no staged token to confirm")

type toConnectDevice struct {
	connectionString string
	device           config.ReadersConnect
}

// isPathConnected checks if any connected reader is using the given path.
func isPathConnected(rs []readers.Reader, path string) bool {
	for _, r := range rs {
		if r != nil && r.Path() == path {
			return true
		}
	}
	return false
}

func connectReaders(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	iq chan<- readers.Scan,
	autoDetector *AutoDetector,
) error {
	rs := st.ListReaders()
	var toConnect []toConnectDevice
	toConnectStrs := func() []string {
		tc := make([]string, 0, len(toConnect))
		for _, device := range toConnect {
			tc = append(tc, device.connectionString)
		}
		return tc
	}

	for _, device := range cfg.Readers().Connect {
		if !device.IsEnabled() {
			log.Debug().Msgf("config device disabled, skipping: %s", device.ConnectionString())
			continue
		}
		if !isPathConnected(rs, device.Path) &&
			!helpers.Contains(toConnectStrs(), device.ConnectionString()) {
			log.Debug().Msgf("config device not connected, adding: %s", device.ConnectionString())
			toConnect = append(toConnect, toConnectDevice{
				connectionString: device.ConnectionString(),
				device:           device,
			})
		}
	}

	// Detect duplicate device paths in config
	pathSeen := make(map[string]string) // path -> connection string
	validToConnect := make([]toConnectDevice, 0, len(toConnect))

	for _, device := range toConnect {
		if firstConn, exists := pathSeen[device.device.Path]; exists {
			log.Warn().Msgf(
				"device path %s configured for multiple readers (%s and %s) - ignoring %s",
				device.device.Path, firstConn, device.connectionString, device.connectionString,
			)
			continue
		}
		pathSeen[device.device.Path] = device.connectionString
		validToConnect = append(validToConnect, device)
	}

	// user defined readers
	for _, device := range validToConnect {
		if !isPathConnected(st.ListReaders(), device.device.Path) {
			rt := readers.NormalizeDriverID(device.device.Driver)
			// SupportedReaders creates new instances; close every reader we don't keep.
			connected := false
			for _, r := range pl.SupportedReaders(cfg) {
				if connected {
					if closeErr := r.Close(); closeErr != nil {
						log.Debug().Err(closeErr).Msg("error closing unused reader")
					}
					continue
				}

				metadata := r.Metadata()
				driver := config.DriverInfo{
					ID:                metadata.ID,
					DefaultEnabled:    metadata.DefaultEnabled,
					DefaultAutoDetect: metadata.DefaultAutoDetect,
				}

				// For user-defined connect entries, driver is implicitly enabled
				// unless explicitly disabled in config.
				if !cfg.IsDriverEnabledForConnect(driver) {
					if closeErr := r.Close(); closeErr != nil {
						log.Debug().Err(closeErr).Msg("error closing unused reader")
					}
					continue
				}

				// Normalize IDs for comparison
				ids := r.IDs()
				normalizedIDs := make([]string, len(ids))
				for i, id := range ids {
					normalizedIDs[i] = readers.NormalizeDriverID(id)
				}
				if !helpers.Contains(normalizedIDs, rt) {
					if closeErr := r.Close(); closeErr != nil {
						log.Debug().Err(closeErr).Msg("error closing unused reader")
					}
					continue
				}

				log.Debug().Msgf("connecting to reader: %s", device.connectionString)
				err := r.Open(device.device, iq, readers.OpenOpts{})
				if err != nil {
					log.Warn().Msgf("error opening reader: %s", err)
					if closeErr := r.Close(); closeErr != nil {
						log.Debug().Err(closeErr).Msg("error closing reader after failed open")
					}
					continue
				}
				st.SetReader(r)
				log.Info().Msgf("opened reader: %s", device.connectionString)
				connected = true
			}
		}
	}

	// auto-detect readers
	if cfg.AutoDetect() && autoDetector != nil {
		if err := autoDetector.DetectReaders(pl, cfg, st, iq); err != nil {
			return fmt.Errorf("auto-detect failed: %w", err)
		}
	}

	return nil
}

func runBeforeExitHook(
	svc *ServiceContext,
	activeMedia models.ActiveMedia, //nolint:gocritic // single-use parameter in service function
) {
	var systemIDs []string
	launchers := svc.Platform.Launchers(svc.Config)
	for i := range launchers {
		l := &launchers[i]
		if l.ID == activeMedia.SystemID {
			systemIDs = append(systemIDs, l.SystemID)
			system, err := systemdefs.LookupSystem(l.SystemID)
			if err == nil {
				systemIDs = append(systemIDs, system.Aliases...)
			}
			break
		}
	}

	if len(systemIDs) > 0 {
		for _, systemID := range systemIDs {
			defaults, ok := svc.Config.LookupSystemDefaults(systemID)
			if !ok || defaults.BeforeExit == "" {
				continue
			}

			if err := runHook(svc, "before_exit", defaults.BeforeExit, nil, nil); err != nil {
				log.Error().Err(err).Msg("error running before_exit script")
			}

			break
		}
	}
}

func timedExit(
	svc *ServiceContext,
	clock clockwork.Clock,
	exitTimer clockwork.Timer,
) clockwork.Timer {
	if exitTimer != nil {
		stopped := exitTimer.Stop()
		if stopped {
			log.Debug().Msg("cancelling previous exit timer")
		}
	}

	if !svc.Config.HoldModeEnabled() {
		log.Debug().Msg("hold mode not enabled, skipping exit timer")
		return exitTimer
	}

	// Only hardware readers support hold mode exit
	lastToken := svc.State.GetLastScanned()
	if lastToken.Source != tokens.SourceReader {
		log.Debug().Str("source", lastToken.Source).Msg("skipping exit timer for non-reader source")
		return exitTimer
	}

	// Check if the reader supports removal detection
	r, ok := svc.State.GetReader(lastToken.ReaderID)
	if !ok {
		log.Debug().Str("readerID", lastToken.ReaderID).Msg("reader not found in state, skipping exit timer")
		return exitTimer
	}
	if !readers.HasCapability(r, readers.CapabilityRemovable) {
		log.Debug().Str("readerID", lastToken.ReaderID).Msg("reader lacks removable capability, skipping exit timer")
		return exitTimer
	}

	timerLen := time.Duration(float64(svc.Config.ReadersScan().ExitDelay) * float64(time.Second))
	log.Debug().Msgf("exit timer set to: %s seconds", timerLen)
	exitTimer = clock.NewTimer(timerLen)

	go func() {
		select {
		case <-exitTimer.Chan():
		case <-svc.State.GetContext().Done():
			return
		}

		if !svc.Config.HoldModeEnabled() {
			log.Debug().Msg("exit timer expired, but hold mode disabled")
			return
		}

		activeMedia := svc.State.ActiveMedia()
		if activeMedia == nil {
			log.Debug().Msg("no active media, cancelling exit")
			return
		}

		if svc.State.GetSoftwareToken() == nil {
			log.Debug().Msg("no active software token, cancelling exit")
			return
		}

		if svc.Config.IsHoldModeIgnoredSystem(activeMedia.SystemID) {
			log.Debug().Msg("active system ignored in config, cancelling exit")
			return
		}

		runBeforeExitHook(svc, *activeMedia)

		log.Info().Msg("exiting media")
		err := svc.Platform.StopActiveLauncher(platforms.StopForMenu)
		if err != nil {
			log.Warn().Msgf("error killing launcher: %s", err)
		}

		svc.LaunchSoftwareQueue <- nil
	}()

	return exitTimer
}

// readerManager is the main service loop to manage active reader hardware
// connections and dispatch token scans from those readers to the token
// input queue.
//
// When a user scans or removes a token with a reader, the reader instance
// forwards it to the "scan queue" which is consumed by this manager.
// The manager will then, if necessary, dispatch the token object to the
// "token input queue" where it may be run.
// This manager also handles the logic of what to do when a token is removed
// from the reader.
func readerManager(
	svc *ServiceContext,
	itq chan<- tokens.Token,
	scanQueue chan readers.Scan,
	player audio.Player,
	clock clockwork.Clock,
) {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}

	var lastError time.Time

	proc := &scanPreprocessor{}
	connectScanSeen := make(map[string]bool)
	var exitTimer clockwork.Timer

	var stagedToken *tokens.Token
	var guardTimeout <-chan time.Time
	var guardDelay <-chan time.Time
	var delayExpired bool

	var autoDetector *AutoDetector
	if svc.Config.AutoDetect() {
		autoDetector = NewAutoDetector(svc.Config)
	}

	readerTicker := time.NewTicker(1 * time.Second)

	playFail := func() {
		if time.Since(lastError) > 1*time.Second {
			path, enabled := svc.Config.FailSoundPath(helpers.DataDir(svc.Platform))
			helpers.PlayConfiguredSound(player, path, enabled, assets.FailSound, "fail")
		}
	}

	// manage reader connections
	go func() {
		log.Info().Msgf("reader manager started, auto-detect=%v", svc.Config.AutoDetect())
		sleepMonitor := helpers.NewSleepWakeMonitor(5 * time.Second)
		readerConnectAttempts := 0
		lastReaderCount := 0
		for {
			select {
			case <-svc.State.GetContext().Done():
				log.Info().Msg("reader manager shutting down via context cancellation")
				return
			case <-readerTicker.C:
				// Check for wake from sleep and reconnect all readers if detected
				if sleepMonitor.Check() {
					log.Info().Msg("detected wake from sleep, reconnecting all readers")
					for _, r := range svc.State.ListReaders() {
						if r != nil {
							svc.State.RemoveReader(r.ReaderID())
						}
					}
					lastReaderCount = 0
				}

				readerConnectAttempts++
				rs := svc.State.ListReaders()

				if len(rs) != lastReaderCount {
					if len(rs) == 0 {
						log.Info().Msg("all readers disconnected")
					} else {
						log.Info().Msgf("reader count changed: %d connected", len(rs))
					}
					lastReaderCount = len(rs)
				} else if readerConnectAttempts%120 == 1 && len(rs) == 0 {
					// Only log if no readers for 2 minutes
					log.Debug().
						Int("attempts", readerConnectAttempts).
						Bool("autoDetect", svc.Config.AutoDetect()).
						Msg("no readers connected")
				}

				for _, r := range rs {
					if r != nil && !r.Connected() {
						readerID := r.ReaderID()
						log.Info().
							Str("readerID", readerID).
							Str("path", r.Path()).
							Str("info", r.Info()).
							Msg("pruning disconnected reader")
						svc.State.RemoveReader(readerID)
						if autoDetector != nil {
							autoDetector.ClearPath(r.Path())
							autoDetector.ClearFailedPath(r.Path())
						}
					}
				}

				connectErr := connectReaders(svc.Platform, svc.Config, svc.State, scanQueue, autoDetector)
				if connectErr != nil {
					log.Warn().Msgf("error connecting rs: %s", connectErr)
				}
				// Reset monitor after potentially blocking operations to avoid
				// counting USB enumeration/connection time as sleep
				sleepMonitor.Reset()
			}
		}
	}()

	// token pre-processing loop
preprocessing:
	for {
		var scan *tokens.Token
		var readerError bool
		var scanSource string

		select {
		case <-svc.State.GetContext().Done():
			log.Debug().Msg("closing reader manager via context cancellation")
			break preprocessing
		case t := <-scanQueue:
			// a reader has sent a token for pre-processing
			log.Debug().Msgf("pre-processing token: %v", t)
			if t.Error != nil {
				log.Warn().Err(t.Error).Msg("error reading card")
				playFail()
				lastError = time.Now()
				continue preprocessing
			}
			scan = t.Token
			readerError = t.ReaderError
			scanSource = t.Source
		case stoken := <-svc.LaunchSoftwareQueue:
			// a token has been launched that starts software, used for managing exits
			log.Debug().Msgf("new software token: %v", stoken)
			if exitTimer != nil && !helpers.TokensEqual(stoken, svc.State.GetSoftwareToken()) {
				if stopped := exitTimer.Stop(); stopped {
					log.Info().Msg("different software token inserted, cancelling exit")
				}
			}
			svc.State.SetSoftwareToken(stoken)
			continue preprocessing
		case result := <-svc.ConfirmQueue:
			// API confirm request — launch the staged token if one exists.
			// API confirm bypasses any active delay.
			if stagedToken == nil {
				result <- ErrNoStagedToken
				continue preprocessing
			}
			log.Info().Msgf("launch guard: API confirmed staged token: %v", stagedToken)
			guardTimeout = nil
			guardDelay = nil
			delayExpired = false
			confirmed := *stagedToken
			stagedToken = nil
			svc.State.SetActiveCard(confirmed)
			select {
			case itq <- confirmed:
			case <-svc.State.GetContext().Done():
				result <- svc.State.GetContext().Err()
				break preprocessing
			}
			result <- nil
			continue preprocessing
		case <-guardDelay:
			// Delay period expired — token is now ready for re-tap confirmation
			log.Info().Msg("launch guard: delay expired, ready for confirmation")
			delayExpired = true
			guardDelay = nil
			notifications.TokensStagedReady(svc.State.Notifications, models.TokenResponse{
				Type:     stagedToken.Type,
				UID:      stagedToken.UID,
				Text:     stagedToken.Text,
				Data:     stagedToken.Data,
				ScanTime: stagedToken.ScanTime,
			})
			path, enabled := svc.Config.ReadySoundPath(helpers.DataDir(svc.Platform))
			helpers.PlayConfiguredSound(player, path, enabled, assets.ReadySound, "ready")
			continue preprocessing
		case <-guardTimeout:
			// Staged token expired
			log.Info().Msg("launch guard: staged token expired")
			stagedToken = nil
			guardTimeout = nil
			guardDelay = nil
			delayExpired = false
			continue preprocessing
		}

		// Clear stale staged token if media has stopped since staging
		if stagedToken != nil && svc.State.ActiveMedia() == nil {
			log.Info().Msg("launch guard: media stopped, clearing stale staged token")
			stagedToken = nil
			guardTimeout = nil
			guardDelay = nil
			delayExpired = false
		}

		// Launch guard confirmation: check BEFORE the preprocessor so that
		// a re-scan of the staged token is not eaten as a duplicate. This is
		// needed for barcode scanners which don't send removal events between
		// scans — the preprocessor would see the re-scan as a duplicate.
		if scan != nil && stagedToken != nil &&
			svc.Config.LaunchGuardEnabled() && !svc.Config.LaunchGuardRequireConfirm() {
			if helpers.TokensEqual(scan, stagedToken) && svc.State.ActiveMedia() != nil {
				if !delayExpired {
					// Re-tap during delay period — reset both timers as punishment
					log.Info().Msg("launch guard: re-tap during delay, resetting timers")
					timeout := svc.Config.LaunchGuardTimeout()
					delay := svc.Config.LaunchGuardDelay()
					if timeout > 0 {
						guardTimeout = clock.After(time.Duration(timeout * float32(time.Second)))
					}
					if delay > 0 {
						guardDelay = clock.After(time.Duration(delay * float32(time.Second)))
					}
					proc.Process(scan, readerError)
					continue preprocessing
				}
				log.Info().Msg("launch guard: re-tap confirmed, launching staged token")
				guardTimeout = nil
				guardDelay = nil
				delayExpired = false
				confirmed := *stagedToken
				stagedToken = nil
				// Let the preprocessor know what's on the reader now
				proc.Process(scan, readerError)
				svc.State.SetActiveCard(confirmed)
				select {
				case itq <- confirmed:
				case <-svc.State.GetContext().Done():
					break preprocessing
				}
				continue preprocessing
			}
		}

		switch proc.Process(scan, readerError) {
		case scanSkipDuplicate:
			log.Debug().
				Str("source", scanSource).
				Bool("readerError", readerError).
				Msg("ignoring duplicate scan")
			continue preprocessing

		case scanNewToken:
			// Suppress the first scan from each newly-connected reader when ignore_on_connect is enabled
			if svc.Config.ScanIgnoreOnConnect() && scan.ReaderID != "" && !connectScanSeen[scan.ReaderID] {
				connectScanSeen[scan.ReaderID] = true
				log.Info().
					Str("readerID", scan.ReaderID).
					Msg("suppressing initial detection from reader (ignore_on_connect enabled)")
				continue preprocessing
			}
			if svc.Config.ScanIgnoreOnConnect() && scan.ReaderID != "" {
				connectScanSeen[scan.ReaderID] = true
			}

			log.Info().Msgf("new token scanned: %v", scan)

			// Run on_scan hook before SetActiveCard so last_scanned refers to previous token
			if onScanScript := svc.Config.ReadersScan().OnScan; onScanScript != "" {
				scanned := &gozapscript.ExprEnvScanned{
					ID:    scan.UID,
					Value: scan.Text,
					Data:  scan.Data,
				}
				if err := runHook(svc, "on_scan", onScanScript, scanned, nil); err != nil {
					log.Warn().Err(err).Msg("on_scan hook blocked token processing")
					continue preprocessing
				}
			}

			svc.State.SetActiveCard(*scan)

			if exitTimer != nil {
				stopped := exitTimer.Stop()
				stoken := svc.State.GetSoftwareToken()
				if stopped && helpers.TokensEqual(scan, stoken) {
					log.Info().Msg("same token reinserted, cancelling exit")
					continue preprocessing
				} else if stopped {
					log.Info().Msg("new token inserted, restarting exit timer")
					exitTimer = timedExit(svc, clock, exitTimer)
				}
			}

			// avoid launching a token that was just written by a reader
			// NOTE: This check requires both UID and Text to match (see helpers.TokensEqual).
			wt := svc.State.GetWroteToken()
			if wt != nil && helpers.TokensEqual(scan, wt) {
				log.Info().Msg("skipping launching just written token")
				svc.State.SetWroteToken(nil)
				continue preprocessing
			}
			svc.State.SetWroteToken(nil)

			// Launch guard: when enabled and media is playing, stage tokens that
			// would disrupt the current media (launches, playlist changes, stop).
			// Utility commands (coin, keyboard, execute, etc.) pass through.
			if svc.Config.LaunchGuardEnabled() && svc.State.ActiveMedia() != nil {
				mappedValue, hasMapping := getMapping(svc.Config, svc.DB, svc.Platform, *scan)
				scriptText := scan.Text
				if hasMapping {
					scriptText = mappedValue
				}
				parser := gozapscript.NewParser(scriptText)
				script, parseErr := parser.ParseScript()

				// Stage conservatively: if parsing fails we can't confirm the token
				// is a safe utility command, so stage it. Only pass through tokens
				// we can positively identify as non-disrupting.
				if parseErr != nil || scriptHasMediaDisruptingCommand(&script) {
					log.Info().Msgf("launch guard: staging token: %v", scan)
					stagedToken = scan

					notifications.TokensStaged(svc.State.Notifications, models.TokenResponse{
						Type:     scan.Type,
						UID:      scan.UID,
						Text:     scan.Text,
						Data:     scan.Data,
						ScanTime: scan.ScanTime,
					})

					path, enabled := svc.Config.PendingSoundPath(helpers.DataDir(svc.Platform))
					helpers.PlayConfiguredSound(player, path, enabled, assets.PendingSound, "pending")

					if timeout := svc.Config.LaunchGuardTimeout(); timeout > 0 {
						guardTimeout = clock.After(time.Duration(timeout * float32(time.Second)))
					} else {
						guardTimeout = nil
					}

					if delay := svc.Config.LaunchGuardDelay(); delay > 0 {
						guardDelay = clock.After(time.Duration(delay * float32(time.Second)))
						delayExpired = false
					} else {
						guardDelay = nil
						delayExpired = true
					}
					continue preprocessing
				}
			}

			log.Info().Msgf("sending token to queue: %v", scan)
			select {
			case itq <- *scan:
			case <-svc.State.GetContext().Done():
				break preprocessing
			}

		case scanReaderErrorRemoval:
			log.Warn().
				Str("source", scanSource).
				Bool("prevTokenSet", proc.PrevToken() != nil).
				Msg("token removal due to reader error, keeping media running")
			// Clear acknowledged state so reconnection triggers a fresh suppression
			if pt := proc.PrevToken(); pt != nil && pt.ReaderID != "" {
				delete(connectScanSeen, pt.ReaderID)
			}
			if exitTimer != nil {
				if stopped := exitTimer.Stop(); stopped {
					log.Debug().Msg("cancelled exit timer due to reader error")
				}
			}
			svc.State.SetActiveCard(tokens.Token{})

		case scanNormalRemoval:
			log.Info().Msg("token was removed")

			// Clear ActiveCard before hook to prevent blocked removals from affecting new scans
			svc.State.SetActiveCard(tokens.Token{})

			// Run on_remove hook; errors skip exit timer but card state is already cleared
			onRemoveScript := svc.Config.ReadersScan().OnRemove
			if svc.Config.HoldModeEnabled() && onRemoveScript != "" {
				if err := runHook(svc, "on_remove", onRemoveScript, nil, nil); err != nil {
					log.Warn().Err(err).Msg("on_remove hook blocked exit, media will keep running")
					continue preprocessing
				}
			}

			exitTimer = timedExit(svc, clock, exitTimer)
		}
	}

	// daemon shutdown
	rs := svc.State.ListReaders()
	for _, r := range rs {
		if r != nil {
			err := r.Close()
			if err != nil {
				log.Warn().Msg("error closing reader")
			}
		}
	}
}
