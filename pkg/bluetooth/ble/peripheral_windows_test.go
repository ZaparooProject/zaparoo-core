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
	"slices"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/saltosystems/winrt-go/windows/devices/bluetooth"
	gatt "github.com/saltosystems/winrt-go/windows/devices/bluetooth/genericattributeprofile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testServiceUUID = "6E400001-B5A3-F393-E0A9-E50E24DCCA9E"
	testRXUUID      = "6E400002-B5A3-F393-E0A9-E50E24DCCA9E"
	testTXUUID      = "6E400003-B5A3-F393-E0A9-E50E24DCCA9E"
	testDeviceID    = "BluetoothLE#BluetoothLE00:11:22:33:44:55-aa:bb:cc:dd:ee:01"
	testDeviceAddr  = "AA:BB:CC:DD:EE:01"
	testOtherID     = "BluetoothLE#BluetoothLE00:11:22:33:44:55-aa:bb:cc:dd:ee:02"
)

var (
	rxLower = "6e400002-b5a3-f393-e0a9-e50e24dcca9e"
	txLower = "6e400003-b5a3-f393-e0a9-e50e24dcca9e"
)

type recWrite struct {
	peer  Peer
	uuid  string
	value []byte
	mtu   int
}

type recSubscribe struct {
	peer       Peer
	uuid       string
	subscribed bool
}

// recHandler records what the peripheral reports.
type recHandler struct {
	readErr      error
	disconnected chan Peer
	readValue    []byte
	writes       []recWrite
	reads        []Peer
	subscribes   []recSubscribe
	mu           syncutil.Mutex
}

func newRecHandler() *recHandler {
	return &recHandler{disconnected: make(chan Peer, 8)}
}

func (h *recHandler) OnWrite(peer Peer, uuid string, value []byte, mtu int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.writes = append(h.writes, recWrite{peer: peer, uuid: uuid, value: slices.Clone(value), mtu: mtu})
}

func (h *recHandler) OnRead(peer Peer, _ string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reads = append(h.reads, peer)
	return h.readValue, h.readErr
}

func (h *recHandler) OnSubscribe(peer Peer, uuid string, subscribed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribes = append(h.subscribes, recSubscribe{peer: peer, uuid: uuid, subscribed: subscribed})
}

func (h *recHandler) OnDisconnect(peer Peer) { h.disconnected <- peer }

func (h *recHandler) recordedWrites() []recWrite {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.writes)
}

func testApp() Application {
	return Application{Services: []Service{{
		UUID:    testServiceUUID,
		Primary: true,
		Characteristics: []Characteristic{
			{UUID: testRXUUID, Flags: []string{FlagWrite, FlagWriteWithoutResponse}},
			{UUID: testTXUUID, Flags: []string{FlagRead, FlagNotify}},
		},
	}}}
}

// serving is a peripheral whose Serve is running against fakes.
type serving struct {
	p       *peripheral
	a       *adapter
	clock   *clockwork.FakeClock
	host    *fakeHost
	handler *recHandler
	err     error
	cancel  context.CancelFunc
	done    chan struct{}
}

func startServe(t *testing.T, host *fakeHost) *serving {
	t.Helper()
	a, clock := newTestAdapter(host)
	s := &serving{
		p:       newPeripheral(a),
		a:       a,
		clock:   clock,
		host:    host,
		handler: newRecHandler(),
		done:    make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() {
		s.err = s.p.Serve(ctx, testApp(), Advertisement{LocalName: "Zaparoo"}, s.handler)
		close(s.done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-s.done:
		case <-time.After(testWait):
			t.Error("Serve did not return")
		}
	})
	return s
}

// startServing starts Serve and waits until it is advertising, so the
// characteristics' events are wired.
func startServing(t *testing.T) *serving {
	t.Helper()
	s := startServe(t, newFakeHost())
	select {
	case <-s.host.provider.advertising:
	case <-time.After(testWait):
		require.FailNow(t, "Serve did not start advertising")
	}
	return s
}

func (s *serving) stop(t *testing.T) error {
	t.Helper()
	s.cancel()
	return s.wait(t)
}

func (s *serving) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-s.done:
		return s.err
	case <-time.After(testWait):
		require.FailNow(t, "Serve did not return")
		return nil
	}
}

