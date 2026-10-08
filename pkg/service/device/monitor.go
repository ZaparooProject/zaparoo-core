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

// Package device keeps Core's view of the machine it runs on current while
// clients are connected, and tells them when it changes.
package device

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// defaultReadTimeout bounds one section's read. A battery on a flaky bus
	// or a filesystem on a dead network share can block for as long as it
	// likes.
	defaultReadTimeout = 2 * time.Second
	// statusBudget bounds how long a status request waits for readings it
	// does not have yet.
	statusBudget = 3 * time.Second
	// failuresBeforeClear is how many reads in a row must fail before a
	// section's last good reading is dropped. One bad read should not make a
	// battery icon blink.
	failuresBeforeClear = 2

	probeBackoffStart = time.Second
	probeInterval     = 5 * time.Minute
)

var errReadTimeout = errors.New("timed out reading device state")

type sectionID int

const (
	sectionPower sectionID = iota
	sectionNetwork
	sectionBluetooth
	sectionStorage
	sectionDisplay
	sectionControllers
	sectionSystem
	sectionCount
)

// sectionIntervals is how often each section is re-read while clients are
// connected. Zero means once. The second column is for hardware where every
// wake-up is felt.
var sectionIntervals = [sectionCount][2]time.Duration{
	sectionPower:       {30 * time.Second, time.Minute},
	sectionNetwork:     {5 * time.Second, 15 * time.Second},
	sectionBluetooth:   {30 * time.Second, time.Minute},
	sectionStorage:     {time.Minute, 5 * time.Minute},
	sectionDisplay:     {5 * time.Second, 15 * time.Second},
	sectionControllers: {5 * time.Second, 15 * time.Second},
	sectionSystem:      {0, 0},
}

type sectionState struct {
	// slot caps a reader that never returns at one stuck goroutine.
	slot        chan struct{}
	lastRead    time.Time
	interval    time.Duration
	failures    int
	read        bool
	due         bool
	unsupported bool
}

// Snapshot is everything Core knows about the device. A nil section has no
// reading.
type Snapshot struct {
	Power       *power.Detail
	Network     *hoststatus.Network
	Bluetooth   *hoststatus.Bluetooth
	Display     *hoststatus.Display
	System      *hoststatus.System
	Sections    map[string]hoststatus.Availability
	Timezone    string
	PlatformID  string
	Storage     []hoststatus.Volume
	Controllers []hoststatus.Controller
	UTCOffset   int
	// StorageKnown and ControllersKnown tell an empty list from no reading.
	StorageKnown     bool
	ControllersKnown bool
	ClockReliable    bool
}

// InternetProber decides whether the internet is reachable.
type InternetProber interface {
	Probe(ctx context.Context) hoststatus.InternetState
}

// Options configures a Monitor.
type Options struct {
	Platform platforms.Platform
	Config   *config.Instance
	Clock    clockwork.Clock
	// Publish delivers a notification to every connected client.
	Publish func(models.Notification)
	Prober  InternetProber
	// Readers are the per-section readers, already resolved against the
	// platform.
	Readers hoststatus.Readers
	// ReadTimeout bounds one section's read. Zero means the default.
	ReadTimeout time.Duration
}

// Monitor reads device state on a schedule while at least one client is
// connected and publishes device.changed when it differs from what clients
// were last told. With nobody connected it does nothing at all.
type Monitor struct {
	readers       hoststatus.Readers
	probeDue      time.Time
	clock         clockwork.Clock
	prober        InternetProber
	platform      platforms.Platform
	cfg           *config.Instance
	publish       func(models.Notification)
	wake          chan struct{}
	probeDone     chan probeResult
	probeCancel   context.CancelFunc
	internet      hoststatus.InternetState
	routeKey      string
	lastPublished []byte
	sections      [sectionCount]sectionState
	snap          Snapshot
	probeWG       sync.WaitGroup
	probeBackoff  time.Duration
	readTimeout   time.Duration
	clients       int
	mu            syncutil.RWMutex
	// collectMu serialises reads so the loop and a status request never read
	// the same hardware at once. It is always taken before mu.
	collectMu syncutil.Mutex
	// pushed means the platform announces changes itself, so sections are
	// only re-read when it says so.
	pushed  bool
	probing bool
}

type probeResult struct {
	routeKey string
	state    hoststatus.InternetState
}

