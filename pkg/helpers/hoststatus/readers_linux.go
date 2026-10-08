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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

func readTrimmed(fs afero.Fs, path string) (string, bool) {
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return "", false
	}
	return strings.Trim(string(data), "\x00 \t\r\n"), true
}

// arphrdEther is the hardware type the kernel reports for Ethernet.
const arphrdEther = "1"

// LinuxNetwork reads links from sysfs and the routing table from procfs.
type LinuxNetwork struct {
	Fs         afero.Fs
	Interfaces func() ([]RawInterface, error)
	bus        busCaller
	ProcRoute  string
	SysNet     string
}

// Read reports the device's links and which one carries the default route.
func (r *LinuxNetwork) Read() (Network, error) {
	raw, err := r.Interfaces()
	if err != nil {
		return Network{}, err
	}
	routes, _ := afero.ReadFile(r.Fs, r.ProcRoute)
	network := buildNetwork(raw, ParseDefaultRouteInterface(string(routes)), r.classify, r.isUp)
	if network.Type == LinkNone {
		network.Internet = InternetNone
		network.InternetAuthoritative = true
		return network, nil
	}
	if state, ok := r.networkManagerState(); ok {
		network.Internet = state
		network.InternetAuthoritative = true
	}
	return network, nil
}

func (r *LinuxNetwork) classify(name string) LinkType {
	dir := filepath.Join(r.SysNet, name)
	if exists, _ := afero.DirExists(r.Fs, filepath.Join(dir, "wireless")); exists || strings.HasPrefix(name, "wl") {
		return LinkWifi
	}
	if hardware, ok := readTrimmed(r.Fs, filepath.Join(dir, "type")); ok && hardware == arphrdEther {
		// Bridges and container links also claim to be Ethernet; only a link
		// backed by hardware counts as a cable.
		if exists, _ := afero.Exists(r.Fs, filepath.Join(dir, "device")); exists {
			return LinkWired
		}
	}
	return LinkOther
}

func (r *LinuxNetwork) isUp(iface *RawInterface) bool {
	state, ok := readTrimmed(r.Fs, filepath.Join(r.SysNet, iface.Name, "operstate"))
	if !ok {
		return iface.Up
	}
	return state == "up" || state == "unknown"
}

const (
	nmService = "org.freedesktop.NetworkManager"
	nmPath    = "/org/freedesktop/NetworkManager"

	nmConnectivityNone    = 1
	nmConnectivityPortal  = 2
	nmConnectivityLimited = 3
	nmConnectivityFull    = 4
)

// networkManagerState returns NetworkManager's own reachability answer when
// it is really checking. With its check disabled it reports "full" for any
// default route, which is a guess Core must not pass on as a reading.
func (r *LinuxNetwork) networkManagerState() (InternetState, bool) {
	if r.bus == nil {
		return "", false
	}
	ctx := context.Background()
	for _, name := range []string{"ConnectivityCheckAvailable", "ConnectivityCheckEnabled"} {
		on, err := busProperty[bool](ctx, r.bus, nmService, nmPath, nmService, name)
		if err != nil || !on {
			return "", false
		}
	}
	state, err := busProperty[uint32](ctx, r.bus, nmService, nmPath, nmService, "Connectivity")
	if err != nil {
		return "", false
	}
	switch state {
	case nmConnectivityFull:
		return InternetFull, true
	case nmConnectivityPortal:
		return InternetPortal, true
	case nmConnectivityNone, nmConnectivityLimited:
		return InternetNone, true
	default:
		return "", false
	}
}

const (
	bluezService   = "org.bluez"
	bluezAdapter   = "org.bluez.Adapter1"
	rfkillStateOn  = "1"
	adapterDirName = "hci"
)

// LinuxBluetooth reads the adapter's state. It never changes it.
type LinuxBluetooth struct {
	Fs      afero.Fs
	bus     busCaller
	SysRoot string
}

