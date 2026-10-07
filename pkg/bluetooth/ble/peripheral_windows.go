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
	"unsafe"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
	"github.com/saltosystems/winrt-go/windows/foundation"
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
	chars   map[string]*gatt.GattLocalCharacteristic
	// sessions holds the GATT session of every peer seen, keyed by device
	// identifier, so its end can be reported.
	sessions map[string]*peerSession
	// subs caches each peer's subscription to a characteristic, keyed by
	// characteristic and device. Looking one up takes several calls into
	// the Bluetooth service, far too slow to do per notification.
	subs    map[string]*gatt.GattSubscribedClient
	mu      syncutil.Mutex
	serving bool
}

// peerSession is one connected central.
type peerSession struct {
	session *gatt.GattSession
	handler *foundation.TypedEventHandler
	peer    Peer
	token   foundation.EventRegistrationToken
}

func newPeripheral(a *adapter) *peripheral {
	return &peripheral{
		a:        a,
		chars:    make(map[string]*gatt.GattLocalCharacteristic),
		sessions: make(map[string]*peerSession),
		subs:     make(map[string]*gatt.GattSubscribedClient),
	}
}

func subKey(charUUID, deviceID string) string {
	return charUUID + "|" + deviceID
}

// dropSubscriptions forgets cached subscriptions: those to one
// characteristic, or all of them when charUUID is empty.
func (p *peripheral) dropSubscriptions(charUUID string) {
	p.mu.Lock()
	var dropped []*gatt.GattSubscribedClient
	for key, client := range p.subs {
		if charUUID == "" || strings.HasPrefix(key, charUUID+"|") {
			dropped = append(dropped, client)
			delete(p.subs, key)
		}
	}
	p.mu.Unlock()
	for _, client := range dropped {
		client.Release()
	}
}