func (s *serving) rx() *fakeLocalChar { return s.host.provider.char(testRXUUID) }

func (s *serving) tx() *fakeLocalChar { return s.host.provider.char(testTXUUID) }

func isGone(a *adapter) bool {
	select {
	case <-a.gone:
		return true
	default:
		return false
	}
}

func TestPropertiesMapsFlags(t *testing.T) {
	t.Parallel()
	assert.Equal(t, gatt.GattCharacteristicPropertiesRead|gatt.GattCharacteristicPropertiesNotify,
		properties([]string{FlagRead, FlagNotify, "indicate"}))
	assert.Equal(t, gatt.GattCharacteristicPropertiesWrite|gatt.GattCharacteristicPropertiesWriteWithoutResponse,
		properties([]string{FlagWrite, FlagWriteWithoutResponse}))
	assert.Equal(t, gatt.GattCharacteristicPropertiesRead, properties([]string{FlagEncryptRead}))
	assert.Equal(t, gatt.GattCharacteristicProperties(0), properties(nil))
}

func TestAdvertisingStatuses(t *testing.T) {
	t.Parallel()
	assert.True(t, advertising(gatt.GattServiceProviderAdvertisementStatusStarted))
	assert.True(t, advertising(gatt.GattServiceProviderAdvertisementStatusStartedWithoutAllAdvertisementData))
	assert.False(t, advertising(gatt.GattServiceProviderAdvertisementStatusAborted))
	assert.False(t, advertising(gatt.GattServiceProviderAdvertisementStatusStopped))
	assert.False(t, advertising(gatt.GattServiceProviderAdvertisementStatusCreated))
}

func TestServeRejectsAnythingButOneService(t *testing.T) {
	t.Parallel()
	a, _ := newTestAdapter(newFakeHost())
	p := newPeripheral(a)
	err := p.Serve(context.Background(), Application{}, Advertisement{}, newRecHandler())
	require.ErrorContains(t, err, "exactly one service, got 0")
	app := testApp()
	app.Services = append(app.Services, app.Services[0])
	err = p.Serve(context.Background(), app, Advertisement{}, newRecHandler())
	require.ErrorContains(t, err, "exactly one service, got 2")
	assert.False(t, isGone(a))
}

func TestServePublishesAndCleansUp(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	prov := s.host.provider

	prov.mu.Lock()
	assert.Equal(t, testServiceUUID, prov.serviceUUID)
	assert.Equal(t, []string{testRXUUID, testTXUUID}, prov.created)
	prov.mu.Unlock()
	assert.Equal(t, gatt.GattCharacteristicPropertiesWrite|gatt.GattCharacteristicPropertiesWriteWithoutResponse,
		s.rx().props)
	assert.Equal(t, gatt.GattCharacteristicPropertiesRead|gatt.GattCharacteristicPropertiesNotify, s.tx().props)

	err := s.p.Serve(context.Background(), testApp(), Advertisement{}, newRecHandler())
	require.ErrorIs(t, err, errAlreadyServing)

	require.NoError(t, s.stop(t))
	assert.Equal(t, int32(1), prov.stops.Load())
	assert.Equal(t, 1, prov.released.count())
	for _, c := range []*fakeLocalChar{s.rx(), s.tx()} {
		assert.Equal(t, int32(1), c.unwatched.Load())
		assert.Equal(t, 1, c.released.count())
	}
	assert.False(t, isGone(s.a), "ending ctx is not the radio going away")

	s.p.mu.Lock()
	assert.False(t, s.p.serving)
	assert.Empty(t, s.p.chars)
	assert.Nil(t, s.p.handler)
	s.p.mu.Unlock()
}

func TestServeReturnsWhenAdapterCloses(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	require.NoError(t, s.a.Close())
	require.NoError(t, s.wait(t))
	assert.Equal(t, int32(1), s.host.provider.stops.Load())
}

func TestServeServiceCreationFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target error
		setup  func(*fakeHost)
		name   string
	}{
		{
			name:   "call fails",
			setup:  func(h *fakeHost) { h.providerErr = errFake },
			target: errFake,
		},
		{
			name:   "no radio",
			setup:  func(h *fakeHost) { h.providerCode = bluetooth.BluetoothErrorRadioNotAvailable },
			target: ErrNoAdapter,
		},
		{
			name:   "disabled",
			setup:  func(h *fakeHost) { h.providerCode = bluetooth.BluetoothErrorDisabledByUser },
			target: ErrUnavailable,
		},
		{
			name:   "characteristic call fails",
			setup:  func(h *fakeHost) { h.provider.charErr[testTXUUID] = errFake },
			target: errFake,
		},
		{
			name: "characteristic not supported",
			setup: func(h *fakeHost) {
				h.provider.charCode[testTXUUID] = bluetooth.BluetoothErrorNotSupported
			},
			target: ErrRoleUnsupported,
		},
		{
			name:   "characteristic events",
			setup:  func(h *fakeHost) { h.provider.char(testTXUUID).watchErr = errFake },
			target: errFake,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host := newFakeHost()
			tt.setup(host)
			s := startServe(t, host)
			require.ErrorIs(t, s.wait(t), tt.target)
			assert.True(t, isGone(s.a), "a failure to publish gives the adapter up")
		})
	}
}

// A Serve that finds no usable radio leaves the adapter gone with the
// reason, since on Windows that is the only place a missing radio shows.
func TestServeFailureLeavesTheCauseWithTheAdapter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target error
		setup  func(*fakeHost)
		name   string
	}{
		{
			name:   "no radio",
			setup:  func(h *fakeHost) { h.providerCode = bluetooth.BluetoothErrorRadioNotAvailable },
			target: ErrNoAdapter,
		},
		{
			name:   "disabled",
			setup:  func(h *fakeHost) { h.providerCode = bluetooth.BluetoothErrorDisabledByUser },
			target: ErrUnavailable,
		},
		{
			name:   "not supported",
			setup:  func(h *fakeHost) { h.providerCode = bluetooth.BluetoothErrorNotSupported },
			target: ErrRoleUnsupported,
		},
		{
			name:   "advertising refused",
			setup:  func(h *fakeHost) { h.provider.startErr = ErrRoleUnsupported },
			target: ErrRoleUnsupported,
		},
		{
			name:   "call fails",
			setup:  func(h *fakeHost) { h.providerErr = errFake },
			target: errFake,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host := newFakeHost()
			tt.setup(host)
			s := startServe(t, host)
			require.ErrorIs(t, s.wait(t), tt.target)
			require.True(t, isGone(s.a))
			require.ErrorIs(t, s.a.GoneCause(), tt.target)
		})
	}
}

// A failure that is not about the radio must not read as a missing one.
func TestServeFailureCauseIsNotMistakenForAMissingRadio(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.providerErr = errFake
	s := startServe(t, host)
	require.ErrorIs(t, s.wait(t), errFake)
	cause := s.a.GoneCause()
	require.Error(t, cause)
	for _, target := range []error{ErrNoAdapter, ErrUnavailable, ErrPoweredOff, ErrRoleUnsupported} {
		require.NotErrorIs(t, cause, target)
	}
}

func TestServeCharacteristicFailureUndoesEarlierOnes(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.provider.charErr[testTXUUID] = errFake
	s := startServe(t, host)
	require.ErrorIs(t, s.wait(t), errFake)
	assert.Equal(t, int32(1), s.rx().unwatched.Load())
	assert.Equal(t, 1, s.rx().released.count())
	assert.Equal(t, 1, host.provider.released.count())
}

func TestServeCharacteristicWatchFailureReleasesIt(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.provider.char(testTXUUID).watchErr = errFake
	s := startServe(t, host)
	require.ErrorIs(t, s.wait(t), errFake)
	assert.Equal(t, 1, s.tx().released.count())
	assert.Equal(t, int32(0), s.tx().unwatched.Load())
	assert.Equal(t, 1, s.rx().released.count())
}

func TestServePublishFailureAfterCancelKeepsAdapter(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.providerErr = context.Canceled
	a, _ := newTestAdapter(host)
	p := newPeripheral(a)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.Serve(ctx, testApp(), Advertisement{}, newRecHandler())
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, isGone(a))
}

