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

package kodi_test

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

const (
	testFirstPlay = 30 * time.Second
	testGrace     = 3 * time.Second
	waitFor       = 5 * time.Second
	tick          = 5 * time.Millisecond
)

type watchRig struct {
	srv    *helpers.MockKodiServer
	client kodi.KodiClient
	clock  *clockwork.FakeClock
	opts   *platforms.LaunchOptions
	// repeatSet receives once per successful repeat-off request.
	repeatSet chan struct{}
	calls     atomic.Int32
	exits     atomic.Int32
	// settled counts watcher loop iterations; it is 1 once the watcher is
	// waiting for its first event and grows by one per event handled.
	settled atomic.Int32
}

func newRig(t *testing.T) *watchRig {
	t.Helper()
	srv := helpers.NewMockKodiServer(t)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL())
	require.NoError(t, err)
	r := &watchRig{srv: srv, clock: clockwork.NewFakeClock()}
	r.client = kodi.NewWatchTestClient(srv.URL(), u.Port(), r.clock, testFirstPlay, testGrace,
		func() { r.settled.Add(1) })
	r.repeatSet = make(chan struct{}, 4)
	kodi.SetRepeatHook(r.client, func() { r.repeatSet <- struct{}{} })
	r.opts = &platforms.LaunchOptions{
		MediaFinished: func() { r.calls.Add(1) },
		MediaExited:   func() { r.exits.Add(1) },
	}
	return r
}

func (r *watchRig) launchMovie(t *testing.T, exp kodi.Expectation) {
	t.Helper()
	require.NoError(t, kodi.RunTracked(r.client, r.opts, exp, func(c kodi.KodiClient) error {
		return c.LaunchMovie("kodi-movie://7/Heat")
	}))
	require.Eventually(t, func() bool { return r.srv.WebSocketClients() == 1 }, waitFor, tick)
}

func (r *watchRig) push(t *testing.T, method string, data any) {
	t.Helper()
	require.NoError(t, r.srv.PushNotification(method, data))
}

func (r *watchRig) waitStopped(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return !kodi.WatcherActive(r.srv.URL()) }, waitFor, tick)
}

// waitSettled blocks until the watcher has handled events events.
func (r *watchRig) waitSettled(t *testing.T, events int32) {
	t.Helper()
	require.Eventually(t, func() bool { return r.settled.Load() > events }, waitFor, tick)
}

func (r *watchRig) waitCalls(t *testing.T, n int32) {
	t.Helper()
	require.Eventually(t, func() bool { return r.calls.Load() >= n }, waitFor, tick)
}

func movie(id int) map[string]any {
	return map[string]any{"type": "movie", "id": id, "title": "Heat"}
}

func playData(item map[string]any) map[string]any {
	return map[string]any{"item": item, "player": map[string]any{"playerid": 1, "speed": 1}}
}

func stopData(item map[string]any, end bool) map[string]any {
	return map[string]any{"item": item, "end": end}
}

func TestWatch_NaturalEndCallsOnce(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})

	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.push(t, "Player.OnAVStart", playData(movie(7)))
	r.push(t, "Player.OnPause", playData(movie(7)))
	r.push(t, "Player.OnStop", stopData(movie(7), true))
	r.push(t, "Player.OnStop", stopData(movie(7), true))

	r.waitSettled(t, 5)
	r.clock.Advance(testGrace)
	r.waitCalls(t, 1)
	r.waitStopped(t)
	assert.Equal(t, int32(1), r.calls.Load())
	assert.Zero(t, r.exits.Load())
}

func TestWatch_UserStopDoesNotCall(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})

	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.push(t, "Player.OnStop", stopData(movie(7), false))

	r.waitStopped(t)
	assert.Zero(t, r.calls.Load())
	assert.Equal(t, int32(1), r.exits.Load())
}

func TestWatch_StopBeforePlayIsIgnored(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})

	// The replaced item stops (even by reaching its end) before ours plays.
	r.push(t, "Player.OnStop", stopData(map[string]any{"type": "movie", "id": 1}, false))
	r.push(t, "Player.OnStop", stopData(map[string]any{"type": "movie", "id": 1}, true))
	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.push(t, "Player.OnStop", stopData(movie(7), true))

	r.waitSettled(t, 4)
	r.clock.Advance(testGrace)
	r.waitCalls(t, 1)
	r.waitStopped(t)
	assert.Equal(t, int32(1), r.calls.Load())
}

