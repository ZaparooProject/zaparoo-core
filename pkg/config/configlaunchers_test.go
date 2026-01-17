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
		name           string
		launcherID     string
		expectedAction string
		defaults       []LaunchersDefault
		expectFound    bool
	}{
		{
			name:        "no defaults configured",
			launcherID:  "Steam",
			defaults:    nil,
			expectFound: false,
		},
		{
			name:       "launcher found with action",
			launcherID: "Steam",
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: "details"},
			},
			expectFound:    true,
			expectedAction: "details",
		},
		{
			name:       "launcher found with empty action",
			launcherID: "Steam",
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: ""},
			},
			expectFound:    true,
			expectedAction: "",
		},
		{
			name:       "launcher not found",
			launcherID: "Epic",
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: "details"},
			},
			expectFound: false,
		},
		{
			name:       "case insensitive match",
			launcherID: "steam",
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: "details"},
			},
			expectFound:    true,
			expectedAction: "details",
		},
		{
			name:       "multiple defaults - finds first match",
			launcherID: "Steam",
			defaults: []LaunchersDefault{
				{Launcher: "GOG", Action: "run"},
				{Launcher: "Steam", Action: "details"},
				{Launcher: "Epic", Action: "run"},
			},
			expectFound:    true,
			expectedAction: "details",
		},
		{
			name:       "launcher found with install_dir",
			launcherID: "Steam",
			defaults: []LaunchersDefault{
				{Launcher: "Steam", Action: "details", InstallDir: "/opt/steam"},
			},
			expectFound:    true,
			expectedAction: "details",
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

			result, found := cfg.LookupLauncherDefaults(tt.launcherID)

			assert.Equal(t, tt.expectFound, found, "found mismatch")
			if tt.expectFound {
				assert.Equal(t, tt.expectedAction, result.Action, "action mismatch")
			}
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
	steamDefault, found := cfg.LookupLauncherDefaults("Steam")
	assert.True(t, found)
	assert.Equal(t, "details", steamDefault.Action)

	// Verify GOG default
	gogDefault, found := cfg.LookupLauncherDefaults("GOG")
	assert.True(t, found)
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
	steamDefault, found := cfg.LookupLauncherDefaults("Steam")
	require.True(t, found, "Steam default should be found")
	assert.Equal(t, "Steam", steamDefault.Launcher)
	assert.Equal(t, "details", steamDefault.Action)
	assert.Empty(t, steamDefault.InstallDir)

	// Verify GOG default without action
	gogDefault, found := cfg.LookupLauncherDefaults("GOG")
	require.True(t, found, "GOG default should be found")
	assert.Equal(t, "GOG", gogDefault.Launcher)
	assert.Empty(t, gogDefault.Action)
	assert.Equal(t, "/opt/gog", gogDefault.InstallDir)

	// Verify Epic default with action and server_url
	epicDefault, found := cfg.LookupLauncherDefaults("Epic")
	require.True(t, found, "Epic default should be found")
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
	steamDefault, found := cfg.LookupLauncherDefaults("Steam")
	require.True(t, found)
	assert.Equal(t, "details", steamDefault.Action)

	gogDefault, found := cfg.LookupLauncherDefaults("GOG")
	require.True(t, found)
	assert.Equal(t, "run", gogDefault.Action)
	assert.Equal(t, "/games/gog", gogDefault.InstallDir)
}
