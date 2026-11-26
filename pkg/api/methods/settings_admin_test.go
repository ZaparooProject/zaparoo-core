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
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	corehelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/hostfiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type adminClient struct {
	name     string
	role     string
	platform string
	local    bool
}

var (
	clientLocal = adminClient{name: "local", local: true}
	clientAdmin = adminClient{name: "admin", role: string(permissions.RoleAdmin)}
	// A legacy client on MiSTer holds settings.write, so a refusal comes
	// from the admin check and not the capability check.
	clientLegacy = adminClient{name: "legacy", platform: platformids.Mister}
	clientMember = adminClient{name: "member", role: string(permissions.RoleMember)}
)

// adminSettingsEnv builds a settings request from client c, with a launcher
// cache, a data folder and a no-op player.
func adminSettingsEnv(t *testing.T, cfg *config.Instance, c adminClient, params string) requests.RequestEnv {
	t.Helper()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("ManagedByPackageManager").Return(false).Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: t.TempDir(), HostManagedPaths: true}).Maybe()
	mockPlatform.On("RootDirs", cfg).Return([]string(nil)).Maybe()
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{
		{ID: "retroarch", SystemID: "Genesis", Groups: []string{"libretro"}},
		{ID: "snes9x", SystemID: "SNES"},
	}).Maybe()
	mockPlatform.On("RefreshLaunchers", cfg).Return(nil).Maybe()

	appState, ns := state.NewState(mockPlatform, "test-boot-uuid")
	t.Cleanup(func() { drainCh(ns) })

	cache := &corehelpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "retroarch", SystemID: "Genesis", Groups: []string{"libretro"}},
		{ID: "snes9x", SystemID: "SNES"},
	})

	player := mocks.NewMockPlayer()
	player.SetupNoOpMock()

	return requests.RequestEnv{
		Context:       context.Background(),
		Platform:      mockPlatform,
		Config:        cfg,
		State:         appState,
		LauncherCache: cache,
		Player:        player,
		Params:        json.RawMessage(params),
		IsLocal:       c.local,
		ClientRole:    c.role,
		PlatformID:    c.platform,
	}
}

func newAdminTestConfig(t *testing.T, vals *config.Values) *config.Instance {
	t.Helper()
	cfg, _ := newAdminTestConfigFile(t, vals)
	return cfg
}

// newAdminTestConfigFile also returns the path of the saved config file.
func newAdminTestConfigFile(t *testing.T, vals *config.Values) (cfg *config.Instance, path string) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.NewConfig(dir, *vals)
	require.NoError(t, err)
	return cfg, filepath.Join(dir, config.CfgFile)
}

func updateResult(t *testing.T, result any) models.UpdateSettingsResult {
	t.Helper()
	res, ok := result.(models.UpdateSettingsResult)
	require.True(t, ok, "settings.update returns UpdateSettingsResult, got %T", result)
	return res
}

