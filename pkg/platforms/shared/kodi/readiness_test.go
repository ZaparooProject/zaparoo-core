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

package kodi

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// playersClient answers GetActivePlayers from a script; the last entry repeats.
type playersClient struct {
	KodiClient
	results []playersResult
	calls   atomic.Int32
}

type playersResult struct {
	err     error
	players []Player
}

func (c *playersClient) GetActivePlayers(context.Context) ([]Player, error) {
	i := min(int(c.calls.Add(1))-1, len(c.results)-1)
	return c.results[i].players, c.results[i].err
}

// itemClient adds the playing item to a players script; the last item repeats.
type itemClient struct {
	items []*PlayerItem
	playersClient
	asked atomic.Int32
}

func (c *itemClient) GetPlayerItem(context.Context, int) (*PlayerItem, error) {
	i := min(int(c.asked.Add(1))-1, len(c.items)-1)
	return c.items[i], nil
}

func TestWaitForPlayer_PreviousItemIsNotReady(t *testing.T) {
	t.Parallel()

	c := &itemClient{
		playersClient: playersClient{results: []playersResult{{players: []Player{{ID: 1}}}}},
		items: []*PlayerItem{
			{Label: "Old", File: "/old.mkv", Type: "movie"},
			{Label: "Old", File: "/old.mkv", Type: "movie"},
			{Label: "New", File: "/new.mkv", Type: "movie"},
		},
	}
	clock := clockwork.NewFakeClock()
	probe := waitForPlayer(func(*config.Instance) KodiClient { return c }, clock)
	done := make(chan error, 1)
	go func() {
		done <- probe(t.Context(), nil,
			models.NewActiveMedia("Movies", "Movies", "kodi-movie://1/Other", "Other", "Kodi"))
	}()

	for range 2 {
		require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
		clock.Advance(readyPollInterval)
	}

	require.NoError(t, <-done)
	assert.Equal(t, int32(3), c.asked.Load(), "ready only once the item changed")
}

func TestWaitForPlayer_LaunchedItemByNameIsReadyAtOnce(t *testing.T) {
	t.Parallel()

	c := &itemClient{
		playersClient: playersClient{results: []playersResult{{players: []Player{{ID: 1}}}}},
		items:         []*PlayerItem{{Label: "Movie", File: "/m.mkv", Type: "movie"}},
	}
	probe := waitForPlayer(func(*config.Instance) KodiClient { return c }, clockwork.NewFakeClock())

	require.NoError(t, probe(t.Context(), nil,
		models.NewActiveMedia("Movies", "Movies", "kodi-movie://1/Movie", "Movie", "Kodi")))
	assert.Equal(t, int32(1), c.asked.Load())
}

func probeFor(c *playersClient, clock clockwork.Clock) func(context.Context, *config.Instance) error {
	probe := waitForPlayer(func(*config.Instance) KodiClient { return c }, clock)
	return func(ctx context.Context, cfg *config.Instance) error { return probe(ctx, cfg, nil) }
}

func TestWaitForPlayer_ReadyWhenPlayerActive(t *testing.T) {
	t.Parallel()

	c := &playersClient{results: []playersResult{{players: []Player{{ID: 1}}}}}

	require.NoError(t, probeFor(c, clockwork.NewFakeClock())(t.Context(), nil))
	assert.Equal(t, int32(1), c.calls.Load())
}

func TestWaitForPlayer_WaitsForPlayback(t *testing.T) {
	t.Parallel()

	c := &playersClient{results: []playersResult{
		{players: nil},
		{players: []Player{}},
		{players: []Player{{ID: 1}}},
	}}
	clock := clockwork.NewFakeClock()
	done := make(chan error, 1)
	go func() { done <- probeFor(c, clock)(t.Context(), nil) }()

	for range 2 {
		require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
		clock.Advance(readyPollInterval)
	}

	require.NoError(t, <-done)
	assert.Equal(t, int32(3), c.calls.Load())
}

func TestWaitForPlayer_NoPlayerIsNotUnavailable(t *testing.T) {
	t.Parallel()

	c := &playersClient{results: []playersResult{{players: nil}}}
	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- probeFor(c, clock)(ctx, nil) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func TestWaitForPlayer_UnreachableKodiIsRetriedThenUnavailable(t *testing.T) {
	t.Parallel()

	down := errors.New("connection refused")
	c := &playersClient{results: []playersResult{
		{err: down},
		{err: down},
	}}
	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- probeFor(c, clock)(ctx, nil) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(readyPollInterval)
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	err := <-done
	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable)
	require.ErrorIs(t, err, down)
	assert.Equal(t, int32(2), c.calls.Load())
}

func TestKodiLaunchersHaveReadinessProbe(t *testing.T) {
	t.Parallel()

	launchers := []platforms.Launcher{
		NewKodiLocalLauncher(),
		NewKodiMovieLauncher(),
		NewKodiTVLauncher(),
		NewKodiMusicLauncher(),
		NewKodiSongLauncher(),
		NewKodiAlbumLauncher(),
		NewKodiArtistLauncher(),
		NewKodiTVShowLauncher(),
	}
	for i := range launchers {
		assert.NotNil(t, launchers[i].WaitForReady, launchers[i].ID)
	}
}

func TestWaitForPlayer_LaunchedItemByPathIsReadyAtOnce(t *testing.T) {
	t.Parallel()

	c := &itemClient{
		playersClient: playersClient{results: []playersResult{{players: []Player{{ID: 1}}}}},
		items:         []*PlayerItem{{Label: "Other label", File: "/media/movies/m.mkv", Type: "movie"}},
	}
	probe := waitForPlayer(func(*config.Instance) KodiClient { return c }, clockwork.NewFakeClock())

	require.NoError(t, probe(t.Context(), nil,
		models.NewActiveMedia("Movies", "Movies", "/media/movies/m.mkv", "Movie", "KodiLocalVideo")))
	assert.Equal(t, int32(1), c.asked.Load())
}
