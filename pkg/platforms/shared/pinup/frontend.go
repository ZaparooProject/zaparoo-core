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
package pinup

import "context"

// Frontend starts Popper's programs. Only the operations with side effects
// live here; whether the menu or web server is running is read from the
// process list, so tests script both through one fake. Events go through the
// web remote: Popper's own Launch\SendPuPEvent.exe does not close a table on
// Popper 2.0, so it is not used.
type Frontend interface {
	// StartMenu launches PinUpMenu.exe detached, with the install folder as
	// its working directory, the way Popper's own startup script does.
	StartMenu(ctx context.Context, inst *Install) error
	// StartServer launches PuPServer.exe, the web remote, listening on port.
	StartServer(ctx context.Context, inst *Install, port int) error
}
