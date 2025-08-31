// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"sync"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/rs/zerolog/log"
)

type AutoDetector struct {
	connected map[string]bool
	failed    map[string]bool // tracks failed connection attempts by connection string
	mu        sync.RWMutex
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
	ad.updateConnected(connectedReaders)

	for _, reader := range supportedReaders {
		metadata := reader.Metadata()

		// Check if auto-detect is enabled for this driver
		if !cfg.IsDriverAutoDetectEnabled(metadata.ID, metadata.DefaultAutoDetect) {
			continue
		}

		// Get failed connections specific to this reader type
		readerFailedConnections := ad.getFailedConnectionsForReader(reader.IDs())

		// Combine connected (all readers) and failed (this reader type only)
		connectedReaders = append(connectedReaders, readerFailedConnections...)
		detect := reader.Detect(connectedReaders)
		if detect == "" {
			continue
		}

		parts := strings.SplitN(detect, ":", 2)
		if len(parts) != 2 {
			log.Error().Msgf("invalid auto-detect string: %s", detect)
			continue
		}

		devicePath := parts[1]
		driverType := parts[0]

		if ad.isConnected(devicePath) {
			if closeErr := reader.Close(); closeErr != nil {
				log.Debug().Err(closeErr).Msg("error closing unused reader")
			}
			continue
		}

		if err := ad.connectReader(reader, driverType, devicePath, detect, st, iq); err != nil {
			log.Error().
				Str("device", detect).
				Err(err).
				Msg("failed to connect detected reader")

			// Mark this connection string as failed
			ad.setFailed(detect)
		}
	}

	return nil
}

func (ad *AutoDetector) connectReader(
	reader readers.Reader,
	driverType, devicePath, connectionString string,
	st *state.State,
	iq chan<- readers.Scan,
) error {
	device := config.ReadersConnect{
		Driver: driverType,
		Path:   devicePath,
	}

	err := reader.Open(device, iq)
	if err != nil {
		return fmt.Errorf("error opening detected reader %s: %w", connectionString, err)
	}

	if reader.Connected() {
		st.SetReader(connectionString, reader)
		ad.setConnected(devicePath)
		// Clear any previous failed attempts for this connection
		ad.ClearFailedConnection(connectionString)
		log.Info().Msgf("successfully connected auto-detected reader: %s", connectionString)
		return nil
	}

	if closeErr := reader.Close(); closeErr != nil {
		log.Debug().Err(closeErr).Msg("error closing reader after failed connection")
	}

	return fmt.Errorf("reader failed to connect: %s", connectionString)
}

func (ad *AutoDetector) updateConnected(connectedReaders []string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.connected = make(map[string]bool)
	for _, connStr := range connectedReaders {
		parts := strings.SplitN(connStr, ":", 2)
		if len(parts) == 2 {
			ad.connected[parts[1]] = true
		}
	}
}

func (ad *AutoDetector) isConnected(devicePath string) bool {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.connected[devicePath]
}

func (ad *AutoDetector) setConnected(devicePath string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.connected[devicePath] = true
}

func (ad *AutoDetector) ClearDevice(devicePath string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	delete(ad.connected, devicePath)
}

func (ad *AutoDetector) setFailed(connectionString string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.failed[connectionString] = true
}

func (ad *AutoDetector) getFailedConnectionsForReader(readerIDs []string) []string {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	failed := make([]string, 0)
	for connectionString := range ad.failed {
		// Extract the driver type from the connection string
		parts := strings.SplitN(connectionString, ":", 2)
		if len(parts) != 2 {
			continue
		}
		driverType := parts[0]

		// Check if this driver type matches any of the reader's IDs
		for _, readerID := range readerIDs {
			if driverType == readerID || strings.HasPrefix(driverType, readerID+"_") {
				failed = append(failed, connectionString)
				break
			}
		}
	}
	return failed
}

func (ad *AutoDetector) ClearFailedConnection(connectionString string) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	delete(ad.failed, connectionString)
}
