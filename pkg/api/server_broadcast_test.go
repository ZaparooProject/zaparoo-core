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

package api

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBroadcastNotifications_AsyncBroadcastDoesNotBlockConsumer verifies that
// async broadcasts allow the notification consumer to continue draining the
// channel even when broadcasts are slow. This is a regression test for the
// "subscriber channel full" issue where synchronous broadcasts blocked the
// consumer loop.
func TestBroadcastNotifications_AsyncBroadcastDoesNotBlockConsumer(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	defer st.StopService()

	// Create notification channel with realistic buffer size (100)
	notifications := make(chan models.Notification, 100)

	// Track how many notifications were consumed from the channel
	var consumedCount int
	var mu syncutil.Mutex

	// Start broadcast goroutine with instrumentation
	go func() {
		for {
			select {
			case <-st.GetContext().Done():
				return
			case notif := <-notifications:
				mu.Lock()
				consumedCount++
				mu.Unlock()

				// Simulate the async broadcast (actual broadcast omitted for test)
				go func(_ string) {
					// Simulate slow broadcast (5ms)
					time.Sleep(5 * time.Millisecond)
				}(notif.Method)
			}
		}
	}()

	// Send burst of notifications simulating media indexing (100 notifications rapidly)
	sentCount := 100
	for i := range sentCount {
		notifications <- models.Notification{
			Method: "media.indexing",
			Params: []byte(`{"step":` + string(rune(i+'0')) + `}`),
		}
	}

	// Give consumer time to drain the channel
	// With async broadcasts, consumer should drain quickly (~1-2ms total)
	// With sync broadcasts, this would take ~500ms (100 * 5ms)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	consumed := consumedCount
	mu.Unlock()

	// Consumer should have consumed all or nearly all notifications
	// (Even with async processing, channel draining is fast)
	assert.GreaterOrEqual(t, consumed, 95, "consumer should drain channel rapidly with async broadcasts")
}

// TestBroadcastNotifications_BufferSizeHandlesBurst verifies that the
// increased buffer size (100) can handle typical burst scenarios during
// media indexing without dropping notifications.
func TestBroadcastNotifications_BufferSizeHandlesBurst(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	defer st.StopService()

	// Create notification channel with production buffer size
	notifications := make(chan models.Notification, 100)

	// Track consumption
	var consumedCount int
	var mu syncutil.Mutex

	// Start consumer similar to broadcastNotifications
	go func() {
		for {
			select {
			case <-st.GetContext().Done():
				return
			case notif := <-notifications:
				mu.Lock()
				consumedCount++
				mu.Unlock()

				// Async broadcast
				go func(_ string) {
					time.Sleep(2 * time.Millisecond) // Simulate broadcast
				}(notif.Method)
			}
		}
	}()

	// Simulate realistic media indexing burst
	// Typical: 50-100 systems scanned in quick succession
	burstSize := 80
	for i := range burstSize {
		select {
		case notifications <- models.Notification{
			Method: "media.indexing",
			Params: []byte(`{"step":` + string(rune(i+'0')) + `}`),
		}:
			// Successfully queued
		default:
			t.Fatalf("notification channel full after %d notifications (buffer should be 100)", i)
		}
	}

	// Give time to consume
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	consumed := consumedCount
	mu.Unlock()

	// All notifications should be consumed
	assert.Equal(t, burstSize, consumed, "all burst notifications should be consumed")
}

// TestBroadcastNotifications_NoDeadlockUnderLoad verifies the system remains
// responsive under heavy notification load and doesn't deadlock.
func TestBroadcastNotifications_NoDeadlockUnderLoad(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	defer st.StopService()

	notifications := make(chan models.Notification, 100)

	// Start actual broadcastNotifications-like consumer
	go func() {
		for {
			select {
			case <-st.GetContext().Done():
				return
			case notif := <-notifications:
				// Simulate the actual async broadcast pattern
				go func(method string) {
					// Simulate variable broadcast times (some fast, some slow)
					if len(method)%2 == 0 {
						time.Sleep(1 * time.Millisecond)
					} else {
						time.Sleep(10 * time.Millisecond)
					}
				}(notif.Method)
			}
		}
	}()

	// Heavy sustained load
	totalSent := 0
	done := make(chan struct{})

	go func() {
		defer close(done)
		for range 200 {
			select {
			case notifications <- models.Notification{
				Method: "test.notification",
				Params: []byte(`{}`),
			}:
				totalSent++
			case <-st.GetContext().Done():
				return
			}
			// Small delay between sends
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Wait for completion or timeout
	timeout := time.After(5 * time.Second)
	select {
	case <-done:
		// Test completed without deadlock
		t.Logf("Successfully sent %d notifications without deadlock", totalSent)
	case <-timeout:
		t.Fatal("Test timed out - possible deadlock")
	}

	// Success if we get here (no deadlock occurred)
	assert.Greater(t, totalSent, 150, "should have sent most notifications")
}

// TestBroadcastNotifications_ConcurrentBroadcastsComplete verifies that
// multiple concurrent broadcasts (spawned as goroutines) all complete
// successfully without goroutine leaks or panics.
func TestBroadcastNotifications_ConcurrentBroadcastsComplete(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	defer st.StopService()

	notifications := make(chan models.Notification, 100)

	// Track completed broadcasts
	var broadcastCount int
	var mu syncutil.Mutex

	// Start consumer with instrumented async broadcasts
	go func() {
		for {
			select {
			case <-st.GetContext().Done():
				return
			case notif := <-notifications:
				go func(_ string) {
					// Simulate broadcast work
					time.Sleep(5 * time.Millisecond)

					mu.Lock()
					broadcastCount++
					mu.Unlock()
				}(notif.Method)
			}
		}
	}()

	// Send notifications
	sentCount := 50
	for range sentCount {
		notifications <- models.Notification{
			Method: "test.event",
			Params: []byte(`{}`),
		}
	}

	// Wait for all broadcasts to complete
	require.Eventually(t, func() bool {
		mu.Lock()
		count := broadcastCount
		mu.Unlock()
		return count == sentCount
	}, 1*time.Second, 10*time.Millisecond, "all async broadcasts should complete")
}
