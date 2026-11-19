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

package shared

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// LaunchExit holds the MediaExited callback of a launcher's latest launch.
// Launchers that play on a remote player have no process for Core to watch,
// so their stop control reports the exit through it once the player accepts
// the stop. A callback from an older launch is harmless: Core ignores it once
// that media is no longer active.
type LaunchExit struct {
	exited func()
	gen    uint64
	mu     syncutil.Mutex
}

// Set records the exit callback from a launch's options.
func (e *LaunchExit) Set(opts *platforms.LaunchOptions) {
	var exited func()
	if opts != nil {
		exited = opts.MediaExited
	}
	e.mu.Lock()
	e.exited = exited
	e.gen++
	e.mu.Unlock()
}

// Capture binds a report to the launch recorded now. A stop captures before
// it sends its command, so a launch that records its own callback while the
// command is in flight is not reported as exited. Calling the returned
// function reports the captured launch's exit, at most once per launch.
func (e *LaunchExit) Capture() func() {
	e.mu.Lock()
	gen, exited := e.gen, e.exited
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		if e.gen == gen {
			if e.exited == nil {
				e.mu.Unlock()
				return
			}
			e.exited = nil
		}
		e.mu.Unlock()
		if exited != nil {
			exited()
		}
	}
}
