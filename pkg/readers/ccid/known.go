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

import "strings"

// knownReader is a contactless reader the driver has been tested with. Known
// readers attach as soon as they are plugged in; any other reader attaches
// once it has shown a contactless tag.
type knownReader struct {
	// nameParts must all appear in the PC/SC reader name.
	nameParts []string
	vendor    uint16
	product   uint16
	// usbInterface is the contactless interface on a reader with several.
	usbInterface uint8
}

var knownReaders = []knownReader{
	{nameParts: []string{"ACR122"}, vendor: 0x072F, product: 0x2200},
	// The ACR1552 has a second smart card interface for its SAM slot.
	{nameParts: []string{"ACR1552", "PICC"}, vendor: 0x072F, product: 0x2303},
	{nameParts: []string{"bR500"}, vendor: 0x096E, product: 0x0623},
}

func isKnownName(name string) bool {
	for i := range knownReaders {
		matched := true
		for _, part := range knownReaders[i].nameParts {
			if !strings.Contains(name, part) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func isKnownUSB(vendor, product uint16, usbInterface uint8) bool {
	for i := range knownReaders {
		k := &knownReaders[i]
		if k.vendor == vendor && k.product == product && k.usbInterface == usbInterface {
			return true
		}
	}
	return false
}
