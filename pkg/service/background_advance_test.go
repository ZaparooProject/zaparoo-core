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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEngineSvc is newAdvanceTestSvc with the auto-advance engine running, the
// way the service wires it.
func newEngineSvc(t *testing.T) *ServiceContext {
	t.Helper()
	svc, cleanup := newAdvanceTestSvc(t)
	svc.AutoAdvance = startAutoAdvance(svc, false)
	t.Cleanup(func() {
		svc.AutoAdvance.Stop()
		cleanup()
	})
	return svc
}

func makeMultiTrackPlaylist(idx int, loop, loopOne bool) *playlists.Playlist {
	return &playlists.Playlist{
		ID:      "bg-list",
		Name:    "bg",
		Slot:    mediaslot.Background,
		Items:   []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}, {ZapScript: "c"}},
		Index:   idx,
		Playing: true,
		Loop:    loop,
		LoopOne: loopOne,
	}
}

// playBackgroundItem plays pls's current item the way the playlist worker
// does: the launch is announced and the track it publishes belongs to it.
func playBackgroundItem(svc *ServiceContext, pls *playlists.Playlist, track string) {
	svc.State.SetBackgroundPlaylist(pls)
	svc.AutoAdvance.ItemLaunching(pls)
	svc.State.SetBackgroundMedia(models.NewActiveMedia(
		"Audio", "Audio", track+".mp3", track, platforms.NativeAudioLauncherID,
	))
	svc.AutoAdvance.ItemLaunched(pls, nil, false)
	svc.AutoAdvance.Sync()
}

// drainBackground ends the background track the way native audio does when
// it plays to its end.
func drainBackground(svc *ServiceContext) {
	registrar := &testDrainCallbackRegistrar{}
	wireNativeAudioDrainCallbacks(registrar, svc)
	registrar.callbacks[mediaslot.Background](true)
	svc.AutoAdvance.Sync()
}

func nextQueued(t *testing.T, svc *ServiceContext) *playlists.Playlist {
	t.Helper()
	select {
	case got := <-svc.PlaylistQueue:
		return got
	case <-time.After(time.Second):
		t.Fatal("timeout: expected playlist on queue")
		return nil
	}
}

func requireNothingQueued(t *testing.T, svc *ServiceContext) {
	t.Helper()
	select {
	case got := <-svc.PlaylistQueue:
		t.Fatalf("unexpected enqueue: %+v", got)
	default:
	}
}

func TestBackgroundFinish_RepeatOffAdvancesWithinPlaylist(t *testing.T) {
	t.Parallel()

	svc := newEngineSvc(t)
	playBackgroundItem(svc, makeMultiTrackPlaylist(0, false, false), "a")
	drainBackground(svc)

	got := nextQueued(t, svc)
	assert.Equal(t, 1, got.Index, "should advance to index 1")
	assert.True(t, got.Playing)
	assert.False(t, got.ForceRelaunch)
	assert.Equal(t, mediaslot.Background, got.Slot)
}

func TestBackgroundFinish_RepeatOffClearsAtEnd(t *testing.T) {
	t.Parallel()

	svc := newEngineSvc(t)
	playBackgroundItem(svc, makeMultiTrackPlaylist(2, false, false), "c")
	drainBackground(svc)

	got := nextQueued(t, svc)
	assert.True(t, got.Clear, "the finished playlist is closed")
	assert.Equal(t, mediaslot.Background, got.Slot)
	assert.Nil(t, svc.State.BackgroundMedia(), "background media is cleared by the drain")
}

func TestBackgroundFinish_RepeatAllWrapsToFirst(t *testing.T) {
	t.Parallel()

	svc := newEngineSvc(t)
	playBackgroundItem(svc, makeMultiTrackPlaylist(2, true, false), "c")
	drainBackground(svc)

	got := nextQueued(t, svc)
	assert.Equal(t, 0, got.Index, "should wrap back to index 0")
	assert.True(t, got.Playing)
	assert.True(t, got.Loop)
	assert.False(t, got.ForceRelaunch)
}

func TestBackgroundFinish_RepeatOneSameTrack(t *testing.T) {
	t.Parallel()

	svc := newEngineSvc(t)
	playBackgroundItem(svc, makeMultiTrackPlaylist(1, false, true), "b")
	drainBackground(svc)

	got := nextQueued(t, svc)
	assert.Equal(t, 1, got.Index, "should stay on same index")
	assert.True(t, got.LoopOne)
	assert.True(t, got.ForceRelaunch, "ForceRelaunch must be set to defeat dedup")
}

