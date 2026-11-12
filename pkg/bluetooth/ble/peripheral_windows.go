//go:build windows

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
package ble

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
)

const (
	// advertiseStartTimeout is how long Serve waits for Windows to report
	// that advertising started.
	advertiseStartTimeout = 10 * time.Second
	advertisePoll         = 100 * time.Millisecond
	// advertiseStoppedSeconds is how many seconds in a row advertising has
	// to be reported stopped before the adapter is given up.
	advertiseStoppedSeconds = 5

	// writeFetchTimeout bounds fetching one write from Windows.
	writeFetchTimeout = 10 * time.Second
	// notifyResultTimeout is how long a queued notification is given to go
	// out before its result stops being waited for.
	notifyResultTimeout = 30 * time.Second

	// attErrorUnlikely is the ATT error for a request that could not be
	// served for a reason the client can do nothing about.
	attErrorUnlikely = 0x0E
)

var errAlreadyServing = errors.New("bluetooth: peripheral is already serving")

// peripheral is the Windows Peripheral, built on GattServiceProvider.
type peripheral struct {
	a       *adapter
	handler PeripheralHandler
	chars   map[string]localCharacteristic
	// sessions holds the GATT session of every peer seen, keyed by device
	// identifier, so its end can be reported.
	sessions map[string]*peerSession
	// subs caches each peer's subscription to a characteristic, keyed by
	// characteristic and device. Looking one up takes several calls into
	// the Bluetooth service, far too slow to do per notification.
	subs    map[string]subscribedClient
	mu      syncutil.Mutex
	serving bool
}

// peerSession is one connected central.
type peerSession struct {
	session serverSession
	closed  eventHandle
	peer    Peer
}

func newPeripheral(a *adapter) *peripheral {
	return &peripheral{
		a:        a,
		chars:    make(map[string]localCharacteristic),
		sessions: make(map[string]*peerSession),
		subs:     make(map[string]subscribedClient),
	}
}

func subKey(charUUID, deviceID string) string {
	return charUUID + "|" + deviceID
}

// dropSubscriptions forgets cached subscriptions: those to one
// characteristic, or all of them when charUUID is empty.
func (p *peripheral) dropSubscriptions(charUUID string) {
	p.mu.Lock()
	var dropped []subscribedClient
	for key, client := range p.subs {
		if charUUID == "" || strings.HasPrefix(key, charUUID+"|") {
			dropped = append(dropped, client)
			delete(p.subs, key)
		}
	}
	p.mu.Unlock()
	for _, client := range dropped {
		client.release()
	}
}

// subscription returns a peer's subscription to a characteristic, from the
// cache when it is there. The cache owns the returned object.
func (p *peripheral) subscription(c localCharacteristic, charUUID, deviceID string) (subscribedClient, error) {
	key := subKey(charUUID, deviceID)
	p.mu.Lock()
	client := p.subs[key]
	p.mu.Unlock()
	if client != nil {
		return client, nil
	}
	client, err := findSubscribedClient(c, deviceID)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if existing := p.subs[key]; existing != nil {
		p.mu.Unlock()
		client.release()
		return existing, nil
	}
	p.subs[key] = client
	p.mu.Unlock()
	return client, nil
}

// findSubscribedClient finds the subscription a device holds on a
// characteristic. The caller releases it.
func findSubscribedClient(c localCharacteristic, deviceID string) (subscribedClient, error) {
	clients, err := c.subscribedClients()
	if err != nil {
		return nil, fmt.Errorf("subscribed clients: %w", err)
	}
	var found subscribedClient
	for _, client := range clients {
		if found == nil {
			if id, idErr := client.deviceID(); idErr == nil && id == deviceID {
				found = client
				continue
			}
		}
		client.release()
	}
	if found == nil {
		return nil, fmt.Errorf("%w: peer is not subscribed", ErrNotFound)
	}
	return found, nil
}

type noopHandler struct{}

func (noopHandler) OnWrite(Peer, string, []byte, int)   {}
func (noopHandler) OnRead(Peer, string) ([]byte, error) { return nil, ErrNotFound }
func (noopHandler) OnSubscribe(Peer, string, bool)      {}
func (noopHandler) OnDisconnect(Peer)                   {}

func (p *peripheral) currentHandler() PeripheralHandler {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handler == nil {
		return noopHandler{}
	}
	return p.handler
}

