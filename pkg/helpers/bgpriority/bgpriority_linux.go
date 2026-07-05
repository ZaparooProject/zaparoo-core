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
	"unsafe"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	// ioprio constants from linux/ioprio.h (not exposed by x/sys/unix).
	ioprioClassIdle  = 3
	ioprioClassShift = 13
	ioprioWhoProcess = 1

	// SCHED_IDLE policy from linux/sched.h.
	schedIdle = 5

	niceLowest = 19
)

type schedParam struct {
	priority int32
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

	param := schedParam{}
	//nolint:gosec // sched_setscheduler requires a raw pointer argument
	if _, _, errno := unix.Syscall(
		unix.SYS_SCHED_SETSCHEDULER, 0, schedIdle, uintptr(unsafe.Pointer(&param)),
	); errno != 0 {
		log.Warn().Err(errno).Msg("bgpriority: failed to set SCHED_IDLE")
	}

	ioprio := uintptr(ioprioClassIdle << ioprioClassShift)
	if _, _, errno := unix.Syscall(
		unix.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), ioprio,
	); errno != 0 {
		log.Warn().Err(errno).Msg("bgpriority: failed to set idle io priority")
	}

	log.Debug().Int("tid", tid).Msg("bgpriority: background priority applied to worker thread")
}
