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
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
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

const maxPendingHoldRemovals = 32

type holdTokenKey struct {
	uid      string
	text     string
	pathRoot string
}

type pendingRemovalHook struct {
	cancel     context.CancelFunc
	token      tokens.Token
	generation uint64
}

type removalHookResult struct {
	err        error
	token      tokens.Token
	generation uint64
}

func newHoldTokenKey(token *tokens.Token) holdTokenKey {
	return holdTokenKey{
		uid:      token.UID,
		text:     token.Text,
		pathRoot: token.PathRoot,
	}
}

func recordPendingHoldRemoval(pending map[holdTokenKey]tokens.Token, token *tokens.Token) {
	if len(pending) >= maxPendingHoldRemovals {
		for key := range pending {
			delete(pending, key)
			break
		}
	}
	pending[newHoldTokenKey(token)] = *token
}

func hookHasDelayCommand(scriptText string) bool {
	parser := gozapscript.NewParser(scriptText)
	script, err := parser.ParseScript()
	if err != nil {
		return false
	}
	for _, cmd := range script.Cmds {
		if cmd.Name == gozapscript.ZapScriptCmdDelay {
			return true
		}
	}
	return false
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

				if !cfg.IsReaderEnabled(driver, config.ReaderEnableContextManualConnect) {
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

func cancelTimedExit(exitTimer clockwork.Timer, exitGeneration *atomic.Uint64) bool {
	exitGeneration.Add(1)
	return exitTimer != nil && exitTimer.Stop()
}

func timedExit(
	svc *ServiceContext,
	clock clockwork.Clock,
	exitTimer clockwork.Timer,
	exitGeneration *atomic.Uint64,
	owner *tokens.Token,
) clockwork.Timer {
	if cancelTimedExit(exitTimer, exitGeneration) {
		log.Debug().Msg("cancelling previous exit timer")
	}

	if !svc.Config.HoldModeEnabled() {
		log.Debug().Msg("hold mode not enabled, skipping exit timer")
		return exitTimer
	}

	if owner.Source != tokens.SourceReader {
		log.Debug().Str("source", owner.Source).Msg("skipping exit timer for non-reader source")
		return exitTimer
	}

	// Only hardware readers that report removal can own hold-mode media.
	r, ok := svc.State.GetReader(owner.ReaderID)
	if !ok {
		log.Debug().Str("readerID", owner.ReaderID).Msg("reader not found in state, skipping exit timer")
		return exitTimer
	}
	if !readers.HasCapability(r, readers.CapabilityRemovable) {
		log.Debug().Str("readerID", owner.ReaderID).Msg("reader lacks removable capability, skipping exit timer")
		return exitTimer
	}

	ownerCopy := *owner
	timerLen := time.Duration(float64(svc.Config.ReadersScan().ExitDelay) * float64(time.Second))
	log.Debug().Msgf("exit timer set to: %s seconds", timerLen)
	exitTimer = clock.NewTimer(timerLen)
	timer := exitTimer
	generation := exitGeneration.Add(1)

	go func() {
		select {
		case <-timer.Chan():
		case <-svc.State.GetContext().Done():
			return
		}

		if exitGeneration.Load() != generation {
			log.Debug().Msg("stale exit timer expired, cancelling exit")
			return
		}
		if !svc.Config.HoldModeEnabled() {
			log.Debug().Msg("exit timer expired, but hold mode disabled")
			return
		}

		softwareToken := svc.State.GetSoftwareToken()
		if !helpers.TokensEqual(&ownerCopy, softwareToken) {
			log.Debug().Msg("hold owner changed, cancelling exit")
			return
		}
		activeCard := svc.State.GetActiveCard()
		if helpers.TokensEqual(&ownerCopy, &activeCard) {
			log.Debug().Msg("hold owner reinserted, cancelling exit")
			return
		}

		activeMedia := svc.State.ActiveMedia()
		if activeMedia == nil {
			log.Debug().Msg("no active media, cancelling exit")
			return
		}
		if svc.Config.IsHoldModeIgnoredSystem(activeMedia.SystemID) {
			log.Debug().Msg("active system ignored in config, cancelling exit")
			return
		}

		if exitGeneration.Load() != generation {
			return
		}
		runBeforeExitHook(svc, *activeMedia)

		softwareToken = svc.State.GetSoftwareToken()
		if exitGeneration.Load() != generation || !helpers.TokensEqual(&ownerCopy, softwareToken) {
			log.Debug().Msg("hold owner changed during before_exit, cancelling exit")
			return
		}

		log.Info().Msg("exiting media")
		err := svc.Platform.StopActiveLauncher(platforms.StopForMenu)
		if err != nil {
			log.Warn().Msgf("error killing launcher: %s", err)
		}

		if svc.PlaylistQueue != nil {
			select {
			case svc.PlaylistQueue <- nil:
			case <-svc.State.GetContext().Done():
				return
			}
		}
		select {
		case svc.LaunchSoftwareQueue <- nil:
		case <-svc.State.GetContext().Done():
			return
		}
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
	pendingRemovals := make(map[holdTokenKey]tokens.Token)
	removalHookResults := make(chan removalHookResult, 1)
	var exitTimer clockwork.Timer
	var exitGeneration atomic.Uint64
	var removalHookGeneration uint64
	var activeRemovalHook *pendingRemovalHook

	scheduleHoldRemoval := func(removedToken *tokens.Token) {
		key := newHoldTokenKey(removedToken)
		recordPendingHoldRemoval(pendingRemovals, removedToken)
		softwareToken := svc.State.GetSoftwareToken()
		if !helpers.TokensEqual(removedToken, softwareToken) {
			log.Debug().Msg("removed token does not own active media, skipping exit timer")
			return
		}
		delete(pendingRemovals, key)
		exitTimer = timedExit(svc, clock, exitTimer, &exitGeneration, removedToken)
	}

	var stagedToken *tokens.Token
	var guardUI *uievents.Handle
	var guardResults <-chan uievents.Result
	var guardDelay <-chan time.Time
	var delayExpired bool

	resetGuardState := func() {
		stagedToken = nil
		guardUI = nil
		guardResults = nil
		guardDelay = nil
		delayExpired = false
	}
	completeGuard := func(outcome models.UIOutcome) error {
		var err error
		if guardUI != nil {
			err = guardUI.Complete(outcome)
		}
		resetGuardState()
		if err != nil {
			return fmt.Errorf("complete launch guard UI event: %w", err)
		}
		return nil
	}
	cancelGuard := func() {
		if err := completeGuard(models.UIOutcomeCancelled); err != nil {
			log.Debug().Err(err).Msg("launch guard: cancellation lost resolution race")
		}
	}
	applyGuardResult := func(result uievents.Result) error {
		staged := stagedToken
		resetGuardState()
		if result.Resolution.Outcome != models.UIOutcomeConfirmed {
			log.Info().Str("outcome", string(result.Resolution.Outcome)).
				Msg("launch guard: staged token resolved without launch")
			return nil
		}
		if staged == nil || svc.State.ActiveMedia() == nil {
			log.Info().Msg("launch guard: ignoring confirmation after media stopped")
			return nil
		}
		confirmed := *staged
		svc.State.SetActiveCard(confirmed)
		select {
		case itq <- confirmed:
			return nil
		case <-svc.State.GetContext().Done():
			return svc.State.GetContext().Err()
		}
	}

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
	if svc.BackgroundWG != nil {
		svc.BackgroundWG.Add(1)
	}
	go func() {
		if svc.BackgroundWG != nil {
			defer svc.BackgroundWG.Done()
		}
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
		var scanProperties []readers.ScanProperty
		var reinsertedDuringRemovalHook bool

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
			scanProperties = t.Properties
		case hookResult := <-removalHookResults:
			if activeRemovalHook == nil || hookResult.generation != activeRemovalHook.generation {
				continue preprocessing
			}
			activeRemovalHook.cancel()
			activeRemovalHook = nil
			if hookResult.err != nil {
				if errors.Is(hookResult.err, context.Canceled) {
					log.Debug().Msg("on_remove hook cancelled")
				} else {
					log.Warn().Err(hookResult.err).Msg("on_remove hook blocked exit, media will keep running")
				}
				continue preprocessing
			}
			scheduleHoldRemoval(&hookResult.token)
			continue preprocessing
		case stoken := <-svc.LaunchSoftwareQueue:
			// A token has launched primary software and now owns hold-mode exit.
			log.Debug().Msgf("new software token: %v", stoken)
			if activeRemovalHook != nil && !helpers.TokensEqual(stoken, &activeRemovalHook.token) {
				activeRemovalHook.cancel()
				activeRemovalHook = nil
				removalHookGeneration++
				log.Debug().Msg("software token changed, cancelling delayed on_remove hook")
			}
			currentSoftwareToken := svc.State.GetSoftwareToken()
			if stoken == nil || !helpers.TokensEqual(stoken, currentSoftwareToken) {
				if cancelTimedExit(exitTimer, &exitGeneration) {
					log.Info().Msg("software token changed, cancelling exit")
				}
			}
			svc.State.SetSoftwareToken(stoken)
			if stoken != nil {
				key := newHoldTokenKey(stoken)
				if removedToken, ok := pendingRemovals[key]; ok && helpers.TokensEqual(stoken, &removedToken) {
					delete(pendingRemovals, key)
					exitTimer = timedExit(svc, clock, exitTimer, &exitGeneration, stoken)
				}
			}
			continue preprocessing
		case result := <-svc.ConfirmQueue:
			// Legacy API confirm remains launch-guard-specific and bypasses delay.
			if stagedToken == nil || svc.State.ActiveMedia() == nil {
				if stagedToken != nil {
					cancelGuard()
				}
				result <- ErrNoStagedToken
				continue preprocessing
			}
			log.Info().Msgf("launch guard: API confirmed staged token: %v", stagedToken)
			confirmed := *stagedToken
			if err := completeGuard(models.UIOutcomeConfirmed); err != nil {
				result <- ErrNoStagedToken
				continue preprocessing
			}
			svc.State.SetActiveCard(confirmed)
			select {
			case itq <- confirmed:
			case <-svc.State.GetContext().Done():
				result <- svc.State.GetContext().Err()
				break preprocessing
			}
			result <- nil
			continue preprocessing
		case uiResult, ok := <-guardResults:
			if !ok {
				guardResults = nil
				continue preprocessing
			}
			if err := applyGuardResult(uiResult); err != nil {
				break preprocessing
			}
			continue preprocessing
		case <-svc.LaunchGuardCancel:
			if stagedToken != nil {
				log.Info().Msg("launch guard: media stopped, cancelling staged token")
				cancelGuard()
			}
			continue preprocessing
		case <-guardDelay:
			// Delay period expired — token is now ready for re-tap confirmation.
			if stagedToken == nil {
				guardDelay = nil
				continue preprocessing
			}
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
		}

		if scan != nil && activeRemovalHook != nil &&
			helpers.TokensEqual(scan, &activeRemovalHook.token) {
			activeRemovalHook.cancel()
			activeRemovalHook = nil
			removalHookGeneration++
			reinsertedDuringRemovalHook = true
			log.Info().Msg("removed token reinserted, cancelling delayed on_remove hook")
		}

		// If a scan races a terminal UI result, process resolution first. This
		// prevents a re-tap from confirming a token whose Core timeout won.
		if stagedToken != nil && guardResults != nil {
			select {
			case uiResult, ok := <-guardResults:
				if !ok {
					guardResults = nil
					continue preprocessing
				}
				if err := applyGuardResult(uiResult); err != nil {
					break preprocessing
				}
				continue preprocessing
			default:
			}
		}

		// Clear stale staged token if media stopped before cancellation arrived.
		if stagedToken != nil && svc.State.ActiveMedia() == nil {
			log.Info().Msg("launch guard: media stopped, clearing stale staged token")
			cancelGuard()
		}

		// Launch guard confirmation: check BEFORE the preprocessor so that
		// a re-scan of the staged token is not eaten as a duplicate. This is
		// needed for barcode scanners which don't send removal events between
		// scans — the preprocessor would see the re-scan as a duplicate.
		if scan != nil && !reinsertedDuringRemovalHook && stagedToken != nil &&
			svc.Config.LaunchGuardEnabled() && !svc.Config.LaunchGuardRequireConfirm() {
			if helpers.TokensEqual(scan, stagedToken) && svc.State.ActiveMedia() != nil {
				if !delayExpired {
					// Re-tap during delay period — reset both timers as punishment.
					log.Info().Msg("launch guard: re-tap during delay, resetting timers")
					timeout := time.Duration(svc.Config.LaunchGuardTimeout() * float32(time.Second))
					delay := svc.Config.LaunchGuardDelay()
					if guardUI != nil {
						if err := guardUI.Update(uievents.Update{Timeout: &timeout}); err != nil {
							log.Warn().Err(err).Msg("launch guard: failed to reset UI expiry")
						}
					}
					if delay > 0 {
						guardDelay = clock.After(time.Duration(delay * float32(time.Second)))
					}
					proc.Process(scan, readerError)
					continue preprocessing
				}
				log.Info().Msg("launch guard: re-tap confirmed, launching staged token")
				confirmed := *stagedToken
				if err := completeGuard(models.UIOutcomeConfirmed); err != nil {
					log.Info().Err(err).Msg("launch guard: re-tap lost resolution race")
					proc.Process(scan, readerError)
					continue preprocessing
				}
				// Let the preprocessor know what's on the reader now.
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

		var removedToken *tokens.Token
		if scan == nil && !readerError {
			if previous := proc.PrevToken(); previous != nil {
				previousCopy := *previous
				removedToken = &previousCopy
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
			delete(pendingRemovals, newHoldTokenKey(scan))

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

			if reinsertedDuringRemovalHook {
				continue preprocessing
			}

			if exitTimer != nil && helpers.TokensEqual(scan, svc.State.GetSoftwareToken()) {
				if cancelTimedExit(exitTimer, &exitGeneration) {
					log.Info().Msg("hold owner reinserted, cancelling exit")
				}
				continue preprocessing
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

			if handlePendingWrite(svc, scan, player) {
				continue preprocessing
			}

			resolveTokenProperties(svc.State.GetContext(), svc, scan, scanProperties)

			// Launch guard: when enabled and media is playing, stage tokens that
			// would disrupt the current media (launches, playlist changes, stop).
			// Utility commands (coin, keyboard, execute, etc.) pass through.
			if !reinsertedDuringRemovalHook && svc.Config.LaunchGuardEnabled() && svc.State.ActiveMedia() != nil {
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

					message := scan.Text
					if message == "" {
						message = scan.UID
					}
					if svc.UI == nil {
						log.Error().Msg("launch guard: UI event service unavailable")
					} else {
						timeout := time.Duration(svc.Config.LaunchGuardTimeout() * float32(time.Second))
						handle, openErr := svc.UI.Open(svc.State.GetContext(), &uievents.Request{
							Kind:             models.UIEventKindConfirm,
							Title:            "Change game?",
							Message:          message,
							Timeout:          timeout,
							Dismissible:      true,
							SkipHostRenderer: true,
						})
						if openErr != nil {
							log.Error().Err(openErr).Msg("launch guard: failed to open UI event")
						} else {
							guardUI = handle
							guardResults = handle.Results
						}
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
			if activeRemovalHook != nil {
				activeRemovalHook.cancel()
				activeRemovalHook = nil
				removalHookGeneration++
				log.Debug().Msg("cancelled delayed on_remove hook due to reader error")
			}
			if cancelTimedExit(exitTimer, &exitGeneration) {
				log.Debug().Msg("cancelled exit timer due to reader error")
			}
			svc.State.SetActiveCard(tokens.Token{})

		case scanNormalRemoval:
			log.Info().Msg("token was removed")

			// Clear ActiveCard before hook to prevent blocked removals from affecting new scans.
			svc.State.SetActiveCard(tokens.Token{})

			// Run on_remove hook for every normal removal. Delayed hooks run outside
			// the reader loop so reinserting the removed token can cancel them.
			onRemoveScript := svc.Config.ReadersScan().OnRemove
			if svc.Config.HoldModeEnabled() && onRemoveScript != "" {
				if removedToken != nil && hookHasDelayCommand(onRemoveScript) {
					if activeRemovalHook != nil {
						activeRemovalHook.cancel()
					}
					removalHookGeneration++
					// Cancellation is retained by activeRemovalHook; goroutine also guarantees cleanup.
					//nolint:gosec // G118 cannot track stored cancel funcs.
					hookCtx, hookCancel := context.WithCancel(svc.State.GetContext())
					tokenCopy := *removedToken
					generation := removalHookGeneration
					activeRemovalHook = &pendingRemovalHook{
						cancel:     hookCancel,
						token:      tokenCopy,
						generation: generation,
					}
					go func() {
						defer hookCancel()
						err := runHookWithContext(hookCtx, svc, "on_remove", onRemoveScript, nil, nil)
						select {
						case removalHookResults <- removalHookResult{
							err:        err,
							token:      tokenCopy,
							generation: generation,
						}:
						case <-svc.State.GetContext().Done():
						}
					}()
					continue preprocessing
				}
				if err := runHook(svc, "on_remove", onRemoveScript, nil, nil); err != nil {
					log.Warn().Err(err).Msg("on_remove hook blocked exit, media will keep running")
					continue preprocessing
				}
			}

			if removedToken != nil {
				scheduleHoldRemoval(removedToken)
			}
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
