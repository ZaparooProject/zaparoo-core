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

package discovery

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/discovery/mdns"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitTimeout bounds how long a test waits for the watch loop to react. It is
// only reached when something is wrong; the happy path returns immediately.
const waitTimeout = 5 * time.Second

// fakeResponder stands in for mdns.Responder so the watch loop can be driven
// without opening a socket or putting traffic on the tester's network.
type fakeResponder struct {
	startErr   error
	startCalls chan []net.Interface
	setCalls   chan []net.Interface
	stopCalls  chan struct{}
	names      []string
	mu         syncutil.Mutex
	changed    bool
}

func newFakeResponder() *fakeResponder {
	return &fakeResponder{
		startCalls: make(chan []net.Interface, 8),
		setCalls:   make(chan []net.Interface, 8),
		stopCalls:  make(chan struct{}, 4),
		changed:    true,
	}
}

func (f *fakeResponder) Start(ifaces []net.Interface) error {
	f.mu.Lock()
	err := f.startErr
	f.mu.Unlock()

	f.startCalls <- ifaces
	return err
}

func (f *fakeResponder) SetInterfaces(ifaces []net.Interface) (bool, error) {
	f.mu.Lock()
	changed := f.changed
	f.mu.Unlock()

	f.setCalls <- ifaces
	return changed, nil
}

func (f *fakeResponder) Interfaces() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.names
}

func (f *fakeResponder) Stop() {
	select {
	case f.stopCalls <- struct{}{}:
	default:
	}
}

func (f *fakeResponder) setStartErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startErr = err
}

func iface(index int, name string) net.Interface {
	return net.Interface{Index: index, Name: name, Flags: net.FlagUp | net.FlagMulticast}
}

// watchHarness wires a discovery service to a fake clock, a scripted interface
// list, and a fake responder.
type watchHarness struct {
	svc       *Service
	responder *fakeResponder
	clock     *clockwork.FakeClock
	ifaces    func() ([]net.Interface, error)
	mu        syncutil.Mutex
}

func newWatchHarness(t *testing.T, initial []net.Interface) *watchHarness {
	t.Helper()

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)

	h := &watchHarness{
		responder: newFakeResponder(),
		clock:     clockwork.NewFakeClock(),
	}
	h.ifaces = func() ([]net.Interface, error) { return initial, nil }

	h.svc = New(cfg)
	h.svc.clock = h.clock
	h.svc.newResponder = func(*mdns.Service, func(string, ...any)) responder {
		return h.responder
	}
	h.svc.listInterfaces = func() ([]net.Interface, error) {
		h.mu.Lock()
		lister := h.ifaces
		h.mu.Unlock()
		return lister()
	}

	return h
}

func (h *watchHarness) setInterfaces(lister func() ([]net.Interface, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ifaces = lister
}

// tick advances the fake clock by one watch interval, waiting for the loop to
// arm its ticker first so the advance is never missed.
func (h *watchHarness) tick(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	require.NoError(t, h.clock.BlockUntilContext(ctx, 1), "watch loop never armed its ticker")
	h.clock.Advance(watchInterval)
}

func waitFor(t *testing.T, calls chan []net.Interface, what string) []net.Interface {
	t.Helper()

	select {
	case got := <-calls:
		return got
	case <-time.After(waitTimeout):
		t.Fatalf("timed out waiting for %s", what)
		return nil
	}
}

func TestWatchRetriesUntilTheNetworkIsReady(t *testing.T) {
	t.Parallel()

	// Core starts before the network does. The old implementation gave up
	// five minutes in; this one keeps watching.
	h := newWatchHarness(t, nil)
	require.NoError(t, h.svc.Start())
	t.Cleanup(h.svc.Stop)

	assert.Empty(t, h.responder.startCalls, "nothing to advertise on yet")

	wired := iface(14, "Ethernet 4")
	h.setInterfaces(func() ([]net.Interface, error) { return []net.Interface{wired}, nil })
	h.tick(t)

	got := waitFor(t, h.responder.startCalls, "registration once an interface appeared")
	assert.Equal(t, []net.Interface{wired}, got)
}

func TestWatchKeepsRetryingAfterAFailedRegistration(t *testing.T) {
	t.Parallel()

	wired := iface(14, "Ethernet 4")
	h := newWatchHarness(t, []net.Interface{wired})
	h.responder.setStartErr(errors.New("socket unavailable"))

	require.NoError(t, h.svc.Start())
	t.Cleanup(h.svc.Stop)
	waitFor(t, h.responder.startCalls, "the first registration attempt")

	h.responder.setStartErr(nil)
	h.tick(t)

	got := waitFor(t, h.responder.startCalls, "a retry after the first attempt failed")
	assert.Equal(t, []net.Interface{wired}, got)
}

func TestWatchReconcilesWhenAnInterfaceAppears(t *testing.T) {
	t.Parallel()

	// A cable plugged in after Core started: the reported failure in #570.
	wireless := iface(22, "Wi-Fi")
	wired := iface(14, "Ethernet 4")

	h := newWatchHarness(t, []net.Interface{wireless})
	require.NoError(t, h.svc.Start())
	t.Cleanup(h.svc.Stop)
	waitFor(t, h.responder.startCalls, "the initial registration")

	h.setInterfaces(func() ([]net.Interface, error) {
		return []net.Interface{wireless, wired}, nil
	})
	h.tick(t)

	got := waitFor(t, h.responder.setCalls, "the interface set to be reconciled")
	assert.Equal(t, []net.Interface{wireless, wired}, got)
	assert.Empty(t, h.responder.startCalls, "an established responder is updated, not restarted")
}

func TestWatchSurvivesAnInterfaceListingFailure(t *testing.T) {
	t.Parallel()

	wireless := iface(22, "Wi-Fi")
	h := newWatchHarness(t, []net.Interface{wireless})
	require.NoError(t, h.svc.Start())
	t.Cleanup(h.svc.Stop)
	waitFor(t, h.responder.startCalls, "the initial registration")

	h.setInterfaces(func() ([]net.Interface, error) {
		return nil, errors.New("interface table unavailable")
	})
	h.tick(t)

	h.setInterfaces(func() ([]net.Interface, error) { return []net.Interface{wireless}, nil })
	h.tick(t)

	got := waitFor(t, h.responder.setCalls, "the watch loop to carry on after a listing error")
	assert.Equal(t, []net.Interface{wireless}, got)
}

func TestStopEndsTheWatchLoopAndTheResponder(t *testing.T) {
	t.Parallel()

	h := newWatchHarness(t, []net.Interface{iface(22, "Wi-Fi")})
	require.NoError(t, h.svc.Start())
	waitFor(t, h.responder.startCalls, "the initial registration")

	h.svc.Stop()

	select {
	case <-h.responder.stopCalls:
	case <-time.After(waitTimeout):
		t.Fatal("responder was never stopped")
	}
	assert.Nil(t, h.svc.responder)

	// Stopping twice must stay safe; the second call has nothing left to do.
	h.svc.Stop()
}

func TestStartAfterStopDoesNotAdvertise(t *testing.T) {
	t.Parallel()

	h := newWatchHarness(t, []net.Interface{iface(22, "Wi-Fi")})
	h.svc.Stop()

	require.NoError(t, h.svc.Start())
	assert.Nil(t, h.svc.responder)
	assert.Empty(t, h.responder.startCalls)
}
