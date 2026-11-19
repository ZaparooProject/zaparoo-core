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

package platforms

import "fmt"

// Reasons a platform's kiosk lock is not sealed, as sent to clients. A
// sealed device is one where nothing outside Zaparoo can be reached from its
// own controls; each reason names one way this device falls short of that.
const (
	// KioskUnsealedHostMenus means the host's own interface (a frontend's
	// menus, a desktop, a system overlay) is not locked by Zaparoo.
	KioskUnsealedHostMenus = "hostMenus"
	// KioskUnsealedMainLock means the MiSTer Main binary running now cannot
	// lock its menu at all.
	KioskUnsealedMainLock = "mainLock"
	// KioskUnsealedMainControls means MiSTer Main locks its menu but still
	// answers controls that leave the running core.
	KioskUnsealedMainControls = "mainControls"
	// KioskUnsealedFrontend means the Zaparoo frontend on this device does
	// not yet hide what a locked device should not offer.
	KioskUnsealedFrontend = "frontend"
	// KioskUnsealedIdleReset means idle reset is turned on but this device
	// cannot tell when it is being used.
	KioskUnsealedIdleReset = "idleReset"
)

// KioskHost is implemented by a platform that takes part in the kiosk lock:
// it can say how far its own interface is closed, and may have a lock of its
// own to keep in step with Core's.
type KioskHost interface {
	// KioskUnsealed lists the reasons this device is not sealed right now.
	// An empty list means it is.
	KioskUnsealed() []string
	// SetKioskLocked brings the host's own lock in line with Core's. It is
	// called on every change and again whenever the host may have lost it,
	// so it must be safe to repeat.
	SetKioskLocked(locked bool) error
}

// KioskUnsealed returns the reasons pl is not sealed. A platform that does
// not take part in the lock is one whose own menus Zaparoo cannot close.
func KioskUnsealed(pl Platform) []string {
	if host, ok := pl.(KioskHost); ok {
		reasons := host.KioskUnsealed()
		if reasons == nil {
			reasons = []string{}
		}
		return reasons
	}
	return []string{KioskUnsealedHostMenus}
}

// SetKioskLocked passes Core's lock position to a platform that has a lock
// of its own, and does nothing for one that has none.
func SetKioskLocked(pl Platform, locked bool) error {
	host, ok := pl.(KioskHost)
	if !ok {
		return nil
	}
	if err := host.SetKioskLocked(locked); err != nil {
		return fmt.Errorf("set host kiosk lock: %w", err)
	}
	return nil
}