// NewMonitor builds a monitor. It reads nothing until Run is called and a
// client connects, or a status is asked for.
func NewMonitor(opts *Options) *Monitor {
	monitor := &Monitor{
		clock:        opts.Clock,
		prober:       opts.Prober,
		platform:     opts.Platform,
		cfg:          opts.Config,
		publish:      opts.Publish,
		readers:      opts.Readers,
		wake:         make(chan struct{}, 1),
		probeDone:    make(chan probeResult, 1),
		probeBackoff: probeBackoffStart,
		readTimeout:  opts.ReadTimeout,
	}
	if monitor.readTimeout <= 0 {
		monitor.readTimeout = defaultReadTimeout
	}
	if monitor.clock == nil {
		monitor.clock = clockwork.NewRealClock()
	}
	if monitor.readers.PowerControl == nil {
		monitor.readers.PowerControl = hoststatus.NoPowerControl{}
	}
	_, monitor.pushed = opts.Platform.(platforms.DeviceStatusPusher)

	column := 0
	if opts.Platform.Settings().ResourceConstrained {
		column = 1
	}
	for id := range monitor.sections {
		section := &monitor.sections[id]
		section.slot = make(chan struct{}, 1)
		section.due = true
		if !monitor.pushed {
			section.interval = sectionIntervals[id][column]
		}
	}
	return monitor
}

// PowerControl returns the controller for power actions on this device.
func (m *Monitor) PowerControl() hoststatus.PowerController {
	return m.readers.PowerControl
}

// ClientConnected records that a client is now listening for notifications.
func (m *Monitor) ClientConnected() {
	m.mu.Lock()
	m.clients++
	first := m.clients == 1
	if first {
		// Whatever was read while nobody was listening may be long stale.
		for id := range m.sections {
			m.sections[id].due = true
		}
		m.probeDue = time.Time{}
	}
	m.mu.Unlock()
	if first {
		m.signal()
	}
}

// ClientDisconnected records that a client has gone.
func (m *Monitor) ClientDisconnected() {
	m.mu.Lock()
	if m.clients > 0 {
		m.clients--
	}
	m.mu.Unlock()
	m.signal()
}

func (m *Monitor) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Monitor) clientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clients
}

// markAllDue is the platform saying something changed.
func (m *Monitor) markAllDue() {
	m.mu.Lock()
	for id := range m.sections {
		m.sections[id].due = true
	}
	m.mu.Unlock()
	m.signal()
}

