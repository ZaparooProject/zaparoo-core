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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

type AutoDetector struct {
	lastLogTime          time.Time
	connected            map[string]bool
	failed               map[string]bool
	lastDetectionSummary string
	mu                   syncutil.RWMutex
}

func NewAutoDetector(_ *config.Instance) *AutoDetector {
	return &AutoDetector{
		connected: make(map[string]bool),
		failed:    make(map[string]bool),
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
	var detectionErrors []string

	for _, reader := range supportedReaders {
		metadata := reader.Metadata()
		driver := config.DriverInfo{
			ID:                metadata.ID,
			DefaultEnabled:    metadata.DefaultEnabled,
			DefaultAutoDetect: metadata.DefaultAutoDetect,
		}

		// Check if driver is enabled (explicit config or default)
		if !cfg.IsDriverEnabledForAutoDetect(driver) {
			continue
		}

		if !cfg.IsDriverAutoDetectEnabled(metadata.ID, metadata.DefaultAutoDetect) {
			continue
		}

		failedPaths := ad.getFailedPaths()

		// Build exclude list from connected reader paths and failed paths.
		excludeList := make([]string, 0, len(connectedReaders)+len(failedPaths))
		for _, r := range connectedReaders {
			if r != nil {
				excludeList = append(excludeList, r.Path())
			}
		}
		excludeList = append(excludeList, failedPaths...)
		detect := reader.Detect(excludeList)
		if detect == "" {
			continue
		}

		parts := strings.SplitN(detect, ":", 2)
		if len(parts) != 2 {
			log.Error().Msgf("invalid auto-detect string: %s", detect)
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

	ad.logDetectionResults(detectedDevices, detectionErrors)

	return nil
}

// logDetectionResults provides intelligent logging that only logs when detection state changes
// or when a heartbeat is needed to show auto-detect is still active
func (ad *AutoDetector) logDetectionResults(detectedDevices, _ []string) {
	// Create a summary of the current detection state (only track what's relevant for changes)
	summary := fmt.Sprintf("new_detected:%d total_failed:%d",
		len(detectedDevices), len(ad.failed))

	// Check if we should log (state changed or heartbeat timeout)
	const heartbeatInterval = 30 * time.Second
	stateChanged := summary != ad.lastDetectionSummary
	heartbeatTime := ad.lastLogTime.IsZero() || time.Since(ad.lastLogTime) > heartbeatInterval

	if stateChanged || heartbeatTime {
		if len(detectedDevices) > 0 {
			log.Debug().
				Strs("new_devices_detected", detectedDevices).
				Msg("auto-detect found new devices available for connection")
		} else if heartbeatTime {
			if len(ad.failed) > 0 {
				log.Trace().
					Int("total_failed_attempts", len(ad.failed)).
					Msg("auto-detect active: no new devices found")
			} else {
				log.Trace().Msg("auto-detect active: no devices detected")
			}
		}

		ad.lastDetectionSummary = summary
		ad.lastLogTime = time.Now()
	}
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

	err := reader.Open(device, iq)
	if err != nil {
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
		if r != nil {
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
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.connected[path] = true
}

func (ad *AutoDetector) ClearPath(path string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	delete(ad.connected, path)
}

func (ad *AutoDetector) setFailed(path string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.failed[path] = true
}

func (ad *AutoDetector) getFailedPaths() []string {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	paths := make([]string, 0, len(ad.failed))
	for path := range ad.failed {
		paths = append(paths, path)
	}
	return paths
}

func (ad *AutoDetector) ClearFailedPath(path string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	delete(ad.failed, path)
}
