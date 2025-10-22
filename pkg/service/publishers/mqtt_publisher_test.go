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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test", nil)
	publisher.client = mockClient
	mockClient.connected = true

	// Stop should not panic and should disconnect
	publisher.Stop()

	// Verify disconnect was called
	assert.Equal(t, 1, mockClient.disconnectCall)
	assert.False(t, mockClient.IsConnected())
}

func TestStopMultipleTimes(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test", nil)
	publisher.client = mockClient
	mockClient.connected = true

	// Stop should be idempotent and safe to call multiple times (no panic)
	publisher.Stop()
	publisher.Stop()
	publisher.Stop()

	// Disconnect is only called on the first Stop() when connected=true
	// Subsequent calls see connected=false and skip disconnect
	assert.Equal(t, 1, mockClient.disconnectCall)
}

func TestPublish_Success(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	publisher.client = mockClient
	mockClient.connected = true

	// Send a test notification
	testNotif := models.Notification{
		Method: "media.started",
		Params: []byte(`{"system": "NES", "name": "Super Mario Bros."}`),
	}
	err := publisher.Publish(testNotif)

	// Should succeed without error
	require.NoError(t, err)

	// Verify message was published (thread-safe check)
	assert.Equal(t, 1, mockClient.getPublishedCount())
}

func TestPublish_FilteredOut(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test/topic", []string{"tokens.added"})
	publisher.client = mockClient
	mockClient.connected = true

	// Send notification that should be filtered out
	err := publisher.Publish(models.Notification{
		Method: "media.started",
		Params: []byte(`{"system": "NES"}`),
	})

	// Should succeed (filtering is not an error)
	require.NoError(t, err)

	// Should not have published anything
	assert.Equal(t, 0, mockClient.getPublishedCount())

	// Now send one that matches filter
	err = publisher.Publish(models.Notification{
		Method: "tokens.added",
		Params: []byte(`{"uid": "test-uid"}`),
	})

	// Should succeed
	require.NoError(t, err)

	// Should have published the matching one
	assert.Equal(t, 1, mockClient.getPublishedCount())
}

func TestPublish_PublishError(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	mockClient.publishError = assert.AnError
	mockClient.connected = true

	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	publisher.client = mockClient

	// Send notification - should return error
	err := publisher.Publish(models.Notification{
		Method: "media.stopped",
		Params: []byte(`{}`),
	})

	// Should get the error back (either timeout or publish error)
	assert.Error(t, err)
}

func TestPublish_DisconnectedClient(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	publisher.client = mockClient
	mockClient.connected = false // Client not connected

	// Send notification - should return error
	err := publisher.Publish(models.Notification{
		Method: "test.notification",
		Params: []byte(`{"valid": "json"}`),
	})

	// Should get error about not being connected
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")

	// Should not have attempted to publish
	assert.Equal(t, 0, mockClient.getPublishedCount())
}

func TestPublish_Concurrent(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test", nil)
	publisher.client = mockClient
	mockClient.connected = true

	// Publish multiple notifications concurrently
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			err := publisher.Publish(models.Notification{
				Method: "test.notification",
				Params: []byte(`{"id": ` + string(rune(id+'0')) + `}`),
			})
			// Use assert in goroutine (require.FailNow doesn't work in goroutines)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range numGoroutines {
		<-done
	}

	// Should have published all notifications
	assert.Equal(t, numGoroutines, mockClient.getPublishedCount())
}
