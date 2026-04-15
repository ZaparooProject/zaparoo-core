//go:build windows

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

package windows

import (
	"context"
	"os/exec"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowsHasKodiLocalLauncher(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Check for KodiLocalVideo launcher
	var kodiLocal *string
	for _, launcher := range launchers {
		if launcher.ID == "KodiLocalVideo" {
			kodiLocal = &launcher.ID
			assert.Equal(t, systemdefs.SystemVideo, launcher.SystemID)
			assert.Contains(t, launcher.Extensions, ".mp4")
			break
		}
	}

	require.NotNil(t, kodiLocal, "KodiLocalVideo launcher should exist")
}

func TestStopActiveLauncher_CustomKill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		customKillFunc    func(*config.Instance) error
		name              string
		customKillCalled  bool
		hasTrackedProcess bool
	}{
		{
			name: "custom Kill function is called when defined",
			customKillFunc: func(_ *config.Instance) error {
				return nil
			},
			customKillCalled:  true,
			hasTrackedProcess: true,
		},
		{
			name: "custom Kill function error is logged but not fatal",
			customKillFunc: func(_ *config.Instance) error {
				return assert.AnError
			},
			customKillCalled:  true,
			hasTrackedProcess: true,
		},
		{
			name:              "tracked process killed when no custom Kill defined",
			customKillFunc:    nil,
			customKillCalled:  false,
			hasTrackedProcess: true,
		},
		{
			name:              "no kill attempted when no tracked process and no custom Kill",
			customKillFunc:    nil,
			customKillCalled:  false,
			hasTrackedProcess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Platform{}
			p.setActiveMedia = func(_ *models.ActiveMedia) {}

			killCalled := false
			var launcher platforms.Launcher
			if tt.customKillFunc != nil {
				launcher.Kill = func(cfg *config.Instance) error {
					killCalled = true
					return tt.customKillFunc(cfg)
				}
			}
			p.setLastLauncher(&launcher)

			if tt.hasTrackedProcess {
				cmd := exec.CommandContext(context.Background(), "cmd", "/C", "timeout", "/T", "10")
				err := cmd.Start()
				require.NoError(t, err)
				defer func() {
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
				}()
				p.SetTrackedProcess(cmd.Process)
			}

			err := p.StopActiveLauncher(platforms.StopForPreemption)
			require.NoError(t, err)

			assert.Equal(t, tt.customKillCalled, killCalled, "custom Kill called mismatch")
		})
	}
}

func TestWindowsHasAllKodiLaunchers(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Build launcher map for quick lookup
	launcherMap := make(map[string]bool)
	for _, launcher := range launchers {
		launcherMap[launcher.ID] = true
	}

	// Test all Kodi launchers exist (same as Linux platform)
	expectedLaunchers := []string{
		"KodiLocalVideo", "KodiMovie", "KodiTVEpisode", "KodiLocalAudio",
		"KodiSong", "KodiAlbum", "KodiArtist", "KodiTVShow",
	}
	for _, expected := range expectedLaunchers {
		assert.True(t, launcherMap[expected], "%s launcher should exist", expected)
	}
}