func TestWatch_PlayOfOtherLibraryItemIsSkippedUntilOurs(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})
	exp := kodi.SongExpectation(9)
	require.NoError(t, kodi.RunTracked(r.client, r.opts, exp, func(c kodi.KodiClient) error {
		return c.LaunchSong("kodi-song://9/Track")
	}))
	// The first socket closes and only the second watcher remains. The open
	// count alone can read 1 while only the first socket is registered.
	require.Eventually(t, func() bool {
		return r.srv.WebSocketsAccepted() == 2 && r.srv.WebSocketClients() == 1
	}, waitFor, tick)

	r.push(t, "Player.OnPlay", playData(map[string]any{"type": "song", "id": 3}))
	r.push(t, "Player.OnStop", stopData(map[string]any{"type": "song", "id": 3}, true))
	r.push(t, "Player.OnPlay", playData(map[string]any{"type": "song", "id": 9}))
	r.push(t, "Player.OnStop", stopData(map[string]any{"type": "song", "id": 9}, true))

	r.waitSettled(t, 5)
	r.clock.Advance(testGrace)
	r.waitCalls(t, 1)
	assert.Equal(t, int32(1), r.calls.Load())
}

func TestWatch_DifferentItemPlayStopsWatching(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})

	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.push(t, "Player.OnPlay", playData(movie(8)))

	r.waitStopped(t)
	assert.Zero(t, r.calls.Load())
	assert.Zero(t, r.exits.Load())
}

func TestWatch_KodiClosingSocketStopsWatching(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})

	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.srv.CloseWebSockets()

	r.waitStopped(t)
	assert.Zero(t, r.calls.Load())
	assert.Equal(t, int32(1), r.exits.Load())
}

func TestWatch_FirstPlayTimeoutGivesUp(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})

	r.waitSettled(t, 0)
	r.clock.Advance(testFirstPlay)

	r.waitStopped(t)
	assert.Zero(t, r.calls.Load())
	assert.Zero(t, r.exits.Load())
}

func TestWatch_FirstPlayTimeoutDoesNotAffectArmedWatcher(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})

	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.waitSettled(t, 1)
	r.clock.Advance(2 * testFirstPlay)
	assert.True(t, kodi.WatcherActive(r.srv.URL()))

	r.push(t, "Player.OnStop", stopData(movie(7), true))
	r.waitSettled(t, 2)
	r.clock.Advance(testGrace)
	r.waitCalls(t, 1)
}

func TestWatch_QueueWaitsForNextItem(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.QueueExpectation)
	ep := func(id int) map[string]any { return map[string]any{"type": "episode", "id": id} }

	r.push(t, "Player.OnPlay", playData(ep(1)))
	r.push(t, "Player.OnStop", stopData(ep(1), true))
	r.waitSettled(t, 2)
	// The next episode starts inside the grace window.
	r.push(t, "Player.OnPlay", playData(ep(2)))
	r.waitSettled(t, 3)
	r.clock.Advance(testGrace)
	assert.Zero(t, r.calls.Load())
	assert.True(t, kodi.WatcherActive(r.srv.URL()))

	// The last episode ends and nothing follows.
	r.push(t, "Player.OnStop", stopData(ep(2), true))
	r.waitSettled(t, 4)
	r.clock.Advance(testGrace)

	r.waitCalls(t, 1)
	r.waitStopped(t)
	assert.Equal(t, int32(1), r.calls.Load())
	assert.Zero(t, r.exits.Load())
}

func TestWatch_QueueUserStopDoesNotCall(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.QueueExpectation)
	ep := map[string]any{"type": "episode", "id": 1}

	r.push(t, "Player.OnPlay", playData(ep))
	r.push(t, "Player.OnStop", stopData(ep, false))

	r.waitStopped(t)
	assert.Zero(t, r.calls.Load())
	assert.Equal(t, int32(1), r.exits.Load())
}

