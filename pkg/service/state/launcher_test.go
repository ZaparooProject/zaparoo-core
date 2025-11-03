// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package state

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLauncherManager_NewContext(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()
	ctx1 := lm.GetContext()

	// Create new context
	ctx2 := lm.NewContext()

	// Should be different contexts
	assert.NotEqual(t, ctx1, ctx2)

	// Old context should be cancelled
	require.Error(t, ctx1.Err())
	assert.Equal(t, context.Canceled, ctx1.Err())

	// New context should not be cancelled
	assert.NoError(t, ctx2.Err())
}

func TestLauncherManager_NewContext_CancelsMultipleTimes(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()
	ctx1 := lm.GetContext()
	ctx2 := lm.NewContext()
	ctx3 := lm.NewContext()

	// All old contexts should be cancelled
	require.Error(t, ctx1.Err())
	require.Error(t, ctx2.Err())

	// Latest context should not be cancelled
	assert.NoError(t, ctx3.Err())
}

func TestLauncherManager_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()
	done := make(chan bool)

	// Concurrent readers
	for range 10 {
		go func() {
			defer func() { done <- true }()
			for range 100 {
				_ = lm.GetContext()
			}
		}()
	}

	// Concurrent writers
	for range 5 {
		go func() {
			defer func() { done <- true }()
			for range 50 {
				_ = lm.NewContext()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Wait for all goroutines
	for range 15 {
		<-done
	}
}

func TestLauncherManager_ContextCancellationSignaling(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()
	ctx := lm.GetContext()

	// Goroutine waiting on context
	cancelled := make(chan bool)
	go func() {
		<-ctx.Done()
		cancelled <- true
	}()

	// Create new context (should cancel old one)
	_ = lm.NewContext()

	// Wait for cancellation signal
	select {
	case <-cancelled:
		// Success - context was cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context was not cancelled")
	}
}

func TestLauncherManager_LauncherSupersession(t *testing.T) {
	t.Parallel()

	// Simulate launcher lifecycle
	lm := NewLauncherManager()

	// First launcher starts
	launcher1Ctx := lm.GetContext()
	require.NoError(t, launcher1Ctx.Err())

	// Launcher 1 is running...
	time.Sleep(10 * time.Millisecond)

	// Second launcher starts (supersedes first)
	launcher2Ctx := lm.NewContext()

	// Launcher 1 context should be cancelled (cleanup can detect staleness)
	require.Error(t, launcher1Ctx.Err())

	// Launcher 2 context should be active
	assert.NoError(t, launcher2Ctx.Err())
}
