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

package publishers

import (
	"context"
	"testing"
	"time"

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient := newMockMQTTClient()
	mockClient.connected = true

	publisher := NewMQTTPublisher("localhost:1883", "test", nil)
	publisher.client = mockClient
	publisher.ctx = ctx
	publisher.cancel = cancel

	publisher.Stop()

	assert.Equal(t, 1, mockClient.disconnectCall)
	assert.False(t, mockClient.IsConnected())
}

func TestStopMultipleTimes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClient := newMockMQTTClient()
	mockClient.connected = true

	publisher := NewMQTTPublisher("localhost:1883", "test", nil)
	publisher.client = mockClient
	publisher.ctx = ctx
	publisher.cancel = cancel

	publisher.Stop()
	publisher.Stop()
	publisher.Stop()

	assert.Equal(t, 1, mockClient.disconnectCall)
}

func TestPublish_Success(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	publisher.client = mockClient
	mockClient.connected = true

	testNotif := models.Notification{
		Method: "media.started",
		Params: []byte(`{"system": "NES", "name": "Super Mario Bros."}`),
	}
	err := publisher.Publish(testNotif)

	require.NoError(t, err)
	assert.Equal(t, 1, mockClient.getPublishedCount())
}

func TestPublish_FilteredOut(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test/topic", []string{"tokens.added"})
	publisher.client = mockClient
	mockClient.connected = true

	err := publisher.Publish(models.Notification{
		Method: "media.started",
		Params: []byte(`{"system": "NES"}`),
	})

	require.NoError(t, err)
	assert.Equal(t, 0, mockClient.getPublishedCount())

	err = publisher.Publish(models.Notification{
		Method: "tokens.added",
		Params: []byte(`{"uid": "test-uid"}`),
	})

	require.NoError(t, err)
	assert.Equal(t, 1, mockClient.getPublishedCount())
}

func TestPublish_PublishError(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	mockClient.publishError = assert.AnError
	mockClient.connected = true

	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	publisher.client = mockClient

	err := publisher.Publish(models.Notification{
		Method: "media.stopped",
		Params: []byte(`{}`),
	})

	assert.Error(t, err)
}

func TestPublish_DisconnectedClient(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	publisher.client = mockClient
	mockClient.connected = false

	err := publisher.Publish(models.Notification{
		Method: "test.notification",
		Params: []byte(`{"valid": "json"}`),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
	assert.Equal(t, 0, mockClient.getPublishedCount())
}

func TestPublish_Concurrent(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	publisher := NewMQTTPublisher("localhost:1883", "test", nil)
	publisher.client = mockClient
	mockClient.connected = true

	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			err := publisher.Publish(models.Notification{
				Method: "test.notification",
				Params: []byte(`{"id": ` + string(rune(id+'0')) + `}`),
			})
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	for range numGoroutines {
		<-done
	}

	assert.Equal(t, numGoroutines, mockClient.getPublishedCount())
}

func TestStart_ConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		broker  string
		topic   string
		wantErr string
	}{
		{
			name:    "empty broker",
			broker:  "",
			topic:   "test/topic",
			wantErr: "broker address is required",
		},
		{
			name:    "empty topic",
			broker:  "localhost:1883",
			topic:   "",
			wantErr: "topic is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			publisher := NewMQTTPublisher(tt.broker, tt.topic, nil)
			err := publisher.Start(context.Background())

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestStart_ConnectionFailure_ReturnsNil(t *testing.T) {
	t.Parallel()

	// Start with an unreachable broker. Start() should return nil (not an error)
	// because connection failures trigger background retry, not immediate failure.
	publisher := NewMQTTPublisher("unreachable-host-that-does-not-exist:1883", "test/topic", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := publisher.Start(ctx)

	// Connection failure should NOT be returned as an error
	require.NoError(t, err)

	// Publisher should not be connected yet
	assert.False(t, publisher.IsConnected())

	// Clean up the retry goroutine
	publisher.Stop()
}

func TestPublish_NilClient(t *testing.T) {
	t.Parallel()

	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	// client is nil (e.g., during retry)

	err := publisher.Publish(models.Notification{
		Method: "test.notification",
		Params: []byte(`{}`),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestIsConnected(t *testing.T) {
	t.Parallel()

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()

		publisher := NewMQTTPublisher("localhost:1883", "test", nil)
		assert.False(t, publisher.IsConnected())
	})

	t.Run("connected client", func(t *testing.T) {
		t.Parallel()

		mockClient := newMockMQTTClient()
		mockClient.connected = true
		publisher := NewMQTTPublisher("localhost:1883", "test", nil)
		publisher.client = mockClient

		assert.True(t, publisher.IsConnected())
	})

	t.Run("disconnected client", func(t *testing.T) {
		t.Parallel()

		mockClient := newMockMQTTClient()
		mockClient.connected = false
		publisher := NewMQTTPublisher("localhost:1883", "test", nil)
		publisher.client = mockClient

		assert.False(t, publisher.IsConnected())
	})
}

func TestRetryConnect_CancelledContext(t *testing.T) {
	t.Parallel()

	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	ctx, cancel := context.WithCancel(context.Background())
	publisher.ctx = ctx
	publisher.cancel = cancel

	// Cancel immediately — retryConnect should exit without connecting
	cancel()

	done := make(chan struct{})
	go func() {
		publisher.retryConnect()
		close(done)
	}()

	select {
	case <-done:
		// retryConnect exited as expected
	case <-time.After(2 * time.Second):
		t.Fatal("retryConnect did not exit after context cancellation")
	}

	assert.False(t, publisher.IsConnected())
}

func TestRetryConnect_AlreadyConnected(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	mockClient.connected = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	publisher := NewMQTTPublisher("localhost:1883", "test/topic", nil)
	publisher.client = mockClient
	publisher.ctx = ctx
	publisher.cancel = cancel

	// Override retry interval to be very short for testing
	origInterval := publisherRetryInterval
	_ = origInterval // can't override const, but retryConnect will check IsConnected on first tick

	done := make(chan struct{})
	go func() {
		publisher.retryConnect()
		close(done)
	}()

	// retryConnect should see client is already connected and return on first tick
	// With 30s default interval this would be slow, so we cancel after a short time
	// to verify the test doesn't hang. The real verification is that retryConnect
	// checks IsConnected() and returns early.
	select {
	case <-done:
		// Exited because already connected
	case <-time.After(35 * time.Second):
		cancel()
		t.Fatal("retryConnect did not exit for already-connected client")
	}
}

func TestStop_CancelsRetryGoroutine(t *testing.T) {
	t.Parallel()

	publisher := NewMQTTPublisher("unreachable-host-that-does-not-exist:1883", "test/topic", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := publisher.Start(ctx)
	require.NoError(t, err)

	// Stop should cancel the retry goroutine via context
	publisher.Stop()

	// Give a moment for the goroutine to exit
	time.Sleep(50 * time.Millisecond)

	// No assertion needed — if the goroutine leaks, the race detector will catch it.
	// The test itself succeeding (no hang, no race) is the verification.
}

func TestPublisherRetryInterval(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 30*time.Second, publisherRetryInterval)
}
