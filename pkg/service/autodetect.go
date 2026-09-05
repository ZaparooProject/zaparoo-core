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
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// failedPathInitialBackoff and failedPathMaxBackoff bound how long a path
	// that failed to open is kept out of auto-detect.
	failedPathInitialBackoff = 2 * time.Second
	failedPathMaxBackoff     = 60 * time.Second
)

// failedPath suppresses a device path that failed to open, temporarily.
//
// It used to suppress it permanently: nothing cleared an entry for a path that
// had never connected, so one failed open — a port still settling at boot, a
// momentary busy or permission error — ended auto-detect for that device until
// Core was restarted. The suppression only exists to stop a 1 Hz retry storm
// against a device that is not answering, which a growing backoff does just as
// well without the dead end.
type failedPath struct {
	retryAt time.Time
	backoff time.Duration
}

// AutoDetector tracks auto-detected readers by device path.
//
// A path is the identity here, so an empty one is not tracked at all. Some
// drivers return a connection string with no path — libnfc's ACR122 fallback
// returns the bare "libnfcauto:" — and keying either map on "" made one
// pathless driver's state apply to every other pathless driver: one failing to
// open would suppress the rest, and the shared entry surfaced in the detection
// summary as a suppressed path that no device owned.
type AutoDetector struct {
	clock                clockwork.Clock
	lastLogTime          time.Time
	connected            map[string]bool
	failed               map[string]failedPath
	lastDetectionSummary string
	mu                   syncutil.RWMutex
}

func NewAutoDetector(clock clockwork.Clock) *AutoDetector {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &AutoDetector{
		clock:     clock,
		connected: make(map[string]bool),
		failed:    make(map[string]failedPath),
	}
}

func (ad *AutoDetector) DetectReaders(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	iq chan<- readers.Scan,
) error {
	supportedReaders := pl.SupportedReaders(cfg)
	if len(supportedReaders) == 0 {
		return nil
	}

	connectedReaders := st.ListReaders()
	ad.updateConnectedFromReaders(connectedReaders)

	var detectedDevices []string
	var skippedDrivers []string

	for _, reader := range supportedReaders {
		metadata := reader.Metadata()
		driver := config.DriverInfo{
			ID:                metadata.ID,
			DefaultEnabled:    metadata.DefaultEnabled,
			DefaultAutoDetect: metadata.DefaultAutoDetect,
		}

		closeUnused := func() {
			if closeErr := reader.Close(); closeErr != nil {
				log.Debug().Err(closeErr).Msg("error closing unused reader")
			}
		}

		if !cfg.IsReaderEnabled(driver, config.ReaderEnableContextAutoDetect) {
			skippedDrivers = append(skippedDrivers, metadata.ID)
			closeUnused()
			continue
		}

		failedPaths := ad.suppressedPaths()

		// Build exclude list from connected reader paths and failed paths.
		// The exclude list uses "driver:path" format for compatibility with Detect()
		// implementations that parse connection strings.
		// TODO: refactor Detect() interface to accept just paths instead of connection strings.
		excludeList := make([]string, 0, len(connectedReaders)+len(failedPaths))
		for _, r := range connectedReaders {
			if r != nil {
				excludeList = append(excludeList, r.Metadata().ID+":"+r.Path())
			}
		}
		// Failed paths are stored as just paths, but Detect() implementations
		// extract the path portion, so we need to format them as "driver:path".
		// We use the current reader's ID since we're building an exclude list for it.
		for _, path := range failedPaths {
			excludeList = append(excludeList, metadata.ID+":"+path)
		}
		detect := reader.Detect(excludeList)
		if detect == "" {
			closeUnused()
			continue
		}

		parts := strings.SplitN(detect, ":", 2)
		if len(parts) != 2 {
			log.Error().Msgf("invalid auto-detect string: %s", detect)
			closeUnused()
			continue
		}

		// Track detected devices for logging summary (only valid devices with actual paths)
		if parts[1] != "" {
			detectedDevices = append(detectedDevices, detect)
		}

		path := parts[1]
		driverID := parts[0]

		if ad.isConnected(path) {
			if closeErr := reader.Close(); closeErr != nil {
				log.Debug().Err(closeErr).Msg("error closing unused reader")
			}
			continue
		}

		if err := ad.connectReader(reader, driverID, path, detect, st, iq); err != nil {
			log.Trace().
				Str("device", detect).
				Err(err).
				Msg("failed to connect detected reader")

			ad.setFailed(path)
		}
	}

	ad.logDetectionResults(detectedDevices, skippedDrivers)

	return nil
}

