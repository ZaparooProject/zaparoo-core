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
	"maps"
	"path/filepath"
	"slices"
)

// Sound names one of the scan feedback sounds in [audio].
type Sound string

const (
	SoundSuccess Sound = "success"
	SoundFail    Sound = "fail"
	SoundLimit   Sound = "limit"
	SoundPending Sound = "pending"
	SoundReady   Sound = "ready"
)

// Sounds lists every scan feedback sound.
var Sounds = []Sound{SoundSuccess, SoundFail, SoundLimit, SoundPending, SoundReady}

func (a *Audio) sound(s Sound) **string {
	switch s {
	case SoundSuccess:
		return &a.SuccessSound
	case SoundFail:
		return &a.FailSound
	case SoundLimit:
		return &a.LimitSound
	case SoundPending:
		return &a.PendingSound
	case SoundReady:
		return &a.ReadySound
	default:
		return nil
	}
}

// SoundSetting returns a sound's setting as written: nil plays the built-in
// sound or an override file in the assets folder, "" plays nothing, and any
// other value is the file to play.
func (c *Instance) SoundSetting(s Sound) *string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	field := c.vals.Audio.sound(s)
	if field == nil || *field == nil {
		return nil
	}
	value := **field
	return &value
}

// SetSoundSetting sets a sound with the meaning SoundSetting describes.
func (c *Instance) SetSoundSetting(s Sound, value *string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	field := c.vals.Audio.sound(s)
	if field == nil {
		return fmt.Errorf("unknown sound %q", s)
	}
	if value == nil {
		*field = nil
		return nil
	}
	v := *value
	*field = &v
	return nil
}

// resolveDataPath resolves a configured folder: a relative one is under the
// data folder, as sound paths are, rather than wherever Core was started.
func resolveDataPath(dataDir, configured string) string {
	if configured == "" || filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Join(dataDir, configured)
}

// BackupLocalDirIn returns backup.local_dir with a relative folder resolved
// under dataDir, or "" when it is not set.
func (c *Instance) BackupLocalDirIn(dataDir string) string {
	return resolveDataPath(dataDir, c.BackupLocalDir())
}

// ScraperGamelistXMLCustomDir returns scraper.gamelist_xml.custom_path with
// a relative folder resolved under dataDir, or "" when it is not set.
func (c *Instance) ScraperGamelistXMLCustomDir(dataDir string) string {
	return resolveDataPath(dataDir, c.ScraperGamelistXMLCustomPath())
}

// VirtualGamepadSetting returns input.gamepad_enabled as written, nil when
// the platform default applies.
func (c *Instance) VirtualGamepadSetting() *bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Input.GamepadEnabled == nil {
		return nil
	}
	v := *c.vals.Input.GamepadEnabled
	return &v
}

// ClearVirtualGamepadSetting goes back to the platform default.
func (c *Instance) ClearVirtualGamepadSetting() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Input.GamepadEnabled = nil
}

func (c *Instance) SetScanOnScan(script string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.OnScan = script
}

func (c *Instance) SetScanOnRemove(script string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.OnRemove = script
}

func (c *Instance) SetScanAllowRelaunch(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.AllowRelaunch = enabled
}

// SetScanHistory sets how many days of scan history are kept.
func (c *Instance) SetScanHistory(days int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.ScanHistory = &days
}

// ReaderDrivers returns a copy of the [readers.drivers] tables.
func (c *Instance) ReaderDrivers() map[string]DriverConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return maps.Clone(c.vals.Readers.Drivers)
}

// SetReaderDrivers replaces the [readers.drivers] tables.
func (c *Instance) SetReaderDrivers(drivers map[string]DriverConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(drivers) == 0 {
		c.vals.Readers.Drivers = nil
		return
	}
	c.vals.Readers.Drivers = maps.Clone(drivers)
}

func (c *Instance) SetLaunchersBeforeMediaStart(script string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Launchers.BeforeMediaStart = script
}

func (c *Instance) SetLaunchersOnMediaStart(script string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Launchers.OnMediaStart = script
}

func (c *Instance) SetLaunchersBeforeExit(script string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Launchers.BeforeExit = script
}

// LauncherDefaults returns a copy of the [[launchers.default]] entries.
func (c *Instance) LauncherDefaults() []LaunchersDefault {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.vals.Launchers.Default)
}

// SetLauncherDefaults replaces the [[launchers.default]] entries, refusing
// the list a config load would refuse.
func (c *Instance) SetLauncherDefaults(defaults []LaunchersDefault) error {
	if err := ValidateLauncherDefaults(defaults); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Launchers.Default = slices.Clone(defaults)
	return nil
}

// SystemCategories returns a copy of the [[systems.category]] entries as
// written, including any a load ignored.
func (c *Instance) SystemCategories() []SystemsCategory {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.vals.Systems.Category)
}

// ValidateSystemCategories reports the first entry SetSystemCategories would
// refuse.
func ValidateSystemCategories(categories []SystemsCategory) error {
	if _, problems := newCategoryResolver(categories); len(problems) > 0 {
		return problems[0]
	}
	return nil
}

// SetSystemCategories replaces the [[systems.category]] entries. Unlike a
// load, which skips a bad entry so a typo cannot stop Core starting, every
// entry must be valid, and the resolver is rebuilt so the change applies
// without a reload.
func (c *Instance) SetSystemCategories(categories []SystemsCategory) error {
	resolver, problems := newCategoryResolver(categories)
	if len(problems) > 0 {
		return problems[0]
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Systems.Category = slices.Clone(categories)
	c.loaded.categoryResolver = resolver
	return nil
}

// DefaultRegionsSetting returns media.default_regions as written, empty when
// the built-in default applies.
func (c *Instance) DefaultRegionsSetting() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.vals.Media.DefaultRegions)
}

func (c *Instance) SetDefaultRegions(regions []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Media.DefaultRegions = slices.Clone(regions)
}

// DefaultLangsSetting returns media.default_langs as written, empty when the
// built-in default applies.
func (c *Instance) DefaultLangsSetting() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.vals.Media.DefaultLangs)
}

func (c *Instance) SetDefaultLangs(langs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Media.DefaultLangs = slices.Clone(langs)
}

func (c *Instance) SetServiceOnBoot(script string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Service.OnBoot = script
}

func (c *Instance) SetServiceOnReady(script string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Service.OnReady = script
}

func (c *Instance) SetInternetCheck(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Service.InternetCheck = &enabled
}

// SetMQTTPublishers replaces the [[service.publishers.mqtt]] entries.
func (c *Instance) SetMQTTPublishers(pubs []MQTTPublisher) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Service.Publishers.MQTT = slices.Clone(pubs)
}

// SetPixelCadePublishers replaces the [[service.publishers.pixelcade]]
// entries.
func (c *Instance) SetPixelCadePublishers(pubs []PixelCadePublisher) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Service.Publishers.PixelCade = slices.Clone(pubs)
}
