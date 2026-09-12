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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/pinup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowsHasPinUPPopperLauncher(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	defer platform.popperIntegration().Stop()

	var found *platforms.Launcher
	for _, launcher := range platform.Launchers(cfg) {
		if launcher.ID == pinup.LauncherID {
			l := launcher
			found = &l
			break
		}
	}
	require.NotNil(t, found, "PinUPPopper launcher should be registered")
	assert.Equal(t, systemdefs.SystemPinball, found.SystemID)
	assert.Equal(t, []string{shared.SchemePopper}, found.Schemes)
	assert.Equal(t, platforms.LifecycleExternal, found.Lifecycle)
	assert.True(t, found.SkipFilesystemScan)
	assert.NotNil(t, found.Kill, "hold mode needs a stop mechanism")
	assert.NotNil(t, found.Availability)
	assert.NotNil(t, found.Scanner)
}

func TestPinUPPopperScraperRequiresInstallation(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "PinUPSystem")
	integration := pinup.NewIntegration(&pinup.Deps{
		Locator: pinup.Locator{FS: fs, Candidates: []string{dir}},
	})
	t.Cleanup(integration.Stop)
	platform := &Platform{popper: integration}

	assert.NotContains(t, platform.Scrapers(nil), "pinup-popper")

	require.NoError(t, fs.MkdirAll(dir, 0o755))
	for _, name := range []string{"PinUpMenu.exe", "PUPDatabase.db"} {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), nil, 0o600))
	}
	assert.Contains(t, platform.Scrapers(nil), "pinup-popper")

	// Registration must rediscover availability, not retain a stale result.
	require.NoError(t, fs.Remove(filepath.Join(dir, "PUPDatabase.db")))
	assert.NotContains(t, platform.Scrapers(nil), "pinup-popper")
}

func TestPopperIntegrationIsCreatedOnce(t *testing.T) {
	t.Parallel()

	platform := &Platform{}
	first := platform.popperIntegration()
	defer first.Stop()
	assert.Same(t, first, platform.popperIntegration())
}
