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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchersBeforeMediaStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		script   string
		expected string
	}{
		{
			name:     "empty script",
			script:   "",
			expected: "",
		},
		{
			name:     "simple script",
			script:   "**echo:before launch",
			expected: "**echo:before launch",
		},
		{
			name:     "execute script",
			script:   "**execute:/usr/bin/notify-send 'Game starting'",
			expected: "**execute:/usr/bin/notify-send 'Game starting'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Launchers: Launchers{
						BeforeMediaStart: tt.script,
					},
				},
			}

			result := cfg.LaunchersBeforeMediaStart()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetExecuteAllowListForTesting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		testCmd   string
		allowList []string
		expected  bool
	}{
		{
			name:      "empty allow list blocks all",
			allowList: []string{},
			testCmd:   "echo hello",
			expected:  false,
		},
		{
			name:      "wildcard allows all",
			allowList: []string{".*"},
			testCmd:   "echo hello",
			expected:  true,
		},
		{
			name:      "specific command allowed",
			allowList: []string{"^echo$"},
			testCmd:   "echo",
			expected:  true,
		},
		{
			name:      "specific command not in list blocked",
			allowList: []string{"^echo$"},
			testCmd:   "rm -rf",
			expected:  false,
		},
		{
			name:      "path pattern matching",
			allowList: []string{"/usr/bin/.*"},
			testCmd:   "/usr/bin/notify-send",
			expected:  true,
		},
		{
			name:      "multiple patterns",
			allowList: []string{"^echo$", "^notify-send$"},
			testCmd:   "notify-send",
			expected:  true,
		},
		{
			name:      "invalid regex is skipped gracefully",
			allowList: []string{"[invalid", "^echo$"},
			testCmd:   "echo",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{}
			cfg.SetExecuteAllowListForTesting(tt.allowList)

			result := cfg.IsExecuteAllowed(tt.testCmd)
			assert.Equal(t, tt.expected, result, "command: %s, allowList: %v", tt.testCmd, tt.allowList)
		})
	}
}

func TestSetExecuteAllowListForTesting_CompilesRegex(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	cfg.SetExecuteAllowListForTesting([]string{"^test.*", "^echo$"})

	// Verify internal state was set correctly
	assert.Len(t, cfg.vals.ZapScript.AllowExecute, 2)
	assert.Len(t, cfg.vals.ZapScript.allowExecuteRe, 2)
	assert.NotNil(t, cfg.vals.ZapScript.allowExecuteRe[0])
	assert.NotNil(t, cfg.vals.ZapScript.allowExecuteRe[1])
}

func TestLookupLauncherDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		launcherID        string
		expectedServerURL string
		expectedAction    string
		expectedInstall   string
		groups            []string
		defaults          []LaunchersDefault
	}{
		{
			name:       "no defaults configured",
			launcherID: "Steam",
			groups:     nil,
			defaults:   nil,
		},
		{
			name:       "launcher found with action",
			launcherID: "Steam",
			groups:     nil,
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: "details"},
			},
			expectedAction: "details",
		},
		{
			name:       "launcher found with empty action",
			launcherID: "Steam",
			groups:     nil,
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: ""},
			},
		},
		{
			name:       "launcher not found returns empty result",
			launcherID: "Epic",
			groups:     nil,
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: "details"},
			},
		},
		{
			name:       "case insensitive match",
			launcherID: "steam",
			groups:     nil,
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: "details"},
			},
			expectedAction: "details",
		},
		{
			name:       "launcher found with install_dir",
			launcherID: "Steam",
			groups:     nil,
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: "details", InstallDir: "/opt/steam"},
			},
			expectedAction:  "details",
			expectedInstall: "/opt/steam",
		},
		{
			name:       "exact launcher ID match",
			launcherID: "KodiTVShow",
			groups:     []string{"Kodi", "KodiTV"},
			defaults: []LaunchersDefault{
				{Launcher: "KodiTVShow", ServerURL: "http://exact:8080", Action: "details"},
			},
			expectedServerURL: "http://exact:8080",
			expectedAction:    "details",
		},
		{
			name:       "group match",
			launcherID: "KodiTVShow",
			groups:     []string{"Kodi", "KodiTV"},
			defaults: []LaunchersDefault{
				{Launcher: "Kodi", ServerURL: "http://group:8080"},
			},
			expectedServerURL: "http://group:8080",
		},
		{
			name:       "case insensitive group match",
			launcherID: "KodiTVShow",
			groups:     []string{"Kodi", "KodiTV"},
			defaults: []LaunchersDefault{
				{Launcher: "kodi", ServerURL: "http://lowercase:8080"},
			},
			expectedServerURL: "http://lowercase:8080",
		},
		{
			name:       "later entries override earlier ones",
			launcherID: "KodiTVShow",
			groups:     []string{"Kodi", "KodiTV"},
			defaults: []LaunchersDefault{
				{Launcher: "Kodi", ServerURL: "http://first:8080", Action: "run"},
				{Launcher: "Kodi", ServerURL: "http://second:8080"},
			},
			expectedServerURL: "http://second:8080",
			expectedAction:    "run", // first entry's action persists since second didn't override
		},
		{
			name:       "hierarchical config - group then specific group",
			launcherID: "KodiTVShow",
			groups:     []string{"Kodi", "KodiTV"},
			defaults: []LaunchersDefault{
				{Launcher: "Kodi", ServerURL: "http://all-kodi:8080"},
				{Launcher: "KodiTV", Action: "details"},
			},
			expectedServerURL: "http://all-kodi:8080",
			expectedAction:    "details",
		},
		{
			name:       "hierarchical config - group then exact ID",
			launcherID: "KodiTVShow",
			groups:     []string{"Kodi", "KodiTV"},
			defaults: []LaunchersDefault{
				{Launcher: "Kodi", ServerURL: "http://all-kodi:8080", Action: "run"},
				{Launcher: "KodiTVShow", Action: "details"},
			},
			expectedServerURL: "http://all-kodi:8080",
			expectedAction:    "details",
		},
		{
			name:       "full hierarchical override chain",
			launcherID: "KodiTVShow",
			groups:     []string{"Kodi", "KodiTV"},
			defaults: []LaunchersDefault{
				{Launcher: "Kodi", ServerURL: "http://base:8080", Action: "run", InstallDir: "/base"},
				{Launcher: "KodiTV", Action: "browse", InstallDir: "/tv"},
				{Launcher: "KodiTVShow", Action: "details"},
			},
			expectedServerURL: "http://base:8080",
			expectedAction:    "details",
			expectedInstall:   "/tv",
		},
		{
			name:       "non-matching entries ignored",
			launcherID: "KodiTVShow",
			groups:     []string{"Kodi", "KodiTV"},
			defaults: []LaunchersDefault{
				{Launcher: "Steam", ServerURL: "http://steam:8080"},
				{Launcher: "Kodi", ServerURL: "http://kodi:8080"},
				{Launcher: "Epic", Action: "run"},
			},
			expectedServerURL: "http://kodi:8080",
		},
		{
			name:       "empty groups only matches exact ID",
			launcherID: "Steam",
			groups:     nil,
			defaults: []LaunchersDefault{
				{Launcher: "Steam", ServerURL: "http://steam:8080"},
				{Launcher: "Kodi", ServerURL: "http://kodi:8080"},
			},
			expectedServerURL: "http://steam:8080",
		},
		{
			name:       "partial field merging",
			launcherID: "KodiMovie",
			groups:     []string{"Kodi"},
			defaults: []LaunchersDefault{
				{Launcher: "Kodi", ServerURL: "http://server:8080"},
				{Launcher: "KodiMovie", Action: "details", InstallDir: "/movies"},
			},
			expectedServerURL: "http://server:8080",
			expectedAction:    "details",
			expectedInstall:   "/movies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Instance{
				vals: Values{
					Launchers: Launchers{
						Default: tt.defaults,
					},
				},
			}

			result := cfg.LookupLauncherDefaults(tt.launcherID, tt.groups)

			assert.Equal(t, tt.launcherID, result.Launcher, "launcher ID should be preserved")
			assert.Equal(t, tt.expectedServerURL, result.ServerURL, "ServerURL mismatch")
			assert.Equal(t, tt.expectedAction, result.Action, "Action mismatch")
			assert.Equal(t, tt.expectedInstall, result.InstallDir, "InstallDir mismatch")
		})
	}
}

