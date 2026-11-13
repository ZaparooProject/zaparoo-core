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
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

// BlueZ 5.61 to 5.63 can fail to give the kernel the LE long-term keys it
// keeps on disk: the 2 s timer meant to catch a missing reply starts when
// the load is queued, not when it is sent, and on expiry it cancels the
// still-queued command (fixed upstream by 5f378404bf, in 5.64). The kernel
// then answers a bonded phone's encryption request with a negative reply and
// the phone drops the link. Core sends the same keys itself, read from the
// same files the same way bluetoothd's load_devices and get_ltk read them.
// Loading a list replaces the kernel's list, so sending the set bluetoothd
// already loaded changes nothing.

const (
	// bluezStorageDir is BlueZ's STORAGEDIR.
	bluezStorageDir = "/var/lib/bluetooth"

	mgmtOpLoadLongTermKeys = 0x0013
	mgmtEvCmdComplete      = 0x0001
	mgmtEvCmdStatus        = 0x0002
	mgmtHeaderSize         = 6

	btprotoHCI        = 1
	hciChannelControl = 3
	hciDevNone        = 0xffff

	// The kernel's address types for LE identity addresses.
	bdaddrLEPublic = 1
	bdaddrLERandom = 2

	// mgmtLTKInfoSize is sizeof(struct mgmt_ltk_info).
	mgmtLTKInfoSize = 36

	// mgmtReplyTimeout bounds the wait for the kernel's reply.
	mgmtReplyTimeout = 3 * time.Second
)

// blockedLTK and blockedIRK are BlueZ's blocked_keys: published keys from
// recalled Google Titan security keys. A device holding either is skipped
// entirely, as bluetoothd does.
var (
	blockedLTK = [16]byte{
		0xbf, 0x01, 0xfb, 0x9d, 0x4e, 0xf3, 0xbc, 0x36,
		0xd8, 0x74, 0xf5, 0x39, 0x41, 0x38, 0x68, 0x4c,
	}
	blockedIRK = [16]byte{
		0xa5, 0x99, 0xba, 0xe4, 0xe1, 0x7c, 0xa6, 0x18,
		0x22, 0x8e, 0x07, 0x56, 0xb4, 0xe8, 0x5f, 0x01,
	}
)

// errMgmtNoReply means the kernel never answered. The keys may still have
// been loaded.
var errMgmtNoReply = errors.New("bluetooth: no reply from the kernel's management interface")

// ltkInfo is one struct mgmt_ltk_info, with every field already in the
// byte order it goes on the wire in.
type ltkInfo struct {
	addr     [6]byte
	val      [16]byte
	rand     [8]byte
	ediv     uint16
	addrType uint8
	keyType  uint8
	central  uint8
	encSize  uint8
}

// keyFile is a parsed GKeyFile: groups of key=value pairs.
type keyFile map[string]map[string]string

// parseKeyFile reads the subset of the GKeyFile format BlueZ writes. Like
// GLib, it stops at the first line it cannot parse and keeps what came
// before, and a later duplicate key replaces an earlier one.
func parseKeyFile(data []byte) keyFile {
	kf := keyFile{}
	var group map[string]string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimLeft(raw, " \t\r\v\f")
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "" || line[0] == '#':
			continue
		case line[0] == '[':
			end := strings.IndexByte(line, ']')
			if end < 0 {
				return kf
			}
			name := line[1:end]
			if kf[name] == nil {
				kf[name] = map[string]string{}
			}
			group = kf[name]
		default:
			eq := strings.IndexByte(line, '=')
			if eq <= 0 || group == nil {
				return kf
			}
			key := strings.TrimRight(line[:eq], " \t")
			group[key] = strings.Trim(line[eq+1:], " \t")
		}
	}
	return kf
}

func (kf keyFile) str(group, key string) (string, bool) {
	g, ok := kf[group]
	if !ok {
		return "", false
	}
	v, ok := g[key]
	return v, ok
}

// integer mirrors g_key_file_get_integer: 0 when the key is missing or the
// value is not a base-10 int.
func (kf keyFile) integer(group, key string) int32 {
	v, ok := kf.str(group, key)
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32)
	if err != nil {
		return 0
	}
	return int32(n)
}

