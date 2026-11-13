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
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxemu"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnsCore(t *testing.T) {
	t.Parallel()
	assert.True(t, ownsCore(os.Getpid()))
	assert.True(t, ownsCore(os.Getppid()), "the process that started Core, such as a Steam reaper")
	assert.False(t, ownsCore(0))
	assert.False(t, ownsCore(-1))

	child := exec.CommandContext(context.Background(), "sleep", "5")
	require.NoError(t, child.Start())
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	assert.False(t, ownsCore(child.Process.Pid), "Core's own child is not one of its ancestors")
}

func TestProcessParent(t *testing.T) {
	t.Parallel()
	parent, ok := processParent(os.Getpid())
	require.True(t, ok)
	assert.Equal(t, os.Getppid(), parent)

	_, ok = processParent(-5)
	assert.False(t, ok)
}

func TestStartPostIgnoresSteamReaperThatStartedCore(t *testing.T) {
	procDir := fakeProc(t)
	cfg := steamConfig(t, t.TempDir())

	store := &activeMediaStore{}
	p := NewPlatform()
	require.NoError(t, p.StartPost(context.Background(), cfg, nil, store.get, store.set, nil, nil))
	t.Cleanup(func() { _ = p.Stop() })

	startSteamGame(t, procDir, os.Getppid(), 620)
	assert.Never(t, func() bool { return store.path() != "" }, 300*time.Millisecond, 10*time.Millisecond,
		"Core's own Steam reaper is not a game")

	startSteamGame(t, procDir, 4242, 730)
	require.Eventually(t, func() bool { return store.path() != "" }, 2*time.Second, 10*time.Millisecond,
		"other Steam games are still tracked")
}

func TestLaunchersNameFilesFromDefaultROMRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := newTestConfig(t)
	require.Empty(t, cfg.IndexRoots())

	romPath := filepath.Join(home, "ROMs", "snes", "game.sfc")
	require.NoError(t, os.MkdirAll(filepath.Dir(romPath), 0o750))
	require.NoError(t, os.WriteFile(romPath, []byte("rom"), 0o600))
	gamelistDir := filepath.Join(home, "ES-DE", "gamelists", "snes")
	require.NoError(t, os.MkdirAll(gamelistDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(gamelistDir, "gamelist.xml"), []byte(
		`<gameList><game><name>Friendly Name</name><path>./game.sfc</path></game></gameList>`,
	), 0o600))

	options := linuxemu.NewOptions(home, sharedretroarch.Options{Exec: []string{"flatpak"}})
	options.IncludeStandalone = false
	options.IncludeProviderDecks = false
	options.IsFlatpakInstalled = func(id string) bool { return id == linuxemu.RetroArchFlatpakID }
	p := NewPlatform()
	p.emulationOptionsOverride = &options

	var scanned bool
	for _, launcher := range p.Launchers(cfg) {
		if launcher.ID != "RetroArchSNES9x" {
			continue
		}
		require.NotNil(t, launcher.Scanner, "ES-DE names apply to the default ~/ROMs root")
		results, err := launcher.Scanner(context.Background(), cfg, systemdefs.SystemSNES,
			[]platforms.ScanResult{{Path: romPath, Name: "game"}})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "Friendly Name", results[0].Name)
		scanned = true
	}
	assert.True(t, scanned, "RetroArch SNES launcher offered")
}
