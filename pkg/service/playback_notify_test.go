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
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/trackmeta"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// changeReportingPlayback is a playback manager that keeps each slot's state
// and reports changes the way LongformPlaybackManager does.
type changeReportingPlayback struct {
	onChange func(slot string)
	states   map[string]audio.PlaybackState
	mu       syncutil.Mutex
}

func newChangeReportingPlayback() *changeReportingPlayback {
	return &changeReportingPlayback{states: make(map[string]audio.PlaybackState)}
}

func (p *changeReportingPlayback) SetChangeCallback(fn func(slot string)) {
	p.mu.Lock()
	p.onChange = fn
	p.mu.Unlock()
}

// update changes slot's state with fn and reports it, outside the lock.
func (p *changeReportingPlayback) update(slot string, fn func(*audio.PlaybackState)) {
	p.mu.Lock()
	st := p.states[slot]
	fn(&st)
	p.states[slot] = st
	onChange := p.onChange
	p.mu.Unlock()
	if onChange != nil {
		onChange(slot)
	}
}

func (p *changeReportingPlayback) Play(slot, path string, _ audio.PlaybackOptions) error {
	p.update(slot, func(st *audio.PlaybackState) {
		*st = audio.PlaybackState{Path: path, Playing: true, Duration: 3 * time.Minute}
	})
	return nil
}

func (p *changeReportingPlayback) Stop(slot string) error {
	p.mu.Lock()
	delete(p.states, slot)
	p.mu.Unlock()
	return nil
}

func (p *changeReportingPlayback) Pause(slot string) error {
	p.update(slot, func(st *audio.PlaybackState) { st.Paused, st.Playing = true, false })
	return nil
}

func (p *changeReportingPlayback) Resume(slot string) error {
	p.update(slot, func(st *audio.PlaybackState) { st.Paused, st.Playing = false, true })
	return nil
}

func (p *changeReportingPlayback) TogglePause(slot string) error {
	p.update(slot, func(st *audio.PlaybackState) { st.Paused, st.Playing = !st.Paused, st.Paused })
	return nil
}

func (p *changeReportingPlayback) Seek(slot string, offset time.Duration) error {
	p.update(slot, func(st *audio.PlaybackState) { st.Position += offset })
	return nil
}

func (*changeReportingPlayback) SetVolume(string, float64) error { return nil }

func (p *changeReportingPlayback) State(slot string) audio.PlaybackState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.states[slot]
}

// tagsRead stores tags for slot the way the manager does once they are read.
func (p *changeReportingPlayback) tagsRead(slot string, meta *trackmeta.Meta) {
	p.update(slot, func(st *audio.PlaybackState) { st.Meta = meta })
}

type playbackNotifyEnv struct {
	st        *state.State
	pm        *changeReportingPlayback
	published chan models.Notification
}

func (e *playbackNotifyEnv) requestEnv() requests.RequestEnv {
	return requests.RequestEnv{State: e.st, PlaybackManager: e.pm}
}

func startTestPlaybackNotifier(t *testing.T) *playbackNotifyEnv {
	t.Helper()
	st, ns := state.NewState(nil, "boot")
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	env := &playbackNotifyEnv{
		st:        st,
		pm:        newChangeReportingPlayback(),
		published: make(chan models.Notification, 16),
	}
	notifier := newPlaybackNotifier(func(slot string) *models.ActiveMediaResponse {
		reqEnv := env.requestEnv()
		return methods.ActiveMediaEntry(&reqEnv, slot)
	}, env.pm, func(n models.Notification) { env.published <- n })
	wirePlaybackNotifier(env.pm, st, notifier)
	go notifier.run(st.GetContext())
	return env
}

func nextMediaPlayback(
	t *testing.T, published <-chan models.Notification,
) (models.Notification, models.ActiveMediaResponse) {
	t.Helper()
	select {
	case n := <-published:
		require.Equal(t, models.NotificationMediaPlayback, n.Method)
		var params models.ActiveMediaResponse
		require.NoError(t, json.Unmarshal(n.Params, &params))
		return n, params
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for media.playback")
	}
	return models.Notification{}, models.ActiveMediaResponse{}
}

func assertNoMediaPlayback(t *testing.T, published <-chan models.Notification) {
	t.Helper()
	select {
	case n := <-published:
		t.Fatalf("unexpected notification %s: %s", n.Method, n.Params)
	case <-time.After(100 * time.Millisecond):
	}
}

func nativeAudioMedia(path string) *models.ActiveMedia {
	media := models.NewActiveMedia("Audio", "Audio", path, "Intro", platforms.NativeAudioLauncherID)
	media.LauncherControls = []string{"pause", "resume", "seek"}
	return media
}

// startTrack plays path in slot and publishes its media, in the order the
// native audio launcher does, and returns the start's notification.
func startTrack(t *testing.T, env *playbackNotifyEnv, slot, path string) models.ActiveMediaResponse {
	t.Helper()
	require.NoError(t, env.pm.Play(slot, path, audio.PlaybackOptions{}))
	if slot == mediaslot.Background {
		env.st.SetBackgroundMedia(nativeAudioMedia(path))
	} else {
		env.st.SetActiveMedia(nativeAudioMedia(path))
	}
	_, params := nextMediaPlayback(t, env.published)
	return params
}

