/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/librarysync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLibrarySyncRunner struct {
	err       error
	delay     time.Duration
	passes    atomic.Int32
	disabled  atomic.Bool
	lastStart atomic.Int64
	lastEnd   atomic.Int64
	mu        syncutil.Mutex
}

func (r *fakeLibrarySyncRunner) Enabled() bool { return !r.disabled.Load() }

func (*fakeLibrarySyncRunner) ApplySetting(context.Context) (bool, error) { return false, nil }

func (*fakeLibrarySyncRunner) DeleteInventory(context.Context) (bool, error) { return false, nil }

func (r *fakeLibrarySyncRunner) SyncInventory(context.Context, bool) (librarysync.InventoryResult, error) {
	r.passes.Add(1)
	r.lastStart.Store(time.Now().UnixMicro())
	r.mu.Lock()
	delay, err := r.delay, r.err
	r.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	r.lastEnd.Store(time.Now().UnixMicro())
	return librarysync.InventoryResult{Outcome: librarysync.InventoryUploaded}, err
}

func (r *fakeLibrarySyncRunner) setDelay(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delay = d
}

func (r *fakeLibrarySyncRunner) setErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func testLibrarySyncTimings(startup time.Duration) *librarySyncTimings {
	return &librarySyncTimings{
		check:          5 * time.Millisecond,
		startup:        startup,
		recheck:        time.Hour,
		initialBackoff: time.Hour,
		maxBackoff:     time.Hour,
	}
}

type testLibrarySyncLoop struct {
	requests chan<- struct{}
	indexing chan<- models.Notification
}

func runTestLibrarySyncLoop(
	t *testing.T, runner librarySyncRunner, timings *librarySyncTimings,
) testLibrarySyncLoop {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan struct{}, 1)
	indexing := make(chan models.Notification, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		librarySyncLoop(ctx, runner, nil, requests, indexing, timings)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return testLibrarySyncLoop{requests: requests, indexing: indexing}
}

func TestLibrarySyncLoop_WaitsForStartupUnlessRequested(t *testing.T) {
	t.Parallel()
	runner := &fakeLibrarySyncRunner{}
	loop := runTestLibrarySyncLoop(t, runner, testLibrarySyncTimings(time.Hour))

	time.Sleep(30 * time.Millisecond)
	assert.Zero(t, runner.passes.Load(), "no pass runs inside the startup delay")

	loop.requests <- struct{}{}
	require.Eventually(t, func() bool { return runner.passes.Load() == 1 }, time.Second, 5*time.Millisecond)
}

func TestLibrarySyncLoop_IndexChangeRunsAnotherPass(t *testing.T) {
	t.Parallel()
	runner := &fakeLibrarySyncRunner{}
	loop := runTestLibrarySyncLoop(t, runner, testLibrarySyncTimings(time.Millisecond))

	require.Eventually(t, func() bool { return runner.passes.Load() == 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, int32(1), runner.passes.Load(), "a quiet pass is trusted until the recheck")

	loop.indexing <- models.Notification{Method: models.NotificationMediaIndexing}
	require.Eventually(t, func() bool { return runner.passes.Load() == 2 }, time.Second, 5*time.Millisecond)
}

func TestLibrarySyncLoop_FailureBacksOffButIdleDoesNot(t *testing.T) {
	t.Parallel()
	runner := &fakeLibrarySyncRunner{}
	runner.setErr(errors.New("server exploded"))
	loop := runTestLibrarySyncLoop(t, runner, testLibrarySyncTimings(time.Millisecond))

	require.Eventually(t, func() bool { return runner.passes.Load() == 1 }, time.Second, 5*time.Millisecond)
	loop.indexing <- models.Notification{Method: models.NotificationMediaIndexing}
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, int32(1), runner.passes.Load(), "an index change does not cut a failure's backoff short")

	// Changing the setting is the user's call and runs straight away.
	runner.setErr(librarysync.ErrDisabled)
	loop.requests <- struct{}{}
	require.Eventually(t, func() bool { return runner.passes.Load() == 2 }, time.Second, 5*time.Millisecond)
	loop.indexing <- models.Notification{Method: models.NotificationMediaIndexing}
	require.Eventually(t, func() bool { return runner.passes.Load() == 3 }, time.Second, 5*time.Millisecond,
		"being off is not a failure, so the next index change runs a pass")
}

