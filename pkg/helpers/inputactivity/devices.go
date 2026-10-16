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
	"bufio"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

// ProcDevicesPath is the kernel's list of input devices.
const ProcDevicesPath = "/proc/bus/input/devices"

// Linux input event types and key codes used to classify a device.
const (
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	keyQ = 16
	keyA = 30
	keyZ = 44

	btnJoystick = 0x120
	btnGamepad  = 0x130
	btnMouse    = 0x110
	btnLast     = 0x13f

	ownDeviceName = "zaparoo"
)

// InputDevice is one entry of /proc/bus/input/devices.
type InputDevice struct {
	bitmaps  map[string][]uint64
	Name     string
	Phys     string
	Sysfs    string
	Uniq     string
	Handlers []string
}

// ParseDevices reads the /proc/bus/input/devices format. Unknown lines are
// ignored so a newer kernel cannot break enumeration.
func ParseDevices(r io.Reader) []InputDevice {
	var devices []InputDevice
	cur := InputDevice{bitmaps: map[string][]uint64{}}
	started := false
	flush := func() {
		if started {
			devices = append(devices, cur)
		}
		cur = InputDevice{bitmaps: map[string][]uint64{}}
		started = false
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			flush()
			continue
		}
		if len(line) < 3 || line[1] != ':' {
			continue
		}
		started = true
		body := strings.TrimSpace(line[2:])
		switch line[0] {
		case 'N':
			cur.Name = unquote(strings.TrimPrefix(body, "Name="))
		case 'P':
			cur.Phys = strings.TrimPrefix(body, "Phys=")
		case 'S':
			cur.Sysfs = strings.TrimPrefix(body, "Sysfs=")
		case 'U':
			cur.Uniq = strings.TrimPrefix(body, "Uniq=")
		case 'H':
			cur.Handlers = strings.Fields(strings.TrimPrefix(body, "Handlers="))
		case 'B':
			name, value, ok := strings.Cut(body, "=")
			if ok {
				cur.bitmaps[name] = parseBitmap(value)
			}
		}
	}
	flush()
	return devices
}

// ReadDevices parses the device list at name on fs.
func ReadDevices(fs afero.Fs, name string) ([]InputDevice, error) {
	f, err := fs.Open(name)
	if err != nil {
		return nil, err //nolint:wrapcheck // callers only test the error
	}
	defer func() { _ = f.Close() }()
	return ParseDevices(f), nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseBitmap decodes the kernel's bitmap text: words are written most
// significant first and are as wide as the kernel's long. The result is
// least significant word first.
func parseBitmap(value string) []uint64 {
	fields := strings.Fields(value)
	words := make([]uint64, 0, len(fields))
	for i := len(fields) - 1; i >= 0; i-- {
		word, err := strconv.ParseUint(fields[i], 16, 64)
		if err != nil {
			return nil
		}
		words = append(words, word)
	}
	return words
}

func (d *InputDevice) hasBit(bitmap string, bit int) bool {
	words := d.bitmaps[bitmap]
	idx := bit / strconv.IntSize
	if bit < 0 || idx >= len(words) {
		return false
	}
	return words[idx]&(1<<uint(bit%strconv.IntSize)) != 0
}

func (d *InputDevice) hasAnyKey(from, to int) bool {
	for code := from; code <= to; code++ {
		if d.hasBit("KEY", code) {
			return true
		}
	}
	return false
}

// HasAbs reports whether the device advertises the absolute axis code.
func (d *InputDevice) HasAbs(code int) bool {
	return d.hasBit("ABS", code)
}

// handler returns the first handler for which match is true.
func (d *InputDevice) handler(match func(string) bool) (string, bool) {
	for _, h := range d.Handlers {
		if match(h) {
			return h, true
		}
	}
	return "", false
}

func isNumbered(prefix string) func(string) bool {
	return func(h string) bool {
		rest, ok := strings.CutPrefix(h, prefix)
		if !ok || rest == "" {
			return false
		}
		_, err := strconv.Atoi(rest)
		return err == nil
	}
}

// EventNode returns the evdev node name such as "event3".
func (d *InputDevice) EventNode() (string, bool) {
	return d.handler(isNumbered("event"))
}

// JoystickNode returns the legacy joystick node name such as "js0".
func (d *InputDevice) JoystickNode() (string, bool) {
	return d.handler(isNumbered("js"))
}

// Own reports whether the device is one Zaparoo created itself.
func (d *InputDevice) Own() bool {
	return strings.Contains(strings.ToLower(d.Name), ownDeviceName)
}

// uinputPath is where the kernel puts devices created through uinput.
// Bluetooth HID devices created by BlueZ through uhid live under
// /devices/virtual/misc/uhid/ instead and are real hardware.
const uinputPath = "/devices/virtual/input/"

// Synthetic reports whether the device was created by software through
// uinput rather than backed by hardware.
func (d *InputDevice) Synthetic() bool {
	return strings.Contains(d.Sysfs, uinputPath)
}

// Skipped reports whether the device must never count as a person. Only
// Zaparoo's own devices qualify: software that creates input devices for a
// real controller, such as Steam Input or ds4drv, is a person's input, and
// treating every uinput device as synthetic would leave that player invisible
// while coverage still claimed to see them. Devices that report nonstop
// without anyone behind them are the chatter filter's job.
func (d *InputDevice) Skipped() bool {
	return d.Own()
}

// Classify returns the single kind of input a device produces. A device with
// gamepad buttons is a controller even if it also exposes keys or a pointer,
// and a device with letter keys is a keyboard even if it has a wheel.
func (d *InputDevice) Classify() Class {
	switch {
	case d.hasAnyKey(btnJoystick, btnLast):
		return Controller
	case d.hasBit("KEY", keyA) && d.hasBit("KEY", keyZ) && d.hasBit("KEY", keyQ):
		return Keyboard
	case d.hasBit("EV", evRel) || d.hasAnyKey(btnMouse, btnMouse+7):
		return Mouse
	default:
		return 0
	}
}

// UsbParent returns the physical parent shared by every interface of one USB
// device, or "" when the device has no such notion (Bluetooth, PS/2).
func (d *InputDevice) UsbParent() string {
	return physParent(d.Phys)
}

func physParent(phys string) string {
	idx := strings.LastIndex(phys, "/")
	if idx <= 0 {
		return ""
	}
	return phys[:idx]
}

// hidrawForPhys finds the /dev/hidrawN node whose HID_PHYS is phys.
func hidrawForPhys(fs afero.Fs, sysClassHidraw, phys string) (string, bool) {
	if phys == "" {
		return "", false
	}
	entries, err := afero.ReadDir(fs, sysClassHidraw)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		name := entry.Name()
		if !isNumbered("hidraw")(name) {
			continue
		}
		data, err := afero.ReadFile(fs, path.Join(sysClassHidraw, name, "device", "uevent"))
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if got, ok := strings.CutPrefix(strings.TrimSpace(line), "HID_PHYS="); ok && got == phys {
				return name, true
			}
		}
	}
	return "", false
}
