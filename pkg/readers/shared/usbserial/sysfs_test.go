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
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
)

// sysDevice is a USB device to place in a fake sysfs tree.
type sysDevice struct {
	name        string
	driver      string
	descriptors []byte
	bus, dev    int
	driverLink  bool
}

func writeFile(t *testing.T, fs afero.Fs, name, content string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(name), 0o755))
	require.NoError(t, afero.WriteFile(fs, name, []byte(content), 0o644))
}

func addSysDevice(t *testing.T, fs afero.Fs, d *sysDevice) {
	t.Helper()
	dir := filepath.Join(sysfsUSBDevices, d.name)
	writeFile(t, fs, filepath.Join(dir, "descriptors"), string(d.descriptors))
	writeFile(t, fs, filepath.Join(dir, "busnum"), fmt.Sprintf("%d\n", d.bus))
	writeFile(t, fs, filepath.Join(dir, "devnum"), fmt.Sprintf("%d\n", d.dev))
	writeFile(t, fs, filepath.Join(dir, "bConfigurationValue"), "1\n")
	writeFile(t, fs, filepath.Join(dir, "manufacturer"), "QinHeng\n")
	writeFile(t, fs, filepath.Join(dir, "product"), "USB Serial\n")
	writeFile(t, fs, filepath.Join(dir, "serial"), "ABC123\n")
	ifaceDir := filepath.Join(sysfsUSBDevices, d.name+":1.0")
	uevent := "DEVTYPE=usb_interface\n"
	if d.driver != "" {
		uevent += "DRIVER=" + d.driver + "\n"
	}
	writeFile(t, fs, filepath.Join(ifaceDir, "uevent"), uevent)
	if d.driverLink {
		require.NoError(t, fs.MkdirAll(filepath.Join(ifaceDir, "driver"), 0o755))
	}
}

func ch340Descriptors() []byte {
	return usbDescriptors(vendorWCH, 0x7523, 0x110, 0x264, 0xFF, 8)
}

func pl2303HXDescriptors() []byte {
	return usbDescriptors(vendorProlific, 0x2303, 0x110, 0x300, 0, 0x40)
}

func TestSysfsLookup(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	addSysDevice(t, fs, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5})
	s := sysfs{fs: fs}

	tgt, err := s.lookup("1-2")
	require.NoError(t, err)
	assert.Equal(t, chipCH34x, tgt.chip)
	assert.Empty(t, tgt.driver)
	assert.Equal(t, "/dev/bus/usb/001/005", tgt.node())
	assert.Equal(t, serialInterface{
		number: 0, bulkIn: 0x83, bulkOut: 0x02, intrIn: 0x81, bulkInPacket: 64, intrPacket: 10,
	}, tgt.iface)

	_, err = s.lookup("1-9")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSysfsLookupRejects(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	addSysDevice(t, fs, &sysDevice{
		name: "1-1", descriptors: usbDescriptors(0x1234, 0x5678, 0x200, 0x100, 0, 64), bus: 1, dev: 2,
	})
	// A WCH chip outside the CH340/CH341 family.
	addSysDevice(t, fs, &sysDevice{
		name: "1-2", descriptors: usbDescriptors(vendorWCH, 0x55D3, 0x200, 0x100, 0, 64), bus: 1, dev: 3,
	})
	addSysDevice(t, fs, &sysDevice{name: "1-3", descriptors: []byte{1, 2, 3}, bus: 1, dev: 4})
	addSysDevice(t, fs, &sysDevice{name: "1-4", descriptors: ch340Descriptors(), bus: 0, dev: 4})
	addSysDevice(t, fs, &sysDevice{name: "1-5", descriptors: ch340Descriptors(), bus: 1, dev: 1000})
	addSysDevice(t, fs, &sysDevice{name: "1-6", descriptors: ch340Descriptors(), bus: 1, dev: 6})
	writeFile(t, fs, filepath.Join(sysfsUSBDevices, "1-6", "bConfigurationValue"), "\n")
	s := sysfs{fs: fs}
	for _, name := range []string{"1-1", "1-2", "1-3", "1-4", "1-5", "1-6"} {
		_, err := s.lookup(name)
		require.ErrorIs(t, err, ErrNotFound, name)
	}
}

