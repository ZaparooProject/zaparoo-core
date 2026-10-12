/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package ccid

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// powerFailureLimit is how many times a slot may claim a card and then fail
// to power it before it is ignored. One failure can be a tag leaving; a slot
// that always fails is not a contactless slot, such as a reader's SAM socket.
const powerFailureLimit = 3

// registry holds what the driver has learned about readers during this run.
//
// The service builds a fresh Reader for every auto-detect tick, so anything
// that must outlive one tick lives here: which readers have shown a
// contactless tag, which are being ignored, and which log lines have already
// been written.
type registry struct {
	ignored       map[string]string
	seen          map[string]bool
	logged        map[string]bool
	powerFailures map[string]int
	// held is the readers this driver has open.
	held map[string]bool
	mu   syncutil.Mutex
}

func newRegistry() *registry {
	return &registry{
		ignored:       make(map[string]string),
		seen:          make(map[string]bool),
		logged:        make(map[string]bool),
		powerFailures: make(map[string]int),
		held:          make(map[string]bool),
	}
}

var defaultRegistry = newRegistry()

// markSeen records that a reader has presented a contactless tag, so it
// attaches straight away from now on.
func (g *registry) markSeen(path string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seen[path] = true
	delete(g.powerFailures, path)
}

func (g *registry) hasSeen(path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.seen[path]
}

func (g *registry) ignore(path, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ignored[path] = reason
}

func (g *registry) isIgnored(path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.ignored[path]
	return ok
}

// hold records that this driver has a reader open, and release that it no
// longer does. A reader the driver holds looks, from outside, like one some
// other program has claimed.
func (g *registry) hold(path string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.held[path] = true
}

func (g *registry) release(path string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.held, path)
}

func (g *registry) isHeld(path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.held[path]
}

// powerFailed counts a failed activation and reports whether the slot has now
// failed often enough to be ignored.
func (g *registry) powerFailed(path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.powerFailures[path]++
	return g.powerFailures[path] >= powerFailureLimit
}

// firstTime reports whether key has not been logged yet, and records it.
func (g *registry) firstTime(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.logged[key] {
		return false
	}
	g.logged[key] = true
	return true
}

// prune forgets every reader that is no longer listed, so a reader that is
// unplugged and plugged back in is looked at afresh.
func (g *registry) prune(listed map[string]bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for path := range g.ignored {
		if !listed[path] {
			delete(g.ignored, path)
		}
	}
	for path := range g.powerFailures {
		if !listed[path] {
			delete(g.powerFailures, path)
		}
	}
	// A USB path is a port, not a device. Whatever is plugged into that port
	// next has to show a contactless tag itself before it is attached.
	for path := range g.seen {
		if !listed[path] {
			delete(g.seen, path)
		}
	}
	for key := range g.logged {
		if path, ok := loggedPath(key); ok && !listed[path] {
			delete(g.logged, key)
		}
	}
}

const logKeySeparator = "\x00"

// logKey builds a firstTime key that prune can tie back to a reader.
func logKey(kind, path string) string {
	return kind + logKeySeparator + path
}

func loggedPath(key string) (string, bool) {
	for i := range len(key) {
		if key[i] == logKeySeparator[0] {
			return key[i+1:], true
		}
	}
	return "", false
}
