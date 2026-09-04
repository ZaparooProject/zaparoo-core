//go:build darwin

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

// fakeDarwinPlatform stands in for the platform callbacks the integration
// drives, recording what it was told so the tests can check ownership rules
// without a Steam install or a real process.
type fakeDarwinPlatform struct {
	media   *models.ActiveMedia
	tracked []int
	mu      syncutil.Mutex
}

func (fp *fakeDarwinPlatform) setTrackedProc(proc *os.Process) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.tracked = append(fp.tracked, proc.Pid)
}

func (fp *fakeDarwinPlatform) activeMedia() *models.ActiveMedia {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return fp.media
}

func (fp *fakeDarwinPlatform) setActiveMedia(media *models.ActiveMedia) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.media = media
}

func (fp *fakeDarwinPlatform) trackedPIDs() []int {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return append([]int(nil), fp.tracked...)
}

func newFakeDarwinIntegration(t *testing.T) (*DarwinPlatformIntegration, *fakeDarwinPlatform) {
	t.Helper()
	fp := &fakeDarwinPlatform{}
	pi := NewDarwinPlatformIntegration(fp.setTrackedProc, fp.activeMedia, fp.setActiveMedia)
	t.Cleanup(pi.Stop)
	return pi, fp
}

func darwinSteamMedia(appID int) *models.ActiveMedia {
	return models.NewActiveMedia(
		systemdefs.SystemPC, "PC", fmt.Sprintf("steam://%d", appID), "Game", "Steam",
	)
}

// A name lookup that finishes after another run took over must not publish,
// otherwise its media would replace the current game's.
func TestDarwinIntegration_PublishRequiresCurrentOwnership(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeDarwinIntegration(t)

	pi.claimLaunch(440, 100)
	pi.claimLaunch(440, 200)

	assert.False(t, pi.publishActiveMediaIfActive(440, 100, darwinSteamMedia(440)))
	assert.Nil(t, fp.activeMedia(), "a replaced run must not publish")

	fresh := darwinSteamMedia(440)
	assert.True(t, pi.publishActiveMediaIfActive(440, 200, fresh))
	assert.Same(t, fresh, fp.activeMedia())
}

// A process found for a run that has since been replaced must not be handed
// to the platform, or the next stop would kill the wrong game.
func TestDarwinIntegration_TrackRequiresCurrentOwnership(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeDarwinIntegration(t)

	pi.claimLaunch(440, 100)
	pi.claimLaunch(440, 200)

	assert.False(t, pi.trackProcessIfActive(440, 100, &os.Process{Pid: 100}))
	assert.Empty(t, fp.trackedPIDs(), "a replaced run must not install its process")

	assert.True(t, pi.trackProcessIfActive(440, 200, &os.Process{Pid: 200}))
	assert.Equal(t, []int{200}, fp.trackedPIDs())
}

// The exit of an earlier run of the same game arrives after the relaunch has
// published. Both runs share an AppID, so only the PID tells them apart; the
// stale exit must leave the relaunch's media alone.
func TestDarwinIntegration_SameGameRelaunchKeepsNewMedia(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeDarwinIntegration(t)

	pi.claimLaunch(440, 100)
	require.True(t, pi.publishActiveMediaIfActive(440, 100, darwinSteamMedia(440)))
	pi.claimLaunch(440, 200)
	second := darwinSteamMedia(440)
	require.True(t, pi.publishActiveMediaIfActive(440, 200, second))

	pi.onGameStop(440, 100)
	assert.Same(t, second, fp.activeMedia(), "the earlier run's exit must not clear the relaunch's media")

	pi.onGameStop(440, 200)
	assert.Nil(t, fp.activeMedia(), "the current run's exit clears its media")
}

// An exit for a game that never owned the media leaves the current game's
// media in place.
func TestDarwinIntegration_StopForOtherGameIsIgnored(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeDarwinIntegration(t)

	pi.claimLaunch(440, 100)
	current := darwinSteamMedia(440)
	require.True(t, pi.publishActiveMediaIfActive(440, 100, current))

	pi.onGameStop(570, 700)

	assert.Same(t, current, fp.activeMedia())
	assert.True(t, pi.ownsLaunch(440, 100))
}

// Nothing owns the game after shutdown, so a search that outlives it cannot
// publish anything.
func TestDarwinIntegration_StopForgetsOwnership(t *testing.T) {
	t.Parallel()
	pi, fp := newFakeDarwinIntegration(t)

	pi.claimLaunch(440, 100)
	pi.Stop()

	assert.False(t, pi.ownsLaunch(440, 100))
	assert.False(t, pi.trackProcessIfActive(440, 100, &os.Process{Pid: 100}))
	assert.Empty(t, fp.trackedPIDs())
}
