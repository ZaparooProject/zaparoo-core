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
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
	"github.com/stretchr/testify/require"
)

// Fakes of the WinRT layer in backend_windows.go. They never reach the
// Windows Runtime, so no test here touches a radio. Every object counts its
// releases so tests can check the backend hands each one back exactly once.

const testWait = 5 * time.Second

var errFake = errors.New("fake failure")

type released struct{ n atomic.Int32 }

func (r *released) release() { r.n.Add(1) }

func (r *released) count() int { return int(r.n.Load()) }

type fakeEvent struct {
	removed  atomic.Int32
	released released
}

func (e *fakeEvent) remove() { e.removed.Add(1) }

func (e *fakeEvent) release() { e.released.release() }

type lookupResult struct {
	dev leDevice
	err error
}

type fakeHost struct {
	providerErr error
	watcherErr  error
	provider    *fakeProvider
	// watchers receives every watcher made.
	watchers chan *fakeWatcher
	// startErr is given to every watcher made.
	startErr     error
	lookups      []lookupResult
	lookupAddrs  []string
	mu           syncutil.Mutex
	providerCode bluetooth.BluetoothError
}

func newFakeHost() *fakeHost {
	return &fakeHost{provider: newFakeProvider(), watchers: make(chan *fakeWatcher, 8)}
}

func (h *fakeHost) createServiceProvider(
	_ context.Context, serviceUUID string,
) (serviceProvider, bluetooth.BluetoothError, error) {
	if h.providerErr != nil {
		return nil, 0, h.providerErr
	}
	if h.providerCode != bluetooth.BluetoothErrorSuccess {
		return nil, h.providerCode, nil
	}
	h.provider.mu.Lock()
	h.provider.serviceUUID = serviceUUID
	h.provider.mu.Unlock()
	return h.provider, 0, nil
}

func (h *fakeHost) newWatcher(onReceived func(ScanResult)) (advWatcher, error) {
	if h.watcherErr != nil {
		return nil, h.watcherErr
	}
	w := &fakeWatcher{onReceived: onReceived, startErr: h.startErr, released: make(chan struct{})}
	h.watchers <- w
	return w, nil
}

// lookupDevice answers from lookups in order, and nil, nil once they run
// out.
func (h *fakeHost) lookupDevice(_ context.Context, address string) (leDevice, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lookupAddrs = append(h.lookupAddrs, address)
	if len(h.lookups) == 0 {
		return nil, nil //nolint:nilnil // matches lookupDevice: an unknown device is not an error
	}
	next := h.lookups[0]
	h.lookups = h.lookups[1:]
	return next.dev, next.err
}

func (h *fakeHost) nextWatcher(t *testing.T) *fakeWatcher {
	t.Helper()
	select {
	case w := <-h.watchers:
		return w
	case <-time.After(testWait):
		require.FailNow(t, "no watcher was made")
		return nil
	}
}

// statusReply answers one advertisementStatus call.
type statusReply struct {
	err    error
	status gatt.GattServiceProviderAdvertisementStatus
}

type fakeProvider struct {
	startErr  error
	statusErr error
	// calls, when set, makes every advertisementStatus call wait for the
	// test to answer it on the channel it sends.
	calls       chan chan statusReply
	chars       map[string]*fakeLocalChar
	charErr     map[string]error
	charCode    map[string]bluetooth.BluetoothError
	advertising chan struct{}
	serviceUUID string
	created     []string
	released    released
	stops       atomic.Int32
	mu          syncutil.Mutex
	status      gatt.GattServiceProviderAdvertisementStatus
	startOnce   atomic.Bool
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		chars:       make(map[string]*fakeLocalChar),
		charErr:     make(map[string]error),
		charCode:    make(map[string]bluetooth.BluetoothError),
		advertising: make(chan struct{}),
		status:      gatt.GattServiceProviderAdvertisementStatusStarted,
	}
}

func (p *fakeProvider) createCharacteristic(
	_ context.Context, uuid string, props gatt.GattCharacteristicProperties,
) (localCharacteristic, bluetooth.BluetoothError, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.created = append(p.created, uuid)
	if err := p.charErr[uuid]; err != nil {
		return nil, 0, err
	}
	if code := p.charCode[uuid]; code != bluetooth.BluetoothErrorSuccess {
		return nil, code, nil
	}
	c := p.chars[uuid]
	if c == nil {
		c = &fakeLocalChar{}
		p.chars[uuid] = c
	}
	c.mu.Lock()
	c.uuid, c.props = uuid, props
	c.mu.Unlock()
	return c, 0, nil
}

