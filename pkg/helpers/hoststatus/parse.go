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

import (
	"strconv"
	"strings"
)

// routeFlagUp is RTF_UP, the flag the kernel sets on a route it is using.
const routeFlagUp = 0x1

// ParseDefaultRouteInterface returns the interface carrying the default route
// with the lowest metric from the contents of /proc/net/route, or "" when
// there is none.
func ParseDefaultRouteInterface(routes string) string {
	var (
		best       string
		bestMetric uint64
		found      bool
	)
	first := true
	for line := range strings.SplitSeq(routes, "\n") {
		if first {
			// The header row.
			first = false
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 16)
		if err != nil || flags&routeFlagUp == 0 {
			continue
		}
		metric := uint64(1<<32 - 1)
		if len(fields) > 6 {
			if parsed, parseErr := strconv.ParseUint(fields[6], 10, 32); parseErr == nil {
				metric = parsed
			}
		}
		if !found || metric < bestMetric {
			best, bestMetric, found = fields[0], metric, true
		}
	}
	return best
}

// Input bus numbers from the kernel's input.h.
const (
	busUSB       = 0x03
	busBluetooth = 0x05
)

// Key codes from the kernel's input-event-codes.h that tell a game controller
// from everything else that produces key events.
const (
	keyEsc      = 0x01
	keyA        = 0x1e
	btnJoystick = 0x120
	btnGamepad  = 0x130
)

// InputModalias is the identity an input device announces in its sysfs
// modalias.
type InputModalias struct {
	Keys    []uint16
	Bus     uint16
	Vendor  uint16
	Product uint16
}

// ParseInputModalias reads an input device modalias of the form
// "input:b0005v054Cp0CE6e8100-e0,1,3,k130,131,ra0,1,mlsfw". The key list is
// read from here rather than from capabilities/key because that bitmap's word
// width depends on the architecture.
func ParseInputModalias(alias string) (InputModalias, bool) {
	alias = strings.TrimSpace(alias)
	rest, ok := strings.CutPrefix(alias, "input:b")
	if !ok {
		return InputModalias{}, false
	}
	head, tail, _ := strings.Cut(rest, "-")
	// The head is bBBBBvVVVVpPPPPeEEEE with the leading "b" already removed.
	if len(head) < 14 || head[4] != 'v' || head[9] != 'p' {
		return InputModalias{}, false
	}
	bus, busErr := strconv.ParseUint(head[0:4], 16, 16)
	vendor, vendorErr := strconv.ParseUint(head[5:9], 16, 16)
	product, productErr := strconv.ParseUint(head[10:14], 16, 16)
	if busErr != nil || vendorErr != nil || productErr != nil {
		return InputModalias{}, false
	}
	parsed := InputModalias{Bus: uint16(bus), Vendor: uint16(vendor), Product: uint16(product)}

	// Capability groups follow, each a letter and a comma-separated list of
	// hex codes; the group's last entry runs straight into the next letter.
	inKeys := false
	for field := range strings.SplitSeq(tail, ",") {
		code := field
		if field != "" && !isHexDigit(field[0]) {
			inKeys = field[0] == 'k'
			code = field[1:]
		}
		if !inKeys {
			continue
		}
		// Trailing group letters such as the "r" in "13fra0" start the next
		// group.
		end := 0
		for end < len(code) && isHexDigit(code[end]) {
			end++
		}
		if end > 0 {
			if key, err := strconv.ParseUint(code[:end], 16, 16); err == nil {
				parsed.Keys = append(parsed.Keys, uint16(key))
			}
		}
		if end < len(code) {
			inKeys = code[end] == 'k'
		}
	}
	return parsed, true
}

// isHexDigit reports whether c is a digit as the kernel prints modalias codes,
// which is upper-case hex.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')
}

// IsGamepad reports whether the device has controller buttons and is not a
// keyboard that happens to expose a few of them.
func (m InputModalias) IsGamepad() bool {
	var pad, keyboard bool
	for _, key := range m.Keys {
		switch key {
		case btnGamepad, btnJoystick:
			pad = true
		case keyEsc, keyA:
			keyboard = true
		}
	}
	return pad && !keyboard
}

// Connection names how the device is attached.
func (m InputModalias) Connection() string {
	switch m.Bus {
	case busUSB:
		return ConnectionUSB
	case busBluetooth:
		return ConnectionBluetooth
	default:
		return ConnectionUnknown
	}
}

// Connector is one display output from /sys/class/drm.
type Connector struct {
	Name      string
	Connected bool
	Enabled   bool
}

// ConnectorName returns the connector half of a /sys/class/drm entry named
// "card<N>-<connector>".
func ConnectorName(entry string) (string, bool) {
	rest, ok := strings.CutPrefix(entry, "card")
	if !ok {
		return "", false
	}
	index, name, ok := strings.Cut(rest, "-")
	if !ok || index == "" || name == "" {
		return "", false
	}
	for i := range len(index) {
		if index[i] < '0' || index[i] > '9' {
			return "", false
		}
	}
	return name, true
}

// internalConnectorPrefixes are the connector types a built-in panel uses.
var internalConnectorPrefixes = []string{"eDP", "LVDS", "DSI", "DPI"}

// ignoredConnectorPrefixes are outputs that are not a screen anyone looks at.
var ignoredConnectorPrefixes = []string{"Writeback", "Virtual"}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// ClassifyDisplay turns a set of connectors into the facts a client lays its
// interface out from. A device with a built-in panel is docked while an
// external display is being driven.
func ClassifyDisplay(connectors []Connector) Display {
	var display Display
	for _, connector := range connectors {
		if hasAnyPrefix(connector.Name, ignoredConnectorPrefixes) {
			continue
		}
		active := connector.Connected && connector.Enabled
		if hasAnyPrefix(connector.Name, internalConnectorPrefixes) {
			display.InternalPanel = display.InternalPanel || connector.Connected
			display.InternalActive = display.InternalActive || active
			continue
		}
		display.ExternalConnected = display.ExternalConnected || connector.Connected
		display.ExternalActive = display.ExternalActive || active
	}
	if display.InternalPanel {
		docked := display.ExternalActive
		display.Docked = &docked
	}
	return display
}
