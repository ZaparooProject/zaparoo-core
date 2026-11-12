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

package device

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// icuZoneCapacity is the room, in UTF-16 units, for an IANA zone name.
const icuZoneCapacity = 128

// systemZoneName asks the ICU library that ships with Windows 10 1903 and later
// for the IANA name of the zone the machine is set to. Earlier versions have
// no answer, and neither does a machine where ICU cannot name the zone.
func systemZoneName() string {
	proc := windows.NewLazySystemDLL("icu.dll").NewProc("ucal_getDefaultTimeZone")
	if proc.Find() != nil {
		return ""
	}
	var (
		buf    [icuZoneCapacity]uint16
		status int32
	)
	//nolint:gosec // G103: pointers are to locals the call fills in
	length, _, _ := proc.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(icuZoneCapacity),
		uintptr(unsafe.Pointer(&status)),
	)
	// A positive UErrorCode is a failure; zero and the negative values are
	// success and warnings. The length is an int32 in the low half of the
	// result, so a negative one reads as far too large.
	length &= 0xFFFFFFFF
	if status > 0 || length == 0 || length >= icuZoneCapacity {
		return ""
	}
	name := windows.UTF16ToString(buf[:length])
	if name == "Etc/Unknown" {
		return ""
	}
	return name
}