func TestHandleSettingsUpdate_AdminFieldsRefusedForOtherClients(t *testing.T) {
	t.Parallel()

	// One request per admin field, each with a change any client may make,
	// so a refusal must leave the whole request unapplied.
	fields := []string{
		`"audioSuccessSound":{"mode":"off"}`,
		`"inputGamepadEnabled":true`,
		`"inputGamepadEnabled":null`,
		`"readersScanIgnoreOnConnect":true`,
		`"readersScanAllowRelaunch":true`,
		`"readersScanOnScan":"**echo:x"`,
		`"readersScanOnRemove":"**echo:x"`,
		`"readersScanHistory":5`,
		`"readersDrivers":[{"id":"pn532uart","enabled":false}]`,
		`"launchersBeforeMediaStart":"**echo:x"`,
		`"launchersOnMediaStart":"**echo:x"`,
		`"launchersBeforeExit":"**echo:x"`,
		`"launcherDefaults":[{"launcher":"retroarch"}]`,
		`"systemCategories":[{"name":"Mine","systems":["SNES"]}]`,
		`"mediaFilenameTags":false`,
		`"mediaDefaultRegions":["jp"]`,
		`"mediaDefaultLangs":["ja"]`,
		`"backupScope":"zaparoo"`,
		`"discoveryEnabled":false`,
		`"discoveryInstanceName":"Den"`,
		`"bleName":"Den"`,
		`"internetCheck":false`,
		`"serviceOnBoot":"**echo:x"`,
		`"serviceOnReady":"**echo:x"`,
		`"publishersMqtt":[{"index":0,"enabled":false}]`,
		`"publishersPixelcade":[{"index":0,"enabled":false}]`,
	}

	for _, c := range []adminClient{clientLegacy, clientMember} {
		for _, field := range fields {
			cfg, cfgFile := newAdminTestConfigFile(t, &config.Values{})
			before, err := os.ReadFile(cfgFile) //nolint:gosec // test path under t.TempDir
			require.NoError(t, err)

			env := adminSettingsEnv(t, cfg, c, `{"debugLogging":true,`+field+`}`)
			_, err = HandleSettingsUpdate(env)
			require.ErrorIs(t, err, ErrForbidden, "%s client sending %s", c.name, field)
			assert.False(t, cfg.DebugLogging(), "%s: a refused request changes nothing", field)

			after, err := os.ReadFile(cfgFile) //nolint:gosec // test path under t.TempDir
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after), "%s: a refused request saves nothing", field)
		}
	}
}

func TestHandleSettings_AdminFieldsOnlyForLocalOrAdmin(t *testing.T) {
	t.Parallel()

	for _, c := range []adminClient{clientLocal, clientAdmin, clientLegacy, clientMember} {
		cfg := newAdminTestConfig(t, &config.Values{})
		result, err := HandleSettings(adminSettingsEnv(t, cfg, c, `{}`))
		require.NoError(t, err)
		resp, ok := result.(models.SettingsResponse)
		require.True(t, ok)
		if c.local || c.role == string(permissions.RoleAdmin) {
			require.NotNil(t, resp.AdminSettings, c.name)
		} else {
			assert.Nil(t, resp.AdminSettings, c.name)
			raw, err := json.Marshal(resp)
			require.NoError(t, err)
			assert.NotContains(t, string(raw), "readersScanOnScan", c.name)
		}
	}
}

