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

import "context"

// Frontend starts Playnite. It is an interface so tests can observe the
// command without running anything.
type Frontend interface {
	// StartGame runs Playnite with its --start argument. A running Playnite
	// receives the request from the new process, which then exits; otherwise
	// Playnite starts and launches the game once it has loaded.
	StartGame(ctx context.Context, inst *Install, gameID string) error
}