func TestWatch_DialFailureDoesNotFailLaunch(t *testing.T) {
	t.Parallel()
	srv := helpers.NewMockKodiServer(t)
	t.Cleanup(srv.Close)
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, deadPort, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	require.NoError(t, ln.Close())

	var calls atomic.Int32
	client := kodi.NewWatchTestClient(srv.URL(), deadPort, clockwork.NewFakeClock(), testFirstPlay, testGrace, nil)
	opts := &platforms.LaunchOptions{
		MediaFinished: func() { calls.Add(1) },
		MediaExited:   func() { calls.Add(1) },
	}

	err = kodi.RunTracked(client, opts, kodi.Expectation{}, func(c kodi.KodiClient) error {
		return c.LaunchMovie("kodi-movie://7/Heat")
	})
	require.NoError(t, err)

	reqs := srv.Requests()
	require.Len(t, reqs, 1)
	assert.NotContains(t, string(reqs[0]), "repeat")
	assert.False(t, kodi.WatcherActive(srv.URL()))
	assert.Zero(t, calls.Load())
}

func TestWatch_SecondLaunchCancelsFirstWatcher(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})
	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.waitSettled(t, 1)

	var second atomic.Int32
	opts := &platforms.LaunchOptions{MediaFinished: func() { second.Add(1) }}
	require.NoError(t, kodi.RunTracked(r.client, opts, kodi.Expectation{}, func(c kodi.KodiClient) error {
		return c.LaunchMovie("kodi-movie://8/Ronin")
	}))
	// The first socket closes and only the second watcher remains. The open
	// count alone can read 1 while only the first socket is registered.
	require.Eventually(t, func() bool {
		return r.srv.WebSocketsAccepted() == 2 && r.srv.WebSocketClients() == 1
	}, waitFor, tick)

	r.push(t, "Player.OnPlay", playData(movie(8)))
	r.push(t, "Player.OnStop", stopData(movie(8), true))
	r.waitSettled(t, 4)
	r.clock.Advance(testGrace)
	require.Eventually(t, func() bool { return second.Load() == 1 }, waitFor, tick)
	r.waitStopped(t)
	assert.Zero(t, r.calls.Load())
	assert.Zero(t, r.exits.Load())
}

func TestWatch_LaunchWithoutCallbackCancelsWatcherAndLeavesBodyUnchanged(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.opts.PlaylistItem = true
	r.launchMovie(t, kodi.Expectation{})

	require.NoError(t, kodi.RunTracked(r.client, nil, kodi.Expectation{}, func(c kodi.KodiClient) error {
		return c.LaunchMovie("kodi-movie://8/Ronin")
	}))
	r.waitStopped(t)
	reqs := r.srv.Requests()
	require.Len(t, reqs, 2)
	assert.Contains(t, string(reqs[0]), `"repeat":"off"`)
	assert.NotContains(t, string(reqs[1]), "repeat")
	assert.Contains(t, string(reqs[1]), `"options":{"resume":true}`)
	require.Eventually(t, func() bool { return r.srv.WebSocketClients() == 0 }, waitFor, tick)
}

func TestWatch_RepeatOffOnlyForPlaylistItems(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		opts   func(*watchRig) *platforms.LaunchOptions
		name   string
		repeat bool
	}{
		{func(r *watchRig) *platforms.LaunchOptions {
			o := *r.opts
			o.PlaylistItem = true
			return &o
		}, "playlist item", true},
		{func(r *watchRig) *platforms.LaunchOptions { return r.opts }, "tracked non-playlist", false},
		{func(*watchRig) *platforms.LaunchOptions { return nil }, "nil options", false},
		{func(*watchRig) *platforms.LaunchOptions { return &platforms.LaunchOptions{} }, "no callback", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			require.NoError(t, kodi.RunTracked(r.client, tt.opts(r), kodi.Expectation{},
				func(c kodi.KodiClient) error { return c.LaunchFile("/media/a.mkv") }))
			reqs := r.srv.Requests()
			require.Len(t, reqs, 1)
			assert.Equal(t, tt.repeat, strings.Contains(string(reqs[0]), `"repeat":"off"`))
			if !tt.repeat {
				assert.Contains(t, string(reqs[0]), `"options":{"resume":true}`)
			}
		})
	}
}

func TestWatch_ExitedOnlyCallbackTracksLaunch(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.opts.PlaylistItem = true
	var exits atomic.Int32
	r.opts = &platforms.LaunchOptions{MediaExited: func() { exits.Add(1) }, PlaylistItem: true}
	r.launchMovie(t, kodi.Expectation{})

	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.push(t, "Player.OnStop", stopData(movie(7), false))
	require.Eventually(t, func() bool { return exits.Load() == 1 }, waitFor, tick)
	r.waitStopped(t)
	assert.Contains(t, string(r.srv.Requests()[0]), `"repeat":"off"`)
}

