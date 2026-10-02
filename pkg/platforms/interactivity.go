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

// InteractivityReader is implemented by a platform that can cheaply answer
// whether its display is interactive right now: on, unlocked, and in active
// use. It is a poll-friendly signal for pacing background work that is only
// worth doing while someone could actually be looking at the screen (for
// example, Android's Online remote-control/Library sync wait, which a
// background process cannot act on anyway while the screen is off) — unlike
// the Android platform's own heavier, per-dispatch ForegroundState, this is
// not evidence for session timing and is never read fresh "before a launch",
// only cheaply and repeatedly by a background loop.
//
// A platform that does not implement this is always treated as interactive,
// so background work paced by it runs unconditionally elsewhere.
type InteractivityReader interface {
	// Interactive reports whether the display is interactive right now.
	Interactive() bool
}
