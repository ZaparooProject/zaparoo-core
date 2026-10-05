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

package playnite

// launchKey identifies one run of a game: its Playnite ID plus the lifecycle
// ID assigned when Core claimed it. A relaunch of the same game gets a fresh
// lifecycle ID, which is what lets a late result for the earlier run be told
// apart from the run that replaced it. Real claims never use zero, so the
// zero value never collides with a live run.
type launchKey struct {
	gameID      string
	lifecycleID int
}

// activeRun is the game Core currently attributes to Playnite. A run is
// claimed when Core asks for a launch and adopted when the extension reports
// the game started; session and pid are only meaningful once adopted.
type activeRun struct {
	path    string
	key     launchKey
	session int
	pid     int
	adopted bool
}
