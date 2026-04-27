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

package middleware_test

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestLastSeenTracker_TouchKeepsNewest(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	tr := middleware.NewLastSeenTracker(db)

	// Older then newer — newest must win.
	tr.Touch("token-a", 1700000100)
	tr.Touch("token-a", 1700000200)
	// Newer then older — older must be ignored.
	tr.Touch("token-a", 1700000050)

	db.On("UpdateClientLastSeen", "token-a", int64(1700000200)).Return(nil).Once()

	tr.Flush(context.Background())
	db.AssertExpectations(t)
}

func TestLastSeenTracker_TouchEmptyTokenIgnored(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	tr := middleware.NewLastSeenTracker(db)

	tr.Touch("", 1700000100)
	// No expectations on db — Flush must be a no-op.
	tr.Flush(context.Background())
	db.AssertExpectations(t)
}

func TestLastSeenTracker_FlushClearsDirty(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	tr := middleware.NewLastSeenTracker(db)

	tr.Touch("token-a", 1700000100)
	tr.Touch("token-b", 1700000200)

	db.On("UpdateClientLastSeen", "token-a", int64(1700000100)).Return(nil).Once()
	db.On("UpdateClientLastSeen", "token-b", int64(1700000200)).Return(nil).Once()
	tr.Flush(context.Background())
	db.AssertExpectations(t)

	// Second flush with no Touches — must not call the db again.
	tr.Flush(context.Background())
	db.AssertExpectations(t)
}

func TestLastSeenTracker_FlushIgnoresDBErrors(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	tr := middleware.NewLastSeenTracker(db)

	tr.Touch("token-a", 1700000100)
	tr.Touch("token-b", 1700000200)

	// First update fails, second still happens — partial failure must
	// not abort the rest of the flush.
	db.On("UpdateClientLastSeen", mock.Anything, mock.Anything).
		Return(assert.AnError).Once()
	db.On("UpdateClientLastSeen", mock.Anything, mock.Anything).
		Return(nil).Once()

	tr.Flush(context.Background())
	db.AssertExpectations(t)
}

func TestLastSeenTracker_StartFlushLoopFlushesOnShutdown(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	tr := middleware.NewLastSeenTracker(db)

	tr.Touch("token-a", 1700000100)

	// The flush goroutine must drain the dirty set when ctx is canceled,
	// even if the periodic ticker has not fired yet.
	done := make(chan struct{}, 1)
	db.On("UpdateClientLastSeen", "token-a", int64(1700000100)).
		Run(func(_ mock.Arguments) { done <- struct{}{} }).
		Return(nil).Once()

	ctx, cancel := context.WithCancel(context.Background())
	tr.StartFlushLoop(ctx, time.Hour) // long interval ⇒ ticker won't fire
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown flush did not run within 1s")
	}
	db.AssertExpectations(t)
}

func TestLastSeenTracker_StartFlushLoopDoneWaitsForFinalFlush(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	tr := middleware.NewLastSeenTracker(db)

	tr.Touch("token-a", 1700000100)

	flushStarted := make(chan struct{})
	releaseFlush := make(chan struct{})
	db.On("UpdateClientLastSeen", "token-a", int64(1700000100)).
		Run(func(_ mock.Arguments) {
			close(flushStarted)
			<-releaseFlush
		}).
		Return(nil).Once()

	ctx, cancel := context.WithCancel(context.Background())
	done := tr.StartFlushLoop(ctx, time.Hour)
	cancel()

	select {
	case <-flushStarted:
	case <-time.After(time.Second):
		t.Fatal("shutdown flush did not start within 1s")
	}

	select {
	case <-done:
		t.Fatal("flush loop reported done before final flush completed")
	default:
	}

	close(releaseFlush)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("flush loop did not finish after final flush completed")
	}
	db.AssertExpectations(t)
}

func TestLastSeenTracker_StartFlushLoopPeriodicFlush(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	tr := middleware.NewLastSeenTracker(db)

	done := make(chan struct{}, 1)
	db.On("UpdateClientLastSeen", "token-a", int64(1700000100)).
		Run(func(_ mock.Arguments) { done <- struct{}{} }).
		Return(nil).Once()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.StartFlushLoop(ctx, 5*time.Millisecond)

	tr.Touch("token-a", 1700000100)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("periodic flush did not run within 1s")
	}
	db.AssertExpectations(t)
}
