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
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

// A before_exit script may launch media, which takes the read side of the same
// gate AcquireMediaStop holds exclusively. If the hook is ever moved after that
// acquisition, this test hangs until the request context expires.
func TestHandleStopRunsBeforeExitBeforeAcquiringStopGate(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForMenu).Return(nil).Once()
	st, _ := state.NewState(mockPlatform, "test-boot")
	defer st.StopService()

	hookRan := false
	st.SetBeforeExitHook(func() {
		hookRan = true
		access, err := st.AcquireMediaLaunch()
		require.NoError(t, err)
		access.Release()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	value, err := HandleStop(requests.RequestEnv{
		Context:  ctx,
		Platform: mockPlatform,
		State:    st,
	})

	require.NoError(t, err)
	assert.Equal(t, NoContent{}, value)
	assert.True(t, hookRan, "before_exit must run on the stop path")
	mockPlatform.AssertExpectations(t)
}

func TestHandleStopRunsBeforeExitBeforeStoppingLauncher(t *testing.T) {
	t.Parallel()

	var events []string
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForMenu).Run(func(_ mock.Arguments) {
		events = append(events, "stop")
	}).Return(nil).Once()
	st, _ := state.NewState(mockPlatform, "test-boot")
	defer st.StopService()

	st.SetBeforeExitHook(func() { events = append(events, "before_exit") })

	_, err := HandleStop(requests.RequestEnv{
		Context:  context.Background(),
		Platform: mockPlatform,
		State:    st,
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"before_exit", "stop"}, events)
}

// markReady stands in for the media-ready probe, which does not run here.
func markReady(t *testing.T, st *state.State) {
	t.Helper()
	gen, active := st.ActiveMediaReadyGeneration()
	require.True(t, active)
	st.MarkActiveMediaReady(gen)
}

// A before_exit script may launch media of its own. Stopping after that would
// kill what the hook just started rather than the media the caller asked to
// stop, so the stop is skipped once the active media has been replaced.
func TestHandleStopLeavesMediaLaunchedByBeforeExitRunning(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("StopActiveLauncher", platforms.StopForMenu).Return(nil).Maybe()
	st, _ := state.NewState(mockPlatform, "test-boot")
	defer st.StopService()

	st.SetActiveMedia(models.NewActiveMedia("NES", "NES", "game.nes", "Game", "test-launcher"))
	markReady(t, st)
	st.SetBeforeExitHook(func() {
		st.SetActiveMedia(models.NewActiveMedia("NES", "NES", "farewell.nes", "Farewell", "test-launcher"))
		// Nothing probes readiness in a unit test, and HandleStop waits for it.
		markReady(t, st)
	})

	value, err := HandleStop(requests.RequestEnv{
		Context:  context.Background(),
		Platform: mockPlatform,
		State:    st,
	})

	require.NoError(t, err)
	assert.Equal(t, NoContent{}, value)
	mockPlatform.AssertNotCalled(t, "StopActiveLauncher", platforms.StopForMenu)
	require.NotNil(t, st.ActiveMedia())
	assert.Equal(t, "farewell.nes", st.ActiveMedia().Path)
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

// TestNoContentMarshalsAsNull pins the wire shape of a void method's result.
// docs/api/methods.md publishes "result": null for every method that returns
// nothing, and JSON-RPC 2.0 §5 requires the key to be present on success.
func TestNoContentMarshalsAsNull(t *testing.T) {
	t.Parallel()

	direct, err := json.Marshal(NoContent{})
	require.NoError(t, err)
	assert.JSONEq(t, "null", string(direct))

	// Handlers return NoContent{} as an any, and encoding/json only finds a
	// pointer-receiver marshaller on an addressable value. This is the case
	// the value receiver exists for.
	boxed, err := json.Marshal(any(NoContent{}))
	require.NoError(t, err)
	assert.JSONEq(t, "null", string(boxed))
}

// TestNoContentResponseObjectCarriesNullResult covers the full response shape.
// All three send paths (plaintext WebSocket, encrypted WebSocket, HTTP POST)
// marshal this same struct, so pinning it here covers each of them.
func TestNoContentResponseObjectCarriesNullResult(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(models.ResponseObject{
		JSONRPC: "2.0",
		ID:      models.NewStringID("no-content-1"),
		Result:  NoContent{},
	})
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))

	result, ok := fields["result"]
	require.True(t, ok, "result must be present on success, not omitted")
	assert.JSONEq(t, "null", string(result))
	assert.NotContains(t, fields, "error")
}