func TestHandleSettingsUpdate_AdminFieldsApplyAndReport(t *testing.T) {
	t.Parallel()

	cfg, cfgFile := newAdminTestConfigFile(t, &config.Values{
		Service: config.Service{Publishers: config.Publishers{
			MQTT: []config.MQTTPublisher{{ //nolint:gosec // fake credentials
				Broker: "tcp://user:secret@broker:1883",
				Topic:  "zaparoo",
			}},
			PixelCade: []config.PixelCadePublisher{{
				Host: "pixelcade.local", Port: 8080, Mode: "stream",
			}},
		}},
	})
	cfg.RecordGamepadAtStart(false, false)

	params := `{
		"audioFailSound":{"mode":"off"},
		"inputGamepadEnabled":true,
		"readersScanIgnoreOnConnect":true,
		"readersScanAllowRelaunch":true,
		"readersScanOnScan":"**echo:scan",
		"readersScanHistory":7,
		"readersDrivers":[{"id":"pn532uart","enabled":false,"scanMode":"HOLD"}],
		"launchersOnMediaStart":"**echo:start",
		"systemCategories":[{"name":"Mine","systems":["SNES"]}],
		"mediaFilenameTags":false,
		"mediaDefaultRegions":["JP","eu"],
		"backupScope":"zaparoo",
		"discoveryInstanceName":"Den",
		"bleName":"Den BLE",
		"internetCheck":false,
		"publishersMqtt":[{"index":0,"filter":["media.started"]}],
		"publishersPixelcade":[{"index":0,"enabled":false,"mode":"write"}]
	}`
	result, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientAdmin, params))
	require.NoError(t, err)

	res := updateResult(t, result)
	assert.Equal(t, []string{models.SettingNeedsRestart}, res.Applied["inputGamepadEnabled"])
	assert.Equal(t, []string{models.SettingNeedsReindex}, res.Applied["mediaFilenameTags"])
	assert.Equal(t, []string{models.SettingNeedsRescrape}, res.Applied["mediaDefaultRegions"])
	assert.Empty(t, res.Applied["readersScanOnScan"])
	assert.Contains(t, res.Applied, "readersScanOnScan")
	assert.Contains(t, res.Applied, "publishersPixelcade")
	assert.Len(t, res.Applied, 17)

	assert.Equal(t, new(""), cfg.SoundSetting(config.SoundFail))
	assert.True(t, cfg.ScanIgnoreOnConnect())
	assert.True(t, cfg.ReadersScan().AllowRelaunch)
	assert.Equal(t, "**echo:scan", cfg.ReadersScan().OnScan)
	assert.Equal(t, 7, cfg.ScanHistory())
	drivers := cfg.ReaderDrivers()
	require.Contains(t, drivers, "pn532uart")
	assert.Equal(t, new(false), drivers["pn532uart"].Enabled)
	assert.Equal(t, config.ScanModeHold, drivers["pn532uart"].ScanMode)
	assert.Equal(t, "**echo:start", cfg.LaunchersOnMediaStart())
	assert.Equal(t, []config.SystemsCategory{{Name: "Mine", Systems: []string{"SNES"}}}, cfg.SystemCategories())
	assert.False(t, cfg.FilenameTags())
	assert.Equal(t, []string{"jp", "eu"}, cfg.DefaultRegions())
	assert.Equal(t, "zaparoo", cfg.BackupScope())
	assert.Equal(t, "Den", cfg.DiscoveryInstanceName())
	assert.Equal(t, "Den BLE", cfg.BLEName())
	assert.False(t, cfg.InternetCheckEnabled())

	mqtt := cfg.GetMQTTPublishers()
	require.Len(t, mqtt, 1)
	assert.Equal(t, "tcp://user:secret@broker:1883", mqtt[0].Broker, "the broker is never written")
	assert.Equal(t, "zaparoo", mqtt[0].Topic)
	assert.Equal(t, []string{"media.started"}, mqtt[0].Filter)
	pixel := cfg.GetPixelCadePublishers()
	require.Len(t, pixel, 1)
	assert.Equal(t, "pixelcade.local", pixel[0].Host)
	assert.Equal(t, 8080, pixel[0].Port)
	assert.Equal(t, "write", pixel[0].Mode)
	assert.Equal(t, new(false), pixel[0].Enabled)

	// What was written is what a fresh load reads back.
	reloaded, reloadedFile := newAdminTestConfigFile(t, &config.Values{})
	data, err := os.ReadFile(cfgFile) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(reloadedFile, data, 0o600)) //nolint:gosec // test path under t.TempDir
	require.NoError(t, reloaded.Load())
	assert.Equal(t, "**echo:scan", reloaded.ReadersScan().OnScan)
	assert.Equal(t, "Den", reloaded.DiscoveryInstanceName())

	settings, err := HandleSettings(adminSettingsEnv(t, cfg, clientAdmin, `{}`))
	require.NoError(t, err)
	resp, ok := settings.(models.SettingsResponse)
	require.True(t, ok)
	require.NotNil(t, resp.AdminSettings)
	assert.Equal(t, []string{"inputGamepadEnabled"}, resp.RestartRequired)
	require.Len(t, resp.PublishersMQTT, 1)
	assert.Equal(t, "tcp://broker:1883", resp.PublishersMQTT[0].Broker, "credentials are not shown")
	assert.Equal(t, models.SoundSetting{Mode: models.SoundModeOff}, resp.AudioFailSound)
	assert.Equal(t, models.SoundSetting{Mode: models.SoundModeDefault}, resp.AudioSuccessSound)
}

