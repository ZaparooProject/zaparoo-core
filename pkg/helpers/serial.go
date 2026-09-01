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

package helpers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
)

type serialDevice struct {
	Vid string
	Pid string
}

var ignoreDevices = []serialDevice{
	// Sinden Lightgun
	{Vid: "16c0", Pid: "0f38"},
	{Vid: "16c0", Pid: "0f39"},
	{Vid: "16c0", Pid: "0f01"},
	{Vid: "16c0", Pid: "0f02"},
	{Vid: "16d0", Pid: "0f38"},
	{Vid: "16d0", Pid: "0f39"},
	{Vid: "16d0", Pid: "0f01"},
	{Vid: "16d0", Pid: "0f02"},
	{Vid: "16d0", Pid: "1094"},
	{Vid: "16d0", Pid: "1095"},
	{Vid: "16d0", Pid: "1096"},
	{Vid: "16d0", Pid: "1097"},
	{Vid: "16d0", Pid: "1098"},
	{Vid: "16d0", Pid: "1099"},
	{Vid: "16d0", Pid: "109a"},
	{Vid: "16d0", Pid: "109b"},
	{Vid: "16d0", Pid: "109c"},
	{Vid: "16d0", Pid: "109d"},
	// Epilogue Operator
	{Vid: "16d0", Pid: "123d"}, // GB Operator
	{Vid: "16d0", Pid: "123e"}, // SN Operator
	{Vid: "16d0", Pid: "134d"}, // 64 Operator
}

func ignoreSerialDevice(path string) bool {
	if _, err := os.Stat(path); err != nil { //nolint:gosec // G703: device path from OS
		return true
	}

	vid, pid, ok := SerialDeviceVIDPID(path)
	if !ok {
		return false
	}

	for _, v := range ignoreDevices {
		if vid == v.Vid && pid == v.Pid {
			return true
		}
	}
	return false
}

// SerialDeviceVIDPID returns the USB vendor and product IDs for a serial
// device, lowercased.
//
// The third return is false when the IDs cannot be determined: a non-USB
// device, or a platform without udevadm. A caller deciding whether to probe a
// port should treat unknown as "cannot rule out" rather than as a match, so a
// device stays reachable where this cannot answer.
func SerialDeviceVIDPID(path string) (vid, pid string, ok bool) {
	if _, err := os.Stat("/usr/bin/udevadm"); err != nil {
		log.Debug().Msg("udevadm not found, cannot read USB IDs")
		return "", "", false
	}

	// Validate device path to prevent command injection
	if !strings.HasPrefix(path, "/dev/") {
		log.Error().Str("path", path).Msg("invalid device path")
		return "", "", false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	//nolint:gosec // Safe: path validated to start with /dev/, udevadm uses absolute path
	cmd := exec.CommandContext(ctx, "/usr/bin/udevadm", "info", "--name="+path)
	out, err := cmd.Output()
	if err != nil {
		// Debug rather than error: this is now called speculatively across
		// every candidate port, where a node with no udev record is an
		// ordinary answer rather than a fault worth reporting.
		log.Debug().Err(err).Str("path", path).Msg("udevadm failed")
		return "", "", false
	}

	for line := range strings.SplitSeq(string(out), "\n") {
		if rest, found := strings.CutPrefix(line, "E: ID_VENDOR_ID="); found {
			vid = rest
		} else if rest, found := strings.CutPrefix(line, "E: ID_MODEL_ID="); found {
			pid = rest
		}
	}
	if vid == "" || pid == "" {
		return "", "", false
	}
	return strings.ToLower(vid), strings.ToLower(pid), true
}

// listLinuxSerialDevices returns /dev nodes whose name starts with one of the
// given prefixes, minus anything on the shared ignore list.
//
// The prefix set is the caller's because a driver for a native-USB device wants
// only CDC-ACM nodes, while the general reader listing also wants USB-to-UART
// bridges.
func listLinuxSerialDevices(prefixes ...string) ([]string, error) {
	const dir = "/dev"

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read /dev directory: %w", err)
	}

	devices := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !hasAnyPrefix(entry.Name(), prefixes) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if ignoreSerialDevice(path) {
			continue
		}
		devices = append(devices, path)
	}
	return devices, nil
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func getLinuxList() ([]string, error) {
	return listLinuxSerialDevices("ttyUSB", "ttyACM")
}

func GetSerialDeviceList() ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxList()
	case "darwin":
		devices := make([]string, 0)
		ports, err := serial.GetPortsList()
		if err != nil {
			return nil, fmt.Errorf("failed to get serial ports list on darwin: %w", err)
		}

		for _, v := range ports {
			if !strings.HasPrefix(v, "/dev/tty.usbserial") {
				continue
			}

			// TODO: check against ignore list

			devices = append(devices, v)
		}

		return devices, nil
	case "windows":
		devices := make([]string, 0)
		ports, err := serial.GetPortsList()
		if err != nil {
			return nil, fmt.Errorf("failed to get serial ports list on windows: %w", err)
		}

		for _, v := range ports {
			if !strings.HasPrefix(v, "COM") {
				continue
			}

			// TODO: check against ignore list

			devices = append(devices, v)
		}

		return devices, nil
	default:
		ports, err := serial.GetPortsList()
		if err != nil {
			return nil, fmt.Errorf("failed to get serial ports list: %w", err)
		}
		return ports, nil
	}
}

// IsUSBCDCDeviceName reports whether a serial device path looks like a USB
// CDC-ACM node for the current platform.
//
// Split out from the listing functions so the naming rules can be tested
// without the host actually having such a device attached.
func IsUSBCDCDeviceName(goos, path string) bool {
	name := filepath.Base(path)
	switch goos {
	case "linux":
		return strings.HasPrefix(name, "ttyACM")
	case "darwin":
		// Native-USB devices appear as tty.usbmodem; tty.usbserial is a
		// USB-to-UART bridge and never a CDC-ACM device.
		return strings.HasPrefix(name, "tty.usbmodem")
	case "windows":
		// Windows exposes every serial port as COMn with no way to tell a
		// CDC-ACM device from a bridge, so the caller's handshake has to.
		return strings.HasPrefix(name, "COM")
	default:
		return true
	}
}

// GetUSBCDCDeviceList returns serial device nodes that are USB CDC-ACM
// interfaces.
//
// Microcontrollers with a native USB peripheral, such as the ESP32-S3,
// enumerate as CDC-ACM rather than through a USB-to-UART bridge, so on macOS
// they appear as tty.usbmodem and are missed entirely by GetSerialDeviceList.
// Using the narrower list also keeps a driver's probes away from bridge-attached
// hardware it could never be.
//
// On Linux the shared ignore list is applied as well, so known non-reader
// devices such as light guns are never opened.
func GetUSBCDCDeviceList() ([]string, error) {
	if runtime.GOOS == "linux" {
		return listLinuxSerialDevices("ttyACM")
	}

	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("failed to get serial ports list: %w", err)
	}

	devices := make([]string, 0, len(ports))
	for _, path := range ports {
		if IsUSBCDCDeviceName(runtime.GOOS, path) {
			devices = append(devices, path)
		}
	}
	return devices, nil
}
