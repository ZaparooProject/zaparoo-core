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
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserialble"
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

	// maxFailures is how many failed attempts in a row the manager makes
	// before it stops trying. An adapter that is missing or switched off
	// is not a failure; one that is there and will not work is.
	maxFailures = 5
)

// roleCentralPeripheral is the role BlueZ reports for a controller that can
// scan for and hold a reader connection while it advertises.
const roleCentralPeripheral = ble.RoleCentral + "-" + ble.RolePeripheral

// PeripheralFunc is called with a ready peripheral each time one becomes
// available. It must not block: start any long-running work on a goroutine.
type PeripheralFunc func(ble.Peripheral)

// Manager keeps an adapter open while the BLE transport is enabled and
// hands its peripheral side to whoever registered interest.
type Manager struct {
	cfg        *config.Instance
	clock      clockwork.Clock
	open       func(ctx context.Context) (ble.Adapter, error)
	adapter    ble.Adapter
	peripheral ble.Peripheral
	cancel     context.CancelFunc
	done       chan struct{}
	callbacks  map[int]PeripheralFunc
	openedAt   time.Time
	mu         syncutil.Mutex
	nextID     int
	// retries is how many quick attempts are left to get back an adapter
	// that was lost. Only the watch loop touches it.
	retries int
	// failures counts attempts in a row that found an adapter and could
	// not use it.
	failures int
	// gaveUp is set after maxFailures. The manager then only checks
	// whether the adapter went away or the setting changed, either of
	// which earns a fresh start.
	gaveUp bool
	// flapping is set while the adapter keeps opening and failing straight
	// away, so that cycle is logged once and not every interval.
	flapping bool
	// absentReported, unavailableReported, roleReported and
	// readerReported keep a machine that has no usable adapter from
	// logging the same line every interval.
	absentReported      bool
	unavailableReported bool
	roleReported        bool
	readerReported      bool
	// waitingForRadio is set when an open adapter turned out to have no
	// usable radio behind it, and cleared once one stays up. Opening such
	// an adapter again proves nothing, so until then it does not count as
	// the radio being back.
	waitingForRadio bool
	// advertisingPaused is applied to every peripheral handed out.
	advertisingPaused bool
	started           bool
	stopped           bool
	// unsupported is set once the platform says it has no BlueZ at all.
	unsupported bool
}

// NewManager builds a manager over the system's Bluetooth stack. It never
// powers the adapter on: the radio may be off on purpose, and switching it
// on would disturb the device's gamepads.
func NewManager(cfg *config.Instance) *Manager {
	return newManagerWith(cfg, clockwork.NewRealClock(), func(ctx context.Context) (ble.Adapter, error) {
		return ble.Open(ctx)
	})
}

func newManagerWith(
	cfg *config.Instance,
	clock clockwork.Clock,
	open func(ctx context.Context) (ble.Adapter, error),
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
func (m *Manager) Roles() []ble.Role {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.adapter == nil {
		return nil
	}
	return m.adapter.Roles()
}

// Serving reports whether a peripheral is open and handed out.
func (m *Manager) Serving() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peripheral != nil
}

// SetAdvertisingPaused stops or resumes advertising on the current
// peripheral and any later one, without dropping connected clients. Core
// pauses it while the user scans for or pairs a controller, so the radio
// is free for that.
func (m *Manager) SetAdvertisingPaused(paused bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.advertisingPaused == paused {
		return
	}
	m.advertisingPaused = paused
	if m.peripheral != nil {
		m.peripheral.SetAdvertising(!paused)
	}
	log.Debug().Bool("paused", paused).Msg("bluetooth advertising pause changed")
}

// OnPeripheral registers fn to receive the peripheral now, if one is ready,
// and again after every re-open. The returned function unregisters it.
func (m *Manager) OnPeripheral(fn PeripheralFunc) func() {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.callbacks[id] = fn
	p := m.peripheral
	m.mu.Unlock()

	if p != nil {
		fn(p)
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
			if retryNow, _ := m.lost(); retryNow {
				m.tick(ctx)
			}
		}
	}
}

