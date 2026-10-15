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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// acquireLaunch takes a launch access and returns the pieces a launcher is
// handed: the publisher, the MediaFinished callback and the gate release.
func acquireLaunch(t *testing.T, st *State) (publish func(*models.ActiveMedia), finished, release func()) {
	t.Helper()
	access, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	require.NotNil(t, access.MediaFinished)
	require.NotNil(t, access.RecordMediaStop)
	return access.SetActiveMedia, access.MediaFinished, access.Release
}

func TestLaunchMediaFinished_ReachesObservers(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	publish, finished, release := acquireLaunch(t, st)
	publish(testMedia("a.sfc"))
	release()

	finished()

	end := observer.lastEnd(t)
	assert.Equal(t, models.MediaStopReasonFinished, end.Reason)
	assert.Equal(t, "a.sfc", end.Media.Path)
	assert.Nil(t, st.ActiveMedia())
}

func TestLaunchMediaFinished_SecondCallIsNoOp(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	publish, finished, release := acquireLaunch(t, st)
	publish(testMedia("a.sfc"))
	release()

	finished()
	// Same path published again by something else must not be finished by a
	// repeated call from the first launch.
	st.SetActiveMedia(testMedia("a.sfc"))
	finished()

	assert.NotNil(t, st.ActiveMedia())
	assert.Len(t, observer.ends, 1)
}

func TestLaunchMediaFinished_StaleAfterReplacementIsIgnored(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	publish, finished, release := acquireLaunch(t, st)
	publish(testMedia("a.sfc"))
	release()

	st.SetActiveMedia(testMedia("b.sfc"))
	finished()

	require.NotNil(t, st.ActiveMedia())
	assert.Equal(t, "b.sfc", st.ActiveMedia().Path)
	end := observer.lastEnd(t)
	assert.Equal(t, models.MediaStopReasonReplaced, end.Reason)
	assert.Len(t, observer.ends, 1)
}

func TestLaunchMediaFinished_BeforePublishIsAppliedOnPublish(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)

	publish, finished, release := acquireLaunch(t, st)
	defer release()

	finished()
	assert.Nil(t, st.ActiveMedia())
	assert.Empty(t, observer.ends)

	publish(testMedia("a.sfc"))

	end := observer.lastEnd(t)
	assert.Equal(t, models.MediaStopReasonFinished, end.Reason)
	assert.Equal(t, "a.sfc", end.Media.Path)
	assert.Nil(t, st.ActiveMedia())
}

func TestLaunchMediaFinished_BeforePublishNeverTouchesOtherMedia(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)
	st.SetActiveMedia(testMedia("other.sfc"))

	_, finished, release := acquireLaunch(t, st)
	finished()
	release()

	require.NotNil(t, st.ActiveMedia())
	assert.Equal(t, "other.sfc", st.ActiveMedia().Path)
	assert.Empty(t, observer.ends)
}

func TestFinishActiveMediaIf_OwnershipDecides(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)
	st.SetActiveMedia(testMedia("a.sfc"))

	assert.False(t, st.FinishActiveMediaIf(func(*models.ActiveMedia) bool { return false }))
	assert.NotNil(t, st.ActiveMedia())
	assert.Empty(t, observer.ends)

	assert.True(t, st.FinishActiveMediaIf(func(m *models.ActiveMedia) bool { return m.Path == "a.sfc" }))
	assert.Nil(t, st.ActiveMedia())
	assert.Equal(t, models.MediaStopReasonFinished, observer.lastEnd(t).Reason)

	assert.False(t, st.FinishActiveMediaIf(func(*models.ActiveMedia) bool { return true }))
}

func TestLaunchMediaExited(t *testing.T) {
	t.Parallel()

	t.Run("reports exited", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		access, err := st.AcquireMediaLaunch()
		require.NoError(t, err)
		access.SetActiveMedia(testMedia("a.sfc"))
		access.Release()

		access.MediaExited()
		assert.Equal(t, models.MediaStopReasonExited, observer.lastEnd(t).Reason)
		assert.Nil(t, st.ActiveMedia())
	})

	t.Run("uses the recorded Core stop", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		access, err := st.AcquireMediaLaunch()
		require.NoError(t, err)
		access.SetActiveMedia(testMedia("a.sfc"))
		access.Release()

		access.RecordMediaStop(models.MediaStopReasonStopped)
		access.MediaExited()
		assert.Equal(t, models.MediaStopReasonStopped, observer.lastEnd(t).Reason)
	})

	t.Run("stale call after replacement is ignored", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		access, err := st.AcquireMediaLaunch()
		require.NoError(t, err)
		access.SetActiveMedia(testMedia("a.sfc"))
		access.Release()

		st.SetActiveMedia(testMedia("b.sfc"))
		access.MediaExited()
		require.NotNil(t, st.ActiveMedia())
		assert.Equal(t, "b.sfc", st.ActiveMedia().Path)
		assert.Len(t, observer.ends, 1)
	})

	t.Run("before publish is applied on publish", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		access, err := st.AcquireMediaLaunch()
		require.NoError(t, err)
		defer access.Release()

		access.MediaExited()
		assert.Empty(t, observer.ends)
		access.SetActiveMedia(testMedia("a.sfc"))
		assert.Equal(t, models.MediaStopReasonExited, observer.lastEnd(t).Reason)
	})

	t.Run("shares one once with finished", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		access, err := st.AcquireMediaLaunch()
		require.NoError(t, err)
		access.SetActiveMedia(testMedia("a.sfc"))
		access.Release()

		access.MediaExited()
		st.SetActiveMedia(testMedia("a.sfc"))
		access.MediaFinished()
		assert.NotNil(t, st.ActiveMedia())
		assert.Len(t, observer.ends, 1)
		assert.Equal(t, models.MediaStopReasonExited, observer.ends[0].Reason)
	})
}