func (p *fakeProvider) startAdvertising() error {
	if p.startErr != nil {
		return p.startErr
	}
	if p.startOnce.CompareAndSwap(false, true) {
		close(p.advertising)
	}
	return nil
}

func (p *fakeProvider) advertisementStatus() (gatt.GattServiceProviderAdvertisementStatus, error) {
	if p.calls != nil {
		reply := make(chan statusReply)
		p.calls <- reply
		r := <-reply
		return r.status, r.err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status, p.statusErr
}

func (p *fakeProvider) stopAdvertising() error {
	p.stops.Add(1)
	return nil
}

func (p *fakeProvider) release() { p.released.release() }

func (p *fakeProvider) char(uuid string) *fakeLocalChar {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.chars[uuid]
	if c == nil {
		c = &fakeLocalChar{}
		p.chars[uuid] = c
	}
	return c
}

type clientNotify struct {
	client subscribedClient
	value  []byte
}

type fakeLocalChar struct {
	watchErr        error
	notifyAllErr    error
	clientsErr      error
	notifyClientErr error
	waitErr         error
	// waited receives the result of every queued notification's wait.
	waited        chan error
	events        charEvents
	uuid          string
	clientIDs     []string
	broadcasts    [][]byte
	clientNotices []clientNotify
	clients       []*fakeSubscribedClient
	released      released
	unwatched     atomic.Int32
	clientsCalls  atomic.Int32
	mu            syncutil.Mutex
	props         gatt.GattCharacteristicProperties
}

func (c *fakeLocalChar) watch(events charEvents) (unwatch func(), err error) {
	if c.watchErr != nil {
		return nil, c.watchErr
	}
	c.mu.Lock()
	c.events = events
	c.mu.Unlock()
	return func() { c.unwatched.Add(1) }, nil
}

func (c *fakeLocalChar) notifyAll(_ context.Context, value []byte) error {
	if c.notifyAllErr != nil {
		return c.notifyAllErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.broadcasts = append(c.broadcasts, slices.Clone(value))
	return nil
}

func (c *fakeLocalChar) notifyClient(
	value []byte, client subscribedClient,
) (wait func(context.Context) error, err error) {
	if c.notifyClientErr != nil {
		return nil, c.notifyClientErr
	}
	c.mu.Lock()
	c.clientNotices = append(c.clientNotices, clientNotify{client: client, value: slices.Clone(value)})
	c.mu.Unlock()
	return func(context.Context) error {
		if c.waited != nil {
			c.waited <- c.waitErr
		}
		return c.waitErr
	}, nil
}

// subscribedClients hands out a fresh object per subscriber on every call,
// as Windows does.
func (c *fakeLocalChar) subscribedClients() ([]subscribedClient, error) {
	c.clientsCalls.Add(1)
	if c.clientsErr != nil {
		return nil, c.clientsErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]subscribedClient, 0, len(c.clientIDs))
	for _, id := range c.clientIDs {
		client := &fakeSubscribedClient{id: id}
		if id == "" {
			client.idErr = errFake
		}
		c.clients = append(c.clients, client)
		out = append(out, client)
	}
	return out, nil
}

func (c *fakeLocalChar) release() { c.released.release() }

func (c *fakeLocalChar) handlers() charEvents {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.events
}

func (c *fakeLocalChar) handedOut() []*fakeSubscribedClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.clients)
}

type fakeSubscribedClient struct {
	idErr    error
	id       string
	released released
}

func (s *fakeSubscribedClient) deviceID() (string, error) { return s.id, s.idErr }

func (s *fakeSubscribedClient) release() { s.released.release() }

type fakeServerSession struct {
	idErr      error
	mtuErr     error
	onClosedFn func()
	watchErr   error
	closed     *fakeEvent
	id         string
	released   released
	mu         syncutil.Mutex
	mtu        uint16
}

func (s *fakeServerSession) deviceID() (string, error) { return s.id, s.idErr }

func (s *fakeServerSession) maxPduSize() (uint16, error) { return s.mtu, s.mtuErr }