// properties maps characteristic flags to the WinRT property bits.
func properties(flags []string) gatt.GattCharacteristicProperties {
	var out gatt.GattCharacteristicProperties
	for _, f := range flags {
		switch f {
		case FlagRead, FlagEncryptRead:
			out |= gatt.GattCharacteristicPropertiesRead
		case FlagWrite:
			out |= gatt.GattCharacteristicPropertiesWrite
		case FlagWriteWithoutResponse:
			out |= gatt.GattCharacteristicPropertiesWriteWithoutResponse
		case FlagNotify:
			out |= gatt.GattCharacteristicPropertiesNotify
		}
	}
	return out
}

// Serve publishes the application and advertises it until ctx ends or the
// radio goes away. Windows advertises the first service's UUID under the
// PC's own Bluetooth name; the name in adv cannot be applied here.
func (p *peripheral) Serve(ctx context.Context, app Application, adv Advertisement, h PeripheralHandler) error {
	if len(app.Services) != 1 {
		return fmt.Errorf("bluetooth: windows serves exactly one service, got %d", len(app.Services))
	}
	p.mu.Lock()
	if p.serving {
		p.mu.Unlock()
		return errAlreadyServing
	}
	p.serving = true
	p.handler = h
	p.mu.Unlock()
	defer p.reset()

	provider, cleanup, err := p.publish(ctx, &app.Services[0])
	if err != nil {
		if ctx.Err() == nil {
			p.a.markGone()
		}
		return err
	}
	defer cleanup()

	if err := p.advertise(ctx, provider); err != nil {
		if ctx.Err() == nil {
			p.a.markGone()
		}
		return err
	}
	defer func() {
		if stopErr := provider.stopAdvertising(); stopErr != nil {
			log.Debug().Err(stopErr).Msg("bluetooth stop advertising failed")
		}
	}()

	log.Info().
		Str("requestedName", adv.LocalName).
		Strs("services", adv.ServiceUUIDs).
		Msg("bluetooth peripheral advertising under the system bluetooth name")

	// Windows stops advertising on its own when the radio is switched off
	// or removed, and says so only through the status. The status also
	// flickers, so it has to stay stopped for a while to count.
	ticker := p.a.clock.NewTicker(time.Second)
	defer ticker.Stop()
	stoppedFor := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.a.gone:
			return nil
		case <-ticker.Chan():
			status, statusErr := provider.advertisementStatus()
			if statusErr == nil && advertising(status) {
				stoppedFor = 0
				continue
			}
			stoppedFor++
			if stoppedFor >= advertiseStoppedSeconds {
				log.Warn().Err(statusErr).Int32("status", int32(status)).Msg("bluetooth advertising stopped")
				p.a.markGone()
				return nil
			}
		}
	}
}

// SetAdvertising implements Peripheral. It does nothing on Windows: Core
// only pauses advertising while the user pairs a controller, and controller
// pairing is a Linux feature. Serve treats advertising that stops as the
// radio going away, so this must stay a no-op until that changes.
func (*peripheral) SetAdvertising(bool) {}

func advertising(status gatt.GattServiceProviderAdvertisementStatus) bool {
	return status == gatt.GattServiceProviderAdvertisementStatusStarted ||
		status == gatt.GattServiceProviderAdvertisementStatusStartedWithoutAllAdvertisementData
}

// reset forgets everything about the Serve call that just ended.
func (p *peripheral) reset() {
	p.mu.Lock()
	sessions := p.sessions
	subs := p.subs
	p.subs = make(map[string]subscribedClient)
	p.sessions = make(map[string]*peerSession)
	p.chars = make(map[string]localCharacteristic)
	p.handler = nil
	p.serving = false
	p.mu.Unlock()
	for _, ps := range sessions {
		ps.release()
	}
	for _, client := range subs {
		client.release()
	}
}

// publish creates the service and its characteristics and wires their
// events. The returned cleanup removes the handlers and releases the
// objects.
func (p *peripheral) publish(ctx context.Context, svc *Service) (provider serviceProvider, cleanup func(), err error) {
	provider, code, err := p.a.host.createServiceProvider(ctx, svc.UUID)
	if err != nil {
		return nil, nil, fmt.Errorf("create gatt service: %w", err)
	}
	if codeErr := bluetoothError("create gatt service", code); codeErr != nil {
		return nil, nil, codeErr
	}

	var undo []func()
	cleanup = func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		provider.release()
	}
	for i := range svc.Characteristics {
		remove, charErr := p.publishCharacteristic(ctx, provider, &svc.Characteristics[i])
		if charErr != nil {
			cleanup()
			return nil, nil, charErr
		}
		undo = append(undo, remove)
	}
	return provider, cleanup, nil
}

