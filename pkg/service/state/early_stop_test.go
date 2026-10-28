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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireStopped(t *testing.T, stopped <-chan struct{}) {
	t.Helper()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("the game that appeared after the stop was not stopped")
	}
}

func requireNotStopped(t *testing.T, stopped <-chan struct{}) {
	t.Helper()
	select {
	case <-stopped:
		t.Fatal("media was stopped that no stop was meant for")
	case <-time.After(50 * time.Millisecond):
	}
}

// launchPublished runs a launch that publishes media and returns, leaving the
// media not yet ready, as a game is while its emulator starts.
func launchPublished(t *testing.T, st *State, path string) {
	t.Helper()
	access, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	access.SetActiveMedia(testMedia(path))
	access.Release()
	require.False(t, st.ActiveMediaReady())
}

// Batocera: the stop reaches the emulator before it is running and does
// nothing, the media is cleared, and the frontend starts the game moments
// later. The tracker's report of that game must stop it, not publish it.
func TestEarlyStop_GameAppearingAfterStopIsStopped(t *testing.T) {
	t.Parallel()
	st, _, stopped := newLaunchStopState(t)

	launchPublished(t, st, "game.nes")
	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(nil)

	st.SetActiveMedia(testMedia("game.nes"))

	assert.Nil(t, st.ActiveMedia(), "the late game is not published")
	requireStopped(t, stopped)
}

// Steam on Windows: the launch hands a URL to Steam and returns without
// publishing anything, and Steam's watcher reports the game later. A stop in
// between has nothing to stop yet, but the game it was meant for is stopped
// when it appears.
func TestEarlyStop_LaunchThatPublishedNothingIsStoppedWhenItAppears(t *testing.T) {
	t.Parallel()
	st, _, stopped := newLaunchStopState(t)

	access, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	access.Release()
	st.RecordMediaStop(models.MediaStopReasonStopped)

	st.SetActiveMedia(testMedia("steam://1942280/Brotato"))

	assert.Nil(t, st.ActiveMedia())
	requireStopped(t, stopped)
}

// A stop of media that is already running ends it for good; the same game
// started again from outside Core afterwards is the user's to play.
func TestEarlyStop_StopOfReadyMediaLeavesLaterPublicationAlone(t *testing.T) {
	t.Parallel()
	st, _, stopped := newLaunchStopState(t)

	launchPublished(t, st, "game.nes")
	gen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)
	st.MarkActiveMediaReady(gen)
	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(nil)

	st.SetActiveMedia(testMedia("game.nes"))

	assert.NotNil(t, st.ActiveMedia())
	requireNotStopped(t, stopped)
}

// Launching again after the stop is a new request, and what it starts is
// published however it turns up.
func TestEarlyStop_NewLaunchSupersedesTheStop(t *testing.T) {
	t.Parallel()
	st, _, stopped := newLaunchStopState(t)

	launchPublished(t, st, "game.nes")
	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(nil)

	access, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	access.Release()
	st.SetActiveMedia(testMedia("game.nes"))

	assert.NotNil(t, st.ActiveMedia())
	requireNotStopped(t, stopped)
}

// A different game appearing is not the one the stop was meant for.
func TestEarlyStop_OtherGameIsPublished(t *testing.T) {
	t.Parallel()
	st, _, stopped := newLaunchStopState(t)

	launchPublished(t, st, "game.nes")
	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(nil)

	st.SetActiveMedia(testMedia("other.nes"))

	assert.NotNil(t, st.ActiveMedia())
	requireNotStopped(t, stopped)
}

// A stop made once the launch has had its window to produce the game is not
// waiting on anything, so a game appearing afterwards is the user's.
func TestEarlyStop_StopAfterTheWindowIsNotEarly(t *testing.T) {
	t.Parallel()
	st, _, stopped := newLaunchStopState(t)

	launchPublished(t, st, "game.nes")
	// Move the launch and its publication out of the window rather than
	// shrinking the window: a coarse clock, as on Windows, can read the same
	// instant for the publication and the stop, which is inside any window.
	st.mu.Lock()
	st.lastLaunchAt = st.lastLaunchAt.Add(-2 * st.earlyStopWindow)
	st.activeMediaPublishedAt = st.activeMediaPublishedAt.Add(-2 * st.earlyStopWindow)
	st.mu.Unlock()
	st.RecordMediaStop(models.MediaStopReasonStopped)
	st.SetActiveMedia(nil)

	st.SetActiveMedia(testMedia("game.nes"))

	assert.NotNil(t, st.ActiveMedia())
	requireNotStopped(t, stopped)
}
