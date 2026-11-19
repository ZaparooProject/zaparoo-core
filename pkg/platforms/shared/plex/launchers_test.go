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

package plex

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetLastLaunch(t *testing.T) {
	t.Helper()
	setLastLaunch(nil)
	t.Cleanup(func() { setLastLaunch(nil) })
}

func testConfig(t *testing.T) *config.Instance {
	t.Helper()
	cfg, err := config.NewConfigWithFs(t.TempDir(), config.BaseDefaults, afero.NewMemMapFs())
	require.NoError(t, err)
	return cfg
}

// launcherRig is a library server, one Companion player and launchers wired
// to them.
type launcherRig struct {
	srv    *fakeServer
	player *fakeCompanion
	clock  *clockwork.FakeClock
	deps   *deps
}

func newLauncherRig(t *testing.T, playerName string) *launcherRig {
	t.Helper()
	resetLastLaunch(t)
	srv := newFakeServer(t)
	useTokens(t, srv.URL, "", "")
	player := newFakeCompanion(t)
	srv.clients = []ServerClient{player.serverClient("Living Room", "player-1")}
	clock := clockwork.NewFakeClock()
	return &launcherRig{
		srv:    srv,
		player: player,
		clock:  clock,
		deps: &deps{
			newClient: func(*config.Instance, string, []string) PlexClient { return srv.client(playerName) },
			finder:    testFinder(srv, nil, nil),
			clock:     clock,
		},
	}
}

func TestLaunchers(t *testing.T) {
	t.Parallel()
	want := []struct {
		id, system, scheme string
		collection         bool
	}{
		{shared.LauncherPlexMovie, systemdefs.SystemMovie, shared.SchemePlexMovie, false},
		{shared.LauncherPlexTVEpisode, systemdefs.SystemTVEpisode, shared.SchemePlexEpisode, false},
		{shared.LauncherPlexTVShow, systemdefs.SystemTVShow, shared.SchemePlexShow, true},
		{shared.LauncherPlexSong, systemdefs.SystemMusicTrack, shared.SchemePlexSong, false},
		{shared.LauncherPlexAlbum, systemdefs.SystemMusicAlbum, shared.SchemePlexAlbum, true},
		{shared.LauncherPlexArtist, systemdefs.SystemMusicArtist, shared.SchemePlexArtist, true},
	}
	launchers := Launchers()
	require.Len(t, launchers, len(want))
	for i, w := range want {
		l := launchers[i]
		assert.Equal(t, w.id, l.ID)
		assert.Equal(t, w.system, l.SystemID)
		assert.Equal(t, []string{w.scheme}, l.Schemes)
		assert.Equal(t, []string{shared.LauncherGroupPlex}, l.Groups)
		assert.True(t, l.SkipFilesystemScan)
		assert.Equal(t, platforms.InstancePlex, l.UsesRunningInstance)
		assert.Equal(t, w.collection, l.Collection != nil, w.id)
		assert.NotNil(t, l.Launch)
		assert.NotNil(t, l.Scanner)
		assert.NotNil(t, l.WaitForReady)
		for _, c := range []string{
			platforms.ControlTogglePause, platforms.ControlPause, platforms.ControlResume, platforms.ControlStop,
			platforms.ControlFastForward, platforms.ControlRewind, platforms.ControlNext, platforms.ControlPrevious,
		} {
			assert.Contains(t, l.Controls, c, w.id)
		}
		path := virtualpath.CreateVirtualPath(w.scheme, "12", "Name") + "?tmdb=1"
		assert.True(t, l.Test(nil, path), w.id)
		assert.False(t, l.Test(nil, "kodi-movie://12/Name"), w.id)
	}
}

func TestLaunchMovie(t *testing.T) {
	// Not parallel: uses the global auth config and the last launch.
	rig := newLauncherRig(t, "Living Room")
	rig.srv.addSection("1", "movie", "Movies", movie("200", "Alien", 1979, "tmdb://348"))
	l := rig.deps.newLauncher(shared.LauncherPlexMovie, systemdefs.SystemMovie, kindMovie)

	path := virtualpath.CreateVirtualPath(shared.SchemePlexMovie, "999", "Alien (1979)") + "?tmdb=348"
	proc, err := l.Launch(testConfig(t), path, nil)
	require.NoError(t, err)
	assert.Nil(t, proc)

	queues := rig.srv.seenPath("/playQueues")
	require.Len(t, queues, 1)
	assert.Equal(t, "video", queues[0].URL.Query().Get("type"))
	assert.Equal(t, libraryURI(testMachineID, "200"), queues[0].URL.Query().Get("uri"),
		"the queue is built from the item found by identity")

	plays := rig.player.calls("/player/playback/playMedia")
	require.Len(t, plays, 1)
	assert.Equal(t, "/library/metadata/200", plays[0].Query().Get("key"))

	rec := currentLaunch()
	require.NotNil(t, rec)
	assert.Equal(t, "200", rec.ratingKey)
	assert.Equal(t, "Living Room", rec.player.Name())
}