// boolean mirrors g_key_file_get_boolean, reporting false for ok when the
// key is missing or the value is not one GLib accepts.
func (kf keyFile) boolean(group, key string) (value, ok bool) {
	v, found := kf.str(group, key)
	if !found {
		return false, false
	}
	switch strings.TrimSpace(v) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	default:
		return false, false
	}
}

// hexDigit returns the value of one hex digit.
func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

// str2buf mirrors BlueZ's str2buf: each pair of characters fills one byte,
// a pair that does not start with a hex digit leaves it zero, and a short
// string leaves the rest zero.
func str2buf(s string, buf []byte) {
	clear(buf)
	n := min(len(s)/2, len(buf))
	for i := range n {
		hi, ok := hexDigit(s[2*i])
		if !ok {
			continue
		}
		if lo, ok := hexDigit(s[2*i+1]); ok {
			buf[i] = hi<<4 | lo
		} else {
			buf[i] = hi
		}
	}
}

// parseHexKey reads a 16-byte key the way get_ltk and get_irk_info do: at
// least 32 characters, with an optional 0x prefix.
func parseHexKey(s string) ([16]byte, bool) {
	var key [16]byte
	if len(s) < 32 {
		return key, false
	}
	str2buf(strings.TrimPrefix(s, "0x"), key[:])
	return key, true
}

// parseRand reads Rand: 0x-prefixed little-endian hex, or decimal as
// written by older BlueZ. The result is in wire order.
func parseRand(s string) [8]byte {
	var out [8]byte
	if rest, ok := strings.CutPrefix(s, "0x"); ok {
		str2buf(rest, out[:])
		return out
	}
	s = strings.TrimLeft(s, " \t")
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	var v uint64
	if end > 0 {
		parsed, err := strconv.ParseUint(s[:end], 10, 64)
		switch {
		case err == nil:
			v = parsed
		case errors.Is(err, strconv.ErrRange):
			v = ^uint64(0)
		}
	}
	binary.LittleEndian.PutUint64(out[:], v)
	return out
}

// validAddress mirrors BlueZ's bachk: exactly six colon-separated pairs of
// hex digits.
func validAddress(s string) bool {
	if len(s) != 17 {
		return false
	}
	for i := range len(s) {
		if i%3 == 2 {
			if s[i] != ':' {
				return false
			}
			continue
		}
		if _, ok := hexDigit(s[i]); !ok {
			return false
		}
	}
	return true
}

// wireAddress converts a display-order address to the kernel's bdaddr_t,
// which is stored least significant octet first.
func wireAddress(s string) ([6]byte, bool) {
	var out [6]byte
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		return out, false
	}
	for i := range 6 {
		out[i] = hw[5-i]
	}
	return out, true
}

// leAddressType mirrors get_le_addr_type.
func leAddressType(kf keyFile) uint8 {
	if t, ok := kf.str("General", "AddressType"); ok && t == "static" {
		return bdaddrLERandom
	}
	return bdaddrLEPublic
}

// readLTK mirrors get_ltk for one group, reporting false when the group
// holds no usable key.
func readLTK(kf keyFile, peer string, addrType uint8, group string, central bool) (ltkInfo, bool) {
	keyStr, ok := kf.str(group, "Key")
	if !ok {
		return ltkInfo{}, false
	}
	val, ok := parseHexKey(keyStr)
	if !ok {
		return ltkInfo{}, false
	}
	randStr, ok := kf.str(group, "Rand")
	if !ok {
		return ltkInfo{}, false
	}
	addr, ok := wireAddress(peer)
	if !ok {
		return ltkInfo{}, false
	}
	// Keys must belong to an identity address: public, or random static
	// (top two bits set). Others are left over from older daemons.
	if addrType == bdaddrLERandom && addr[5]&0xc0 != 0xc0 {
		return ltkInfo{}, false
	}
	info := ltkInfo{
		addr:     addr,
		addrType: addrType,
		val:      val,
		rand:     parseRand(randStr),
		central:  1,
	}
	// The kernel's fields are narrower than an int; like the C
	// assignments, keep the low bits.
	info.keyType = uint8(kf.integer(group, "Authenticated") & 0xff) //nolint:gosec // masked to 8 bits
	info.encSize = uint8(kf.integer(group, "EncSize") & 0xff)       //nolint:gosec // masked to 8 bits
	info.ediv = uint16(kf.integer(group, "EDiv") & 0xffff)          //nolint:gosec // masked to 16 bits
	if master, ok := kf.boolean(group, "Master"); ok && !master {
		info.central = 0
	}
	if !central {
		info.central = 0
	}
	return info, true
}

