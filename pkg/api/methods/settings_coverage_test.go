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

package methods

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
)

// settingsField points a config key at the settings field any client reads
// and writes.
type settingsField string

// adminField points a config key at the AdminSettings field only a local or
// admin client reads and writes.
type adminField string

// otherMethod points a config key at the API method that manages it.
type otherMethod string

// fileOnly gives the reason a config key is only set in config.toml.
type fileOnly string

const (
	reasonAllowList = "security allow list; widening it from the network defeats it"
	reasonGroovy    = "the GMC proxy listens unauthenticated on every interface and turns packets into tokens"
	reasonInput     = "input mode and command blocks bound what a token may type or run; " +
		"opening them remotely is close to running code"
	reasonExecDir = "Core runs programs found in this folder"
	reasonLogins  = "saved logins are sent to this server"
	reasonRoots   = "widens what media.asset, browse and launch accept"
	reasonWrites  = "Core writes files into this folder"
	reasonHost    = "scans would be sent to a host the caller picks; shown, never written"
	reasonCustom  = "custom launcher commands run on the host; launchers.custom shows them read-only"
)

// configKeyAPI classifies every config.toml key. A new key fails
// TestEveryConfigKeyIsClassified until it is exposed through the API or
// given a reason to stay file-only.
var configKeyAPI = map[string]any{
	"audio.fail_sound":     adminField("audioFailSound"),
	"audio.limit_sound":    adminField("audioLimitSound"),
	"audio.midi_soundfont": settingsField("audioMidiSoundFont"),
	"audio.music_autoplay": settingsField("audioMusicAutoplay"),
	"audio.music_volume":   settingsField("audioMusicVolume"),
	"audio.normalize":      settingsField("audioNormalize"),
	"audio.pending_sound":  adminField("audioPendingSound"),
	"audio.ready_sound":    adminField("audioReadySound"),
	"audio.scan_feedback":  settingsField("audioScanFeedback"),
	"audio.success_sound":  adminField("audioSuccessSound"),
	"audio.volume":         settingsField("audioVolume"),

	"backup.local_dir":          fileOnly(reasonWrites),
	"backup.remote.auto_backup": settingsField("backupRemoteAutoBackup"),
	"backup.remote.enabled":     settingsField("backupRemoteEnabled"),
	"backup.remote.schedule":    settingsField("backupRemoteSchedule"),
	"backup.scope":              adminField("backupScope"),

	"config_schema":   fileOnly("config migration marker Core manages itself"),
	"debug_logging":   settingsField("debugLogging"),
	"error_reporting": settingsField("errorReporting"),

	"groovy.gmc_proxy_beacon_interval": fileOnly(reasonGroovy),
	"groovy.gmc_proxy_enabled":         fileOnly(reasonGroovy),
	"groovy.gmc_proxy_port":            fileOnly(reasonGroovy),

	"input.gamepad_enabled": adminField("inputGamepadEnabled"),

	"launchers.allow_file":                      fileOnly(reasonAllowList),
	"launchers.before_exit":                     adminField("launchersBeforeExit"),
	"launchers.before_media_start":              adminField("launchersBeforeMediaStart"),
	"launchers.custom.backend":                  fileOnly(reasonCustom),
	"launchers.custom.categories":               fileOnly(reasonCustom),
	"launchers.custom.category":                 fileOnly(reasonCustom),
	"launchers.custom.controls":                 fileOnly(reasonCustom),
	"launchers.custom.execute":                  fileOnly(reasonCustom),
	"launchers.custom.file_exts":                fileOnly(reasonCustom),
	"launchers.custom.groups":                   fileOnly(reasonCustom),
	"launchers.custom.id":                       fileOnly(reasonCustom),
	"launchers.custom.kind":                     fileOnly(reasonCustom),
	"launchers.custom.lifecycle":                fileOnly(reasonCustom),
	"launchers.custom.load_path":                fileOnly(reasonCustom),
	"launchers.custom.media_dirs":               fileOnly(reasonCustom),
	"launchers.custom.name":                     fileOnly(reasonCustom),
	"launchers.custom.restricted":               fileOnly(reasonCustom),
	"launchers.custom.schemes":                  fileOnly(reasonCustom),
	"launchers.custom.system":                   fileOnly(reasonCustom),
	"launchers.default.action":                  adminField("launcherDefaults"),
	"launchers.default.before_exit":             adminField("launcherDefaults"),
	"launchers.default.client":                  adminField("launcherDefaults"),
	"launchers.default.install_dir":             fileOnly(reasonExecDir),
	"launchers.default.launcher":                adminField("launcherDefaults"),
	"launchers.default.load_path":               adminField("launcherDefaults"),
	"launchers.default.render_resolution":       adminField("launcherDefaults"),
	"launchers.default.render_scale":            adminField("launcherDefaults"),
	"launchers.default.scan_duplicates":         adminField("launcherDefaults"),
	"launchers.default.server_url":              fileOnly(reasonLogins),
	"launchers.index_root":                      fileOnly(reasonRoots),
	"launchers.media_dir":                       fileOnly(reasonWrites),
	"launchers.on_media_start":                  adminField("launchersOnMediaStart"),
	"launchers.preference":                      settingsField("launcherPreference"),
	"library.sync":                              settingsField("librarySyncEnabled"),
	"mappings.entry.match_pattern":              otherMethod(models.MethodMappingsNew),
	"mappings.entry.token_key":                  otherMethod(models.MethodMappingsNew),
	"mappings.entry.zapscript":                  otherMethod(models.MethodMappingsNew),
	"media.default_langs":                       adminField("mediaDefaultLangs"),
	"media.default_regions":                     adminField("mediaDefaultRegions"),
	"media.filename_tags":                       adminField("mediaFilenameTags"),
	"media.image_dir":                           settingsField("mediaImageDir"),
	"media.thumb_cache_max_mb":                  settingsField("mediaThumbCacheMaxMb"),
	"playtime.limits.daily":                     otherMethod(models.MethodPlaytimeLimitsUpdate),
	"playtime.limits.enabled":                   otherMethod(models.MethodPlaytimeLimitsUpdate),
	"playtime.limits.session":                   otherMethod(models.MethodPlaytimeLimitsUpdate),
	"playtime.limits.session_reset":             otherMethod(models.MethodPlaytimeLimitsUpdate),
	"playtime.limits.warnings":                  otherMethod(models.MethodPlaytimeLimitsUpdate),
	"playtime.retention":                        otherMethod(models.MethodPlaytimeLimitsUpdate),
	"playtime.sync":                             settingsField("playtimeSyncEnabled"),
	"profiles.require_for_launch":               settingsField("profilesRequireForLaunch"),
	"profiles.swap_data":                        settingsField("profilesSwapData"),
	"readers.auto_detect":                       settingsField("readersAutoDetect"),
	"readers.connect.driver":                    settingsField("readersConnect"),
	"readers.connect.enabled":                   settingsField("readersConnect"),
	"readers.connect.id_source":                 settingsField("readersConnect"),
	"readers.connect.path":                      settingsField("readersConnect"),
	"readers.connect.scan_mode":                 settingsField("readersConnect"),
	"readers.drivers.audio_cd_launcher":         adminField("readersDrivers"),
	"readers.drivers.auto_detect":               adminField("readersDrivers"),
	"readers.drivers.enabled":                   adminField("readersDrivers"),
	"readers.drivers.keep_reading":              adminField("readersDrivers"),
	"readers.drivers.launch_order":              adminField("readersDrivers"),
	"readers.drivers.scan_mode":                 adminField("readersDrivers"),
	"readers.scan.allow_relaunch":               adminField("readersScanAllowRelaunch"),
	"readers.scan.exit_delay":                   settingsField("readersScanExitDelay"),
	"readers.scan.ignore_on_connect":            adminField("readersScanIgnoreOnConnect"),
	"readers.scan.ignore_system":                settingsField("readersScanIgnoreSystems"),
	"readers.scan.launch_guard.delay":           settingsField("launchGuardDelay"),
	"readers.scan.launch_guard.enabled":         settingsField("launchGuardEnabled"),
	"readers.scan.launch_guard.require_confirm": settingsField("launchGuardRequireConfirm"),
	"readers.scan.launch_guard.timeout":         settingsField("launchGuardTimeout"),
	"readers.scan.mode":                         settingsField("readersScanMode"),
	"readers.scan.on_remove":                    adminField("readersScanOnRemove"),
	"readers.scan.on_scan":                      adminField("readersScanOnScan"),
	"readers.scan_history":                      adminField("readersScanHistory"),

	"scraper.gamelist_xml.custom_path":     fileOnly(reasonRoots),
	"scraper.libretro_thumbnails.base_url": fileOnly("Core fetches images from this host"),

	"service.allow_run":                    fileOnly(reasonAllowList),
	"service.allowed_ips":                  fileOnly(reasonAllowList),
	"service.allowed_origins":              fileOnly(reasonAllowList),
	"service.api_listen":                   adminField("apiListen"),
	"service.api_port":                     adminField("apiPort"),
	"service.ble.enabled":                  settingsField("bleEnabled"),
	"service.ble.name":                     adminField("bleName"),
	"service.device_id":                    adminField("deviceId"),
	"service.discovery.enabled":            adminField("discoveryEnabled"),
	"service.discovery.instance_name":      adminField("discoveryInstanceName"),
	"service.encryption":                   settingsField("encryption"),
	"service.internet_check":               adminField("internetCheck"),
	"service.on_boot":                      adminField("serviceOnBoot"),
	"service.on_ready":                     adminField("serviceOnReady"),
	"service.online_base_url":              settingsField("onlineBaseUrl"),
	"service.publishers.mqtt.broker":       fileOnly(reasonHost),
	"service.publishers.mqtt.enabled":      adminField("publishersMqtt"),
	"service.publishers.mqtt.filter":       adminField("publishersMqtt"),
	"service.publishers.mqtt.topic":        fileOnly(reasonHost),
	"service.publishers.pixelcade.enabled": adminField("publishersPixelcade"),
	"service.publishers.pixelcade.filter":  adminField("publishersPixelcade"),
	"service.publishers.pixelcade.host":    fileOnly(reasonHost),
	"service.publishers.pixelcade.mode":    adminField("publishersPixelcade"),
	"service.publishers.pixelcade.port":    fileOnly(reasonHost),
	"service.remote_control.enabled":       settingsField("remoteControlEnabled"),

	"systems.category.name":           adminField("systemCategories"),
	"systems.category.systems":        adminField("systemCategories"),
	"systems.default.before_exit":     settingsField("systemDefaults"),
	"systems.default.launcher":        settingsField("systemDefaults"),
	"systems.default.pause_on_launch": settingsField("systemDefaults"),
	"systems.default.system":          settingsField("systemDefaults"),

	"updates.channel": settingsField("updateChannel"),
	"updates.check":   settingsField("updateCheck"),
	"updates.install": settingsField("updateInstall"),

	"zapscript.allow_execute":  fileOnly(reasonAllowList),
	"zapscript.allow_http":     fileOnly(reasonAllowList),
	"zapscript.block_commands": fileOnly(reasonInput),
	"zapscript.input.allow":    fileOnly(reasonInput),
	"zapscript.input.block":    fileOnly(reasonInput),
	"zapscript.input.mode":     fileOnly(reasonInput),
}

