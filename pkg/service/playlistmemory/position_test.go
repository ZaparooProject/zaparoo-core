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

package playlistmemory_test

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlistmemory"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const bookPath = "/media/fat/Audio/book.mp3"

// fakePlayback is a primary slot whose state a test sets.
type fakePlayback struct {
	audio.PlaybackManager
	state audio.PlaybackState
	mu    syncutil.Mutex
}

func (f *fakePlayback) State(string) audio.PlaybackState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakePlayback) set(state audio.PlaybackState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = state
}

type trackerRig struct {
	mem      *playlistmemory.Memory
	store    database.PlaylistMemoryStore
	profile  *profileSwitch
	playback *fakePlayback
	clock    *clockwork.FakeClock
	tracker  *playlistmemory.PositionTracker
	media    *models.ActiveMedia
	mu       syncutil.Mutex
}

func newTrackerRig(t *testing.T) *trackerRig {
	t.Helper()
	mem, store, profile := newMemory(t)
	rig := &trackerRig{
		mem: mem, store: store, profile: profile, playback: &fakePlayback{}, clock: clockwork.NewFakeClock(),
	}
	rig.tracker = playlistmemory.NewPositionTracker(mem, rig.playback, rig.activeMedia, rig.clock)
	return rig
}

func (r *trackerRig) activeMedia() *models.ActiveMedia {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.media
}

func (r *trackerRig) play(path string, position, length time.Duration, paused bool) {
	r.mu.Lock()
	r.media = models.NewActiveMedia(systemdefs.SystemAudio, "Audio", path, "Book", platforms.NativeAudioLauncherID)
	r.mu.Unlock()
	r.playback.set(audio.PlaybackState{
		Path: path, Position: position, Duration: length, Playing: !paused, Paused: paused,
	})
}

func (r *trackerRig) stop() {
	r.mu.Lock()
	r.media = nil
	r.mu.Unlock()
	r.playback.set(audio.PlaybackState{})
}

func (r *trackerRig) saved(t *testing.T, profileID, path string) (time.Duration, bool) {
	t.Helper()
	r.mem.Flush(context.Background())
	pos, found, err := r.store.GetMediaPosition(profileID, systemdefs.SystemAudio, path)
	require.NoError(t, err)
	return time.Duration(pos.PositionMs) * time.Millisecond, found
}

func TestTrackerThrottlesWritesWhilePlaying(t *testing.T) {
	t.Parallel()
	rig := newTrackerRig(t)

	rig.play(bookPath, 5*time.Minute, time.Hour, false)
	rig.tracker.Sample()
	rig.clock.Advance(29 * time.Second)
	rig.play(bookPath, 5*time.Minute+29*time.Second, time.Hour, false)
	rig.tracker.Sample()
	_, found := rig.saved(t, "", bookPath)
	assert.False(t, found, "nothing is written before the save interval")

	rig.clock.Advance(time.Second)
	rig.play(bookPath, 5*time.Minute+30*time.Second, time.Hour, false)
	rig.tracker.Sample()
	pos, found := rig.saved(t, "", bookPath)
	require.True(t, found)
	assert.Equal(t, 5*time.Minute+30*time.Second, pos)
}

func TestTrackerSavesOnPauseAndStop(t *testing.T) {
	t.Parallel()
	rig := newTrackerRig(t)

	rig.play(bookPath, 12*time.Minute, time.Hour, false)
	rig.tracker.Sample()
	rig.clock.Advance(5 * time.Second)
	rig.play(bookPath, 12*time.Minute+5*time.Second, time.Hour, true)
	rig.tracker.Sample()
	pos, found := rig.saved(t, "", bookPath)
	require.True(t, found, "a pause is saved at once")
	assert.Equal(t, 12*time.Minute+5*time.Second, pos)

	rig.play(bookPath, 20*time.Minute, time.Hour, false)
	rig.tracker.Sample()
	rig.stop()
	rig.tracker.Sample()
	pos, _ = rig.saved(t, "", bookPath)
	assert.Equal(t, 20*time.Minute, pos, "a stop saves the last position seen")
}

