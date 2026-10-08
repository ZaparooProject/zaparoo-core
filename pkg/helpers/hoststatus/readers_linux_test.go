//go:build linux && !android

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
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBus answers system bus calls from a table keyed by service and the
// final argument-qualified method.
type fakeBus struct {
	replies map[string][]any
	calls   []string
}

func (b *fakeBus) call(
	_ context.Context,
	service string,
	_ dbus.ObjectPath,
	method string,
	args ...any,
) ([]any, error) {
	parts := []string{service, method}
	for _, arg := range args {
		if text, ok := arg.(string); ok {
			parts = append(parts, text)
		}
	}
	key := strings.Join(parts, " ")
	b.calls = append(b.calls, key)
	reply, ok := b.replies[key]
	if !ok {
		return nil, errNoService
	}
	return reply, nil
}

func variant(value any) []any { return []any{dbus.MakeVariant(value)} }

func writeFiles(t *testing.T, fs afero.Fs, files map[string]string) {
	t.Helper()
	for path, content := range files {
		require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))
	}
}

func sys(parts ...string) string {
	return filepath.Join(append([]string{string(filepath.Separator), "sys"}, parts...)...)
}

func newNetworkReader(fs afero.Fs, bus busCaller, raw []RawInterface) *LinuxNetwork {
	return &LinuxNetwork{
		Fs:         fs,
		Interfaces: func() ([]RawInterface, error) { return raw, nil },
		bus:        bus,
		ProcRoute:  filepath.Join(string(filepath.Separator), "proc", "net", "route"),
		SysNet:     sys("class", "net"),
	}
}

func TestLinuxNetwork_Read(t *testing.T) {
	t.Parallel()

	raw := []RawInterface{
		{Name: "lo", Loopback: true, Up: true, Addresses: []net.IP{net.ParseIP("127.0.0.1")}},
		{Name: "wlan0", Up: true, Addresses: []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("fe80::1")}},
		{Name: "eth0", Up: false},
		{Name: "docker0", Up: true, Addresses: []net.IP{net.ParseIP("172.17.0.1")}},
	}
	fs := afero.NewMemMapFs()
	writeFiles(t, fs, map[string]string{
		filepath.Join(string(filepath.Separator), "proc", "net", "route"): routeHeader +
			"wlan0\t00000000\t0101A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n",
		sys("class", "net", "wlan0", "operstate"):     "up\n",
		sys("class", "net", "wlan0", "wireless", "x"): "",
		sys("class", "net", "wlan0", "type"):          "1\n",
		sys("class", "net", "eth0", "operstate"):      "down\n",
		sys("class", "net", "eth0", "type"):           "1\n",
		sys("class", "net", "eth0", "device", "x"):    "",
		sys("class", "net", "docker0", "operstate"):   "up\n",
		sys("class", "net", "docker0", "type"):        "1\n",
	})

	network, err := newNetworkReader(fs, nil, raw).Read()
	require.NoError(t, err)
	assert.Equal(t, Network{
		Type:      LinkWifi,
		Interface: "wlan0",
		Interfaces: []Interface{
			{Name: "eth0", Type: LinkWired, Up: false, Addresses: []string{}},
			{Name: "wlan0", Type: LinkWifi, Up: true, Addresses: []string{"192.168.1.20"}},
		},
	}, network)
}

func TestLinuxNetwork_NoDefaultRouteIsOffline(t *testing.T) {
	t.Parallel()

	raw := []RawInterface{{Name: "eth0", Up: false}}
	fs := afero.NewMemMapFs()
	writeFiles(t, fs, map[string]string{
		filepath.Join(string(filepath.Separator), "proc", "net", "route"): routeHeader,
		sys("class", "net", "eth0", "type"):                               "1\n",
		sys("class", "net", "eth0", "device", "x"):                        "",
	})

	network, err := newNetworkReader(fs, nil, raw).Read()
	require.NoError(t, err)
	assert.Equal(t, LinkNone, network.Type)
	assert.Equal(t, InternetNone, network.Internet)
	assert.True(t, network.InternetAuthoritative)
}