func TestHandleSettingsUpdate_GamepadNullRestoresPlatformDefault(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{})
	cfg.RecordGamepadAtStart(true, true)

	result, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal, `{"inputGamepadEnabled":false}`))
	require.NoError(t, err)
	assert.Equal(t, []string{models.SettingNeedsRestart}, updateResult(t, result).Applied["inputGamepadEnabled"])
	assert.Equal(t, new(false), cfg.VirtualGamepadSetting())

	result, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal, `{"inputGamepadEnabled":null}`))
	require.NoError(t, err)
	assert.Nil(t, cfg.VirtualGamepadSetting(), "null goes back to the platform default")
	assert.Equal(t, []string{}, updateResult(t, result).Applied["inputGamepadEnabled"],
		"null is reported as applied, and the default is what Core started with")
	assert.Empty(t, cfg.RestartRequired())

	result, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal, `{"debugLogging":true}`))
	require.NoError(t, err)
	assert.NotContains(t, updateResult(t, result).Applied, "inputGamepadEnabled", "a missing key changes nothing")
	assert.Nil(t, cfg.VirtualGamepadSetting())
}

func TestHandleSettingsUpdate_ResultForNonAdminFields(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{})
	result, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLegacy, `{"debugLogging":true}`))
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"debugLogging": {}}, updateResult(t, result).Applied)
}

func TestHandleSettingsUpdate_NeedsOnlyOnChange(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{})
	cfg.RecordGamepadAtStart(false, false)
	defaults := `"mediaDefaultRegions":["us","world"],"mediaFilenameTags":true,"inputGamepadEnabled":false`
	result, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal, `{`+defaults+`}`))
	require.NoError(t, err)
	for field, needs := range updateResult(t, result).Applied {
		assert.Empty(t, needs, "%s set to its current value needs nothing", field)
	}

	// An empty list restores the built-in default.
	cfg.SetDefaultRegions([]string{"jp"})
	require.NoError(t, cfg.Save())
	result, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal, `{"mediaDefaultRegions":[]}`))
	require.NoError(t, err)
	assert.Equal(t, []string{models.SettingNeedsRescrape}, updateResult(t, result).Applied["mediaDefaultRegions"])
	assert.Equal(t, []string{"us", "world"}, cfg.DefaultRegions())
}

