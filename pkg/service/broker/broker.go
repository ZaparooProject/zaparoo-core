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

// Package broker provides a simple in-process notification broker for broadcasting
// messages to multiple consumers without blocking.
package broker

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// subscriberState manages a single subscription, including a drain goroutine that
// delivers coalesced notifications when the output channel fills up.
//
// For coaleseable methods: if outChan is full when a notification arrives, the
// notification is stored in the coalesced map (replacing any prior pending
// notification for the same method) and the drain goroutine is signalled. This
// means slow consumers see only the latest value for high-frequency progress
// events rather than losing the update entirely.
//
// For all other methods: broadcast falls back to a non-blocking send with a
// drop warning (existing behaviour).
type subscriberState struct {
	outChan   chan models.Notification
	coalesced map[string]models.Notification
	signal    chan struct{}
	stop      chan struct{}
	stopped   chan struct{}
	mu        syncutil.Mutex
}

func newSubscriberState(bufferSize int) *subscriberState {
	s := &subscriberState{
		outChan:   make(chan models.Notification, bufferSize),
		coalesced: make(map[string]models.Notification),
		signal:    make(chan struct{}, 1),
		stop:      make(chan struct{}),
		stopped:   make(chan struct{}),
	}
	go s.run()
	return s
}

func (s *subscriberState) run() {
	defer close(s.stopped)
	defer close(s.outChan)
	for {
		select {
		case <-s.stop:
			return
		case <-s.signal:
			s.mu.Lock()
			snapshot := s.coalesced
			s.coalesced = make(map[string]models.Notification, len(snapshot))
			s.mu.Unlock()
			for _, notif := range snapshot {
				select {
				case s.outChan <- notif:
				case <-s.stop:
					return
				}
			}
		}
	}
}

// Broker manages notification subscriptions and broadcasts messages to all subscribers.
// It uses non-blocking sends to ensure that slow consumers cannot block the system.
// For methods listed in coalesceable, a per-subscriber drain goroutine delivers the
// latest notification whenever the output channel has space, preventing stale drops.
type Broker struct {
	ctx          context.Context
	source       <-chan models.Notification
	subscribers  map[int]*subscriberState
	coalesceable map[string]bool
	mu           syncutil.RWMutex
	nextID       int
}

// NewBroker creates a new notification broker that reads from the source channel
// and broadcasts to all subscribers. coalesceableMethods lists notification methods
// that use last-write-wins coalescing when a subscriber's channel is full; all other
// methods are dropped with a warning (existing behaviour).
func NewBroker(ctx context.Context, source <-chan models.Notification, coalesceableMethods ...string) *Broker {
	cm := make(map[string]bool, len(coalesceableMethods))
	for _, m := range coalesceableMethods {
		cm[m] = true
	}
	return &Broker{
		ctx:          ctx,
		source:       source,
		subscribers:  make(map[int]*subscriberState),
		coalesceable: cm,
		nextID:       0,
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

// broadcast sends a notification to all subscribers.
// For coalesceable methods: tries a direct non-blocking send; if the channel is
// full, stores the latest payload in the subscriber's coalesced slot and wakes
// the drain goroutine so it can deliver when space opens.
// For all other methods: non-blocking send with drop warning on full channel.
func (b *Broker) broadcast(notif models.Notification) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	coalesce := b.coalesceable[notif.Method]

	for id, sub := range b.subscribers {
		select {
		case sub.outChan <- notif:
			// Delivered directly.
		default:
			if coalesce {
				// Channel full: store latest and wake drain goroutine.
				sub.mu.Lock()
				sub.coalesced[notif.Method] = notif
				sub.mu.Unlock()
				select {
				case sub.signal <- struct{}{}:
				default: // Signal already pending; drain goroutine will pick it up.
				}
			} else {
				log.Warn().
					Int("subscriber_id", id).
					Str("method", notif.Method).
					Msg("subscriber channel full, dropping notification")
			}
		}
	}
}

// Subscribe creates a new subscription and returns a channel that will receive
// notifications. The bufferSize determines how many notifications can be queued
// before coalescing (for coalesceable methods) or dropping (for all others) kicks in.
//
// Returns the notification channel and a subscription ID that can be used for unsubscribing.
func (b *Broker) Subscribe(bufferSize int) (notifChan <-chan models.Notification, id int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id = b.nextID
	b.nextID++

	sub := newSubscriberState(bufferSize)
	b.subscribers[id] = sub

	log.Debug().
		Int("subscriber_id", id).
		Int("buffer_size", bufferSize).
		Msg("new subscriber registered")

	notifChan = sub.outChan
	return
}

// Unsubscribe removes a subscription and waits for its drain goroutine to exit,
// after which the subscription channel is closed. It is safe to call multiple
// times with the same ID.
func (b *Broker) Unsubscribe(id int) {
	b.mu.Lock()
	sub, ok := b.subscribers[id]
	if ok {
		delete(b.subscribers, id)
	}
	b.mu.Unlock()

	if ok {
		close(sub.stop) // signal drain goroutine to exit
		<-sub.stopped   // wait; goroutine closes outChan on exit
		log.Debug().Int("subscriber_id", id).Msg("subscriber unsubscribed")
	}
}

// Stop gracefully shuts down the broker by closing all subscriber channels.
// This should be called during service shutdown.
func (b *Broker) Stop() {
	b.closeAllSubscribers()
}

// closeAllSubscribers stops all drain goroutines, closes all subscriber channels,
// and clears the subscriber map.
func (b *Broker) closeAllSubscribers() {
	b.mu.Lock()
	subs := b.subscribers
	b.subscribers = make(map[int]*subscriberState)
	b.mu.Unlock()

	for id, sub := range subs {
		close(sub.stop)
		<-sub.stopped // goroutine closes outChan on exit
		log.Debug().Int("subscriber_id", id).Msg("closed subscriber channel on shutdown")
	}
}