func TestLinuxNetwork_DefaultRouteOnDownLinkIsOffline(t *testing.T) {
	t.Parallel()

	raw := []RawInterface{{Name: "eth0", Up: true}}
	fs := afero.NewMemMapFs()
	writeFiles(t, fs, map[string]string{
		filepath.Join(string(filepath.Separator), "proc", "net", "route"): routeHeader +
			"eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
		sys("class", "net", "eth0", "operstate"):   "down\n",
		sys("class", "net", "eth0", "type"):        "1\n",
		sys("class", "net", "eth0", "device", "x"): "",
	})

	network, err := newNetworkReader(fs, nil, raw).Read()
	require.NoError(t, err)
	assert.Equal(t, LinkNone, network.Type)
}

func TestLinuxNetwork_NetworkManager(t *testing.T) {
	t.Parallel()

	raw := []RawInterface{{Name: "eth0", Up: true}}
	prefix := nmService + " " + dbusPropertiesGet + " " + nmService + " "

	tests := []struct {
		replies       map[string][]any
		name          string
		want          InternetState
		authoritative bool
	}{
		{
			name: "full when it is checking",
			replies: map[string][]any{
				prefix + "ConnectivityCheckAvailable": variant(true),
				prefix + "ConnectivityCheckEnabled":   variant(true),
				prefix + "Connectivity":               variant(uint32(nmConnectivityFull)),
			},
			want: InternetFull, authoritative: true,
		},
		{
			name: "portal when it is checking",
			replies: map[string][]any{
				prefix + "ConnectivityCheckAvailable": variant(true),
				prefix + "ConnectivityCheckEnabled":   variant(true),
				prefix + "Connectivity":               variant(uint32(nmConnectivityPortal)),
			},
			want: InternetPortal, authoritative: true,
		},
		{
			name: "limited is no internet",
			replies: map[string][]any{
				prefix + "ConnectivityCheckAvailable": variant(true),
				prefix + "ConnectivityCheckEnabled":   variant(true),
				prefix + "Connectivity":               variant(uint32(nmConnectivityLimited)),
			},
			want: InternetNone, authoritative: true,
		},
		{
			name: "its guess is ignored when the check is disabled",
			replies: map[string][]any{
				prefix + "ConnectivityCheckAvailable": variant(true),
				prefix + "ConnectivityCheckEnabled":   variant(false),
				prefix + "Connectivity":               variant(uint32(nmConnectivityFull)),
			},
		},
		{
			name: "its guess is ignored when no check is configured",
			replies: map[string][]any{
				prefix + "ConnectivityCheckAvailable": variant(false),
				prefix + "Connectivity":               variant(uint32(nmConnectivityFull)),
			},
		},
		{
			name: "an unknown state is left to the probe",
			replies: map[string][]any{
				prefix + "ConnectivityCheckAvailable": variant(true),
				prefix + "ConnectivityCheckEnabled":   variant(true),
				prefix + "Connectivity":               variant(uint32(0)),
			},
		},
		{name: "not running", replies: map[string][]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			writeFiles(t, fs, map[string]string{
				filepath.Join(string(filepath.Separator), "proc", "net", "route"): routeHeader +
					"eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
				sys("class", "net", "eth0", "operstate"):   "up\n",
				sys("class", "net", "eth0", "type"):        "1\n",
				sys("class", "net", "eth0", "device", "x"): "",
			})
			network, err := newNetworkReader(fs, &fakeBus{replies: tt.replies}, raw).Read()
			require.NoError(t, err)
			assert.Equal(t, LinkWired, network.Type)
			assert.Equal(t, tt.want, network.Internet)
			assert.Equal(t, tt.authoritative, network.InternetAuthoritative)
		})
	}
}

