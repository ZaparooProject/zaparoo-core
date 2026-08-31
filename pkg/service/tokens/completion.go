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

package tokens

import "sync/atomic"

// Completion carries the terminal result of processing a queued token back
// to the caller that queued it. Only callers that wait on the outcome (the
// JSON-RPC run method) attach one; readers, playlists, REST and GMC leave it
// nil, and every method is a no-op on a nil receiver.
//
// The channel is buffered so the token worker never blocks on a caller that
// has stopped waiting, and Complete is guarded so the result is delivered at
// most once however many terminal paths reach it.
type Completion struct {
	ch   chan error
	done atomic.Bool
}

// NewCompletion returns a Completion ready to receive exactly one result.
func NewCompletion() *Completion {
	return &Completion{ch: make(chan error, 1)}
}

// Complete records err as the terminal result. The first call wins and
// returns true; later calls, and calls on a nil receiver, return false. It
// never blocks.
func (c *Completion) Complete(err error) bool {
	if c == nil || !c.done.CompareAndSwap(false, true) {
		return false
	}
	c.ch <- err
	return true
}

// Done returns a channel that yields the terminal result once. It is nil for
// a nil receiver, which blocks forever in a select and so matches tokens that
// never expected a result.
func (c *Completion) Done() <-chan error {
	if c == nil {
		return nil
	}
	return c.ch
}
