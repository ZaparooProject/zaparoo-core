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
	"errors"
	"sync/atomic"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mediaEndRecorder struct {
	ends []state.MediaEnd
	mu   syncutil.Mutex
}

func (*mediaEndRecorder) MediaStarted(state.MediaStart) {}

//nolint:gocritic // state.MediaObserver passes events by value
func (r *mediaEndRecorder) MediaEnded(event state.MediaEnd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, event)
}

// reasons lists the end reasons seen for one slot.
func (r *mediaEndRecorder) reasons(slot string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for i := range r.ends {
		if r.ends[i].Slot == slot {
			out = append(out, r.ends[i].Reason)
		}
	}
	return out
}

func newRecordedService(t *testing.T) (*ServiceContext, *mediaEndRecorder) {
	t.Helper()
	st, ns := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
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
	rec := &mediaEndRecorder{}
	st.AddMediaObserver(rec)
	return &ServiceContext{State: st, PlaylistQueue: make(chan *playlists.Playlist, 1)}, rec
}

func nativeTrack(name string) *models.ActiveMedia {
	return models.NewActiveMedia("Audio", "Audio", name+".mp3", name, platforms.NativeAudioLauncherID)
}

func TestNativeAudioDrain_ReportsFinished(t *testing.T) {
	t.Parallel()

	svc, rec := newRecordedService(t)
	svc.State.SetActiveMedia(nativeTrack("primary"))
	svc.State.SetBackgroundMedia(nativeTrack("background"))

	registrar := &testDrainCallbackRegistrar{}
	wireNativeAudioDrainCallbacks(registrar, svc)
	registrar.callbacks[mediaslot.Primary](true)
	registrar.callbacks[mediaslot.Background](true)

	assert.Equal(t, []string{models.MediaStopReasonFinished}, rec.reasons(mediaslot.Primary))
	assert.Equal(t, []string{models.MediaStopReasonFinished}, rec.reasons(mediaslot.Background))
}

func TestNativeAudioDrain_NonNaturalReportsNothing(t *testing.T) {
	t.Parallel()

	svc, rec := newRecordedService(t)
	svc.State.SetActiveMedia(nativeTrack("primary"))
	svc.State.SetBackgroundMedia(nativeTrack("background"))

	registrar := &testDrainCallbackRegistrar{}
	wireNativeAudioDrainCallbacks(registrar, svc)
	registrar.callbacks[mediaslot.Primary](false)
	registrar.callbacks[mediaslot.Background](false)

	assert.Empty(t, rec.reasons(mediaslot.Primary))
	assert.Empty(t, rec.reasons(mediaslot.Background))
}

func TestNativeAudioDrain_PrimaryKeepsOtherLaunchersMediaAndReportsNothing(t *testing.T) {
	t.Parallel()

	svc, rec := newRecordedService(t)
	svc.State.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "game.sfc", "Game", "emulator"))

	registrar := &testDrainCallbackRegistrar{}
	wireNativeAudioDrainCallbacks(registrar, svc)
	registrar.callbacks[mediaslot.Primary](true)

	assert.NotNil(t, svc.State.ActiveMedia())
	assert.Empty(t, rec.reasons(mediaslot.Primary))
}

func TestStopNativeAudioPrimaryMedia_RecordsStoppedOnlyForNativeAudio(t *testing.T) {
	t.Parallel()

	t.Run("native audio track", func(t *testing.T) {
		t.Parallel()
		svc, rec := newRecordedService(t)
		svc.State.SetActiveMedia(nativeTrack("track"))

		require.NoError(t, stopNativeAudioPrimaryMedia(context.Background(), svc, func() error { return nil }))
		assert.Equal(t, []string{models.MediaStopReasonStopped}, rec.reasons(mediaslot.Primary))
	})

	t.Run("a game that exits later is not mislabelled", func(t *testing.T) {
		t.Parallel()
		svc, rec := newRecordedService(t)
		svc.State.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "game.sfc", "Game", "emulator"))

		require.NoError(t, stopNativeAudioPrimaryMedia(context.Background(), svc, func() error { return nil }))
		svc.State.SetActiveMedia(nil)
		assert.Equal(t, []string{models.MediaStopReasonExited}, rec.reasons(mediaslot.Primary))
	})
}