// subscription returns a peer's subscription to a characteristic, from the
// cache when it is there. The cache owns the returned object.
func (p *peripheral) subscription(
	c *gatt.GattLocalCharacteristic, charUUID, deviceID string,
) (*gatt.GattSubscribedClient, error) {
	key := subKey(charUUID, deviceID)
	p.mu.Lock()
	client := p.subs[key]
	p.mu.Unlock()
	if client != nil {
		return client, nil
	}
	client, err := subscribedClient(c, deviceID)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if existing := p.subs[key]; existing != nil {
		p.mu.Unlock()
		client.Release()
		return existing, nil
	}
	p.subs[key] = client
	p.mu.Unlock()
	return client, nil
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
		case FlagRead:
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
		if stopErr := provider.StopAdvertising(); stopErr != nil {
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
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	stoppedFor := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.a.gone:
			return nil
		case <-ticker.C:
			status, statusErr := provider.GetAdvertisementStatus()
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

func advertising(status gatt.GattServiceProviderAdvertisementStatus) bool {
	return status == gatt.GattServiceProviderAdvertisementStatusStarted ||
		status == gatt.GattServiceProviderAdvertisementStatusStartedWithoutAllAdvertisementData
}

// reset forgets everything about the Serve call that just ended.
func (p *peripheral) reset() {
	p.mu.Lock()
	sessions := p.sessions
	subs := p.subs
	p.subs = make(map[string]*gatt.GattSubscribedClient)
	p.sessions = make(map[string]*peerSession)
	p.chars = make(map[string]*gatt.GattLocalCharacteristic)
	p.handler = nil
	p.serving = false
	p.mu.Unlock()
	for _, ps := range sessions {
		ps.release()
	}
	for _, client := range subs {
		client.Release()
	}
}

// publish creates the service and its characteristics and wires their
// events. The returned cleanup removes the handlers and releases the
// objects.
func (p *peripheral) publish(
	ctx context.Context, svc *Service,
) (provider *gatt.GattServiceProvider, cleanup func(), err error) {
	svcGUID, err := guidFromUUID(svc.UUID)
	if err != nil {
		return nil, nil, err
	}
	op, err := gatt.GattServiceProviderCreateAsync(svcGUID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: create gatt service: %w", ErrUnavailable, err)
	}
	defer op.Release()
	if err = await(ctx, op, gatt.SignatureGattServiceProviderResult); err != nil {
		return nil, nil, fmt.Errorf("create gatt service: %w", err)
	}
	res, err := op.GetResults()
	if err != nil {
		return nil, nil, fmt.Errorf("create gatt service: %w", err)
	}
	result := (*gatt.GattServiceProviderResult)(res)
	defer result.Release()
	code, err := result.GetError()
	if err != nil {
		return nil, nil, fmt.Errorf("create gatt service: %w", err)
	}
	if codeErr := bluetoothError("create gatt service", code); codeErr != nil {
		return nil, nil, codeErr
	}
	provider, err = result.GetServiceProvider()
	if err != nil {
		return nil, nil, fmt.Errorf("gatt service provider: %w", err)
	}
	local, err := provider.GetService()
	if err != nil {
		provider.Release()
		return nil, nil, fmt.Errorf("gatt local service: %w", err)
	}
	defer local.Release()

	var undo []func()
	cleanup = func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		provider.Release()
	}
	for i := range svc.Characteristics {
		remove, charErr := p.publishCharacteristic(ctx, local, &svc.Characteristics[i])
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
	ctx context.Context, local *gatt.GattLocalService, ch *Characteristic,
) (remove func(), err error) {
	params, err := gatt.NewGattLocalCharacteristicParameters()
	if err != nil {
		return nil, fmt.Errorf("characteristic parameters: %w", err)
	}
	defer params.Release()
	if err = params.SetCharacteristicProperties(properties(ch.Flags)); err != nil {
		return nil, fmt.Errorf("characteristic properties: %w", err)
	}
	charGUID, err := guidFromUUID(ch.UUID)
	if err != nil {
		return nil, err
	}
	op, err := local.CreateCharacteristicAsync(charGUID, params)
	if err != nil {
		return nil, fmt.Errorf("create characteristic %s: %w", ch.UUID, err)
	}
	defer op.Release()
	if err = await(ctx, op, gatt.SignatureGattLocalCharacteristicResult); err != nil {
		return nil, fmt.Errorf("create characteristic %s: %w", ch.UUID, err)
	}
	res, err := op.GetResults()
	if err != nil {
		return nil, fmt.Errorf("create characteristic %s: %w", ch.UUID, err)
	}
	result := (*gatt.GattLocalCharacteristicResult)(res)
	defer result.Release()
	code, err := result.GetError()
	if err != nil {
		return nil, fmt.Errorf("create characteristic %s: %w", ch.UUID, err)
	}
	if codeErr := bluetoothError("create characteristic "+ch.UUID, code); codeErr != nil {
		return nil, codeErr
	}
	c, err := result.GetCharacteristic()
	if err != nil {
		return nil, fmt.Errorf("characteristic %s: %w", ch.UUID, err)
	}

	uuid := strings.ToLower(ch.UUID)
	// Windows raises these one at a time and waits for the handler, and
	// each write takes a round trip to fetch. Handled inline, a client
	// writing without responses outruns that and writes are lost, so each
	// one is taken off the callback: the event is held with a reference
	// and a deferral, and the chunks' own sequence numbers restore order.
	writeHandler := typedHandler(gatt.SignatureGattLocalCharacteristic, gatt.SignatureGattWriteRequestedEventArgs,
		func(_, args unsafe.Pointer) {
			event := (*gatt.GattWriteRequestedEventArgs)(args)
			event.AddRef()
			deferral, deferErr := event.GetDeferral()
			go func() {
				defer event.Release()
				if deferErr == nil {
					defer func() {
						_ = deferral.Complete()
						deferral.Release()
					}()
				}
				p.onWrite(uuid, event)
			}()
		})
	readHandler := typedHandler(gatt.SignatureGattLocalCharacteristic, gatt.SignatureGattReadRequestedEventArgs,
		func(_, args unsafe.Pointer) { p.onRead(uuid, (*gatt.GattReadRequestedEventArgs)(args)) })
	subHandler := typedHandler(gatt.SignatureGattLocalCharacteristic, objectSignature,
		func(_, _ unsafe.Pointer) {
			p.dropSubscriptions(uuid)
			p.currentHandler().OnSubscribe(Peer{}, uuid, true)
		})

	writeToken, err := c.AddWriteRequested(writeHandler)
	if err != nil {
		c.Release()
		return nil, fmt.Errorf("watch writes to %s: %w", ch.UUID, err)
	}
	readToken, err := c.AddReadRequested(readHandler)
	if err != nil {
		c.Release()
		return nil, fmt.Errorf("watch reads of %s: %w", ch.UUID, err)
	}
	subToken, err := c.AddSubscribedClientsChanged(subHandler)
	if err != nil {
		c.Release()
		return nil, fmt.Errorf("watch subscriptions to %s: %w", ch.UUID, err)
	}

	p.mu.Lock()
	p.chars[uuid] = c
	p.mu.Unlock()
	return func() {
		_ = c.RemoveWriteRequested(writeToken)
		_ = c.RemoveReadRequested(readToken)
		_ = c.RemoveSubscribedClientsChanged(subToken)
		writeHandler.Release()
		readHandler.Release()
		subHandler.Release()
		c.Release()
	}, nil
}

// advertise starts advertising and waits for Windows to confirm it, which
// is where a radio that cannot act as a peripheral is found out.
func (*peripheral) advertise(ctx context.Context, provider *gatt.GattServiceProvider) error {
	params, err := gatt.NewGattServiceProviderAdvertisingParameters()
	if err != nil {
		return fmt.Errorf("advertising parameters: %w", err)
	}
	defer params.Release()
	if err = params.SetIsConnectable(true); err != nil {
		return fmt.Errorf("advertising parameters: %w", err)
	}
	if err = params.SetIsDiscoverable(true); err != nil {
		return fmt.Errorf("advertising parameters: %w", err)
	}
	if err = provider.StartAdvertisingWithParameters(params); err != nil {
		return fmt.Errorf("%w: start advertising: %w", ErrRoleUnsupported, err)
	}

	deadline := time.After(advertiseStartTimeout)
	ticker := time.NewTicker(advertisePoll)
	defer ticker.Stop()
	for {
		status, statusErr := provider.GetAdvertisementStatus()
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
		case <-deadline:
			if status == gatt.GattServiceProviderAdvertisementStatusAborted {
				return fmt.Errorf("%w: windows aborted advertising; the radio may be off or unable to advertise",
					ErrRoleUnsupported)
			}
			return fmt.Errorf("%w: advertising did not start (status %d)", ErrUnavailable, status)
		case <-ticker.C:
		}
	}
}

// track remembers a peer's session the first time it is seen and reports
// the peer leaving when the session closes. It takes ownership of session
// when it keeps it and releases it otherwise.
func (p *peripheral) track(session *gatt.GattSession) (Peer, error) {
	peer, err := peerFromSession(session)
	if err != nil {
		session.Release()
		return Peer{}, err
	}
	p.mu.Lock()
	_, known := p.sessions[peer.Path]
	p.mu.Unlock()
	if known {
		session.Release()
		return peer, nil
	}

	ps := &peerSession{session: session, peer: peer}
	ps.handler = typedHandler(gatt.SignatureGattSession, gatt.SignatureGattSessionStatusChangedEventArgs,
		func(_, args unsafe.Pointer) {
			status, statusErr := (*gatt.GattSessionStatusChangedEventArgs)(args).GetStatus()
			if statusErr == nil && status == gatt.GattSessionStatusClosed {
				p.forget(peer.Path)
			}
		})
	token, err := session.AddSessionStatusChanged(ps.handler)
	if err != nil {
		ps.handler.Release()
		session.Release()
		return peer, nil //nolint:nilerr // the peer is still usable; only its departure goes unreported
	}
	ps.token = token

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
	_ = ps.session.RemoveSessionStatusChanged(ps.token)
	ps.handler.Release()
	ps.session.Release()
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

func (p *peripheral) onWrite(uuid string, args *gatt.GattWriteRequestedEventArgs) {
	session, err := args.GetSession()
	if err != nil {
		log.Debug().Err(err).Msg("bluetooth write without a session")
		return
	}
	mtu := 0
	if size, mtuErr := session.GetMaxPduSize(); mtuErr == nil {
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
	op, err := args.GetRequestAsync()
	if err != nil {
		fail("request", err)
		return
	}
	defer op.Release()
	if err = await(ctx, op, gatt.SignatureGattWriteRequest); err != nil {
		fail("await request", err)
		return
	}
	res, err := op.GetResults()
	if err != nil || res == nil {
		fail("request result", err)
		return
	}
	request := (*gatt.GattWriteRequest)(res)
	defer request.Release()

	buf, err := request.GetValue()
	if err != nil {
		fail("value", err)
		return
	}
	value, err := bufferBytes(buf)
	buf.Release()
	if err != nil {
		fail("read value", err)
		return
	}
	if option, optErr := request.GetOption(); optErr == nil && option == gatt.GattWriteOptionWriteWithResponse {
		if respondErr := request.Respond(); respondErr != nil {
			fail("respond", respondErr)
		}
	}
	p.currentHandler().OnWrite(peer, uuid, value, mtu)
}

func (p *peripheral) onRead(uuid string, args *gatt.GattReadRequestedEventArgs) {
	deferral, err := args.GetDeferral()
	if err == nil {
		defer func() {
			_ = deferral.Complete()
			deferral.Release()
		}()
	}

	peer := Peer{}
	if session, sessionErr := args.GetSession(); sessionErr == nil {
		if tracked, trackErr := p.track(session); trackErr == nil {
			peer = tracked
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultCallTimeout)
	defer cancel()
	op, err := args.GetRequestAsync()
	if err != nil {
		return
	}
	defer op.Release()
	if err = await(ctx, op, gatt.SignatureGattReadRequest); err != nil {
		return
	}
	res, err := op.GetResults()
	if err != nil || res == nil {
		return
	}
	request := (*gatt.GattReadRequest)(res)
	defer request.Release()

	value, err := p.currentHandler().OnRead(peer, uuid)
	if err != nil {
		_ = request.RespondWithProtocolError(attErrorUnlikely)
		return
	}
	// A client reading a long value asks for it in pieces.
	if offset, offErr := request.GetOffset(); offErr == nil && int(offset) <= len(value) {
		value = value[offset:]
	}
	buf, err := bytesBuffer(value)
	if err != nil {
		_ = request.RespondWithProtocolError(attErrorUnlikely)
		return
	}
	defer buf.Release()
	_ = request.RespondWithValue(buf)
}

// Notify sends value to peer alone, which Windows can do, or to every
// subscriber when peer is the zero value.
func (p *peripheral) Notify(peer Peer, charUUID string, value []byte) error {
	p.mu.Lock()
	c := p.chars[strings.ToLower(charUUID)]
	p.mu.Unlock()
	if c == nil {
		return fmt.Errorf("%w: characteristic %s", ErrNotFound, charUUID)
	}
	buf, err := bytesBuffer(value)
	if err != nil {
		return err
	}
	defer buf.Release()

	ctx, cancel := context.WithTimeout(context.Background(), DefaultCallTimeout)
	defer cancel()

	if peer.Path == "" {
		op, notifyErr := c.NotifyValueAsync(buf)
		if notifyErr != nil {
			return fmt.Errorf("notify %s: %w", charUUID, notifyErr)
		}
		defer op.Release()
		signature := fmt.Sprintf("pinterface({%s};%s)",
			collectionsVectorViewGUID, gatt.SignatureGattClientNotificationResult)
		if err = await(ctx, op, signature); err != nil {
			return fmt.Errorf("notify %s: %w", charUUID, err)
		}
		return nil
	}

	uuid := strings.ToLower(charUUID)
	client, err := p.subscription(c, uuid, peer.Path)
	if err != nil {
		return fmt.Errorf("notify %s: %w", charUUID, err)
	}
	op, err := c.NotifyValueForSubscribedClientAsync(buf, client)
	if err != nil {
		// The cached subscription may have gone stale.
		p.dropSubscriptions(uuid)
		return fmt.Errorf("notify %s: %w", charUUID, err)
	}
	// Windows queues the notification and keeps their order. Waiting for
	// each to go out before handing over the next held the link to a few a
	// second; the caller's own window is what bounds how many are queued.
	go func() {
		defer op.Release()
		waitCtx, waitCancel := context.WithTimeout(context.Background(), notifyResultTimeout)
		defer waitCancel()
		if waitErr := await(waitCtx, op, gatt.SignatureGattClientNotificationResult); waitErr != nil {
			log.Debug().Err(waitErr).Str("characteristic", charUUID).Msg("bluetooth notification not confirmed")
		}
	}()
	return nil
}

// subscribedClient finds the subscription a device holds on a
// characteristic. The caller releases it.
func subscribedClient(c *gatt.GattLocalCharacteristic, deviceID string) (*gatt.GattSubscribedClient, error) {
	clients, err := c.GetSubscribedClients()
	if err != nil {
		return nil, fmt.Errorf("subscribed clients: %w", err)
	}
	defer clients.Release()
	n, err := clients.GetSize()
	if err != nil {
		return nil, fmt.Errorf("subscribed clients: %w", err)
	}
	for i := range n {
		item, itemErr := clients.GetAt(i)
		if itemErr != nil || item == nil {
			continue
		}
		client := (*gatt.GattSubscribedClient)(item)
		session, sessionErr := client.GetSession()
		if sessionErr != nil {
			client.Release()
			continue
		}
		id, idErr := sessionDeviceID(session)
		session.Release()
		if idErr == nil && id == deviceID {
			return client, nil
		}
		client.Release()
	}
	return nil, fmt.Errorf("%w: peer is not subscribed", ErrNotFound)
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
