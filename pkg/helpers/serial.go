// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
}

func ignoreSerialDevice(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return true
	}

	if _, err := os.Stat("/usr/bin/udevadm"); err != nil {
		log.Debug().Msgf("udevadm not found, skipping ignore list check")
		return false
	}

	// Validate device path to prevent command injection
	if !strings.HasPrefix(path, "/dev/") {
		log.Error().Str("path", path).Msg("invalid device path")
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	//nolint:gosec // Safe: path validated to start with /dev/, udevadm uses absolute path
	cmd := exec.CommandContext(ctx, "/usr/bin/udevadm", "info", "--name="+path)
	out, err := cmd.Output()
	if err != nil {
		log.Error().Err(err).Msg("udevadm failed")
		return false
	}

	vid := ""
	pid := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "E: ID_VENDOR_ID=") {
			vid = strings.TrimPrefix(line, "E: ID_VENDOR_ID=")
		} else if strings.HasPrefix(line, "E: ID_MODEL_ID=") {
			pid = strings.TrimPrefix(line, "E: ID_MODEL_ID=")
		}
	}

	if vid == "" || pid == "" {
		return false
	}

	vid = strings.ToLower(vid)
	pid = strings.ToLower(pid)

	for _, v := range ignoreDevices {
		if vid == v.Vid && pid == v.Pid {
			return true
		}
	}

	return false
}

func getLinuxList() ([]string, error) {
	path := "/dev"

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return []string{}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev directory: %w", err)
	}
	defer func(f *os.File) {
		closeErr := f.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close serial device folder")
		}
	}(f)

	files, err := f.Readdir(0)
	if err != nil {
		return nil, fmt.Errorf("failed to read /dev directory: %w", err)
	}

	devices := make([]string, 0, len(files))

	for _, v := range files {
		if v.IsDir() {
			continue
		}

		if !strings.HasPrefix(v.Name(), "ttyUSB") && !strings.HasPrefix(v.Name(), "ttyACM") {
			continue
		}

		if ignoreSerialDevice(filepath.Join(path, v.Name())) {
			continue
		}

		devices = append(devices, filepath.Join(path, v.Name()))
	}

	return devices, nil
}

func GetSerialDeviceList() ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxList()
	case "darwin":
		var devices []string
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
		var devices []string
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