func (s *fakeServerSession) onClosed(fn func()) (eventHandle, error) {
	if s.watchErr != nil {
		return nil, s.watchErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClosedFn = fn
	s.closed = &fakeEvent{}
	return s.closed, nil
}

func (s *fakeServerSession) release() { s.released.release() }

// close reports the session closing, as Windows does on its own thread.
func (s *fakeServerSession) close() {
	s.mu.Lock()
	fn := s.onClosedFn
	s.mu.Unlock()
	fn()
}

func (s *fakeServerSession) closedEvent() *fakeEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

type fakeWriteEvent struct {
	sessErr error
	reqErr  error
	sess    *fakeServerSession
	req     *fakeWriteRequest
}

func (e *fakeWriteEvent) session() (serverSession, error) {
	if e.sessErr != nil {
		return nil, e.sessErr
	}
	return e.sess, nil
}

func (e *fakeWriteEvent) request(context.Context) (writeRequest, error) {
	if e.reqErr != nil {
		return nil, e.reqErr
	}
	return e.req, nil
}

type fakeWriteRequest struct {
	valueErr   error
	optionErr  error
	respondErr error
	val        []byte
	released   released
	responded  atomic.Int32
	opt        gatt.GattWriteOption
}

func (r *fakeWriteRequest) value() ([]byte, error) { return r.val, r.valueErr }

func (r *fakeWriteRequest) option() (gatt.GattWriteOption, error) { return r.opt, r.optionErr }

func (r *fakeWriteRequest) respond() error {
	r.responded.Add(1)
	return r.respondErr
}

func (r *fakeWriteRequest) release() { r.released.release() }

type fakeReadEvent struct {
	sessErr error
	reqErr  error
	sess    *fakeServerSession
	req     *fakeReadRequest
}

func (e *fakeReadEvent) session() (serverSession, error) {
	if e.sessErr != nil {
		return nil, e.sessErr
	}
	return e.sess, nil
}

func (e *fakeReadEvent) request(context.Context) (readRequest, error) {
	if e.reqErr != nil {
		return nil, e.reqErr
	}
	return e.req, nil
}

type fakeReadRequest struct {
	offsetErr  error
	valueErr   error
	value      []byte
	errorCodes []byte
	released   released
	off        uint32
	answered   bool
}

func (r *fakeReadRequest) offset() (uint32, error) { return r.off, r.offsetErr }

func (r *fakeReadRequest) respondWithValue(value []byte) error {
	if r.valueErr != nil {
		return r.valueErr
	}
	r.value, r.answered = slices.Clone(value), true
	return nil
}

func (r *fakeReadRequest) respondWithError(code byte) error {
	r.errorCodes = append(r.errorCodes, code)
	return nil
}

func (r *fakeReadRequest) release() { r.released.release() }

type fakeWatcher struct {
	startErr     error
	stopErr      error
	onReceived   func(ScanResult)
	released     chan struct{}
	started      atomic.Int32
	stopped      atomic.Int32
	unregistered atomic.Int32
	releases     atomic.Int32
}

func (w *fakeWatcher) start() error {
	w.started.Add(1)
	return w.startErr
}

func (w *fakeWatcher) stop() error {
	w.stopped.Add(1)
	return w.stopErr
}

func (w *fakeWatcher) unregister() { w.unregistered.Add(1) }

func (w *fakeWatcher) release() {
	if w.releases.Add(1) == 1 {
		close(w.released)
	}
}

func (w *fakeWatcher) waitReleased(t *testing.T) {
	t.Helper()
	select {
	case <-w.released:
	case <-time.After(testWait):
		require.FailNow(t, "watcher was not released")
	}
}

type fakeLEDevice struct {
	sessionErr     error
	watchErr       error
	discoverErr    error
	session        *fakeClientSession
	statusEvent    *fakeEvent
	onStatus       func(bluetooth.BluetoothConnectionStatus)
	services       []remoteService
	closed         atomic.Int32
	mu             syncutil.Mutex
	discoverStatus gatt.GattCommunicationStatus
}

func (d *fakeLEDevice) openSession(context.Context) (clientSession, error) {
	if d.sessionErr != nil {
		return nil, d.sessionErr
	}
	if d.session == nil {
		d.session = &fakeClientSession{}
	}
	return d.session, nil
}

func (d *fakeLEDevice) onConnectionStatusChanged(
	fn func(bluetooth.BluetoothConnectionStatus),
) (eventHandle, error) {
	if d.watchErr != nil {
		return nil, d.watchErr
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onStatus = fn
	d.statusEvent = &fakeEvent{}
	return d.statusEvent, nil
}

func (d *fakeLEDevice) discoverServices(context.Context) (gatt.GattCommunicationStatus, []remoteService, error) {
	if d.discoverErr != nil {
		return 0, nil, d.discoverErr
	}
	if d.discoverStatus != gatt.GattCommunicationStatusSuccess {
		return d.discoverStatus, nil, nil
	}
	return d.discoverStatus, d.services, nil
}

func (d *fakeLEDevice) close() { d.closed.Add(1) }

func (d *fakeLEDevice) setStatus(status bluetooth.BluetoothConnectionStatus) {
	d.mu.Lock()
	fn := d.onStatus
	d.mu.Unlock()
	fn(status)
}

type fakeClientSession struct {
	mtuErr error
	closed atomic.Int32
	mtu    uint16
}

func (s *fakeClientSession) maxPduSize() (uint16, error) { return s.mtu, s.mtuErr }

func (s *fakeClientSession) close() { s.closed.Add(1) }

type fakeService struct {
	uuidErr  error
	charsErr error
	id       string
	chars    []*fakeGattChar
	closed   atomic.Int32
	status   gatt.GattCommunicationStatus
}

func (s *fakeService) uuid() (string, error) { return s.id, s.uuidErr }

func (s *fakeService) characteristics(context.Context) (gatt.GattCommunicationStatus, []gattCharacteristic, error) {
	if s.charsErr != nil {
		return 0, nil, s.charsErr
	}
	if s.status != gatt.GattCommunicationStatusSuccess {
		return s.status, nil, nil
	}
	out := make([]gattCharacteristic, 0, len(s.chars))
	for _, c := range s.chars {
		out = append(out, c)
	}
	return s.status, out, nil
}

func (s *fakeService) close() { s.closed.Add(1) }

type fakeGattChar struct {
	uuidErr      error
	configureErr error
	watchErr     error
	writeErr     error
	readErr      error
	event        *fakeEvent
	onValue      func([]byte)
	id           string
	configured   []gatt.GattClientCharacteristicConfigurationDescriptorValue
	writes       []clientWrite
	readValue    []byte
	released     released
	mu           syncutil.Mutex
	configStatus gatt.GattCommunicationStatus
	writeStatus  gatt.GattCommunicationStatus
	readStatus   gatt.GattCommunicationStatus
}

type clientWrite struct {
	value  []byte
	option gatt.GattWriteOption
}

func (c *fakeGattChar) uuid() (string, error) { return c.id, c.uuidErr }

func (c *fakeGattChar) configure(
	_ context.Context, value gatt.GattClientCharacteristicConfigurationDescriptorValue,
) (gatt.GattCommunicationStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configured = append(c.configured, value)
	return c.configStatus, c.configureErr
}

func (c *fakeGattChar) onValueChanged(fn func([]byte)) (eventHandle, error) {
	if c.watchErr != nil {
		return nil, c.watchErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onValue = fn
	c.event = &fakeEvent{}
	return c.event, nil
}

func (c *fakeGattChar) write(
	_ context.Context, value []byte, option gatt.GattWriteOption,
) (gatt.GattCommunicationStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, clientWrite{value: slices.Clone(value), option: option})
	return c.writeStatus, c.writeErr
}

func (c *fakeGattChar) read(context.Context) (gatt.GattCommunicationStatus, []byte, error) {
	if c.readErr != nil {
		return 0, nil, c.readErr
	}
	if c.readStatus != gatt.GattCommunicationStatusSuccess {
		return c.readStatus, nil, nil
	}
	return c.readStatus, c.readValue, nil
}

func (c *fakeGattChar) release() { c.released.release() }

func (c *fakeGattChar) notify(value []byte) {
	c.mu.Lock()
	fn := c.onValue
	c.mu.Unlock()
	fn(value)
}

func (c *fakeGattChar) configurations() []gatt.GattClientCharacteristicConfigurationDescriptorValue {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.configured)
}

func (c *fakeGattChar) valueEvent() *fakeEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.event
}

func newTestAdapter(host winHost) (*adapter, *clockwork.FakeClock) {
	clock := clockwork.NewFakeClock()
	return newAdapter(host, clock), clock
}
