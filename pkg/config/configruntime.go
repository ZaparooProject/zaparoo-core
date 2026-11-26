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

package config

import (
	"maps"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// ChangeKind names a group of settings whose consumer captured the values
// when it started, and so has to be told to start again from the config for
// a change to apply without restarting Core.
type ChangeKind string

const (
	// ChangeDiscovery covers service.discovery: the mDNS record.
	ChangeDiscovery ChangeKind = "discovery"
	// ChangeBLEName covers service.ble.name: the name being advertised.
	ChangeBLEName ChangeKind = "ble_name"
	// ChangePublishers covers service.publishers.
	ChangePublishers ChangeKind = "publishers"
	// ChangeScanHistory covers readers.scan_history: a retention run applies it.
	ChangeScanHistory ChangeKind = "scan_history"
	// ChangeIgnoreOnConnect is readers.scan.ignore_on_connect being turned
	// on: readers already connected then are past their connect scan.
	ChangeIgnoreOnConnect ChangeKind = "ignore_on_connect"
)

// Settings fields reported as waiting for a restart when their value no
// longer matches what the running Core started with.
const (
	RestartGamepadEnabled = "inputGamepadEnabled"
	RestartErrorReporting = "errorReporting"
)

// runtimeState is what the running service knows about the config that the
// file does not hold. Load leaves it alone.
type runtimeState struct {
	listeners   map[ChangeKind][]func()
	startValues map[string]startValue
	mu          syncutil.Mutex
}

// startValue is a setting as the running Core started with it, and how to
// read its current value.
type startValue struct {
	current func(*Instance) bool
	atStart bool
}

// OnChange registers fn to run when a client changes settings of the given
// kind. Listeners run on the caller of NotifyChanged, after the change is
// saved and outside every config lock, so they may read the config.
func (c *Instance) OnChange(kind ChangeKind, fn func()) {
	c.runtime.mu.Lock()
	defer c.runtime.mu.Unlock()
	if c.runtime.listeners == nil {
		c.runtime.listeners = make(map[ChangeKind][]func())
	}
	c.runtime.listeners[kind] = append(c.runtime.listeners[kind], fn)
}

// NotifyChanged runs the listeners for each kind once, in the order given.
// A kind with no listener, such as discovery on a host that advertises the
// record itself, is skipped.
func (c *Instance) NotifyChanged(kinds ...ChangeKind) {
	seen := make([]ChangeKind, 0, len(kinds))
	for _, kind := range kinds {
		if slices.Contains(seen, kind) {
			continue
		}
		seen = append(seen, kind)
		c.runtime.mu.Lock()
		fns := slices.Clone(c.runtime.listeners[kind])
		c.runtime.mu.Unlock()
		log.Debug().Str("kind", string(kind)).Int("listeners", len(fns)).Msg("applying settings change")
		for _, fn := range fns {
			fn()
		}
	}
}

func (c *Instance) recordStartValue(field string, atStart bool, current func(*Instance) bool) {
	c.runtime.mu.Lock()
	defer c.runtime.mu.Unlock()
	if c.runtime.startValues == nil {
		c.runtime.startValues = make(map[string]startValue)
	}
	c.runtime.startValues[field] = startValue{atStart: atStart, current: current}
}

// RecordGamepadAtStart records whether the virtual gamepad was created when
// the platform started, with the default that platform applies, so
// RestartRequired can tell when input.gamepad_enabled has moved away from
// the running device.
func (c *Instance) RecordGamepadAtStart(defaultEnabled, enabled bool) {
	c.recordStartValue(RestartGamepadEnabled, enabled, func(c *Instance) bool {
		return c.VirtualGamepadEnabled(defaultEnabled)
	})
}

// RecordErrorReportingAtStart records whether error reporting was set up
// when Core started. It is only set up then.
func (c *Instance) RecordErrorReportingAtStart(enabled bool) {
	c.recordStartValue(RestartErrorReporting, enabled, (*Instance).ErrorReporting)
}

// RestartRequired lists the settings fields whose current value only takes
// effect after Core restarts. A value set back to what Core started with
// drops off the list, and the list is empty after a restart.
func (c *Instance) RestartRequired() []string {
	c.runtime.mu.Lock()
	values := maps.Clone(c.runtime.startValues)
	c.runtime.mu.Unlock()

	fields := make([]string, 0, len(values))
	for field, value := range values {
		if value.current(c) != value.atStart {
			fields = append(fields, field)
		}
	}
	slices.Sort(fields)
	return fields
}
