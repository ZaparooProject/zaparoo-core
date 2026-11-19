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

package serialport

import (
	"slices"

	"github.com/rs/zerolog/log"
)

// USBPort is a USB serial device node.
type USBPort struct {
	// Path is the device node, such as /dev/ttyUSB0.
	Path string
	// VIDPID is the USB vendor and product ID as "VVVV:PPPP" in upper case,
	// or empty when it could not be read.
	VIDPID string
}

// ProbeGuard keeps auto-detection from disturbing serial ports that turn out
// not to be readers. A port another process has open is reported busy and
// must not be probed. Every other port has its line settings saved, and
// Release puts them back unless the probe found a reader there.
//
// The zero value guards nothing and is safe to use.
type ProbeGuard struct {
	busy  map[string]struct{}
	saved map[string]*lineState
}

// StaticProbeGuard returns a guard that reports the given ports busy and
// saves no settings, for tests of code that takes a guard.
func StaticProbeGuard(busy ...string) *ProbeGuard {
	guard := &ProbeGuard{busy: make(map[string]struct{}, len(busy))}
	for _, path := range busy {
		guard.busy[path] = struct{}{}
	}
	return guard
}

// Busy reports whether another process has the port open.
func (g *ProbeGuard) Busy(path string) bool {
	if g == nil {
		return false
	}
	_, ok := g.busy[path]
	return ok
}

// BusyPaths returns the guarded ports another process has open.
func (g *ProbeGuard) BusyPaths() []string {
	if g == nil {
		return nil
	}
	paths := make([]string, 0, len(g.busy))
	for path := range g.busy {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

// Release restores the saved line settings of every guarded port except
// those in keep, which are the ports a reader was found on, and lets go of
// all of them. The guard must not be used afterwards.
func (g *ProbeGuard) Release(keep ...string) {
	if g == nil {
		return
	}
	for path, state := range g.saved {
		if slices.Contains(keep, path) {
			state.discard()
			continue
		}
		if err := state.restore(); err != nil {
			log.Debug().Err(err).Str("path", path).Msg("failed to restore serial port settings after probe")
		}
	}
	g.saved = nil
}
