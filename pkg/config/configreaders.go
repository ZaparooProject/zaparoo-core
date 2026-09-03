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
	Enabled    *bool  `toml:"enabled,omitempty"`
	AutoDetect *bool  `toml:"auto_detect,omitempty"`
	ScanMode   string `toml:"scan_mode,omitempty"`
}

type ReadersScan struct {
	Mode            string          `toml:"mode"`
	OnScan          string          `toml:"on_scan,omitempty"`
	OnRemove        string          `toml:"on_remove,omitempty"`
	IgnoreSystem    []string        `toml:"ignore_system,omitempty"`
	ExitDelay       float32         `toml:"exit_delay,omitempty"`
	IgnoreOnConnect bool            `toml:"ignore_on_connect,omitempty"`
	LaunchGuard     ScanLaunchGuard `toml:"launch_guard,omitempty"`
}

type ScanLaunchGuard struct {
	Timeout        float32 `toml:"timeout,omitempty"`
	Delay          float32 `toml:"delay,omitempty"`
	Enabled        bool    `toml:"enabled,omitempty"`
	RequireConfirm bool    `toml:"require_confirm,omitempty"`
}

type ReadersConnect struct {
	Enabled  *bool  `toml:"enabled,omitempty"`
	Driver   string `toml:"driver"`
	Path     string `toml:"path,omitempty"`
	IDSource string `toml:"id_source,omitempty"`
	ScanMode string `toml:"scan_mode,omitempty"`
}

// DriverInfo contains driver metadata needed for enabled/auto-detect checks.
// This is a subset of reader metadata to avoid circular imports.
type DriverInfo struct {
	ID                string
	DefaultEnabled    bool
	DefaultAutoDetect bool
}

// ReaderEnableContext selects the reader usage path being evaluated.
type ReaderEnableContext string

const (
	// ReaderEnableContextCandidate filters platform-supported reader instances.
	ReaderEnableContextCandidate ReaderEnableContext = "candidate"
	// ReaderEnableContextManualConnect evaluates a configured [[readers.connect]] entry.
	ReaderEnableContextManualConnect ReaderEnableContext = "manual_connect"
	// ReaderEnableContextAutoDetect evaluates whether a reader may probe for devices.
	ReaderEnableContextAutoDetect ReaderEnableContext = "auto_detect"
)

// IsEnabled returns whether this connection is enabled.
// nil (omitted) and true both mean enabled; only explicit false disables.
func (r ReadersConnect) IsEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}

func (r ReadersConnect) ConnectionString() string {
	return fmt.Sprintf("%s:%s", normalizeDriverID(r.Driver), r.Path)
}

// normalizeDriverID returns a driver ID in the form driver IDs are compared in.
// Underscores are dropped for backwards compatibility with the legacy format
// (e.g. "simple_serial"), and case is folded because a driver ID is something a
// user types into a config file: "PN532" and "pn532" name the same driver.
//
// This must stay in step with readers.NormalizeDriverID, which pkg/config
// cannot import.
func normalizeDriverID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "_", ""))
}

func (c *Instance) ReadersScan() ReadersScan {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan
}

func (c *Instance) ScanIgnoreOnConnect() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan.IgnoreOnConnect
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
	// Normalized like GlobalScanMode and ScanModeForReader read the same
	// value: "TAP" is tap, and an unset or unrecognised mode falls through to
	// the tap default rather than reporting neither mode.
	return NormalizeScanMode(c.vals.Readers.Scan.Mode) != ScanModeHold
}

func (c *Instance) HoldModeEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// Normalized, like ScanModeForReader does with the same value: otherwise a
	// global mode of "HOLD" or " hold " holds for a token that came from a
	// reader and taps for one that did not.
	return NormalizeScanMode(c.vals.Readers.Scan.Mode) == ScanModeHold
}

// NormalizeScanMode returns mode as a recognised scan mode, or "" when it is
// unset or unrecognised. Callers reading configuration treat "" as "not set
// here" and fall through to the next level of the override chain rather than
// failing the whole config; callers accepting a value from an API caller
// reject it instead, so a typo is reported rather than silently ignored.
func NormalizeScanMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ScanModeTap:
		return ScanModeTap
	case ScanModeHold:
		return ScanModeHold
	default:
		return ""
	}
}

// matchesAnyDriverID reports whether a config entry's driver names one of the
// IDs a reader answers to. Drivers accept several spellings of their name and
// open under whichever one the config used, so the entry has to be matched
// against the whole set: a reader configured as "pn532_uart" is the same
// reader as one configured as "pn532", and its settings must reach it either
// way.
func matchesAnyDriverID(driverIDs []string, entryDriver string) bool {
	normalizedEntry := normalizeDriverID(entryDriver)
	for _, id := range driverIDs {
		if normalizeDriverID(id) == normalizedEntry {
			return true
		}
	}
	return false
}

// scanModeForReaderLocked resolves a reader's configured scan mode, preferring
// a matching [[readers.connect]] entry over its [readers.drivers.<id>] entry.
// Returns "" when neither sets one. Caller must hold c.mu.
func (c *Instance) scanModeForReaderLocked(driverIDs []string, path string) string {
	for _, conn := range c.vals.Readers.Connect {
		if !matchesAnyDriverID(driverIDs, conn.Driver) || conn.Path != path {
			continue
		}
		if mode := NormalizeScanMode(conn.ScanMode); mode != "" {
			return mode
		}
	}

	for key, driver := range c.vals.Readers.Drivers {
		if !matchesAnyDriverID(driverIDs, key) {
			continue
		}
		if mode := NormalizeScanMode(driver.ScanMode); mode != "" {
			return mode
		}
	}

	return ""
}

