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
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
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
	// KeepReading, LaunchOrder and AudioCDLauncher apply to the optical
	// drive reader only.
	KeepReading *bool `toml:"keep_reading,omitempty"`
	// Features turns on optional driver behaviour for every reader of the
	// driver. See ReaderFeatures. A pointer, so an explicit empty list
	// survives a save as distinct from an unset one.
	Features        *[]string `toml:"features,omitempty"`
	ScanMode        string    `toml:"scan_mode,omitempty"`
	LaunchOrder     string    `toml:"launch_order,omitempty"`
	AudioCDLauncher string    `toml:"audio_cd_launcher,omitempty"`
	// Rotation turns a display accessory's output, in degrees clockwise, for a
	// panel mounted the other way up. Degrees rather than a flip flag so
	// quarter turns do not need a new field later. A driver whose hardware
	// cannot do the requested angle reports that rather than guessing.
	Rotation int `toml:"rotation,omitempty"`
}

// Display rotations a driver may be asked for. Only 0 and 180 work on current
// hardware: a quarter turn needs a layout built for the other aspect, not a
// transform, so it is accepted here and refused by the driver until some
// display can honour it.
const (
	RotationNone     = 0
	RotationQuarter  = 90
	RotationHalf     = 180
	RotationThreeQtr = 270
)

// IsValidRotation reports whether degrees is a rotation the config accepts.
func IsValidRotation(degrees int) bool {
	switch degrees {
	case RotationNone, RotationQuarter, RotationHalf, RotationThreeQtr:
		return true
	default:
		return false
	}
}

// Optical drive launch orders. They decide between a library image the disc
// matched and launching the disc itself.
const (
	DiscLaunchImageFirst = "image_first"
	DiscLaunchDiscFirst  = "disc_first"
	DiscLaunchImageOnly  = "image_only"
	DiscLaunchDiscOnly   = "disc_only"
)

const opticalDriveDriverID = "opticaldrive"

type ReadersScan struct {
	Mode            string          `toml:"mode"`
	OnScan          string          `toml:"on_scan,omitempty"`
	OnRemove        string          `toml:"on_remove,omitempty"`
	IgnoreSystem    []string        `toml:"ignore_system,omitempty"`
	ExitDelay       float32         `toml:"exit_delay,omitempty"`
	IgnoreOnConnect bool            `toml:"ignore_on_connect,omitempty"`
	AllowRelaunch   bool            `toml:"allow_relaunch,omitempty"`
	LaunchGuard     ScanLaunchGuard `toml:"launch_guard,omitempty"`
}

type ScanLaunchGuard struct {
	Timeout        float32 `toml:"timeout,omitempty"`
	Delay          float32 `toml:"delay,omitempty"`
	Enabled        bool    `toml:"enabled,omitempty"`
	RequireConfirm bool    `toml:"require_confirm,omitempty"`
}

