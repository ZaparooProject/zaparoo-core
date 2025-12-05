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

package notifications

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSendNotification_NonBlocking is a regression test for the deadlock fix.
// Previously, sendNotification used blocking sends which could freeze callers
// when the channel buffer was full. The fix uses select/default for non-blocking sends.
func TestSendNotification_NonBlocking(t *testing.T) {
	t.Parallel()

	// Create a channel with no buffer - any send would block without non-blocking logic
	ns := make(chan models.Notification)

	// This should return immediately, not block
	done := make(chan struct{})
	go func() {
		TokensAdded(ns, models.TokenResponse{UID: "test"})
		close(done)
	}()

	select {
	case <-done:
		// Success - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sendNotification blocked on full channel - non-blocking fix has regressed")
	}
}

// TestSendNotification_SuccessfulSend verifies notifications are sent when channel has capacity.
func TestSendNotification_SuccessfulSend(t *testing.T) {
	t.Parallel()

	ns := make(chan models.Notification, 1)

	TokensAdded(ns, models.TokenResponse{
		UID:  "ABC123",
		Text: "test-text",
	})

	select {
	case notification := <-ns:
		assert.Equal(t, models.NotificationTokensAdded, notification.Method)
		assert.Contains(t, string(notification.Params), "ABC123")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected notification was not sent")
	}
}

// TestSendNotification_NilPayload verifies notifications work without payload.
func TestSendNotification_NilPayload(t *testing.T) {
	t.Parallel()

	ns := make(chan models.Notification, 1)

	TokensRemoved(ns)

	select {
	case notification := <-ns:
		assert.Equal(t, models.NotificationTokensRemoved, notification.Method)
		assert.Nil(t, notification.Params)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected notification was not sent")
	}
}

// TestSendNotification_DropsWhenFull verifies notifications are dropped (not blocked)
// when the channel is full.
func TestSendNotification_DropsWhenFull(t *testing.T) {
	t.Parallel()

	// Buffer of 1, pre-fill it
	ns := make(chan models.Notification, 1)
	ns <- models.Notification{Method: "prefill"}

	// These should be dropped, not block
	done := make(chan struct{})
	go func() {
		for range 10 {
			TokensAdded(ns, models.TokenResponse{UID: "dropped"})
		}
		close(done)
	}()

	select {
	case <-done:
		// Success - all sends completed without blocking
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sendNotification blocked when channel was full")
	}

	// Verify only the prefill message is in the channel
	msg := <-ns
	assert.Equal(t, "prefill", msg.Method)
}

// TestMediaStarted_Payload verifies MediaStarted marshals payload correctly.
func TestMediaStarted_Payload(t *testing.T) {
	t.Parallel()

	ns := make(chan models.Notification, 1)

	MediaStarted(ns, models.MediaStartedParams{
		SystemID:   "nes",
		SystemName: "Nintendo Entertainment System",
		MediaName:  "Super Mario Bros",
		MediaPath:  "/roms/nes/smb.nes",
	})

	notification := <-ns
	assert.Equal(t, models.NotificationStarted, notification.Method)
	assert.Contains(t, string(notification.Params), "nes")
	assert.Contains(t, string(notification.Params), "Super Mario Bros")
}

// TestMediaStopped verifies MediaStopped sends correctly.
func TestMediaStopped(t *testing.T) {
	t.Parallel()

	ns := make(chan models.Notification, 1)

	MediaStopped(ns)

	notification := <-ns
	assert.Equal(t, models.NotificationStopped, notification.Method)
}

// TestReadersAdded_Payload verifies ReadersAdded marshals payload correctly.
func TestReadersAdded_Payload(t *testing.T) {
	t.Parallel()

	ns := make(chan models.Notification, 1)

	ReadersAdded(ns, models.ReaderResponse{
		Connected: true,
		Driver:    "pn532",
		Path:      "/dev/ttyUSB0",
	})

	notification := <-ns
	assert.Equal(t, models.NotificationReadersConnected, notification.Method)
	assert.Contains(t, string(notification.Params), "pn532")
}

// TestReadersRemoved_Payload verifies ReadersRemoved marshals payload correctly.
func TestReadersRemoved_Payload(t *testing.T) {
	t.Parallel()

	ns := make(chan models.Notification, 1)

	ReadersRemoved(ns, models.ReaderResponse{
		Connected: false,
		Driver:    "pn532",
		Path:      "/dev/ttyUSB0",
	})

	notification := <-ns
	assert.Equal(t, models.NotificationReadersDisconnected, notification.Method)
}

// TestCriticalNotifications verifies the critical notification list is correct.
func TestCriticalNotifications(t *testing.T) {
	t.Parallel()

	// These should be marked as critical
	criticalMethods := []string{
		models.NotificationTokensAdded,
		models.NotificationTokensRemoved,
		models.NotificationReadersConnected,
		models.NotificationReadersDisconnected,
		models.NotificationStarted,
		models.NotificationStopped,
	}

	for _, method := range criticalMethods {
		assert.True(t, criticalNotifications[method], "%s should be marked as critical", method)
	}

	// MediaIndexing should NOT be critical (high-volume)
	assert.False(t, criticalNotifications[models.NotificationMediaIndexing],
		"MediaIndexing should not be critical")
}

// TestMediaIndexing_Payload verifies MediaIndexing marshals payload correctly.
func TestMediaIndexing_Payload(t *testing.T) {
	t.Parallel()

	ns := make(chan models.Notification, 1)

	totalSteps := 5
	currentStep := 2
	totalMedia := 100
	MediaIndexing(ns, models.IndexingStatusResponse{
		Indexing:    true,
		TotalSteps:  &totalSteps,
		CurrentStep: &currentStep,
		TotalMedia:  &totalMedia,
	})

	notification := <-ns
	assert.Equal(t, models.NotificationMediaIndexing, notification.Method)
	require.NotNil(t, notification.Params)
	assert.Contains(t, string(notification.Params), `"indexing":true`)
}

// TestPlaytimeLimitReached_Payload verifies PlaytimeLimitReached marshals correctly.
func TestPlaytimeLimitReached_Payload(t *testing.T) {
	t.Parallel()

	ns := make(chan models.Notification, 1)

	PlaytimeLimitReached(ns, models.PlaytimeLimitReachedParams{
		Reason: "daily_limit",
	})

	notification := <-ns
	assert.Equal(t, models.NotificationPlaytimeLimitReached, notification.Method)
	assert.Contains(t, string(notification.Params), "daily_limit")
}

// TestPlaytimeLimitWarning_Payload verifies PlaytimeLimitWarning marshals correctly.
func TestPlaytimeLimitWarning_Payload(t *testing.T) {
	t.Parallel()

	ns := make(chan models.Notification, 1)

	PlaytimeLimitWarning(ns, models.PlaytimeLimitWarningParams{
		Interval:  "daily",
		Remaining: "5 minutes",
	})

	notification := <-ns
	assert.Equal(t, models.NotificationPlaytimeLimitWarning, notification.Method)
	assert.Contains(t, string(notification.Params), "5 minutes")
}
