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

//go:build linux

package bgpriority

import (
	"runtime"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	// ioprio constants from linux/ioprio.h (not exposed by x/sys/unix).
	ioprioClassIdle  = 3
	ioprioClassShift = 13
	ioprioWhoProcess = 1

	niceLowest = 19
)

// ioprioIdleValue packs IOPRIO_CLASS_IDLE into the IOPRIO_PRIO_VALUE(class,
// data) layout expected by ioprio_set, per linux/ioprio.h. Data is left at 0:
// IOPRIO_CLASS_IDLE has no priority levels within the class.
func ioprioIdleValue() uintptr {
	return uintptr(ioprioClassIdle << ioprioClassShift)
}

// Apply locks the calling goroutine to its OS thread and drops that thread
// to the lowest CPU and IO scheduling priority (nice 19, SCHED_IDLE,
// IOPRIO_CLASS_IDLE). Call it at the top of a dedicated background worker
// goroutine. The goroutine must not call runtime.UnlockOSThread: keeping the
// thread locked until the goroutine exits makes the runtime destroy the
// thread, so the lowered priorities never leak to other goroutines.
//
// IO priority is honored by the bfq and mq-deadline (kernel 5.13+) block
// schedulers and ignored by none; CPU changes apply everywhere. All
// failures are logged and non-fatal — the caller continues at normal
// priority in the worst case.
func Apply() {
	runtime.LockOSThread()
	tid := unix.Gettid()

	if err := unix.Setpriority(unix.PRIO_PROCESS, tid, niceLowest); err != nil {
		log.Warn().Err(err).Msg("bgpriority: failed to set nice level")
	}

	// pid 0 means the calling thread, matching the tid we just locked to.
	if err := unix.SchedSetAttr(0, &unix.SchedAttr{Policy: unix.SCHED_IDLE}, 0); err != nil {
		log.Warn().Err(err).Msg("bgpriority: failed to set SCHED_IDLE")
	}

	if _, _, errno := unix.Syscall(
		unix.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), ioprioIdleValue(),
	); errno != 0 {
		log.Warn().Err(errno).Msg("bgpriority: failed to set idle io priority")
	}

	log.Debug().Int("tid", tid).Msg("bgpriority: background priority applied to worker thread")
}
