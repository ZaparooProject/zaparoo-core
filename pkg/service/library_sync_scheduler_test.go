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
	"testing/synctest"
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

// fakeLibraryStateRunner is driven from inside a synctest bubble, so it keeps
// to atomics: the deadlock build's mutex draws timers from a pool shared with
// goroutines outside the bubble, which a bubble's clock cannot survive. Tests
// only change the error once synctest.Wait has shown no pass is running.
type fakeLibraryStateRunner struct {
	err        atomic.Pointer[error]
	passes     atomic.Int32
	deckPasses atomic.Int32
	deckPulls  atomic.Int32
	cursor     atomic.Int64
}

func (r *fakeLibraryStateRunner) setErr(err error) {
	if err == nil {
		r.err.Store(nil)
		return
	}
	r.err.Store(&err)
}

func (r *fakeLibraryStateRunner) SyncState(context.Context) (librarysync.StateResult, error) {
	r.passes.Add(1)
	if err := r.err.Load(); err != nil {
		return librarysync.StateResult{}, *err
	}
	return librarysync.StateResult{}, nil
}

func (r *fakeLibraryStateRunner) SyncDecks(context.Context) (librarysync.DecksResult, error) {
	r.deckPasses.Add(1)
	return librarysync.DecksResult{}, nil
}

func (r *fakeLibraryStateRunner) PullDecksIfStale(context.Context) error {
	r.deckPulls.Add(1)
	return nil
}

// HintNeedsPull treats revisions above the fake cursor as unseen.
func (r *fakeLibraryStateRunner) HintNeedsPull(_ string, revision int64) bool {
	return revision > r.cursor.Load()
}

// testLibraryStateLoop is a running state loop and the channels that drive it.
type testLibraryStateLoop struct {
	requests chan struct{}
	accesses chan struct{}
	hints    chan libraryHint
	// stop ends the loop; a bubble cannot finish while the loop still runs.
	stop func()
}

// startTestLibraryStateLoop runs the state loop inside a synctest bubble, so
// its timers run on the bubble's clock: a test moves time with time.Sleep and
// lets the loop catch up with synctest.Wait, and nothing depends on how the
// scheduler treats a real sleep.
func startTestLibraryStateLoop(
	t *testing.T, runner *fakeLibraryStateRunner, pipe *atomic.Bool, timings *libraryStateTimings,
) *testLibraryStateLoop {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	loop := &testLibraryStateLoop{
		requests: make(chan struct{}, 1),
		accesses: make(chan struct{}, 1),
		hints:    make(chan libraryHint, 4),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		libraryStateLoop(ctx, runner, loop.requests, loop.accesses, loop.hints, pipe.Load, timings)
	}()
	loop.stop = func() {
		cancel()
		<-done
	}
	return loop
}

func TestLibraryStateLoop_HintRunsAPassWhenItNamesAnUnseenWrite(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		runner := &fakeLibraryStateRunner{}
		runner.cursor.Store(5)
		var pipe atomic.Bool
		loop := startTestLibraryStateLoop(t, runner, &pipe, &libraryStateTimings{
			check: time.Hour, startup: time.Hour, debounce: 20 * time.Millisecond,
			interval: time.Hour, intervalNoPipe: time.Hour, initialBackoff: time.Hour, maxBackoff: time.Hour,
		})
		defer loop.stop()

		loop.hints <- libraryHint{kinds: []string{"state"}, revision: 3}
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Zero(t, runner.passes.Load(), "a hint at or below the cursor is an echo")

		loop.hints <- libraryHint{kinds: []string{"decks", "state"}, revision: 9}
		time.Sleep(19 * time.Millisecond)
		synctest.Wait()
		assert.Zero(t, runner.passes.Load(), "the hint waits out the debounce like an edit")
		time.Sleep(2 * time.Millisecond)
		synctest.Wait()
		assert.Equal(t, int32(1), runner.passes.Load())
	})
}

func TestLibraryStateLoop_PipeStateSelectsTheTimer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		runner := &fakeLibraryStateRunner{}
		var pipe atomic.Bool
		pipe.Store(true)
		// The timer only runs passes once the startup pass has happened.
		loop := startTestLibraryStateLoop(t, runner, &pipe, &libraryStateTimings{
			check: time.Minute, startup: time.Minute, debounce: time.Second,
			interval: 15 * time.Minute, intervalNoPipe: 5 * time.Minute,
			initialBackoff: time.Hour, maxBackoff: time.Hour,
		})
		defer loop.stop()

		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, int32(1), runner.passes.Load(), "the startup pass")

		time.Sleep(10 * time.Minute)
		synctest.Wait()
		assert.Equal(t, int32(1), runner.passes.Load(), "with the pipe held the long timer applies")
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		assert.Equal(t, int32(2), runner.passes.Load(), "the long timer runs a pass when it is due")

		pipe.Store(false)
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		assert.Equal(t, int32(3), runner.passes.Load(), "without the pipe the short timer applies")
	})
}

