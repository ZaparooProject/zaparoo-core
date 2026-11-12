//go:build darwin

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

package inputactivity

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <stdint.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>

// hidIdleNanoseconds returns the time since the last keyboard or mouse event
// in nanoseconds, or -1 when IOKit will not say.
static int64_t hidIdleNanoseconds(void) {
    io_iterator_t iter;
    if (IOServiceGetMatchingServices(kIOMainPortDefault, IOServiceMatching("IOHIDSystem"), &iter) != KERN_SUCCESS) {
        return -1;
    }
    int64_t result = -1;
    io_registry_entry_t entry = IOIteratorNext(iter);
    if (entry != 0) {
        CFTypeRef value = IORegistryEntryCreateCFProperty(entry, CFSTR("HIDIdleTime"), kCFAllocatorDefault, 0);
        if (value != NULL) {
            int64_t ns = 0;
            if (CFGetTypeID(value) == CFNumberGetTypeID() && CFNumberGetValue(value, kCFNumberSInt64Type, &ns)) {
                result = ns;
            }
            CFRelease(value);
        }
        IOObjectRelease(entry);
    }
    IOObjectRelease(iter);
    return result;
}
*/
import "C"

import (
	"context"
	"errors"
	"time"

	"github.com/jonboulle/clockwork"
)

var errNoHIDIdle = errors.New("HIDIdleTime is not available")

// Mac reports keyboard and mouse from IOKit's HIDIdleTime. Game controllers
// have no equivalent counter, so they are not covered.
type Mac struct {
	clock clockwork.Clock
}

// NewMac returns a keyboard and mouse source.
func NewMac() *Mac {
	return &Mac{clock: clockwork.NewRealClock()}
}

func hidIdle() (time.Duration, bool) {
	ns := int64(C.hidIdleNanoseconds())
	if ns < 0 {
		return 0, false
	}
	return time.Duration(ns), true
}

// Coverage reports keyboard and mouse when IOKit answers.
func (*Mac) Coverage() Class {
	if _, ok := hidIdle(); ok {
		return Keyboard | Mouse
	}
	return 0
}

// Watch blocks until ctx is cancelled.
func (m *Mac) Watch(ctx context.Context, responsive bool, onActivity func()) error {
	if _, ok := hidIdle(); !ok {
		return errNoHIDIdle
	}
	every := pollEvery(responsive)
	tracker := NewIdleTracker(m.clock)
	throttle := NewThrottle(m.clock, NotifyInterval, onActivity)
	watchPolled(ctx, every, func() bool {
		idle, ok := hidIdle()
		if !ok {
			// Not knowing counts as activity so rotation never fires blind.
			return true
		}
		return tracker.Observe(idle) || idle < every
	}, throttle.Trigger)
	return nil
}