func TestLinuxBluetooth_Read(t *testing.T) {
	t.Parallel()

	root := sys("class", "bluetooth")
	powered := bluezService + " " + dbusPropertiesGet + " " + bluezAdapter + " Powered"

	t.Run("no bluetooth class", func(t *testing.T) {
		t.Parallel()
		got, err := (&LinuxBluetooth{Fs: afero.NewMemMapFs(), SysRoot: root}).Read()
		require.NoError(t, err)
		assert.Equal(t, Bluetooth{}, got)
	})

	t.Run("connections are not adapters", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFiles(t, fs, map[string]string{filepath.Join(root, "hci0:256", "x"): ""})
		got, err := (&LinuxBluetooth{Fs: fs, SysRoot: root}).Read()
		require.NoError(t, err)
		assert.False(t, got.Present)
	})

	t.Run("bluetoothd says powered", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFiles(t, fs, map[string]string{filepath.Join(root, "hci0", "x"): ""})
		bus := &fakeBus{replies: map[string][]any{powered: variant(true)}}
		got, err := (&LinuxBluetooth{Fs: fs, bus: bus, SysRoot: root}).Read()
		require.NoError(t, err)
		assert.Equal(t, Bluetooth{Present: true, Powered: boolPtr(true)}, got)
	})

	t.Run("bluetoothd says off", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFiles(t, fs, map[string]string{filepath.Join(root, "hci0", "x"): ""})
		bus := &fakeBus{replies: map[string][]any{powered: variant(false)}}
		got, err := (&LinuxBluetooth{Fs: fs, bus: bus, SysRoot: root}).Read()
		require.NoError(t, err)
		assert.Equal(t, Bluetooth{Present: true, Powered: boolPtr(false)}, got)
	})

	t.Run("a blocked radio without bluetoothd is off", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFiles(t, fs, map[string]string{filepath.Join(root, "hci0", "rfkill3", "state"): "0\n"})
		got, err := (&LinuxBluetooth{Fs: fs, bus: &fakeBus{}, SysRoot: root}).Read()
		require.NoError(t, err)
		assert.Equal(t, Bluetooth{Present: true, Powered: boolPtr(false)}, got)
	})

	t.Run("an unblocked radio without bluetoothd is unknown", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFiles(t, fs, map[string]string{filepath.Join(root, "hci0", "rfkill3", "state"): "1\n"})
		got, err := (&LinuxBluetooth{Fs: fs, SysRoot: root}).Read()
		require.NoError(t, err)
		assert.Equal(t, Bluetooth{Present: true}, got)
	})
}

func TestLinuxDisplay_Read(t *testing.T) {
	t.Parallel()

	root := sys("class", "drm")

	t.Run("no drm class is unsupported", func(t *testing.T) {
		t.Parallel()
		_, err := (&LinuxDisplay{Fs: afero.NewMemMapFs(), DRMRoot: root}).Read()
		require.ErrorIs(t, err, ErrUnsupported)
	})

	t.Run("a card with no connectors is unsupported", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFiles(t, fs, map[string]string{filepath.Join(root, "card0", "dev"): "226:0\n"})
		_, err := (&LinuxDisplay{Fs: fs, DRMRoot: root}).Read()
		require.ErrorIs(t, err, ErrUnsupported)
	})

	t.Run("docked handheld", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFiles(t, fs, map[string]string{
			filepath.Join(root, "card0-eDP-1", "status"):  "connected\n",
			filepath.Join(root, "card0-eDP-1", "enabled"): "disabled\n",
			filepath.Join(root, "card0-DP-1", "status"):   "connected\n",
			filepath.Join(root, "card0-DP-1", "enabled"):  "enabled\n",
			filepath.Join(root, "version"):                "drm 1.1.0\n",
		})
		got, err := (&LinuxDisplay{Fs: fs, DRMRoot: root}).Read()
		require.NoError(t, err)
		assert.Equal(t, Display{
			InternalPanel: true, ExternalConnected: true, ExternalActive: true, Docked: boolPtr(true),
		}, got)
	})
}

