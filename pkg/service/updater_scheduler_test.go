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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	stateservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdaterIntervalJitter(t *testing.T) {
	t.Parallel()

	assert.Equal(t, updaterCheckInterval, updaterInterval(""))
	first := updaterInterval("device-1")
	assert.Equal(t, first, updaterInterval("device-1"))
	assert.GreaterOrEqual(t, first, updaterCheckInterval-updaterCheckInterval/10)
	assert.LessOrEqual(t, first, updaterCheckInterval+updaterCheckInterval/10)
	assert.NotEqual(t, first, updaterInterval("device-2"))
}

func TestIntervalStateBackoffAndReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	var state intervalState
	assert.True(t, state.due(now, updaterCheckInterval))
	state.recordFailure(now, updaterFailureInitialBackoff, updaterFailureMaxBackoff)
	assert.False(t, state.due(now.Add(time.Minute), updaterCheckInterval))
	assert.True(t, state.due(now.Add(updaterFailureInitialBackoff), updaterCheckInterval))
	assert.Equal(t, 2*updaterFailureInitialBackoff, state.backoff)

	state.backoff = updaterFailureMaxBackoff
	state.recordFailure(now, updaterFailureInitialBackoff, updaterFailureMaxBackoff)
	assert.Equal(t, updaterFailureMaxBackoff, state.backoff)

	state.recordSuccess(now, updaterFailureInitialBackoff)
	assert.False(t, state.due(now.Add(time.Hour), updaterCheckInterval))
	assert.True(t, state.due(now.Add(updaterCheckInterval), updaterCheckInterval))
	assert.Equal(t, updaterFailureInitialBackoff, state.backoff)
}

func TestUpdaterSchedulerEagerCheckHonorsSuccessInterval(t *testing.T) {
	root := t.TempDir()
	cfg, err := config.NewConfig(root, config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetUpdateCheck(true)

	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("mock-platform")
	pl.On("Settings").Return(platforms.Settings{DataDir: root})
	pl.On("ManagedByPackageManager").Return(false)
	st, _ := stateservice.NewState(pl, "test-boot")
	t.Cleanup(st.StopService)
	clock := clockwork.NewFakeClockAt(time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC))
	idleSched := idle.NewWithClock(clock)
	scheduler := newUpdaterScheduler(cfg, pl, nil, st, idleSched)
	scheduler.clock = clock
	scheduler.waitInternet = func(context.Context, int) bool { return true }
	checked := make(chan struct{}, 2)
	scheduler.check = func(context.Context, updater.Options) (*updater.Result, error) {
		checked <- struct{}{}
		return &updater.Result{
			CurrentVersion: "2.9.0", LatestVersion: "2.9.0",
			Eligibility: updater.EligibilityEligible,
		}, nil
	}

	scheduler.tryCheck(t.Context())
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(updaterCheckIdleQuietWindow)
	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("eager updater check did not run")
	}
	require.Eventually(t, func() bool { return !scheduler.checkScheduled.Load() }, time.Second, time.Millisecond)

	scheduler.tryCheck(t.Context())
	select {
	case <-checked:
		t.Fatal("updater checked again before its interval")
	default:
	}
	idleSched.Wait()
	pl.AssertExpectations(t)
}

func TestUpdaterSchedulerRunInstallRechecksAndRestarts(t *testing.T) {
	root := t.TempDir()
	cfg, err := config.NewConfig(root, config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetUpdateCheck(true)
	cfg.SetUpdateInstall(true)

	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("mock-platform")
	pl.On("Settings").Return(platforms.Settings{DataDir: root})
	pl.On("ManagedByPackageManager").Return(false)
	st, _ := stateservice.NewState(pl, "test-boot")
	t.Cleanup(st.StopService)
	scheduler := newUpdaterScheduler(cfg, pl, nil, st, idle.New())
	fresh := &updater.Result{
		CurrentVersion: "2.9.0", LatestVersion: "2.10.0",
		UpdateAvailable: true, Eligibility: updater.EligibilityEligible,
	}
	scheduler.check = func(_ context.Context, opts updater.Options) (*updater.Result, error) {
		assert.Equal(t, updater.ModeAuto, opts.Mode)
		return fresh, nil
	}
	applied := false
	scheduler.apply = func(ctx context.Context, opts updater.Options) (string, error) {
		applied = true
		assert.Equal(t, updater.ModeAuto, opts.Mode)
		require.NotNil(t, opts.PreQuiesce)
		require.NoError(t, opts.PreQuiesce(ctx))
		return "2.10.0", nil
	}

	scheduler.runInstall(t.Context())

	assert.True(t, applied)
	assert.True(t, st.RestartRequested())
	pl.AssertExpectations(t)
}

func TestAutoInstallable(t *testing.T) {
	t.Parallel()

	eligible := &updater.Result{UpdateAvailable: true, Eligibility: updater.EligibilityEligible}
	assert.True(t, autoInstallable(eligible))
	copyResult := *eligible
	copyResult.RolloutHeld = true
	assert.False(t, autoInstallable(&copyResult))
	copyResult = *eligible
	copyResult.Eligibility = updater.EligibilityManaged
	assert.False(t, autoInstallable(&copyResult))
	copyResult = *eligible
	copyResult.UpdateAvailable = false
	assert.False(t, autoInstallable(&copyResult))
	assert.False(t, autoInstallable(nil))
}
