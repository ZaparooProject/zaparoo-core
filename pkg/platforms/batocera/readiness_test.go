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

package batocera

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/gamehandoff"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gameMedia(path string) *models.ActiveMedia {
	return models.NewActiveMedia("NES", "NES", path, "Game", "NES")
}

func writeGame(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "game.nes")
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	return path
}

// advanceUntil drives the fake clock until the wait returns.
func advanceUntil(t *testing.T, clock *clockwork.FakeClock, done <-chan error) error {
	t.Helper()
	for {
		select {
		case err := <-done:
			return err
		case <-time.After(time.Millisecond):
			clock.Advance(gameSettle)
		}
	}
}

func TestWaitForGame_ReadyAfterTheEmulatorReadsTheGame(t *testing.T) {
	t.Parallel()
	game := writeGame(t)
	clock := clockwork.NewFakeClock()
	p := &Platform{clock: clock}
	p.launches().Note(game)
	events := make(chan gamehandoff.Kind)
	p.watchGame = func(context.Context, string) (<-chan gamehandoff.Kind, error) { return events, nil }
	done := make(chan error, 1)
	go func() { done <- p.waitForGame(t.Context(), nil, gameMedia(game)) }()

	events <- gamehandoff.Open
	events <- gamehandoff.Close

	require.NoError(t, advanceUntil(t, clock, done))
}

func TestWaitForGame_WatchesTheRealFile(t *testing.T) {
	t.Parallel()
	game := writeGame(t)
	clock := clockwork.NewFakeClock()
	p := &Platform{clock: clock}
	p.launches().Note(game)
	done := make(chan error, 1)
	go func() { done <- p.waitForGame(t.Context(), nil, gameMedia(game)) }()
	// The watch is set up asynchronously; read the file until the wait sees it.
	ready := make(chan struct{})
	go func() {
		defer close(ready)
		for range 200 {
			f, err := os.Open(game) //nolint:gosec // test path
			if err == nil {
				_ = f.Close()
			}
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
				clock.Advance(gameSettle)
			}
		}
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("wait did not finish")
	}
	<-ready
}

func TestWaitForGame_GameCoreDidNotLaunchIsReadyImmediately(t *testing.T) {
	t.Parallel()
	game := writeGame(t)
	p := &Platform{clock: clockwork.NewFakeClock()}
	p.watchGame = func(context.Context, string) (<-chan gamehandoff.Kind, error) {
		t.Fatal("watched a game Core did not launch")
		return nil, errors.New("unreachable")
	}

	require.NoError(t, p.waitForGame(t.Context(), nil, gameMedia(game)))
}

func TestWaitForGame_StaleLaunchIsReadyImmediately(t *testing.T) {
	t.Parallel()
	game := writeGame(t)
	clock := clockwork.NewFakeClock()
	p := &Platform{clock: clock}
	p.launches().Note(game)
	clock.Advance(launchFreshness + time.Second)
	p.watchGame = func(context.Context, string) (<-chan gamehandoff.Kind, error) {
		t.Fatal("watched a game from a stale launch")
		return nil, errors.New("unreachable")
	}

	require.NoError(t, p.waitForGame(t.Context(), nil, gameMedia(game)))
}

func TestWaitForGame_FolderOrMissingGameIsReadyImmediately(t *testing.T) {
	t.Parallel()
	for name, path := range map[string]string{
		"folder":  t.TempDir(),
		"missing": filepath.Join(t.TempDir(), "gone.nes"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := &Platform{clock: clockwork.NewFakeClock()}
			p.launches().Note(path)
			p.watchGame = func(context.Context, string) (<-chan gamehandoff.Kind, error) {
				t.Fatal("watched something that is not a game file")
				return nil, errors.New("unreachable")
			}

			require.NoError(t, p.waitForGame(t.Context(), nil, gameMedia(path)))
		})
	}
}

func TestWaitForGame_WatchFailureIsUnavailable(t *testing.T) {
	t.Parallel()
	game := writeGame(t)
	p := &Platform{clock: clockwork.NewFakeClock()}
	p.launches().Note(game)
	p.watchGame = func(context.Context, string) (<-chan gamehandoff.Kind, error) {
		return nil, errors.New("inotify limit reached")
	}

	err := p.waitForGame(t.Context(), nil, gameMedia(game))

	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func TestWaitForGame_NeverOpenedIsReadyAfterTheWindow(t *testing.T) {
	t.Parallel()
	game := writeGame(t)
	clock := clockwork.NewFakeClock()
	p := &Platform{clock: clock}
	p.launches().Note(game)
	p.watchGame = func(context.Context, string) (<-chan gamehandoff.Kind, error) {
		return make(chan gamehandoff.Kind), nil
	}
	done := make(chan error, 1)
	go func() { done <- p.waitForGame(t.Context(), nil, gameMedia(game)) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(gameOpenWindow)

	require.NoError(t, <-done)
}

func TestNoteLaunches_RecordsAndPassesThrough(t *testing.T) {
	t.Parallel()
	p := &Platform{clock: clockwork.NewFakeClock()}
	boom := errors.New("launch failed")
	var got string
	launch := p.noteLaunches(func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
		got = path
		return nil, boom
	})

	_, err := launch(nil, "/roms/nes/game.nes", nil)

	require.ErrorIs(t, err, boom)
	assert.Equal(t, "/roms/nes/game.nes", got)
	assert.True(t, p.launches().Recent("/roms/nes/game.nes", launchFreshness))
}

func TestLaunchers_EmulationStationSystemsHaveReadinessProbe(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	launchers := (&Platform{}).Launchers(cfg)

	var withProbe int
	for i := range launchers {
		switch launchers[i].ID {
		case "Generic":
			assert.Nil(t, launchers[i].WaitForReady, "a script launch is ready when it starts")
		default:
			if launchers[i].WaitForReady != nil {
				withProbe++
			}
		}
	}
	assert.Greater(t, withProbe, 8, "every ES system plus the Kodi launchers")
}

func TestGameOpenWindowIsShorterThanTheServiceTimeout(t *testing.T) {
	t.Parallel()

	// The service ends every probe after 10 s (pkg/service mediaReadyTimeout);
	// a longer window could never fire.
	assert.Less(t, gameOpenWindow, 10*time.Second)
}
