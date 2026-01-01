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

//go:build !deadlock

// Package syncutil provides mutex primitives with optional deadlock detection.
// Use build tag -tags=deadlock to enable deadlock detection during development.
package syncutil

import "sync"

// DeadlockEnabled is true if the deadlock detector is enabled.
const DeadlockEnabled = false

// A Mutex is a mutual exclusion lock.
//
//nolint:gocritic // embedding sync.Mutex is intentional - this IS the wrapper
type Mutex struct {
	sync.Mutex //nolint:forbidigo // this package wraps sync.Mutex
}

// An RWMutex is a reader/writer mutual exclusion lock.
//
//nolint:gocritic // embedding sync.RWMutex is intentional - this IS the wrapper
type RWMutex struct {
	sync.RWMutex //nolint:forbidigo // this package wraps sync.RWMutex
}
