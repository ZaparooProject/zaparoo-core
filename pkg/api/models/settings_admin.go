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

//nolint:revive // custom validation tags (system, etc.) are unknown to revive
package models

import (
	"bytes"
	"encoding/json"
	"reflect"
)

// What a settings change needs before it shows, beyond being saved.
const (
	// SettingNeedsRestart means the value is used from the next Core start.
	SettingNeedsRestart = "restart"
	// SettingNeedsReindex means the media database reflects the value after
	// the next index.
	SettingNeedsReindex = "reindex"
	// SettingNeedsRescrape means scraped metadata reflects the value after
	// the next scrape.
	SettingNeedsRescrape = "rescrape"
)

// Sound setting modes.
const (
	SoundModeDefault = "default"
	SoundModeOff     = "off"
	SoundModeFile    = "file"
)

// UpdateSettingsResult is the result of settings.update. Applied names each
// field the request set, with what it still needs to take effect; an empty
// list means it applied at once.
type UpdateSettingsResult struct {
	Applied map[string][]string `json:"applied"`
}

// SoundSetting is one scan feedback sound. Mode "default" plays the built-in
// sound, or an override file in Core's assets folder; "off" plays nothing;
// "file" plays Path.
type SoundSetting struct {
	Mode string `json:"mode"`
	Path string `json:"path,omitempty"`
}

// SoundSettingParam sets one scan feedback sound. For mode "file", Root and
// Path name a file found with files.browse purpose "audio"; Path alone is an
// absolute path on the Core host.
type SoundSettingParam struct {
	Mode string `json:"mode" validate:"required,oneof=default off file"`
	Root string `json:"root,omitempty" validate:"omitempty,max=64"`
	Path string `json:"path,omitempty" validate:"omitempty,max=4096"`
}

// ReaderDriver is one [readers.drivers] table. Nil fields use the driver's
// default.
type ReaderDriver struct {
	Enabled         *bool  `json:"enabled,omitempty"`
	AutoDetect      *bool  `json:"autoDetect,omitempty"`
	KeepReading     *bool  `json:"keepReading,omitempty"`
	ID              string `json:"id" validate:"required,min=1,max=64,printascii"`
	ScanMode        string `json:"scanMode,omitempty"`
	LaunchOrder     string `json:"launchOrder,omitempty" validate:"omitempty,oneof=image_first disc_first image_only disc_only"`
	AudioCDLauncher string `json:"audioCdLauncher,omitempty" validate:"omitempty,max=128"`
}

// LauncherDefault is one [[launchers.default]] entry. InstallDir and
// ServerURL are shown but only change in config.toml.
type LauncherDefault struct {
	RenderScale      *int   `json:"renderScale,omitempty"`
	ScanDuplicates   *bool  `json:"scanDuplicates,omitempty"`
	Launcher         string `json:"launcher"`
	InstallDir       string `json:"installDir,omitempty"`
	ServerURL        string `json:"serverUrl,omitempty"`
	RenderResolution string `json:"renderResolution,omitempty"`
	Client           string `json:"client,omitempty"`
	Action           string `json:"action,omitempty"`
	LoadPath         string `json:"loadPath,omitempty"`
	BeforeExit       string `json:"beforeExit,omitempty"`
}

// LauncherDefaultParam sets one [[launchers.default]] entry. An entry keeps
// the install_dir and server_url of the entry for the same launcher it
// replaces.
type LauncherDefaultParam struct {
	RenderScale      *int   `json:"renderScale,omitempty" validate:"omitempty,gt=0,lte=16"`
	ScanDuplicates   *bool  `json:"scanDuplicates,omitempty"`
	Launcher         string `json:"launcher" validate:"required,min=1,max=128"`
	RenderResolution string `json:"renderResolution,omitempty" validate:"omitempty,max=32"`
	Client           string `json:"client,omitempty" validate:"omitempty,max=256"`
	Action           string `json:"action,omitempty" validate:"omitempty,oneof=run details"`
	LoadPath         string `json:"loadPath,omitempty" validate:"omitempty,max=512"`
	BeforeExit       string `json:"beforeExit,omitempty" validate:"omitempty,max=4096"`
}

// SystemCategory is one [[systems.category]] entry.
type SystemCategory struct {
	Name    string   `json:"name" validate:"required,min=1,max=400"`
	Systems []string `json:"systems" validate:"omitempty,max=1024,dive,system"`
}

