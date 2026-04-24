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