func TestHandleSettingsUpdate_AdminValidation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	textFile := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(textFile, []byte("x"), 0o600))

	for _, tt := range []struct {
		name   string
		params string
		want   string
	}{
		{name: "sound file needs a path", params: `{"audioFailSound":{"mode":"file"}}`, want: "needs a path"},
		{
			name: "default sound takes no file", params: `{"audioFailSound":{"mode":"default","path":"/x.wav"}}`,
			want: "takes no file",
		},
		{
			name: "relative sound path", params: `{"audioFailSound":{"mode":"file","path":"x.wav"}}`,
			want: "must be absolute",
		},
		{name: "missing sound file", params: `{"audioFailSound":{"mode":"file","path":"` +
			filepath.ToSlash(filepath.Join(dir, "missing.wav")) + `"}}`, want: "file not found"},
		{name: "sound not audio", params: `{"audioFailSound":{"mode":"file","path":"` +
			filepath.ToSlash(textFile) + `"}}`, want: "not an audio file"},
		{
			name: "discovery name too long", params: `{"discoveryInstanceName":"` + strings.Repeat("é", 32) + `"}`,
			want: "at most 63 bytes",
		},
		{name: "ble name too long", params: `{"bleName":"` + strings.Repeat("a", 30) + `"}`},
		{name: "name with control character", params: `{"bleName":"a\u0007b"}`, want: "control characters"},
		{name: "name with surrounding space", params: `{"bleName":" Den"}`, want: "start or end with a space"},
		{name: "unknown region", params: `{"mediaDefaultRegions":["moon"]}`, want: "unknown value"},
		{name: "region twice", params: `{"mediaDefaultRegions":["us","US"]}`, want: "listed twice"},
		{name: "unknown language", params: `{"mediaDefaultLangs":["klingon"]}`, want: "unknown value"},
		{
			name: "driver twice", params: `{"readersDrivers":[{"id":"pn532uart"},{"id":"pn532uart"}]}`,
			want: "listed twice",
		},
		{name: "driver bad id", params: `{"readersDrivers":[{"id":"../x"}]}`, want: "invalid driver id"},
		{
			name: "driver bad scan mode", params: `{"readersDrivers":[{"id":"pn532uart","scanMode":"slap"}]}`,
			want: "scanMode",
		},
		{
			name:   "driver unknown audio cd launcher",
			params: `{"readersDrivers":[{"id":"optical","audioCdLauncher":"nope"}]}`, want: "unknown launcher",
		},
		{
			name: "load path absolute", params: `{"launcherDefaults":[{"launcher":"retroarch","loadPath":"/x.so"}]}`,
			want: "invalid loadPath",
		},
		{
			name: "load path parent", params: `{"launcherDefaults":[{"launcher":"retroarch","loadPath":"a/../../x"}]}`,
			want: "invalid loadPath",
		},
		{
			name: "load path backslash", params: `{"launcherDefaults":[{"launcher":"retroarch","loadPath":"a\\x"}]}`,
			want: "invalid loadPath",
		},
		{
			name: "unknown launcher default", params: `{"launcherDefaults":[{"launcher":"nope"}]}`,
			want: "unknown launcher",
		},
		{name: "category unknown system", params: `{"systemCategories":[{"name":"Mine","systems":["Nope"]}]}`},
		{
			name: "publisher index out of range", params: `{"publishersMqtt":[{"index":0,"enabled":true}]}`,
			want: "no publisher at index 0",
		},
		{name: "scan history negative", params: `{"readersScanHistory":-1}`},
		{name: "backup scope unknown", params: `{"backupScope":"everything"}`},
	} {
		cfg, cfgFile := newAdminTestConfigFile(t, &config.Values{})
		before, err := os.ReadFile(cfgFile) //nolint:gosec // test path under t.TempDir
		require.NoError(t, err)

		_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal, tt.params))
		require.Error(t, err, tt.name)
		var clientErr *models.ClientError
		require.ErrorAs(t, err, &clientErr, "%s: refused as a client error", tt.name)
		if tt.want != "" {
			assert.Contains(t, err.Error(), tt.want, tt.name)
		}

		after, err := os.ReadFile(cfgFile) //nolint:gosec // test path under t.TempDir
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "%s: nothing is saved", tt.name)
	}
}

