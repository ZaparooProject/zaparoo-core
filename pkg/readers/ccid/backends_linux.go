//go:build linux && !android

/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package ccid

import "github.com/spf13/afero"

// platformBackends returns the ways this platform reaches readers, in order
// of preference. A reader the PC/SC service holds is used through it; one
// that nothing holds, as on a system with no such service, is used over USB.
func platformBackends() []backend {
	return []backend{
		newPCSCBackend(establishPCSC),
		&usbBackend{fs: afero.NewOsFs(), open: openUSBFS},
	}
}