// Read reports whether an adapter exists and, where that can be learned
// without disturbing it, whether it is powered.
func (r *LinuxBluetooth) Read() (Bluetooth, error) {
	entries, err := afero.ReadDir(r.Fs, r.SysRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Bluetooth{}, nil
		}
		return Bluetooth{}, fmt.Errorf("listing bluetooth adapters: %w", err)
	}
	var adapters []string
	for _, entry := range entries {
		// Connections show up beside the adapter as "hci0:256".
		if name := entry.Name(); strings.HasPrefix(name, adapterDirName) && !strings.Contains(name, ":") {
			adapters = append(adapters, name)
		}
	}
	if len(adapters) == 0 {
		return Bluetooth{}, nil
	}
	return Bluetooth{Present: true, Powered: r.powered(adapters)}, nil
}

func (r *LinuxBluetooth) powered(adapters []string) *bool {
	if r.bus != nil {
		known := false
		on := false
		for _, adapter := range adapters {
			path := dbus.ObjectPath("/org/bluez/" + adapter)
			value, err := busProperty[bool](context.Background(), r.bus, bluezService, path, bluezAdapter, "Powered")
			if err != nil {
				continue
			}
			known = true
			on = on || value
		}
		if known {
			return &on
		}
	}

	// Without bluetoothd the only thing sysfs tells us is whether the radio
	// is blocked: a blocked radio is certainly off, an unblocked one may be
	// either.
	for _, adapter := range adapters {
		if !r.blocked(adapter) {
			return nil
		}
	}
	off := false
	return &off
}

func (r *LinuxBluetooth) blocked(adapter string) bool {
	dir := filepath.Join(r.SysRoot, adapter)
	entries, err := afero.ReadDir(r.Fs, dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "rfkill") {
			continue
		}
		state, ok := readTrimmed(r.Fs, filepath.Join(dir, entry.Name(), "state"))
		return ok && state != rfkillStateOn
	}
	return false
}

// LinuxDisplay reads display outputs from the kernel's DRM class.
type LinuxDisplay struct {
	Fs      afero.Fs
	DRMRoot string
}

// Read reports which outputs are connected and driven.
func (r *LinuxDisplay) Read() (Display, error) {
	entries, err := afero.ReadDir(r.Fs, r.DRMRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Display{}, ErrUnsupported
		}
		return Display{}, fmt.Errorf("listing display connectors: %w", err)
	}
	var connectors []Connector
	for _, entry := range entries {
		name, ok := ConnectorName(entry.Name())
		if !ok {
			continue
		}
		dir := filepath.Join(r.DRMRoot, entry.Name())
		status, _ := readTrimmed(r.Fs, filepath.Join(dir, "status"))
		enabled, _ := readTrimmed(r.Fs, filepath.Join(dir, "enabled"))
		connectors = append(connectors, Connector{
			Name:      name,
			Connected: status == "connected",
			Enabled:   enabled == "enabled",
		})
	}
	if len(connectors) == 0 {
		// A framebuffer-only device has a DRM class with nothing in it.
		return Display{}, ErrUnsupported
	}
	return ClassifyDisplay(connectors), nil
}

// LinuxControllers lists physical game controllers from the kernel's input
// class.
type LinuxControllers struct {
	Fs afero.Fs
	// Resolve returns the real device path behind a sysfs class entry.
	Resolve     func(path string) (string, error)
	InputRoot   string
	PowerRoot   string
	VirtualName string
}

// virtualDevicePath is where the kernel roots devices that software created.
var virtualDevicePath = filepath.Join("devices", "virtual") + string(filepath.Separator)

type controllerSupply struct {
	parent string
	name   string
	dir    string
}

