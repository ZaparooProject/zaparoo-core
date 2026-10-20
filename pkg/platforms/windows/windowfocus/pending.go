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

package windowfocus

import (
	"context"
	"fmt"
)

// Focuser activates the window of a launched process.
type Focuser interface {
	Focus(ctx context.Context, pid uint32) error
}

// Pending is a Focus call that is running in the background. Its outcome is
// the earliest sign that a launched program has a window that takes input.
type Pending struct {
	err  error
	done chan struct{}
}

// Start runs f.Focus for pid in the background.
func Start(ctx context.Context, f Focuser, pid uint32) *Pending {
	p := &Pending{done: make(chan struct{})}
	go func() {
		defer close(p.done)
		p.err = f.Focus(ctx, pid)
	}()
	return p
}

// Err waits for the focus attempt to finish and returns its outcome: nil once
// the window took focus, the attempt's error if it gave up or was cancelled,
// or ctx's error if ctx ends first.
func (p *Pending) Err(ctx context.Context) error {
	select {
	case <-p.done:
		return p.err
	case <-ctx.Done():
		return fmt.Errorf("wait for window focus: %w", ctx.Err())
	}
}

// HasWindow reports whether pid, or a process it started, owns a visible
// top-level window. It does not activate the window.
func (m *Manager) HasWindow(pid uint32) bool {
	if m == nil || m.api == nil || pid == 0 {
		return false
	}
	_, found := m.api.findProcessWindow(pid)
	return found
}