func TestWatch_SocketCloseBeforePlayReportsNothing(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})

	r.srv.CloseWebSockets()

	r.waitStopped(t)
	assert.Zero(t, r.calls.Load())
	assert.Zero(t, r.exits.Load())
}

func TestWatch_QueueLaunchSendsRepeatOff(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.opts.PlaylistItem = true
	require.NoError(t, kodi.RunTracked(r.client, r.opts, kodi.QueueExpectation, func(c kodi.KodiClient) error {
		return c.LaunchAlbum("kodi-album://4/Abbey")
	}))
	reqs := r.srv.Requests()
	require.Len(t, reqs, 1)
	assert.Contains(t, string(reqs[0]), `"repeat":"off"`)
}

func TestWatch_FailedLaunchStopsWatcher(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	err := kodi.RunTracked(r.client, r.opts, kodi.Expectation{}, func(kodi.KodiClient) error {
		return assert.AnError
	})
	require.ErrorIs(t, err, assert.AnError)
	assert.False(t, kodi.WatcherActive(r.srv.URL()))
}

// Not parallel: goleak inspects every goroutine in the process.
func TestWatch_NoGoroutineLeak(t *testing.T) {
	ignore := goleak.IgnoreCurrent()
	srv := helpers.NewMockKodiServer(t)
	u, err := url.Parse(srv.URL())
	require.NoError(t, err)
	client := kodi.NewWatchTestClient(srv.URL(), u.Port(), clockwork.NewFakeClock(), testFirstPlay, testGrace, nil)
	opts := &platforms.LaunchOptions{MediaFinished: func() {}}

	require.NoError(t, kodi.RunTracked(client, opts, kodi.Expectation{}, func(c kodi.KodiClient) error {
		return c.LaunchMovie("kodi-movie://7/Heat")
	}))
	require.Eventually(t, func() bool { return srv.WebSocketClients() == 1 }, waitFor, tick)
	require.NoError(t, kodi.RunTracked(client, nil, kodi.Expectation{}, func(c kodi.KodiClient) error {
		return c.LaunchMovie("kodi-movie://8/Ronin")
	}))
	require.Eventually(t, func() bool { return srv.WebSocketClients() == 0 }, waitFor, tick)
	require.Eventually(t, func() bool { return !kodi.WatcherActive(srv.URL()) }, waitFor, tick)

	srv.CloseClientConnections()
	srv.Close()
	goleak.VerifyNone(t, ignore)
}

func TestWatch_DialTimeoutIsBounded(t *testing.T) {
	t.Parallel()
	srv := helpers.NewMockKodiServer(t)
	t.Cleanup(srv.Close)
	// Accepts TCP but never answers the handshake, like a filtered port.
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	client := kodi.NewWatchTestClient(srv.URL(), port, clockwork.NewFakeClock(), testFirstPlay, testGrace, nil)
	kodi.SetDialTimeout(client, 100*time.Millisecond)
	opts := &platforms.LaunchOptions{MediaExited: func() { t.Error("unexpected callback") }, PlaylistItem: true}

	start := time.Now()
	err = kodi.RunTracked(client, opts, kodi.Expectation{}, func(c kodi.KodiClient) error {
		return c.LaunchMovie("kodi-movie://7/Heat")
	})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.False(t, kodi.WatcherActive(srv.URL()))
	reqs := srv.Requests()
	require.Len(t, reqs, 1)
	assert.NotContains(t, string(reqs[0]), "repeat")
}

func hasRequest(r *watchRig, needle string) bool {
	for _, b := range r.srv.Requests() {
		if strings.Contains(string(b), needle) {
			return true
		}
	}
	return false
}

func playerData(item map[string]any, playerID int) map[string]any {
	return map[string]any{"item": item, "player": map[string]any{"playerid": playerID, "speed": 1}}
}

