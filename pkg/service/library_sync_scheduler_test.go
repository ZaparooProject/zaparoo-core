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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/librarysync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLibrarySyncRunner struct {
	err    error
	passes atomic.Int32
	mu     syncutil.Mutex
}

func (*fakeLibrarySyncRunner) ApplySetting(context.Context) (bool, error) { return false, nil }

func (*fakeLibrarySyncRunner) DeleteInventory(context.Context) (bool, error) { return false, nil }

func (r *fakeLibrarySyncRunner) SyncInventory(context.Context, bool) (librarysync.InventoryResult, error) {
	r.passes.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	return librarysync.InventoryResult{Outcome: librarysync.InventoryUploaded}, r.err
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

type fakeLibraryStateRunner struct {
	passes atomic.Int32
}

func (r *fakeLibraryStateRunner) SyncState(context.Context) (librarysync.StateResult, error) {
	r.passes.Add(1)
	return librarysync.StateResult{}, nil
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