// TestLibrarySyncPass_OffDoesNotWaitForIdle pins that a pass with Library sync
// off returns as soon as it has applied the setting. The idle scheduler here
// never falls idle, so a pass that waits for it before checking the setting
// blocks for the whole max wait on a device that is merely busy.
func TestLibrarySyncPass_OffDoesNotWaitForIdle(t *testing.T) {
	t.Parallel()
	runner := &fakeLibrarySyncRunner{}
	runner.disabled.Store(true)
	idleSched := idle.New()
	idleSched.RequestStarted() // never ended: the device is never idle
	timings := testLibrarySyncTimings(time.Millisecond)
	timings.idleQuiet = time.Second
	timings.idleMaxWait = 30 * time.Second

	done := make(chan error, 1)
	go func() { done <- runLibrarySyncPass(context.Background(), runner, idleSched, timings) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("a pass with Library sync off waited for the device to fall idle")
	}
	assert.Zero(t, runner.passes.Load(), "an upload is not attempted while sync is off")
}

// TestLibrarySyncLoop_BackoffRunsFromTheEndOfThePass covers a pass that takes
// longer than the backoff, which a first sync of a large library does by a wide
// margin. Measured from when the pass started, the next attempt would already
// be in the past by the time it failed, so the backoff would not hold at all.
func TestLibrarySyncLoop_BackoffRunsFromTheEndOfThePass(t *testing.T) {
	t.Parallel()
	runner := &fakeLibrarySyncRunner{}
	runner.setErr(errors.New("server exploded"))
	runner.setDelay(800 * time.Millisecond)
	timings := testLibrarySyncTimings(time.Millisecond)
	timings.initialBackoff = 500 * time.Millisecond
	timings.maxBackoff = 500 * time.Millisecond
	runTestLibrarySyncLoop(t, runner, timings)

	require.Eventually(t, func() bool { return runner.lastEnd.Load() > 0 }, 5*time.Second, 5*time.Millisecond,
		"the first pass should finish")
	firstEnd := runner.lastEnd.Load()

	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, int32(1), runner.passes.Load(), "a failed pass waits out its backoff from when it ended")

	require.Eventually(t, func() bool { return runner.passes.Load() >= 2 }, 3*time.Second, 5*time.Millisecond,
		"the backoff should elapse and another pass should run")
	assert.Greater(t, runner.lastStart.Load(), firstEnd+400_000,
		"the second pass starts a backoff after the first one ended")
}

// TestWaitForQuietDevice_GivesUpWhenSyncIsTurnedOff pins that a pass waiting for
// a busy device to fall quiet notices the setting being turned off. The loop
// cannot hand the pass the setting request, and on a device with an app polling
// it the wait otherwise runs for its full limit before the opt-out is acted on.
func TestWaitForQuietDevice_GivesUpWhenSyncIsTurnedOff(t *testing.T) {
	t.Parallel()
	runner := &fakeLibrarySyncRunner{}
	idleSched := idle.New()
	idleSched.RequestStarted() // never ended: the device is never idle
	timings := testLibrarySyncTimings(time.Millisecond)
	timings.idleQuiet = time.Second
	timings.idleSlice = 50 * time.Millisecond
	timings.idleMaxWait = 30 * time.Second

	done := make(chan error, 1)
	go func() { done <- waitForQuietDevice(context.Background(), runner, idleSched, timings) }()

	time.Sleep(150 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("the wait ended while the device was busy and sync was still on")
	default:
	}
	runner.disabled.Store(true)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("the wait did not give up after Library sync was turned off")
	}
}

// TestWaitForQuietDevice_GivesUpAtTheLimit pins that a device that never falls
// quiet does not hold a pass forever: the wait ends at its limit and the pass
// reads the library anyway.
func TestWaitForQuietDevice_GivesUpAtTheLimit(t *testing.T) {
	t.Parallel()
	runner := &fakeLibrarySyncRunner{}
	idleSched := idle.New()
	idleSched.RequestStarted()
	timings := testLibrarySyncTimings(time.Millisecond)
	timings.idleQuiet = time.Second
	timings.idleSlice = 30 * time.Millisecond
	timings.idleMaxWait = 120 * time.Millisecond

	start := time.Now()
	require.NoError(t, waitForQuietDevice(context.Background(), runner, idleSched, timings))
	assert.GreaterOrEqual(t, time.Since(start), timings.idleMaxWait,
		"the wait runs to its limit before giving up on a busy device")
}

