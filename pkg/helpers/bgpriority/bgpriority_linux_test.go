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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIoprioIdleValue checks the packed IOPRIO_PRIO_VALUE(class, data) layout
// against the known constant from linux/ioprio.h: IOPRIO_CLASS_IDLE (3)
// shifted into IOPRIO_CLASS_SHIFT (13), data left at 0.
func TestIoprioIdleValue(t *testing.T) {
	t.Parallel()

	const wantIdleValue = 0x6000
	require.EqualValues(t, wantIdleValue, ioprioIdleValue())
}

// TestApply_DoesNotPanic runs Apply on a dedicated, throwaway goroutine —
// never on the test goroutine itself, since Apply intentionally never calls
// runtime.UnlockOSThread and permanently lowers whichever OS thread it runs
// on. Letting the wrapper goroutine exit afterward makes the runtime destroy
// that thread, so the lowered priority never leaks into the rest of the test
// binary.
func TestApply_DoesNotPanic(t *testing.T) {
	t.Parallel()

	done := make(chan any, 1)
	go func() {
		defer func() {
			done <- recover()
		}()
		Apply()
	}()

	select {
	case r := <-done:
		require.Nil(t, r, "Apply panicked: %v", r)
	case <-time.After(5 * time.Second):
		t.Fatal("Apply did not return in time")
	}
}
