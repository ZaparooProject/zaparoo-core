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
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/rs/zerolog/log"
)

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
		var tc []string
		for _, device := range toConnect {
			tc = append(tc, device.connectionString)
		}
		return tc
	}

	for _, device := range cfg.Readers().Connect {
		if !isPathConnected(rs, device.Path) &&
			!helpers.Contains(toConnectStrs(), device.ConnectionString()) {
			log.Debug().Msgf("config device not connected, adding: %s", device)
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
			for _, r := range pl.SupportedReaders(cfg) {
				metadata := r.Metadata()
				driver := config.DriverInfo{
					ID:                metadata.ID,
					DefaultEnabled:    metadata.DefaultEnabled,
					DefaultAutoDetect: metadata.DefaultAutoDetect,
				}

				// For user-defined connect entries, driver is implicitly enabled
				// unless explicitly disabled in config.
				if !cfg.IsDriverEnabledForConnect(driver) {
					continue
				}

				// Normalize IDs for comparison
				ids := r.IDs()
				normalizedIDs := make([]string, len(ids))
				for i, id := range ids {
					normalizedIDs[i] = readers.NormalizeDriverID(id)
				}
				if helpers.Contains(normalizedIDs, rt) {
					log.Debug().Msgf("connecting to reader: %s", device)
					err := r.Open(device.device, iq)
					if err != nil {
						log.Warn().Msgf("error opening reader: %s", err)
						continue
					}
					st.SetReader(r)
					log.Info().Msgf("opened reader: %s", device)
					break
				}
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
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	db *database.Database,
	lsq chan *tokens.Token,
	plq chan *playlists.Playlist,
	activeMedia models.ActiveMedia, //nolint:gocritic // single-use parameter in service function
) {
	var systemIDs []string
	launchers := pl.Launchers(cfg)
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
			defaults, ok := cfg.LookupSystemDefaults(systemID)
			if !ok || defaults.BeforeExit == "" {
				continue
			}

			if err := runHook(pl, cfg, st, db, lsq, plq, "before_exit", defaults.BeforeExit, nil); err != nil {
				log.Error().Err(err).Msg("error running before_exit script")
			}

			break
		}
	}
}