func TestWatch_RepeatOneLoopIsNotFinished(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})

	// Kodi loops a repeat-one item through OnStop end=true then OnPlay.
	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.push(t, "Player.OnStop", stopData(movie(7), true))
	r.waitSettled(t, 2)
	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.waitSettled(t, 3)
	r.clock.Advance(testGrace)
	assert.Zero(t, r.calls.Load())
	assert.True(t, kodi.WatcherActive(r.srv.URL()))

	// A real end has no restart.
	r.push(t, "Player.OnStop", stopData(movie(7), true))
	r.waitSettled(t, 4)
	r.clock.Advance(testGrace)
	r.waitCalls(t, 1)
	assert.Equal(t, int32(1), r.calls.Load())
}

func TestWatch_SetRepeatOnlyForPlaylistItems(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.launchMovie(t, kodi.Expectation{})
	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.waitSettled(t, 1)
	assert.False(t, hasRequest(r, "Player.SetRepeat"))
}

func TestWatch_PlaylistItemSetsRepeatOffOnNotifiedPlayer(t *testing.T) {
	t.Parallel()
	for _, id := range []int{0, 1} {
		t.Run(strings.Repeat("p", id+1), func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			r.opts.PlaylistItem = true
			r.launchMovie(t, kodi.Expectation{})

			r.push(t, "Player.OnPlay", playerData(movie(7), id))
			want := `"params":{"repeat":"off","playerid":` + strconv.Itoa(id) + `}`
			require.Eventually(t, func() bool {
				for _, b := range r.srv.Requests() {
					if strings.Contains(string(b), "Player.SetRepeat") && strings.Contains(string(b), want) {
						return true
					}
				}
				return false
			}, waitFor, tick)
		})
	}
}

func TestWatch_PlaylistItemFinishesImmediatelyOnceRepeatIsOff(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.opts.PlaylistItem = true
	r.launchMovie(t, kodi.Expectation{})

	r.push(t, "Player.OnPlay", playData(movie(7)))
	// The request is recorded before the reply; wait until the guard has seen it succeed.
	select {
	case <-r.repeatSet:
	case <-time.After(waitFor):
		t.Fatal("repeat was not turned off")
	}
	r.push(t, "Player.OnStop", stopData(movie(7), true))

	r.waitCalls(t, 1)
	r.waitStopped(t)
}

func TestWatch_SetRepeatFailureKeepsLaunchAndWatcher(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.opts.PlaylistItem = true
	r.srv.FailMethod(kodi.APIMethodPlayerSetRepeat)
	r.launchMovie(t, kodi.Expectation{})

	r.push(t, "Player.OnPlay", playData(movie(7)))
	require.Eventually(t, func() bool { return hasRequest(r, "Player.SetRepeat") }, waitFor, tick)
	r.push(t, "Player.OnStop", stopData(movie(7), true))
	r.waitSettled(t, 2)
	// Repeat is not known to be off, so the end waits out the grace window.
	assert.Zero(t, r.calls.Load())
	r.clock.Advance(testGrace)

	r.waitCalls(t, 1)
	assert.Zero(t, r.exits.Load())
}

func TestWatch_RepeatChangedMidItemRestoresGrace(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.opts.PlaylistItem = true
	r.launchMovie(t, kodi.Expectation{})

	r.push(t, "Player.OnPlay", playData(movie(7)))
	select {
	case <-r.repeatSet:
	case <-time.After(waitFor):
		t.Fatal("repeat was not turned off")
	}
	r.push(t, "Player.OnPropertyChanged", map[string]any{"property": map[string]any{"repeat": "one"}})
	r.push(t, "Player.OnStop", stopData(movie(7), true))
	r.waitSettled(t, 3)
	assert.Zero(t, r.calls.Load())
	r.clock.Advance(testGrace)
	r.waitCalls(t, 1)
}

func TestTracking_FollowsWatcherLifetime(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	serverURL := r.client.GetURL()
	assert.False(t, kodi.Tracking(serverURL))

	r.launchMovie(t, kodi.Expectation{})
	assert.True(t, kodi.Tracking(serverURL))

	r.push(t, "Player.OnPlay", playData(movie(7)))
	r.push(t, "Player.OnStop", stopData(movie(7), true))
	r.waitSettled(t, 2)
	// Still tracked while the grace window is pending.
	assert.True(t, kodi.Tracking(serverURL))
	r.clock.Advance(testGrace)
	r.waitCalls(t, 1)
	r.waitStopped(t)
	assert.False(t, kodi.Tracking(serverURL))
}
