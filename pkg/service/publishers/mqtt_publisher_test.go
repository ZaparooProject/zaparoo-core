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

package publishers

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
)

func TestNewMQTTPublisher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		broker string
		topic  string
		filter []string
	}{
		{
			name:   "with filter",
			broker: "localhost:1883",
			topic:  "zaparoo/events",
			filter: []string{"media.launched", "media.stopped"},
		},
		{
			name:   "without filter",
			broker: "broker.example.com:8883",
			topic:  "notifications",
			filter: nil,
		},
		{
			name:   "empty filter",
			broker: "test:1883",
			topic:  "test/topic",
			filter: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			publisher := NewMQTTPublisher(tt.broker, tt.topic, tt.filter)

			assert.NotNil(t, publisher)
			assert.Equal(t, tt.broker, publisher.broker)
			assert.Equal(t, tt.topic, publisher.topic)
			assert.Equal(t, tt.filter, publisher.filter)
			assert.NotNil(t, publisher.stopCh)
		})
	}
}

func TestMatchesFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		method  string
		wantMsg string
		filter  []string
		want    bool
	}{
		{
			name:    "empty filter matches all",
			filter:  []string{},
			method:  "media.started",
			want:    true,
			wantMsg: "empty filter should match all notifications",
		},
		{
			name:    "nil filter matches all",
			filter:  nil,
			method:  "tokens.added",
			want:    true,
			wantMsg: "nil filter should match all notifications",
		},
		{
			name:    "method in filter",
			filter:  []string{"media.started", "media.stopped"},
			method:  "media.started",
			want:    true,
			wantMsg: "should match when method is in filter",
		},
		{
			name:    "method not in filter",
			filter:  []string{"media.started", "media.stopped"},
			method:  "readers.added",
			want:    false,
			wantMsg: "should not match when method not in filter",
		},
		{
			name:    "single item filter match",
			filter:  []string{"tokens.added"},
			method:  "tokens.added",
			want:    true,
			wantMsg: "should match single item in filter",
		},
		{
			name:    "single item filter no match",
			filter:  []string{"tokens.added"},
			method:  "tokens.removed",
			want:    false,
			wantMsg: "should not match when not in single-item filter",
		},
		{
			name:    "case sensitive",
			filter:  []string{"media.started"},
			method:  "Media.Started",
			want:    false,
			wantMsg: "filter matching should be case-sensitive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			publisher := &MQTTPublisher{
				filter: tt.filter,
			}

			result := publisher.matchesFilter(tt.method)

			assert.Equal(t, tt.want, result, tt.wantMsg)
		})
	}
}

func TestStop(t *testing.T) {
	t.Parallel()

	publisher := NewMQTTPublisher("localhost:1883", "test", nil)

	// Stop should not panic and should close the channel
	publisher.Stop()

	// Verify stopCh is closed
	_, ok := <-publisher.stopCh
	assert.False(t, ok, "stopCh should be closed after Stop()")
}

func TestStart_Success(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)

	// Replace client creation with mock
	publisher.client = mockClient

	notifChan := make(chan models.Notification, 10)

	// Manually connect the mock (Start would do this via mqtt.NewClient)
	mockClient.connected = true

	publisher.wg.Add(1)
	go publisher.publishNotifications(notifChan)

	// Send a test notification
	testNotif := models.Notification{
		Method: "media.started",
		Params: []byte(`{"system": "NES", "name": "Super Mario Bros."}`),
	}
	notifChan <- testNotif

	// Wait for publish
	time.Sleep(50 * time.Millisecond)

	// Verify message was published (thread-safe check)
	assert.Equal(t, 1, mockClient.getPublishedCount())

	// Cleanup
	publisher.Stop()
}

func TestPublishNotifications_FilteredOut(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test/topic", []string{"tokens.added"})
	publisher.client = mockClient
	mockClient.connected = true

	notifChan := make(chan models.Notification, 10)

	publisher.wg.Add(1)
	go publisher.publishNotifications(notifChan)

	// Send notification that should be filtered out
	notifChan <- models.Notification{
		Method: "media.started",
		Params: []byte(`{"system": "NES"}`),
	}

	// Wait briefly
	time.Sleep(50 * time.Millisecond)

	// Should not have published anything
	assert.Equal(t, 0, mockClient.getPublishedCount())

	// Now send one that matches filter
	notifChan <- models.Notification{
		Method: "tokens.added",
		Params: []byte(`{"uid": "test-uid"}`),
	}

	time.Sleep(50 * time.Millisecond)

	// Should have published the matching one
	assert.Equal(t, 1, mockClient.getPublishedCount())

	publisher.Stop()
}

func TestPublishNotifications_PublishError(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	mockClient.publishError = assert.AnError
	mockClient.connected = true

	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	publisher.client = mockClient

	notifChan := make(chan models.Notification, 10)

	publisher.wg.Add(1)
	go publisher.publishNotifications(notifChan)

	// Send notification
	notifChan <- models.Notification{
		Method: "media.stopped",
		Params: []byte(`{}`),
	}

	// Wait briefly - should handle error gracefully
	time.Sleep(50 * time.Millisecond)

	// No panic means success - error was handled
	publisher.Stop()
}

func TestPublishNotifications_ChannelClosed(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	mockClient.connected = true

	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	publisher.client = mockClient

	notifChan := make(chan models.Notification, 10)

	publisher.wg.Add(1)
	go publisher.publishNotifications(notifChan)

	// Close notification channel
	close(notifChan)

	// Wait for goroutine to exit
	time.Sleep(50 * time.Millisecond)

	// Should have exited gracefully
	// No assertions needed - we're verifying no panic occurs
}

func TestStop_WithConnectedClient(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	mockClient.connected = true

	publisher := NewMQTTPublisher("localhost:1883", "test", nil)
	publisher.client = mockClient

	publisher.Stop()

	// Verify disconnect was called
	assert.Equal(t, 1, mockClient.disconnectCall)
	assert.False(t, mockClient.IsConnected())

	// Verify stopCh is closed
	_, ok := <-publisher.stopCh
	assert.False(t, ok, "stopCh should be closed after Stop()")
}
