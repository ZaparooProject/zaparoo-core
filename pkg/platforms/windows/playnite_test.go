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
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/playnite"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowsHasPlayniteLauncher(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	defer platform.popperIntegration().Stop()
	defer platform.playniteIntegration().Stop()

	var found *platforms.Launcher
	for _, launcher := range platform.Launchers(cfg) {
		if launcher.ID == playnite.LauncherID {
			l := launcher
			found = &l
			break
		}
	}
	require.NotNil(t, found, "Playnite launcher should be registered")
	assert.Empty(t, found.SystemID, "Playnite holds games for many systems")
	assert.Equal(t, []string{shared.SchemePlaynite}, found.Schemes)
	assert.Equal(t, platforms.LifecycleExternal, found.Lifecycle)
	assert.NotNil(t, found.Test, "a scheme launcher must reject paths it cannot launch")
	assert.NotNil(t, found.Kill, "hold mode needs a stop mechanism")
	assert.NotNil(t, found.Availability)
	assert.NotNil(t, found.Scanner)
}

func TestPlayniteScraperRequiresInstallation(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "Playnite")
	integration := playnite.NewIntegration(&playnite.Deps{
		Locator: playnite.Locator{FS: fs, Candidates: []string{dir}},
	})
	t.Cleanup(integration.Stop)
	platform := &Platform{playnite: integration}
	t.Cleanup(platform.popperIntegration().Stop)

	assert.NotContains(t, platform.Scrapers(nil), "playnite")

	require.NoError(t, fs.MkdirAll(dir, 0o755))
	exe := filepath.Join(dir, "Playnite.DesktopApp.exe")
	require.NoError(t, afero.WriteFile(fs, exe, nil, 0o600))
	assert.Contains(t, platform.Scrapers(nil), "playnite")

	// Registration must rediscover availability, not retain a stale result.
	require.NoError(t, fs.Remove(exe))
	assert.NotContains(t, platform.Scrapers(nil), "playnite")
}

func TestPlayniteIntegrationIsCreatedOnce(t *testing.T) {
	t.Parallel()

	platform := &Platform{}
	first := platform.playniteIntegration()
	defer first.Stop()
	assert.Same(t, first, platform.playniteIntegration())
}

func TestPlayniteProcessMatches(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	require.NoError(t, err)
	pid := os.Getpid()

	assert.True(t, playniteProcessMatches(pid, self))
	assert.False(t, playniteProcessMatches(pid, filepath.Join(filepath.Dir(self), "other.exe")),
		"a PID now running another program must not be adopted")
	assert.False(t, playniteProcessMatches(pid, ""), "a PID with no image to check is not trusted")
	assert.False(t, playniteProcessMatches(0, self))
}

func TestTrackPlayniteProcessIgnoresMismatch(t *testing.T) {
	t.Parallel()

	platform := &Platform{}
	platform.trackPlayniteProcess(os.Getpid(), filepath.Join(t.TempDir(), "other.exe"))
	platform.trackPlayniteProcess(0, "")

	platform.processMu.RLock()
	defer platform.processMu.RUnlock()
	assert.Nil(t, platform.trackedProcess)
}
