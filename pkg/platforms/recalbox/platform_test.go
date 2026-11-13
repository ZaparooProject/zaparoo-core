//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlatform(t *testing.T) {
	t.Parallel()
	p := NewPlatform()
	assert.Equal(t, platformids.Recalbox, p.ID())
	assert.Equal(t, esCommandAddr, p.commandAddr)
	assert.NotNil(t, p.subscribe)
	assert.NotNil(t, p.signal)
}

func TestSettings(t *testing.T) {
	t.Parallel()
	s := (&Platform{}).Settings()
	assert.Equal(t, InstallDir, s.DataDir)
	assert.Equal(t, InstallDir, s.ConfigDir)
	assert.Equal(t, filepath.Join(InstallDir, config.LogsDir), s.LogDir)
	assert.Equal(t, filepath.Join(os.TempDir(), config.AppName), s.TempDir)

	var p platforms.Platform = &Platform{}
	provider, ok := p.(platforms.ServiceBinaryDirProvider)
	require.True(t, ok, "the share cannot execute files, so the service needs its own binary dir")
	assert.Equal(t, ServiceBinaryDir, provider.ServiceBinaryDir())
}

func TestRootDirs(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	p, _, _ := newTestPlatform(t, newTestFs(t))
	assert.Equal(t, []string{romsRoot}, p.RootDirs(cfg))

	p, _, _ = newTestPlatform(t, afero.NewMemMapFs())
	assert.Empty(t, p.RootDirs(cfg))
}

func TestRootDirs_StockROMsAndExternalDrives(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	fs := newTestFs(t)
	for _, dir := range []string{
		shareInitRomsRoot,
		filepath.Join(externalsRoot, "usb0", "recalbox", "roms"),
		filepath.Join(externalsRoot, "usb0", "roms"),
		filepath.Join(externalsRoot, "usb1", "roms"),
		filepath.Join(externalsRoot, "usb2", "snes"),
	} {
		require.NoError(t, fs.MkdirAll(dir, 0o755))
	}
	require.NoError(t, afero.WriteFile(fs, filepath.Join(externalsRoot, "stray"), nil, 0o644))

	p, _, _ := newTestPlatform(t, fs)
	assert.Equal(t, []string{
		shareInitRomsRoot,
		romsRoot,
		filepath.Join(externalsRoot, "usb0", "recalbox", "roms"),
		filepath.Join(externalsRoot, "usb1", "roms"),
		filepath.Join(externalsRoot, "usb2"),
	}, p.RootDirs(cfg))
}

func TestLaunchers_FromSystemList(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, userSystemList, testUserSystems)
	p, _, _ := newTestPlatform(t, fs)

	byID := map[string]platforms.Launcher{}
	for _, l := range p.esLaunchers() {
		byID[l.ID] = l
	}

	snes, ok := byID["SNES"]
	require.True(t, ok)
	assert.Equal(t, systemdefs.SystemSNES, snes.SystemID)
	assert.Equal(t, []string{"snes"}, snes.Folders)
	assert.Equal(t, []string{".sfc"}, snes.Extensions, "user list overrides the template")
	assert.Equal(t, platforms.LifecycleExternal, snes.Lifecycle)
	assert.NotNil(t, snes.Launch)

	ports, ok := byID[portsLauncherID]
	require.True(t, ok)
	assert.Equal(t, systemdefs.SystemPC, ports.SystemID)
	assert.ElementsMatch(t, []string{"ports/2048", "ports/OutRun"}, ports.Folders)
	assert.ElementsMatch(t, []string{".game", ".zip"}, ports.Extensions)

	mame, ok := byID["MAME"]
	require.True(t, ok)
	assert.Equal(t, []string{"/media/usb/arcade"}, mame.Folders)
	assert.NotEmpty(t, mame.Extensions, "empty descriptor falls back to known extensions")

	segacd, ok := byID[systemdefs.SystemMegaCD]
	require.True(t, ok)
	assert.Equal(t, []string{"segacd"}, segacd.Folders)

	assert.Len(t, byID, 4, "240ptestsuite is unmapped and the escaping folder is skipped")
}

func TestLaunchers_NoSystemList(t *testing.T) {
	t.Parallel()
	p, _, _ := newTestPlatform(t, afero.NewMemMapFs())
	assert.Empty(t, p.esLaunchers())
}

func TestStartPostAndStop(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, _ := newTestPlatform(t, fs)
	subscribed := make(chan context.Context, 1)
	p.subscribe = func(ctx context.Context, _ func()) {
		subscribed <- ctx
	}
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	require.NoError(t, p.StartPost(context.Background(), cfg, nil, rec.get, rec.set, nil, nil))
	ctx := <-subscribed
	require.NoError(t, p.Stop())
	<-ctx.Done()
	require.NoError(t, p.Stop(), "stopping twice is fine")
}