// publishCharacteristic creates one characteristic and subscribes to its
// reads, writes and subscription changes.
func (p *peripheral) publishCharacteristic(
	ctx context.Context, provider serviceProvider, ch *Characteristic,
) (remove func(), err error) {
	c, code, err := provider.createCharacteristic(ctx, ch.UUID, properties(ch.Flags))
	if err != nil {
		return nil, fmt.Errorf("create characteristic %s: %w", ch.UUID, err)
	}
	if codeErr := bluetoothError("create characteristic "+ch.UUID, code); codeErr != nil {
		return nil, codeErr
	}

	uuid := strings.ToLower(ch.UUID)
	unwatch, err := c.watch(charEvents{
		write: func(event writeEvent) { p.onWrite(uuid, event) },
		read:  func(event readEvent) { p.onRead(uuid, event) },
		subscribersChanged: func() {
			p.dropSubscriptions(uuid)
			p.currentHandler().OnSubscribe(Peer{}, uuid, true)
		},
	})
	if err != nil {
		c.release()
		return nil, fmt.Errorf("watch %s: %w", ch.UUID, err)
	}

	p.mu.Lock()
	p.chars[uuid] = c
	p.mu.Unlock()
	return func() {
		unwatch()
		c.release()
	}, nil
}

// advertise starts advertising and waits for Windows to confirm it, which
// is where a radio that cannot act as a peripheral is found out.
func (p *peripheral) advertise(ctx context.Context, provider serviceProvider) error {
	if err := provider.startAdvertising(); err != nil {
		return fmt.Errorf("start advertising: %w", err)
	}

	deadline := p.a.clock.NewTimer(advertiseStartTimeout)
	defer deadline.Stop()
	ticker := p.a.clock.NewTicker(advertisePoll)
	defer ticker.Stop()
	for {
		status, statusErr := provider.advertisementStatus()
		if statusErr != nil {
			return fmt.Errorf("advertising status: %w", statusErr)
		}
		if advertising(status) {
			return nil
		}
		// Windows reports Aborted for a moment before Started, so only a
		// status that never becomes Started is a failure.
		select {
		case <-ctx.Done():
			return fmt.Errorf("start advertising: %w", ctx.Err())
		case <-deadline.Chan():
			if status == gatt.GattServiceProviderAdvertisementStatusAborted {
				return fmt.Errorf("%w: windows aborted advertising; the radio may be off or unable to advertise",
					ErrRoleUnsupported)
			}
			return fmt.Errorf("%w: advertising did not start (status %d)", ErrUnavailable, status)
		case <-ticker.Chan():
		}
	}
}

// track remembers a peer's session the first time it is seen and reports
// the peer leaving when the session closes. It takes ownership of session
// when it keeps it and releases it otherwise.
func (p *peripheral) track(session serverSession) (Peer, error) {
	id, err := session.deviceID()
	if err != nil {
		session.release()
		return Peer{}, fmt.Errorf("identify peer: %w", err)
	}
	// The device identifier stands in for the object path BlueZ would give.
	peer := Peer{Path: id, Address: addressFromDeviceID(id)}
	p.mu.Lock()
	_, known := p.sessions[peer.Path]
	p.mu.Unlock()
	if known {
		session.release()
		return peer, nil
	}

	closed, err := session.onClosed(func() { p.forget(peer.Path) })
	if err != nil {
		session.release()
		return peer, nil //nolint:nilerr // the peer is still usable; only its departure goes unreported
	}
	ps := &peerSession{session: session, closed: closed, peer: peer}

	p.mu.Lock()
	if _, raced := p.sessions[peer.Path]; raced {
		p.mu.Unlock()
		ps.release()
		return peer, nil
	}
	p.sessions[peer.Path] = ps
	p.mu.Unlock()
	return peer, nil
}

func (ps *peerSession) release() {
	ps.closed.remove()
	ps.closed.release()
	ps.session.release()
}

