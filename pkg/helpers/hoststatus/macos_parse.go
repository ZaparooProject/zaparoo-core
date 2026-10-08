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
	"strings"
)

// The parsers for macOS command output live outside the darwin build tag so
// they can be tested on the machines that run the test suite.

// ParseRouteDefaultInterface reads the interface from the output of
// `route -n get default`.
func ParseRouteDefaultInterface(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "interface:"); ok {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

// ParseHardwarePorts reads the kind of link behind each device from the
// output of `networksetup -listallhardwareports`.
func ParseHardwarePorts(output string) map[string]LinkType {
	ports := map[string]LinkType{}
	port := ""
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if name, ok := strings.CutPrefix(line, "Hardware Port:"); ok {
			port = strings.ToLower(strings.TrimSpace(name))
			continue
		}
		device, ok := strings.CutPrefix(line, "Device:")
		if !ok || port == "" {
			continue
		}
		linkType := LinkOther
		switch {
		case strings.Contains(port, "wi-fi") || strings.Contains(port, "airport"):
			linkType = LinkWifi
		case strings.Contains(port, "ethernet") || strings.Contains(port, "lan"):
			linkType = LinkWired
		}
		ports[strings.TrimSpace(device)] = linkType
		port = ""
	}
	return ports
}
