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

package windowfocus

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAPI struct {
	findAfter     int
	activateAfter int
	findCalls     int
	activateCalls int
}

func (*fakeAPI) allowProcessForeground(_ uint32) {}

func (f *fakeAPI) findProcessWindow(_ uint32) (uintptr, bool) {
	f.findCalls++
	return 42, f.findCalls >= f.findAfter
}

func (f *fakeAPI) activateWindow(_ uintptr) bool {
	f.activateCalls++
	return f.activateCalls >= f.activateAfter
}

func TestProcessTreeIncludesNestedChildren(t *testing.T) {
	t.Parallel()

	pids := processTree(10, []processRelation{
		{pid: 30, parentPID: 20},
		{pid: 20, parentPID: 10},
		{pid: 40, parentPID: 99},
	})

	assert.Equal(t, map[uint32]struct{}{10: {}, 20: {}, 30: {}}, pids)
}

func TestManagerFocusWaitsForWindowAndActivation(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{findAfter: 2, activateAfter: 2}
	manager := newManager(api, time.Millisecond, 100*time.Millisecond)

	require.NoError(t, manager.Focus(context.Background(), 123))
	assert.GreaterOrEqual(t, api.findCalls, 3)
	assert.Equal(t, 2, api.activateCalls)
}

func TestManagerFocusStopsWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := &fakeAPI{findAfter: 100, activateAfter: 1}
	manager := newManager(api, time.Millisecond, time.Second)

	err := manager.Focus(ctx, 123)
	require.ErrorIs(t, err, context.Canceled)
}

func TestManagerFocusTimesOut(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{findAfter: 100, activateAfter: 1}
	manager := newManager(api, time.Millisecond, 10*time.Millisecond)

	err := manager.Focus(context.Background(), 123)
	require.ErrorIs(t, err, errFocusTimeout)
}

func TestManagerFocusIgnoresInvalidRequests(t *testing.T) {
	t.Parallel()

	assert.NoError(t, (*Manager)(nil).Focus(context.Background(), 123))
	assert.NoError(t, newManager(nil, time.Millisecond, time.Second).Focus(context.Background(), 123))
	assert.NoError(t, newManager(&fakeAPI{}, time.Millisecond, time.Second).Focus(context.Background(), 0))
}

type fakeFocuser struct {
	err     error
	release chan struct{}
}

func (f *fakeFocuser) Focus(ctx context.Context, _ uint32) error {
	select {
	case <-f.release:
		return f.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPending_ReportsAFocusedWindow(t *testing.T) {
	t.Parallel()
	f := &fakeFocuser{release: make(chan struct{})}
	pending := Start(t.Context(), f, 7)

	close(f.release)

	require.NoError(t, pending.Err(t.Context()))
}

func TestPending_ReportsAFocusThatGaveUp(t *testing.T) {
	t.Parallel()
	f := &fakeFocuser{release: make(chan struct{}), err: errFocusTimeout}
	pending := Start(t.Context(), f, 7)

	close(f.release)

	require.ErrorIs(t, pending.Err(t.Context()), errFocusTimeout)
}

func TestPending_WaitEndsWhenTheCallerGivesUp(t *testing.T) {
	t.Parallel()
	f := &fakeFocuser{release: make(chan struct{})}
	pending := Start(t.Context(), f, 7)
	ctx, cancel := context.WithCancel(t.Context())

	cancel()

	require.ErrorIs(t, pending.Err(ctx), context.Canceled)
	close(f.release)
}

func TestPending_CancellingTheFocusEndsTheAttempt(t *testing.T) {
	t.Parallel()
	f := &fakeFocuser{release: make(chan struct{})}
	focusCtx, cancel := context.WithCancel(t.Context())
	pending := Start(focusCtx, f, 7)

	cancel()

	require.ErrorIs(t, pending.Err(t.Context()), context.Canceled)
}

func TestManager_HasWindow(t *testing.T) {
	t.Parallel()

	assert.True(t, newManager(&fakeAPI{findAfter: 1}, time.Millisecond, time.Second).HasWindow(7))
	assert.False(t, newManager(&fakeAPI{findAfter: 2}, time.Millisecond, time.Second).HasWindow(7))
	assert.False(t, newManager(&fakeAPI{findAfter: 1}, time.Millisecond, time.Second).HasWindow(0))
	assert.False(t, (*Manager)(nil).HasWindow(7))
}

func TestTracker_ReadyOnceTheTrackedWindowTakesFocus(t *testing.T) {
	t.Parallel()
	f := &fakeFocuser{release: make(chan struct{})}
	var tr Tracker
	tr.Track(t.Context(), f, 7)
	done := make(chan error, 1)
	go func() { done <- tr.Wait(t.Context(), clockwork.NewFakeClock(), false) }()

	close(f.release)

	require.NoError(t, <-done)
}

func TestTracker_NoProcessIsReadyUnlessOneIsExpected(t *testing.T) {
	t.Parallel()
	var tr Tracker

	require.NoError(t, tr.Wait(t.Context(), clockwork.NewFakeClock(), false))
}

func TestTracker_WaitsForAProcessThatAppearsLater(t *testing.T) {
	t.Parallel()
	var tr Tracker
	clock := clockwork.NewFakeClock()
	done := make(chan error, 1)
	go func() { done <- tr.Wait(t.Context(), clock, true) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	f := &fakeFocuser{release: make(chan struct{})}
	tr.Track(t.Context(), f, 7)
	close(f.release)
	clock.Advance(processPollInterval)

	require.NoError(t, <-done)
}

func TestTracker_NeverTrackedEndsWithTheCallersContext(t *testing.T) {
	t.Parallel()
	var tr Tracker
	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- tr.Wait(ctx, clock, true) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestTracker_NoWindowIsTreatedAsReady(t *testing.T) {
	t.Parallel()
	f := &fakeFocuser{release: make(chan struct{}), err: errFocusTimeout}
	var tr Tracker
	tr.Track(t.Context(), f, 7)
	close(f.release)

	require.NoError(t, tr.Wait(t.Context(), clockwork.NewFakeClock(), true))
}

func TestTracker_ForgetDropsTheAttempt(t *testing.T) {
	t.Parallel()
	f := &fakeFocuser{release: make(chan struct{})}
	var tr Tracker
	tr.Track(t.Context(), f, 7)

	tr.Forget()

	require.NoError(t, tr.Wait(t.Context(), clockwork.NewFakeClock(), false), "nothing tracked, nothing to wait for")
	close(f.release)
}

func TestTracker_ReplacedAttemptIsReplacedNotWaitedOn(t *testing.T) {
	t.Parallel()
	var tr Tracker
	first := &fakeFocuser{release: make(chan struct{})}
	ctx1, cancel1 := context.WithCancel(t.Context())
	tr.Track(ctx1, first, 1)
	clock := clockwork.NewFakeClock()
	done := make(chan error, 1)
	go func() { done <- tr.Wait(t.Context(), clock, true) }()

	// The first attempt is cancelled, as when its process is replaced; the
	// wait moves on to the second.
	second := &fakeFocuser{release: make(chan struct{})}
	tr.Track(t.Context(), second, 2)
	cancel1()
	close(second.release)

	require.NoError(t, <-done)
	close(first.release)
}
