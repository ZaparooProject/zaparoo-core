//go:build windows

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

package steamtracker

import (
	"fmt"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWindowsPlatform stands in for the platform callbacks the integration
// drives, recording what it was told so the tests can check ownership rules
// without a registry, a Steam install or a real process.
type fakeWindowsPlatform struct {
	media   *models.ActiveMedia
	cleared []int
	mu      syncutil.Mutex
}

func (*fakeWindowsPlatform) setTrackedProc(_ *os.Process) {}

func (fp *fakeWindowsPlatform) clearTrackedPID(pid int) bool {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.cleared = append(fp.cleared, pid)
	return true
}

func (fp *fakeWindowsPlatform) activeMedia() *models.ActiveMedia {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return fp.media
}

func (fp *fakeWindowsPlatform) setActiveMedia(media *models.ActiveMedia) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.media = media
}

func (fp *fakeWindowsPlatform) clearedPIDs() []int {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return append([]int(nil), fp.cleared...)
}

func newFakeWindowsIntegration(t *testing.T) (*WindowsPlatformIntegration, *fakeWindowsPlatform) {
	t.Helper()
	fp := &fakeWindowsPlatform{}
	pi := NewWindowsPlatformIntegration(
		fp.setTrackedProc, fp.clearTrackedPID, fp.activeMedia, fp.setActiveMedia, nil,
	)
	t.Cleanup(pi.Stop)
	return pi, fp
}

func steamMedia(appID int) *models.ActiveMedia {
	return models.NewActiveMedia(
		systemdefs.SystemPC, "PC", fmt.Sprintf("steam://%d", appID), "Game", "Steam",
	)
}

func trackedLaunchFor(pi *WindowsPlatformIntegration, appID int) (trackedLaunch, bool) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	launch, ok := pi.tracked[appID]
	return launch, ok
}

// Steam can restart a game before the tracker has delivered the previous
// run's exit, because the start and stop callbacks are dispatched
// concurrently. The late exit must not discard the process the new run has
// already recorded, or the new run could never be stopped.
func TestWindowsIntegration_StaleStopKeepsRelaunchTracked(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeWindowsIntegration(t)

	pi.claimLaunch(440, 1)
	require.True(t, pi.trackProcessIfActive(440, 1, 100, &os.Process{Pid: 100}))
	pi.claimLaunch(440, 2)
	require.True(t, pi.trackProcessIfActive(440, 2, 200, &os.Process{Pid: 200}))

	pi.onGameStop(440, 1)

	assert.Empty(t, fp.clearedPIDs(), "the first run's exit must not discard the relaunch's process")
	launch, ok := trackedLaunchFor(pi, 440)
	require.True(t, ok, "the relaunch must still be tracked")
	assert.Equal(t, trackedLaunch{pid: 200, lifecycleID: 2}, launch)
	assert.True(t, pi.ownsLaunch(440, 2), "the relaunch still owns the game")

	pi.onGameStop(440, 2)

	assert.Equal(t, []int{200}, fp.clearedPIDs())
	_, ok = trackedLaunchFor(pi, 440)
	assert.False(t, ok)
}

// A name lookup that finishes after another run took over must not publish,
// otherwise its media would replace the current game's.
func TestWindowsIntegration_PublishRequiresCurrentOwnership(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeWindowsIntegration(t)

	pi.claimLaunch(440, 1)
	pi.claimLaunch(440, 2)

	stale := steamMedia(440)
	assert.False(t, pi.publishActiveMediaIfActive(440, 1, stale))
	assert.Nil(t, fp.activeMedia(), "a replaced run must not publish")

	fresh := steamMedia(440)
	assert.True(t, pi.publishActiveMediaIfActive(440, 2, fresh))
	assert.Same(t, fresh, fp.activeMedia())
}

// The exit of an earlier run of the same game arrives after the relaunch has
// published. Both runs share an AppID, so only the lifecycle tells them apart;
// the stale exit must leave the relaunch's media alone.
func TestWindowsIntegration_SameGameRelaunchKeepsNewMedia(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeWindowsIntegration(t)

	pi.claimLaunch(440, 1)
	require.True(t, pi.publishActiveMediaIfActive(440, 1, steamMedia(440)))
	pi.claimLaunch(440, 2)
	second := steamMedia(440)
	require.True(t, pi.publishActiveMediaIfActive(440, 2, second))

	pi.onGameStop(440, 1)
	assert.Same(t, second, fp.activeMedia(), "the earlier run's exit must not clear the relaunch's media")

	pi.onGameStop(440, 2)
	assert.Nil(t, fp.activeMedia(), "the current run's exit clears its media")
}

// An exit for a game that never owned the media leaves the current game's
// media in place.
func TestWindowsIntegration_StopForOtherGameIsIgnored(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeWindowsIntegration(t)

	pi.claimLaunch(440, 1)
	current := steamMedia(440)
	require.True(t, pi.publishActiveMediaIfActive(440, 1, current))

	pi.onGameStop(570, 7)

	assert.Same(t, current, fp.activeMedia())
	assert.True(t, pi.ownsLaunch(440, 1))
}

// The tracker reports no exits when it shuts down, so Stop has to hand back
// every process it gave the platform itself; a handle left behind would be
// killed by the next launch that replaced it.
func TestWindowsIntegration_StopReleasesTrackedProcesses(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeWindowsIntegration(t)

	pi.claimLaunch(440, 1)
	require.True(t, pi.trackProcessIfActive(440, 1, 100, &os.Process{Pid: 100}))

	pi.Stop()

	assert.Equal(t, []int{100}, fp.clearedPIDs())
	_, ok := trackedLaunchFor(pi, 440)
	assert.False(t, ok)
	assert.False(t, pi.ownsLaunch(440, 1), "nothing owns the game after shutdown")
	assert.False(t, pi.trackProcessIfActive(440, 1, 100, &os.Process{Pid: 100}),
		"nothing may be tracked after shutdown")
}
