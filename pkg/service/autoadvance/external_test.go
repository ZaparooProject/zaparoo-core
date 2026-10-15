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

package autoadvance

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isExternal(id string) bool { return id == "LaunchBox" || id == "Steam" }

// publishAsync publishes media the way an external tracker does: from its own
// goroutine, some time after the launch call returned.
func (r *rig) publishAsync(launcherID, name string) uint64 {
	r.t.Helper()
	r.nextID++
	gen := r.nextID
	media := r.media(name)
	media.LauncherID = launcherID
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.eng.MediaStarted(state.MediaStart{Slot: mediaslot.Primary, Gen: gen, Media: media})
	}()
	<-done
	r.eng.Sync()
	return gen
}

func (r *rig) endMedia(gen uint64, reason string) {
	r.t.Helper()
	r.eng.MediaEnded(state.MediaEnd{Slot: mediaslot.Primary, Gen: gen, Reason: reason})
	r.eng.Sync()
}

// The launch returns without publishing, the tracker publishes a second or two
// later, and the game is quit long afterwards: the countdown and advance run.
func TestExternalLaunch_BindsLaterMediaAndCountsDownOnExit(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withPublishes(isExternal))
	pls := testPlaylist(3, 0, nil)
	asyncLaunch(r, pls)

	r.tick(2 * time.Second)
	gen := r.publishAsync("LaunchBox", "game")
	r.tick(40 * time.Second)
	r.endMedia(gen, exited)

	require.Equal(t, 1, r.ui.opened(), "the exit opens the countdown")
	assert.Equal(t, "Up next: GameB", r.ui.request(t, 0).Title)
	r.tick(time10s)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestExternalLaunch_AdvanceTimerRunsFromTheLaterPublication(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withPublishes(isExternal))
	pls := advancing(3, 0)
	asyncLaunch(r, pls)
	r.tick(5 * time.Second)
	r.publishAsync("LaunchBox", "game")

	r.tick(59 * time.Second)
	r.quiet()
	r.tick(time.Second)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestExternalLaunch_SkipsMediaFromOtherLaunchers(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withPublishes(isExternal))
	asyncLaunch(r, testPlaylist(3, 0, nil))

	menu := r.publishAsync("mister-menu", "someone else's game")
	r.endMedia(menu, models.MediaStopReasonFinished)
	r.quiet()

	gen := r.publishAsync("Steam", "ours")
	r.endMedia(gen, models.MediaStopReasonFinished)
	assert.Equal(t, 1, r.host.last(t).Index, "the window was still open for the matching launcher")
}

// LaunchBox hands some games to Steam: the first media ends at once with
// elapsed near zero and a second one is published by the Steam tracker.
func TestExternalLaunch_HandoffRebindsToTheReplacement(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withPublishes(isExternal))
	asyncLaunch(r, testPlaylist(3, 0, nil))
	r.tick(time.Second)
	first := r.publishAsync("LaunchBox", "game")
	r.tick(time.Second)
	r.endMedia(first, exited)
	assert.Zero(t, r.ui.opened(), "a handoff is not the game quitting")
	r.quiet()

	r.tick(3 * time.Second)
	second := r.publishAsync("Steam", "game")
	r.tick(30 * time.Second)
	r.endMedia(second, exited)

	require.Equal(t, 1, r.ui.opened(), "quitting the real game counts down")
	r.tick(time10s)
	assert.Equal(t, 1, r.host.last(t).Index)
}

func TestExternalLaunch_HandoffWithoutReplacementIsAnExit(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withPublishes(isExternal))
	asyncLaunch(r, testPlaylist(3, 0, nil))
	first := r.publishAsync("LaunchBox", "game")
	r.tick(time.Second)
	r.endMedia(first, exited)
	r.tick(handoffWindow - time.Second)
	assert.Zero(t, r.ui.opened())
	r.tick(time.Second)
	require.Equal(t, 1, r.ui.opened(), "nothing took over, so the game did quit")
}

func TestExternalLaunch_HandoffWaitClosesOnAnotherLaunch(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withPublishes(isExternal))
	asyncLaunch(r, testPlaylist(3, 0, nil))
	first := r.publishAsync("LaunchBox", "game")
	r.tick(time.Second)
	r.endMedia(first, exited)

	other := testPlaylist(3, 2, nil)
	r.eng.ItemLaunching(other)
	r.eng.ItemLaunched(other, nil, false)
	r.eng.Sync()
	r.tick(time.Minute)
	assert.Zero(t, r.ui.opened(), "the earlier item is no longer waited on")
}

func TestExternalLaunch_ALongRunExitIsNotAHandoff(t *testing.T) {
	t.Parallel()

	r := newRig(t, withUI(false), withPublishes(isExternal))
	asyncLaunch(r, testPlaylist(3, 0, nil))
	gen := r.publishAsync("LaunchBox", "game")
	r.tick(handoffMaxRun)
	r.endMedia(gen, exited)
	assert.Equal(t, 1, r.ui.opened())
}

// A stop requested after the engine decided a move cancels it.
func TestMoves_StampedWithTheStopCountAtDecision(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.host.userStop()
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, models.MediaStopReasonFinished)
	got := r.host.last(t)
	assert.Equal(t, uint64(1), got.StopSeq)
	assert.False(t, got.EngineFromArmed)
	assert.Equal(t, 0, got.EngineFromIndex)
}

func TestMoves_ArmedStartIsMarkedAsComingFromAnArmedPlaylist(t *testing.T) {
	t.Parallel()

	r := newRig(t, withInput())
	r.load(armedPlaylist())
	r.input.nextWatch(t)
	r.tick(idleFor)
	assert.True(t, r.host.last(t).EngineFromArmed)
}

func TestMoves_RetryDroppedAfterAUserStop(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	gen := r.play(testPlaylist(3, 0, nil))
	r.end(gen, models.MediaStopReasonFinished)
	r.eng.ItemRefused(r.host.last(t))
	r.eng.Sync()
	r.host.userStop()
	r.tick(time.Second)
	assert.Len(t, r.host.queued(), 1)
}
