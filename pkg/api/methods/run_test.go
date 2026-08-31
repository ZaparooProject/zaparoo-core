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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stopResult struct {
	value any
	err   error
}

func TestHandleStopWaitsForLaunchAndMediaReadiness(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForMenu).Return(nil).Once()
	st, _ := state.NewState(mockPlatform, "test-boot")
	defer st.StopService()

	launchAccess, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	launchAccess.SetActiveMedia(models.NewActiveMedia("snes", "SNES", "game.sfc", "Game", "RASNES"))
	readyGen, active := st.ActiveMediaReadyGeneration()
	require.True(t, active)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resultCh := make(chan stopResult, 1)
	go func() {
		value, stopErr := HandleStop(requests.RequestEnv{
			Context:  ctx,
			Platform: mockPlatform,
			State:    st,
		})
		resultCh <- stopResult{value: value, err: stopErr}
	}()

	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
		t.Fatal("stop completed while media launch was in flight")
	case <-time.After(50 * time.Millisecond):
	}
	mockPlatform.AssertNotCalled(t, "StopActiveLauncher", platforms.StopForMenu)

	launchAccess.Release()
	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
		t.Fatal("stop completed before active media became ready")
	case <-time.After(50 * time.Millisecond):
	}
	mockPlatform.AssertNotCalled(t, "StopActiveLauncher", platforms.StopForMenu)

	st.MarkActiveMediaReady(readyGen)
	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
		assert.Equal(t, NoContent{}, result.value)
	case <-time.After(time.Second):
		t.Fatal("stop did not complete after media became ready")
	}
	mockPlatform.AssertExpectations(t)
}

func TestHandleStopCanceledWhileLaunchInFlight(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot")
	defer st.StopService()

	launchAccess, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	defer launchAccess.Release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value, err := HandleStop(requests.RequestEnv{
		Context:  ctx,
		Platform: mockPlatform,
		State:    st,
	})

	assert.Nil(t, value)
	require.ErrorIs(t, err, context.Canceled)
	mockPlatform.AssertNotCalled(t, "StopActiveLauncher", platforms.StopForMenu)
}

func TestHandleStopWithoutActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForMenu).Return(nil).Once()
	st, _ := state.NewState(mockPlatform, "test-boot")
	defer st.StopService()

	value, err := HandleStop(requests.RequestEnv{
		Context:  context.Background(),
		Platform: mockPlatform,
		State:    st,
	})

	require.NoError(t, err)
	assert.Equal(t, NoContent{}, value)
	mockPlatform.AssertExpectations(t)
}