func TestSysfsDriverBound(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	addSysDevice(t, fs, &sysDevice{name: "1-1", descriptors: ch340Descriptors(), bus: 1, dev: 2, driver: "ch341"})
	addSysDevice(t, fs, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 3, driverLink: true})
	addSysDevice(t, fs, &sysDevice{name: "1-3", descriptors: ch340Descriptors(), bus: 1, dev: 4, driver: "usbfs"})
	s := sysfs{fs: fs}

	for name, want := range map[string]string{"1-1": "ch341", "1-2": "unknown", "1-3": "usbfs"} {
		tgt, err := s.lookup(name)
		require.NoError(t, err)
		assert.Equal(t, want, tgt.driver, name)
	}
}

func TestSysfsList(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	addSysDevice(t, fs, &sysDevice{name: "1-2", descriptors: ch340Descriptors(), bus: 1, dev: 5})
	addSysDevice(t, fs, &sysDevice{name: "1-1.4", descriptors: pl2303HXDescriptors(), bus: 1, dev: 7})
	addSysDevice(t, fs, &sysDevice{name: "1-3", descriptors: ch340Descriptors(), bus: 1, dev: 8, driver: "ch341"})
	addSysDevice(t, fs, &sysDevice{name: "2-1", descriptors: ch340Descriptors(), bus: 2, dev: 2, driver: "usbfs"})
	addSysDevice(t, fs, &sysDevice{
		name: "3-1", descriptors: usbDescriptors(0x1234, 0x5678, 0x200, 0x100, 0, 64), bus: 3, dev: 2,
	})
	writeFile(t, fs, filepath.Join(sysfsUSBDevices, "usb1", "descriptors"), string(ch340Descriptors()))

	infos, err := sysfs{fs: fs}.list()
	require.NoError(t, err)
	assert.Equal(t, []PortInfo{
		{
			Path: "usbserial:1-1.4", Name: "QinHeng USB Serial", Chip: "PL2303", Serial: "ABC123",
			VendorID: vendorProlific, ProductID: 0x2303,
		},
		{
			Path: "usbserial:1-2", Name: "QinHeng USB Serial", Chip: "CH34x", Serial: "ABC123",
			VendorID: vendorWCH, ProductID: 0x7523,
		},
	}, infos)
	assert.Equal(t, "1A86:7523", infos[1].VIDPID())

	_, err = sysfs{fs: afero.NewMemMapFs()}.list()
	require.Error(t, err)
}

func TestSysfsDescriptorsBounded(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	big := append(ch340Descriptors(), make([]byte, 2*maxDescriptorsSize)...)
	writeFile(t, fs, filepath.Join(sysfsUSBDevices, "1-1", "descriptors"), string(big))
	raw, err := sysfs{fs: fs}.readDescriptors(filepath.Join(sysfsUSBDevices, "1-1"))
	require.NoError(t, err)
	assert.Len(t, raw, maxDescriptorsSize)
}

func TestPaths(t *testing.T) {
	t.Parallel()
	assert.True(t, IsPath("usbserial:1-2"))
	assert.False(t, IsPath("/dev/ttyUSB0"))
	assert.Equal(t, "usbserial:1-2.3", MakePath("1-2.3"))
	assert.Equal(t, "1-2.3", Topology("usbserial:1-2.3"))

	valid := []string{"usbserial:1-2", "usbserial:12-1.2.3", "usbserial:001-999.1"}
	for _, p := range valid {
		_, err := parsePath(p)
		require.NoError(t, err, p)
	}
	invalid := []string{
		"usbserial:", "usbserial:1", "usbserial:usb1", "usbserial:1-2:1.0", "usbserial:../../etc",
		"usbserial:1-2/../../x", "usbserial:1-", "usbserial:1-2.", "usbserial:1234-1",
		"usbserial:1-2.x", "/dev/ttyUSB0", "usbserial:-1",
	}
	for _, p := range invalid {
		_, err := parsePath(p)
		require.ErrorIs(t, err, ErrBadPath, p)
		assert.Empty(t, Topology(p), p)
	}
}

func TestSupportedChip(t *testing.T) {
	t.Parallel()
	for _, pid := range ch34xProducts {
		kind, ok := supportedChip(vendorWCH, pid)
		assert.True(t, ok)
		assert.Equal(t, chipCH34x, kind)
	}
	for _, pid := range pl2303Products {
		kind, ok := supportedChip(vendorProlific, pid)
		assert.True(t, ok)
		assert.Equal(t, chipPL2303, kind)
	}
	ids := [][2]uint16{{vendorWCH, 0x55D3}, {vendorWCH, 0x7524}, {vendorProlific, 0x0611}, {0x0403, 0x6001}}
	for _, id := range ids {
		_, ok := supportedChip(id[0], id[1])
		assert.False(t, ok, "%04x:%04x", id[0], id[1])
	}
	assert.Equal(t, "CH34x", chipCH34x.String())
	assert.Equal(t, "PL2303", chipPL2303.String())
	assert.Equal(t, "unknown", chipKind(0).String())
}

