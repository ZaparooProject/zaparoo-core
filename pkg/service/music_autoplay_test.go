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

package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var autoplayTrack = filepath.Join(string(filepath.Separator), "media", "usb0", "Music", "intro.mp3")

type autoplayFixture struct {
	svc      *ServiceContext
	autoplay *musicAutoplay
	clock    *clockwork.FakeClock
	fs       afero.Fs
	ready    chan struct{}
}

func newAutoplayFixture(t *testing.T) *autoplayFixture {
	t.Helper()
	svc := setupPlaylistTestEnv(t)
	clock := clockwork.NewFakeClock()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, autoplayTrack, []byte("mp3"), 0o600))
	ready := make(chan struct{})
	autoplay := newMusicAutoplay(svc, func(ctx context.Context) {
		select {
		case <-ready:
		case <-ctx.Done():
		}
	})
	autoplay.clock = clock
	autoplay.fs = fs
	svc.musicAutoplay.Store(autoplay)
	return &autoplayFixture{svc: svc, autoplay: autoplay, clock: clock, fs: fs, ready: ready}
}

func restoredBackgroundPlaylist(svc *ServiceContext) *playlists.Playlist {
	pls := playlists.NewPlaylist("/media/usb0/Music", "Music", []playlists.PlaylistItem{
		{Name: "intro", ZapScript: autoplayTrack},
	})
	pls.Slot = mediaslot.Background
	svc.State.SetBackgroundPlaylist(pls)
	return pls
}

// runAutoplay starts the autoplay, lets it past readiness, and advances the
// fake clock over the settle wait.
func (f *autoplayFixture) runAutoplay(t *testing.T) (done <-chan struct{}) {
	t.Helper()
	ctx := t.Context()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		f.autoplay.run(ctx)
	}()
	close(f.ready)
	require.NoError(t, f.clock.BlockUntilContext(ctx, 1))
	f.clock.Advance(musicAutoplaySettle)
	return finished
}

func expectQueued(t *testing.T, svc *ServiceContext) *playlists.Playlist {
	t.Helper()
	select {
	case pls := <-svc.PlaylistQueue:
		return pls
	case <-time.After(2 * time.Second):
		t.Fatal("expected the background playlist to be queued")
	}
	return nil
}

func expectNothingQueued(t *testing.T, svc *ServiceContext) {
	t.Helper()
	select {
	case pls := <-svc.PlaylistQueue:
		t.Fatalf("unexpected playlist update: %+v", pls)
	default:
	}
}

func TestMusicAutoplay_PlaysRestoredBackgroundPlaylist(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	restored := restoredBackgroundPlaylist(f.svc)

	<-f.runAutoplay(t)
	queued := expectQueued(t, f.svc)
	assert.Equal(t, restored.ID, queued.ID)
	assert.Equal(t, mediaslot.Background, queued.Slot)
	assert.True(t, queued.Playing)
}

func TestMusicAutoplay_WaitsForReadinessAndSettle(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	restoredBackgroundPlaylist(f.svc)

	ctx := t.Context()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		f.autoplay.run(ctx)
	}()
	expectNothingQueued(t, f.svc)

	close(f.ready)
	require.NoError(t, f.clock.BlockUntilContext(ctx, 1))
	f.clock.Advance(musicAutoplaySettle - time.Millisecond)
	expectNothingQueued(t, f.svc)
	f.clock.Advance(time.Millisecond)
	<-finished
	expectQueued(t, f.svc)
}

func TestMusicAutoplay_NoBackgroundPlaylistDoesNothing(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)

	<-f.runAutoplay(t)
	expectNothingQueued(t, f.svc)
}

func TestMusicAutoplay_AlreadyPlayingIsLeftAlone(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	pls := restoredBackgroundPlaylist(f.svc)
	f.svc.State.SetBackgroundPlaylist(playlists.Play(*pls))

	<-f.runAutoplay(t)
	expectNothingQueued(t, f.svc)
}

func TestMusicAutoplay_GameRunningStartsMusicWhenItExits(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	restoredBackgroundPlaylist(f.svc)
	f.svc.State.SetOnMediaStopHook(func() { resumeBackgroundAfterMediaStop(f.svc) })
	f.svc.State.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "game.sfc", "Game", "core"))

	<-f.runAutoplay(t)
	expectNothingQueued(t, f.svc)
	assert.True(t, f.autoplay.pending.Load(), "the music waits for the game")

	f.svc.State.SetActiveMedia(nil)
	require.NoError(t, f.clock.BlockUntilContext(t.Context(), 1))
	f.clock.Advance(musicAutoplaySettle)
	queued := expectQueued(t, f.svc)
	assert.True(t, queued.Playing)
	assert.False(t, f.autoplay.pending.Load())
}

