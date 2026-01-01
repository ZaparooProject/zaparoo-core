/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package broker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
)

func TestNewBroker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification)
	broker := NewBroker(ctx, source)

	assert.NotNil(t, broker)
	assert.NotNil(t, broker.subscribers)
	assert.Equal(t, 0, broker.nextID)
}

func TestBroker_Subscribe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification)
	broker := NewBroker(ctx, source)

	// Subscribe with buffer size
	ch, id := broker.Subscribe(10)

	assert.NotNil(t, ch)
	assert.Equal(t, 0, id)
	assert.Len(t, broker.subscribers, 1)

	// Subscribe again, should get incremented ID
	ch2, id2 := broker.Subscribe(20)

	assert.NotNil(t, ch2)
	assert.Equal(t, 1, id2)
	assert.Len(t, broker.subscribers, 2)
}

func TestBroker_Unsubscribe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification)
	broker := NewBroker(ctx, source)

	ch, id := broker.Subscribe(10)

	// Unsubscribe should close channel and remove from map
	broker.Unsubscribe(id)

	assert.Empty(t, broker.subscribers)

	// Channel should be closed
	_, ok := <-ch
	assert.False(t, ok, "channel should be closed")

	// Unsubscribing again should be safe (no-op)
	broker.Unsubscribe(id)
}

func TestBroker_BroadcastToMultipleSubscribers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification, 10)
	broker := NewBroker(ctx, source)
	broker.Start()

	// Create three subscribers
	sub1, _ := broker.Subscribe(10)
	sub2, _ := broker.Subscribe(10)
	sub3, _ := broker.Subscribe(10)

	// Send a notification
	notif := models.Notification{
		Method: "test.event",
		Params: []byte(`{"data": "test"}`),
	}

	source <- notif

	// All three subscribers should receive it
	received1 := <-sub1
	received2 := <-sub2
	received3 := <-sub3

	assert.Equal(t, notif.Method, received1.Method)
	assert.Equal(t, notif.Method, received2.Method)
	assert.Equal(t, notif.Method, received3.Method)
}

func TestBroker_SlowConsumerDoesNotBlockFastConsumer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification, 100)
	broker := NewBroker(ctx, source)
	broker.Start()

	// Fast consumer with small buffer
	fastConsumer, _ := broker.Subscribe(10)

	// Slow consumer with tiny buffer (will fill up quickly)
	_, _ = broker.Subscribe(2)

	// Send many notifications quickly
	sentCount := 20
	for range sentCount {
		source <- models.Notification{
			Method: "test.event",
			Params: []byte(`{}`),
		}
	}

	// Give broker time to process
	time.Sleep(50 * time.Millisecond)

	// Fast consumer should receive many messages without blocking
	fastReceived := 0
	fastTimeout := time.After(100 * time.Millisecond)
	for {
		select {
		case <-fastConsumer:
			fastReceived++
		case <-fastTimeout:
			goto checkResults
		}
	}

checkResults:
	// Fast consumer should have received many notifications
	// (might not be all due to test timing, but should be significant)
	assert.Greater(t, fastReceived, 5, "fast consumer should have received several notifications")

	// Slow consumer may have received some or dropped some, but system stayed responsive
	// This test verifies no deadlock occurred (by completing successfully)
}

func TestBroker_NonBlockingSendDropsWhenFull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification, 100)
	broker := NewBroker(ctx, source)
	broker.Start()

	// Create subscriber with very small buffer
	subscriber, _ := broker.Subscribe(2)

	// Don't read from subscriber channel - let it fill up

	// Send more notifications than buffer can hold
	for range 10 {
		source <- models.Notification{
			Method: "test.event",
			Params: []byte(`{}`),
		}
	}

	// Give broker time to attempt sends
	time.Sleep(100 * time.Millisecond)

	// Verify test didn't deadlock (by completing)
	// Now drain the channel - should only have buffer size worth
	received := 0
	timeout := time.After(50 * time.Millisecond)
drainLoop:
	for {
		select {
		case <-subscriber:
			received++
		case <-timeout:
			break drainLoop
		}
	}

	// Should have received approximately buffer size (might be buffer+1 due to timing)
	assert.LessOrEqual(t, received, 3, "should have dropped excess notifications")
}

func TestBroker_ContextCancellationStopsBroker(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	source := make(chan models.Notification, 10)
	broker := NewBroker(ctx, source)
	broker.Start()

	subscriber, _ := broker.Subscribe(10)

	// Cancel context
	cancel()

	// Give broker time to shut down
	time.Sleep(50 * time.Millisecond)

	// Subscriber channel should be closed
	_, ok := <-subscriber
	assert.False(t, ok, "subscriber channel should be closed on context cancellation")
}

func TestBroker_SourceChannelClosureStopsBroker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification, 10)
	broker := NewBroker(ctx, source)
	broker.Start()

	subscriber, _ := broker.Subscribe(10)

	// Close source channel
	close(source)

	// Give broker time to shut down
	time.Sleep(50 * time.Millisecond)

	// Subscriber channel should be closed
	_, ok := <-subscriber
	assert.False(t, ok, "subscriber channel should be closed when source closes")
}

func TestBroker_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification, 100)
	broker := NewBroker(ctx, source)
	broker.Start()

	var wg sync.WaitGroup
	subscriberCount := 10

	// Concurrently subscribe and unsubscribe
	for range subscriberCount {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Subscribe
			_, id := broker.Subscribe(5)

			// Do some work
			time.Sleep(10 * time.Millisecond)

			// Unsubscribe
			broker.Unsubscribe(id)
		}()
	}

	// Also send some notifications concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			source <- models.Notification{
				Method: "test.event",
				Params: []byte(`{}`),
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Verify no panic and test completes successfully
	// This tests thread safety of concurrent operations
}

func TestBroker_SubscriberReceivesInOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification, 100)
	broker := NewBroker(ctx, source)
	broker.Start()

	subscriber, _ := broker.Subscribe(100)

	// Send notifications in order with different methods to track order
	methods := []string{"event.one", "event.two", "event.three", "event.four", "event.five"}
	for _, method := range methods {
		source <- models.Notification{
			Method: method,
			Params: []byte(`{}`),
		}
	}

	// Receive and verify order
	for i, expectedMethod := range methods {
		notif := <-subscriber
		assert.Equal(t, expectedMethod, notif.Method, "notification %d should maintain order", i)
	}
}

func TestBroker_MultipleNotificationTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := make(chan models.Notification, 10)
	broker := NewBroker(ctx, source)
	broker.Start()

	subscriber, _ := broker.Subscribe(10)

	// Send different notification types
	notifications := []models.Notification{
		{Method: models.NotificationStarted, Params: []byte(`{"test": "started"}`)},
		{Method: models.NotificationStopped, Params: []byte(`{"test": "stopped"}`)},
		{Method: "custom.event", Params: []byte(`{"test": "custom"}`)},
	}

	for _, notif := range notifications {
		source <- notif
	}

	// All should be received
	for i := range notifications {
		received := <-subscriber
		assert.Equal(t, notifications[i].Method, received.Method)
	}
}