// TestStopReportsLaunchExit guards that a stop the player accepted clears
// Core's active media: the player is remote, so nothing else reports it.
func TestStopReportsLaunchExit(t *testing.T) {
	// Not parallel: uses the global auth config and the last launch.
	rig := newLauncherRig(t, "Living Room")
	rig.srv.addSection("1", "movie", "Movies", movie("200", "Alien", 1979))
	l := rig.deps.newLauncher(shared.LauncherPlexMovie, systemdefs.SystemMovie, kindMovie)
	cfg := testConfig(t)
	var exits atomic.Int32
	_, err := l.Launch(cfg, shared.SchemePlexMovie+"://200/Alien", &platforms.LaunchOptions{
		MediaExited: func() { exits.Add(1) },
	})
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, l.Controls[platforms.ControlPause].Func(ctx, cfg, platforms.ControlParams{}))
	assert.Zero(t, exits.Load(), "only stop ends the media")
	require.NoError(t, l.Controls[platforms.ControlStop].Func(ctx, cfg, platforms.ControlParams{}))
	assert.Len(t, rig.player.calls("/player/playback/stop"), 1)
	assert.Equal(t, int32(1), exits.Load())
}

func TestLaunchShowStartsOnSelectedEpisode(t *testing.T) {
	// Not parallel: uses the global auth config and the last launch.
	rig := newLauncherRig(t, "")
	rig.srv.addSection("1", "show", "TV", Metadata{RatingKey: "10", Type: "show", Title: "Firefly"})
	rig.srv.queue = []Metadata{
		episode("11", "Firefly", 1, 1, "Serenity"),
		episode("12", "Firefly", 1, 2, "Train Job"),
	}
	l := rig.deps.newLauncher(shared.LauncherPlexTVShow, systemdefs.SystemTVShow, kindShow)
	_, err := l.Launch(testConfig(t), shared.SchemePlexShow+"://10/Firefly", nil)
	require.NoError(t, err)

	queues := rig.srv.seenPath("/playQueues")
	require.Len(t, queues, 1)
	assert.Equal(t, libraryURI(testMachineID, "10"), queues[0].URL.Query().Get("uri"))
	plays := rig.player.calls("/player/playback/playMedia")
	require.Len(t, plays, 1)
	assert.Equal(t, "/library/metadata/11", plays[0].Query().Get("key"))
	assert.Equal(t, "11", currentLaunch().ratingKey)
}

func TestLaunchPlayerNotFound(t *testing.T) {
	// Not parallel: uses the global auth config and the last launch.
	rig := newLauncherRig(t, "Bedroom")
	rig.srv.addSection("1", "movie", "Movies", movie("200", "Alien", 1979))
	l := rig.deps.newLauncher(shared.LauncherPlexMovie, systemdefs.SystemMovie, kindMovie)
	_, err := l.Launch(testConfig(t), shared.SchemePlexMovie+"://200/Alien", nil)
	require.ErrorContains(t, err, `plex player "Bedroom" not found; players found: "Living Room" (Companion)`)
	assert.Nil(t, currentLaunch())
	assert.Empty(t, rig.player.calls("/player/playback/playMedia"))
}

func TestLaunchMissingItem(t *testing.T) {
	// Not parallel: uses the global auth config and the last launch.
	rig := newLauncherRig(t, "")
	l := rig.deps.newLauncher(shared.LauncherPlexMovie, systemdefs.SystemMovie, kindMovie)
	_, err := l.Launch(testConfig(t), shared.SchemePlexMovie+"://200/Alien", nil)
	require.ErrorContains(t, err, "no longer available")
	assert.Empty(t, rig.srv.seenPath("/playQueues"))
}

