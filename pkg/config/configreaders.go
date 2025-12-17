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

package config

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

type Readers struct {
	Drivers     map[string]DriverConfig `toml:"drivers,omitempty"`
	ScanHistory *int                    `toml:"scan_history,omitempty"`
	Connect     []ReadersConnect        `toml:"connect,omitempty"`
	Scan        ReadersScan             `toml:"scan,omitempty"`
	AutoDetect  bool                    `toml:"auto_detect"`
}

type DriverConfig struct {
	Enabled    *bool `toml:"enabled,omitempty"`
	AutoDetect *bool `toml:"auto_detect,omitempty"`
}

type ReadersScan struct {
	Mode         string   `toml:"mode"`
	OnScan       string   `toml:"on_scan,omitempty"`
	OnRemove     string   `toml:"on_remove,omitempty"`
	IgnoreSystem []string `toml:"ignore_system,omitempty"`
	ExitDelay    float32  `toml:"exit_delay,omitempty"`
}

type ReadersConnect struct {
	Driver   string `toml:"driver"`
	Path     string `toml:"path,omitempty"`
	IDSource string `toml:"id_source,omitempty"`
}

func (r ReadersConnect) ConnectionString() string {
	// Normalize driver ID by removing underscores
	normalizedDriver := strings.ReplaceAll(r.Driver, "_", "")
	return fmt.Sprintf("%s:%s", normalizedDriver, r.Path)
}

// normalizeDriverID removes underscores from driver IDs for backwards compatibility.
func normalizeDriverID(id string) string {
	return strings.ReplaceAll(id, "_", "")
}

func (c *Instance) ReadersScan() ReadersScan {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan
}

func (c *Instance) IsHoldModeIgnoredSystem(systemID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, ignoredSystem := range c.vals.Readers.Scan.IgnoreSystem {
		configSystem, err := systemdefs.LookupSystem(ignoredSystem)
		if err != nil {
			continue
		}
		if strings.EqualFold(configSystem.ID, systemID) {
			return true
		}
	}
	return false
}

func (c *Instance) TapModeEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	switch c.vals.Readers.Scan.Mode {
	case ScanModeTap, "":
		return true
	default:
		return false
	}
}

func (c *Instance) HoldModeEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan.Mode == ScanModeHold
}

func (c *Instance) SetScanMode(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.Mode = mode
}

func (c *Instance) SetScanExitDelay(exitDelay float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.ExitDelay = exitDelay
}

func (c *Instance) SetScanIgnoreSystem(ignoreSystem []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.IgnoreSystem = ignoreSystem
}

func (c *Instance) Readers() Readers {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers
}

func (c *Instance) AutoDetect() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.AutoDetect
}

func (c *Instance) SetAutoDetect(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.AutoDetect = enabled
}

func (c *Instance) SetReaderConnections(rcs []ReadersConnect) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Connect = rcs
}

func (c *Instance) IsDriverEnabled(driverID string, defaultEnabled bool) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Try normalized ID first, then fall back to original
	normalizedID := normalizeDriverID(driverID)
	for key, cfg := range c.vals.Readers.Drivers {
		if normalizeDriverID(key) == normalizedID && cfg.Enabled != nil {
			return *cfg.Enabled
		}
	}
	return defaultEnabled
}

func (c *Instance) IsDriverAutoDetectEnabled(driverID string, defaultAutoDetect bool) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Try normalized ID first, then fall back to original
	normalizedID := normalizeDriverID(driverID)
	for key, cfg := range c.vals.Readers.Drivers {
		if normalizeDriverID(key) == normalizedID && cfg.AutoDetect != nil {
			return *cfg.AutoDetect
		}
	}

	return c.vals.Readers.AutoDetect && defaultAutoDetect
}

func (c *Instance) ScanHistory() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Readers.ScanHistory == nil {
		return 30 // Default: keep 30 days of scan history
	}
	return *c.vals.Readers.ScanHistory
}