// Read reports connected controllers. Devices created in software are left
// out, which covers Core's own virtual gamepad and the ones a game launcher
// presents in place of the real hardware.
func (r *LinuxControllers) Read() ([]Controller, error) {
	entries, err := afero.ReadDir(r.Fs, r.InputRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrUnsupported
		}
		return nil, fmt.Errorf("listing input devices: %w", err)
	}
	supplies := r.deviceSupplies()

	controllers := []Controller{}
	for _, entry := range entries {
		id := entry.Name()
		if !strings.HasPrefix(id, "input") {
			continue
		}
		dir := filepath.Join(r.InputRoot, id)
		alias, ok := readTrimmed(r.Fs, filepath.Join(dir, "modalias"))
		if !ok {
			continue
		}
		parsed, ok := ParseInputModalias(alias)
		if !ok || !parsed.IsGamepad() {
			continue
		}
		name, _ := readTrimmed(r.Fs, filepath.Join(dir, "name"))
		realPath, resolveErr := r.Resolve(dir)
		if resolveErr == nil && strings.Contains(realPath, virtualDevicePath) {
			continue
		}
		if name == r.VirtualName && r.VirtualName != "" {
			continue
		}
		uniq, _ := readTrimmed(r.Fs, filepath.Join(dir, "uniq"))
		controllers = append(controllers, Controller{
			ID:         id,
			Name:       name,
			VendorID:   fmt.Sprintf("%04x", parsed.Vendor),
			ProductID:  fmt.Sprintf("%04x", parsed.Product),
			Connection: parsed.Connection(),
			Battery:    r.battery(supplies, realPath, uniq),
		})
	}
	sort.Slice(controllers, func(a, b int) bool {
		return inputIndex(controllers[a].ID) < inputIndex(controllers[b].ID)
	})
	return controllers, nil
}

func inputIndex(id string) int {
	index, err := strconv.Atoi(strings.TrimPrefix(id, "input"))
	if err != nil {
		return 0
	}
	return index
}

// deviceSupplies lists the batteries that belong to a peripheral rather than
// to the machine.
func (r *LinuxControllers) deviceSupplies() []controllerSupply {
	entries, err := afero.ReadDir(r.Fs, r.PowerRoot)
	if err != nil {
		return nil
	}
	var supplies []controllerSupply
	for _, entry := range entries {
		dir := filepath.Join(r.PowerRoot, entry.Name())
		if scope, _ := readTrimmed(r.Fs, filepath.Join(dir, "scope")); scope != "Device" {
			continue
		}
		supply := controllerSupply{name: strings.ToLower(entry.Name()), dir: dir}
		if realPath, resolveErr := r.Resolve(dir); resolveErr == nil {
			// <device>/power_supply/<name>: the device is two levels up.
			supply.parent = filepath.Dir(filepath.Dir(realPath)) + string(filepath.Separator)
		}
		supplies = append(supplies, supply)
	}
	return supplies
}

func (r *LinuxControllers) battery(supplies []controllerSupply, realPath, uniq string) *ControllerBattery {
	uniq = strings.ToLower(uniq)
	for _, supply := range supplies {
		sameDevice := realPath != "" && len(supply.parent) > 1 && strings.HasPrefix(realPath, supply.parent)
		sameAddress := uniq != "" && strings.Contains(supply.name, uniq)
		if !sameDevice && !sameAddress {
			continue
		}
		raw, ok := readTrimmed(r.Fs, filepath.Join(supply.dir, "capacity"))
		if !ok {
			return nil
		}
		percent, err := strconv.Atoi(raw)
		if err != nil || percent < 0 || percent > 100 {
			log.Debug().Str("supply", supply.name).Str("capacity", raw).Msg("ignoring controller battery reading")
			return nil
		}
		return &ControllerBattery{Percent: &percent, Level: BatteryLevel(percent)}
	}
	return nil
}

// LinuxSystem reads what the machine calls itself.
type LinuxSystem struct {
	Fs         afero.Fs
	Hostname   func() (string, error)
	ModelPaths []string
}

// placeholderModels are what firmware reports when the vendor never filled
// the field in.
var placeholderModels = map[string]struct{}{
	"to be filled by o.e.m.": {},
	"system product name":    {},
	"default string":         {},
	"none":                   {},
}

// Read reports the hostname and hardware model.
func (r *LinuxSystem) Read() (System, error) {
	var system System
	if hostname, err := r.Hostname(); err == nil {
		system.Hostname = hostname
	}
	for _, path := range r.ModelPaths {
		model, ok := readTrimmed(r.Fs, path)
		if !ok || model == "" {
			continue
		}
		if _, placeholder := placeholderModels[strings.ToLower(model)]; placeholder {
			continue
		}
		system.Model = model
		break
	}
	return system, nil
}