func TestTrackerSavesOnTrackChangeForTheLaunchingProfile(t *testing.T) {
	t.Parallel()
	rig := newTrackerRig(t)

	rig.profile.set("kid")
	rig.play(bookPath, 15*time.Minute, time.Hour, false)
	rig.tracker.Sample()
	rig.profile.set("")
	rig.play("/media/fat/Audio/other.mp3", time.Minute, time.Hour, false)
	rig.tracker.Sample()

	pos, found := rig.saved(t, "kid", bookPath)
	require.True(t, found, "the track belongs to the profile active when it started")
	assert.Equal(t, 15*time.Minute, pos)
	_, found = rig.saved(t, "", bookPath)
	assert.False(t, found)
}

func TestTrackerIgnoresShortTracksAndOtherLaunchers(t *testing.T) {
	t.Parallel()
	rig := newTrackerRig(t)

	rig.play("/song.mp3", 2*time.Minute, 4*time.Minute, true)
	rig.tracker.Sample()
	_, found := rig.saved(t, "", "/song.mp3")
	assert.False(t, found, "a song always plays from the start")

	rig.mu.Lock()
	rig.media = models.NewActiveMedia("NES", "NES", bookPath, "Game", "retroarch")
	rig.mu.Unlock()
	rig.playback.set(audio.PlaybackState{Path: bookPath, Position: 20 * time.Minute, Duration: time.Hour, Paused: true})
	rig.tracker.Sample()
	_, found = rig.saved(t, "", bookPath)
	assert.False(t, found, "only native audio is tracked")
}

func TestTrackerForgetsNearEitherEndAndOnFinish(t *testing.T) {
	t.Parallel()
	rig := newTrackerRig(t)

	rig.play(bookPath, 20*time.Minute, time.Hour, true)
	rig.tracker.Sample()
	_, found := rig.saved(t, "", bookPath)
	require.True(t, found)

	rig.play(bookPath, 10*time.Second, time.Hour, false)
	rig.tracker.Sample()
	rig.play(bookPath, 10*time.Second, time.Hour, true)
	rig.tracker.Sample()
	_, found = rig.saved(t, "", bookPath)
	assert.False(t, found, "rewinding to the start forgets the old position")

	rig.play(bookPath, 30*time.Minute, time.Hour, true)
	rig.tracker.Sample()
	rig.play(bookPath, 30*time.Minute, time.Hour, false)
	rig.tracker.Sample()
	rig.tracker.Finished(bookPath)
	_, found = rig.saved(t, "", bookPath)
	assert.False(t, found, "a finished track starts over next time")
}

func TestTrackerStopSavesBeforeShutdown(t *testing.T) {
	t.Parallel()
	rig := newTrackerRig(t)
	rig.tracker.Start()

	rig.play(bookPath, 42*time.Minute, time.Hour, false)
	rig.tracker.Stop()
	pos, found := rig.saved(t, "", bookPath)
	require.True(t, found)
	assert.Equal(t, 42*time.Minute, pos)
}

func TestResumeAt(t *testing.T) {
	t.Parallel()
	rig := newTrackerRig(t)

	rig.mem.SavePosition("", systemdefs.SystemAudio, bookPath, 25*time.Minute, time.Hour)
	rig.mem.SavePosition("", systemdefs.SystemAudio, "/end.mp3", time.Hour-10*time.Second, time.Hour)
	rig.mem.Flush(context.Background())

	pos, length := rig.mem.ResumeAt(systemdefs.SystemAudio, bookPath)
	assert.Equal(t, 25*time.Minute, pos)
	assert.Equal(t, time.Hour, length)

	pos, _ = rig.mem.ResumeAt(systemdefs.SystemAudio, "/end.mp3")
	assert.Zero(t, pos, "a position that is not resumable is not offered")

	rig.profile.set("kid")
	pos, _ = rig.mem.ResumeAt(systemdefs.SystemAudio, bookPath)
	assert.Zero(t, pos, "another profile has its own positions")
}
