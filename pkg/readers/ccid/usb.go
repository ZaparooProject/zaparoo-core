/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package ccid

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/ccid/proto"
	"github.com/spf13/afero"
)

const (
	backendUSB = "usb"

	usbPathPrefix    = "usb:"
	sysfsUSBDevices  = "/sys/bus/usb/devices"
	usbDeviceDescLen = 18
	// driverUSBFS is the kernel's name for an interface a program has claimed.
	driverUSBFS = "usbfs"
)

var errBadUSBPath = errors.New("not a USB reader path")

// usbLink is an open, claimed USB interface.
type usbLink interface {
	proto.Transport
	Close() error
}

// usbTarget is everything needed to open one smart card interface.
type usbTarget struct {
	sysName string
	driver  string
	bus     int
	dev     int
	iface   proto.Interface
	vendor  uint16
	product uint16
}

// usbBackend reaches readers by speaking CCID over USB itself, for systems
// with no PC/SC service. Readers are found through sysfs, which costs nothing
// and touches no device, so it is safe to do on every auto-detect tick.
type usbBackend struct {
	fs   afero.Fs
	open func(target *usbTarget) (usbLink, error)
}

var _ backend = (*usbBackend)(nil)

func (*usbBackend) Name() string {
	return backendUSB
}

// usbPath is a reader's identity: the port it is plugged into and the
// interface number. Unlike the bus address, it survives unplugging.
func usbPath(sysName string, ifaceNumber uint8) string {
	return usbPathPrefix + sysName + ":" + strconv.Itoa(int(ifaceNumber))
}

func parseUSBPath(readerPath string) (sysName string, ifaceNumber uint8, err error) {
	rest, ok := strings.CutPrefix(readerPath, usbPathPrefix)
	if !ok {
		return "", 0, fmt.Errorf("%w: %q", errBadUSBPath, readerPath)
	}
	idx := strings.LastIndex(rest, ":")
	if idx <= 0 {
		return "", 0, fmt.Errorf("%w: %q", errBadUSBPath, readerPath)
	}
	number, err := strconv.ParseUint(rest[idx+1:], 10, 8)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %q", errBadUSBPath, readerPath)
	}
	return rest[:idx], uint8(number), nil
}

func (b *usbBackend) readTrimmed(name string) string {
	data, err := afero.ReadFile(b.fs, name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// interfaceDriver returns the kernel driver bound to an interface, or an
// empty string when it is free.
func (b *usbBackend) interfaceDriver(sysName string, ifaceNumber uint8) string {
	entries, err := afero.ReadDir(b.fs, path.Join(sysfsUSBDevices, sysName))
	if err != nil {
		return ""
	}
	suffix := "." + strconv.Itoa(int(ifaceNumber))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, sysName+":") || !strings.HasSuffix(name, suffix) {
			continue
		}
		uevent := b.readTrimmed(path.Join(sysfsUSBDevices, sysName, name, "uevent"))
		for _, line := range strings.Split(uevent, "\n") {
			if driver, ok := strings.CutPrefix(line, "DRIVER="); ok {
				return driver
			}
		}
	}
	return ""
}

// targets returns every smart card interface on one USB device.
func (b *usbBackend) targets(sysName string) []usbTarget {
	dir := path.Join(sysfsUSBDevices, sysName)
	raw, err := afero.ReadFile(b.fs, path.Join(dir, "descriptors"))
	if err != nil || len(raw) < usbDeviceDescLen {
		return nil
	}
	ifaces := proto.ParseInterfaces(raw)
	if len(ifaces) == 0 {
		return nil
	}
	bus, busErr := strconv.Atoi(b.readTrimmed(path.Join(dir, "busnum")))
	dev, devErr := strconv.Atoi(b.readTrimmed(path.Join(dir, "devnum")))
	if busErr != nil || devErr != nil {
		return nil
	}

	out := make([]usbTarget, 0, len(ifaces))
	for _, iface := range ifaces {
		out = append(out, usbTarget{
			sysName: sysName,
			driver:  b.interfaceDriver(sysName, iface.Number),
			iface:   iface,
			bus:     bus,
			dev:     dev,
			vendor:  binary.LittleEndian.Uint16(raw[8:10]),
			product: binary.LittleEndian.Uint16(raw[10:12]),
		})
	}
	return out
}

