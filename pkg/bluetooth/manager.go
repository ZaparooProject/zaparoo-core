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

// Package bluetooth owns the lifecycle of the local Bluetooth adapter for
// the app-facing BLE transport: when to open it, when to give up, and when
// to try again after a dongle is plugged in.
package bluetooth

import (
	"context"
	"errors"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// watchInterval is how often the manager re-checks configuration and the
// adapter. A dongle plugged in after boot is picked up within one interval.
const watchInterval = 15 * time.Second

const (
	// After losing an adapter that had been working, typically because
	// bluetoothd restarted or the controller was reset, the manager tries
	// to get it back this often, this many times, before settling back to
	// watchInterval.
	lossRetryInterval = 2 * time.Second
	lossRetryAttempts = 15

	// stableAfter is how long an adapter must stay open to count as having
	// worked. One lost sooner is failing to come up at all, and is retried
	// quietly at watchInterval instead.
	stableAfter = 2 * watchInterval
)

// PeripheralFunc is called with a ready peripheral each time one becomes
// available. It must not block: start any long-running work on a goroutine.
type PeripheralFunc func(bluez.Peripheral)

// Manager keeps an adapter open while the BLE transport is enabled and
// hands its peripheral side to whoever registered interest.
type Manager struct {
	cfg       *config.Instance
	clock     clockwork.Clock
	open      func(ctx context.Context) (bluez.Adapter, error)
	adapter   bluez.Adapter
	cancel    context.CancelFunc
	done      chan struct{}
	callbacks map[int]PeripheralFunc
	openedAt  time.Time
	mu        syncutil.Mutex
	nextID    int
	// retries is how many quick attempts are left to get back an adapter
	// that was lost. Only the watch loop touches it.
	retries int
	// flapping is set while the adapter keeps opening and failing straight
	// away, so that cycle is logged once and not every interval.
	flapping bool
	// unavailableReported and roleReported keep a machine that has no
	// usable adapter from logging the same line every interval.
	unavailableReported bool
	roleReported        bool
	started             bool
	stopped             bool
	// unsupported is set once the platform says it has no BlueZ at all.
	unsupported bool
}

// NewManager builds a manager over the real BlueZ layer.
func NewManager(cfg *config.Instance) *Manager {
	return newManagerWith(cfg, clockwork.NewRealClock(), func(ctx context.Context) (bluez.Adapter, error) {
		// The user enabled the transport, so a powered-off adapter is
		// switched on for them.
		return bluez.Open(ctx, bluez.WithPowerOn())
	})
}

func newManagerWith(
	cfg *config.Instance,
	clock clockwork.Clock,
	open func(ctx context.Context) (bluez.Adapter, error),
) *Manager {
	return &Manager{cfg: cfg, clock: clock, open: open, callbacks: make(map[int]PeripheralFunc)}
}

// Start begins watching. It never fails: an absent bus, daemon or adapter
// is reported once and retried every interval.
func (m *Manager) Start() {
	m.mu.Lock()
	if m.started || m.stopped {
		m.mu.Unlock()
		return
	}
	m.started = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	m.mu.Unlock()

	go m.run(ctx)
}

// Stop closes the adapter and waits for the watch loop to exit.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	cancel, done := m.cancel, m.done
	m.mu.Unlock()

	if cancel != nil {
		cancel()
		<-done
	}
}

// Roles reports what the open adapter supports, or nil without one.
func (m *Manager) Roles() []bluez.Role {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.adapter == nil {
		return nil
	}
	return m.adapter.Roles()
}

// OnPeripheral registers fn to receive the peripheral now, if one is ready,
// and again after every re-open. The returned function unregisters it.
func (m *Manager) OnPeripheral(fn PeripheralFunc) func() {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.callbacks[id] = fn
	adapter := m.adapter
	m.mu.Unlock()

	if adapter != nil {
		if p, err := adapter.Peripheral(); err == nil {
			fn(p)
		}
	}
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.callbacks, id)
	}
}

