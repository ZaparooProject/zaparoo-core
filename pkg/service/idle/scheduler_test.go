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

package idle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_RequestCounter(t *testing.T) {
	t.Parallel()
	s := New()
	assert.Equal(t, int64(0), s.InFlight())

	s.RequestStarted()
	s.RequestStarted()
	assert.Equal(t, int64(2), s.InFlight())

	s.RequestEnded()
	assert.Equal(t, int64(1), s.InFlight())

	s.RequestEnded()
	assert.Equal(t, int64(0), s.InFlight())
}

// TestScheduler_RequestEnded_UnmatchedClampsToZero asserts that an unmatched
// RequestEnded — double release, or release without a matching start — does
// not drive inFlight negative. A negative counter would prevent
// WaitForIdle's `inFlight == 0` predicate from ever holding, wedging waiters
// until the maxWait hard cap.
func TestScheduler_RequestEnded_UnmatchedClampsToZero(t *testing.T) {
	t.Parallel()
	fc := clockwork.NewFakeClock()
	s := NewWithClock(fc)
	ctx := context.Background()

	// Unmatched RequestEnded with no prior RequestStarted.
	s.RequestEnded()
	assert.Equal(t, int64(0), s.InFlight(), "inFlight must clamp at zero, not go negative")

	// Double release: one start, two ends.
	s.RequestStarted()
	s.RequestEnded()
	s.RequestEnded()
	assert.Equal(t, int64(0), s.InFlight())

	// WaitForIdle must still fire after the quiet window despite the
	// underflow attempts above.
	errCh := runWaitForIdle(ctx, s, 80*time.Millisecond, 5*time.Second)
	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	fc.Advance(81 * time.Millisecond)

	select {
	case err := <-errCh:
		require.NoError(t, err, "WaitForIdle should return nil, not ErrMaxWaitElapsed")
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForIdle wedged after unmatched RequestEnded")
	}
}

// runWaitForIdle starts WaitForIdle in a goroutine and returns a channel that
// receives the call's error. The test drives the FakeClock to make the call
// return.
func runWaitForIdle(
	ctx context.Context, s *Scheduler, quietWindow, maxWait time.Duration,
) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.WaitForIdle(ctx, quietWindow, maxWait)
	}()
	return errCh
}

func TestScheduler_WaitForIdle_FiresAfterQuietWindow(t *testing.T) {
	t.Parallel()
	fc := clockwork.NewFakeClock()
	s := NewWithClock(fc)
	ctx := context.Background()

	// Drive a request through the counter so lastRequestEndedAt is "now".
	s.RequestStarted()
	s.RequestEnded()

	errCh := runWaitForIdle(ctx, s, 80*time.Millisecond, 5*time.Second)

	// Wait for WaitForIdle to set up its timer, then advance just past the
	// quiet window so the predicate trips on the next iteration.
	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	fc.Advance(81 * time.Millisecond)

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForIdle did not return after advancing past quiet window")
	}
}

func TestScheduler_WaitForIdle_DoesNotFireBeforeQuietWindow(t *testing.T) {
	t.Parallel()
	fc := clockwork.NewFakeClock()
	s := NewWithClock(fc)
	ctx := context.Background()

	s.RequestStarted()
	s.RequestEnded()

	errCh := runWaitForIdle(ctx, s, 80*time.Millisecond, 5*time.Second)

	// Advance just under the quiet window. The predicate must not trip and
	// WaitForIdle must keep waiting.
	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	fc.Advance(79 * time.Millisecond)

	select {
	case err := <-errCh:
		t.Fatalf("WaitForIdle returned early after %dms (under %dms quiet window): err=%v",
			79, 80, err)
	case <-time.After(50 * time.Millisecond):
		// Expected: still waiting.
	}

	// Now push past the quiet window — it should fire.
	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	fc.Advance(5 * time.Millisecond)

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForIdle did not return after fully advancing past quiet window")
	}
}