// forget drops a peer whose session ended and tells the handler.
func (p *peripheral) forget(path string) {
	p.mu.Lock()
	ps := p.sessions[path]
	delete(p.sessions, path)
	p.mu.Unlock()
	if ps == nil {
		return
	}
	p.currentHandler().OnDisconnect(ps.peer)
	// Released off the callback that reported the session closing.
	go ps.release()
}

func (p *peripheral) onWrite(uuid string, event writeEvent) {
	session, err := event.session()
	if err != nil {
		log.Debug().Err(err).Msg("bluetooth write without a session")
		return
	}
	mtu := 0
	if size, mtuErr := session.maxPduSize(); mtuErr == nil {
		mtu = int(size)
	}
	peer, err := p.track(session)
	if err != nil {
		log.Debug().Err(err).Msg("bluetooth write from an unidentified peer")
		return
	}

	// A write that cannot be fetched is lost, and the sequence numbers
	// will show the gap; say why, since nothing else will.
	fail := func(stage string, failErr error) {
		log.Debug().Err(failErr).Str("stage", stage).Str("peer", peer.Address).Msg("bluetooth write lost")
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeFetchTimeout)
	defer cancel()
	request, err := event.request(ctx)
	if err != nil {
		fail("request", err)
		return
	}
	defer request.release()

	value, err := request.value()
	if err != nil {
		fail("value", err)
		return
	}
	if option, optErr := request.option(); optErr == nil && option == gatt.GattWriteOptionWriteWithResponse {
		if respondErr := request.respond(); respondErr != nil {
			fail("respond", respondErr)
		}
	}
	p.currentHandler().OnWrite(peer, uuid, value, mtu)
}

func (p *peripheral) onRead(uuid string, event readEvent) {
	peer := Peer{}
	if session, sessionErr := event.session(); sessionErr == nil {
		if tracked, trackErr := p.track(session); trackErr == nil {
			peer = tracked
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultCallTimeout)
	defer cancel()
	request, err := event.request(ctx)
	if err != nil {
		return
	}
	defer request.release()

	value, err := p.currentHandler().OnRead(peer, uuid)
	if err != nil {
		_ = request.respondWithError(attErrorUnlikely)
		return
	}
	// A client reading a long value asks for it in pieces.
	if offset, offErr := request.offset(); offErr == nil && int(offset) <= len(value) {
		value = value[offset:]
	}
	if err := request.respondWithValue(value); err != nil {
		_ = request.respondWithError(attErrorUnlikely)
	}
}

// Notify sends value to peer alone, which Windows can do, or to every
// subscriber when peer is the zero value.
func (p *peripheral) Notify(peer Peer, charUUID string, value []byte) error {
	uuid := strings.ToLower(charUUID)
	p.mu.Lock()
	c := p.chars[uuid]
	p.mu.Unlock()
	if c == nil {
		return fmt.Errorf("%w: characteristic %s", ErrNotFound, charUUID)
	}

	if peer.Path == "" {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultCallTimeout)
		defer cancel()
		if err := c.notifyAll(ctx, value); err != nil {
			return fmt.Errorf("notify %s: %w", charUUID, err)
		}
		return nil
	}

	client, err := p.subscription(c, uuid, peer.Path)
	if err != nil {
		return fmt.Errorf("notify %s: %w", charUUID, err)
	}
	wait, err := c.notifyClient(value, client)
	if err != nil {
		// The cached subscription may have gone stale.
		p.dropSubscriptions(uuid)
		return fmt.Errorf("notify %s: %w", charUUID, err)
	}
	// Windows queues the notification and keeps their order. Waiting for
	// each to go out before handing over the next held the link to a few a
	// second; the caller's own window is what bounds how many are queued.
	go func() {
		waitCtx, waitCancel := context.WithTimeout(context.Background(), notifyResultTimeout)
		defer waitCancel()
		if waitErr := wait(waitCtx); waitErr != nil {
			log.Debug().Err(waitErr).Str("characteristic", charUUID).Msg("bluetooth notification not confirmed")
		}
	}()
	return nil
}

// Disconnect cannot drop a central on Windows: the GATT server API has no
// call for it. The link ends when the peer leaves, which is still reported,
// so the caller has to tell the peer and stop serving it in the meantime.
func (*peripheral) Disconnect(_ context.Context, peer Peer) error {
	if peer.Path == "" {
		return fmt.Errorf("%w: peer has no path", ErrNotFound)
	}
	return nil
}
