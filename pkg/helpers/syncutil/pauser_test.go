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

package syncutil

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPauser_WaitReturnsImmediatelyWhenNotPaused(t *testing.T) {
	p := NewPauser()
	err := p.Wait(context.Background())
	assert.NoError(t, err)
}

func TestPauser_WaitBlocksWhenPaused(t *testing.T) {
	p := NewPauser()
	p.Pause()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := p.Wait(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPauser_WaitUnblocksOnResume(t *testing.T) {
	p := NewPauser()
	p.Pause()

	done := make(chan error, 1)
	go func() {
		done <- p.Wait(context.Background())
	}()

	// Give the goroutine time to block
	time.Sleep(10 * time.Millisecond)
	p.Resume()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Wait did not unblock after Resume")
	}
}

func TestPauser_WaitReturnsCancelledContext(t *testing.T) {
	p := NewPauser()
	p.Pause()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Wait(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPauser_PauseIsIdempotent(t *testing.T) {
	p := NewPauser()
	p.Pause()
	p.Pause() // should not panic or deadlock

	require.True(t, p.IsPaused())
}

func TestPauser_ResumeIsIdempotent(t *testing.T) {
	p := NewPauser()
	p.Resume() // not paused, should not panic
	p.Resume()

	require.False(t, p.IsPaused())
}

func TestPauser_IsPaused(t *testing.T) {
	p := NewPauser()
	assert.False(t, p.IsPaused())

	p.Pause()
	assert.True(t, p.IsPaused())

	p.Resume()
	assert.False(t, p.IsPaused())
}

func TestPauser_NilReceiverWaitReturnsNil(t *testing.T) {
	var p *Pauser
	err := p.Wait(context.Background())
	assert.NoError(t, err)
}

func TestPauser_NilReceiverIsPausedReturnsFalse(t *testing.T) {
	var p *Pauser
	assert.False(t, p.IsPaused())
}

func TestPauser_NilReceiverIsThrottledReturnsFalse(t *testing.T) {
	var p *Pauser
	assert.False(t, p.IsThrottled())
}

func TestPauser_NilReceiverLevelReturnsLight(t *testing.T) {
	var p *Pauser
	assert.Equal(t, ThrottleLight, p.Level())
}

func TestPauser_MultipleWaitersUnblocked(t *testing.T) {
	p := NewPauser()
	p.Pause()

	const n = 10
	done := make(chan error, n)
	for range n {
		go func() {
			done <- p.Wait(context.Background())
		}()
	}

	time.Sleep(10 * time.Millisecond)
	p.Resume()

	for range n {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("not all waiters unblocked")
		}
	}
}

func TestPauser_ThrottleAllowsWorkWithinQuantum(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleLight)
	p.SetThrottleQuanta(time.Hour, time.Hour)

	// Work window has not expired, so Wait must return immediately.
	err := p.Wait(context.Background())
	require.NoError(t, err)
	assert.True(t, p.IsThrottled())
	assert.False(t, p.IsPaused())
}

func TestPauser_ThrottleSleepsAfterQuantumExpires(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleLight)
	p.SetThrottleQuanta(time.Nanosecond, 50*time.Millisecond)

	// Ensure the work window is expired.
	time.Sleep(time.Millisecond)

	start := time.Now()
	err := p.Wait(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 40*time.Millisecond, "Wait should have slept for the sleep quantum")
}

func TestPauser_ThrottleResetsWindowAfterSleep(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleLight)
	p.SetThrottleQuanta(time.Hour, time.Millisecond)

	// Force the first window to be expired.
	p.mu.Lock()
	p.workStart = time.Now().Add(-2 * time.Hour)
	p.mu.Unlock()

	require.NoError(t, p.Wait(context.Background()))

	// Window was reset to a fresh hour-long quantum: immediate return.
	start := time.Now()
	require.NoError(t, p.Wait(context.Background()))
	assert.Less(t, time.Since(start), 10*time.Millisecond)
}

func TestPauser_ThrottleCancelledDuringSleep(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleLight)
	p.SetThrottleQuanta(time.Nanosecond, time.Hour)
	time.Sleep(time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := p.Wait(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPauser_ResumeInterruptsThrottleSleep(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleLight)
	p.SetThrottleQuanta(time.Nanosecond, time.Hour)
	time.Sleep(time.Millisecond)

	done := make(chan error, 1)
	go func() {
		done <- p.Wait(context.Background())
	}()

	time.Sleep(10 * time.Millisecond)
	p.Resume()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Wait did not unblock after Resume during throttle sleep")
	}
	assert.False(t, p.IsThrottled())
}

func TestPauser_PauseOverridesThrottle(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleLight)
	p.Pause()

	assert.True(t, p.IsPaused())
	assert.False(t, p.IsThrottled())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := p.Wait(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPauser_ThrottleReleasesPausedWaiters(t *testing.T) {
	p := NewPauser()
	p.SetThrottleQuanta(time.Hour, time.Hour)
	p.Pause()

	done := make(chan error, 1)
	go func() {
		done <- p.Wait(context.Background())
	}()

	time.Sleep(10 * time.Millisecond)
	p.Throttle(ThrottleLight)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("paused waiter was not released into throttled state")
	}
	assert.True(t, p.IsThrottled())
}

func TestPauser_ThrottleIsIdempotent(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleLight)
	p.Throttle(ThrottleLight) // should not panic or reset state

	assert.True(t, p.IsThrottled())
	p.Resume()
	assert.False(t, p.IsThrottled())
}

func TestPauser_ThrottleLightUsesLightPreset(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleLight)

	assert.Equal(t, ThrottleLight, p.Level())
	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Equal(t, lightThrottleWork, p.workQuantum)
	assert.Equal(t, lightThrottleSleep, p.sleepQuantum)
}

func TestPauser_ThrottleHeavyUsesHeavyPreset(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleHeavy)

	assert.Equal(t, ThrottleHeavy, p.Level())
	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Equal(t, heavyThrottleWork, p.workQuantum)
	assert.Equal(t, heavyThrottleSleep, p.sleepQuantum)
}

func TestPauser_ThrottleLevelSwitchUpdatesQuanta(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleLight)
	require.Equal(t, ThrottleLight, p.Level())

	p.Throttle(ThrottleHeavy)
	assert.Equal(t, ThrottleHeavy, p.Level())
	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Equal(t, heavyThrottleWork, p.workQuantum)
	assert.Equal(t, heavyThrottleSleep, p.sleepQuantum)
}

func TestPauser_ThrottleSameLevelIsNoOpOnWorkWindow(t *testing.T) {
	p := NewPauser()
	p.Throttle(ThrottleHeavy)

	p.mu.Lock()
	firstWorkStart := p.workStart
	p.mu.Unlock()

	time.Sleep(time.Millisecond)
	p.Throttle(ThrottleHeavy) // same level: idempotent, must not reset the window

	p.mu.Lock()
	defer p.mu.Unlock()
	assert.Equal(t, firstWorkStart, p.workStart)
}

func TestPauser_PauseResumeRapidCycling(t *testing.T) {
	p := NewPauser()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if err := p.Wait(ctx); err != nil {
				return
			}
		}
	}()

	for range 100 {
		p.Pause()
		p.Resume()
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine did not exit after cancel")
	}
}