func TestHandleSettingsUpdate_PixelcadeModeOnlyForPixelcade(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{
		Service: config.Service{Publishers: config.Publishers{
			MQTT: []config.MQTTPublisher{{Broker: "broker:1883", Topic: "t"}},
		}},
	})
	_, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal,
		`{"publishersMqtt":[{"index":0,"mode":"write"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mode is not a setting")
}

func TestHandleSettingsUpdate_SoundFromAbsolutePath(t *testing.T) {
	t.Parallel()

	sound := filepath.Join(t.TempDir(), "Beep.WAV")
	require.NoError(t, os.WriteFile(sound, []byte("RIFF"), 0o600))

	cfg := newAdminTestConfig(t, &config.Values{})
	env := adminSettingsEnv(t, cfg, clientLocal,
		`{"audioSuccessSound":{"mode":"file","path":`+jsonString(t, sound)+`}}`)
	player := mocks.NewMockPlayer()
	player.SetupNoOpMock()
	env.Player = player

	_, err := HandleSettingsUpdate(env)
	require.NoError(t, err)
	assert.Equal(t, &sound, cfg.SoundSetting(config.SoundSuccess))
	player.AssertCalled(t, "ClearFileCache")

	result, err := HandleSettings(adminSettingsEnv(t, cfg, clientLocal, `{}`))
	require.NoError(t, err)
	resp, ok := result.(models.SettingsResponse)
	require.True(t, ok)
	assert.Equal(t, models.SoundSetting{Mode: models.SoundModeFile, Path: sound}, resp.AudioSuccessSound)
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}

func TestHandleSettingsUpdate_LauncherDefaultsKeepInstallSettings(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{
		Launchers: config.Launchers{Default: []config.LaunchersDefault{
			{ //nolint:gosec // fake credentials
				Launcher:   "retroarch",
				InstallDir: "/opt/ra",
				ServerURL:  "http://user:pw@media:8096",
			},
			{Launcher: "snes9x", InstallDir: "/opt/snes9x"},
		}},
	})

	params := `{"launcherDefaults":[
		{"launcher":"RetroArch","renderScale":2,"scanDuplicates":true},
		{"launcher":"snes9x","beforeExit":"**echo:bye"}
	]}`
	result, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal, params))
	require.NoError(t, err)
	assert.Equal(t, []string{models.SettingNeedsReindex}, updateResult(t, result).Applied["launcherDefaults"])

	defaults := cfg.LauncherDefaults()
	require.Len(t, defaults, 2)
	assert.Equal(t, "/opt/ra", defaults[0].InstallDir)
	assert.Equal(t, "http://user:pw@media:8096", defaults[0].ServerURL)
	assert.Equal(t, new(2), defaults[0].RenderScale)
	assert.Equal(t, "/opt/snes9x", defaults[1].InstallDir)
	assert.Equal(t, "**echo:bye", defaults[1].BeforeExit)

	// installDir and serverUrl are not parameters, so a client sending them
	// cannot change them.
	_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal,
		`{"launcherDefaults":[{"launcher":"retroarch","installDir":"/tmp/evil","serverUrl":"http://evil"}]}`))
	require.NoError(t, err)
	defaults = cfg.LauncherDefaults()
	require.Len(t, defaults, 1)
	assert.Equal(t, "/opt/ra", defaults[0].InstallDir)
	assert.Equal(t, "http://user:pw@media:8096", defaults[0].ServerURL)

	settings, err := HandleSettings(adminSettingsEnv(t, cfg, clientLocal, `{}`))
	require.NoError(t, err)
	resp, ok := settings.(models.SettingsResponse)
	require.True(t, ok)
	require.Len(t, resp.LauncherDefaults, 1)
	assert.Equal(t, "http://media:8096", resp.LauncherDefaults[0].ServerURL, "credentials are not shown")
}

func TestHandleSettingsUpdate_HookChangesNeedLocalOrAdmin(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{
		Systems: config.Systems{Default: []config.SystemsDefault{
			{System: "SNES", BeforeExit: "**echo:old"},
		}},
	})

	// Resending the hook a legacy client already sees is fine; changing it is
	// not.
	same := `{"systemDefaults":[{"system":"SNES","beforeExit":"**echo:old"}]}`
	_, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLegacy, same))
	require.NoError(t, err)

	cleared := `{"systemDefaults":[{"system":"SNES"}]}`
	_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLegacy, cleared))
	require.NoError(t, err, "clearing a hook is allowed")
	assert.Empty(t, cfg.SystemDefaults()[0].BeforeExit)

	for _, c := range []adminClient{clientLegacy, clientMember} {
		changed := `{"systemDefaults":[{"system":"SNES","beforeExit":"**launch.system:menu"}]}`
		_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, c, changed))
		require.ErrorIs(t, err, ErrForbidden, c.name)
		assert.Empty(t, cfg.SystemDefaults()[0].BeforeExit, c.name)
	}

	_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientAdmin,
		`{"systemDefaults":[{"system":"SNES","beforeExit":"**launch.system:menu"}]}`))
	require.NoError(t, err)
	assert.Equal(t, "**launch.system:menu", cfg.SystemDefaults()[0].BeforeExit)
}

func TestHandleSettingsUpdate_RemoteHooksFollowAllowRun(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{
		Service: config.Service{AllowRun: []string{`^\*\*echo:.*$`}},
	})

	_, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientAdmin, `{"readersScanOnScan":"**echo:ok"}`))
	require.NoError(t, err)
	assert.Equal(t, "**echo:ok", cfg.ReadersScan().OnScan)

	for _, params := range []string{
		`{"readersScanOnScan":"**launch.system:menu"}`,
		`{"serviceOnBoot":"**launch.system:menu"}`,
		`{"launcherDefaults":[{"launcher":"retroarch","beforeExit":"**launch.system:menu"}]}`,
		`{"systemDefaults":[{"system":"SNES","beforeExit":"**launch.system:menu"}]}`,
	} {
		_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientAdmin, params))
		require.ErrorIs(t, err, ErrForbidden, params)
		assert.Contains(t, err.Error(), "allow_run", params)
	}
	assert.Equal(t, "**echo:ok", cfg.ReadersScan().OnScan)

	// A local client is not bound by allow_run.
	_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal, `{"readersScanOnScan":"**launch.system:menu"}`))
	require.NoError(t, err)
	assert.Equal(t, "**launch.system:menu", cfg.ReadersScan().OnScan)
}

func TestHandleSettingsUpdate_NotifiesLiveChanges(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{})
	var got []config.ChangeKind
	for _, kind := range []config.ChangeKind{
		config.ChangeDiscovery, config.ChangeBLEName, config.ChangePublishers,
		config.ChangeScanHistory, config.ChangeIgnoreOnConnect,
	} {
		cfg.OnChange(kind, func() { got = append(got, kind) })
	}

	_, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal,
		`{"discoveryEnabled":false,"readersScanHistory":3,"readersScanIgnoreOnConnect":true,"publishersMqtt":[]}`))
	require.NoError(t, err)
	assert.ElementsMatch(t, []config.ChangeKind{
		config.ChangeDiscovery, config.ChangePublishers, config.ChangeScanHistory, config.ChangeIgnoreOnConnect,
	}, got)

	// Setting the same values again changes nothing live.
	got = nil
	_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal,
		`{"discoveryEnabled":false,"readersScanHistory":3,"readersScanIgnoreOnConnect":true,"bleName":""}`))
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLocal, `{"discoveryInstanceName":"Den"}`))
	require.NoError(t, err)
	assert.ElementsMatch(t, []config.ChangeKind{config.ChangeDiscovery, config.ChangeBLEName}, got)
}

func TestHandleSettingsUpdate_SystemDefaultsPauseOnLaunch(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{
		Systems: config.Systems{Default: []config.SystemsDefault{
			{System: "Audio", PauseOnLaunch: new(false)},
		}},
	})

	_, err := HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLegacy, `{"systemDefaults":[{"system":"Audio"}]}`))
	require.NoError(t, err)
	assert.Equal(t, new(false), cfg.SystemDefaults()[0].PauseOnLaunch, "omitted keeps the value")

	_, err = HandleSettingsUpdate(adminSettingsEnv(t, cfg, clientLegacy,
		`{"systemDefaults":[{"system":"Audio","pauseOnLaunch":true}]}`))
	require.NoError(t, err)
	assert.Equal(t, new(true), cfg.SystemDefaults()[0].PauseOnLaunch)

	result, err := HandleSettings(adminSettingsEnv(t, cfg, clientLegacy, `{}`))
	require.NoError(t, err)
	resp, ok := result.(models.SettingsResponse)
	require.True(t, ok)
	require.Len(t, resp.SystemDefaults, 1)
	assert.Equal(t, new(true), resp.SystemDefaults[0].PauseOnLaunch)
}

func TestHandleFilesBrowse_AudioNeedsLocalOrAdmin(t *testing.T) {
	t.Parallel()

	for _, c := range []adminClient{clientLegacy, clientMember} {
		cfg := newAdminTestConfig(t, &config.Values{})
		_, err := HandleFilesBrowse(adminSettingsEnv(t, cfg, c, `{"purpose":"audio"}`))
		require.ErrorIs(t, err, ErrForbidden, c.name)
	}

	cfg := newAdminTestConfig(t, &config.Values{})
	env := adminSettingsEnv(t, cfg, clientAdmin, `{"purpose":"audio"}`)
	require.NoError(t, os.MkdirAll(filepath.Join(corehelpers.DataDir(env.Platform), config.AssetsDir), 0o750))
	result, err := HandleFilesBrowse(env)
	require.NoError(t, err)
	resp, ok := result.(models.FilesBrowseResponse)
	require.True(t, ok)
	require.Len(t, resp.Roots, 1)
	assert.Equal(t, hostfiles.AssetsRootID, resp.Roots[0].ID)
}

func TestHandleLaunchersCustom(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{
		Launchers: config.Launchers{Custom: []config.LaunchersCustom{{
			ID: "MyEmu", System: "SNES", Execute: "myemu [[media_path]]",
			MediaDirs: []string{"/roms/snes"}, FileExts: []string{".sfc"},
		}}},
	})

	for _, c := range []adminClient{clientLegacy, clientMember} {
		_, err := HandleLaunchersCustom(adminSettingsEnv(t, cfg, c, `{}`))
		require.ErrorIs(t, err, ErrForbidden, c.name)
	}

	result, err := HandleLaunchersCustom(adminSettingsEnv(t, cfg, clientAdmin, `{}`))
	require.NoError(t, err)
	resp, ok := result.(models.CustomLaunchersResponse)
	require.True(t, ok)
	require.Len(t, resp.Launchers, 1)
	got := resp.Launchers[0]
	assert.Equal(t, "MyEmu", got.ID)
	assert.Equal(t, "SNES", got.System)
	assert.Equal(t, "myemu [[media_path]]", got.Execute)
	assert.Equal(t, []string{"/roms/snes"}, got.MediaDirs)
	assert.Equal(t, []string{".sfc"}, got.FileExts)
	assert.Empty(t, got.Source, "config.toml entries have no source file")
}

func TestHandleLaunchers_MarksCustom(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{
		Launchers: config.Launchers{Custom: []config.LaunchersCustom{{
			ID: "MyEmu", System: "SNES", Execute: "myemu [[media_path]]",
		}}},
	})
	env := adminSettingsEnv(t, cfg, clientLegacy, `{}`)
	env.LauncherCache.InitializeFromSlice([]platforms.Launcher{
		{ID: "myemu", SystemID: "SNES"},
		{ID: "snes9x", SystemID: "SNES"},
	})

	result, err := HandleLaunchers(env)
	require.NoError(t, err)
	resp, ok := result.(models.LaunchersResponse)
	require.True(t, ok)
	custom := map[string]bool{}
	for _, l := range resp.Launchers {
		custom[l.ID] = l.Custom
	}
	assert.Equal(t, map[string]bool{"myemu": true, "snes9x": false}, custom)
}

func TestHandleSettingsUpdate_SoundFromBrowseRoot(t *testing.T) {
	t.Parallel()

	cfg := newAdminTestConfig(t, &config.Values{})
	env := adminSettingsEnv(t, cfg, clientAdmin,
		`{"audioReadySound":{"mode":"file","root":"assets","path":"chimes/ready.ogg"}}`)
	assets := filepath.Join(corehelpers.DataDir(env.Platform), config.AssetsDir)
	require.NoError(t, os.MkdirAll(filepath.Join(assets, "chimes"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(assets, "chimes", "ready.ogg"), []byte("OggS"), 0o600))
	realAssets, err := filepath.EvalSymlinks(assets)
	require.NoError(t, err)

	_, err = HandleSettingsUpdate(env)
	require.NoError(t, err)
	want := filepath.Join(realAssets, "chimes", "ready.ogg")
	assert.Equal(t, &want, cfg.SoundSetting(config.SoundReady))

	env.Params = json.RawMessage(`{"audioReadySound":{"mode":"file","root":"assets","path":"../../etc/passwd"}}`)
	_, err = HandleSettingsUpdate(env)
	require.Error(t, err, "a path out of the root is refused")
	assert.Equal(t, &want, cfg.SoundSetting(config.SoundReady))
}