func TestStopNativePlaybackBeforePrimaryCommand_RecordsReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{name: "stop", cmd: gozapscript.ZapScriptCmdStop, want: models.MediaStopReasonStopped},
		{name: "playlist.stop", cmd: gozapscript.ZapScriptCmdPlaylistStop, want: models.MediaStopReasonStopped},
		{name: "launch replaces", cmd: gozapscript.ZapScriptCmdLaunch, want: models.MediaStopReasonReplaced},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, rec := newRecordedService(t)
			svc.PlaybackManager = &servicePlaybackRecorder{}
			svc.State.SetActiveMedia(nativeTrack("track"))

			require.NoError(t, stopNativePlaybackBeforePrimaryCommand(svc, gozapscript.Command{Name: tt.cmd}, nil))
			assert.Equal(t, []string{tt.want}, rec.reasons(mediaslot.Primary))
		})
	}
}

func TestTimedExit_RecordsStoppedReason(t *testing.T) {
	t.Parallel()

	fx := newTimedExitStopFixture(t, nil)
	rec := &mediaEndRecorder{}
	fx.st.AddMediaObserver(rec)

	clock := clockwork.NewFakeClock()
	var exitGeneration atomic.Uint64
	timedExit(fx.svc, clock, nil, &exitGeneration, &fx.owner, 0)
	clock.Advance(time.Millisecond)

	select {
	case <-fx.stopCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("exit did not stop the launcher")
	}

	// The platform's clear lands after the stop was issued.
	fx.st.SetActiveMedia(nil)
	assert.Equal(t, []string{models.MediaStopReasonStopped}, rec.reasons(mediaslot.Primary))
}

func TestStopMediaForExit_FailedStopLeavesLaterExitAsExited(t *testing.T) {
	t.Parallel()

	fx := newTimedExitStopFixture(t, platforms.ErrStopFailed)
	rec := &mediaEndRecorder{}
	fx.st.AddMediaObserver(rec)

	assert.False(t, stopMediaForExit(fx.svc))

	fx.st.SetActiveMedia(nil)
	assert.Equal(t, []string{models.MediaStopReasonExited}, rec.reasons(mediaslot.Primary))
}

func TestStopNativeAudioPrimaryMedia_FailedStopLeavesLaterExitAsExited(t *testing.T) {
	t.Parallel()

	svc, rec := newRecordedService(t)
	svc.State.SetActiveMedia(nativeTrack("track"))

	err := stopNativeAudioPrimaryMedia(context.Background(), svc, func() error { return errors.New("busy") })
	require.Error(t, err)

	svc.State.SetActiveMedia(nil)
	assert.Equal(t, []string{models.MediaStopReasonExited}, rec.reasons(mediaslot.Primary))
}

type failingStopPlayback struct{ servicePlaybackRecorder }

func (*failingStopPlayback) Stop(string) error { return errors.New("busy") }

func TestStopNativePlaybackBeforePrimaryCommand_FailedStopLeavesLaterExitAsExited(t *testing.T) {
	t.Parallel()

	svc, rec := newRecordedService(t)
	svc.PlaybackManager = &failingStopPlayback{}
	svc.State.SetActiveMedia(nativeTrack("track"))

	err := stopNativePlaybackBeforePrimaryCommand(svc, gozapscript.Command{Name: gozapscript.ZapScriptCmdStop}, nil)
	require.Error(t, err)

	svc.State.SetActiveMedia(nil)
	assert.Equal(t, []string{models.MediaStopReasonExited}, rec.reasons(mediaslot.Primary))
}

func TestNativeAudioDrain_BackgroundStaleDrainKeepsOtherMedia(t *testing.T) {
	t.Parallel()

	svc, rec := newRecordedService(t)
	svc.State.SetBackgroundMedia(models.NewActiveMedia("SNES", "SNES", "x.sfc", "X", "emulator"))

	registrar := &testDrainCallbackRegistrar{}
	wireNativeAudioDrainCallbacks(registrar, svc)
	registrar.callbacks[mediaslot.Background](true)

	assert.NotNil(t, svc.State.BackgroundMedia())
	assert.Empty(t, rec.reasons(mediaslot.Background))
}
