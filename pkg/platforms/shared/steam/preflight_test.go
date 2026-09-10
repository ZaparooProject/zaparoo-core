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

package steam

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaunchInstallationPreflight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		id            string
		action        string
		defaultAction string
		wantAction    string
		installed     bool
		nilOptions    bool
	}{
		{name: "absent", id: "730", wantAction: "details"},
		{name: "nil options", id: "730", nilOptions: true, wantAction: "details"},
		{name: "installed", id: "730", installed: true},
		{name: "explicit run", id: "730", action: "run", wantAction: "run"},
		{name: "explicit details", id: "730", action: "DETAILS", installed: true, wantAction: "DETAILS"},
		{name: "configured run", id: "730", defaultAction: "run", wantAction: "run"},
		{name: "configured details", id: "730", defaultAction: "details", installed: true, wantAction: "details"},
		{name: "override configured details", id: "730", action: "run", defaultAction: "details", wantAction: "run"},
		{name: "shortcut", id: "14663603771387461632"},
		{name: "shortcut details", id: "14663603771387461632", action: "details", wantAction: "details"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := testhelpers.NewMemoryFS()
			root := filepath.Join(t.TempDir(), "Steam")
			require.NoError(t, fs.Fs.MkdirAll(root, 0o750))
			if tt.installed {
				require.NoError(t, fs.WriteFile(filepath.Join(root, "steamapps", "appmanifest_730.acf"),
					[]byte(`"AppState" { "appid" "730" "StateFlags" "4" "installdir" "Game" }`), 0o600))
				require.NoError(t, fs.Fs.MkdirAll(filepath.Join(root, "steamapps", "common", "Game"), 0o750))
			}
			cfg := &config.Instance{}
			require.NoError(t, cfg.LoadTOML(fmt.Sprintf(`
[[launchers.default]]
launcher = "Steam"
install_dir = %q
action = %q
`, root, tt.defaultAction)))
			mockCmd := testhelpers.NewMockCommandExecutor()
			client := NewClientWithExecutor(Options{}, mockCmd)
			client.fs = fs.Fs
			opts := &platforms.LaunchOptions{Action: tt.action}
			if tt.nilOptions {
				opts = nil
			}

			proc, err := client.Launch(cfg, "steam://"+tt.id+"/Game", opts)

			require.NoError(t, err)
			assert.Nil(t, proc)
			wantURL := BuildSteamURL(tt.id)
			if platforms.IsActionDetails(tt.wantAction) {
				wantURL = BuildSteamDetailsURL(tt.id)
			}
			require.Len(t, mockCmd.Calls, 1)
			call := mockCmd.Calls[0]
			args, ok := call.Arguments.Get(len(call.Arguments) - 1).([]string)
			require.True(t, ok)
			require.NotEmpty(t, args)
			assert.Equal(t, wantURL, args[len(args)-1])
			if opts != nil {
				assert.Equal(t, tt.wantAction, opts.Action)
			}
		})
	}
}

// Preflight runs before Core stops the running media, so a scan for an
// uninstalled app opens its store page and leaves the current game alone.
// Verified on the Windows test box: scanning steam://440 while FTL was
// running used to kill FTL and clear ActiveMedia.
func TestSteamPreflightDetailsDoesNotPublishActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	// Registered so an unexpected call is counted rather than panicking.
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Maybe()
	client := NewClientWithExecutor(Options{}, testhelpers.NewMockCommandExecutor())
	client.fs = testhelpers.NewMemoryFS().Fs
	launcher := NewSteamLauncher(Options{})
	launcher.Launch = client.Launch
	params := &platforms.LaunchParams{
		Platform: mockPlatform,
		Config:   &config.Instance{},
		Launcher: &launcher,
		Path:     "steam://730/Game",
		SetActiveMedia: func(*models.ActiveMedia) {
			t.Error("opening details must not publish active media")
		},
	}

	require.NoError(t, platforms.DoLaunch(params, func(string) string { return "Game" }))
	assert.Equal(t, "details", params.Options.Action)
	mockPlatform.AssertNumberOfCalls(t, "StopActiveLauncher", 0)
	mockPlatform.AssertExpectations(t)
}
