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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const routeHeader = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

func TestParseDefaultRouteInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		routes string
		want   string
	}{
		{name: "empty table", routes: "", want: ""},
		{name: "header only", routes: routeHeader, want: ""},
		{
			name: "single default route",
			routes: routeHeader +
				"wlan0\t00000000\t0101A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
				"wlan0\t0001A8C0\t00000000\t0001\t0\t0\t600\t00FFFFFF\t0\t0\t0\n",
			want: "wlan0",
		},
		{
			name: "the lowest metric wins",
			routes: routeHeader +
				"wlan0\t00000000\t0101A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
				"eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
			want: "eth0",
		},
		{
			name: "a route that is not up is ignored",
			routes: routeHeader +
				"eth0\t00000000\t0101A8C0\t0002\t0\t0\t100\t00000000\t0\t0\t0\n",
			want: "",
		},
		{
			name:   "a truncated row is ignored",
			routes: routeHeader + "eth0\t00000000\n",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ParseDefaultRouteInterface(tt.routes))
		})
	}
}

const (
	dualSenseAlias = "input:b0005v054Cp0CE6e8100-e0,1,3,k130,131,132,133,134,135,136,137,138,139,13A,13B,13C,13D," +
		"ra0,1,2,3,4,5,10,11,m4,lsfw"
	xboxAlias = "input:b0003v045Ep028Ee0114-e0,1,3,15,k130,131,133,134,136,137,13A,13B,13C,13D,13E," +
		"ra0,1,2,3,4,5,10,11,mlsf50,51,58,59,5A,60,w"
	keyboardAlias = "input:b0003v046DpC31Ce0110-e0,1,4,11,14,k71,72,73,74,75,77,79,7A,7B,7C,7D,7E,7F,80," +
		"1,2,3,1E,ram4,l0,1,2,sfw"
	mouseAlias    = "input:b0003v046DpC077e0111-e0,1,2,4,k110,111,112,r0,1,8,B,am4,lsfw"
	touchpadAlias = "input:b0005v054Cp0CE6e8100-e0,1,3,k110,145,14A,14D,ra0,1,2F,35,36,39,mlsfw"
)

func TestParseInputModalias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		alias      string
		connection string
		vendor     uint16
		product    uint16
		gamepad    bool
	}{
		{
			name: "bluetooth pad", alias: dualSenseAlias, vendor: 0x054c, product: 0x0ce6,
			gamepad: true, connection: ConnectionBluetooth,
		},
		{name: "usb pad", alias: xboxAlias, vendor: 0x045e, product: 0x028e, gamepad: true, connection: ConnectionUSB},
		{name: "keyboard", alias: keyboardAlias, vendor: 0x046d, product: 0xc31c, connection: ConnectionUSB},
		{name: "mouse", alias: mouseAlias, vendor: 0x046d, product: 0xc077, connection: ConnectionUSB},
		{
			name: "a pad's touchpad is not a pad", alias: touchpadAlias, vendor: 0x054c, product: 0x0ce6,
			connection: ConnectionBluetooth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parsed, ok := ParseInputModalias(tt.alias + "\n")
			require.True(t, ok)
			assert.Equal(t, tt.vendor, parsed.Vendor)
			assert.Equal(t, tt.product, parsed.Product)
			assert.Equal(t, tt.gamepad, parsed.IsGamepad())
			assert.Equal(t, tt.connection, parsed.Connection())
		})
	}
}

func TestParseInputModalias_Rejects(t *testing.T) {
	t.Parallel()

	rejected := []string{
		"", "usb:v1234", "input:b0003", "input:bZZZZv046DpC31Ce0110-e0", "input:b0003x046DpC31Ce0110-",
	}
	for _, alias := range rejected {
		_, ok := ParseInputModalias(alias)
		assert.False(t, ok, alias)
	}
}

func TestParseInputModalias_KeysStopAtNextGroup(t *testing.T) {
	t.Parallel()

	parsed, ok := ParseInputModalias("input:b0019v0000p0001e0000-e0,1,k74,ramlsfw")
	require.True(t, ok)
	assert.Equal(t, []uint16{0x74}, parsed.Keys)
	assert.Equal(t, ConnectionUnknown, parsed.Connection())
}

func TestConnectorName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		entry string
		want  string
		ok    bool
	}{
		{entry: "card0-eDP-1", want: "eDP-1", ok: true},
		{entry: "card12-HDMI-A-1", want: "HDMI-A-1", ok: true},
		{entry: "card0", ok: false},
		{entry: "card-eDP-1", ok: false},
		{entry: "cardX-eDP-1", ok: false},
		{entry: "card0-", ok: false},
		{entry: "renderD128", ok: false},
		{entry: "version", ok: false},
	}
	for _, tt := range tests {
		name, ok := ConnectorName(tt.entry)
		assert.Equal(t, tt.ok, ok, tt.entry)
		assert.Equal(t, tt.want, name, tt.entry)
	}
}