func TestWaitForReady(t *testing.T) {
	// Not parallel: uses the global auth config and the last launch.
	rig := newLauncherRig(t, "")
	rig.srv.addSection("1", "movie", "Movies", movie("200", "Alien", 1979))
	l := rig.deps.newLauncher(shared.LauncherPlexMovie, systemdefs.SystemMovie, kindMovie)

	err := l.WaitForReady(context.Background(), nil, nil)
	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable, "nothing was launched")

	_, err = l.Launch(testConfig(t), shared.SchemePlexMovie+"://200/Alien", nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- l.WaitForReady(ctx, nil, nil) }()

	step := func(timeline string) {
		require.NoError(t, rig.clock.BlockUntilContext(ctx, 1))
		rig.player.setTimeline(timeline)
		rig.clock.Advance(readyPollInterval)
	}
	step(`<MediaContainer><Timeline type="video" state="playing" ratingKey="150"/></MediaContainer>`)
	step(`<MediaContainer><Timeline type="video" state="buffering" ratingKey="200"/></MediaContainer>`)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("readiness never completed")
	}
}

func TestControls(t *testing.T) {
	// Not parallel: uses the global auth config and the last launch.
	rig := newLauncherRig(t, "")
	l := rig.deps.newLauncher(shared.LauncherPlexSong, systemdefs.SystemMusicTrack, kindTrack)
	cfg := testConfig(t)
	ctx := context.Background()
	run := func(control string) {
		t.Helper()
		require.NoError(t, l.Controls[control].Func(ctx, cfg, platforms.ControlParams{}), control)
	}

	run(platforms.ControlNext)
	calls := rig.player.calls("/player/playback/skipNext")
	require.Len(t, calls, 1, "with nothing launched the configured player is used")
	assert.Equal(t, "music", calls[0].Query().Get("type"))

	setLastLaunch(&launchRecord{
		player: &companionPlayer{
			http: rig.srv.Client(), name: "Living Room", machineID: "player-1",
			baseURL: rig.player.URL, clientID: testClientID,
		},
		kind:      kindMovie,
		ratingKey: "200",
	})
	for control, path := range map[string]string{
		platforms.ControlPause:       "/player/playback/pause",
		platforms.ControlResume:      "/player/playback/play",
		platforms.ControlStop:        "/player/playback/stop",
		platforms.ControlFastForward: "/player/playback/stepForward",
		platforms.ControlRewind:      "/player/playback/stepBack",
		platforms.ControlPrevious:    "/player/playback/skipPrevious",
	} {
		run(control)
		calls := rig.player.calls(path)
		require.NotEmpty(t, calls, control)
		assert.Equal(t, "video", calls[len(calls)-1].Query().Get("type"), "the launched kind is controlled")
	}

	rig.player.setTimeline(`<MediaContainer><Timeline type="video" state="playing" ratingKey="200"/></MediaContainer>`)
	run(platforms.ControlTogglePause)
	assert.Len(t, rig.player.calls("/player/playback/pause"), 2)
	rig.player.setTimeline(`<MediaContainer><Timeline type="video" state="paused" ratingKey="200"/></MediaContainer>`)
	run(platforms.ControlTogglePause)
	assert.Len(t, rig.player.calls("/player/playback/play"), 2)
}

func TestScannerAndCollectionThroughLauncher(t *testing.T) {
	// Not parallel: uses the global auth config and the last launch.
	rig := newLauncherRig(t, "")
	rig.srv.addSection("1", "artist", "Music", Metadata{
		RatingKey: "30", Type: "album", Title: "Kid A",
		ParentTitle: "Radiohead", Year: 2000,
	})
	rig.srv.leaves["30"] = []Metadata{
		track("32", "Radiohead", "Kid A", "30", 1, 2, "Kid A"),
		track("31", "Radiohead", "Kid A", "30", 1, 1, "Everything In Its Right Place"),
	}
	l := rig.deps.newLauncher(shared.LauncherPlexAlbum, systemdefs.SystemMusicAlbum, kindAlbum)
	results, err := l.Scanner(context.Background(), testConfig(t), systemdefs.SystemMusicAlbum, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Radiohead - Kid A (2000)", results[0].Name)

	items, err := l.Collection(context.Background(), testConfig(t), results[0].Path)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Radiohead - Kid A - Everything In Its Right Place", items[0].Name)
}