// logDetectionResults reports what auto-detect did, once per change rather than
// once per tick.
//
// It logs at info because this is the only account of an auto-detect that found
// nothing, and a user reporting "my reader is not detected" has no reason to
// have turned debug logging on first. The 1 Hz tick makes that affordable only
// because the summary is stable while the hardware is: a run that keeps finding
// the same nothing logs one line and then stays quiet until something changes.
func (ad *AutoDetector) logDetectionResults(detectedDevices, skippedDrivers []string) {
	suppressed := ad.suppressedPaths()

	summary := fmt.Sprintf("detected:%s skipped:%s suppressed:%s",
		strings.Join(detectedDevices, ","),
		strings.Join(skippedDrivers, ","),
		strings.Join(suppressed, ","))

	const heartbeatInterval = 30 * time.Second
	stateChanged := summary != ad.lastDetectionSummary
	heartbeatTime := ad.lastLogTime.IsZero() || ad.clock.Since(ad.lastLogTime) > heartbeatInterval
	if !stateChanged && !heartbeatTime {
		return
	}

	if stateChanged {
		event := log.Info()
		if len(detectedDevices) > 0 {
			event = event.Strs("detected", detectedDevices)
		}
		if len(skippedDrivers) > 0 {
			event = event.Strs("skipped_drivers", skippedDrivers)
		}
		if len(suppressed) > 0 {
			event = event.Strs("suppressed_paths", suppressed)
		}
		event.Msg("reader auto-detect result changed")
	} else {
		log.Trace().
			Int("detected", len(detectedDevices)).
			Int("suppressed", len(suppressed)).
			Msg("reader auto-detect still active")
	}

	ad.lastDetectionSummary = summary
	ad.lastLogTime = ad.clock.Now()
}

func (ad *AutoDetector) connectReader(
	reader readers.Reader,
	driverID, path, connectionString string,
	st *state.State,
	iq chan<- readers.Scan,
) error {
	device := config.ReadersConnect{
		Driver: driverID,
		Path:   path,
	}

	err := reader.Open(device, iq, readers.OpenOpts{Probing: true})
	if err != nil {
		if closeErr := reader.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("error closing reader after failed open")
		}
		return fmt.Errorf("error opening detected reader %s: %w", connectionString, err)
	}

	if reader.Connected() {
		st.SetReader(reader)
		ad.setConnected(path)
		// Clear any previous failed attempts for this path
		ad.ClearFailedPath(path)
		log.Info().Msgf("successfully connected auto-detected reader: %s", reader.ReaderID())
		return nil
	}

	if closeErr := reader.Close(); closeErr != nil {
		log.Debug().Err(closeErr).Msg("error closing reader after failed connection")
	}

	return fmt.Errorf("reader failed to connect: %s", connectionString)
}

func (ad *AutoDetector) updateConnectedFromReaders(connectedReaders []readers.Reader) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.connected = make(map[string]bool)
	for _, r := range connectedReaders {
		if r != nil && r.Path() != "" {
			ad.connected[r.Path()] = true
		}
	}
}

func (ad *AutoDetector) isConnected(path string) bool {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.connected[path]
}

func (ad *AutoDetector) setConnected(path string) {
	if path == "" {
		return
	}
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.connected[path] = true
}

func (ad *AutoDetector) ClearPath(path string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	delete(ad.connected, path)
}

// setFailed backs a path off after a failed open, doubling the wait each time
// up to failedPathMaxBackoff. The entry is kept once the wait expires so a path
// that keeps failing keeps escalating instead of restarting at the short wait.
func (ad *AutoDetector) setFailed(path string) {
	if path == "" {
		return
	}
	ad.mu.Lock()
	defer ad.mu.Unlock()

	entry := ad.failed[path]
	if entry.backoff == 0 {
		entry.backoff = failedPathInitialBackoff
	} else {
		entry.backoff = min(entry.backoff*2, failedPathMaxBackoff)
	}
	entry.retryAt = ad.clock.Now().Add(entry.backoff)
	ad.failed[path] = entry
}

// suppressedPaths returns the paths still inside their failure backoff, sorted
// so the detection summary a caller builds from them does not flap on map
// iteration order.
func (ad *AutoDetector) suppressedPaths() []string {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	now := ad.clock.Now()
	paths := make([]string, 0, len(ad.failed))
	for path, entry := range ad.failed {
		if now.Before(entry.retryAt) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

func (ad *AutoDetector) ClearFailedPath(path string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	delete(ad.failed, path)
}
