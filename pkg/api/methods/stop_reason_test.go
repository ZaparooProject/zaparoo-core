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

package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type endRecorder struct {
	ends []state.MediaEnd
	mu   syncutil.Mutex
}

func (*endRecorder) MediaStarted(state.MediaStart) {}

//nolint:gocritic // state.MediaObserver passes events by value
func (r *endRecorder) MediaEnded(event state.MediaEnd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, event)
}

func (r *endRecorder) reasons() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.ends))
	for i := range r.ends {
		out = append(out, r.ends[i].Reason)
	}
	return out
}

func newRecordingState(t *testing.T, pl platforms.Platform) (*state.State, *endRecorder) {
	t.Helper()
	st, _ := state.NewState(pl, "test-boot")
	t.Cleanup(st.StopService)
	rec := &endRecorder{}
	st.AddMediaObserver(rec)
	st.SetActiveMedia(models.NewActiveMedia("SNES", "SNES", "game.sfc", "Game", "emulator"))
	gen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)
	st.MarkActiveMediaReady(gen)
	return st, rec
}

func TestHandleStop_RecordsStoppedReason(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.On("StopActiveLauncher", platforms.StopForMenu).Return(nil).Once()
	st, rec := newRecordingState(t, pl)

	_, err := HandleStop(requests.RequestEnv{Context: context.Background(), Platform: pl, State: st})
	require.NoError(t, err)

	// The platform's own clear arrives after the stop was issued.
	st.SetActiveMedia(nil)
	assert.Equal(t, []string{models.MediaStopReasonStopped}, rec.reasons())
}

// media.active.update with no params is how trackers and scripts report that
// media ended on its own; Core never calls it for a stop it issued, so the
// clear must not be claimed as a Core stop.
func TestHandleUpdateActiveMedia_ClearReportsExited(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, rec := newRecordingState(t, pl)

	_, err := HandleUpdateActiveMedia(requests.RequestEnv{
		Context: context.Background(), State: st, Params: json.RawMessage{},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{models.MediaStopReasonExited}, rec.reasons())
}

func TestHandleStop_FailedStopLeavesLaterExitAsExited(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.On("StopActiveLauncher", platforms.StopForMenu).Return(platforms.ErrStopFailed).Once()
	st, rec := newRecordingState(t, pl)

	_, err := HandleStop(requests.RequestEnv{Context: context.Background(), Platform: pl, State: st})
	require.Error(t, err)

	st.SetActiveMedia(nil)
	assert.Equal(t, []string{models.MediaStopReasonExited}, rec.reasons())
}

func TestHandleMediaControl_FailedStopLeavesLaterExitAsExited(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, rec := newRecordingState(t, pl)

	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{{
		ID: "emulator",
		Controls: map[string]platforms.Control{platforms.ControlStop: {
			Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
				return platforms.ErrStopFailed
			},
		}},
	}})

	_, err := HandleMediaControl(requests.RequestEnv{
		Context:       context.Background(),
		State:         st,
		LauncherCache: cache,
		Params:        json.RawMessage(`{"action":"stop"}`),
	})
	require.Error(t, err)

	st.SetActiveMedia(nil)
	assert.Equal(t, []string{models.MediaStopReasonExited}, rec.reasons())
}

func TestHandleMediaControl_StopRecordsStoppedReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		action     string
		wantReason string
	}{
		{name: "stop is a Core stop", action: platforms.ControlStop, wantReason: models.MediaStopReasonStopped},
		{name: "pause is not a stop", action: platforms.ControlPause, wantReason: models.MediaStopReasonExited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pl := mocks.NewMockPlatform()
			pl.SetupBasicMock()
			st, rec := newRecordingState(t, pl)

			noop := platforms.Control{
				Func: func(context.Context, *config.Instance, platforms.ControlParams) error { return nil },
			}
			cache := &helpers.LauncherCache{}
			cache.InitializeFromSlice([]platforms.Launcher{{
				ID:       "emulator",
				Controls: map[string]platforms.Control{platforms.ControlStop: noop, platforms.ControlPause: noop},
			}})

			_, err := HandleMediaControl(requests.RequestEnv{
				Context:       context.Background(),
				State:         st,
				LauncherCache: cache,
				Params:        json.RawMessage(`{"action":"` + tt.action + `"}`),
			})
			require.NoError(t, err)

			// A launcher's process exit lands after the control returned.
			st.SetActiveMedia(nil)
			assert.Equal(t, []string{tt.wantReason}, rec.reasons())
		})
	}
}