// configTOMLKeys lists the dotted key of every leaf in t, following tables
// and arrays of tables.
func configTOMLKeys(t reflect.Type, prefix string) []string {
	t = derefType(t)
	var keys []string
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			keys = append(keys, configTOMLKeys(f.Type, prefix)...)
			continue
		}
		if name == "" {
			name = f.Name
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		ft := derefType(f.Type)
		if ft.Kind() == reflect.Struct && ft != reflect.TypeFor[time.Time]() {
			keys = append(keys, configTOMLKeys(ft, key)...)
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice ||
		t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	return t
}

// jsonFieldNames lists the JSON names of t's own fields, skipping embedded
// structs.
func jsonFieldNames(t reflect.Type) []string {
	var names []string
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}

func TestEveryConfigKeyIsClassified(t *testing.T) {
	t.Parallel()

	keys := configTOMLKeys(reflect.TypeFor[config.Values](), "")
	settingsFields := jsonFieldNames(reflect.TypeFor[models.SettingsResponse]())
	adminFields := jsonFieldNames(reflect.TypeFor[models.AdminSettings]())
	adminParams := jsonFieldNames(reflect.TypeFor[models.AdminSettingsParams]())

	for _, key := range keys {
		class, ok := configKeyAPI[key]
		if !assert.Truef(t, ok, "config key %q is not in configKeyAPI: expose it through "+
			"settings or give it a fileOnly reason", key) {
			continue
		}
		switch v := class.(type) {
		case settingsField:
			assert.Containsf(t, settingsFields, string(v), "%s: settings has no field %q", key, v)
		case adminField:
			assert.Containsf(t, adminFields, string(v), "%s: AdminSettings has no field %q", key, v)
			if !slices.Contains([]string{"deviceId", "apiListen", "apiPort"}, string(v)) {
				assert.Containsf(t, adminParams, string(v),
					"%s: settings.update cannot write admin field %q", key, v)
			}
		case otherMethod:
			assert.NotEmptyf(t, string(v), "%s: no method named", key)
		case fileOnly:
			assert.NotEmptyf(t, string(v), "%s: file-only needs a reason", key)
		default:
			t.Errorf("%s: unknown classification %T", key, class)
		}
	}

	for key := range configKeyAPI {
		assert.Containsf(t, keys, key, "configKeyAPI lists %q, which is not a config key", key)
	}
}