func TestLinuxControllers_Read(t *testing.T) {
	t.Parallel()

	inputRoot := sys("class", "input")
	powerRoot := sys("class", "power_supply")
	devices := sys("devices")
	padDevice := filepath.Join(devices, "pci0000:00", "usb1", "1-1", "0005:054C:0CE6.0003")

	fs := afero.NewMemMapFs()
	writeFiles(t, fs, map[string]string{
		// A Bluetooth pad with a battery.
		filepath.Join(inputRoot, "input17", "modalias"): dualSenseAlias + "\n",
		filepath.Join(inputRoot, "input17", "name"):     "DualSense Wireless Controller\n",
		filepath.Join(inputRoot, "input17", "uniq"):     "aa:bb:cc:dd:ee:ff\n",
		// The same pad's touchpad node.
		filepath.Join(inputRoot, "input18", "modalias"): touchpadAlias + "\n",
		filepath.Join(inputRoot, "input18", "name"):     "DualSense Wireless Controller Touchpad\n",
		// A wired pad with no battery, listed out of order.
		filepath.Join(inputRoot, "input5", "modalias"): xboxAlias + "\n",
		filepath.Join(inputRoot, "input5", "name"):     "Microsoft X-Box 360 pad\n",
		// A keyboard.
		filepath.Join(inputRoot, "input2", "modalias"): keyboardAlias + "\n",
		filepath.Join(inputRoot, "input2", "name"):     "Keyboard\n",
		// Core's own virtual pad, and another program's.
		filepath.Join(inputRoot, "input30", "modalias"): "input:b0003v1234p5678e0000-e0,1,3,k130,131,ra0,1,mlsfw\n",
		filepath.Join(inputRoot, "input30", "name"):     "Zaparoo\n",
		filepath.Join(inputRoot, "input31", "modalias"): xboxAlias + "\n",
		filepath.Join(inputRoot, "input31", "name"):     "Microsoft X-Box 360 pad 0\n",
		// Not a device directory at all.
		filepath.Join(inputRoot, "event4", "dev"): "13:68\n",
		// The pad's battery, the machine's battery, and a mouse's.
		filepath.Join(powerRoot, "ps-controller-battery-aa:bb:cc:dd:ee:ff", "scope"):    "Device\n",
		filepath.Join(powerRoot, "ps-controller-battery-aa:bb:cc:dd:ee:ff", "capacity"): "80\n",
		filepath.Join(powerRoot, "BAT0", "capacity"):                                    "50\n",
		filepath.Join(powerRoot, "hidpp_battery_0", "scope"):                            "Device\n",
		filepath.Join(powerRoot, "hidpp_battery_0", "capacity"):                         "10\n",
	})

	realPaths := map[string]string{
		filepath.Join(inputRoot, "input17"): filepath.Join(padDevice, "input", "input17"),
		filepath.Join(inputRoot, "input5"):  filepath.Join(devices, "pci0000:00", "usb1", "1-2", "input", "input5"),
		filepath.Join(inputRoot, "input30"): filepath.Join(devices, "virtual", "input", "input30"),
		filepath.Join(inputRoot, "input31"): filepath.Join(devices, "virtual", "input", "input31"),
		filepath.Join(powerRoot, "ps-controller-battery-aa:bb:cc:dd:ee:ff"): filepath.Join(
			padDevice, "power_supply", "ps-controller-battery-aa:bb:cc:dd:ee:ff"),
		filepath.Join(powerRoot, "hidpp_battery_0"): filepath.Join(
			devices, "pci0000:00", "usb1", "1-3", "power_supply", "hidpp_battery_0"),
	}
	reader := &LinuxControllers{
		Fs: fs,
		Resolve: func(path string) (string, error) {
			if target, ok := realPaths[path]; ok {
				return target, nil
			}
			return "", errors.New("not a link")
		},
		InputRoot:   inputRoot,
		PowerRoot:   powerRoot,
		VirtualName: "Zaparoo",
	}

	controllers, err := reader.Read()
	require.NoError(t, err)
	percent := 80
	assert.Equal(t, []Controller{
		{
			ID: "input5", Name: "Microsoft X-Box 360 pad", VendorID: "045e", ProductID: "028e",
			Connection: ConnectionUSB,
		},
		{
			ID: "input17", Name: "DualSense Wireless Controller", VendorID: "054c", ProductID: "0ce6",
			Connection: ConnectionBluetooth,
			Battery:    &ControllerBattery{Percent: &percent, Level: BatteryLevelFull},
		},
	}, controllers)
}