func TestServeAdvertiseStartFails(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.provider.startErr = ErrRoleUnsupported
	s := startServe(t, host)
	require.ErrorIs(t, s.wait(t), ErrRoleUnsupported)
	assert.True(t, isGone(s.a))
	assert.Equal(t, int32(0), host.provider.stops.Load(), "advertising never started")
	assert.Equal(t, 1, host.provider.released.count())
}

func TestServeAdvertiseStatusError(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.provider.statusErr = errFake
	s := startServe(t, host)
	err := s.wait(t)
	require.ErrorIs(t, err, errFake)
	require.ErrorContains(t, err, "advertising status")
	assert.True(t, isGone(s.a))
}

// answer takes the next advertisementStatus call and replies to it.
func answer(t *testing.T, prov *fakeProvider, status gatt.GattServiceProviderAdvertisementStatus) {
	t.Helper()
	select {
	case reply := <-prov.calls:
		reply <- statusReply{status: status}
	case <-time.After(testWait):
		require.FailNow(t, "no advertisement status call")
	}
}

// tick advances the clock a second at a time until Serve asks for the
// advertisement status, then replies. Serve's ticker is made after the
// advertising wait ends, which nothing outside can observe, so the clock is
// advanced until a tick lands.
func tick(t *testing.T, s *serving, status gatt.GattServiceProviderAdvertisementStatus) {
	t.Helper()
	deadline := time.After(testWait)
	for {
		s.clock.Advance(time.Second)
		select {
		case reply := <-s.host.provider.calls:
			reply <- statusReply{status: status}
			return
		case <-deadline:
			require.FailNow(t, "Serve did not poll the advertisement status")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestAdvertiseWaitsThroughAbortedUntilStarted(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.provider.calls = make(chan chan statusReply)
	s := startServe(t, host)

	answer(t, host.provider, gatt.GattServiceProviderAdvertisementStatusAborted)
	s.clock.Advance(advertisePoll)
	answer(t, host.provider, gatt.GattServiceProviderAdvertisementStatusStartedWithoutAllAdvertisementData)
	tick(t, s, gatt.GattServiceProviderAdvertisementStatusStarted)
	assert.False(t, isGone(s.a))
	require.NoError(t, s.stop(t))
}

func TestAdvertiseTimesOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target error
		name   string
		status gatt.GattServiceProviderAdvertisementStatus
	}{
		{name: "aborted", status: gatt.GattServiceProviderAdvertisementStatusAborted, target: ErrRoleUnsupported},
		{name: "created", status: gatt.GattServiceProviderAdvertisementStatusCreated, target: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host := newFakeHost()
			host.provider.calls = make(chan chan statusReply)
			s := startServe(t, host)

			answer(t, host.provider, tt.status)
			s.clock.Advance(advertiseStartTimeout)
			for {
				select {
				case reply := <-host.provider.calls:
					reply <- statusReply{status: tt.status}
					continue
				case <-s.done:
					require.ErrorIs(t, s.err, tt.target)
					assert.True(t, isGone(s.a))
					require.ErrorIs(t, s.a.GoneCause(), tt.target)
				}
				break
			}
		})
	}
}

func TestAdvertiseCanceled(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.provider.calls = make(chan chan statusReply)
	s := startServe(t, host)
	answer(t, host.provider, gatt.GattServiceProviderAdvertisementStatusCreated)
	s.cancel()
	err := s.wait(t)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, isGone(s.a))
}

func TestServeGivesUpAfterAdvertisingStaysStopped(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.provider.calls = make(chan chan statusReply)
	s := startServe(t, host)
	answer(t, host.provider, gatt.GattServiceProviderAdvertisementStatusStarted)

	stopped := gatt.GattServiceProviderAdvertisementStatusStopped
	for range advertiseStoppedSeconds - 1 {
		tick(t, s, stopped)
	}
	// A flicker back to started resets the count.
	tick(t, s, gatt.GattServiceProviderAdvertisementStatusStarted)
	for range advertiseStoppedSeconds - 1 {
		tick(t, s, stopped)
	}
	assert.False(t, isGone(s.a))

	tick(t, s, stopped)
	require.NoError(t, s.wait(t))
	assert.True(t, isGone(s.a))
	require.ErrorIs(t, s.a.GoneCause(), ErrUnavailable, "advertising that stops is the radio going away")
	assert.Equal(t, int32(1), host.provider.stops.Load())
}