func TestMusicAutoplay_GameReplacedKeepsMusicPending(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	restoredBackgroundPlaylist(f.svc)
	f.svc.State.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "game.sfc", "Game", "core"))

	<-f.runAutoplay(t)
	require.True(t, f.autoplay.pending.Load())

	// The stop of one game replaced by another: by the time the music would
	// start, the next game is running.
	f.autoplay.mediaStopped()
	f.svc.State.SetActiveMedia(models.NewActiveMedia("NES", "NES", "game.nes", "Other", "core"))
	require.NoError(t, f.clock.BlockUntilContext(t.Context(), 1))
	f.clock.Advance(musicAutoplaySettle)
	require.Eventually(t, f.autoplay.pending.Load, 2*time.Second, time.Millisecond,
		"the music stays pending for the new game")
	expectNothingQueued(t, f.svc)
}

func TestMusicAutoplay_GameRunningWithoutPauseOnLaunchPlaysNow(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	cfg := &config.Instance{}
	cfg.SetAudioPauseOnLaunch(false)
	f.svc.Config = cfg
	restoredBackgroundPlaylist(f.svc)
	f.svc.State.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "game.sfc", "Game", "core"))

	<-f.runAutoplay(t)
	expectQueued(t, f.svc)
	assert.False(t, f.autoplay.pending.Load())
}

func TestMusicAutoplay_RetriesOnceForALateTrack(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	require.NoError(t, f.fs.Remove(autoplayTrack))
	restoredBackgroundPlaylist(f.svc)

	done := f.runAutoplay(t)
	require.NoError(t, f.clock.BlockUntilContext(t.Context(), 1))
	expectNothingQueued(t, f.svc)

	// The drive mounts while autoplay waits.
	require.NoError(t, afero.WriteFile(f.fs, autoplayTrack, []byte("mp3"), 0o600))
	f.clock.Advance(musicAutoplayRetryDelay)
	<-done
	expectQueued(t, f.svc)
}

func TestMusicAutoplay_GivesUpWhenTheTrackStaysMissing(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	require.NoError(t, f.fs.Remove(autoplayTrack))
	restoredBackgroundPlaylist(f.svc)

	done := f.runAutoplay(t)
	require.NoError(t, f.clock.BlockUntilContext(t.Context(), 1))
	f.clock.Advance(musicAutoplayRetryDelay)
	<-done
	expectNothingQueued(t, f.svc)
}

func TestMusicAutoplay_StopsWithTheService(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	restoredBackgroundPlaylist(f.svc)

	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		f.autoplay.run(ctx)
	}()
	close(f.ready)
	require.NoError(t, f.clock.BlockUntilContext(ctx, 1))
	cancel()
	<-finished
	expectNothingQueued(t, f.svc)
}

func TestMusicAutoplay_MediaStoppedWithoutPendingDoesNothing(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	restoredBackgroundPlaylist(f.svc)

	f.autoplay.mediaStopped()
	var nilAutoplay *musicAutoplay
	nilAutoplay.mediaStopped()
	expectNothingQueued(t, f.svc)
}

func TestMusicAutoplay_MissingTrackReadsTheLaunchPath(t *testing.T) {
	t.Parallel()
	f := newAutoplayFixture(t)
	relative := filepath.Join(string(filepath.Separator), "mock", "roms", "Music", "late.mp3")

	tests := []struct {
		name     string
		script   string
		missing  bool
		absolute bool
	}{
		{"launch command with the track present", "**launch:" + autoplayTrack + "?system=Audio", false, false},
		{"launch command with the track missing", "**launch:/media/usb1/gone.mp3?system=Audio", true, true},
		{"relative track under a games folder", "**launch:Music/late.mp3", true, false},
		{"not a launch", "**stop", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pls := playlists.NewPlaylist("", "Music", []playlists.PlaylistItem{{Name: "t", ZapScript: tt.script}})
			_, missing, absolute := f.autoplay.missingTrack(pls)
			assert.Equal(t, tt.missing, missing)
			assert.Equal(t, tt.absolute, absolute)
		})
	}

	// The drive mounts: the relative track is found under the games folder.
	require.NoError(t, afero.WriteFile(f.fs, relative, []byte("mp3"), 0o600))
	pls := playlists.NewPlaylist("", "Music", []playlists.PlaylistItem{
		{Name: "t", ZapScript: "**launch:Music/late.mp3"},
	})
	_, missing, _ := f.autoplay.missingTrack(pls)
	assert.False(t, missing)
}
