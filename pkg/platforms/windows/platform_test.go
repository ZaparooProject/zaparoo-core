//go:build windows

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
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