func TestServeCountsStatusErrorsAsStopped(t *testing.T) {
	t.Parallel()
	host := newFakeHost()
	host.provider.calls = make(chan chan statusReply)
	s := startServe(t, host)
	answer(t, host.provider, gatt.GattServiceProviderAdvertisementStatusStarted)

	deadline := time.After(testWait)
	for replies := 0; replies < advertiseStoppedSeconds; {
		s.clock.Advance(time.Second)
		select {
		case reply := <-host.provider.calls:
			reply <- statusReply{status: gatt.GattServiceProviderAdvertisementStatusStarted, err: errFake}
			replies++
		case <-deadline:
			require.FailNow(t, "Serve did not poll the advertisement status")
		case <-time.After(10 * time.Millisecond):
		}
	}
	require.NoError(t, s.wait(t))
	assert.True(t, isGone(s.a))
}

func writeFrom(id string, value []byte, opt gatt.GattWriteOption) (*fakeWriteEvent, *fakeServerSession) {
	sess := &fakeServerSession{id: id, mtu: 247}
	return &fakeWriteEvent{sess: sess, req: &fakeWriteRequest{val: value, opt: opt}}, sess
}

func TestWriteReachesHandlerWithPeerAndMTU(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	events := s.rx().handlers()

	first, firstSess := writeFrom(testDeviceID, []byte("one"), gatt.GattWriteOptionWriteWithoutResponse)
	events.write(first)
	second, secondSess := writeFrom(testDeviceID, []byte("two"), gatt.GattWriteOptionWriteWithResponse)
	events.write(second)

	peer := Peer{Path: testDeviceID, Address: testDeviceAddr}
	assert.Equal(t, []recWrite{
		{peer: peer, uuid: rxLower, value: []byte("one"), mtu: 247},
		{peer: peer, uuid: rxLower, value: []byte("two"), mtu: 247},
	}, s.handler.recordedWrites())
	assert.Equal(t, int32(0), first.req.responded.Load(), "a command is not answered")
	assert.Equal(t, int32(1), second.req.responded.Load(), "a request is answered")
	assert.Equal(t, 1, first.req.released.count())
	assert.Equal(t, 1, second.req.released.count())

	assert.Equal(t, 0, firstSess.released.count(), "the first session is kept to watch for the peer leaving")
	assert.Equal(t, 1, secondSess.released.count(), "a known peer's session is handed back")

	require.NoError(t, s.stop(t))
	assert.Equal(t, 1, firstSess.released.count())
	assert.Equal(t, int32(1), firstSess.closedEvent().removed.Load())
	assert.Equal(t, 1, firstSess.closedEvent().released.count())
}

func TestWriteMTUUnknown(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	event, sess := writeFrom(testDeviceID, []byte("x"), gatt.GattWriteOptionWriteWithoutResponse)
	sess.mtuErr = errFake
	s.rx().handlers().write(event)
	writes := s.handler.recordedWrites()
	require.Len(t, writes, 1)
	assert.Zero(t, writes[0].mtu)
}

func TestWriteFailuresAreDropped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		modify       func(*fakeWriteEvent)
		name         string
		requestFreed bool
	}{
		{name: "no session", modify: func(e *fakeWriteEvent) { e.sessErr = errFake }},
		{name: "unidentified peer", modify: func(e *fakeWriteEvent) { e.sess.idErr = errFake }},
		{name: "request", modify: func(e *fakeWriteEvent) { e.reqErr = errFake }},
		{name: "value", modify: func(e *fakeWriteEvent) { e.req.valueErr = errFake }, requestFreed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := startServing(t)
			event, _ := writeFrom(testDeviceID, []byte("x"), gatt.GattWriteOptionWriteWithResponse)
			tt.modify(event)
			s.rx().handlers().write(event)
			assert.Empty(t, s.handler.recordedWrites())
			assert.Equal(t, int32(0), event.req.responded.Load())
			if tt.requestFreed {
				assert.Equal(t, 1, event.req.released.count())
			}
		})
	}
}

