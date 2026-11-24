//go:build linux && !android

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

package sim

import (
	"sort"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// AllLaunchers is the fault key that applies to every launcher.
const AllLaunchers = "*"

// Fault is a misbehaviour injected into a launcher. A fault set for one
// launcher overrides the one set for AllLaunchers field by field.
type Fault struct {
	Launcher string `json:"launcher"`
	// FailLaunches makes the next N launches fail.
	FailLaunches int `json:"failLaunches,omitempty"`
	// LaunchDelayMS delays each launch.
	LaunchDelayMS int `json:"launchDelayMs,omitempty"`
	// CrashAfterMS kills the launched child process after this long.
	CrashAfterMS int `json:"crashAfterMs,omitempty"`
	// ExitAfterMS makes the child exit by itself after this long, as media
	// that reached its end.
	ExitAfterMS int `json:"exitAfterMs,omitempty"`
	// FailStops makes the next N stops fail with ErrStopFailed, leaving the
	// media running.
	FailStops int `json:"failStops,omitempty"`
	// FailPreflight makes Preflight fail.
	FailPreflight bool `json:"failPreflight,omitempty"`
	// ReadyTimeout makes WaitForReady block until its context ends.
	ReadyTimeout bool `json:"readyTimeout,omitempty"`
	// NotRunning makes GameRunning report that no game is running.
	NotRunning bool `json:"notRunning,omitempty"`
	// Unavailable makes Availability fail.
	Unavailable bool `json:"unavailable,omitempty"`
	// FailKill makes a custom Kill fail, so the signal fallback runs.
	FailKill bool `json:"failKill,omitempty"`
}

// Faults holds the active faults.
type Faults struct {
	byLauncher map[string]Fault
	mu         syncutil.Mutex
}

// NewFaults returns an empty fault set.
func NewFaults() *Faults {
	return &Faults{byLauncher: map[string]Fault{}}
}

// Set replaces the fault for f.Launcher (AllLaunchers when empty).
func (f *Faults) Set(fault Fault) {
	if fault.Launcher == "" {
		fault.Launcher = AllLaunchers
	}
	f.mu.Lock()
	f.byLauncher[fault.Launcher] = fault
	f.mu.Unlock()
}

// Clear drops the fault for a launcher, or every fault when id is empty.
func (f *Faults) Clear(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id == "" {
		f.byLauncher = map[string]Fault{}
		return
	}
	delete(f.byLauncher, id)
}

// List returns the active faults sorted by launcher.
func (f *Faults) List() []Fault {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Fault, 0, len(f.byLauncher))
	for _, v := range f.byLauncher {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Launcher < out[j].Launcher })
	return out
}

// For returns the effective fault for a launcher.
func (f *Faults) For(id string) Fault {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.effectiveLocked(id)
}

func (f *Faults) effectiveLocked(id string) Fault {
	all := f.byLauncher[AllLaunchers]
	one, ok := f.byLauncher[id]
	if !ok {
		all.Launcher = id
		return all
	}
	if one.LaunchDelayMS == 0 {
		one.LaunchDelayMS = all.LaunchDelayMS
	}
	if one.CrashAfterMS == 0 {
		one.CrashAfterMS = all.CrashAfterMS
	}
	if one.ExitAfterMS == 0 {
		one.ExitAfterMS = all.ExitAfterMS
	}
	one.FailPreflight = one.FailPreflight || all.FailPreflight
	one.ReadyTimeout = one.ReadyTimeout || all.ReadyTimeout
	one.NotRunning = one.NotRunning || all.NotRunning
	one.Unavailable = one.Unavailable || all.Unavailable
	one.FailKill = one.FailKill || all.FailKill
	return one
}

// takeCounter consumes one from a counted fault, preferring the launcher's
// own entry, and reports whether it fired.
func (f *Faults) takeCounter(id string, get func(*Fault) *int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, key := range []string{id, AllLaunchers} {
		v, ok := f.byLauncher[key]
		if !ok {
			continue
		}
		n := get(&v)
		if *n > 0 {
			*n--
			f.byLauncher[key] = v
			return true
		}
	}
	return false
}

// TakeLaunchFailure consumes one queued launch failure.
func (f *Faults) TakeLaunchFailure(id string) bool {
	return f.takeCounter(id, func(v *Fault) *int { return &v.FailLaunches })
}

// TakeStopFailure consumes one queued stop failure.
func (f *Faults) TakeStopFailure(id string) bool {
	return f.takeCounter(id, func(v *Fault) *int { return &v.FailStops })
}

func ms(n int) time.Duration {
	return time.Duration(n) * time.Millisecond
}