// TestLibrarySyncPass_SettingFailureStopsThePass pins that a pass which cannot
// even read the setting reports a failure rather than carrying on to upload.
func TestLibrarySyncPass_SettingFailureStopsThePass(t *testing.T) {
	t.Parallel()
	runner := &failingSettingRunner{}
	err := runLibrarySyncPass(context.Background(), runner, nil, testLibrarySyncTimings(time.Millisecond))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apply library sync setting")
	assert.Zero(t, runner.passes.Load(), "no upload is attempted")
}

type failingSettingRunner struct {
	fakeLibrarySyncRunner
}

func (*failingSettingRunner) ApplySetting(context.Context) (bool, error) {
	return false, errors.New("user database is gone")
}

type fakeLibraryStateRunner struct {
	err    error
	passes atomic.Int32
	mu     syncutil.Mutex
}

func (r *fakeLibraryStateRunner) setErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *fakeLibraryStateRunner) SyncState(context.Context) (librarysync.StateResult, error) {
	r.passes.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	return librarysync.StateResult{}, r.err
}

func TestLibraryStateLoop_DebouncesEdits(t *testing.T) {
	t.Parallel()
	runner := &fakeLibraryStateRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		libraryStateLoop(ctx, runner, requests, &libraryStateTimings{
			check: time.Hour, startup: time.Hour, debounce: 20 * time.Millisecond,
			interval: time.Hour, initialBackoff: time.Hour, maxBackoff: time.Hour,
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	for range 3 {
		requests <- struct{}{}
		time.Sleep(5 * time.Millisecond)
	}
	require.Eventually(t, func() bool { return runner.passes.Load() == 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), runner.passes.Load(), "a burst of edits is pushed in one pass")
}

// TestLibraryStateLoop_DeferredPassKeepsAsking pins that an edit made while the
// media database is being indexed still goes out soon after the index finishes.
// A deferred pass is not a failure, so it must not start the hourly interval
// over and leave the edit sitting for an hour.
func TestLibraryStateLoop_DeferredPassKeepsAsking(t *testing.T) {
	t.Parallel()
	runner := &fakeLibraryStateRunner{}
	runner.setErr(librarysync.ErrNotSettled)
	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		libraryStateLoop(ctx, runner, requests, &libraryStateTimings{
			check: 10 * time.Millisecond, startup: time.Hour, debounce: 10 * time.Millisecond,
			interval: time.Hour, initialBackoff: time.Hour, maxBackoff: time.Hour,
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	requests <- struct{}{}
	require.Eventually(t, func() bool { return runner.passes.Load() >= 3 }, 2*time.Second, 5*time.Millisecond,
		"an edit deferred by indexing keeps asking rather than waiting out the hour")

	// The index finishes and the edit goes out, after which the loop settles.
	runner.setErr(nil)
	require.Eventually(t, func() bool { return runner.passes.Load() >= 4 }, 2*time.Second, 5*time.Millisecond)
	settled := runner.passes.Load()
	time.Sleep(100 * time.Millisecond)
	assert.LessOrEqual(t, runner.passes.Load(), settled+1, "a pushed edit stops the retries")
}

// TestLibraryStateLoop_DeferredStartupPassKeepsAsking pins the first pass after
// a boot that runs straight into indexing. Nothing was edited, so there is no
// request to keep the loop honest, and a deferred pass that counted as a
// success would leave the device unsynced for the whole hourly interval.
func TestLibraryStateLoop_DeferredStartupPassKeepsAsking(t *testing.T) {
	t.Parallel()
	runner := &fakeLibraryStateRunner{}
	runner.setErr(librarysync.ErrNotSettled)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		libraryStateLoop(ctx, runner, make(chan struct{}), &libraryStateTimings{
			check: 10 * time.Millisecond, startup: 10 * time.Millisecond, debounce: time.Hour,
			interval: time.Hour, initialBackoff: time.Hour, maxBackoff: time.Hour,
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, func() bool { return runner.passes.Load() >= 3 }, 2*time.Second, 5*time.Millisecond,
		"a startup pass deferred by indexing keeps asking without an edit to prompt it")

	runner.setErr(nil)
	require.Eventually(t, func() bool { return runner.passes.Load() >= 4 }, 2*time.Second, 5*time.Millisecond)
	settled := runner.passes.Load()
	time.Sleep(100 * time.Millisecond)
	assert.LessOrEqual(t, runner.passes.Load(), settled+1, "a synced pass stops the retries")
}