func TestWriteDeliveredWhenRespondFails(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	event, _ := writeFrom(testDeviceID, []byte("x"), gatt.GattWriteOptionWriteWithResponse)
	event.req.respondErr = errFake
	s.rx().handlers().write(event)
	assert.Len(t, s.handler.recordedWrites(), 1)
}

func TestUnidentifiedSessionIsReleased(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	event, sess := writeFrom(testDeviceID, []byte("x"), gatt.GattWriteOptionWriteWithoutResponse)
	sess.idErr = errFake
	s.rx().handlers().write(event)
	assert.Equal(t, 1, sess.released.count())
}

func TestSessionWatchFailureStillDelivers(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	event, sess := writeFrom(testDeviceID, []byte("x"), gatt.GattWriteOptionWriteWithoutResponse)
	sess.watchErr = errFake
	s.rx().handlers().write(event)
	assert.Len(t, s.handler.recordedWrites(), 1)
	assert.Equal(t, 1, sess.released.count())
	s.p.mu.Lock()
	assert.Empty(t, s.p.sessions)
	s.p.mu.Unlock()
}

func TestSessionCloseReportsDisconnect(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	event, sess := writeFrom(testDeviceID, []byte("x"), gatt.GattWriteOptionWriteWithoutResponse)
	s.rx().handlers().write(event)

	sess.close()
	select {
	case peer := <-s.handler.disconnected:
		assert.Equal(t, Peer{Path: testDeviceID, Address: testDeviceAddr}, peer)
	case <-time.After(testWait):
		require.FailNow(t, "no disconnect reported")
	}
	assert.Eventually(t, func() bool { return sess.released.count() == 1 }, testWait, time.Millisecond)
	assert.Equal(t, int32(1), sess.closedEvent().removed.Load())

	// A second close of a forgotten session reports nothing.
	sess.close()
	select {
	case peer := <-s.handler.disconnected:
		assert.Failf(t, "reported twice", "%v", peer)
	default:
	}

	// The peer comes back with a new session, which is tracked afresh.
	again, againSess := writeFrom(testDeviceID, []byte("y"), gatt.GattWriteOptionWriteWithoutResponse)
	s.rx().handlers().write(again)
	assert.Equal(t, 0, againSess.released.count())
}

func TestReadAnswersFromHandler(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	s.handler.readValue = []byte("0123456789")
	events := s.tx().handlers()

	whole := &fakeReadRequest{}
	events.read(&fakeReadEvent{sess: &fakeServerSession{id: testDeviceID}, req: whole})
	assert.Equal(t, []byte("0123456789"), whole.value)
	assert.Equal(t, 1, whole.released.count())

	tail := &fakeReadRequest{off: 4}
	events.read(&fakeReadEvent{sess: &fakeServerSession{id: testDeviceID}, req: tail})
	assert.Equal(t, []byte("456789"), tail.value)

	end := &fakeReadRequest{off: 10}
	events.read(&fakeReadEvent{sessErr: errFake, req: end})
	assert.True(t, end.answered)
	assert.Empty(t, end.value)

	past := &fakeReadRequest{off: 11}
	events.read(&fakeReadEvent{sessErr: errFake, req: past})
	assert.Equal(t, []byte("0123456789"), past.value, "an offset past the end is ignored")

	noOffset := &fakeReadRequest{off: 4, offsetErr: errFake}
	events.read(&fakeReadEvent{sessErr: errFake, req: noOffset})
	assert.Equal(t, []byte("0123456789"), noOffset.value)

	s.handler.mu.Lock()
	peer := Peer{Path: testDeviceID, Address: testDeviceAddr}
	assert.Equal(t, []Peer{peer, peer, {}, {}, {}}, s.handler.reads)
	s.handler.mu.Unlock()
}

func TestReadFailuresAnswerWithProtocolError(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	events := s.tx().handlers()

	s.handler.readErr = ErrNotFound
	refused := &fakeReadRequest{}
	events.read(&fakeReadEvent{sessErr: errFake, req: refused})
	assert.Equal(t, []byte{attErrorUnlikely}, refused.errorCodes)
	assert.False(t, refused.answered)

	s.handler.readErr = nil
	unprepared := &fakeReadRequest{valueErr: errFake}
	events.read(&fakeReadEvent{sessErr: errFake, req: unprepared})
	assert.Equal(t, []byte{attErrorUnlikely}, unprepared.errorCodes)

	events.read(&fakeReadEvent{sessErr: errFake, reqErr: errFake})
	s.handler.mu.Lock()
	assert.Len(t, s.handler.reads, 2, "a request that cannot be fetched is not passed on")
	s.handler.mu.Unlock()
}

