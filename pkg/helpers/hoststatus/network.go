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
	"fmt"
	"net"
	"sort"
)

// RawInterface is a network interface as the operating system lists it,
// before Core decides what kind of link it is.
type RawInterface struct {
	Name      string
	Addresses []net.IP
	Loopback  bool
	Up        bool
}

// ListRawInterfaces lists the machine's interfaces and their addresses.
func ListRawInterfaces() ([]RawInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("listing network interfaces: %w", err)
	}
	raw := make([]RawInterface, 0, len(ifaces))
	for i := range ifaces {
		iface := &ifaces[i]
		entry := RawInterface{
			Name:     iface.Name,
			Loopback: iface.Flags&net.FlagLoopback != 0,
			Up:       iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagRunning != 0,
		}
		// An interface that vanished between the two calls just has no
		// addresses.
		addrs, addrErr := iface.Addrs()
		if addrErr == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok {
					entry.Addresses = append(entry.Addresses, ipNet.IP)
				}
			}
		}
		raw = append(raw, entry)
	}
	return raw, nil
}

// routableAddresses keeps the addresses another machine could use to reach
// this one.
func routableAddresses(ips []net.IP) []string {
	addresses := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			continue
		}
		addresses = append(addresses, ip.String())
	}
	sort.Strings(addresses)
	return addresses
}

// buildNetwork assembles the device's network view. classify names the kind
// of link an interface is, and isUp may overrule the operating system's flag
// where a better signal exists; either may leave the raw answer alone.
func buildNetwork(
	raw []RawInterface,
	defaultInterface string,
	classify func(name string) LinkType,
	isUp func(iface *RawInterface) bool,
) Network {
	network := Network{Type: LinkNone, Interfaces: []Interface{}}
	for i := range raw {
		iface := &raw[i]
		if iface.Loopback {
			continue
		}
		linkType := classify(iface.Name)
		isDefault := defaultInterface != "" && iface.Name == defaultInterface
		// Bridges, tunnels and container links are only worth reporting when
		// one of them is what the device actually routes through.
		if linkType == LinkOther && !isDefault {
			continue
		}
		up := isUp(iface)
		network.Interfaces = append(network.Interfaces, Interface{
			Name:      iface.Name,
			Type:      linkType,
			Up:        up,
			Addresses: routableAddresses(iface.Addresses),
		})
		if isDefault && up {
			network.Type = linkType
			network.Interface = iface.Name
		}
	}
	sort.Slice(network.Interfaces, func(a, b int) bool {
		return network.Interfaces[a].Name < network.Interfaces[b].Name
	})
	return network
}
