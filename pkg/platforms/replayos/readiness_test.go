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

package replayos

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readyTestGame = "/media/sd/roms/nintendo_nes/Game.nes"

func readyTestMedia() *models.ActiveMedia {
	return models.NewActiveMedia("NES", "NES", readyTestGame, "Game", "ReplayOSNES")
}

func TestWaitForGame_ReadyWhenGameRunning(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{view: "game_play", token: "604318", gameFile: readyTestGame}
	n := newTestNetControl(t, f, testCfg)

	err := n.waitForGame(clockwork.NewFakeClock())(context.Background(), nil, readyTestMedia())

	require.NoError(t, err)
	assert.Len(t, f.requests, 1)
}

func TestWaitForGame_WaitsThroughMenuRestart(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{
		view:     "game_play",
		views:    []string{"system_list", "system_list"},
		token:    "604318",
		gameFile: readyTestGame,
	}
	n := newTestNetControl(t, f, testCfg)
	clock := clockwork.NewFakeClock()
	done := make(chan error, 1)
	go func() { done <- n.waitForGame(clock)(t.Context(), nil, readyTestMedia()) }()

	for range 2 {
		require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
		clock.Advance(readyPollInterval)
	}

	require.NoError(t, <-done)
	assert.Len(t, f.requests, 3)
}

func TestWaitForGame_OtherGameIsNotReady(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{view: "game_play", token: "604318", gameFile: "/media/sd/roms/nintendo_nes/Other.nes"}
	n := newTestNetControl(t, f, testCfg)
	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- n.waitForGame(clock)(ctx, nil, readyTestMedia()) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func TestWaitForGame_SamePathSpelledDifferently(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{view: "game_play", token: "604318", gameFile: "/media/usb/../sd/roms/nintendo_nes/Game.nes"}
	n := newTestNetControl(t, f, testCfg)

	require.NoError(t, n.waitForGame(clockwork.NewFakeClock())(t.Context(), nil, readyTestMedia()))
}

func TestWaitForGame_RateLimitIsRetried(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{view: "game_play", token: "604318", gameFile: readyTestGame, rateLimit: 1}
	n := newTestNetControl(t, f, testCfg)
	clock := clockwork.NewFakeClock()
	done := make(chan error, 1)
	go func() { done <- n.waitForGame(clock)(t.Context(), nil, readyTestMedia()) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(readyPollInterval)

	require.NoError(t, <-done)
}

func TestWaitForGame_UnreachableAPIReportsUnavailable(t *testing.T) {
	t.Parallel()

	n := newTestNetControl(t, &fakeReplay{token: "604318"}, testCfg)
	n.baseURL = "http://127.0.0.1:1/api/v1"
	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- n.waitForGame(clock)(ctx, nil, readyTestMedia()) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	err := <-done
	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable)
	require.ErrorIs(t, err, errNetControlUnavailable)
}

func TestWaitForGame_RejectedCodeEndsWait(t *testing.T) {
	t.Parallel()

	f := &fakeReplay{view: "game_play", token: "999999", gameFile: readyTestGame}
	n := newTestNetControl(t, f, testCfg)

	err := n.waitForGame(clockwork.NewFakeClock())(t.Context(), nil, readyTestMedia())

	require.ErrorIs(t, err, errNetControlToken)
	require.NotErrorIs(t, err, platforms.ErrReadinessUnavailable)
	assert.Len(t, f.requests, 1, "a rejected code is not retried")
}

func TestLaunchersHaveReadinessProbe(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	launchers := (&Platform{}).Launchers(cfg)
	var generic, systems int
	for i := range launchers {
		if launchers[i].ID == "Generic" {
			generic++
			assert.Nil(t, launchers[i].WaitForReady, "a script launch is ready when it starts")
			continue
		}
		systems++
		assert.NotNil(t, launchers[i].WaitForReady, launchers[i].ID)
	}
	assert.Equal(t, 1, generic)
	assert.Positive(t, systems)
}

func TestSameGame(t *testing.T) {
	t.Parallel()

	assert.True(t, sameGame("/media/sd/roms/nintendo_nes/Game.nes", "/media/sd/roms/nintendo_nes/Game.nes"))
	assert.True(t, sameGame("/media/usb/roms/nintendo_nes/Game.nes", "/media/sd/roms/nintendo_nes/Game.nes"),
		"a mount spelled differently")
	assert.True(t, sameGame("Game.nes", "/media/sd/roms/nintendo_nes/Game.nes"), "a bare file name")
	assert.False(t, sameGame("/media/sd/roms/sega_smd/game.zip", "/media/sd/roms/nintendo_nes/game.zip"),
		"the same file name under another system")
	assert.False(t, sameGame("", "/roms/a.nes"))
	assert.False(t, sameGame("/roms/a.nes", ""))
}