func boolPtr(value bool) *bool { return &value }

func TestClassifyDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		want       Display
		connectors []Connector
	}{
		{
			name:       "handheld on its own screen",
			connectors: []Connector{{Name: "eDP-1", Connected: true, Enabled: true}, {Name: "DP-1"}},
			want:       Display{InternalPanel: true, InternalActive: true, Docked: boolPtr(false)},
		},
		{
			name: "handheld driving an external display",
			connectors: []Connector{
				{Name: "eDP-1", Connected: true},
				{Name: "DP-1", Connected: true, Enabled: true},
			},
			want: Display{InternalPanel: true, ExternalConnected: true, ExternalActive: true, Docked: boolPtr(true)},
		},
		{
			name: "an external display that is plugged in but not driven is not docked",
			connectors: []Connector{
				{Name: "DSI-1", Connected: true, Enabled: true},
				{Name: "HDMI-A-1", Connected: true},
			},
			want: Display{InternalPanel: true, InternalActive: true, ExternalConnected: true, Docked: boolPtr(false)},
		},
		{
			name:       "a machine with no panel has no docked state",
			connectors: []Connector{{Name: "HDMI-A-1", Connected: true, Enabled: true}},
			want:       Display{ExternalConnected: true, ExternalActive: true},
		},
		{
			name: "writeback and virtual outputs are not screens",
			connectors: []Connector{
				{Name: "eDP-1", Connected: true, Enabled: true},
				{Name: "Writeback-1", Connected: true, Enabled: true},
				{Name: "Virtual-1", Connected: true, Enabled: true},
			},
			want: Display{InternalPanel: true, InternalActive: true, Docked: boolPtr(false)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ClassifyDisplay(tt.connectors))
		})
	}
}

func TestBatteryLevel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, BatteryLevelFull, BatteryLevel(100))
	assert.Equal(t, BatteryLevelFull, BatteryLevel(75))
	assert.Equal(t, BatteryLevelMedium, BatteryLevel(74))
	assert.Equal(t, BatteryLevelMedium, BatteryLevel(40))
	assert.Equal(t, BatteryLevelLow, BatteryLevel(39))
	assert.Equal(t, BatteryLevelLow, BatteryLevel(15))
	assert.Equal(t, BatteryLevelEmpty, BatteryLevel(14))
}

func TestParseRouteDefaultInterface(t *testing.T) {
	t.Parallel()

	output := "   route to: default\ndestination: default\n       mask: default\n" +
		"    gateway: 192.168.1.1\n  interface: en0\n      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>\n"
	assert.Equal(t, "en0", ParseRouteDefaultInterface(output))
	assert.Empty(t, ParseRouteDefaultInterface("route: writing to routing socket: not in table\n"))
}

func TestParseHardwarePorts(t *testing.T) {
	t.Parallel()

	output := "\nHardware Port: Ethernet\nDevice: en0\nEthernet Address: aa:bb:cc:dd:ee:ff\n\n" +
		"Hardware Port: Wi-Fi\nDevice: en1\nEthernet Address: aa:bb:cc:dd:ee:00\n\n" +
		"Hardware Port: Thunderbolt Bridge\nDevice: bridge0\nEthernet Address: aa:bb:cc:dd:ee:01\n\n" +
		"Hardware Port: USB 10/100/1000 LAN\nDevice: en5\nEthernet Address: aa:bb:cc:dd:ee:02\n"
	assert.Equal(t, map[string]LinkType{
		"en0":     LinkWired,
		"en1":     LinkWifi,
		"bridge0": LinkOther,
		"en5":     LinkWired,
	}, ParseHardwarePorts(output))
}

func FuzzParseDefaultRouteInterface(f *testing.F) {
	f.Add(routeHeader + "wlan0\t00000000\t0101A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n")
	f.Add("")
	f.Fuzz(func(_ *testing.T, routes string) {
		ParseDefaultRouteInterface(routes)
	})
}

func FuzzParseInputModalias(f *testing.F) {
	f.Add(dualSenseAlias)
	f.Add(keyboardAlias)
	f.Add("input:b")
	f.Fuzz(func(_ *testing.T, alias string) {
		if parsed, ok := ParseInputModalias(alias); ok {
			parsed.IsGamepad()
			parsed.Connection()
		}
	})
}

func FuzzConnectorName(f *testing.F) {
	f.Add("card0-eDP-1")
	f.Add("card")
	f.Fuzz(func(t *testing.T, entry string) {
		if name, ok := ConnectorName(entry); ok && name == "" {
			t.Fatal("accepted an empty connector name")
		}
	})
}
