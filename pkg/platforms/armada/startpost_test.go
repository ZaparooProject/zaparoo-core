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

package armada

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxemu"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// activeMediaStore stands in for the service's active media state.
type activeMediaStore struct {
	media *models.ActiveMedia
	mu    syncutil.Mutex
}

func (s *activeMediaStore) get() *models.ActiveMedia {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.media
}

func (s *activeMediaStore) set(m *models.ActiveMedia) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.media = m
}

func (s *activeMediaStore) path() string {
	if m := s.get(); m != nil {
		return m.Path
	}
	return ""
}

// fakeProc points the platform's process scanner at an empty /proc tree.
// Not parallel: it swaps a package variable.
func fakeProc(t *testing.T) string {
	t.Helper()
	procDir := t.TempDir()
	old := newProcScanner
	t.Cleanup(func() { newProcScanner = old })
	newProcScanner = func() *procscanner.Scanner {
		return procscanner.New(
			procscanner.WithProcPath(procDir),
			procscanner.WithPollInterval(10*time.Millisecond),
		)
	}
	return procDir
}

// startSteamGame writes a Steam reaper process into the fake /proc tree.
func startSteamGame(t *testing.T, procDir string, pid, appID int) {
	t.Helper()
	dir := filepath.Join(procDir, strconv.Itoa(pid))
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "comm"), []byte("reaper\n"), 0o600))
	cmdline := fmt.Sprintf("reaper\x00SteamLaunch\x00AppId=%d\x00--\x00/games/game.exe\x00", appID)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o600))
}

// steamConfig returns a config naming steamRoot as the Steam install and
// an isolated home, so the host's own Steam is never seen.
func steamConfig(t *testing.T, steamRoot string) *config.Instance {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg, err := helpers.NewTestConfig(helpers.NewOSFS(), filepath.Join(t.TempDir(), "config"))
	require.NoError(t, err)
	if steamRoot != "" {
		require.NoError(t, cfg.LoadTOML(fmt.Sprintf(`
[[launchers.default]]
launcher = "Steam"
install_dir = %q
`, steamRoot)))
	}
	return cfg
}

func TestStartPostTracksSteamGames(t *testing.T) {
	procDir := fakeProc(t)
	steamRoot := t.TempDir()
	steamApps := filepath.Join(steamRoot, "steamapps")
	require.NoError(t, os.MkdirAll(steamApps, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(steamApps, "appmanifest_620.acf"),
		[]byte("\"AppState\"\n{\n\t\"appid\"\t\t\"620\"\n\t\"name\"\t\t\"Portal 2\"\n}\n"), 0o600))
	cfg := steamConfig(t, steamRoot)

	store := &activeMediaStore{}
	p := NewPlatform()
	before := goleak.IgnoreCurrent()
	require.NoError(t, p.StartPost(context.Background(), cfg, nil, store.get, store.set, nil, nil))
	require.NotNil(t, p.procScanner)
	require.NotNil(t, p.steamTracker)

	startSteamGame(t, procDir, 4242, 620)
	require.Eventually(t, func() bool { return store.path() != "" }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, steam.MediaPath(620, "Portal 2"), store.path(), "named from the configured Steam root")

	require.NoError(t, p.Stop())
	goleak.VerifyNone(t, before) // the process scanner's poll loop has exited
}

func TestStartPostWithoutSteamStartsNoScanner(t *testing.T) {
	fakeProc(t)
	cfg := steamConfig(t, "")

	p := NewPlatform()
	require.NoError(t, p.StartPost(context.Background(), cfg, nil, nil, nil, nil, nil))
	assert.Nil(t, p.procScanner)
	assert.Nil(t, p.steamTracker)
	require.NoError(t, p.Stop())
}

func TestLaunchMediaRunsGivenLauncher(t *testing.T) {
	cfg := steamConfig(t, "")
	store := &activeMediaStore{}
	p := NewPlatform()
	require.NoError(t, p.StartPost(context.Background(), cfg, nil, store.get, store.set, nil, nil))
	t.Cleanup(func() { _ = p.Stop() })

	var launched string
	launcher := &platforms.Launcher{
		ID:       "TestLauncher",
		SystemID: "PC",
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			launched = path
			return nil, nil //nolint:nilnil // a launcher with no tracked process
		},
	}
	require.NoError(t, p.LaunchMedia(cfg, "/games/thing.exe", launcher, nil, nil))
	assert.Equal(t, "/games/thing.exe", launched)
}

func TestInputCoverageWithholdsControllers(t *testing.T) {
	old := baseCoverage
	t.Cleanup(func() { baseCoverage = old })
	baseCoverage = func(*linuxbase.Base) platforms.InputClass { return platforms.InputAll }

	got := NewPlatform().InputCoverage()
	assert.Equal(t, withoutControllers(platforms.InputAll), got)
	assert.False(t, got.Has(platforms.InputController))
}

func TestDefaultEmulationOptionsFindEmuDeckInHome(t *testing.T) {
	cfg := steamConfig(t, "")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	paths := linuxemu.DefaultEmuDeckPaths(home)
	require.NoError(t, os.MkdirAll(filepath.Join(paths.RomsPath, "gc"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(paths.GamelistPath, "gc"), 0o750))
	wrapper := filepath.Join(paths.ToolsPath, "launchers", "dolphin-emu.sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o750))
	require.NoError(t, os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o600))
	require.NoError(t, os.Chmod(wrapper, 0o700)) //nolint:gosec // Test wrapper must be runnable.

	assert.True(t, launcherIDs(NewPlatform().Launchers(cfg))["EmuDeckGameCube"])
}