// lost closes an adapter that went away. One that had been working is worth
// getting back quickly, so it arms the quick retries; one that failed as
// soon as it was opened counts as a failure and waits for the watch
// interval. One that says it has no usable radio is neither: like an adapter
// that is absent when opened, it is waited for at the watch interval for as
// long as it takes. It reports whether to try again at once, and whether the
// radio is what was missing.
func (m *Manager) lost() (retryNow, noRadio bool) {
	m.mu.Lock()
	cause := goneCause(m.adapter)
	noRadio = radioUnusable(cause)
	stable := m.clock.Since(m.openedAt) >= stableAfter
	// The line that reports the missing radio covers the opening and
	// closing that found it out, so those stay quiet too.
	m.flapping = !stable || noRadio
	if stable {
		m.failures = 0
	}
	switch {
	case noRadio:
		m.waitingForRadio = true
	case stable:
		m.waitingForRadio = false
		m.absentReported = false
	}
	m.mu.Unlock()
	m.closeAdapter("adapter went away")
	switch {
	case noRadio:
		m.retries = 0
		m.resetFailures()
		m.reportAbsent(cause)
		return false, true
	case stable:
		m.retries = lossRetryAttempts
	default:
		m.failed(errors.New("adapter went away straight after opening"))
	}
	return stable, false
}

// goneCause returns why an adapter went away, where it can say.
func goneCause(adapter ble.Adapter) error {
	gc, ok := adapter.(ble.GoneCauser)
	if !ok {
		return nil
	}
	return gc.GoneCause() //nolint:wrapcheck // passed on unchanged to be classified
}

// failed counts one failed attempt and gives up after maxFailures.
func (m *Manager) failed(err error) {
	m.mu.Lock()
	m.failures++
	attempts := m.failures
	giveUp := attempts >= maxFailures && !m.gaveUp
	if giveUp {
		m.gaveUp = true
	}
	m.mu.Unlock()
	if !giveUp {
		return
	}
	m.retries = 0
	log.Warn().Err(err).Int("attempts", attempts).
		Msg("bluetooth app transport keeps failing; giving up until the adapter or the setting changes")
}

// resetFailures gives the manager a fresh start.
func (m *Manager) resetFailures() {
	m.mu.Lock()
	wasGivenUp := m.gaveUp
	m.failures = 0
	m.gaveUp = false
	m.mu.Unlock()
	if wasGivenUp {
		log.Info().Msg("bluetooth adapter or setting changed, trying the app transport again")
	}
}

func (m *Manager) givenUp() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gaveUp
}

// absent reports whether err means there is simply no adapter to use: none
// plugged in, switched off, or no Bluetooth service running.
func absent(err error) bool {
	return errors.Is(err, ble.ErrPoweredOff) ||
		errors.Is(err, ble.ErrNoAdapter) ||
		errors.Is(err, ble.ErrUnavailable)
}

// radioUnusable reports whether an adapter that opened was lost because it
// has no radio that can serve, and not because something broke. That is the
// absent errors and, here only, an unsupported role. An adapter that reports
// its cause is one that could not say what its radio does when it was
// opened: Windows reports a radio that is switched off and one that cannot
// advertise the same way, and opening it again would never show that the
// radio had been swapped for a capable one, so giving up on it would last
// until Core restarts. It is checked at the watch interval instead. An
// adapter that knows its roles when opened still counts a missing one as a
// failure in tick.
func radioUnusable(cause error) bool {
	return absent(cause) || errors.Is(cause, ble.ErrRoleUnsupported)
}