func TestPlaybackNotifier_ReportsStartOnceMediaIsPublished(t *testing.T) {
	t.Parallel()
	env := startTestPlaybackNotifier(t)
	const path = "/music/01 Intro.mp3"

	// The track is live before its media is published; that moment has no
	// media to report.
	require.NoError(t, env.pm.Play(mediaslot.Background, path, audio.PlaybackOptions{}))
	assertNoMediaPlayback(t, env.published)

	env.st.SetBackgroundMedia(nativeAudioMedia(path))
	n, params := nextMediaPlayback(t, env.published)
	assert.Equal(t, mediaslot.Background, n.CoalesceKey, "merged per slot")
	assert.Equal(t, mediaslot.Background, params.Slot)
	assert.Equal(t, path, params.Path)
	assert.Equal(t, path, params.ZapScript)
	assert.Equal(t, models.MediaPlaybackStatePlaying, params.PlaybackState)
	require.NotNil(t, params.DurationMs)
	assert.Equal(t, int64(180000), *params.DurationMs)
	require.NotNil(t, params.PositionMs)
	assert.Equal(t, int64(0), *params.PositionMs)
}

func TestPlaybackNotifier_ReportsPauseResumeSeekAndTags(t *testing.T) {
	t.Parallel()
	env := startTestPlaybackNotifier(t)
	const path = "/music/01 Intro.mp3"
	startTrack(t, env, mediaslot.Background, path)

	require.NoError(t, env.pm.Pause(mediaslot.Background))
	_, params := nextMediaPlayback(t, env.published)
	assert.Equal(t, models.MediaPlaybackStatePaused, params.PlaybackState)

	require.NoError(t, env.pm.Resume(mediaslot.Background))
	_, params = nextMediaPlayback(t, env.published)
	assert.Equal(t, models.MediaPlaybackStatePlaying, params.PlaybackState)

	require.NoError(t, env.pm.TogglePause(mediaslot.Background))
	_, params = nextMediaPlayback(t, env.published)
	assert.Equal(t, models.MediaPlaybackStatePaused, params.PlaybackState)

	require.NoError(t, env.pm.Seek(mediaslot.Background, 30*time.Second))
	_, params = nextMediaPlayback(t, env.published)
	require.NotNil(t, params.PositionMs)
	assert.Equal(t, int64(30000), *params.PositionMs)
	assert.Nil(t, params.Track, "no tags before they are read")

	env.pm.tagsRead(mediaslot.Background, &trackmeta.Meta{Title: "Intro", Artist: "Someone", HasArt: true})
	_, params = nextMediaPlayback(t, env.published)
	require.NotNil(t, params.Track)
	assert.Equal(t, "Intro", params.Track.Title)
	assert.Equal(t, "Someone", params.Track.Artist)
	assert.True(t, params.Track.HasArt)
}

func TestPlaybackNotifier_ReportsPrimaryNativeAudio(t *testing.T) {
	t.Parallel()
	env := startTestPlaybackNotifier(t)
	const path = "/music/Podcast.mp3"
	params := startTrack(t, env, mediaslot.Primary, path)
	assert.Equal(t, mediaslot.Primary, params.Slot)

	require.NoError(t, env.pm.Pause(mediaslot.Primary))
	n, params := nextMediaPlayback(t, env.published)
	assert.Equal(t, mediaslot.Primary, n.CoalesceKey)
	assert.Equal(t, models.MediaPlaybackStatePaused, params.PlaybackState)
}

func TestPlaybackNotifier_IgnoresOtherMedia(t *testing.T) {
	t.Parallel()
	env := startTestPlaybackNotifier(t)

	// A game in the primary slot is not native audio.
	env.st.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "/roms/game.sfc", "Game", "SNES"))
	assertNoMediaPlayback(t, env.published)

	// A track that replaced the published one but is not published yet is
	// not reported against the old track's media.
	startTrack(t, env, mediaslot.Background, "/music/01.mp3")
	require.NoError(t, env.pm.Play(mediaslot.Background, "/music/02.mp3", audio.PlaybackOptions{}))
	assertNoMediaPlayback(t, env.published)
}

func TestPlaybackNotifier_PayloadMatchesMediaActive(t *testing.T) {
	t.Parallel()
	env := startTestPlaybackNotifier(t)
	const path = "/music/01 Intro.mp3"
	startTrack(t, env, mediaslot.Background, path)
	env.pm.tagsRead(mediaslot.Background, &trackmeta.Meta{Title: "Intro", TrackNumber: 1})
	n, _ := nextMediaPlayback(t, env.published)

	reqEnv := env.requestEnv()
	reqEnv.Params = json.RawMessage(`{"slot":"background"}`)
	active, err := methods.HandleActiveMedia(reqEnv)
	require.NoError(t, err)
	want, err := json.Marshal(active)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(n.Params), "media.playback carries what media.active returns")

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(n.Params, &fields))
	for _, key := range []string{
		"slot", "positionMs", "durationMs", "playbackState", "launcherControls", "track", "zapScript",
		"mediaPath", "mediaName", "launcherId", "systemId", "systemName", "started",
	} {
		assert.Contains(t, fields, key)
	}
}

func TestPlaybackNotifier_SignalNeverBlocks(t *testing.T) {
	t.Parallel()
	notifier := newPlaybackNotifier(nil, nil, nil)
	for range 10 {
		notifier.Signal(mediaslot.Primary)
		notifier.Signal(mediaslot.Background)
		notifier.Signal("nowhere")
	}
	var nilNotifier *playbackNotifier
	nilNotifier.Signal(mediaslot.Primary)
}
