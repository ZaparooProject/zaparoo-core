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
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startFrontendGame(t *testing.T, fs afero.Fs, name, system, relDir, file string) {
	t.Helper()
	cfg := filepath.Join(infoContentDir, relDir, name+".cfg")
	writeFile(t, fs, cfg, name+"\ncore.so\n"+system+"\ncat\n0\ntag\n/mnt/mmc/ROMS/\n"+relDir+"\n"+file+"\n")
	writeFile(t, fs, lastPlayFile, cfg)
	writeFile(t, fs, overlayFile, name+"\n"+system+"\ncore.so")
}

func TestTrackerFollowsFrontendLaunches(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, media := newTestPlatform(t, fs)

	startFrontendGame(t, fs, "Game", "Nintendo Game Boy Advance", "GBA", "Game.gba")
	p.pollOverlay()

	active := media.get()
	require.NotNil(t, active)
	assert.Equal(t, systemdefs.SystemGBA, active.SystemID)
	assert.Equal(t, systemdefs.SystemGBA, active.LauncherID)
	assert.Equal(t, "/mnt/mmc/ROMS/GBA/Game.gba", active.Path)
	assert.Equal(t, "Game", active.Name)
	assert.NotEqual(t, active.SystemID, active.SystemName)

	p.pollOverlay()
	assert.Len(t, media.history, 1, "an unchanged overlay publishes nothing")

	require.NoError(t, fs.Remove(overlayFile))
	p.pollOverlay()
	assert.Nil(t, media.get())
	assert.Len(t, media.history, 2)

	p.pollOverlay()
	assert.Len(t, media.history, 2, "clearing happens once")
}

func TestTrackerLeavesZaparooLaunchesToTheLauncher(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, media := newTestPlatform(t, fs)
	p.pending = "Game"

	startFrontendGame(t, fs, "Game", "Nintendo Game Boy Advance", "GBA", "Game.gba")
	p.pollOverlay()

	assert.Empty(t, media.history)
	assert.Empty(t, p.pending)
}

func TestTrackerWaitsWhileLaunching(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, media := newTestPlatform(t, fs)
	p.launching = true

	startFrontendGame(t, fs, "Game", "Nintendo Game Boy Advance", "GBA", "Game.gba")
	p.pollOverlay()
	assert.Empty(t, media.history)
	assert.Empty(t, p.lastOverlay)

	p.launching = false
	p.pollOverlay()
	assert.NotNil(t, media.get())
}

func TestTrackerSkipsUnknownContent(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, media := newTestPlatform(t, fs)

	startFrontendGame(t, fs, "Game", "Some Homebrew System", "Homebrew", "Game.bin")
	p.pollOverlay()
	assert.Empty(t, media.history, "unmapped system")

	writeFile(t, fs, overlayFile, "Other\nNintendo Game Boy Advance\ncore.so")
	p.pollOverlay()
	assert.Empty(t, media.history, "last played is a different game")
}

func TestTrackerKeepsMatchingActiveMedia(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, media := newTestPlatform(t, fs)
	media.set(models.NewActiveMedia(systemdefs.SystemGBA, "GBA", "/mnt/mmc/ROMS/GBA/Game.gba", "Game", "GBA"))

	startFrontendGame(t, fs, "Game", "Nintendo Game Boy Advance", "GBA", "Game.gba")
	p.pollOverlay()
	assert.Len(t, media.history, 1)
}

func TestTrackerRunsOnClock(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	clock, ok := p.clock.(*clockwork.FakeClock)
	require.True(t, ok)
	published := make(chan *models.ActiveMedia, 4)
	p.setActiveMedia = func(m *models.ActiveMedia) { published <- m }
	p.activeMedia = func() *models.ActiveMedia { return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p.startTracker(ctx)
	defer p.stopTracker()

	startFrontendGame(t, fs, "Game", "Nintendo Game Boy Advance", "GBA", "Game.gba")
	require.NoError(t, clock.BlockUntilContext(ctx, 1))
	clock.Advance(trackerInterval)

	select {
	case m := <-published:
		require.NotNil(t, m)
		assert.Equal(t, "Game", m.Name)
	case <-ctx.Done():
		t.Fatal("tracker did not publish")
	}

	p.stopTracker()
	p.stopTracker()
}
