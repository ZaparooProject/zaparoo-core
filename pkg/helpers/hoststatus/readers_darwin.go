//go:build darwin

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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

const darwinCommandTimeout = 2 * time.Second

// Tools are addressed by absolute path so a reading does not depend on the
// PATH the service inherited.
var (
	darwinRoute        = filepath.Join(string(filepath.Separator), "sbin", "route")
	darwinNetworkSetup = filepath.Join(string(filepath.Separator), "usr", "sbin", "networksetup")
	darwinSysctl       = filepath.Join(string(filepath.Separator), "usr", "sbin", "sysctl")
	darwinPmset        = filepath.Join(string(filepath.Separator), "usr", "bin", "pmset")
)

// NewDefaults returns the generic readers for macOS. Bluetooth, displays and
// controllers need frameworks Core does not link, so they are not offered.
func NewDefaults(opts DefaultOptions) Readers {
	network := &darwinNetwork{executor: opts.Executor}
	system := &darwinSystem{executor: opts.Executor}
	return Readers{
		Power:   power.ReadDetail,
		Network: network.Read,
		Storage: NewStorageReader().Read,
		System:  system.Read,
		PowerControl: &ExecPowerControl{
			Executor: opts.Executor,
			Commands: map[PowerAction][]string{PowerSuspend: {darwinPmset, "sleepnow"}},
		},
	}
}

type darwinNetwork struct {
	executor command.Executor
	ports    map[string]LinkType
	mu       syncutil.Mutex
}

func (r *darwinNetwork) output(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), darwinCommandTimeout)
	defer cancel()
	output, err := r.executor.Output(ctx, name, args...)
	if err != nil {
		return ""
	}
	return string(output)
}

// hardwarePorts is read once: which device is the Wi-Fi card does not change
// while the machine is running.
func (r *darwinNetwork) hardwarePorts() map[string]LinkType {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ports == nil {
		if ports := ParseHardwarePorts(r.output(darwinNetworkSetup, "-listallhardwareports")); len(ports) > 0 {
			r.ports = ports
		}
	}
	return r.ports
}

func (r *darwinNetwork) Read() (Network, error) {
	raw, err := ListRawInterfaces()
	if err != nil {
		return Network{}, err
	}
	ports := r.hardwarePorts()
	classify := func(name string) LinkType {
		if linkType, ok := ports[name]; ok {
			return linkType
		}
		return LinkOther
	}
	isUp := func(iface *RawInterface) bool { return iface.Up }
	defaultInterface := ParseRouteDefaultInterface(r.output(darwinRoute, "-n", "get", "default"))

	network := buildNetwork(raw, defaultInterface, classify, isUp)
	if network.Type == LinkNone {
		network.Internet = InternetNone
		network.InternetAuthoritative = true
	}
	return network, nil
}

type darwinSystem struct {
	executor command.Executor
}

func (r *darwinSystem) Read() (System, error) {
	var system System
	if hostname, err := os.Hostname(); err == nil {
		system.Hostname = hostname
	}
	ctx, cancel := context.WithTimeout(context.Background(), darwinCommandTimeout)
	defer cancel()
	if output, err := r.executor.Output(ctx, darwinSysctl, "-n", "hw.model"); err == nil {
		system.Model = strings.TrimSpace(string(output))
	}
	return system, nil
}