func TestSetLauncherDefaultsForTesting(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}

	defaults := []LaunchersDefault{
		{Launcher: "Steam", Action: "details"},
		{Launcher: "GOG", Action: "run", InstallDir: "/opt/gog"},
	}

	cfg.SetLauncherDefaultsForTesting(defaults)

	// Verify defaults were set
	assert.Len(t, cfg.vals.Launchers.Default, 2)

	// Verify Steam default
	steamDefault := cfg.LookupLauncherDefaults("Steam", nil)
	assert.Equal(t, "details", steamDefault.Action)

	// Verify GOG default
	gogDefault := cfg.LookupLauncherDefaults("GOG", nil)
	assert.Equal(t, "run", gogDefault.Action)
	assert.Equal(t, "/opt/gog", gogDefault.InstallDir)
}

func TestLauncherDefaults_TOMLParsing(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, CfgFile)

	// Config file with launcher defaults including action field
	configContent := fmt.Sprintf(`config_schema = %d

[[launchers.default]]
launcher = "Steam"
action = "details"

[[launchers.default]]
launcher = "GOG"
install_dir = "/opt/gog"

[[launchers.default]]
launcher = "Epic"
action = "run"
server_url = "http://localhost:8080"
`, SchemaVersion)

	err := os.WriteFile(cfgPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	cfg := &Instance{
		cfgPath:  cfgPath,
		vals:     Values{ConfigSchema: SchemaVersion},
		defaults: Values{ConfigSchema: SchemaVersion},
	}

	err = cfg.Load()
	require.NoError(t, err)

	// Verify Steam default with action
	steamDefault := cfg.LookupLauncherDefaults("Steam", nil)
	assert.Equal(t, "Steam", steamDefault.Launcher)
	assert.Equal(t, "details", steamDefault.Action)
	assert.Empty(t, steamDefault.InstallDir)

	// Verify GOG default without action
	gogDefault := cfg.LookupLauncherDefaults("GOG", nil)
	assert.Equal(t, "GOG", gogDefault.Launcher)
	assert.Empty(t, gogDefault.Action)
	assert.Equal(t, "/opt/gog", gogDefault.InstallDir)

	// Verify Epic default with action and server_url
	epicDefault := cfg.LookupLauncherDefaults("Epic", nil)
	assert.Equal(t, "Epic", epicDefault.Launcher)
	assert.Equal(t, "run", epicDefault.Action)
	assert.Equal(t, "http://localhost:8080", epicDefault.ServerURL)
}

func TestLauncherDefaults_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create config with BaseDefaults
	cfg, err := NewConfig(tempDir, BaseDefaults)
	require.NoError(t, err)

	// Set launcher defaults using the testing helper
	cfg.SetLauncherDefaultsForTesting([]LaunchersDefault{
		{Launcher: "Steam", Action: "details"},
		{Launcher: "GOG", Action: "run", InstallDir: "/games/gog"},
	})

	// Save and reload
	err = cfg.Save()
	require.NoError(t, err)

	err = cfg.Load()
	require.NoError(t, err)

	// Verify defaults persist after save/load
	steamDefault := cfg.LookupLauncherDefaults("Steam", nil)
	assert.Equal(t, "details", steamDefault.Action)

	gogDefault := cfg.LookupLauncherDefaults("GOG", nil)
	assert.Equal(t, "run", gogDefault.Action)
	assert.Equal(t, "/games/gog", gogDefault.InstallDir)
}
