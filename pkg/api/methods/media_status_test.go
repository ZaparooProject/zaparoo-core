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

package methods

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMediaIndexStatus_StateManagement(t *testing.T) {
	// Reset status for clean test
	statusInstance.clear()

	t.Run("initial state", func(t *testing.T) {
		assert.False(t, statusInstance.isRunning(), "Should not be running initially")
		assert.Nil(t, statusInstance.getCancelFunc(), "Should have no cancel function initially")
	})

	t.Run("set running state", func(t *testing.T) {
		statusInstance.setRunning(true)
		assert.True(t, statusInstance.isRunning(), "Should be running after setting true")

		statusInstance.setRunning(false)
		assert.False(t, statusInstance.isRunning(), "Should not be running after setting false")
	})

	t.Run("set cancel function", func(t *testing.T) {
		_, cancelFunc := context.WithCancel(context.Background())
		defer cancelFunc()

		statusInstance.setCancelFunc(cancelFunc)
		assert.NotNil(t, statusInstance.getCancelFunc(), "Should have cancel function after setting")

		statusInstance.setCancelFunc(nil)
		assert.Nil(t, statusInstance.getCancelFunc(), "Should have no cancel function after clearing")
	})

	t.Run("clear state", func(t *testing.T) {
		// Set some state
		_, cancelFunc := context.WithCancel(context.Background())
		defer cancelFunc()

		statusInstance.setRunning(true)
		statusInstance.setCancelFunc(cancelFunc)

		// Clear state
		statusInstance.clear()

		assert.False(t, statusInstance.isRunning(), "Should not be running after clear")
		assert.Nil(t, statusInstance.getCancelFunc(), "Should have no cancel function after clear")
	})
}

func TestMediaIndexStatus_ThreadSafety(t *testing.T) {
	// Reset status for clean test
	statusInstance.clear()

	const numGoroutines = 100
	const iterations = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Test concurrent access to status
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			for range iterations {
				// Create unique context for each goroutine/iteration
				_, cancelFunc := context.WithCancel(context.Background())

				// Concurrent operations
				statusInstance.setRunning(id%2 == 0) // Alternate true/false
				statusInstance.setCancelFunc(cancelFunc)
				_ = statusInstance.isRunning()
				_ = statusInstance.getCancelFunc()

				// Cleanup
				cancelFunc()
				time.Sleep(time.Microsecond) // Small delay to increase chance of race conditions
			}
		}(i)
	}

	// Wait for all goroutines to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no race conditions detected
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out - possible deadlock")
	}

	// Final cleanup
	statusInstance.clear()
}

func TestMediaIndexStatus_CancelFunctionBehavior(t *testing.T) {
	// Reset status for clean test
	statusInstance.clear()

	t.Run("cancel function works correctly", func(t *testing.T) {
		statusInstance.clear() // Clean state first

		ctx, cancelFunc := context.WithCancel(context.Background())
		statusInstance.setCancelFunc(cancelFunc)
		statusInstance.setRunning(true) // Need to be running for cancel to work

		// Verify context is not cancelled initially
		select {
		case <-ctx.Done():
			t.Fatal("Context should not be cancelled initially")
		default:
			// Expected
		}

		// Use the status cancel method (not direct function call)
		cancelled := statusInstance.cancel()
		assert.True(t, cancelled, "Cancel should return true when successful")

		// Verify context is now cancelled
		select {
		case <-ctx.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Context should be cancelled after calling cancel function")
		}

		// Verify error is context.Canceled
		assert.Equal(t, context.Canceled, ctx.Err())

		// Clean up
		statusInstance.clear()
	})

	t.Run("multiple cancel calls are safe", func(t *testing.T) {
		statusInstance.clear() // Clean state first

		ctx, cancelFunc := context.WithCancel(context.Background())
		statusInstance.setCancelFunc(cancelFunc)
		statusInstance.setRunning(true)

		// Multiple calls to status cancel method
		cancelled1 := statusInstance.cancel()
		cancelled2 := statusInstance.cancel() // Should return false since no longer running
		cancelled3 := statusInstance.cancel()

		assert.True(t, cancelled1, "First cancel should succeed")
		assert.False(t, cancelled2, "Second cancel should fail (not running)")
		assert.False(t, cancelled3, "Third cancel should fail (not running)")

		// Context should still be cancelled correctly
		assert.Equal(t, context.Canceled, ctx.Err())

		// Clean up
		statusInstance.clear()
	})

	t.Run("nil cancel function is safe", func(t *testing.T) {
		statusInstance.clear() // Clear any previous state
		statusInstance.setCancelFunc(nil)
		cancelFunc := statusInstance.getCancelFunc()
		assert.Nil(t, cancelFunc, "Should return nil when no cancel function is set")
	})
}

