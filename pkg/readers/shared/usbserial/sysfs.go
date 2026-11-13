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

package usbserial

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

const (
	sysfsUSBDevices = "/sys/bus/usb/devices"
	usbDeviceNode   = "/dev/bus/usb/%03d/%03d"
	// maxDescriptorsSize bounds the sysfs descriptors read. Real adapters
	// have well under a hundred bytes.
	maxDescriptorsSize = 64 * 1024
)

// target is a supported adapter found in sysfs.
type target struct {
	sysName string
	driver  string
	chip    chipKind
	desc    deviceDescriptor
	iface   serialInterface
	bus     int
	dev     int
}

// node returns the usbfs device node of the target.
func (t *target) node() string {
	return fmt.Sprintf(usbDeviceNode, t.bus, t.dev)
}

// sysfs reads USB devices from the kernel's sysfs tree. Reading it touches no
// device, so it is cheap enough for every auto-detect tick.
type sysfs struct {
	fs afero.Fs
}

func (s sysfs) readTrimmed(name string) string {
	data, err := afero.ReadFile(s.fs, name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (s sysfs) readDescriptors(dir string) ([]byte, error) {
	file, err := s.fs.Open(filepath.Join(dir, "descriptors"))
	if err != nil {
		return nil, fmt.Errorf("read descriptors: %w", err)
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxDescriptorsSize))
	if err != nil {
		return nil, fmt.Errorf("read descriptors: %w", err)
	}
	return raw, nil
}

// interfaceDriver returns the driver bound to an interface, or an empty
// string when none is. An interface a program has claimed through usbfs
// reports the driver "usbfs".
func (s sysfs) interfaceDriver(sysName, config string, number uint8) string {
	dir := filepath.Join(sysfsUSBDevices, fmt.Sprintf("%s:%s.%d", sysName, config, number))
	for _, line := range strings.Split(s.readTrimmed(filepath.Join(dir, "uevent")), "\n") {
		if driver, ok := strings.CutPrefix(line, "DRIVER="); ok && driver != "" {
			return driver
		}
	}
	// The uevent file only names the driver when it is bound; the driver
	// link is checked too so a bound interface is never mistaken for a
	// free one.
	if _, err := s.fs.Stat(filepath.Join(dir, "driver")); err == nil {
		return "unknown"
	}
	return ""
}

// lookup returns the supported adapter with the given sysfs device name.
func (s sysfs) lookup(sysName string) (*target, error) {
	dir := filepath.Join(sysfsUSBDevices, sysName)
	raw, err := s.readDescriptors(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNotFound, sysName, err)
	}
	desc, ifaces, err := parseDescriptors(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNotFound, sysName, err)
	}
	chip, ok := supportedChip(desc.vendorID, desc.productID)
	if !ok || len(ifaces) == 0 {
		return nil, fmt.Errorf("%w: %s is %04x:%04x", ErrNotFound, sysName, desc.vendorID, desc.productID)
	}
	bus, busErr := strconv.Atoi(s.readTrimmed(filepath.Join(dir, "busnum")))
	dev, devErr := strconv.Atoi(s.readTrimmed(filepath.Join(dir, "devnum")))
	if busErr != nil || devErr != nil || bus <= 0 || dev <= 0 || bus > 999 || dev > 999 {
		return nil, fmt.Errorf("%w: %s has no bus address", ErrNotFound, sysName)
	}
	config := s.readTrimmed(filepath.Join(dir, "bConfigurationValue"))
	if config == "" {
		// An unconfigured device has no interfaces to use.
		return nil, fmt.Errorf("%w: %s is not configured", ErrNotFound, sysName)
	}
	iface := ifaces[0]
	return &target{
		sysName: sysName,
		driver:  s.interfaceDriver(sysName, config, iface.number),
		chip:    chip,
		desc:    desc,
		iface:   iface,
		bus:     bus,
		dev:     dev,
	}, nil
}

// info describes a target for enumeration.
func (s sysfs) info(t *target) PortInfo {
	dir := filepath.Join(sysfsUSBDevices, t.sysName)
	name := strings.TrimSpace(s.readTrimmed(filepath.Join(dir, "manufacturer")) + " " +
		s.readTrimmed(filepath.Join(dir, "product")))
	return PortInfo{
		Path:      MakePath(t.sysName),
		Name:      name,
		Chip:      t.chip.String(),
		Serial:    s.readTrimmed(filepath.Join(dir, "serial")),
		VendorID:  t.desc.vendorID,
		ProductID: t.desc.productID,
	}
}

// list returns every supported adapter whose serial interface no driver is
// bound to, sorted by path. A non-nil ready leaves out the adapters it
// refuses.
func (s sysfs) list(ready func(*target) bool) ([]PortInfo, error) {
	entries, err := afero.ReadDir(s.fs, sysfsUSBDevices)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", sysfsUSBDevices, err)
	}
	var infos []PortInfo
	for _, entry := range entries {
		// Interface directories are "<device>:<config>.<interface>", and
		// root hubs are "usb<N>"; neither is a port.
		if !validSysName(entry.Name()) {
			continue
		}
		t, err := s.lookup(entry.Name())
		if err != nil || t.driver != "" || (ready != nil && !ready(t)) {
			continue
		}
		infos = append(infos, s.info(t))
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Path < infos[j].Path })
	return infos, nil
}
