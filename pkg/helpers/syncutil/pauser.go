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

package syncutil

import "context"

// Pauser is a thread-safe pause/resume primitive using the closed-channel
// pattern. When not paused, Wait returns immediately. When paused, Wait
// blocks until Resume is called or the context is cancelled.
//
// A nil *Pauser is safe to use: Wait always returns nil.
type Pauser struct {
	ch     chan struct{}
	paused bool
	mu     Mutex
}

// NewPauser returns a Pauser in the unpaused state.
func NewPauser() *Pauser {
	ch := make(chan struct{})
	close(ch)
	return &Pauser{ch: ch}
}

// Pause requests a pause. Idempotent: calling Pause when already paused
// is a no-op.
func (p *Pauser) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.paused {
		return
	}
	p.paused = true
	p.ch = make(chan struct{})
}

// Resume unblocks all goroutines waiting in Wait. Idempotent: calling
// Resume when not paused is a no-op.
func (p *Pauser) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.paused {
		return
	}
	p.paused = false
	close(p.ch)
}

// IsPaused reports whether the Pauser is currently in the paused state.
func (p *Pauser) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

// Wait blocks while the Pauser is paused. It returns nil immediately when
// not paused, blocks until Resume is called, or returns the context error
// if the context is cancelled while paused. A nil receiver returns nil.
func (p *Pauser) Wait(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	ch := p.ch
	p.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
