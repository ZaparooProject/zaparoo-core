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

//go:build deadlock

// Package syncutil provides mutex primitives with optional deadlock detection.
// Use build tag -tags=deadlock to enable deadlock detection during development.
package syncutil

import (
	"time"

	deadlock "github.com/sasha-s/go-deadlock"
)

// DeadlockEnabled is true if the deadlock detector is enabled.
const DeadlockEnabled = true

func init() {
	deadlock.Opts.DeadlockTimeout = 30 * time.Second
}

// A Mutex is a mutual exclusion lock.
type Mutex struct {
	deadlock.Mutex
}

// An RWMutex is a reader/writer mutual exclusion lock.
type RWMutex struct {
	deadlock.RWMutex
}
