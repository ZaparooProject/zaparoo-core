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

package discovery

import (
	"net"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		platformID string
	}{
		{"mister platform", "mister"},
		{"linux platform", "linux"},
		{"steamos platform", "steamos"},
		{"windows platform", "windows"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := New(nil, tt.platformID)

			assert.NotNil(t, svc)
			assert.Equal(t, tt.platformID, svc.platformID)
		})
	}
}

func TestServiceType(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "_zaparoo._tcp", ServiceType)
}

func TestStopIdempotent(t *testing.T) {
	t.Parallel()

	svc := New(nil, "test")

	// Stop should be safe to call multiple times even when not started
	svc.Stop()
	svc.Stop()
	svc.Stop()

	// No panic means success
	assert.Nil(t, svc.server)
}

func TestInstanceNameBeforeStart(t *testing.T) {
	t.Parallel()

	svc := New(nil, "test")

	// InstanceName should return empty string before Start() is called
	assert.Empty(t, svc.InstanceName())
}

func TestInstanceNameWhenDiscoveryDisabled(t *testing.T) {
	t.Parallel()

	// Create a test config with discovery disabled
	configDir := t.TempDir()
	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)

	// Disable discovery
	cfg.SetDiscoveryEnabled(false)

	svc := New(cfg, "test")
	err = svc.Start()
	require.NoError(t, err)

	// InstanceName should be empty when discovery is disabled
	assert.Empty(t, svc.InstanceName())
}

func TestInstanceNameUsesHostname(t *testing.T) {
	t.Parallel()

	// Create a test config with discovery enabled (default)
	configDir := t.TempDir()
	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)

	// Get expected hostname
	expectedHostname, err := os.Hostname()
	require.NoError(t, err)

	svc := New(cfg, "test")

	// Start will fail at zeroconf.Register, but instanceName is set
	// before Register is called, so we can still verify the resolution
	_ = svc.Start() // Ignore error - Register may fail without network

	// instanceName should be set to the hostname (since no config override)
	assert.Equal(t, expectedHostname, svc.InstanceName())
}

func TestInstanceNameUsesConfigOverride(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)

	// Set a custom instance name in config
	cfg.SetDiscoveryInstanceName("my-custom-name")

	svc := New(cfg, "test")
	_ = svc.Start() // Ignore error - Register may fail without network

	// instanceName should use the config override, not hostname
	assert.Equal(t, "my-custom-name", svc.InstanceName())
}

func TestIsVirtualInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ifName   string
		expected bool
	}{
		// Virtual interfaces that should be filtered
		{"docker0", "docker0", true},
		{"docker bridge", "docker-bridge", true},
		{"docker uppercase", "Docker0", true},
		{"bridge interface", "br-abc123", true},
		{"veth pair", "veth123abc", true},
		{"virbr libvirt", "virbr0", true},
		{"lxc container", "lxcbr0", true},
		{"lxd container", "lxdbr0", true},
		{"cni kubernetes", "cni0", true},
		{"flannel overlay", "flannel.1", true},
		{"calico interface", "cali123", true},
		{"tunnel interface", "tunl0", true},
		{"wireguard", "wg0", true},
		{"wireguard numbered", "wg1", true},

		// Real interfaces that should NOT be filtered
		{"ethernet", "eth0", false},
		{"ethernet enp", "enp3s0", false},
		{"wifi wlan", "wlan0", false},
		{"wifi wlp", "wlp2s0", false},
		{"loopback", "lo", false},
		{"bond interface", "bond0", false},
		{"team interface", "team0", false},
		{"macos ethernet", "en0", false},
		{"macos wifi", "en1", false},
		{"windows ethernet", "Ethernet", false},
		{"windows wifi", "Wi-Fi", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isVirtualInterface(tt.ifName)
			assert.Equal(t, tt.expected, result, "isVirtualInterface(%q)", tt.ifName)
		})
	}
}

func TestVirtualInterfacePrefixes(t *testing.T) {
	t.Parallel()

	// Ensure all expected prefixes are in the list
	expectedPrefixes := []string{
		"docker", "br-", "veth", "virbr", "lxc", "lxd",
		"cni", "flannel", "cali", "tunl", "wg",
	}

	assert.Equal(t, expectedPrefixes, virtualInterfacePrefixes,
		"virtualInterfacePrefixes should contain all expected virtual interface prefixes")
}