func TestLineFromMode(t *testing.T) {
	t.Parallel()
	line, err := lineFromMode(nil)
	require.NoError(t, err)
	assert.Equal(t, lineSettings{baud: 9600, dataBits: 8}, line)

	line, err = lineFromMode(&serial.Mode{
		BaudRate: 115200, DataBits: 7, Parity: serial.EvenParity, StopBits: serial.TwoStopBits,
	})
	require.NoError(t, err)
	want := lineSettings{baud: 115200, dataBits: 7, parity: serial.EvenParity, stopBits: serial.TwoStopBits}
	assert.Equal(t, want, line)

	for _, mode := range []*serial.Mode{
		{BaudRate: -1},
		{DataBits: 4},
		{DataBits: 9},
		{Parity: serial.Parity(9)},
		{StopBits: serial.StopBits(9)},
	} {
		_, err := lineFromMode(mode)
		require.Error(t, err, mode)
	}
}

func TestParseDescriptors(t *testing.T) {
	t.Parallel()
	desc, ifaces, err := parseDescriptors(pl2303HXDescriptors())
	require.NoError(t, err)
	assert.Equal(t, deviceDescriptor{
		bcdUSB: 0x110, vendorID: vendorProlific, productID: 0x2303, bcdDevice: 0x300, maxPacket0: 0x40,
	}, desc)
	require.Len(t, ifaces, 1)

	// An alternate setting, an interface without a bulk pair and a second
	// configuration are all skipped.
	raw := usbDescriptors(vendorWCH, 0x7523, 0x110, 0x264, 0xFF, 8)
	raw = append(raw,
		9, descTypeInterface, 0, 1, 1, 0xFF, 0, 0, 0,
		7, descTypeEndpoint, 0x84, endpointBulk, 64, 0, 0,
		9, descTypeInterface, 1, 0, 1, 0xFF, 0, 0, 0,
		7, descTypeEndpoint, 0x85, endpointBulk, 64, 0, 0,
		9, descTypeConfig, 18, 0, 1, 2, 0, 0x80, 50,
		9, descTypeInterface, 2, 0, 2, 0xFF, 0, 0, 0,
		7, descTypeEndpoint, 0x86, endpointBulk, 64, 0, 0,
		7, descTypeEndpoint, 0x06, endpointBulk, 64, 0, 0,
	)
	_, ifaces, err = parseDescriptors(raw)
	require.NoError(t, err)
	require.Len(t, ifaces, 1)
	assert.Equal(t, uint8(0x83), ifaces[0].bulkIn)

	// A truncated trailing descriptor ends the walk.
	_, ifaces, err = parseDescriptors(raw[:30])
	require.NoError(t, err)
	assert.Empty(t, ifaces)

	bads := [][]byte{
		nil,
		make([]byte, 17),
		append([]byte{18, 2}, make([]byte, 16)...),
		append([]byte{9, 1}, make([]byte, 16)...),
	}
	for _, bad := range bads {
		_, _, err := parseDescriptors(bad)
		require.ErrorIs(t, err, errBadDescriptor)
	}
}

func FuzzParseDescriptors(f *testing.F) {
	f.Add(ch340Descriptors())
	f.Add(pl2303HXDescriptors())
	f.Add([]byte{18, 1})
	f.Add(append(ch340Descriptors(), 0, 0, 255, 4))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, ifaces, err := parseDescriptors(raw)
		if err != nil {
			return
		}
		if len(raw) < deviceDescLen {
			t.Fatalf("accepted %d bytes", len(raw))
		}
		for _, iface := range ifaces {
			if iface.bulkIn&endpointDirIn == 0 || iface.bulkOut&endpointDirIn != 0 {
				t.Fatalf("bad endpoint directions %+v", iface)
			}
			if iface.bulkInPacket > maxPacketMask || iface.intrPacket > maxPacketMask {
				t.Fatalf("packet size out of range %+v", iface)
			}
		}
	})
}