type ReadersConnect struct {
	Enabled *bool `toml:"enabled,omitempty"`
	// Features replaces the driver's features for this reader. It is a
	// pointer so that an empty list, which turns them all off, is told apart
	// from a missing one, which keeps the driver's.
	Features *[]string `toml:"features,omitempty"`
	Driver   string    `toml:"driver"`
	Path     string    `toml:"path,omitempty"`
	IDSource string    `toml:"id_source,omitempty"`
	ScanMode string    `toml:"scan_mode,omitempty"`
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
func (r ReadersConnect) IsEnabled() bool { //nolint:gocritic // hugeParam
	return r.Enabled == nil || *r.Enabled
}

func (r ReadersConnect) ConnectionString() string { //nolint:gocritic // hugeParam
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
	// Connect entries are a slice, so more than one entry naming the same
	// reader resolves in the order the config file lists them.
	for _, conn := range c.vals.Readers.Connect {
		if !matchesAnyDriverID(driverIDs, conn.Driver) || conn.Path != path {
			continue
		}
		if mode := NormalizeScanMode(conn.ScanMode); mode != "" {
			return mode
		}
	}

	// The driver table is a map, and a reader answers to several IDs, so more
	// than one entry can name it. Walk driverIDs in order instead of the map,
	// which puts the canonical ID ahead of its aliases and keeps the answer the
	// same on every call.
	for _, id := range driverIDs {
		if mode := c.driverScanModeLocked(id); mode != "" {
			return mode
		}
	}

	return ""
}

// driverScanModeLocked returns the scan mode set by the [readers.drivers.<id>]
// entry for one driver ID, or "" when no entry names it or none sets a mode.
// Distinct keys can normalize alike ("pn532" and "PN532"), so a tie is broken
// on the key itself rather than on map iteration order. Caller must hold c.mu.
func (c *Instance) driverScanModeLocked(driverID string) string {
	normalizedID := normalizeDriverID(driverID)

	matched, mode := "", ""
	for key, driver := range c.vals.Readers.Drivers {
		if normalizeDriverID(key) != normalizedID {
			continue
		}
		keyMode := NormalizeScanMode(driver.ScanMode)
		if keyMode == "" {
			continue
		}
		if matched == "" || key < matched {
			matched, mode = key, keyMode
		}
	}
	return mode
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

// ReaderFeatureFeliCa makes a PN532 reader poll for FeliCa cards as well as
// ISO14443A ones.
const ReaderFeatureFeliCa = "felica"

// NormalizeReaderFeature returns the canonical spelling of a reader feature,
// or "" when it is not one.
func NormalizeReaderFeature(feature string) string {
	switch strings.ToLower(strings.TrimSpace(feature)) {
	case ReaderFeatureFeliCa:
		return ReaderFeatureFeliCa
	default:
		return ""
	}
}

// ReaderFeatures returns the features turned on for a connected reader,
// canonical and without duplicates. A matching [[readers.connect]] entry that
// sets features replaces the [readers.drivers.<id>] list, so an empty list
// there turns every feature off for that reader. Unknown names are dropped;
// they were warned about when the config loaded.
//
// driverIDs is every ID the reader answers to. Callers pass readers.DriverIDs.
func (c *Instance) ReaderFeatures(driverIDs []string, path string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return NormalizeReaderFeatures(c.readerFeaturesLocked(driverIDs, path))
}

// HasReaderFeature reports whether a feature is turned on for a connected
// reader.
func (c *Instance) HasReaderFeature(driverIDs []string, path, feature string) bool {
	return slices.Contains(c.ReaderFeatures(driverIDs, path), feature)
}

// readerFeaturesLocked resolves a reader's raw feature list in the same order
// as scanModeForReaderLocked. Caller must hold c.mu.
func (c *Instance) readerFeaturesLocked(driverIDs []string, path string) []string {
	for _, conn := range c.vals.Readers.Connect {
		if conn.Features == nil || conn.Path != path || !matchesAnyDriverID(driverIDs, conn.Driver) {
			continue
		}
		return *conn.Features
	}

	for _, id := range driverIDs {
		normalizedID := normalizeDriverID(id)
		matched := ""
		var features []string
		for key, driver := range c.vals.Readers.Drivers {
			if driver.Features == nil || normalizeDriverID(key) != normalizedID {
				continue
			}
			if matched == "" || key < matched {
				matched, features = key, *driver.Features
			}
		}
		if matched != "" {
			return features
		}
	}
	return nil
}

// NormalizeReaderFeatures returns the features in raw that Core knows, in
// canonical form and without duplicates. Unknown names are dropped.
func NormalizeReaderFeatures(raw []string) []string {
	var out []string
	for _, feature := range raw {
		if canonical := NormalizeReaderFeature(feature); canonical != "" && !slices.Contains(out, canonical) {
			out = append(out, canonical)
		}
	}
	return out
}

// warnUnknownReaderFeatures logs each feature name the config gives a reader
// that Core does not know. They are ignored rather than failing the load.
func warnUnknownReaderFeatures(readers *Readers) {
	warn := func(where string, features []string) {
		for _, feature := range features {
			if NormalizeReaderFeature(feature) == "" {
				log.Warn().Str("reader", where).Str("feature", feature).
					Msg("ignoring unknown reader feature in config.toml")
			}
		}
	}
	for id, driver := range readers.Drivers {
		if driver.Features != nil {
			warn(id, *driver.Features)
		}
	}
	for _, conn := range readers.Connect {
		if conn.Features != nil {
			warn(conn.Driver+":"+conn.Path, *conn.Features)
		}
	}
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

// replacedDriverIDs maps a driver to the IDs of drivers it took over from.
// Settings and connections written for a replaced driver apply to its
// successor, so an install that disabled the old one does not find the new
// one switched on.
var replacedDriverIDs = map[string][]string{
	"ccid": {"acr122pcsc"},
}

// driverConfigIDs returns the normalized IDs whose config applies to a driver:
// its own first, then any it replaced.
func driverConfigIDs(driverID string) []string {
	normalizedID := normalizeDriverID(driverID)
	return append([]string{normalizedID}, replacedDriverIDs[normalizedID]...)
}

func (c *Instance) hasEnabledReaderConnectionLocked(driverID string) bool {
	ids := driverConfigIDs(driverID)
	for _, conn := range c.vals.Readers.Connect {
		if conn.IsEnabled() && slices.Contains(ids, normalizeDriverID(conn.Driver)) {
			return true
		}
	}
	return false
}

// driverSettingLocked returns the first value a [readers.drivers.<id>] entry
// sets for a driver, looking at the driver's own entry before one written for
// a driver it replaced. Distinct keys can normalize alike, so a tie is broken
// on the key itself rather than on map iteration order. Caller must hold c.mu.
func (c *Instance) driverSettingLocked(driverID string, pick func(DriverConfig) *bool) *bool {
	for _, id := range driverConfigIDs(driverID) {
		matched := ""
		var value *bool
		for key, cfg := range c.vals.Readers.Drivers {
			if normalizeDriverID(key) != id {
				continue
			}
			if picked := pick(cfg); picked != nil && (matched == "" || key < matched) {
				matched, value = key, picked
			}
		}
		if value != nil {
			return value
		}
	}
	return nil
}

// driverStringSettingLocked is driverSettingLocked for string settings,
// where empty means unset. Caller must hold c.mu.
func (c *Instance) driverStringSettingLocked(driverID string, pick func(DriverConfig) string) string {
	for _, id := range driverConfigIDs(driverID) {
		matched := ""
		for key, cfg := range c.vals.Readers.Drivers {
			if normalizeDriverID(key) == id && pick(cfg) != "" && (matched == "" || key < matched) {
				matched = key
			}
		}
		if matched != "" {
			return pick(c.vals.Readers.Drivers[matched])
		}
	}
	return ""
}

// OpticalDriveKeepReading reports whether the optical drive reader reads the
// disc every poll instead of only asking the drive for its state. Reading
// keeps the drive audibly busy, which can cause skips during disc playback.
func (c *Instance) OpticalDriveKeepReading() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v := c.driverSettingLocked(opticalDriveDriverID, func(cfg DriverConfig) *bool { return cfg.KeepReading })
	return v != nil && *v
}

// OpticalDriveLaunchOrder returns how a scanned disc chooses between a
// matching library image and the disc itself. Unknown values fall back to
// DiscLaunchImageFirst.
func (c *Instance) OpticalDriveLaunchOrder() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v := strings.ToLower(strings.TrimSpace(
		c.driverStringSettingLocked(opticalDriveDriverID, func(cfg DriverConfig) string { return cfg.LaunchOrder })))
	switch v {
	case DiscLaunchImageFirst, DiscLaunchDiscFirst, DiscLaunchImageOnly, DiscLaunchDiscOnly:
		return v
	default:
		return DiscLaunchImageFirst
	}
}

// OpticalDriveAudioCDLauncher returns the launcher ID that plays audio CDs
// from the drive, or "" for the platform default.
func (c *Instance) OpticalDriveAudioCDLauncher() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return strings.TrimSpace(
		c.driverStringSettingLocked(opticalDriveDriverID, func(cfg DriverConfig) string { return cfg.AudioCDLauncher }))
}

func (c *Instance) isDriverEnabledLocked(driverID string, defaultEnabled bool) bool {
	enabled := c.driverSettingLocked(driverID, func(cfg DriverConfig) *bool { return cfg.Enabled })
	if enabled != nil {
		return *enabled
	}
	return defaultEnabled
}

func (c *Instance) isDriverAutoDetectEnabledLocked(driverID string, defaultAutoDetect bool) bool {
	autoDetect := c.driverSettingLocked(driverID, func(cfg DriverConfig) *bool { return cfg.AutoDetect })
	if autoDetect != nil {
		return *autoDetect
	}
	return c.vals.Readers.AutoDetect && defaultAutoDetect
}

// DriverRotation returns the display rotation configured for a driver, in
// degrees clockwise, or 0 when none is set.
//
// The value is returned as written rather than validated here. Which angles are
// possible is a property of the panel, not of the config, so the driver is the
// only place that can tell a typo from an angle its hardware cannot manage -
// and it reports both the same way.
func (c *Instance) DriverRotation(driverID string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normalizedID := normalizeDriverID(driverID)
	for key, cfg := range c.vals.Readers.Drivers {
		if normalizeDriverID(key) == normalizedID {
			return cfg.Rotation
		}
	}
	return RotationNone
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