// deviceLTKs returns the long-term keys load_devices takes from one
// device's info file, in the order it takes them.
func deviceLTKs(kf keyFile, peer string) []ltkInfo {
	addrType := leAddressType(kf)
	ltk, hasLTK := readLTK(kf, peer, addrType, "LongTermKey", true)
	peripheralLTK, hasPeripheralLTK := readLTK(kf, peer, addrType, "SlaveLongTermKey", false)

	// Any blocked key discards every key of the device.
	if hasLTK && ltk.val == blockedLTK || hasPeripheralLTK && peripheralLTK.val == blockedLTK {
		return nil
	}
	if irkStr, ok := kf.str("IdentityResolvingKey", "Key"); ok {
		if irk, ok := parseHexKey(irkStr); ok && irk == blockedIRK {
			return nil
		}
	}

	var out []ltkInfo
	if hasLTK {
		out = append(out, ltk)
	}
	if hasPeripheralLTK {
		out = append(out, peripheralLTK)
	}
	return out
}

// storedLTKs reads every LE long-term key BlueZ keeps for the adapter at
// adapterAddr. The adapter directory itself must be readable: without it
// there is nothing to compare the kernel's keys against, so the caller must
// leave them alone.
func storedLTKs(fs afero.Fs, root, adapterAddr string) ([]ltkInfo, error) {
	dir := path.Join(root, adapterAddr)
	entries, err := afero.ReadDir(fs, dir)
	if errors.Is(err, os.ErrNotExist) {
		// An adapter with a random static address keeps its storage
		// under that name.
		dir = path.Join(root, "static-"+adapterAddr)
		entries, err = afero.ReadDir(fs, dir)
	}
	if err != nil {
		return nil, fmt.Errorf("read bluetooth storage: %w", err)
	}
	var keys []ltkInfo
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !validAddress(name) {
			continue
		}
		// A missing or unreadable info file is an empty key file to
		// bluetoothd too.
		data, readErr := afero.ReadFile(fs, path.Join(dir, name, "info"))
		if readErr != nil {
			continue
		}
		keys = append(keys, deviceLTKs(parseKeyFile(data), name)...)
	}
	return keys, nil
}

// encodeLoadLTKs builds a complete MGMT_OP_LOAD_LONG_TERM_KEYS command for
// the controller at index.
func encodeLoadLTKs(index uint16, keys []ltkInfo) ([]byte, error) {
	paramLen := 2 + len(keys)*mgmtLTKInfoSize
	if paramLen > 0xffff {
		return nil, fmt.Errorf("too many long-term keys to load: %d", len(keys))
	}
	buf := bytes.NewBuffer(make([]byte, 0, mgmtHeaderSize+paramLen))
	header := [mgmtHeaderSize]byte{}
	binary.LittleEndian.PutUint16(header[0:], mgmtOpLoadLongTermKeys)
	binary.LittleEndian.PutUint16(header[2:], index)
	binary.LittleEndian.PutUint16(header[4:], uint16(paramLen)) //nolint:gosec // bounded above
	buf.Write(header[:])
	var count [2]byte
	binary.LittleEndian.PutUint16(count[:], uint16(len(keys))) //nolint:gosec // bounded above
	buf.Write(count[:])
	for i := range keys {
		k := &keys[i]
		buf.Write(k.addr[:])
		buf.WriteByte(k.addrType)
		buf.WriteByte(k.keyType)
		buf.WriteByte(k.central)
		buf.WriteByte(k.encSize)
		var ediv [2]byte
		binary.LittleEndian.PutUint16(ediv[:], k.ediv)
		buf.Write(ediv[:])
		buf.Write(k.rand[:])
		buf.Write(k.val[:])
	}
	return buf.Bytes(), nil
}

