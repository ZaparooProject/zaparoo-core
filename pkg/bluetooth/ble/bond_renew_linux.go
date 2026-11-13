//go:build linux

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

package ble

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"unsafe"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

// A phone that forgot its bond and pairs again is still paired as far as
// bluetoothd knows. With JustWorksRepairing set to always, bluetoothd
// accepts the new pairing itself, so the agent is never asked, and since
// Paired does not change, nothing on D-Bus says it happened. The link
// stalls exactly as a first pairing's can. The kernel announces every new
// long-term key on the management interface, so while the window is open
// Core watches for those and treats a new key for a device connected to
// Core's advertisement as a bond the window accepted. A device the adapter
// connected to itself, a gamepad above all, is left alone.

const (
	mgmtEvNewLongTermKey = 0x000A

	// mgmtNewLTKMinSize covers store_hint and the key's address, all that
	// is read of the event. The key itself is never looked at.
	mgmtNewLTKMinSize = 1 + 7

	// hciGetConnList is HCIGETCONNLIST, _IOR('H', 212, int).
	hciGetConnList = 2<<30 | 4<<16 | 'H'<<8 | 212

	// hciConnInfoSize is sizeof(struct hci_conn_info).
	hciConnInfoSize = 16
	hciConnListMax  = 32
	hciLELink       = 0x80
	hciLMMaster     = 0x0001
)

// renewedBondAddress reads the identity address from a New Long Term Key
// event for the controller at index. Keys the kernel does not ask to keep,
// for an address that will change, are skipped like bluetoothd skips them.
func renewedBondAddress(ev []byte, index uint16) ([6]byte, bool) {
	var addr [6]byte
	if len(ev) < mgmtHeaderSize+mgmtNewLTKMinSize ||
		binary.LittleEndian.Uint16(ev[0:]) != mgmtEvNewLongTermKey ||
		binary.LittleEndian.Uint16(ev[2:]) != index {
		return addr, false
	}
	p := ev[mgmtHeaderSize:]
	if p[0] == 0 {
		return addr, false
	}
	if t := p[7]; t != bdaddrLEPublic && t != bdaddrLERandom {
		return addr, false
	}
	copy(addr[:], p[1:7])
	return addr, true
}

// addressString formats a wire-order address as BlueZ prints it.
func addressString(wire [6]byte) string {
	var b strings.Builder
	for i := 5; i >= 0; i-- {
		fmt.Fprintf(&b, "%02X", wire[i])
		if i > 0 {
			b.WriteByte(':')
		}
	}
	return b.String()
}

// connectedAsPeripheral reports whether the controller at index holds an LE
// link to addr on which it is the peripheral, which is how a central that
// found Core's advertisement is connected.
func connectedAsPeripheral(index uint16, addr [6]byte) (bool, error) {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW|unix.SOCK_CLOEXEC, btprotoHCI)
	if err != nil {
		return false, fmt.Errorf("open bluetooth socket: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()

	buf := make([]byte, 4+hciConnListMax*hciConnInfoSize)
	binary.NativeEndian.PutUint16(buf[0:], index)
	binary.NativeEndian.PutUint16(buf[2:], hciConnListMax)
	//nolint:gosec // G103: the kernel fills in a buffer this function owns
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), hciGetConnList, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return false, fmt.Errorf("list bluetooth connections: %w", errno)
	}
	return peripheralLinkIn(buf, addr), nil
}

// peripheralLinkIn finds addr among the links of a filled hci_conn_list_req.
func peripheralLinkIn(buf []byte, addr [6]byte) bool {
	if len(buf) < 4 {
		return false
	}
	n := int(binary.NativeEndian.Uint16(buf[2:]))
	for i := range n {
		off := 4 + i*hciConnInfoSize
		if off+hciConnInfoSize > len(buf) {
			return false
		}
		ci := buf[off : off+hciConnInfoSize]
		if [6]byte(ci[2:8]) != addr || ci[8] != hciLELink {
			continue
		}
		return binary.NativeEndian.Uint32(ci[12:])&hciLMMaster == 0
	}
	return false
}

// watchRenewedBonds reads key events until conn is closed.
func (w *bondWindow) watchRenewedBonds(conn mgmtConn, index uint16) {
	buf := make([]byte, 0xffff+mgmtHeaderSize)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		addr, ok := renewedBondAddress(buf[:n], index)
		// The event carries the key; nothing keeps it.
		clear(buf[:n])
		if ok {
			w.renewed(index, addr)
		}
	}
}

// renewed takes a new bond made while the window is open as one the window
// accepted, when the device came in through Core's advertisement.
func (w *bondWindow) renewed(index uint16, addr [6]byte) {
	select {
	case <-w.closed:
		return
	default:
	}
	address := addressString(addr)
	peripheral, err := w.p.linkRole(index, addr)
	if err != nil {
		log.Debug().Err(err).Str("peer", address).Msg("bluetooth bond renewed but its link could not be checked")
		return
	}
	if !peripheral {
		return
	}
	device, ok := w.p.devicePath(address)
	if !ok {
		log.Debug().Str("peer", address).Msg("bluetooth bond renewed for a device bluetoothd does not list")
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.closed:
		return
	default:
	}
	if w.accepted[device] {
		return
	}
	w.accepted[device] = true
	log.Info().Str("peer", address).Msg("bluetooth bond renewed by bluetoothd during Core's pairing")
	go w.p.dropIfStalled(device, w.p.awaitUse(device), w.p.bondStallTimeout)
}

// devicePath finds this adapter's Device1 object for an address. A device
// first seen at a private address keeps that path after its identity is
// learned, so the path cannot be built from the address.
func (p *peripheral) devicePath(address string) (dbus.ObjectPath, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), bondDisconnectTimeout)
	defer cancel()
	objects, err := p.a.managedObjects(ctx)
	if err != nil {
		return "", false
	}
	prefix := p.a.devicePathPrefix()
	for path, ifaces := range objects {
		props, ok := ifaces[deviceIface]
		if !ok || !strings.HasPrefix(string(path), prefix) {
			continue
		}
		if strings.EqualFold(stringProp(props, "Address"), address) {
			return path, true
		}
	}
	return "", false
}
