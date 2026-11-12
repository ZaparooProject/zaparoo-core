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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNetworkAddresses(t *testing.T) {
	t.Parallel()

	lan := func() []string { return []string{"192.168.1.20", "10.0.0.5"} }
	tcp := func(ip string) net.Addr { return &net.TCPAddr{IP: net.ParseIP(ip), Port: 7497} }
	tests := []struct {
		bound net.Addr
		name  string
		want  []string
	}{
		{name: "all interfaces", bound: &net.TCPAddr{Port: 7497}, want: []string{"192.168.1.20", "10.0.0.5"}},
		{name: "unspecified IPv4", bound: tcp("0.0.0.0"), want: []string{"192.168.1.20", "10.0.0.5"}},
		{name: "unspecified IPv6", bound: tcp("::"), want: []string{"192.168.1.20", "10.0.0.5"}},
		{name: "one address", bound: tcp("192.168.1.20"), want: []string{"192.168.1.20"}},
		{name: "one IPv6 address", bound: tcp("fd00::20"), want: []string{"fd00::20"}},
		{name: "loopback", bound: tcp("127.0.0.1")},
		{name: "IPv6 loopback", bound: tcp("::1")},
		// A hostname in api_listen binds whatever it resolved to, so the
		// bound address is what counts, not the name.
		{name: "hostname bound to loopback", bound: tcp("127.0.1.1")},
		{name: "hostname bound to one address", bound: tcp("10.0.0.5"), want: []string{"10.0.0.5"}},
		{name: "unix socket", bound: &net.UnixAddr{Name: "/run/zaparoo/api.sock", Net: "unix"}},
		{name: "not on the network"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, networkAddresses(tt.bound, lan)())
		})
	}
}