func TestScheduler_WaitForIdle_MaxWaitCap(t *testing.T) {
	t.Parallel()
	fc := clockwork.NewFakeClock()
	s := NewWithClock(fc)
	ctx := context.Background()

	// Hold one request open the whole time so InFlight never goes to zero.
	s.RequestStarted()
	defer s.RequestEnded()

	errCh := runWaitForIdle(ctx, s, 100*time.Millisecond, 200*time.Millisecond)

	// Advance past maxWait. The poll loop reads `now` once per iteration, so
	// we may need to step the clock multiple times to reach the deadline.
	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	fc.Advance(250 * time.Millisecond)

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, ErrMaxWaitElapsed,
			"maxWait timeout should return ErrMaxWaitElapsed sentinel")
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForIdle did not return after advancing past maxWait")
	}
}

func TestScheduler_WaitForIdle_ContextCancellation(t *testing.T) {
	t.Parallel()
	fc := clockwork.NewFakeClock()
	s := NewWithClock(fc)

	// Hold a request so quiet window can never fire on its own.
	s.RequestStarted()
	defer s.RequestEnded()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := runWaitForIdle(ctx, s, 1*time.Second, 10*time.Second)

	// Wait for the goroutine to be parked on the timer, then cancel.
	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForIdle did not return after context cancellation")
	}
}

func TestScheduler_WaitForIdle_QuietWindowResetsOnRequest(t *testing.T) {
	t.Parallel()
	fc := clockwork.NewFakeClock()
	s := NewWithClock(fc)
	ctx := context.Background()

	// First request closes well before WaitForIdle starts.
	s.RequestStarted()
	s.RequestEnded()

	errCh := runWaitForIdle(ctx, s, 100*time.Millisecond, 5*time.Second)

	// Mid-wait, fire another request. lastRequestEndedAt updates to fake-now,
	// so the next idle anchor moves forward by 100ms.
	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	fc.Advance(40 * time.Millisecond)
	s.RequestStarted()
	s.RequestEnded()

	// 41ms after the second RequestEnded should NOT be enough to satisfy the
	// 100ms quiet window; WaitForIdle must still be running.
	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	fc.Advance(41 * time.Millisecond)
	select {
	case err := <-errCh:
		t.Fatalf("WaitForIdle returned too early: %v", err)
	case <-time.After(50 * time.Millisecond):
		// Expected — still waiting.
	}

	// Advance past the reset quiet window.
	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	fc.Advance(70 * time.Millisecond)
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForIdle did not return after reset quiet window")
	}
}

func TestScheduler_Schedule_RunsTaskAfterIdle(t *testing.T) {
	t.Parallel()
	fc := clockwork.NewFakeClock()
	s := NewWithClock(fc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer s.Wait()

	s.RequestStarted()
	s.RequestEnded()

	var ran atomic.Bool
	done := make(chan struct{})
	s.Schedule(ctx, "test-task", 50*time.Millisecond, 5*time.Second, func(_ context.Context) {
		ran.Store(true)
		close(done)
	})

	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	fc.Advance(60 * time.Millisecond)

	select {
	case <-done:
		assert.True(t, ran.Load())
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled task did not run within timeout")
	}
}

func TestScheduler_Schedule_ContextCancelSkipsTask(t *testing.T) {
	t.Parallel()
	fc := clockwork.NewFakeClock()
	s := NewWithClock(fc)
	ctx, cancel := context.WithCancel(context.Background())
	defer s.Wait()

	// Hold inFlight high so quiet window can never fire.
	s.RequestStarted()
	defer s.RequestEnded()

	var ran atomic.Bool
	s.Schedule(ctx, "doomed-task", 1*time.Second, 10*time.Second, func(_ context.Context) {
		ran.Store(true)
	})

	require.NoError(t, fc.BlockUntilContext(ctx, 1))
	cancel()

	// Give the goroutine a moment to observe ctx.Done() and exit.
	s.Wait()

	assert.False(t, ran.Load(), "task must not run when ctx cancels first")
}
