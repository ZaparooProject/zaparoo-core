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

package api

import (
	"net"
	"strings"
)

// networkAddresses returns what device.endpoints reports as the LAN
// addresses of the network API. reachable is false when the API only
// listens on a listener the host supplied, such as a Unix socket. An API
// bound to one address answers there alone, and one bound to loopback is
// not on the network at all.
func networkAddresses(listenAddr string, reachable bool, localIPs LocalIPsProvider) func() []string {
	return func() []string {
		if !reachable {
			return nil
		}
		host, _, err := net.SplitHostPort(listenAddr)
		if err != nil {
			host = ""
		}
		host = strings.Trim(host, "[]")
		if strings.EqualFold(host, "localhost") {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
			if ip.IsLoopback() {
				return nil
			}
			return []string{ip.String()}
		}
		return localIPs()
	}
}
