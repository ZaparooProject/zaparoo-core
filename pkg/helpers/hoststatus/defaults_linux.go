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
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/spf13/afero"
)

// NewDefaults returns the generic readers for Linux.
func NewDefaults(opts DefaultOptions) Readers {
	fs := afero.NewOsFs()
	root := string(filepath.Separator)
	sysClass := filepath.Join(root, "sys", "class")
	bus := newSystemBus()

	network := &LinuxNetwork{
		Fs:         fs,
		Interfaces: ListRawInterfaces,
		bus:        bus,
		ProcRoute:  filepath.Join(root, "proc", "net", "route"),
		SysNet:     filepath.Join(sysClass, "net"),
	}
	bluetooth := &LinuxBluetooth{Fs: fs, bus: bus, SysRoot: filepath.Join(sysClass, "bluetooth")}
	display := &LinuxDisplay{Fs: fs, DRMRoot: filepath.Join(sysClass, "drm")}
	controllers := &LinuxControllers{
		Fs:          fs,
		Resolve:     filepath.EvalSymlinks,
		InputRoot:   filepath.Join(sysClass, "input"),
		PowerRoot:   filepath.Join(sysClass, "power_supply"),
		VirtualName: opts.VirtualInputName,
	}
	system := &LinuxSystem{
		Fs:       fs,
		Hostname: os.Hostname,
		ModelPaths: []string{
			filepath.Join(root, "sys", "firmware", "devicetree", "base", "model"),
			filepath.Join(sysClass, "dmi", "id", "product_name"),
		},
	}
	powerControl := NewLinuxPowerControl(opts.Executor, exec.LookPath, os.Geteuid)
	powerControl.bus = bus

	return Readers{
		Power:        power.ReadDetail,
		Network:      network.Read,
		Bluetooth:    bluetooth.Read,
		Storage:      NewStorageReader().Read,
		Display:      display.Read,
		Controllers:  controllers.Read,
		System:       system.Read,
		PowerControl: powerControl,
	}
}
