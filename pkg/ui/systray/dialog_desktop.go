//go:build windows || darwin

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

package systray

import "github.com/nixinwang/dialog"

// nativeDialog puts a message in a window belonging to the desktop session.
//
// The import lives in this file rather than beside its callers because the
// library binds GTK on Linux through cgo. Only the Windows and macOS binaries
// build a tray, so confining it here keeps a Linux build of this package free
// of GTK and X11.
func nativeDialog(title, message string) {
	dialog.Message("%s", message).Title(title).Info()
}
