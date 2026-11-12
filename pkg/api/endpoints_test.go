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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNetworkAddresses(t *testing.T) {
	t.Parallel()

	lan := func() []string { return []string{"192.168.1.20", "10.0.0.5"} }
	tests := []struct {
		name      string
		listen    string
		want      []string
		reachable bool
	}{
		{name: "all interfaces", listen: ":7497", reachable: true, want: []string{"192.168.1.20", "10.0.0.5"}},
		{name: "unspecified IPv4", listen: "0.0.0.0:7497", reachable: true, want: []string{"192.168.1.20", "10.0.0.5"}},
		{name: "unspecified IPv6", listen: "[::]:7497", reachable: true, want: []string{"192.168.1.20", "10.0.0.5"}},
		{name: "no port", listen: "", reachable: true, want: []string{"192.168.1.20", "10.0.0.5"}},
		{name: "one address", listen: "192.168.1.20:7497", reachable: true, want: []string{"192.168.1.20"}},
		{name: "loopback", listen: "127.0.0.1:7497", reachable: true},
		{name: "IPv6 loopback", listen: "[::1]:7497", reachable: true},
		{name: "localhost", listen: "LocalHost:7497", reachable: true},
		{name: "not on the network", listen: ":7497"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, networkAddresses(tt.listen, tt.reachable, lan)())
		})
	}
}
