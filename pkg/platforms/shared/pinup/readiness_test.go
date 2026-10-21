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

package pinup

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readyTablePath = "popper://7/Table"

func readyMedia() *models.ActiveMedia {
	return models.NewActiveMedia("Pinball", "Pinball", readyTablePath, "Table", LauncherID)
}

func newReadyIntegration(clock clockwork.Clock, hasWindow func(int) bool) *Integration {
	return NewIntegration(&Deps{Clock: clock, HasWindow: hasWindow})
}

func (i *Integration) trackForTest(gameID, pid int) {
	i.mu.Lock()
	i.tracked[gameID] = trackedLaunch{pid: pid}
	i.mu.Unlock()
}

func TestWaitForReady_ReadyOnceTheEmulatorHasAWindow(t *testing.T) {
	t.Parallel()
	var windowed atomic.Bool
	clock := clockwork.NewFakeClock()
	i := newReadyIntegration(clock, func(pid int) bool { return pid == 4242 && windowed.Load() })
	i.trackForTest(7, 4242)
	done := make(chan error, 1)
	go func() { done <- i.WaitForReady(t.Context(), nil, readyMedia()) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(readyPoll)
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	windowed.Store(true)
	clock.Advance(readyPoll)

	require.NoError(t, <-done)
}

func TestWaitForReady_WaitsForTheTableToBeAdopted(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	i := newReadyIntegration(clock, func(int) bool { return true })
	done := make(chan error, 1)
	go func() { done <- i.WaitForReady(t.Context(), nil, readyMedia()) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	i.trackForTest(7, 4242)
	clock.Advance(readyPoll)

	require.NoError(t, <-done)
}

func TestWaitForReady_EndsWhenCancelled(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	i := newReadyIntegration(clock, func(int) bool { return false })
	i.trackForTest(7, 4242)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- i.WaitForReady(ctx, nil, readyMedia()) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestWaitForReady_NoWindowCheckMeansReady(t *testing.T) {
	t.Parallel()
	i := newReadyIntegration(clockwork.NewFakeClock(), nil)

	require.NoError(t, i.WaitForReady(t.Context(), nil, readyMedia()))
}

func TestWaitForReady_NotATablePathIsReady(t *testing.T) {
	t.Parallel()
	i := newReadyIntegration(clockwork.NewFakeClock(), func(int) bool { return false })
	media := models.NewActiveMedia("Pinball", "Pinball", "/not/a/table", "Table", LauncherID)

	require.NoError(t, i.WaitForReady(t.Context(), nil, media))
}

func TestNewLauncher_HasReadinessProbe(t *testing.T) {
	t.Parallel()

	launcher := NewLauncher(newReadyIntegration(clockwork.NewFakeClock(), nil))

	assert.NotNil(t, launcher.WaitForReady)
}
