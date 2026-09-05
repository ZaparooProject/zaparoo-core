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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/apigatt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/discovery"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

// bleTransportDeps is everything the BLE transport shares with the other
// transports.
type bleTransportDeps struct {
	core        *requestDeps
	methodMap   *MethodMap
	encGateway  *apimiddleware.EncryptionGateway
	lastSeen    *apimiddleware.LastSeenTracker
	tracker     RequestTracker
	pairing     *PairingManager
	notifBroker *broker.Broker
	clock       clockwork.Clock
}

// bleTransport serves the JSON-RPC API over the Zaparoo GATT service. Each
// connected central gets a bleSession; the transport routes GATT events to
// sessions and fans notifications out to them.
type bleTransport struct {
	bleTransportDeps
	// ctx ends with the service or with stop, whichever comes first, and
	// bounds every goroutine the transport starts.
	ctx    context.Context
	cancel context.CancelFunc
	// pairLimiter bounds pairing requests across every connection. The
	// per-session limiter resets whenever a peer reconnects under a fresh
	// private address, so it cannot be the only one.
	pairLimiter *rate.Limiter
	sessions    map[string]*bleSession
	peripheral  bluez.Peripheral
	serveCancel context.CancelFunc
	// unregister detaches serve from the bluetooth manager on stop.
	unregister func()
	wg         sync.WaitGroup
	mu         syncutil.Mutex
	stopped    bool
}

func newBLETransport(d *bleTransportDeps) *bleTransport {
	t := &bleTransport{
		bleTransportDeps: *d,
		pairLimiter:      rate.NewLimiter(bleTransportPairingRate, bleTransportPairingBurst),
		sessions:         make(map[string]*bleSession),
	}
	t.ctx, t.cancel = context.WithCancel(d.core.st.GetContext())
	if t.clock == nil {
		t.clock = clockwork.NewRealClock()
	}
	if d.notifBroker != nil {
		notifs, subID := d.notifBroker.Subscribe(100)
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.broadcast(notifs)
			d.notifBroker.Unsubscribe(subID)
		}()
	}
	return t
}

// application is the GATT layout the app expects.
func (*bleTransport) application() bluez.Application {
	return bluez.Application{Services: []bluez.Service{{
		UUID:    apigatt.ServiceUUID,
		Primary: true,
		Characteristics: []bluez.Characteristic{
			{UUID: apigatt.RXCharUUID, Flags: []string{bluez.FlagWrite, bluez.FlagWriteWithoutResponse}},
			{UUID: apigatt.TXCharUUID, Flags: []string{bluez.FlagNotify}},
			{UUID: apigatt.InfoCharUUID, Flags: []string{bluez.FlagRead}},
		},
	}}}
}

// localName is what the device is called in a phone's scan list.
func (t *bleTransport) localName() string {
	if name := t.core.cfg.BLEName(); name != "" {
		return name
	}
	return discovery.ResolveInstanceName(t.core.cfg)
}

// serve starts serving on a peripheral. The bluetooth manager calls it each
// time an adapter becomes ready; it returns at once and serves in the
// background until the peripheral goes away or the transport stops.
func (t *bleTransport) serve(p bluez.Peripheral) {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	if t.serveCancel != nil {
		t.serveCancel()
	}
	ctx, cancel := context.WithCancel(t.ctx)
	t.serveCancel = cancel
	t.peripheral = p
	// Counted under the lock so stop, which sets stopped under the same
	// lock before waiting, can never miss this goroutine.
	t.wg.Add(1)
	t.mu.Unlock()

	// The name is read once here: renaming the device takes effect the
	// next time advertising starts.
	adv := bluez.Advertisement{LocalName: t.localName(), ServiceUUIDs: []string{apigatt.ServiceUUID}}
	go func() {
		defer t.wg.Done()
		err := p.Serve(ctx, t.application(), adv, t)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Warn().Err(err).Msg("bluetooth api transport stopped")
		default:
			log.Info().Msg("bluetooth api transport stopped")
		}
		// Only this peripheral's sessions: a replacement may already be
		// serving clients of its own.
		t.closeSessions("transport stopped", p)
	}()
}

// stop ends serving and every session, then waits for the background work.
func (t *bleTransport) stop() {
	t.mu.Lock()
	t.stopped = true
	unregister := t.unregister
	t.mu.Unlock()
	if unregister != nil {
		unregister()
	}
	t.cancel()
	t.closeSessions("transport stopped", nil)
	t.wg.Wait()
}