func TestReadWithoutServeUsesNoopHandler(t *testing.T) {
	t.Parallel()
	a, _ := newTestAdapter(newFakeHost())
	p := newPeripheral(a)
	req := &fakeReadRequest{}
	p.onRead(txLower, &fakeReadEvent{sessErr: errFake, req: req})
	assert.Equal(t, []byte{attErrorUnlikely}, req.errorCodes)

	h := p.currentHandler()
	h.OnWrite(Peer{}, txLower, nil, 0)
	h.OnSubscribe(Peer{}, txLower, true)
	h.OnDisconnect(Peer{})
}

func TestNotifyUnknownCharacteristic(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	err := s.p.Notify(Peer{}, "6e400009-b5a3-f393-e0a9-e50e24dcca9e", []byte("x"))
	require.ErrorIs(t, err, ErrNotFound)
}

func TestNotifyBroadcast(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	require.NoError(t, s.p.Notify(Peer{}, testTXUUID, []byte("all")))
	s.tx().mu.Lock()
	assert.Equal(t, [][]byte{[]byte("all")}, s.tx().broadcasts)
	s.tx().mu.Unlock()

	s.tx().notifyAllErr = errFake
	err := s.p.Notify(Peer{}, txLower, []byte("all"))
	require.ErrorIs(t, err, errFake)
	assert.Equal(t, int32(0), s.tx().clientsCalls.Load())
}

func TestNotifyOnePeerCachesSubscription(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	tx := s.tx()
	tx.clientIDs = []string{"", testOtherID, testDeviceID, testDeviceID}
	tx.waited = make(chan error, 4)
	tx.waitErr = errFake
	peer := Peer{Path: testDeviceID, Address: testDeviceAddr}

	require.NoError(t, s.p.Notify(peer, testTXUUID, []byte("a")))
	require.NoError(t, s.p.Notify(peer, testTXUUID, []byte("b")))
	assert.Equal(t, int32(1), tx.clientsCalls.Load(), "the subscription is looked up once")

	clients := tx.handedOut()
	require.Len(t, clients, 4)
	assert.Equal(t, 1, clients[0].released.count())
	assert.Equal(t, 1, clients[1].released.count())
	assert.Equal(t, 0, clients[2].released.count(), "the match is cached")
	assert.Equal(t, 1, clients[3].released.count(), "only the first match is kept")

	tx.mu.Lock()
	require.Len(t, tx.clientNotices, 2)
	assert.Same(t, clients[2], tx.clientNotices[0].client)
	assert.Equal(t, []byte("b"), tx.clientNotices[1].value)
	tx.mu.Unlock()
	for range 2 {
		select {
		case err := <-tx.waited:
			require.ErrorIs(t, err, errFake)
		case <-time.After(testWait):
			require.FailNow(t, "queued notification was not waited for")
		}
	}

	// A change in subscriptions drops the cache.
	tx.handlers().subscribersChanged()
	assert.Equal(t, 1, clients[2].released.count())
	s.handler.mu.Lock()
	assert.Equal(t, []recSubscribe{{uuid: txLower, subscribed: true}}, s.handler.subscribes)
	s.handler.mu.Unlock()
	require.NoError(t, s.p.Notify(peer, testTXUUID, []byte("c")))
	assert.Equal(t, int32(2), tx.clientsCalls.Load())

	require.NoError(t, s.stop(t))
	for _, c := range tx.handedOut() {
		assert.Equal(t, 1, c.released.count(), "every subscription is released once")
	}
}

func TestNotifyPeerNotSubscribed(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	s.tx().clientIDs = []string{testOtherID}
	err := s.p.Notify(Peer{Path: testDeviceID}, testTXUUID, []byte("a"))
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, 1, s.tx().handedOut()[0].released.count())

	s.tx().clientsErr = errFake
	err = s.p.Notify(Peer{Path: testDeviceID}, testTXUUID, []byte("a"))
	require.ErrorIs(t, err, errFake)
}

