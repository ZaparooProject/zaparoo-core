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
)

// networkAddresses returns what device.endpoints reports as the LAN
// addresses of the network API. bound is the address the network-facing
// listener actually holds, or nil when the API only listens on a listener
// the host supplied, such as a Unix socket. The configured address is not
// consulted: a hostname in it resolved to one address when the listener was
// bound, and that address is the only one that answers. An API bound to
// every interface is reported at each local IP, one bound to a single
// address there alone, and one bound to loopback is not on the network.
func networkAddresses(bound net.Addr, localIPs LocalIPsProvider) func() []string {
	return func() []string {
		tcp, ok := bound.(*net.TCPAddr)
		if !ok || tcp == nil {
			return nil
		}
		switch {
		case tcp.IP == nil || tcp.IP.IsUnspecified():
			return localIPs()
		case tcp.IP.IsLoopback():
			return nil
		default:
			return []string{tcp.IP.String()}
		}
	}
}