// Run drives the monitor until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	if pusher, ok := m.platform.(platforms.DeviceStatusPusher); ok {
		pusher.SetDeviceStatusChanged(m.markAllDue)
		defer pusher.SetDeviceStatusChanged(nil)
	}
	defer func() {
		m.stopProbe()
		m.probeWG.Wait()
	}()

	for {
		if m.clientCount() == 0 {
			m.stopProbe()
			select {
			case <-m.wake:
				continue
			case <-m.probeDone:
				continue
			case <-ctx.Done():
				return
			}
		}

		m.collect()
		m.startProbeIfDue(ctx)
		m.publishIfChanged()

		var timeout <-chan time.Time
		var timer clockwork.Timer
		if delay, ok := m.nextDelay(); ok {
			timer = m.clock.NewTimer(delay)
			timeout = timer.Chan()
		}
		select {
		case <-timeout:
		case <-m.wake:
		case result := <-m.probeDone:
			m.finishProbe(result)
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// nextDelay is how long until something next needs doing. The second result
// is false when nothing is scheduled and only a wake-up will do.
func (m *Monitor) nextDelay() (time.Duration, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := m.clock.Now()
	var (
		next  time.Time
		found bool
	)
	consider := func(at time.Time) {
		if !found || at.Before(next) {
			next, found = at, true
		}
	}
	for id := range m.sections {
		section := &m.sections[id]
		switch {
		case section.due:
			consider(now)
		case section.interval > 0 && !section.unsupported:
			consider(section.lastRead.Add(section.interval))
		}
	}
	if m.needsProbeLocked() && !m.probing {
		consider(m.probeDue)
	}
	if !found {
		return 0, false
	}
	delay := next.Sub(now)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

// guarded runs read with a timeout, never leaving more than one stuck read
// behind for a section. The timeout is wall-clock time on purpose: it guards
// against hardware that does not answer, which no schedule decides.
func guarded[T any](timeout time.Duration, slot chan struct{}, read func() (T, error)) (T, error) {
	var zero T
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case slot <- struct{}{}:
	case <-timer.C:
		return zero, errReadTimeout
	}

	type result struct {
		err   error
		value T
	}
	done := make(chan result, 1)
	go func() {
		value, err := read()
		<-slot
		done <- result{value: value, err: err}
	}()

	select {
	case res := <-done:
		return res.value, res.err
	case <-timer.C:
		return zero, errReadTimeout
	}
}

// readSection reads one section and records the outcome. set is called with
// the new reading, or with nil to drop the old one.
func readSection[T any](m *Monitor, id sectionID, read func() (T, error), set func(*T)) {
	section := &m.sections[id]

	m.mu.Lock()
	wanted := section.due || !section.read ||
		(section.interval > 0 && !section.unsupported &&
			!m.clock.Now().Before(section.lastRead.Add(section.interval)))
	// Cleared before the read, not after: a change announced while the read
	// is under way must leave the section due again.
	section.due = false
	m.mu.Unlock()
	if !wanted {
		return
	}

	var (
		value T
		err   = hoststatus.ErrUnsupported
	)
	if read != nil {
		value, err = guarded(m.readTimeout, section.slot, read)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	section.read = true
	section.lastRead = m.clock.Now()
	switch {
	case err == nil:
		section.failures = 0
		section.unsupported = false
		set(&value)
	case errors.Is(err, hoststatus.ErrUnsupported) || errors.Is(err, power.ErrDetailUnsupported):
		section.unsupported = true
		set(nil)
	default:
		section.unsupported = false
		section.failures++
		log.Debug().Err(err).Int("section", int(id)).Msg("could not read device state")
		if section.failures >= failuresBeforeClear {
			set(nil)
		}
	}
}

// collect reads every section that is due.
func (m *Monitor) collect() {
	m.collectMu.Lock()
	defer m.collectMu.Unlock()

	readSection(m, sectionPower, m.readers.Power, func(v *power.Detail) { m.snap.Power = v })
	readSection(m, sectionNetwork, m.readers.Network, func(v *hoststatus.Network) { m.snap.Network = v })
	readSection(m, sectionBluetooth, m.readers.Bluetooth, func(v *hoststatus.Bluetooth) { m.snap.Bluetooth = v })
	readSection(m, sectionDisplay, m.readers.Display, func(v *hoststatus.Display) { m.snap.Display = v })
	readSection(m, sectionSystem, m.readers.System, func(v *hoststatus.System) { m.snap.System = v })

	var readControllers func() ([]hoststatus.Controller, error)
	if m.readers.Controllers != nil {
		readControllers = m.readers.Controllers
	}
	readSection(m, sectionControllers, readControllers, func(v *[]hoststatus.Controller) {
		m.snap.ControllersKnown = v != nil
		m.snap.Controllers = nil
		if v != nil {
			m.snap.Controllers = *v
		}
	})

	var readStorage func() ([]hoststatus.Volume, error)
	if m.readers.Storage != nil {
		readStorage = func() ([]hoststatus.Volume, error) { return m.readers.Storage(m.storageRoots()) }
	}
	readSection(m, sectionStorage, readStorage, func(v *[]hoststatus.Volume) {
		m.snap.StorageKnown = v != nil
		m.snap.Storage = nil
		if v != nil {
			m.snap.Storage = *v
		}
	})

	m.afterNetworkRead()
}

func (m *Monitor) storageRoots() []hoststatus.StorageRoot {
	dirs := m.platform.RootDirs(m.cfg)
	roots := make([]hoststatus.StorageRoot, 0, len(dirs)+1)
	for _, dir := range dirs {
		roots = append(roots, hoststatus.StorageRoot{Path: dir, Role: hoststatus.RoleMedia})
	}
	if dataDir := helpers.DataDir(m.platform); dataDir != "" {
		roots = append(roots, hoststatus.StorageRoot{Path: dataDir, Role: hoststatus.RoleData})
	}
	return roots
}

// afterNetworkRead settles what the network reading means for reachability:
// either the reader already knows, or a probe has to find out.
func (m *Monitor) afterNetworkRead() {
	m.mu.Lock()
	defer m.mu.Unlock()

	network := m.snap.Network
	if network == nil {
		m.internet = ""
		m.routeKey = ""
		return
	}
	if network.InternetAuthoritative {
		m.internet = network.Internet
		m.routeKey = ""
		return
	}
	key := string(network.Type) + "|" + network.Interface
	if key != m.routeKey {
		// A different link is a different answer: forget the old one and ask
		// again straight away.
		m.routeKey = key
		m.internet = ""
		m.probeDue = m.clock.Now()
		m.probeBackoff = probeBackoffStart
	}
}

// needsProbeLocked reports whether reachability is Core's to find out.
func (m *Monitor) needsProbeLocked() bool {
	network := m.snap.Network
	if m.cfg != nil && !m.cfg.InternetCheckEnabled() {
		return false
	}
	return m.prober != nil && network != nil && !network.InternetAuthoritative &&
		network.Type != hoststatus.LinkNone
}

func (m *Monitor) startProbeIfDue(ctx context.Context) {
	m.mu.Lock()
	if m.probing || !m.needsProbeLocked() || m.clock.Now().Before(m.probeDue) {
		m.mu.Unlock()
		return
	}
	m.probing = true
	probeCtx, cancel := context.WithCancel(ctx)
	m.probeCancel = cancel
	key := m.routeKey
	m.mu.Unlock()

	m.probeWG.Add(1)
	go func() {
		defer m.probeWG.Done()
		defer cancel()
		state := m.prober.Probe(probeCtx)
		select {
		case m.probeDone <- probeResult{routeKey: key, state: state}:
		case <-probeCtx.Done():
		}
	}()
}

func (m *Monitor) stopProbe() {
	m.mu.Lock()
	cancel := m.probeCancel
	m.probeCancel = nil
	m.probing = false
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Monitor) finishProbe(result probeResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.probing = false
	m.probeCancel = nil
	if result.routeKey != m.routeKey || result.state == "" {
		// The link changed underneath the probe, or it was cut short. Either
		// way the next pass asks again.
		return
	}
	m.applyProbeLocked(result.state)
}

func (m *Monitor) applyProbeLocked(state hoststatus.InternetState) {
	m.internet = state
	now := m.clock.Now()
	if state == hoststatus.InternetFull {
		m.probeBackoff = probeBackoffStart
		m.probeDue = now.Add(probeInterval)
		return
	}
	m.probeDue = now.Add(m.probeBackoff)
	m.probeBackoff *= 2
	if m.probeBackoff > probeInterval {
		m.probeBackoff = probeInterval
	}
}

// Snapshot returns the device's current state, reading whatever is missing
// or stale first. It is what answers a status request, including one from a
// caller that holds no connection for the monitor to be running for.
func (m *Monitor) Snapshot(ctx context.Context) Snapshot {
	ctx, cancel := context.WithTimeout(ctx, statusBudget)
	defer cancel()

	m.collect()
	if m.clientCount() == 0 {
		m.probeOnce(ctx)
	}
	return m.current()
}

// probeOnce answers reachability for a caller the loop is not running for.
func (m *Monitor) probeOnce(ctx context.Context) {
	m.mu.Lock()
	wanted := m.needsProbeLocked() && !m.probing && !m.clock.Now().Before(m.probeDue)
	key := m.routeKey
	m.mu.Unlock()
	if !wanted {
		return
	}
	state := m.prober.Probe(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	if state != "" && key == m.routeKey {
		m.applyProbeLocked(state)
	}
}

func (m *Monitor) current() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := m.snap
	if snap.Network != nil {
		network := *snap.Network
		network.Internet = m.internet
		snap.Network = &network
	}
	snap.PlatformID = m.platform.ID()

	now := m.clock.Now()
	snap.ClockReliable = helpers.IsClockReliable(now)
	snap.Timezone, snap.UTCOffset = localZone(now)

	names := [sectionCount]string{
		sectionPower:       models.DeviceSectionPower,
		sectionNetwork:     models.DeviceSectionNetwork,
		sectionBluetooth:   models.DeviceSectionBluetooth,
		sectionStorage:     models.DeviceSectionStorage,
		sectionDisplay:     models.DeviceSectionDisplay,
		sectionControllers: models.DeviceSectionControllers,
		sectionSystem:      models.DeviceSectionSystem,
	}
	snap.Sections = make(map[string]hoststatus.Availability, int(sectionCount)+1)
	for id := range m.sections {
		availability := hoststatus.Supported
		if m.sections[id].unsupported {
			availability = hoststatus.Unsupported
		}
		snap.Sections[names[id]] = availability
	}
	snap.Sections[models.DeviceSectionTime] = hoststatus.Supported
	return snap
}

func (m *Monitor) publishIfChanged() {
	snap := m.current()
	payload, err := json.Marshal(ChangedParams(&snap))
	if err != nil {
		log.Error().Err(err).Msg("could not encode device state")
		return
	}

	m.mu.Lock()
	changed := !bytes.Equal(payload, m.lastPublished)
	if changed {
		m.lastPublished = payload
	}
	m.mu.Unlock()

	if changed && m.publish != nil {
		m.publish(models.Notification{Method: models.NotificationDeviceChanged, Params: payload})
	}
}