// readerConfigured reports whether a Bluetooth LE reader is set up to
// connect.
func (m *Manager) readerConfigured() bool {
	ids := []string{simpleserialble.DriverID}
	info := config.DriverInfo{ID: simpleserialble.DriverID, DefaultEnabled: true}
	if !m.cfg.IsReaderEnabled(info, config.ReaderEnableContextManualConnect) {
		return false
	}
	for _, c := range m.cfg.Readers().Connect {
		if c.IsEnabled() && readers.MatchesDriverID(ids, c.Driver) {
			return true
		}
	}
	return false
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
		m.resetFailures()
		return
	}

	m.mu.Lock()
	current := m.adapter
	openedAt := m.openedAt
	m.mu.Unlock()
	if current != nil {
		select {
		case <-current.Gone():
			if _, noRadio := m.lost(); noRadio {
				// Opening again now would only find the same; the next
				// interval looks.
				return
			}
		default:
			if m.readerConfigured() && !ble.SupportsRole(current.Roles(), roleCentralPeripheral) {
				// A reader added while the transport was serving takes the
				// adapter over; the next pass logs why.
				m.closeAdapter("bluetooth reader configured")
				return
			}
			if m.clock.Since(openedAt) >= stableAfter {
				m.mu.Lock()
				m.failures = 0
				radioBack := m.waitingForRadio
				m.waitingForRadio = false
				if radioBack {
					m.absentReported = false
				}
				m.mu.Unlock()
				if radioBack {
					log.Info().Str("adapter", current.Address()).
						Msg("bluetooth is usable again, app transport running")
				}
			}
			return
		}
	}

	adapter, err := m.open(ctx)
	if err != nil {
		switch {
		case errors.Is(err, ble.ErrUnsupported):
			// Nothing will change that on this platform, and the transport
			// is on by default, so say nothing and stop asking.
			m.mu.Lock()
			m.unsupported = true
			m.mu.Unlock()
		case absent(err):
			m.resetFailures()
			m.reportAbsent(err)
		default:
			if !m.givenUp() {
				m.reportUnavailable(err)
				m.failed(err)
			}
		}
		return
	}
	m.mu.Lock()
	if !m.waitingForRadio {
		m.absentReported = false
	}
	m.mu.Unlock()

	if m.givenUp() {
		// A given-up manager still opens the adapter every interval, only
		// to look. Where opening reads the adapter's properties, a radio
		// that went away and came back shows up above as absent, which
		// earns a fresh start. An adapter that opens with no radio behind
		// it never shows that here, so giving up on one lasts until the
		// setting changes; its missing radio is caught in lost instead and
		// does not count towards giving up.
		_ = adapter.Close()
		return
	}

	if m.readerConfigured() && !ble.SupportsRole(adapter.Roles(), roleCentralPeripheral) {
		m.mu.Lock()
		reported := m.readerReported
		m.readerReported = true
		m.mu.Unlock()
		if !reported {
			log.Info().Str("adapter", adapter.Address()).Strs("roles", roleStrings(adapter)).
				Msg("bluetooth reader configured and the adapter cannot run it alongside the app " +
					"transport; leaving the app transport off")
		}
		_ = adapter.Close()
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
		m.failed(err)
		return
	}

	reloadKeys(ctx, adapter)

	m.mu.Lock()
	m.adapter = adapter
	m.peripheral = peripheral
	m.openedAt = m.clock.Now()
	flapping := m.flapping
	m.unavailableReported = false
	m.roleReported = false
	m.readerReported = false
	peripheral.SetAdvertising(!m.advertisingPaused)
	callbacks := make([]PeripheralFunc, 0, len(m.callbacks))
	for _, fn := range m.callbacks {
		callbacks = append(callbacks, fn)
	}
	m.mu.Unlock()

	event := log.Info()
	if flapping {
		event = log.Debug()
	}
	event.Str("adapter", adapter.Address()).Strs("roles", roleStrings(adapter)).Msg("bluetooth adapter ready")

	for _, fn := range callbacks {
		fn(peripheral)
	}
}

// reloadKeys hands the controller the long-term keys of bonded devices
// where the platform supports it, so a phone bonded earlier can reconnect
// even when bluetoothd failed to load them at boot.
func reloadKeys(ctx context.Context, adapter ble.Adapter) {
	kr, ok := adapter.(ble.KeyReloader)
	if !ok {
		return
	}
	n, err := kr.ReloadLongTermKeys(ctx)
	switch {
	case errors.Is(err, os.ErrPermission):
		log.Debug().Err(err).Msg("no permission to reload bluetooth long-term keys")
	case err != nil:
		log.Warn().Err(err).Str("adapter", adapter.Address()).Msg("failed to reload bluetooth long-term keys")
	case n > 0:
		log.Info().Int("keys", n).Str("adapter", adapter.Address()).Msg("reloaded bluetooth long-term keys")
	}
}

func roleStrings(adapter ble.Adapter) []string {
	roles := make([]string, 0, len(adapter.Roles()))
	for _, r := range adapter.Roles() {
		roles = append(roles, string(r))
	}
	return roles
}

// reportAbsent logs once that there is no adapter to use, until one shows
// up again.
func (m *Manager) reportAbsent(err error) {
	m.mu.Lock()
	reported := m.absentReported
	m.absentReported = true
	m.mu.Unlock()
	if reported {
		log.Debug().Err(err).Msg("bluetooth still unavailable")
		return
	}
	if errors.Is(err, ble.ErrPoweredOff) {
		log.Info().Msg("bluetooth is off; Core leaves it off and will use it once it is switched on")
		return
	}
	log.Info().Err(err).Msg("bluetooth unavailable, will keep checking")
}

// reportUnavailable logs the first failure at info and later ones at debug,
// so a machine with a broken adapter does not fill the log.
func (m *Manager) reportUnavailable(err error) {
	m.mu.Lock()
	reported := m.unavailableReported
	m.unavailableReported = true
	m.mu.Unlock()
	if reported {
		log.Debug().Err(err).Msg("bluetooth adapter still failing to open")
		return
	}
	log.Info().Err(err).Msg("bluetooth adapter failed to open, will retry")
}

func (m *Manager) closeAdapter(reason string) {
	m.mu.Lock()
	adapter := m.adapter
	m.adapter = nil
	m.peripheral = nil
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