// A background track from an untrusted playlist is still untrusted when it
// repeats or advances on its own, with nothing the user did to re-establish
// trust.
func TestBackgroundFinish_KeepsUnsafe(t *testing.T) {
	t.Parallel()

	for name, pls := range map[string]*playlists.Playlist{
		"repeat one": makeMultiTrackPlaylist(1, false, true),
		"advance":    makeMultiTrackPlaylist(0, false, false),
		"wrap":       makeMultiTrackPlaylist(2, true, false),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc := newEngineSvc(t)
			pls.Unsafe = true
			playBackgroundItem(svc, pls, "track")
			drainBackground(svc)

			assert.True(t, nextQueued(t, svc).Unsafe, "an untrusted background playlist stays untrusted")
		})
	}
}

func TestBackgroundFinish_RepeatAllSingleItemUsesForceRelaunch(t *testing.T) {
	t.Parallel()

	svc := newEngineSvc(t)
	pls := &playlists.Playlist{
		ID:      "single",
		Name:    "single",
		Slot:    mediaslot.Background,
		Items:   []playlists.PlaylistItem{{ZapScript: "only-track"}},
		Playing: true,
		Loop:    true,
	}
	playBackgroundItem(svc, pls, "only-track")
	drainBackground(svc)

	got := nextQueued(t, svc)
	assert.Equal(t, 0, got.Index, "single-item loop stays at index 0")
	assert.True(t, got.Loop)
	assert.True(t, got.ForceRelaunch, "single-item loop needs ForceRelaunch")
}

func TestBackgroundFinish_SingleTrackNotAPlaylistJustClears(t *testing.T) {
	t.Parallel()

	svc := newEngineSvc(t)
	svc.State.SetBackgroundMedia(models.NewActiveMedia(
		"Audio", "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID,
	))
	require.NotNil(t, svc.State.BackgroundMedia())

	drainBackground(svc)

	assert.Nil(t, svc.State.BackgroundMedia(), "single-track end must clear background media")
	requireNothingQueued(t, svc)
}

// Only a track the playlist launched is the playlist's to advance from.
func TestBackgroundFinish_UnboundTrackDoesNotAdvance(t *testing.T) {
	t.Parallel()

	svc := newEngineSvc(t)
	svc.State.SetBackgroundPlaylist(makeMultiTrackPlaylist(0, false, false))
	svc.State.SetBackgroundMedia(models.NewActiveMedia(
		"Audio", "Audio", "other.mp3", "other", platforms.NativeAudioLauncherID,
	))
	svc.AutoAdvance.Sync()

	drainBackground(svc)

	requireNothingQueued(t, svc)
}

func TestBackgroundStopAndReplaceNeverAdvance(t *testing.T) {
	t.Parallel()

	t.Run("stopped", func(t *testing.T) {
		t.Parallel()
		svc := newEngineSvc(t)
		playBackgroundItem(svc, makeMultiTrackPlaylist(0, false, false), "a")
		svc.State.SetBackgroundMedia(nil)
		svc.AutoAdvance.Sync()
		requireNothingQueued(t, svc)
	})

	t.Run("replaced", func(t *testing.T) {
		t.Parallel()
		svc := newEngineSvc(t)
		playBackgroundItem(svc, makeMultiTrackPlaylist(0, false, false), "a")
		svc.State.SetBackgroundMedia(models.NewActiveMedia(
			"Audio", "Audio", "z.mp3", "zzz", platforms.NativeAudioLauncherID,
		))
		svc.AutoAdvance.Sync()
		requireNothingQueued(t, svc)
	})
}

func TestBackgroundFinish_GuardsStopAdvance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		change func(*ServiceContext, *playlists.Playlist)
		name   string
	}{
		{name: "paused", change: func(svc *ServiceContext, p *playlists.Playlist) {
			svc.State.SetBackgroundPlaylist(playlists.Pause(*p))
		}},
		{name: "moved to another item", change: func(svc *ServiceContext, p *playlists.Playlist) {
			svc.State.SetBackgroundPlaylist(playlists.Goto(*p, 2))
		}},
		{name: "replaced by another playlist", change: func(svc *ServiceContext, p *playlists.Playlist) {
			other := *p
			other.ID = "other"
			svc.State.SetBackgroundPlaylist(&other)
		}},
		{name: "paused by a failure", change: func(svc *ServiceContext, p *playlists.Playlist) {
			svc.State.PausePlaylistIfCurrent(mediaslot.Background, p)
		}},
		{name: "cleared", change: func(svc *ServiceContext, _ *playlists.Playlist) {
			svc.State.SetBackgroundPlaylist(nil)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newEngineSvc(t)
			pls := makeMultiTrackPlaylist(0, false, false)
			playBackgroundItem(svc, pls, "a")
			tt.change(svc, pls)
			drainBackground(svc)
			requireNothingQueued(t, svc)
		})
	}
}
