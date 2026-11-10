//go:build linux && !android

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

package muos

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ platforms.Platform = (*Platform)(nil)

func TestSettingsUseSDCard(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)

	s := p.Settings()
	assert.Equal(t, "/mnt/mmc/MUOS/zaparoo", s.DataDir)
	assert.Equal(t, "/mnt/mmc/MUOS/zaparoo", s.ConfigDir)
	assert.Equal(t, "/mnt/mmc/MUOS/zaparoo/"+config.LogsDir, s.LogDir)

	writeFile(t, fs, romMountFile, "/media/card\n")
	assert.Equal(t, "/media/card/MUOS/zaparoo", p.Settings().DataDir)
}

func TestRootDirs(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	fs := newTestFs(t)
	writeFile(t, fs, sdcardMountFile, "/mnt/mmc\n")
	p, _, _ := newTestPlatform(t, fs)

	assert.Equal(t, []string{"/mnt/usb/ROMS", "/mnt/mmc/ROMS"}, p.RootDirs(cfg))
}

func TestTrivialReturns(t *testing.T) {
	t.Parallel()
	p, _, _ := newTestPlatform(t, newTestFs(t))

	assert.Equal(t, platformids.MuOS, p.ID())
	require.ErrorIs(t, p.LaunchSystem(nil, "GBA"), platforms.ErrNotSupported)
	_, err := p.Screenshot()
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	_, err = p.ForwardCmd(nil)
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	_, ok := p.LookupMapping(nil)
	assert.False(t, ok)
	require.NoError(t, p.ScanHook(nil))
	assert.False(t, p.ManagedByPackageManager())
	assert.Equal(t, platforms.NoOpConsoleManager{}, p.ConsoleManager())
	assert.Len(t, p.Scrapers(nil), 4)
	p.SetTrackedProcess(nil)
}

func TestZeroValuePlatformDefaults(t *testing.T) {
	t.Parallel()
	p := &Platform{}
	assert.NotNil(t, p.filesystem())
	assert.NotNil(t, p.getClock())
	assert.NotNil(t, p.cmdExec())
	assert.Equal(t, "/proc", p.procDir())
	assert.Positive(t, p.selfPID())
	assert.NotEmpty(t, p.muArch())
	p.stopTracker()
}

func TestCoreDBReloadsAfterTTL(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	first := p.coreDB()
	assert.Same(t, first, p.coreDB())
	p.dbLoaded = p.dbLoaded.Add(-2 * cacheTTL)
	assert.NotSame(t, first, p.coreDB())
}
