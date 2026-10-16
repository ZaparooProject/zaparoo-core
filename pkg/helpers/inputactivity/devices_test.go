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

package inputactivity

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bitmapText renders a kernel bitmap line value with the given bits set, in
// the kernel's word width.
func bitmapText(bits ...int) string {
	maxBit := 0
	for _, b := range bits {
		maxBit = max(maxBit, b)
	}
	words := make([]uint64, maxBit/strconv.IntSize+1)
	for _, b := range bits {
		words[b/strconv.IntSize] |= 1 << uint(b%strconv.IntSize)
	}
	parts := make([]string, 0, len(words))
	for i := len(words) - 1; i >= 0; i-- {
		parts = append(parts, strconv.FormatUint(words[i], 16))
	}
	return strings.Join(parts, " ")
}

type procEntry struct {
	name     string
	phys     string
	sysfs    string
	handlers string
	ev       []int
	key      []int
	abs      []int
}

func procText(entries ...procEntry) string {
	var b strings.Builder
	for i := range entries {
		e := &entries[i]
		fmt.Fprintf(&b, "I: Bus=0003 Vendor=%04x Product=0001 Version=0111\n", i+1)
		fmt.Fprintf(&b, "N: Name=\"%s\"\n", e.name)
		fmt.Fprintf(&b, "P: Phys=%s\n", e.phys)
		fmt.Fprintf(&b, "S: Sysfs=%s\n", e.sysfs)
		b.WriteString("U: Uniq=\n")
		fmt.Fprintf(&b, "H: Handlers=%s\n", e.handlers)
		b.WriteString("B: PROP=0\n")
		fmt.Fprintf(&b, "B: EV=%s\n", bitmapText(e.ev...))
		if len(e.key) > 0 {
			fmt.Fprintf(&b, "B: KEY=%s\n", bitmapText(e.key...))
		}
		if len(e.abs) > 0 {
			fmt.Fprintf(&b, "B: ABS=%s\n", bitmapText(e.abs...))
		}
		b.WriteString("\n")
	}
	return b.String()
}

var (
	letterKeys = []int{1, 16, 30, 44, 57}
	padButtons = []int{0x130, 0x131, 0x133}
	mouseKeys  = []int{0x110, 0x111}
)

func keyboardEntry(name, phys, handlers string) procEntry {
	return procEntry{
		name: name, phys: phys, sysfs: "/devices/pci0000:00/usb1/1-2/input/" + name,
		handlers: handlers, ev: []int{evKey}, key: letterKeys,
	}
}

func padEntry(name, phys, handlers string) procEntry {
	return procEntry{
		name: name, phys: phys, sysfs: "/devices/pci0000:00/usb1/1-3/input/" + name,
		handlers: handlers, ev: []int{evKey, evAbs}, key: padButtons, abs: []int{0, 1, 16},
	}
}

func mouseEntry(name, phys, handlers string) procEntry {
	return procEntry{
		name: name, phys: phys, sysfs: "/devices/pci0000:00/usb1/1-4/input/" + name,
		handlers: handlers, ev: []int{evKey, evRel}, key: mouseKeys,
	}
}

func TestParseAndClassify(t *testing.T) {
	t.Parallel()
	text := procText(
		keyboardEntry("Logitech Keyboard", "usb-0000:00:14.0-2/input0", "sysrq kbd event3 leds"),
		padEntry("Xbox Pad", "usb-0000:00:14.0-3/input0", "event4 js0"),
		mouseEntry("Gaming Mouse", "usb-0000:00:14.0-4/input0", "mouse0 event5"),
		procEntry{
			name: "Power Button", phys: "LNXPWRBN/button/input0", sysfs: "/devices/LNXSYSTM:00/input/input0",
			handlers: "kbd event0", ev: []int{evKey}, key: []int{116},
		},
		procEntry{
			name: "Zaparoo Keyboard", phys: "", sysfs: "/devices/virtual/input/input9",
			handlers: "sysrq kbd event9", ev: []int{evKey}, key: letterKeys,
		},
		procEntry{
			name: "Motion Sensors", phys: "", sysfs: "/devices/x/input/input10",
			handlers: "event10", ev: []int{evAbs}, abs: []int{0, 1, 2},
		},
	)
	devs := ParseDevices(strings.NewReader(text))
	require.Len(t, devs, 6)

	tests := []struct {
		name     string
		class    Class
		skipped  bool
		hasEvent bool
	}{
		{"Logitech Keyboard", Keyboard, false, true},
		{"Xbox Pad", Controller, false, true},
		{"Gaming Mouse", Mouse, false, true},
		{"Power Button", 0, false, true},
		{"Zaparoo Keyboard", Keyboard, true, true},
		{"Motion Sensors", 0, false, true},
	}
	for i, tt := range tests {
		dev := &devs[i]
		assert.Equal(t, tt.name, dev.Name)
		assert.Equal(t, tt.class, devs[i].Classify(), tt.name)
		assert.Equal(t, tt.skipped, devs[i].Skipped(), tt.name)
		_, ok := devs[i].EventNode()
		assert.Equal(t, tt.hasEvent, ok, tt.name)
	}

	node, ok := devs[1].JoystickNode()
	assert.True(t, ok)
	assert.Equal(t, "js0", node)
	_, ok = devs[0].JoystickNode()
	assert.False(t, ok)
	assert.True(t, devs[1].HasAbs(16))
	assert.False(t, devs[1].HasAbs(5))
}

