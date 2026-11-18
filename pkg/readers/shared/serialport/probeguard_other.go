//go:build !linux

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

package serialport

import (
	"fmt"
	"os"
	"strconv"
)

// USBPorts lists the USB serial device nodes reader auto-detection probes.
// Only Linux guards its probes, so there is nothing to list elsewhere.
func USBPorts() []USBPort {
	return nil
}

// GuardProbes returns a guard that protects nothing: finding a port's other
// users and saving its settings are only implemented on Linux.
func GuardProbes([]string) *ProbeGuard {
	return &ProbeGuard{}
}

// PortsInUse returns which of the given serial ports another process has
// open. It finds none outside Linux.
func PortsInUse([]string) []string {
	return nil
}

// lineState is never created outside Linux.
type lineState struct{}

func (*lineState) restore() error { return nil }

func (*lineState) discard() {}

// NodeID identifies the device node at path until it is removed and created
// again.
func NodeID(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	return strconv.FormatInt(info.ModTime().UnixNano(), 10), nil
}
