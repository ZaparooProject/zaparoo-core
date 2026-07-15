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

import (
	"context"
	"errors"
)

// ErrProfileDataUnavailable is returned (wrapped) by ApplyProfile when the
// swap cannot run on the current storage setup — e.g. saves are on a
// read-only network mount — as opposed to an operation failing. Callers
// report it as "unavailable" rather than "failed".
var ErrProfileDataUnavailable = errors.New("profile data swap unavailable")

// ProfileItem owner classes. "profile" data swaps with the active profile,
// "device" data never swaps (it belongs to the hardware/display), and
// "shared" data is ambiguous by default and needs an explicit user choice
// before it would ever swap.
const (
	ProfileItemOwnerProfile = "profile"
	ProfileItemOwnerDevice  = "device"
	ProfileItemOwnerShared  = "shared"
)

// ProfileItem describes one category of data a profile change can affect
// on a platform. It is purely descriptive: no paths, no mechanisms.
type ProfileItem struct {
	ID    string // stable identity, e.g. "saves", "savestates"
	Label string // for client UI, e.g. "Save files"
	Owner string // one of the ProfileItemOwner* constants
}

// ProfileRef identifies the profile whose data should be made current. An
// empty ID means the shared profile (the device's un-profiled state). Name
// is the display name, used by platforms to label per-profile storage for
// humans browsing it outside Zaparoo.
type ProfileRef struct {
	ID   string
	Name string
}

// ProfileDataWatcher is an optional platform capability: platforms whose
// profile data state can change underneath the service (e.g. MiSTer's
// mount table, where a cifs boot script or a USB drive can appear at any
// time) implement it so the service can re-reconcile on those changes.
type ProfileDataWatcher interface {
	// WatchProfileData invokes onChange (possibly from another goroutine)
	// whenever platform storage state changes, until ctx is done.
	WatchProfileData(ctx context.Context, onChange func())
}

// ProfileDataSwapper is an optional platform capability: platforms that can
// swap profile-scoped data (save files, save states) implement it and core
// discovers it by type assertion. Platforms without the capability simply
// don't implement it — profiles there are limits + attribution only.
type ProfileDataSwapper interface {
	// ProfileItems reports what a profile change can affect on this
	// platform.
	ProfileItems() []ProfileItem
	// ApplyProfile makes the platform's profile-scoped data current for
	// the given profile, applying only the enabled item IDs. It is called
	// only while no media is running, must be idempotent, and must leave
	// existing data intact on failure.
	ApplyProfile(ref ProfileRef, enabledItems []string) error
}
