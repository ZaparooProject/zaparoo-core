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

package hoststatus

import "strings"

// controllerNames maps "vendor:product" (four lowercase hex digits each) to a
// controller's model name. Every ID comes from the Linux kernel's hid-ids.h
// and xpad.c or SDL's usb_ids.h. A clone that reuses an ID is named as the
// pad it imitates, which is also what the kernel drives it as.
var controllerNames = map[string]string{ //nolint:gochecknoglobals // Static lookup table.
	// Sony
	"054c:0268": "DualShock 3",
	"054c:05c4": "DualShock 4",
	"054c:09cc": "DualShock 4",
	"054c:0ba0": "DualShock 4",
	"054c:0ce6": "DualSense",
	"054c:0df2": "DualSense Edge",
	"054c:03d5": "PlayStation Move Motion Controller",
	"054c:042f": "PlayStation Move Navigation Controller",

	// Microsoft
	"045e:0202": "Xbox Controller",
	"045e:0285": "Xbox Controller",
	"045e:0289": "Xbox Controller",
	"045e:0287": "Xbox Controller S",
	"045e:0288": "Xbox Controller S",
	"045e:028e": "Xbox 360 Controller",
	"045e:028f": "Xbox 360 Controller",
	"045e:0719": "Xbox 360 Wireless Controller",
	"045e:02d1": "Xbox One Controller",
	"045e:02dd": "Xbox One Controller",
	"045e:02ea": "Xbox One S Controller",
	"045e:02e0": "Xbox One S Controller",
	"045e:02fd": "Xbox One S Controller",
	"045e:0b20": "Xbox One S Controller",
	"045e:02e3": "Xbox Elite Controller",
	"045e:0b00": "Xbox Elite Series 2 Controller",
	"045e:0b05": "Xbox Elite Series 2 Controller",
	"045e:0b22": "Xbox Elite Series 2 Controller",
	"045e:0b0a": "Xbox Adaptive Controller",
	"045e:0b0c": "Xbox Adaptive Controller",
	"045e:0b21": "Xbox Adaptive Controller",
	"045e:0b12": "Xbox Series Controller",
	"045e:0b13": "Xbox Series Controller",

	// Nintendo
	"057e:2009": "Switch Pro Controller",
	"057e:2006": "Joy-Con (L)",
	"057e:2007": "Joy-Con (R)",
	"057e:200e": "Joy-Con Charging Grip",
	"057e:2017": "SNES Controller",
	"057e:2019": "N64 Controller",
	"057e:201e": "Genesis Controller",
	"057e:2069": "Switch 2 Pro Controller",
	"057e:2067": "Joy-Con 2 (L)",
	"057e:2066": "Joy-Con 2 (R)",
	"057e:2073": "GameCube Controller",
	"057e:0306": "Wii Remote",
	"057e:0330": "Wii Remote Plus",

	// 8BitDo
	"2dc8:6000": "8BitDo SF30 Pro",
	"2dc8:6100": "8BitDo SF30 Pro",
	"2dc8:6001": "8BitDo SN30 Pro",
	"2dc8:6101": "8BitDo SN30 Pro",
	"2dc8:6003": "8BitDo Pro 2",
	"2dc8:6006": "8BitDo Pro 2",
	"2dc8:2000": "8BitDo Pro 2",
	"2dc8:201e": "8BitDo Pro 2",
	"2dc8:6009": "8BitDo Pro 3",
	"2dc8:3109": "8BitDo Ultimate Wireless",
	"2dc8:6012": "8BitDo Ultimate 2 Wireless",
	"2dc8:310b": "8BitDo Ultimate 2 Wireless",
	"2dc8:301b": "8BitDo Ultimate 2C Wireless",
	"2dc8:310a": "8BitDo Ultimate 2C Wireless",
	"2dc8:200f": "8BitDo Ultimate 3-mode",

	// Valve
	"28de:1205": "Steam Deck",
	"28de:1102": "Steam Controller",
	"28de:1142": "Steam Controller",
}

// ControllerDisplayName returns the model name for a controller's vendor and
// product IDs, or the empty string when the model is not known. IDs are
// matched without regard to case.
func ControllerDisplayName(vendorID, productID string) string {
	if vendorID == "" || productID == "" {
		return ""
	}
	return controllerNames[strings.ToLower(vendorID)+":"+strings.ToLower(productID)]
}

// ParseModalias reads the vendor and product IDs from a device's modalias,
// such as "usb:v054Cp09CCd0100" or "bluetooth:v000Fp1200d1436". Both come
// back as four lowercase hex digits, or empty when the modalias is not in that
// form.
func ParseModalias(modalias string) (vendorID, productID string) {
	source, ids, ok := strings.Cut(modalias, ":")
	if !ok || (source != "usb" && source != "bluetooth") {
		return "", ""
	}
	const field = 5 // a letter and four hex digits
	if len(ids) < 2*field || (ids[0] != 'v' && ids[0] != 'V') || (ids[field] != 'p' && ids[field] != 'P') {
		return "", ""
	}
	if len(ids) > 2*field && ids[2*field] != 'd' && ids[2*field] != 'D' {
		return "", ""
	}
	vendor, product := ids[1:field], ids[field+1:2*field]
	if !allHex(vendor) || !allHex(product) {
		return "", ""
	}
	return strings.ToLower(vendor), strings.ToLower(product)
}

func allHex(text string) bool {
	for i := range len(text) {
		if !isHex(text[i]) {
			return false
		}
	}
	return true
}