func TestSkippedOnlyForOwnDevices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		dev  InputDevice
		want bool
	}{
		{InputDevice{Name: "ZAPAROO Gamepad"}, true},
		{InputDevice{Name: "Steam Virtual Gamepad", Sysfs: "/devices/virtual/input/input2"}, false},
		{InputDevice{Name: "BT Pad", Sysfs: uhidSysfs + "/input/input21"}, false},
		{InputDevice{Name: "Pad", Sysfs: "/devices/pci0000:00/usb1/input/input2"}, false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.dev.Skipped(), tt.dev.Name+tt.dev.Sysfs)
	}
}

func TestParseDevicesToleratesJunk(t *testing.T) {
	t.Parallel()
	devs := ParseDevices(strings.NewReader("garbage\nN: Name=\"A\"\nB: KEY=zz\nX: what\n\n\n\nN: Name=\"B\"\n"))
	require.Len(t, devs, 2)
	assert.Equal(t, "A", devs[0].Name)
	assert.Equal(t, Class(0), devs[0].Classify())
	assert.Empty(t, ParseDevices(strings.NewReader("")))
}

func TestUsbParent(t *testing.T) {
	t.Parallel()
	tests := []struct{ phys, want string }{
		{"usb-0000:00:14.0-2/input0", "usb-0000:00:14.0-2"},
		{"usb-0000:00:14.0-2/input1", "usb-0000:00:14.0-2"},
		{"aa:bb:cc:dd:ee:ff", ""},
		{"", ""},
		{"/input0", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, (&InputDevice{Phys: tt.phys}).UsbParent(), tt.phys)
	}
}

func TestPlanSAM(t *testing.T) {
	t.Parallel()
	text := procText(
		keyboardEntry("Real Keyboard", "usb-0000:00:14.0-2/input0", "sysrq kbd event3"),
		padEntry("Pad A", "usb-0000:00:14.0-3/input0", "event4 js0"),
		// The pad's own keyboard interface shares its USB parent.
		keyboardEntry("Pad A Keyboard", "usb-0000:00:14.0-3/input1", "sysrq kbd event6"),
		padEntry("Bluetooth Pad", "aa:bb:cc:dd:ee:ff", "event7 js1"),
		keyboardEntry("Bluetooth Keyboard", "aa:bb:cc:dd:ee:ff", "sysrq kbd event8"),
		padEntry("Zaparoo Pad", "", "event9 js2"),
	)
	plan := planSAM(ParseDevices(strings.NewReader(text)))
	assert.Equal(t, []string{"js0", "js1"}, plan.joysticks)

	names := make([]string, 0, len(plan.keyboards))
	for _, kb := range plan.keyboards {
		names = append(names, kb.Name)
	}
	assert.Equal(t, []string{"Real Keyboard", "Bluetooth Keyboard"}, names)
}

func TestHidrawForPhys(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	write := func(node, uevent string) {
		require.NoError(t, afero.WriteFile(fs, "/sys/class/hidraw/"+node+"/device/uevent", []byte(uevent), 0o644))
	}
	write("hidraw0", "DRIVER=hid-generic\nHID_ID=0003:0001\nHID_PHYS=usb-0000:00:14.0-3/input1\n")
	write("hidraw1", "HID_ID=0003:0002\nHID_PHYS=usb-0000:00:14.0-2/input0\nHID_NAME=Kbd\n")
	write("hidraw10", "HID_PHYS=usb-0000:00:14.0-2/input10\n")
	require.NoError(t, fs.MkdirAll("/sys/class/hidraw/notanode", 0o755))

	node, ok := hidrawForPhys(fs, "/sys/class/hidraw", "usb-0000:00:14.0-2/input0")
	assert.True(t, ok)
	assert.Equal(t, "hidraw1", node)

	_, ok = hidrawForPhys(fs, "/sys/class/hidraw", "usb-0000:00:14.0-2/input")
	assert.False(t, ok, "phys must match exactly")
	_, ok = hidrawForPhys(fs, "/sys/class/hidraw", "")
	assert.False(t, ok)
	_, ok = hidrawForPhys(fs, "/nope", "x")
	assert.False(t, ok)
}

const uhidSysfs = "/devices/virtual/misc/uhid/0005:045E:0B13.0003"

func bluetoothEntries() []procEntry {
	return []procEntry{
		{
			name: "BT Keyboard", phys: "aa:bb:cc:dd:ee:01", sysfs: uhidSysfs + "/input/input20",
			handlers: "sysrq kbd event20", ev: []int{evKey}, key: letterKeys,
		},
		{
			name: "BT Mouse", phys: "aa:bb:cc:dd:ee:01", sysfs: uhidSysfs + "/input/input21",
			handlers: "mouse1 event21", ev: []int{evKey, evRel}, key: mouseKeys,
		},
		{
			name: "BT Gamepad", phys: "aa:bb:cc:dd:ee:01", sysfs: uhidSysfs + "/input/input22",
			handlers: "event22 js3", ev: []int{evKey, evAbs}, key: padButtons, abs: []int{0, 1, 16},
		},
	}
}

func TestBluetoothUhidDevicesAreNotSkipped(t *testing.T) {
	t.Parallel()
	devs := ParseDevices(strings.NewReader(procText(bluetoothEntries()...)))
	require.Len(t, devs, 3)
	want := []Class{Keyboard, Mouse, Controller}
	for i := range devs {
		assert.False(t, devs[i].Skipped(), devs[i].Name)
		assert.Equal(t, want[i], devs[i].Classify(), devs[i].Name)
	}

	plan := planSAM(devs)
	assert.Equal(t, []string{"js3"}, plan.joysticks)
	require.Len(t, plan.keyboards, 1)
	assert.Equal(t, "BT Keyboard", plan.keyboards[0].Name)
}