func TestRecordMediaStop_CancelWithdrawsOnlyOwnNote(t *testing.T) {
	t.Parallel()

	t.Run("withdrawn note leaves a later exit as exited", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		st.SetActiveMedia(testMedia("a.sfc"))

		cancel := st.RecordMediaStop(models.MediaStopReasonStopped)
		cancel()
		st.SetActiveMedia(nil)

		assert.Equal(t, models.MediaStopReasonExited, observer.lastEnd(t).Reason)
	})

	t.Run("a newer note survives an older cancel", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		st.SetActiveMedia(testMedia("a.sfc"))

		cancelFirst := st.RecordMediaStop(models.MediaStopReasonReplaced)
		st.RecordMediaStop(models.MediaStopReasonStopped)
		cancelFirst()
		st.SetActiveMedia(nil)

		assert.Equal(t, models.MediaStopReasonStopped, observer.lastEnd(t).Reason)
	})

	t.Run("no media gives a harmless cancel", func(t *testing.T) {
		t.Parallel()
		st, _ := newObservedState(t)
		assert.NotPanics(t, st.RecordMediaStop(models.MediaStopReasonStopped))
	})
}

func TestClearBackgroundMediaIf(t *testing.T) {
	t.Parallel()

	owned := func(m *models.ActiveMedia) bool { return m != nil && m.LauncherID == "native" }

	t.Run("owned media is cleared with the reason", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		st.SetBackgroundMedia(models.NewActiveMedia("Audio", "Audio", "b.mp3", "B", "native"))

		assert.True(t, st.ClearBackgroundMediaIf(owned, models.MediaStopReasonFinished))
		assert.Nil(t, st.BackgroundMedia())
		assert.Equal(t, models.MediaStopReasonFinished, observer.lastEnd(t).Reason)
	})

	t.Run("media owned by someone else is kept", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		st.SetBackgroundMedia(models.NewActiveMedia("Audio", "Audio", "b.mp3", "B", "other"))

		assert.False(t, st.ClearBackgroundMediaIf(owned, models.MediaStopReasonFinished))
		assert.NotNil(t, st.BackgroundMedia())
		assert.Empty(t, observer.ends)
	})

	t.Run("empty slot is a no-op", func(t *testing.T) {
		t.Parallel()
		st, observer := newObservedState(t)
		assert.False(t, st.ClearBackgroundMediaIf(owned, models.MediaStopReasonFinished))
		assert.Empty(t, observer.ends)
	})
}

// A natural end reported after Core recorded a stop is the end the media
// actually had.
func TestFinishActiveMedia_ReportsFinishedOverARecordedStop(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)
	st.SetActiveMedia(testMedia("a.mkv"))
	gen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)

	st.RecordMediaStop(models.MediaStopReasonStopped)
	require.True(t, st.FinishActiveMedia(gen))

	assert.Equal(t, models.MediaStopReasonFinished, observer.lastEnd(t).Reason)
}

// A finish that does not apply must leave the recorded stop for the clear it
// was meant for.
func TestFinishActiveMedia_StaleFinishKeepsTheRecordedStop(t *testing.T) {
	t.Parallel()
	st, observer := newObservedState(t)
	st.SetActiveMedia(testMedia("a.mkv"))
	gen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)

	st.RecordMediaStop(models.MediaStopReasonStopped)
	assert.False(t, st.FinishActiveMedia(gen+1))
	assert.False(t, st.FinishActiveMediaIf(func(*models.ActiveMedia) bool { return false }))
	st.SetActiveMedia(nil)

	assert.Equal(t, models.MediaStopReasonStopped, observer.lastEnd(t).Reason)
}
