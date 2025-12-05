/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
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

// Package broker provides a simple in-process notification broker for broadcasting
// messages to multiple consumers without blocking.
package broker

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// Broker manages notification subscriptions and broadcasts messages to all subscribers.
// It uses non-blocking sends to ensure that slow consumers cannot block the system.
type Broker struct {
	ctx         context.Context
	source      <-chan models.Notification
	subscribers map[int]chan models.Notification
	mu          syncutil.RWMutex
	nextID      int
}

// NewBroker creates a new notification broker that reads from the source channel
// and broadcasts to all subscribers.
func NewBroker(ctx context.Context, source <-chan models.Notification) *Broker {
	return &Broker{
		ctx:         ctx,
		source:      source,
		subscribers: make(map[int]chan models.Notification),
		nextID:      0,
	}
}

// Start begins the broker's main broadcast loop in a goroutine.
// It reads notifications from the source channel and sends them to all subscribers
// using non-blocking sends. When the source channel closes or context is cancelled,
// it closes all subscriber channels and exits.
func (b *Broker) Start() {
	go func() {
		for {
			select {
			case notif, ok := <-b.source:
				if !ok {
					// Source channel closed, shut down gracefully
					log.Debug().Msg("broker: source channel closed")
					b.closeAllSubscribers()
					return
				}
				b.broadcast(notif)
			case <-b.ctx.Done():
				log.Debug().Msg("broker: context cancelled, shutting down")
				b.closeAllSubscribers()
				return
			}
		}
	}()
}

// broadcast sends a notification to all subscribers using non-blocking sends.
// If a subscriber's channel is full, the notification is dropped for that subscriber
// and a warning is logged.
func (b *Broker) broadcast(notif models.Notification) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for id, ch := range b.subscribers {
		select {
		case ch <- notif:
			// Successfully sent to this subscriber
		default:
			// Subscriber's channel is full, drop the notification
			log.Warn().
				Int("subscriber_id", id).
				Str("method", notif.Method).
				Msg("subscriber channel full, dropping notification")
		}
	}
}

// Subscribe creates a new subscription and returns a channel that will receive
// notifications. The bufferSize determines how many notifications can be queued
// before sends start blocking (and eventually dropping with warnings).
//
// Returns the notification channel and a subscription ID that can be used for unsubscribing.
func (b *Broker) Subscribe(bufferSize int) (notifChan <-chan models.Notification, id int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id = b.nextID
	b.nextID++

	ch := make(chan models.Notification, bufferSize)
	b.subscribers[id] = ch

	log.Debug().
		Int("subscriber_id", id).
		Int("buffer_size", bufferSize).
		Msg("new subscriber registered")

	notifChan = ch
	return
}

// Unsubscribe removes a subscription and closes its channel.
// It's safe to call this multiple times with the same ID.
func (b *Broker) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.subscribers[id]; ok {
		delete(b.subscribers, id)
		close(ch)
		log.Debug().Int("subscriber_id", id).Msg("subscriber unsubscribed")
	}
}

// Stop gracefully shuts down the broker by closing all subscriber channels.
// This should be called during service shutdown.
func (b *Broker) Stop() {
	b.closeAllSubscribers()
}

// closeAllSubscribers closes all subscriber channels and clears the subscriber map.
// This is called when the broker shuts down.
func (b *Broker) closeAllSubscribers() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for id, ch := range b.subscribers {
		close(ch)
		log.Debug().Int("subscriber_id", id).Msg("closed subscriber channel on shutdown")
	}
	b.subscribers = make(map[int]chan models.Notification)
}