func TestLinuxControllers_BatteryMatchedByAddress(t *testing.T) {
	t.Parallel()

	inputRoot := sys("class", "input")
	powerRoot := sys("class", "power_supply")
	fs := afero.NewMemMapFs()
	writeFiles(t, fs, map[string]string{
		filepath.Join(inputRoot, "input3", "modalias"):                                  dualSenseAlias,
		filepath.Join(inputRoot, "input3", "name"):                                      "Pad",
		filepath.Join(inputRoot, "input3", "uniq"):                                      "AA:BB:CC:DD:EE:FF",
		filepath.Join(powerRoot, "ps-controller-battery-aa:bb:cc:dd:ee:ff", "scope"):    "Device",
		filepath.Join(powerRoot, "ps-controller-battery-aa:bb:cc:dd:ee:ff", "capacity"): "12",
	})
	reader := &LinuxControllers{
		Fs:        fs,
		Resolve:   func(string) (string, error) { return "", errors.New("no links here") },
		InputRoot: inputRoot,
		PowerRoot: powerRoot,
	}

	controllers, err := reader.Read()
	require.NoError(t, err)
	require.Len(t, controllers, 1)
	require.NotNil(t, controllers[0].Battery)
	assert.Equal(t, 12, *controllers[0].Battery.Percent)
	assert.Equal(t, BatteryLevelEmpty, controllers[0].Battery.Level)
}

func TestLinuxControllers_NoInputClass(t *testing.T) {
	t.Parallel()

	reader := &LinuxControllers{Fs: afero.NewMemMapFs(), InputRoot: sys("class", "input")}
	_, err := reader.Read()
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestLinuxSystem_Read(t *testing.T) {
	t.Parallel()

	deviceTree := sys("firmware", "devicetree", "base", "model")
	dmi := sys("class", "dmi", "id", "product_name")
	hostname := func() (string, error) { return "handheld", nil }

	t.Run("device tree model", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFiles(t, fs, map[string]string{deviceTree: "Raspberry Pi 5 Model B Rev 1.0\x00"})
		got, err := (&LinuxSystem{Fs: fs, Hostname: hostname, ModelPaths: []string{deviceTree, dmi}}).Read()
		require.NoError(t, err)
		assert.Equal(t, System{Hostname: "handheld", Model: "Raspberry Pi 5 Model B Rev 1.0"}, got)
	})

	t.Run("firmware placeholder is not a model", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFiles(t, fs, map[string]string{dmi: "To Be Filled By O.E.M.\n"})
		got, err := (&LinuxSystem{Fs: fs, Hostname: hostname, ModelPaths: []string{deviceTree, dmi}}).Read()
		require.NoError(t, err)
		assert.Equal(t, System{Hostname: "handheld"}, got)
	})

	t.Run("hostname failure leaves it empty", func(t *testing.T) {
		t.Parallel()
		failing := func() (string, error) { return "", errors.New("no hostname") }
		got, err := (&LinuxSystem{Fs: afero.NewMemMapFs(), Hostname: failing}).Read()
		require.NoError(t, err)
		assert.Equal(t, System{}, got)
	})
}
