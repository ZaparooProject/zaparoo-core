//go:build linux && !android

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

package batocera

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type settlingLaunch struct {
	platform *Platform
	clock    *clockwork.FakeClock
	manager  *state.LauncherManager
	launched chan struct{}
	result   chan error
}

// startSettlingLaunch runs a LaunchMedia that replaces a game ES reports as
// running. Core has no media of its own, so the preemption stop is a no-op
// and the launch parks in the ES settle delay on the fake clock.
// The mock ES API binds a fixed port, so callers must not run in parallel.
func startSettlingLaunch(t *testing.T) *settlingLaunch {
	t.Helper()
	helpers.NewMockESAPIServer(t).WithRunningGame(&esapi.RunningGameResponse{
		ID: "1", Name: "Old Game", SystemName: "snes", Path: "/userdata/roms/snes/old.sfc",
	})

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	db, cleanup := helpers.NewTestDatabase(t)
	t.Cleanup(cleanup)

	sl := &settlingLaunch{
		clock:    clockwork.NewFakeClock(),
		manager:  state.NewLauncherManager(),
		launched: make(chan struct{}, 1),
		result:   make(chan error, 1),
	}
	sl.platform = &Platform{
		cfg:             cfg,
		clock:           sl.clock,
		launcherManager: sl.manager,
		activeMedia:     func() *models.ActiveMedia { return nil },
		setActiveMedia:  func(*models.ActiveMedia) {},
	}
	launcher := &platforms.Launcher{
		ID:       "KodiMovie",
		SystemID: systemdefs.SystemMovie,
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			sl.launched <- struct{}{}
			return nil, nil //nolint:nilnil // No process to track.
		},
	}
	go func() {
		sl.result <- sl.platform.LaunchMedia(cfg, "kodi-movie://1/Film", launcher, db, &platforms.LaunchOptions{})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, sl.clock.BlockUntilContext(ctx, 1), "launch must reach the settle delay")
	return sl
}

func (sl *settlingLaunch) waitResult(t *testing.T) error {
	t.Helper()
	select {
	case err := <-sl.result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("LaunchMedia did not return")
		return nil
	}
}

func TestLaunchMedia_StopDuringSettleAbortsLaunch(t *testing.T) {
	for name, intent := range map[string]platforms.StopIntent{
		"menu": platforms.StopForMenu, "console reset": platforms.StopForConsoleReset,
	} {
		t.Run(name, func(t *testing.T) {
			sl := startSettlingLaunch(t)

			// Nothing is active, so this stop has nothing to act on; it must
			// still cancel the launch in flight.
			require.NoError(t, sl.platform.StopActiveLauncher(intent))

			err := sl.waitResult(t)
			require.ErrorIs(t, err, context.Canceled)
			assert.Empty(t, sl.launched, "the launcher must not run after a stop")
		})
	}
}

func TestLaunchMedia_NoStopLaunchProceeds(t *testing.T) {
	sl := startSettlingLaunch(t)

	sl.clock.Advance(esSettleDelay)

	require.NoError(t, sl.waitResult(t))
	assert.Len(t, sl.launched, 1)
}

func TestLaunchMedia_OwnPreemptionStopDoesNotCancelLaunch(t *testing.T) {
	sl := startSettlingLaunch(t)
	ctxBefore := sl.manager.GetContext()

	// DoLaunch preempts the running media the same way.
	require.NoError(t, sl.platform.StopActiveLauncher(platforms.StopForPreemption))
	assert.NoError(t, ctxBefore.Err())

	sl.clock.Advance(esSettleDelay)
	require.NoError(t, sl.waitResult(t))
	assert.Len(t, sl.launched, 1)
}

func TestLaunchMedia_StopAfterSettleBeforeLaunchAborts(t *testing.T) {
	sl := startSettlingLaunch(t)

	// The settle timer fires and a stop lands before the launch is committed.
	sl.manager.NewContext()
	sl.clock.Advance(esSettleDelay)

	require.ErrorIs(t, sl.waitResult(t), context.Canceled)
	assert.Empty(t, sl.launched)
}