// closeSessions ends every session served by p, or every session when p is
// nil.
func (t *bleTransport) closeSessions(reason string, p bluez.Peripheral) {
	t.mu.Lock()
	sessions := make([]*bleSession, 0, len(t.sessions))
	for _, s := range t.sessions {
		if p == nil || s.p == p {
			sessions = append(sessions, s)
		}
	}
	t.mu.Unlock()
	for _, s := range sessions {
		s.shutdown(reason)
	}
}

// sessionFor returns the session for a peer, creating it on first contact.
func (t *bleTransport) sessionFor(peer bluez.Peer) *bleSession {
	if peer.Path == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return nil
	}
	if s, ok := t.sessions[peer.Path]; ok {
		return s
	}
	if t.peripheral == nil {
		return nil
	}
	s := newBLESession(t, peer, t.peripheral)
	t.sessions[peer.Path] = s
	log.Info().Str("peer", peer.Address).Msg("bluetooth client connected")
	return s
}

// forget removes a session from the table once it has closed.
func (t *bleTransport) forget(s *bleSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions[s.peer.Path] == s {
		delete(t.sessions, s.peer.Path)
	}
}

// OnWrite implements bluez.PeripheralHandler: every write to RX is a chunk.
func (t *bleTransport) OnWrite(peer bluez.Peer, charUUID string, value []byte, mtu int) {
	if !strings.EqualFold(charUUID, apigatt.RXCharUUID) {
		return
	}
	if s := t.sessionFor(peer); s != nil {
		s.handleChunk(value, mtu)
	}
}

// OnRead implements bluez.PeripheralHandler: only Info is readable.
func (t *bleTransport) OnRead(_ bluez.Peer, charUUID string) ([]byte, error) {
	if !strings.EqualFold(charUUID, apigatt.InfoCharUUID) {
		return nil, bluez.ErrNotFound
	}
	data, err := json.Marshal(apigatt.NewInfo(t.core.cfg.DeviceID()))
	if err != nil {
		return nil, fmt.Errorf("marshal info: %w", err)
	}
	return data, nil
}

// OnSubscribe implements bluez.PeripheralHandler. BlueZ does not say which
// peer subscribed, so there is nothing to act on: sessions start on the
// first write and end when the peer disconnects.
func (*bleTransport) OnSubscribe(_ bluez.Peer, charUUID string, subscribed bool) {
	log.Trace().Str("characteristic", charUUID).Bool("subscribed", subscribed).Msg("ble: subscription changed")
}

// OnDisconnect implements bluez.PeripheralHandler.
func (t *bleTransport) OnDisconnect(peer bluez.Peer) {
	t.mu.Lock()
	s := t.sessions[peer.Path]
	t.mu.Unlock()
	if s != nil {
		s.shutdownWith("client disconnected", false)
	}
}

// broadcast fans notifications out to every authenticated session. Each
// session decides what it can afford to send.
func (t *bleTransport) broadcast(notifs <-chan models.Notification) {
	for {
		select {
		case <-t.ctx.Done():
			return
		case notif := <-notifs:
			// Most devices never have a BLE client, so look before
			// spending a marshal on every notification.
			t.mu.Lock()
			sessions := make([]*bleSession, 0, len(t.sessions))
			for _, s := range t.sessions {
				sessions = append(sessions, s)
			}
			t.mu.Unlock()
			if len(sessions) == 0 {
				continue
			}
			data, err := json.Marshal(models.NotificationObject{
				JSONRPC: "2.0",
				Method:  notif.Method,
				Params:  notif.Params,
			})
			if err != nil {
				log.Error().Err(err).Msg("marshalling notification for bluetooth")
				continue
			}
			for _, s := range sessions {
				s.sendNotification(notif.Method, data)
			}
		}
	}
}

// disconnectPeer asks the peripheral to drop a peer, best effort.
func disconnectPeer(p bluez.Peripheral, peer bluez.Peer) {
	ctx, cancel := context.WithTimeout(context.Background(), bluez.DefaultCallTimeout)
	defer cancel()
	if err := p.Disconnect(ctx, peer); err != nil && !errors.Is(err, bluez.ErrNotFound) {
		log.Debug().Err(err).Str("peer", peer.Address).Msg("bluetooth disconnect failed")
	}
}