func timedExit(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	db *database.Database,
	lsq chan *tokens.Token,
	plq chan *playlists.Playlist,
	exitTimer *time.Timer,
) *time.Timer {
	if exitTimer != nil {
		stopped := exitTimer.Stop()
		if stopped {
			log.Debug().Msg("cancelling previous exit timer")
		}
	}

	if !cfg.HoldModeEnabled() {
		return exitTimer
	}

	// Check if the reader supports removal detection
	lastToken := st.GetLastScanned()
	if lastToken.ReaderID == "" {
		return exitTimer
	}
	r, ok := st.GetReader(lastToken.ReaderID)
	if !ok || !readers.HasCapability(r, readers.CapabilityRemovable) {
		return exitTimer
	}

	timerLen := time.Second * time.Duration(cfg.ReadersScan().ExitDelay)
	log.Debug().Msgf("exit timer set to: %s seconds", timerLen)
	exitTimer = time.NewTimer(timerLen)

	go func() {
		<-exitTimer.C

		if !cfg.HoldModeEnabled() {
			log.Debug().Msg("exit timer expired, but hold mode disabled")
			return
		}

		activeMedia := st.ActiveMedia()
		if activeMedia == nil {
			log.Debug().Msg("no active media, cancelling exit")
			return
		}

		if st.GetSoftwareToken() == nil {
			log.Debug().Msg("no active software token, cancelling exit")
			return
		}

		if cfg.IsHoldModeIgnoredSystem(activeMedia.SystemID) {
			log.Debug().Msg("active system ignored in config, cancelling exit")
			return
		}

		runBeforeExitHook(pl, cfg, st, db, lsq, plq, *activeMedia)

		log.Info().Msg("exiting media")
		err := pl.StopActiveLauncher(platforms.StopForMenu)
		if err != nil {
			log.Warn().Msgf("error killing launcher: %s", err)
		}

		lsq <- nil
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
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	db *database.Database,
	itq chan<- tokens.Token,
	lsq chan *tokens.Token,
	plq chan *playlists.Playlist,
) {
	scanQueue := make(chan readers.Scan)

	var lastError time.Time

	var prevToken *tokens.Token
	var exitTimer *time.Timer

	var autoDetector *AutoDetector
	if cfg.AutoDetect() {
		autoDetector = NewAutoDetector(cfg)
	}

	readerTicker := time.NewTicker(1 * time.Second)

	playFail := func() {
		if time.Since(lastError) > 1*time.Second {
			path, enabled := cfg.FailSoundPath(helpers.DataDir(pl))
			helpers.PlayConfiguredSound(path, enabled, assets.FailSound, "fail")
		}
	}

	// manage reader connections
	go func() {
		log.Info().Msgf("reader manager started, auto-detect=%v", cfg.AutoDetect())
		sleepMonitor := helpers.NewSleepWakeMonitor(5 * time.Second)
		readerConnectAttempts := 0
		lastReaderCount := 0
		for {
			select {
			case <-st.GetContext().Done():
				log.Info().Msg("reader manager shutting down via context cancellation")
				return
			case <-readerTicker.C:
				// Check for wake from sleep and reconnect all readers if detected
				if sleepMonitor.Check() {
					log.Info().Msg("detected wake from sleep, reconnecting all readers")
					for _, r := range st.ListReaders() {
						if r != nil {
							st.RemoveReader(r.ReaderID())
						}
					}
					lastReaderCount = 0
				}

				readerConnectAttempts++
				rs := st.ListReaders()

				if len(rs) != lastReaderCount {
					if len(rs) == 0 {
						log.Info().Msg("all readers disconnected")
					} else {
						log.Info().Msgf("reader count changed: %d connected", len(rs))
					}
					lastReaderCount = len(rs)
				} else if readerConnectAttempts%120 == 1 && len(rs) == 0 {
					// Only log if no readers for 2 minutes
					log.Info().Msgf("no readers connected after %d attempts, auto-detect=%v",
						readerConnectAttempts, cfg.AutoDetect())
				}

				for _, r := range rs {
					if r != nil && !r.Connected() {
						readerID := r.ReaderID()
						log.Debug().Msgf("pruning disconnected reader: %s", readerID)
						st.RemoveReader(readerID)
						if autoDetector != nil {
							autoDetector.ClearPath(r.Path())
							autoDetector.ClearFailedConnection(readerID)
						}
					}
				}

				if connectErr := connectReaders(pl, cfg, st, scanQueue, autoDetector); connectErr != nil {
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

		select {
		case <-st.GetContext().Done():
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
		case stoken := <-lsq:
			// a token has been launched that starts software, used for managing exits
			log.Debug().Msgf("new software token: %v", st)
			if exitTimer != nil && !helpers.TokensEqual(stoken, st.GetSoftwareToken()) {
				if stopped := exitTimer.Stop(); stopped {
					log.Info().Msg("different software token inserted, cancelling exit")
				}
			}
			st.SetSoftwareToken(stoken)
			continue preprocessing
		}

		if helpers.TokensEqual(scan, prevToken) {
			log.Debug().Msg("ignoring duplicate scan")
			continue preprocessing
		}

		prevToken = scan

		if scan != nil {
			log.Info().Msgf("new token scanned: %v", scan)

			// Run on_scan hook before SetActiveCard so last_scanned refers to previous token
			if onScanScript := cfg.ReadersScan().OnScan; onScanScript != "" {
				scannedOpts := &zapscript.ExprEnvOptions{
					Scanned: &parser.ExprEnvScanned{
						ID:    scan.UID,
						Value: scan.Text,
						Data:  scan.Data,
					},
				}
				if err := runHook(pl, cfg, st, db, lsq, plq, "on_scan", onScanScript, scannedOpts); err != nil {
					log.Warn().Err(err).Msg("on_scan hook blocked token processing")
					continue preprocessing
				}
			}

			st.SetActiveCard(*scan)

			if exitTimer != nil {
				stopped := exitTimer.Stop()
				activeToken := st.GetActiveCard()
				if stopped && helpers.TokensEqual(scan, &activeToken) {
					log.Info().Msg("same token reinserted, cancelling exit")
					continue preprocessing
				} else if stopped {
					log.Info().Msg("new token inserted, restarting exit timer")
					exitTimer = timedExit(pl, cfg, st, db, lsq, plq, exitTimer)
				}
			}

			// avoid launching a token that was just written by a reader
			// NOTE: This check requires both UID and Text to match (see helpers.TokensEqual).
			wt := st.GetWroteToken()
			if wt != nil && helpers.TokensEqual(scan, wt) {
				log.Info().Msg("skipping launching just written token")
				st.SetWroteToken(nil)
				continue preprocessing
			}
			st.SetWroteToken(nil)

			log.Info().Msgf("sending token to queue: %v", scan)

			itq <- *scan
		} else {
			log.Info().Msg("token was removed")

			// If removal was due to reader error, skip on_remove and exit to keep media running
			if readerError {
				log.Warn().Msg("token removal due to reader error - skipping on_remove and exit to keep media running")
				st.SetActiveCard(tokens.Token{})
				continue preprocessing
			}

			// Clear ActiveCard before hook to prevent blocked removals from affecting new scans
			st.SetActiveCard(tokens.Token{})

			// Run on_remove hook; errors skip exit timer but card state is already cleared
			if onRemoveScript := cfg.ReadersScan().OnRemove; cfg.HoldModeEnabled() && onRemoveScript != "" {
				if err := runHook(pl, cfg, st, db, lsq, plq, "on_remove", onRemoveScript, nil); err != nil {
					log.Warn().Err(err).Msg("on_remove hook blocked exit, media will keep running")
					continue preprocessing
				}
			}

			exitTimer = timedExit(pl, cfg, st, db, lsq, plq, exitTimer)
		}
	}

	// daemon shutdown
	rs := st.ListReaders()
	for _, r := range rs {
		if r != nil {
			err := r.Close()
			if err != nil {
				log.Warn().Msg("error closing reader")
			}
		}
	}
}