// MQTTPublisherSetting is one [[service.publishers.mqtt]] entry. Broker and
// Topic are shown but only change in config.toml.
type MQTTPublisherSetting struct {
	Broker  string   `json:"broker"`
	Topic   string   `json:"topic"`
	Filter  []string `json:"filter"`
	Index   int      `json:"index"`
	Enabled bool     `json:"enabled"`
}

// PixelcadePublisherSetting is one [[service.publishers.pixelcade]] entry.
// Host and Port are shown but only change in config.toml.
type PixelcadePublisherSetting struct {
	Host    string   `json:"host"`
	Mode    string   `json:"mode"`
	Filter  []string `json:"filter"`
	Index   int      `json:"index"`
	Port    int      `json:"port"`
	Enabled bool     `json:"enabled"`
}

// PublisherUpdate changes the publisher at Index, which is required so a
// missing index cannot fall back to the first publisher. Mode applies to
// Pixelcade publishers only.
type PublisherUpdate struct {
	Enabled *bool     `json:"enabled,omitempty"`
	Filter  *[]string `json:"filter,omitempty" validate:"omitempty,max=64,dive,min=1,max=128"`
	Mode    *string   `json:"mode,omitempty" validate:"omitempty,oneof=stream write"`
	Index   *int      `json:"index" validate:"required,gte=0"`
}

// AdminSettings are the settings only a local or admin client reads. They
// are left out of the settings response for every other client.
type AdminSettings struct {
	AudioSuccessSound          SoundSetting                `json:"audioSuccessSound"`
	AudioFailSound             SoundSetting                `json:"audioFailSound"`
	AudioLimitSound            SoundSetting                `json:"audioLimitSound"`
	AudioPendingSound          SoundSetting                `json:"audioPendingSound"`
	AudioReadySound            SoundSetting                `json:"audioReadySound"`
	InputGamepadEnabled        *bool                       `json:"inputGamepadEnabled"`
	ReadersScanOnScan          string                      `json:"readersScanOnScan"`
	ReadersScanOnRemove        string                      `json:"readersScanOnRemove"`
	LaunchersBeforeMediaStart  string                      `json:"launchersBeforeMediaStart"`
	LaunchersOnMediaStart      string                      `json:"launchersOnMediaStart"`
	LaunchersBeforeExit        string                      `json:"launchersBeforeExit"`
	BackupScope                string                      `json:"backupScope"`
	DiscoveryInstanceName      string                      `json:"discoveryInstanceName"`
	BLEName                    string                      `json:"bleName"`
	ServiceOnBoot              string                      `json:"serviceOnBoot"`
	ServiceOnReady             string                      `json:"serviceOnReady"`
	DeviceID                   string                      `json:"deviceId"`
	APIListen                  string                      `json:"apiListen"`
	ReadersDrivers             []ReaderDriver              `json:"readersDrivers"`
	LauncherDefaults           []LauncherDefault           `json:"launcherDefaults"`
	SystemCategories           []SystemCategory            `json:"systemCategories"`
	MediaDefaultRegions        []string                    `json:"mediaDefaultRegions"`
	MediaDefaultLangs          []string                    `json:"mediaDefaultLangs"`
	PublishersMQTT             []MQTTPublisherSetting      `json:"publishersMqtt"`
	PublishersPixelcade        []PixelcadePublisherSetting `json:"publishersPixelcade"`
	ReadersScanHistory         int                         `json:"readersScanHistory"`
	APIPort                    int                         `json:"apiPort"`
	ReadersScanIgnoreOnConnect bool                        `json:"readersScanIgnoreOnConnect"`
	ReadersScanAllowRelaunch   bool                        `json:"readersScanAllowRelaunch"`
	MediaFilenameTags          bool                        `json:"mediaFilenameTags"`
	DiscoveryEnabled           bool                        `json:"discoveryEnabled"`
	InternetCheck              bool                        `json:"internetCheck"`
}