func (m *Manager) run(ctx context.Context) {
	defer close(m.done)
	ticker := m.clock.NewTicker(watchInterval)
	defer ticker.Stop()

	m.tick(ctx)
	for {
		m.mu.Lock()
		current := m.adapter
		m.mu.Unlock()
		var gone <-chan struct{}
		var retry <-chan time.Time
		switch {
		case current != nil:
			gone = current.Gone()
			m.retries = 0
		case m.retries > 0:
			retry = m.clock.After(lossRetryInterval)
		}

		select {
		case <-ctx.Done():
			m.closeAdapter("stopping")
			return
		case <-ticker.Chan():
			m.tick(ctx)
		case <-retry:
			m.retries--
			m.tick(ctx)
		case <-gone:
			if m.lost() {
				m.tick(ctx)
			}
		}
	}
}

// lost closes an adapter that went away. One that had been working is worth
// getting back quickly, so it arms the quick retries; one that failed as
// soon as it was opened waits for the watch interval. It reports which.
func (m *Manager) lost() (retryNow bool) {
	m.mu.Lock()
	stable := m.clock.Since(m.openedAt) >= stableAfter
	m.flapping = !stable
	m.mu.Unlock()
	m.closeAdapter("adapter went away")
	if stable {
		m.retries = lossRetryAttempts
	}
	return stable
}

// tick reconciles the adapter with configuration: closed while disabled,
// re-opened after it went away, opened when it first becomes possible.
func (m *Manager) tick(ctx context.Context) {
	m.mu.Lock()
	unsupported := m.unsupported
	m.mu.Unlock()
	if unsupported {
		return
	}
	if !m.cfg.BLEEnabled() {
		m.closeAdapter("disabled by configuration")
		return
	}

	m.mu.Lock()
	current := m.adapter
	m.mu.Unlock()
	if current != nil {
		select {
		case <-current.Gone():
			m.lost()
		default:
			return
		}
	}

	adapter, err := m.open(ctx)
	if err != nil {
		if errors.Is(err, bluez.ErrUnsupported) {
			// Nothing will change that on this platform, and the transport
			// is on by default, so say nothing and stop asking.
			m.mu.Lock()
			m.unsupported = true
			m.mu.Unlock()
			return
		}
		m.reportUnavailable(err)
		return
	}

	peripheral, err := adapter.Peripheral()
	if err != nil {
		m.mu.Lock()
		reported := m.roleReported
		m.roleReported = true
		m.mu.Unlock()
		if !reported {
			log.Warn().Err(err).
				Str("adapter", adapter.Address()).
				Msg("bluetooth adapter cannot act as a peripheral, app transport unavailable")
		}
		_ = adapter.Close()
		return
	}

	m.mu.Lock()
	m.adapter = adapter
	m.openedAt = m.clock.Now()
	flapping := m.flapping
	m.unavailableReported = false
	m.roleReported = false
	callbacks := make([]PeripheralFunc, 0, len(m.callbacks))
	for _, fn := range m.callbacks {
		callbacks = append(callbacks, fn)
	}
	m.mu.Unlock()

	roles := make([]string, 0, len(adapter.Roles()))
	for _, r := range adapter.Roles() {
		roles = append(roles, string(r))
	}
	event := log.Info()
	if flapping {
		event = log.Debug()
	}
	event.Str("adapter", adapter.Address()).Strs("roles", roles).Msg("bluetooth adapter ready")

	for _, fn := range callbacks {
		fn(peripheral)
	}
}

// reportUnavailable logs the first failure at info and later ones at debug,
// so a machine without Bluetooth does not fill the log.
func (m *Manager) reportUnavailable(err error) {
	m.mu.Lock()
	reported := m.unavailableReported
	m.unavailableReported = true
	m.mu.Unlock()
	if reported {
		log.Debug().Err(err).Msg("bluetooth still unavailable")
		return
	}
	log.Info().Err(err).Msg("bluetooth unavailable, will keep checking")
}

func (m *Manager) closeAdapter(reason string) {
	m.mu.Lock()
	adapter := m.adapter
	m.adapter = nil
	flapping := m.flapping
	m.mu.Unlock()
	if adapter == nil {
		return
	}
	event := log.Info()
	if flapping {
		event = log.Debug()
	}
	event.Str("reason", reason).Msg("closing bluetooth adapter")
	if err := adapter.Close(); err != nil {
		log.Debug().Err(err).Msg("error closing bluetooth adapter")
	}
}
