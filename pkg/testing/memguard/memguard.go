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

// Package memguard stops a test binary whose resident memory grows past a
// limit, before it takes the rest of the machine down with it.
//
// It is meant for packages whose tests drive native code, where a decoder
// that never ends or an allocation sized from untrusted input grows outside
// the Go heap and no Go memory limit applies. Start it from TestMain:
//
//	func TestMain(m *testing.M) {
//		memguard.Start(memguard.DefaultLimit)
//		m.Run()
//	}
//
// It samples the whole process, Go heap and C allocations alike, and does
// nothing on platforms other than Linux.
package memguard

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// DefaultLimit is far above what any test package needs today, race
// detector included, and far below what hurts a developer machine.
const DefaultLimit = 2 << 30

// interval is how often the resident size is read. Reading it costs a few
// microseconds; at memory bandwidth a runaway can add a few hundred
// megabytes between samples, which the limit leaves room for.
const interval = 20 * time.Millisecond

// Start watches the process in the background and stops it with a message
// naming the test binary once resident memory passes limit bytes.
func Start(limit uint64) {
	if !supported {
		return
	}
	go watch(limit, interval, residentBytes, exceeded)
}

// watch polls read until it reports more than limit bytes, then calls trip
// once and returns. A failed read stops the watch: the guard must never be
// the reason a test fails.
func watch(limit uint64, every time.Duration, read func() (uint64, error), trip func(rss, limit uint64)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for range ticker.C {
		rss, err := read()
		if err != nil {
			return
		}
		if rss > limit {
			trip(rss, limit)
			return
		}
	}
}

func exceeded(rss, limit uint64) {
	debug.SetTraceback("all")
	panic(message(filepath.Base(os.Args[0]), rss, limit))
}

func message(binary string, rss, limit uint64) string {
	return fmt.Sprintf(
		"memguard: %s resident memory reached %d MiB, over the %d MiB limit; "+
			"a test is allocating without bound (the goroutine dump below shows where)",
		binary, rss>>20, limit>>20,
	)
}
