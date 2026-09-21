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
	"sync"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

func TestHostManagedStartSkipsWatchdog(t *testing.T) {
	t.Parallel()
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{DisableSelfUpdate: true})
	startupErr := errors.New("host startup failed")
	_, err := startWith(platform, &config.Instance{}, func(context.Context, string, string) error {
		t.Fatal("host-managed startup must not invoke executable update watchdog")
		return nil
	}, func(platforms.Platform, *config.Instance) (*StartResult, error) {
		return nil, startupErr
	})
	require.ErrorIs(t, err, startupErr)
}

func TestHostManagedStartStillReleasesStartupFailure(t *testing.T) {
	t.Parallel()
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{DisableSelfUpdate: true})
	platform.On("Stop").Return(nil)
	st, _ := state.NewState(platform, "host-managed-failure-test")
	defer st.StopService()
	startupErr := errors.New("platform support unavailable")
	_, err := startWith(platform, &config.Instance{}, func(context.Context, string, string) error {
		t.Fatal("host-managed startup must not invoke executable update watchdog")
		return nil
	}, func(pl platforms.Platform, _ *config.Instance) (*StartResult, error) {
		return nil, newStartupFailure(pl, st, nil, true, "headline", "detail", startupErr)
	})
	require.ErrorIs(t, err, startupErr)
	require.Error(t, st.GetContext().Err(), "skipping update recovery must not skip releasing the failed start")
	platform.AssertCalled(t, "Stop")
}

func TestHostManagedStartSkipsUpdateScheduler(t *testing.T) {
	t.Parallel()
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{DisableSelfUpdate: true})
	var workers sync.WaitGroup
	// Missing scheduler dependencies intentionally fail if construction is attempted.
	startUpdaterScheduler(t.Context(), nil, platform, nil, nil, nil, &workers)
	workers.Wait()
}
