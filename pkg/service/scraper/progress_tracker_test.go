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

package scraper

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProgressTracker(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)
	require.NotNil(t, tracker)

	// Verify initial state
	progress := tracker.Get()
	assert.Equal(t, "idle", progress.Status)
	assert.Equal(t, 0, progress.ProcessedGames)
	assert.Equal(t, 0, progress.TotalGames)
	assert.Equal(t, "", progress.CurrentGame)
	assert.Equal(t, "", progress.LastError)
	assert.Equal(t, 0, progress.ErrorCount)
	assert.False(t, progress.IsRunning)
}

func TestProgressTracker_SetStatus(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)

	tracker.SetStatus("running")
	progress := tracker.Get()
	assert.Equal(t, "running", progress.Status)

	tracker.SetStatus("completed")
	progress = tracker.Get()
	assert.Equal(t, "completed", progress.Status)

	// Verify notification was sent
	select {
	case notification := <-notifChan:
		assert.Equal(t, "scraper.progress", notification.Method)
		assert.NotNil(t, notification.Params)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification but didn't receive one")
	}
}

func TestProgressTracker_SetProgress(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)

	tracker.SetProgress(5, 10)
	progress := tracker.Get()
	assert.Equal(t, 5, progress.ProcessedGames)
	assert.Equal(t, 10, progress.TotalGames)

	// Test progress update
	tracker.SetProgress(8, 10)
	progress = tracker.Get()
	assert.Equal(t, 8, progress.ProcessedGames)
	assert.Equal(t, 10, progress.TotalGames)
}

func TestProgressTracker_SetCurrentGame(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)

	tracker.SetCurrentGame("Super Mario Bros", 123)
	progress := tracker.Get()
	assert.Equal(t, "Super Mario Bros", progress.CurrentGame)
}

func TestProgressTracker_IncrementProgress(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)

	// Set initial total
	tracker.SetProgress(0, 10)

	// Increment multiple times
	tracker.IncrementProgress()
	progress := tracker.Get()
	assert.Equal(t, 1, progress.ProcessedGames)

	tracker.IncrementProgress()
	tracker.IncrementProgress()
	progress = tracker.Get()
	assert.Equal(t, 3, progress.ProcessedGames)
	assert.Equal(t, 10, progress.TotalGames)
}

func TestProgressTracker_SetError(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)

	// Set an error
	testError := errors.New("test error message")
	tracker.SetError(testError)

	progress := tracker.Get()
	assert.Equal(t, "test error message", progress.LastError)
	assert.Equal(t, 1, progress.ErrorCount)

	// Set another error
	anotherError := errors.New("another error")
	tracker.SetError(anotherError)

	progress = tracker.Get()
	assert.Equal(t, "another error", progress.LastError)
	assert.Equal(t, 2, progress.ErrorCount)

	// Clear error
	tracker.SetError(nil)
	progress = tracker.Get()
	assert.Equal(t, "", progress.LastError)
	assert.Equal(t, 2, progress.ErrorCount) // Error count shouldn't decrease
}

func TestProgressTracker_Reset(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)

	// Set some state
	tracker.SetStatus("running")
	tracker.SetProgress(5, 10)
	tracker.SetCurrentGame("Test Game", 123)
	tracker.SetError(errors.New("test error"))

	// Reset
	tracker.Reset()

	progress := tracker.Get()
	assert.Equal(t, "idle", progress.Status)
	assert.Equal(t, 0, progress.ProcessedGames)
	assert.Equal(t, 0, progress.TotalGames)
	assert.Equal(t, "", progress.CurrentGame)
	assert.Equal(t, "", progress.LastError)
	assert.Equal(t, 0, progress.ErrorCount)
	assert.False(t, progress.IsRunning)
}

func TestProgressTracker_Complete(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)

	// Set initial state
	tracker.SetProgress(8, 10)
	tracker.SetCurrentGame("Last Game", 123)
	tracker.SetError(errors.New("some error"))

	// Complete
	tracker.Complete()

	progress := tracker.Get()
	assert.Equal(t, "completed", progress.Status)
	assert.Equal(t, 10, progress.ProcessedGames) // Should match total
	assert.Equal(t, 10, progress.TotalGames)
	assert.Equal(t, "", progress.CurrentGame)
	assert.Equal(t, "", progress.LastError)
	assert.False(t, progress.IsRunning)

	// Verify completion notification was sent
	foundProgressNotif := false
	foundCompleteNotif := false

	// Drain notification channel
	for {
		select {
		case notification := <-notifChan:
			if notification.Method == "scraper.progress" {
				foundProgressNotif = true
			} else if notification.Method == "scraper.complete" {
				foundCompleteNotif = true
			}
		case <-time.After(50 * time.Millisecond):
			goto done
		}
	}