// AdminSettingsParams are the settings.update fields only a local or admin
// client may send.
type AdminSettingsParams struct {
	AudioSuccessSound          *SoundSettingParam      `json:"audioSuccessSound,omitempty"`
	AudioFailSound             *SoundSettingParam      `json:"audioFailSound,omitempty"`
	AudioLimitSound            *SoundSettingParam      `json:"audioLimitSound,omitempty"`
	AudioPendingSound          *SoundSettingParam      `json:"audioPendingSound,omitempty"`
	AudioReadySound            *SoundSettingParam      `json:"audioReadySound,omitempty"`
	ReadersScanIgnoreOnConnect *bool                   `json:"readersScanIgnoreOnConnect,omitempty"`
	ReadersScanAllowRelaunch   *bool                   `json:"readersScanAllowRelaunch,omitempty"`
	ReadersScanOnScan          *string                 `json:"readersScanOnScan,omitempty" validate:"omitempty,max=4096"`
	ReadersScanOnRemove        *string                 `json:"readersScanOnRemove,omitempty" validate:"omitempty,max=4096"`
	ReadersScanHistory         *int                    `json:"readersScanHistory,omitempty" validate:"omitempty,gte=0,lte=36500"`
	ReadersDrivers             *[]ReaderDriver         `json:"readersDrivers,omitempty" validate:"omitempty,max=64,dive"`
	LaunchersBeforeMediaStart  *string                 `json:"launchersBeforeMediaStart,omitempty" validate:"omitempty,max=4096"`
	LaunchersOnMediaStart      *string                 `json:"launchersOnMediaStart,omitempty" validate:"omitempty,max=4096"`
	LaunchersBeforeExit        *string                 `json:"launchersBeforeExit,omitempty" validate:"omitempty,max=4096"`
	LauncherDefaults           *[]LauncherDefaultParam `json:"launcherDefaults,omitempty" validate:"omitempty,max=256,dive"`
	SystemCategories           *[]SystemCategory       `json:"systemCategories,omitempty" validate:"omitempty,max=256,dive"`
	MediaFilenameTags          *bool                   `json:"mediaFilenameTags,omitempty"`
	MediaDefaultRegions        *[]string               `json:"mediaDefaultRegions,omitempty" validate:"omitempty,max=64,dive,min=1,max=32"`
	MediaDefaultLangs          *[]string               `json:"mediaDefaultLangs,omitempty" validate:"omitempty,max=64,dive,min=1,max=32"`
	BackupScope                *string                 `json:"backupScope,omitempty" validate:"omitempty,oneof=platform zaparoo"`
	DiscoveryEnabled           *bool                   `json:"discoveryEnabled,omitempty"`
	DiscoveryInstanceName      *string                 `json:"discoveryInstanceName,omitempty" validate:"omitempty,max=63"`
	BLEName                    *string                 `json:"bleName,omitempty" validate:"omitempty,max=29"`
	InternetCheck              *bool                   `json:"internetCheck,omitempty"`
	ServiceOnBoot              *string                 `json:"serviceOnBoot,omitempty" validate:"omitempty,max=4096"`
	ServiceOnReady             *string                 `json:"serviceOnReady,omitempty" validate:"omitempty,max=4096"`
	PublishersMQTT             *[]PublisherUpdate      `json:"publishersMqtt,omitempty" validate:"omitempty,max=64,dive"`
	PublishersPixelcade        *[]PublisherUpdate      `json:"publishersPixelcade,omitempty" validate:"omitempty,max=64,dive"`
	InputGamepadEnabled        NullableBool            `json:"inputGamepadEnabled,omitzero"`
}

// Set reports whether the request sends any admin-only field.
func (p *AdminSettingsParams) Set() bool {
	v := reflect.ValueOf(p).Elem()
	for i := range v.NumField() {
		if ParamSent(v.Field(i)) {
			return true
		}
	}
	return false
}

// NullableBool is a parameter where null is a value of its own: null puts
// the setting back to its default, while leaving the key out changes nothing.
type NullableBool struct {
	Value *bool
	Sent  bool
}

func (n *NullableBool) UnmarshalJSON(data []byte) error {
	n.Sent = true
	n.Value = nil
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	var v bool
	if err := json.Unmarshal(data, &v); err != nil {
		return err //nolint:wrapcheck // the decoder reports the field
	}
	n.Value = &v
	return nil
}

func (n NullableBool) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*n.Value) //nolint:wrapcheck // a bool always marshals
}

// ParamSent reports whether a request parameter field was in the request:
// a non-nil pointer, or a NullableBool that was sent, null included.
func ParamSent(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer:
		return !v.IsNil()
	case reflect.Struct:
		if n, ok := v.Interface().(NullableBool); ok {
			return n.Sent
		}
	default:
	}
	return false
}