// ScanModeForReader returns the effective scan mode for a connected reader,
// resolving its [[readers.connect]] entry, then its [readers.drivers.<id>]
// entry, then the global readers.scan.mode. A per-token ZapScript override
// takes precedence over this and is applied by the service layer.
//
// driverIDs is every ID the reader answers to, not just its canonical one, so
// an entry written with an alias resolves. Callers pass readers.DriverIDs.
func (c *Instance) ScanModeForReader(driverIDs []string, path string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if mode := c.scanModeForReaderLocked(driverIDs, path); mode != "" {
		return mode
	}
	if mode := NormalizeScanMode(c.vals.Readers.Scan.Mode); mode != "" {
		return mode
	}
	return ScanModeTap
}

// GlobalScanMode returns the effective readers.scan.mode, normalized, for
// callers that have no reader to resolve against. Never empty.
func (c *Instance) GlobalScanMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if mode := NormalizeScanMode(c.vals.Readers.Scan.Mode); mode != "" {
		return mode
	}
	return ScanModeTap
}

// HoldModeEnabledForReader reports whether a connected reader holds by default.
func (c *Instance) HoldModeEnabledForReader(driverIDs []string, path string) bool {
	return c.ScanModeForReader(driverIDs, path) == ScanModeHold
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

func (c *Instance) SetScanIgnoreOnConnect(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.IgnoreOnConnect = enabled
}

// DefaultLaunchGuardTimeout is the default timeout in seconds for staged tokens
// when launch_guard is enabled and no custom timeout is set.
const DefaultLaunchGuardTimeout float32 = 15

func (c *Instance) LaunchGuardEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan.LaunchGuard.Enabled
}

func (c *Instance) LaunchGuardTimeout() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Readers.Scan.LaunchGuard.Timeout == 0 {
		return DefaultLaunchGuardTimeout
	}
	return c.vals.Readers.Scan.LaunchGuard.Timeout
}

func (c *Instance) LaunchGuardRequireConfirm() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan.LaunchGuard.RequireConfirm
}

func (c *Instance) SetLaunchGuard(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.LaunchGuard.Enabled = enabled
}

func (c *Instance) SetLaunchGuardTimeout(timeout float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.LaunchGuard.Timeout = timeout
}

func (c *Instance) SetLaunchGuardRequireConfirm(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.LaunchGuard.RequireConfirm = enabled
}

func (c *Instance) LaunchGuardDelay() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	delay := c.vals.Readers.Scan.LaunchGuard.Delay
	if delay == 0 {
		return 0
	}
	timeout := c.vals.Readers.Scan.LaunchGuard.Timeout
	if timeout == 0 {
		timeout = DefaultLaunchGuardTimeout
	}
	if timeout < 0 {
		return 0
	}
	if delay >= timeout {
		return timeout / 2
	}
	return delay
}

func (c *Instance) SetLaunchGuardDelay(delay float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.LaunchGuard.Delay = delay
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

func (c *Instance) hasEnabledReaderConnectionLocked(driverID string) bool {
	normalizedID := normalizeDriverID(driverID)
	for _, conn := range c.vals.Readers.Connect {
		if conn.IsEnabled() && normalizeDriverID(conn.Driver) == normalizedID {
			return true
		}
	}
	return false
}

func (c *Instance) isDriverEnabledLocked(driverID string, defaultEnabled bool) bool {
	normalizedID := normalizeDriverID(driverID)
	for key, cfg := range c.vals.Readers.Drivers {
		if normalizeDriverID(key) == normalizedID && cfg.Enabled != nil {
			return *cfg.Enabled
		}
	}
	return defaultEnabled
}

func (c *Instance) isDriverAutoDetectEnabledLocked(driverID string, defaultAutoDetect bool) bool {
	normalizedID := normalizeDriverID(driverID)
	for key, cfg := range c.vals.Readers.Drivers {
		if normalizeDriverID(key) == normalizedID && cfg.AutoDetect != nil {
			return *cfg.AutoDetect
		}
	}

	return c.vals.Readers.AutoDetect && defaultAutoDetect
}

// IsReaderEnabled centralizes reader enablement rules across platform lists,
// manual connections, and auto-detection.
func (c *Instance) IsReaderEnabled(driver DriverInfo, context ReaderEnableContext) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	switch context {
	case ReaderEnableContextCandidate:
		if c.isDriverEnabledLocked(driver.ID, driver.DefaultEnabled) {
			return true
		}
		return c.hasEnabledReaderConnectionLocked(driver.ID) && c.isDriverEnabledLocked(driver.ID, true)
	case ReaderEnableContextManualConnect:
		return c.isDriverEnabledLocked(driver.ID, true)
	case ReaderEnableContextAutoDetect:
		return c.isDriverEnabledLocked(driver.ID, driver.DefaultEnabled) &&
			c.isDriverAutoDetectEnabledLocked(driver.ID, driver.DefaultAutoDetect)
	default:
		return false
	}
}

func (c *Instance) ScanHistory() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Readers.ScanHistory == nil {
		return 30 // Default: keep 30 days of scan history
	}
	return *c.vals.Readers.ScanHistory
}