done:
	assert.True(t, foundProgressNotif, "Expected progress notification")
	assert.True(t, foundCompleteNotif, "Expected completion notification")
}

func TestProgressTracker_Cancel(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)

	// Set some running state
	tracker.SetStatus("running")
	tracker.SetProgress(3, 10)
	tracker.SetError(errors.New("some error"))

	// Cancel
	tracker.Cancel()

	progress := tracker.Get()
	assert.Equal(t, "cancelled", progress.Status)
	assert.Equal(t, "", progress.LastError)
	assert.False(t, progress.IsRunning)
	// Other fields should remain unchanged
	assert.Equal(t, 3, progress.ProcessedGames)
	assert.Equal(t, 10, progress.TotalGames)
}

func TestProgressTracker_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 100)
	tracker := NewProgressTracker(notifChan)

	const numGoroutines = 10
	const incrementsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Set initial total
	tracker.SetProgress(0, numGoroutines*incrementsPerGoroutine)

	// Launch multiple goroutines that increment progress concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				tracker.IncrementProgress()
			}
		}()
	}

	wg.Wait()

	progress := tracker.Get()
	assert.Equal(t, numGoroutines*incrementsPerGoroutine, progress.ProcessedGames)
	assert.Equal(t, numGoroutines*incrementsPerGoroutine, progress.TotalGames)
}

func TestProgressTracker_NotificationBlocking(t *testing.T) {
	t.Parallel()

	// Create a small channel that will fill up
	notifChan := make(chan models.Notification, 1)
	tracker := NewProgressTracker(notifChan)

	// Fill the channel
	notifChan <- models.Notification{Method: "test"}

	// These operations should not block even though the channel is full
	tracker.SetStatus("running")
	tracker.SetProgress(1, 10)
	tracker.SetCurrentGame("Test", 1)

	// Verify the operations completed (didn't block)
	progress := tracker.Get()
	assert.Equal(t, "running", progress.Status)
	assert.Equal(t, 1, progress.ProcessedGames)
	assert.Equal(t, "Test", progress.CurrentGame)
}

func TestProgressTracker_JSONSerialization(t *testing.T) {
	t.Parallel()

	notifChan := make(chan models.Notification, 10)
	tracker := NewProgressTracker(notifChan)

	tracker.SetStatus("running")
	tracker.SetProgress(5, 10)
	tracker.SetCurrentGame("Test Game", 123)

	// Drain notifications and find the latest one with our data
	var lastNotification models.Notification
	var found bool

	timeout := time.After(200 * time.Millisecond)
	for {
		select {
		case notification := <-notifChan:
			lastNotification = notification
			found = true
		case <-timeout:
			goto checkResult
		default:
			if found {
				goto checkResult
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

checkResult:
	require.True(t, found, "Expected at least one notification")
	assert.Equal(t, "scraper.progress", lastNotification.Method)

	// Verify that the params can be unmarshaled back
	var progress map[string]interface{}
	err := json.Unmarshal(lastNotification.Params, &progress)
	require.NoError(t, err)

	// Check if the expected fields exist
	if status, ok := progress["Status"]; ok {
		assert.Equal(t, "running", status)
	}
	if processedGames, ok := progress["ProcessedGames"]; ok {
		assert.Equal(t, float64(5), processedGames) // Should be 5 from SetProgress call
	}
	if totalGames, ok := progress["TotalGames"]; ok {
		assert.Equal(t, float64(10), totalGames)
	}
	if currentGame, ok := progress["CurrentGame"]; ok {
		assert.Equal(t, "Test Game", currentGame)
	}
}

func TestProgressTracker_NilNotificationChannel(t *testing.T) {
	t.Parallel()

	// Create tracker with nil notification channel
	tracker := NewProgressTracker(nil)
	require.NotNil(t, tracker)

	// These operations should not panic
	tracker.SetStatus("running")
	tracker.SetProgress(5, 10)
	tracker.Complete()
	tracker.Cancel()

	// Verify state is still tracked correctly
	progress := tracker.Get()
	assert.Equal(t, "cancelled", progress.Status)
	assert.Equal(t, 10, progress.ProcessedGames)
	assert.Equal(t, 10, progress.TotalGames)
}