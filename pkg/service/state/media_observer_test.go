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

package state

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingObserver struct {
	starts []MediaStart
	ends   []MediaEnd
	mu     syncutil.Mutex
}

//nolint:gocritic // state.MediaObserver passes events by value
func (r *recordingObserver) MediaStarted(event MediaStart) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, event)
}

//nolint:gocritic // state.MediaObserver passes events by value
func (r *recordingObserver) MediaEnded(event MediaEnd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, event)
}

func (r *recordingObserver) lastEnd(t *testing.T) MediaEnd {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.ends)
	return r.ends[len(r.ends)-1]
}

func newObservedState(t *testing.T) (*State, *recordingObserver) {
	t.Helper()
	st, _ := NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(st.StopService)
	observer := &recordingObserver{}
	st.AddMediaObserver(observer)
	return st, observer
}

func testMedia(path string) *models.ActiveMedia {
	return models.NewActiveMedia("SNES", "Super Nintendo", path, path, "launcher")
}

func TestMediaEnd_ReasonDefaultsToExited(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	st.SetActiveMedia(testMedia("a.sfc"))
	st.SetActiveMedia(nil)

	end := observer.lastEnd(t)
	assert.Equal(t, models.MediaStopReasonExited, end.Reason)
	assert.Equal(t, mediaslot.Primary, end.Slot)
	assert.Equal(t, "a.sfc", end.Media.Path)
	assert.NotZero(t, end.Gen)
}

func TestMediaEnd_RecordedStopIsReported(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	st.SetActiveMedia(testMedia("a.sfc"))
	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(nil)

	assert.Equal(t, models.MediaStopReasonStopped, observer.lastEnd(t).Reason)
}

func TestMediaEnd_ReplacementDefaultsToReplaced(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	st.SetActiveMedia(testMedia("a.sfc"))
	st.SetActiveMedia(testMedia("b.sfc"))

	end := observer.lastEnd(t)
	assert.Equal(t, models.MediaStopReasonReplaced, end.Reason)
	assert.Equal(t, "a.sfc", end.Media.Path)
	observer.mu.Lock()
	defer observer.mu.Unlock()
	require.Len(t, observer.starts, 2)
	assert.Equal(t, "b.sfc", observer.starts[1].Media.Path)
}

func TestMediaEnd_StopNoteIsConsumedOnce(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	st.SetActiveMedia(testMedia("a.sfc"))
	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(nil)
	st.SetActiveMedia(testMedia("b.sfc"))
	st.SetActiveMedia(nil)

	assert.Equal(t, models.MediaStopReasonExited, observer.lastEnd(t).Reason,
		"a stop recorded for the first media must not label the second")
}

func TestMediaEnd_StopNoteForOtherMediaIsIgnored(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	st.SetActiveMedia(testMedia("a.sfc"))
	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(testMedia("b.sfc"))
	st.SetActiveMedia(nil)

	assert.Equal(t, models.MediaStopReasonExited, observer.lastEnd(t).Reason)
}

func TestMediaEnd_StopNoteExpires(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)
	st.stopIntentTTL = time.Millisecond

	st.SetActiveMedia(testMedia("a.sfc"))
	st.RecordMediaStop(models.MediaStopReasonStopped)
	time.Sleep(20 * time.Millisecond)
	st.SetActiveMedia(nil)

	assert.Equal(t, models.MediaStopReasonExited, observer.lastEnd(t).Reason)
}

func TestRecordMediaStop_NoMediaDoesNothing(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(testMedia("a.sfc"))
	st.SetActiveMedia(nil)

	assert.Equal(t, models.MediaStopReasonExited, observer.lastEnd(t).Reason)
}

func TestFinishActiveMedia_ClearsAndReportsFinished(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	st.SetActiveMedia(testMedia("a.mkv"))
	gen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)

	assert.True(t, st.FinishActiveMedia(gen))

	assert.Nil(t, st.ActiveMedia())
	assert.Equal(t, models.MediaStopReasonFinished, observer.lastEnd(t).Reason)
}

func TestFinishActiveMedia_IgnoresStaleGeneration(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	st.SetActiveMedia(testMedia("a.mkv"))
	staleGen, _ := st.ActiveMediaReadyGeneration()
	st.SetActiveMedia(testMedia("b.mkv"))

	assert.False(t, st.FinishActiveMedia(staleGen))

	require.NotNil(t, st.ActiveMedia())
	assert.Equal(t, "b.mkv", st.ActiveMedia().Path)
	assert.Equal(t, models.MediaStopReasonReplaced, observer.lastEnd(t).Reason)
}

func TestFinishActiveMedia_NoMedia(t *testing.T) {
	t.Parallel()
	st, _ := newObservedState(t)

	assert.False(t, st.FinishActiveMedia(1))
}

func TestBackgroundMedia_ReasonsReachObservers(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	st.SetBackgroundMedia(testMedia("a.mp3"))
	st.ClearBackgroundMedia(models.MediaStopReasonFinished)
	assert.Equal(t, models.MediaStopReasonFinished, observer.lastEnd(t).Reason)
	assert.Equal(t, mediaslot.Background, observer.lastEnd(t).Slot)

	st.SetBackgroundMedia(testMedia("b.mp3"))
	st.SetBackgroundMedia(nil)
	assert.Equal(t, models.MediaStopReasonStopped, observer.lastEnd(t).Reason)

	st.SetBackgroundMedia(testMedia("c.mp3"))
	st.SetBackgroundMedia(testMedia("d.mp3"))
	assert.Equal(t, models.MediaStopReasonReplaced, observer.lastEnd(t).Reason)
}

func TestMediaStopped_NotificationCarriesReason(t *testing.T) {
	t.Parallel()
	st, notifications := NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	drainState(t, st, notifications)

	st.SetActiveMedia(testMedia("a.sfc"))
	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(nil)

	for {
		select {
		case notification := <-notifications:
			if notification.Method != models.NotificationStopped {
				continue
			}
			assert.JSONEq(t, `{"systemId":"SNES","systemName":"Super Nintendo","mediaName":"a.sfc",`+
				`"mediaPath":"a.sfc","launcherId":"launcher","slot":"primary","reason":"stopped","elapsed":0}`,
				string(notification.Params))
			return
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for media.stopped")
		}
	}
}