func TestNotifyFailureDropsStaleSubscription(t *testing.T) {
	t.Parallel()
	s := startServing(t)
	tx := s.tx()
	tx.clientIDs = []string{testDeviceID}
	tx.notifyClientErr = errFake
	err := s.p.Notify(Peer{Path: testDeviceID}, testTXUUID, []byte("a"))
	require.ErrorIs(t, err, errFake)
	assert.Equal(t, 1, tx.handedOut()[0].released.count())
	s.p.mu.Lock()
	assert.Empty(t, s.p.subs)
	s.p.mu.Unlock()
}

// racingChar caches a subscription for the peer while the lookup runs,
// as a concurrent notification would.
type racingChar struct {
	*fakeLocalChar
	p        *peripheral
	existing subscribedClient
}

func (c *racingChar) subscribedClients() ([]subscribedClient, error) {
	c.p.mu.Lock()
	c.p.subs[subKey(txLower, testDeviceID)] = c.existing
	c.p.mu.Unlock()
	return c.fakeLocalChar.subscribedClients()
}

func TestSubscriptionLookupRaceKeepsCachedOne(t *testing.T) {
	t.Parallel()
	a, _ := newTestAdapter(newFakeHost())
	p := newPeripheral(a)
	existing := &fakeSubscribedClient{id: testDeviceID}
	c := &racingChar{fakeLocalChar: &fakeLocalChar{clientIDs: []string{testDeviceID}}, p: p, existing: existing}

	got, err := p.subscription(c, txLower, testDeviceID)
	require.NoError(t, err)
	assert.Same(t, existing, got)
	found := c.handedOut()
	require.Len(t, found, 1)
	assert.Equal(t, 1, found[0].released.count())
	assert.Equal(t, 0, existing.released.count())
}

func TestDropSubscriptionsByCharacteristic(t *testing.T) {
	t.Parallel()
	a, _ := newTestAdapter(newFakeHost())
	p := newPeripheral(a)
	rx := &fakeSubscribedClient{}
	tx := &fakeSubscribedClient{}
	p.subs[subKey(rxLower, testDeviceID)] = rx
	p.subs[subKey(txLower, testDeviceID)] = tx

	p.dropSubscriptions(txLower)
	assert.Equal(t, 0, rx.released.count())
	assert.Equal(t, 1, tx.released.count())
	p.dropSubscriptions("")
	assert.Equal(t, 1, rx.released.count())
	assert.Empty(t, p.subs)
}

func TestTrackRaceKeepsFirstSession(t *testing.T) {
	t.Parallel()
	a, _ := newTestAdapter(newFakeHost())
	p := newPeripheral(a)
	sess := &fakeServerSession{id: testDeviceID}
	winner := &fakeServerSession{id: testDeviceID, closed: &fakeEvent{}}
	// Another callback registers the peer between the check and the insert.
	racing := &racingSession{fakeServerSession: sess, onWatch: func() {
		p.mu.Lock()
		p.sessions[testDeviceID] = &peerSession{session: winner, closed: winner.closed}
		p.mu.Unlock()
	}}
	peer, err := p.track(racing)
	require.NoError(t, err)
	assert.Equal(t, testDeviceAddr, peer.Address)
	assert.Equal(t, 1, sess.released.count())
	assert.Equal(t, int32(1), sess.closedEvent().removed.Load())
	assert.Equal(t, 0, winner.released.count())
}

type racingSession struct {
	*fakeServerSession
	onWatch func()
}

func (r *racingSession) onClosed(fn func()) (eventHandle, error) {
	h, err := r.fakeServerSession.onClosed(fn)
	r.onWatch()
	return h, err
}

func TestPeripheralDisconnect(t *testing.T) {
	t.Parallel()
	a, _ := newTestAdapter(newFakeHost())
	p := newPeripheral(a)
	require.ErrorIs(t, p.Disconnect(context.Background(), Peer{}), ErrNotFound)
	require.NoError(t, p.Disconnect(context.Background(), Peer{Path: testDeviceID}))
	p.SetAdvertising(false)
	p.SetAdvertising(true)
}
