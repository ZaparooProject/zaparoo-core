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

package mdns

import (
	"fmt"
	"net"
)

// InterfaceAddrs returns the addresses of iface worth putting in an mDNS
// answer, split by family and in a stable order.
//
// IPv4 link-local addresses are dropped: Windows leaves a 169.254.0.0/16
// address on adapters that are up but not connected to anything, and
// advertising one sends clients to an address that cannot answer. IPv6
// link-local addresses are only used when the interface has no other IPv6
// address, matching what a client can actually reach without a scope id.
func InterfaceAddrs(iface *net.Interface) (v4, v6 []net.IP, err error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, nil, fmt.Errorf("list addresses for %s: %w", iface.Name, err)
	}

	v4, v6 = selectAddrs(addrs)
	return v4, v6, nil
}

// selectAddrs applies the address rules described on InterfaceAddrs.
func selectAddrs(addrs []net.Addr) (v4, v6 []net.IP) {
	var v6Local []net.IP
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}

		if ip4 := ip.To4(); ip4 != nil {
			if ip4.IsLinkLocalUnicast() {
				continue
			}
			v4 = appendUniqueIP(v4, ip4)
			continue
		}

		if ip.To16() == nil {
			continue
		}
		if ip.IsLinkLocalUnicast() {
			v6Local = appendUniqueIP(v6Local, ip)
			continue
		}
		v6 = appendUniqueIP(v6, ip)
	}

	if len(v6) == 0 {
		v6 = v6Local
	}

	return v4, v6
}

func appendUniqueIP(list []net.IP, ip net.IP) []net.IP {
	for _, existing := range list {
		if existing.Equal(ip) {
			return list
		}
	}
	return append(list, ip)
}