func TestMediaIndexStatus_ConcurrentCancellation(t *testing.T) {
	// Reset status for clean test
	statusInstance.clear()

	// Setup indexing state
	ctx, cancelFunc := context.WithCancel(context.Background())
	statusInstance.setRunning(true)
	statusInstance.setCancelFunc(cancelFunc)

	const numCancellers = 10
	var wg sync.WaitGroup
	wg.Add(numCancellers)

	cancelCalled := make([]bool, numCancellers)

	// Launch multiple goroutines that try to cancel
	for i := range numCancellers {
		go func(id int) {
			defer wg.Done()

			cancelled := statusInstance.cancel()
			if cancelled {
				cancelCalled[id] = true
			}
		}(i)
	}

	// Wait for all cancellers to complete
	wg.Wait()

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled")
	}

	// Count how many succeeded (should be exactly one)
	successCount := 0
	for _, called := range cancelCalled {
		if called {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "Exactly one goroutine should have succeeded in cancelling")

	// Cleanup
	statusInstance.clear()
}

func TestMediaIndexStatus_StateTransitions(t *testing.T) {
	// Reset status for clean test
	statusInstance.clear()

	// Test typical indexing lifecycle
	t.Run("normal indexing lifecycle", func(t *testing.T) {
		// 1. Start indexing
		_, cancelFunc := context.WithCancel(context.Background())
		statusInstance.setRunning(true)
		statusInstance.setCancelFunc(cancelFunc)

		assert.True(t, statusInstance.isRunning())
		assert.NotNil(t, statusInstance.getCancelFunc())

		// 2. Complete indexing normally
		statusInstance.clear()

		assert.False(t, statusInstance.isRunning())
		assert.Nil(t, statusInstance.getCancelFunc())

		// Cleanup
		cancelFunc()
	})

	t.Run("cancelled indexing lifecycle", func(t *testing.T) {
		// 1. Start indexing
		ctx, cancelFunc := context.WithCancel(context.Background())
		statusInstance.setRunning(true)
		statusInstance.setCancelFunc(cancelFunc)

		assert.True(t, statusInstance.isRunning())
		assert.NotNil(t, statusInstance.getCancelFunc())

		// 2. Cancel indexing using the status cancel method
		cancelled := statusInstance.cancel()
		assert.True(t, cancelled, "Cancel should succeed")

		// 3. Status should automatically be not running after cancel
		assert.False(t, statusInstance.isRunning())

		// 4. Clear the rest of the state
		statusInstance.clear()
		assert.Nil(t, statusInstance.getCancelFunc())

		// Verify context was cancelled
		assert.Equal(t, context.Canceled, ctx.Err())
	})

	t.Run("rapid state changes", func(t *testing.T) {
		// Test rapid setting and clearing of state
		for range 100 {
			_, cancelFunc := context.WithCancel(context.Background())

			statusInstance.setRunning(true)
			statusInstance.setCancelFunc(cancelFunc)
			statusInstance.clear()

			// Cleanup context
			cancelFunc()
		}

		// Final state should be clear
		assert.False(t, statusInstance.isRunning())
		assert.Nil(t, statusInstance.getCancelFunc())
	})
}