// mgmtConn is one open HCI control channel socket.
type mgmtConn interface {
	Write(b []byte) (int, error)
	Read(b []byte) (int, error)
	SetReadDeadline(t time.Time) error
	Close() error
}

// openMgmt opens the kernel's Bluetooth management interface, which needs
// CAP_NET_ADMIN.
func openMgmt() (mgmtConn, error) {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, btprotoHCI)
	if err != nil {
		return nil, fmt.Errorf("open bluetooth management socket: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrHCI{Dev: hciDevNone, Channel: hciChannelControl}); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("bind bluetooth management socket: %w", err)
	}
	// A non-blocking descriptor joins the runtime poller, which is what
	// makes read deadlines work.
	return os.NewFile(uintptr(fd), "bluetooth-mgmt"), nil
}

// mgmtExchange sends one command and waits for the kernel's reply to it,
// skipping the unrelated events every control socket also receives.
func mgmtExchange(conn mgmtConn, cmd []byte, timeout time.Duration) (status uint8, err error) {
	if len(cmd) < mgmtHeaderSize {
		return 0, errors.New("management command too short")
	}
	opcode := binary.LittleEndian.Uint16(cmd[0:])
	index := binary.LittleEndian.Uint16(cmd[2:])
	if _, err := conn.Write(cmd); err != nil {
		return 0, fmt.Errorf("send management command: %w", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, fmt.Errorf("set management read deadline: %w", err)
	}
	buf := make([]byte, 0xffff+mgmtHeaderSize)
	for {
		n, err := conn.Read(buf)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return 0, errMgmtNoReply
		}
		if err != nil {
			return 0, fmt.Errorf("read management reply: %w", err)
		}
		if n < mgmtHeaderSize+3 {
			continue
		}
		event := binary.LittleEndian.Uint16(buf[0:])
		if event != mgmtEvCmdComplete && event != mgmtEvCmdStatus {
			continue
		}
		if binary.LittleEndian.Uint16(buf[2:]) != index ||
			binary.LittleEndian.Uint16(buf[mgmtHeaderSize:]) != opcode {
			continue
		}
		return buf[mgmtHeaderSize+2], nil
	}
}

// controllerIndex reads the kernel's controller index from a BlueZ adapter
// path such as /org/bluez/hci0.
func controllerIndex(adapterPath string) (uint16, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(path.Base(adapterPath), "hci"), 10, 16)
	if err != nil || !strings.HasPrefix(path.Base(adapterPath), "hci") {
		return 0, fmt.Errorf("no controller index in adapter path %q", adapterPath)
	}
	return uint16(n), nil
}

// reloadLTKs loads the stored keys into the controller at adapterPath. An
// empty store is left alone: bluetoothd would send an empty list, which
// clears the kernel's, but Core gains nothing from clearing it.
func reloadLTKs(
	fs afero.Fs,
	root, adapterAddr, adapterPath string,
	open func() (mgmtConn, error),
) (int, error) {
	index, err := controllerIndex(adapterPath)
	if err != nil {
		return 0, err
	}
	keys, err := storedLTKs(fs, root, adapterAddr)
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}
	cmd, err := encodeLoadLTKs(index, keys)
	if err != nil {
		return 0, err
	}
	conn, err := open()
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	status, err := mgmtExchange(conn, cmd, mgmtReplyTimeout)
	if err != nil {
		return 0, err
	}
	if status != 0 {
		return 0, fmt.Errorf("kernel refused the long-term keys: status 0x%02x", status)
	}
	return len(keys), nil
}

// ReloadLongTermKeys implements KeyReloader.
func (a *adapter) ReloadLongTermKeys(_ context.Context) (int, error) {
	return reloadLTKs(afero.NewOsFs(), bluezStorageDir, a.address, string(a.path), openMgmt)
}