func (b *usbBackend) info(target *usbTarget) readerInfo {
	dir := path.Join(sysfsUSBDevices, target.sysName)
	manufacturer := b.readTrimmed(path.Join(dir, "manufacturer"))
	name := strings.TrimSpace(manufacturer + " " + b.readTrimmed(path.Join(dir, "product")))
	if name == "" {
		name = fmt.Sprintf("%04x:%04x", target.vendor, target.product)
	}
	return readerInfo{
		Path:  usbPath(target.sysName, target.iface.Number),
		Name:  name,
		Known: isKnownUSB(target.vendor, target.product, target.iface.Number),
		Details: map[string]string{
			"usbID":         fmt.Sprintf("%04x:%04x", target.vendor, target.product),
			"interface":     strconv.Itoa(int(target.iface.Number)),
			"exchangeLevel": target.iface.ExchangeLevel(),
			"features":      fmt.Sprintf("%08X", target.iface.Features),
			"protocols":     fmt.Sprintf("%08X", target.iface.Protocols),
			"maxMessage":    strconv.FormatUint(uint64(target.iface.MaxMessageLength), 10),
			"slots":         strconv.Itoa(int(target.iface.MaxSlotIndex) + 1),
			"kernelDriver":  target.driver,
		},
	}
}

func (b *usbBackend) List(context.Context) ([]readerInfo, error) {
	entries, err := afero.ReadDir(b.fs, sysfsUSBDevices)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", sysfsUSBDevices, err)
	}

	var infos []readerInfo
	for _, entry := range entries {
		// Interface directories are named "<device>:<config>.<interface>".
		if strings.Contains(entry.Name(), ":") {
			continue
		}
		targets := b.targets(entry.Name())
		for i := range targets {
			target := &targets[i]
			info := b.info(target)
			switch {
			case target.driver == driverUSBFS:
				// Claimed by a program: the PC/SC service, or this driver
				// already. Either way it is not free to open.
				info.Unusable = "in use by another program"
				info.Busy = true
			case !target.iface.APDULevel():
				info.Unusable = "reader does not exchange whole APDUs"
			}
			infos = append(infos, info)
		}
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Path < infos[j].Path })
	return infos, nil
}

func (b *usbBackend) resolve(readerPath string) (*usbTarget, error) {
	sysName, ifaceNumber, err := parseUSBPath(readerPath)
	if err != nil {
		return nil, err
	}
	targets := b.targets(sysName)
	for i := range targets {
		if targets[i].iface.Number == ifaceNumber {
			return &targets[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", errReaderGone, readerPath)
}

func (b *usbBackend) connect(readerPath string) (*proto.Conn, usbLink, error) {
	target, err := b.resolve(readerPath)
	if err != nil {
		return nil, nil, err
	}
	link, err := b.open(target)
	if err != nil {
		return nil, nil, err
	}
	return proto.New(link, 0), link, nil
}

func (b *usbBackend) CardPresent(ctx context.Context, info *readerInfo) (bool, error) {
	conn, link, err := b.connect(info.Path)
	if err != nil {
		return false, err
	}
	defer func() { _ = link.Close() }()

	status, err := conn.SlotStatus(ctx)
	if err != nil {
		return false, fmt.Errorf("slot status: %w", err)
	}
	return status != proto.ICCAbsent, nil
}

func (b *usbBackend) Open(_ context.Context, info *readerInfo) (device, error) {
	conn, link, err := b.connect(info.Path)
	if err != nil {
		return nil, err
	}
	return newSlotDevice(conn, link.Close), nil
}

func (*usbBackend) Close() error {
	return nil
}