func TestFilterInterfaces(t *testing.T) {
	t.Parallel()

	// Helper flags for readability
	const (
		up        = net.FlagUp
		loopback  = net.FlagLoopback
		multicast = net.FlagMulticast
	)

	tests := []struct {
		name     string
		input    []net.Interface
		expected []string // expected interface names in result
	}{
		{
			name:     "empty input",
			input:    []net.Interface{},
			expected: []string{},
		},
		{
			name: "filters down interfaces",
			input: []net.Interface{
				{Name: "eth0", Flags: up | multicast},
				{Name: "eth1", Flags: multicast}, // down (no FlagUp)
			},
			expected: []string{"eth0"},
		},
		{
			name: "filters loopback",
			input: []net.Interface{
				{Name: "eth0", Flags: up | multicast},
				{Name: "lo", Flags: up | loopback | multicast},
			},
			expected: []string{"eth0"},
		},
		{
			name: "filters non-multicast interfaces",
			input: []net.Interface{
				{Name: "eth0", Flags: up | multicast},
				{Name: "tun0", Flags: up}, // no multicast
			},
			expected: []string{"eth0"},
		},
		{
			name: "filters virtual interfaces",
			input: []net.Interface{
				{Name: "eth0", Flags: up | multicast},
				{Name: "docker0", Flags: up | multicast},
				{Name: "br-abc123", Flags: up | multicast},
				{Name: "veth123", Flags: up | multicast},
			},
			expected: []string{"eth0"},
		},
		{
			name: "keeps multiple valid interfaces",
			input: []net.Interface{
				{Name: "eth0", Flags: up | multicast},
				{Name: "wlan0", Flags: up | multicast},
				{Name: "enp3s0", Flags: up | multicast},
			},
			expected: []string{"eth0", "wlan0", "enp3s0"},
		},
		{
			name: "mixed filtering scenario",
			input: []net.Interface{
				{Name: "lo", Flags: up | loopback | multicast},         // filtered: loopback
				{Name: "eth0", Flags: up | multicast},                  // kept
				{Name: "eth1", Flags: multicast},                       // filtered: down
				{Name: "docker0", Flags: up | multicast},               // filtered: virtual
				{Name: "wlan0", Flags: up | multicast},                 // kept
				{Name: "virbr0", Flags: up | multicast},                // filtered: virtual
				{Name: "tun0", Flags: up},                              // filtered: no multicast
				{Name: "enp3s0", Flags: up | multicast},                // kept
				{Name: "br-network", Flags: up | multicast},            // filtered: virtual
				{Name: "veth123abc", Flags: up | loopback | multicast}, // filtered: loopback + virtual
			},
			expected: []string{"eth0", "wlan0", "enp3s0"},
		},
		{
			name: "all interfaces filtered returns empty",
			input: []net.Interface{
				{Name: "lo", Flags: up | loopback | multicast},
				{Name: "docker0", Flags: up | multicast},
				{Name: "tun0", Flags: up},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := filterInterfaces(tt.input)

			// Extract names from result for easier comparison
			resultNames := make([]string, len(result))
			for i, iface := range result {
				resultNames[i] = iface.Name
			}

			assert.Equal(t, tt.expected, resultNames)
		})
	}
}

func TestGetPreferredInterfaces(t *testing.T) {
	t.Parallel()

	// This test verifies getPreferredInterfaces works on the real system.
	// We can't predict exact results, but we can verify invariants.
	ifaces, err := getPreferredInterfaces()
	require.NoError(t, err)

	for _, iface := range ifaces {
		// All returned interfaces must be up
		assert.NotEqual(t, iface.Flags&net.FlagUp, 0,
			"interface %s should be up", iface.Name)

		// None should be loopback
		assert.Equal(t, iface.Flags&net.FlagLoopback, 0,
			"interface %s should not be loopback", iface.Name)

		// All should support multicast
		assert.NotEqual(t, iface.Flags&net.FlagMulticast, 0,
			"interface %s should support multicast", iface.Name)

		// None should be virtual interfaces
		assert.False(t, isVirtualInterface(iface.Name),
			"interface %s should not be a virtual interface", iface.Name)
	}
}