func TestLibraryStateLoop_DebouncesEdits(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		runner := &fakeLibraryStateRunner{}
		var pipe atomic.Bool
		loop := startTestLibraryStateLoop(t, runner, &pipe, &libraryStateTimings{
			check: time.Hour, startup: time.Hour, debounce: 20 * time.Millisecond,
			interval: time.Hour, intervalNoPipe: time.Hour, initialBackoff: time.Hour, maxBackoff: time.Hour,
		})
		defer loop.stop()

		for range 3 {
			loop.requests <- struct{}{}
			time.Sleep(5 * time.Millisecond)
		}
		synctest.Wait()
		assert.Zero(t, runner.passes.Load(), "each edit restarts the debounce")
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Equal(t, int32(1), runner.passes.Load(), "a burst of edits is pushed in one pass")
		assert.Equal(t, int32(1), runner.deckPasses.Load(), "decks sync in the same pass")

		loop.accesses <- struct{}{}
		synctest.Wait()
		assert.Equal(t, int32(1), runner.deckPulls.Load())
		assert.Equal(t, int32(1), runner.passes.Load(), "looking at decks does not push")
	})
}

// TestLibraryStateLoop_DeferredPassKeepsAsking pins an edit made while the
// media database is indexing: the pass defers, and the loop asks again at the
// check interval instead of counting the deferral as a success and waiting out
// the hour.
func TestLibraryStateLoop_DeferredPassKeepsAsking(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		runner := &fakeLibraryStateRunner{}
		runner.setErr(librarysync.ErrNotSettled)
		var pipe atomic.Bool
		loop := startTestLibraryStateLoop(t, runner, &pipe, &libraryStateTimings{
			check: time.Minute, startup: 24 * time.Hour, debounce: time.Second,
			interval: time.Hour, intervalNoPipe: time.Hour, initialBackoff: time.Hour, maxBackoff: time.Hour,
		})
		defer loop.stop()

		loop.requests <- struct{}{}
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		deferred := runner.passes.Load()
		assert.GreaterOrEqual(t, deferred, int32(3),
			"an edit deferred by indexing keeps asking rather than waiting out the hour")

		// The index finishes and the edit goes out, after which the loop settles.
		runner.setErr(nil)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		assert.Equal(t, deferred+1, runner.passes.Load(), "the edit goes out once the index finishes")
		time.Sleep(30 * time.Minute)
		synctest.Wait()
		assert.Equal(t, deferred+1, runner.passes.Load(), "a pushed edit stops the retries")
	})
}

// TestLibraryStateLoop_DeferredStartupPassKeepsAsking pins the first pass after
// a boot that runs straight into indexing. Nothing was edited, so there is no
// request to keep the loop honest, and a deferred pass that counted as a
// success would leave the device unsynced for the whole hourly interval.
func TestLibraryStateLoop_DeferredStartupPassKeepsAsking(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		runner := &fakeLibraryStateRunner{}
		runner.setErr(librarysync.ErrNotSettled)
		var pipe atomic.Bool
		loop := startTestLibraryStateLoop(t, runner, &pipe, &libraryStateTimings{
			check: time.Minute, startup: time.Minute, debounce: time.Second,
			interval: time.Hour, intervalNoPipe: time.Hour, initialBackoff: time.Hour, maxBackoff: time.Hour,
		})
		defer loop.stop()

		time.Sleep(5 * time.Minute)
		synctest.Wait()
		deferred := runner.passes.Load()
		assert.GreaterOrEqual(t, deferred, int32(3),
			"a startup pass deferred by indexing keeps asking without an edit to prompt it")

		runner.setErr(nil)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		assert.Equal(t, deferred+1, runner.passes.Load(), "the pass runs once the index finishes")
		time.Sleep(30 * time.Minute)
		synctest.Wait()
		assert.Equal(t, deferred+1, runner.passes.Load(), "a synced pass stops the retries")
	})
}

// TestOfferLibraryHint pins that the wait loop is never held up by Library
// sync: a hint that finds the channel full is dropped, not waited on.
func TestOfferLibraryHint(t *testing.T) {
	t.Parallel()
	hints := make(chan libraryHint, 1)

	offerLibraryHint(hints, []string{"state"}, 3)
	offerLibraryHint(hints, []string{"decks"}, 4)

	require.Len(t, hints, 1)
	got := <-hints
	assert.Equal(t, []string{"state"}, got.kinds)
	assert.Equal(t, int64(3), got.revision)
}
