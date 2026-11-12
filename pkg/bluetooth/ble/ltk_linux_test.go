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
	"encoding/binary"
	"errors"
	"os"
	"path"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAdapterAddr = "E8:48:B8:C8:20:00"
	testPeerAddr    = "90:5F:7A:10:90:A4"
	testKeyHex      = "00112233445566778899AABBCCDDEEFF"
)

// testInfo is an info file the way bluetoothd 5.61 writes one for a phone
// that bonded with Secure Connections.
const testInfo = `[General]
Name=Phone
AddressType=public
SupportedTechnologies=LE;
Trusted=false
Blocked=false
Services=00001800-0000-1000-8000-00805f9b34fb;

[IdentityResolvingKey]
Key=0F0E0D0C0B0A09080706050403020100

[LongTermKey]
Key=00112233445566778899AABBCCDDEEFF
Authenticated=0
EncSize=16
EDiv=0
Rand=0

[ConnectionParameters]
MinInterval=24
`

func TestParseKeyFile(t *testing.T) {
	t.Parallel()

	kf := parseKeyFile([]byte("# comment\n\n  [A]\nx = 1 \r\nx=2\n\t# other\n" +
		"[B]\ny=\n[A]\nz=3\nbroken line\n[C]\nw=4\n"))
	v, ok := kf.str("A", "x")
	assert.True(t, ok)
	assert.Equal(t, "2", v, "a later duplicate replaces an earlier one")
	v, ok = kf.str("A", "z")
	assert.True(t, ok, "a repeated group adds to the first")
	assert.Equal(t, "3", v)
	v, ok = kf.str("B", "y")
	assert.True(t, ok)
	assert.Empty(t, v)
	_, ok = kf.str("C", "w")
	assert.False(t, ok, "parsing stops at a line it cannot read")
	_, ok = kf.str("Missing", "x")
	assert.False(t, ok)

	assert.Empty(t, parseKeyFile([]byte("x=1\n")), "a key outside a group ends parsing")
	assert.Empty(t, parseKeyFile([]byte("[open\n")), "an unclosed group ends parsing")
}

func TestKeyFileNumbers(t *testing.T) {
	t.Parallel()

	kf := parseKeyFile([]byte("[G]\nn=42\nneg=-1\nbad=4x\nbig=99999999999\n" +
		"t=true\none=1\nf=false\nzero=0\nmaybe=yes\n"))
	assert.Equal(t, int32(42), kf.integer("G", "n"))
	assert.Equal(t, int32(-1), kf.integer("G", "neg"))
	assert.Zero(t, kf.integer("G", "bad"))
	assert.Zero(t, kf.integer("G", "big"))
	assert.Zero(t, kf.integer("G", "missing"))

	for key, want := range map[string]bool{"t": true, "one": true, "f": false, "zero": false} {
		got, ok := kf.boolean("G", key)
		assert.True(t, ok, key)
		assert.Equal(t, want, got, key)
	}
	_, ok := kf.boolean("G", "maybe")
	assert.False(t, ok)
	_, ok = kf.boolean("G", "missing")
	assert.False(t, ok)
}

func TestStr2buf(t *testing.T) {
	t.Parallel()

	buf := []byte{9, 9, 9, 9}
	str2buf("0aFF", buf)
	assert.Equal(t, []byte{0x0a, 0xff, 0, 0}, buf, "a short string leaves the rest zero")

	str2buf("zz1g01020304", buf)
	assert.Equal(t, []byte{0, 0x01, 0x01, 0x02}, buf, "a bad pair is zero and a half-good one keeps its first digit")
}

func TestParseRand(t *testing.T) {
	t.Parallel()

	assert.Equal(t, [8]byte{}, parseRand("0"))
	assert.Equal(t, [8]byte{0x15, 0xcd, 0x5b, 0x07}, parseRand("123456789"), "decimal is written little-endian")
	assert.Equal(t, [8]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, parseRand("99999999999999999999999"))
	assert.Equal(t, [8]byte{}, parseRand("junk"))
	assert.Equal(t, [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, parseRand("0x0102030405060708"), "hex is already in wire order")
}

func TestValidAddress(t *testing.T) {
	t.Parallel()

	assert.True(t, validAddress(testPeerAddr))
	assert.True(t, validAddress("aa:bb:cc:dd:ee:ff"))
	for _, bad := range []string{
		"", "cache", "90:5F:7A:10:90:A", "90-5F-7A-10-90-A4", "90:5F:7A:10:90:G4", "90:5F:7A:10:90:A4:",
	} {
		assert.False(t, validAddress(bad), bad)
	}
}

func TestControllerIndex(t *testing.T) {
	t.Parallel()

	idx, err := controllerIndex("/org/bluez/hci3")
	require.NoError(t, err)
	assert.Equal(t, uint16(3), idx)
	for _, bad := range []string{"/org/bluez/foo", "/org/bluez/hcix", "/org/bluez/3", "/org/bluez/hci70000"} {
		_, err := controllerIndex(bad)
		assert.Error(t, err, bad)
	}
}

func TestDeviceLTKs(t *testing.T) {
	t.Parallel()

	keys := deviceLTKs(parseKeyFile([]byte(testInfo)), testPeerAddr)
	require.Len(t, keys, 1)
	k := keys[0]
	assert.Equal(t, [6]byte{0xa4, 0x90, 0x10, 0x7a, 0x5f, 0x90}, k.addr)
	assert.Equal(t, uint8(bdaddrLEPublic), k.addrType)
	assert.Equal(t, uint8(1), k.central)
	assert.Equal(t, uint8(16), k.encSize)
	assert.Equal(t, [16]byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
		0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
	}, k.val)
}

func TestDeviceLTKs_Fields(t *testing.T) {
	t.Parallel()

	info := `[General]
AddressType=static
[LongTermKey]
Key=0x` + testKeyHex + `
Authenticated=2
EncSize=7
EDiv=4660
Rand=0x0102030405060708
Master=false
[SlaveLongTermKey]
Key=` + testKeyHex + `
Authenticated=1
EncSize=16
EDiv=65537
Rand=1
Master=true
`
	keys := deviceLTKs(parseKeyFile([]byte(info)), "C0:00:00:00:00:01")
	require.Len(t, keys, 2)
	assert.Equal(t, uint8(bdaddrLERandom), keys[0].addrType)
	assert.Equal(t, uint8(2), keys[0].keyType)
	assert.Equal(t, uint8(7), keys[0].encSize)
	assert.Equal(t, uint16(4660), keys[0].ediv)
	assert.Equal(t, [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, keys[0].rand)
	assert.Zero(t, keys[0].central, "Master=false clears central")
	assert.Equal(t, keys[1].val, keys[0].val, "the 0x prefix is optional")

	assert.Zero(t, keys[1].central, "the peripheral key is never central")
	assert.Equal(t, uint16(1), keys[1].ediv, "EDiv keeps its low 16 bits")
	assert.Equal(t, [8]byte{1}, keys[1].rand)
}

func TestDeviceLTKs_Skipped(t *testing.T) {
	t.Parallel()

	ltk := func(key, rand string) string {
		s := "[LongTermKey]\nKey=" + key + "\n"
		if rand != "" {
			s += "Rand=" + rand + "\n"
		}
		return s
	}
	cases := map[string]struct {
		info string
		peer string
	}{
		"short key":      {ltk("0011", "0"), testPeerAddr},
		"no rand":        {ltk(testKeyHex, ""), testPeerAddr},
		"no key":         {"[LongTermKey]\nRand=0\n", testPeerAddr},
		"no groups":      {"[General]\nName=x\n", testPeerAddr},
		"random private": {"[General]\nAddressType=static\n" + ltk(testKeyHex, "0"), "40:00:00:00:00:01"},
		"blocked ltk":    {ltk("bf01fb9d4ef3bc36d874f5394138684c", "0"), testPeerAddr},
		"blocked slave": {
			ltk(testKeyHex, "0") + "[SlaveLongTermKey]\nKey=bf01fb9d4ef3bc36d874f5394138684c\nRand=0\n",
			testPeerAddr,
		},
		"blocked irk": {
			ltk(testKeyHex, "0") + "[IdentityResolvingKey]\nKey=a599bae4e17ca618228e0756b4e85f01\n",
			testPeerAddr,
		},
		"bad peer string": {ltk(testKeyHex, "0"), "not-an-address"},
	}
	for name, tc := range cases {
		assert.Empty(t, deviceLTKs(parseKeyFile([]byte(tc.info)), tc.peer), name)
	}

	// A short IRK is no IRK, so it blocks nothing.
	keys := deviceLTKs(parseKeyFile([]byte(ltk(testKeyHex, "0")+"[IdentityResolvingKey]\nKey=a599\n")), testPeerAddr)
	assert.Len(t, keys, 1)
}

func writeInfo(t *testing.T, fs afero.Fs, dir, peer, info string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(path.Join(dir, peer), 0o700))
	require.NoError(t, afero.WriteFile(fs, path.Join(dir, peer, "info"), []byte(info), 0o600))
}

func TestStoredLTKs(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := path.Join(bluezStorageDir, testAdapterAddr)
	writeInfo(t, fs, dir, testPeerAddr, testInfo)
	writeInfo(t, fs, dir, "11:22:33:44:55:66", testInfo)
	writeInfo(t, fs, dir, "cache", testInfo)
	require.NoError(t, fs.MkdirAll(path.Join(dir, "AA:AA:AA:AA:AA:AA"), 0o700))
	require.NoError(t, afero.WriteFile(fs, path.Join(dir, "BB:BB:BB:BB:BB:BB"), []byte(testInfo), 0o600))
	require.NoError(t, afero.WriteFile(fs, path.Join(dir, "settings"), []byte("[General]\n"), 0o600))

	keys, err := storedLTKs(fs, bluezStorageDir, testAdapterAddr)
	require.NoError(t, err)
	assert.Len(t, keys, 2, "only address-named directories with an info file count")

	_, err = storedLTKs(fs, bluezStorageDir, "00:00:00:00:00:01")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestStoredLTKs_StaticAdapter(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeInfo(t, fs, path.Join(bluezStorageDir, "static-"+testAdapterAddr), testPeerAddr, testInfo)
	keys, err := storedLTKs(fs, bluezStorageDir, testAdapterAddr)
	require.NoError(t, err)
	assert.Len(t, keys, 1)
}

func TestEncodeLoadLTKs(t *testing.T) {
	t.Parallel()

	key := ltkInfo{
		addr:     [6]byte{1, 2, 3, 4, 5, 6},
		addrType: 1,
		keyType:  2,
		central:  1,
		encSize:  16,
		ediv:     0x1234,
		rand:     [8]byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17},
		val: [16]byte{
			0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
			0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf,
		},
	}
	got, err := encodeLoadLTKs(2, []ltkInfo{key})
	require.NoError(t, err)
	want := []byte{
		0x13, 0x00, // opcode
		0x02, 0x00, // index
		38, 0x00, // parameter length
		0x01, 0x00, // key count
		1, 2, 3, 4, 5, 6, // address
		1,          // address type
		2,          // key type
		1,          // central
		16,         // encryption size
		0x34, 0x12, // ediv
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf,
	}
	assert.Equal(t, want, got)

	empty, err := encodeLoadLTKs(0, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x13, 0, 0, 0, 2, 0, 0, 0}, empty)

	_, err = encodeLoadLTKs(0, make([]ltkInfo, 0xffff/mgmtLTKInfoSize+1))
	assert.Error(t, err)
}

// fakeMgmt replays queued reads and records writes.
type fakeMgmt struct {
	writeErr error
	readErr  error
	written  [][]byte
	reads    [][]byte
	closed   bool
}

func (f *fakeMgmt) Write(b []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.written = append(f.written, append([]byte(nil), b...))
	return len(b), nil
}

func (f *fakeMgmt) Read(b []byte) (int, error) {
	if len(f.reads) == 0 {
		if f.readErr != nil {
			return 0, f.readErr
		}
		return 0, os.ErrDeadlineExceeded
	}
	n := copy(b, f.reads[0])
	f.reads = f.reads[1:]
	return n, nil
}

func (*fakeMgmt) SetReadDeadline(time.Time) error { return nil }

func (f *fakeMgmt) Close() error {
	f.closed = true
	return nil
}

func mgmtEvent(event, index, opcode uint16, status uint8) []byte {
	b := make([]byte, 9)
	binary.LittleEndian.PutUint16(b[0:], event)
	binary.LittleEndian.PutUint16(b[2:], index)
	binary.LittleEndian.PutUint16(b[4:], 3)
	binary.LittleEndian.PutUint16(b[6:], opcode)
	b[8] = status
	return b
}

func TestMgmtExchange(t *testing.T) {
	t.Parallel()

	cmd, err := encodeLoadLTKs(0, nil)
	require.NoError(t, err)
	conn := &fakeMgmt{reads: [][]byte{
		{1, 2}, // too short
		mgmtEvent(0x0006, 0, mgmtOpLoadLongTermKeys, 0),            // new settings, not a reply
		mgmtEvent(mgmtEvCmdComplete, 1, mgmtOpLoadLongTermKeys, 0), // another controller
		mgmtEvent(mgmtEvCmdComplete, 0, 0x0005, 0),                 // another command
		mgmtEvent(mgmtEvCmdStatus, 0, mgmtOpLoadLongTermKeys, 0x0d),
	}}
	status, err := mgmtExchange(conn, cmd, time.Second)
	require.NoError(t, err)
	assert.Equal(t, uint8(0x0d), status)
	assert.Equal(t, [][]byte{cmd}, conn.written)

	_, err = mgmtExchange(&fakeMgmt{}, cmd, time.Second)
	require.ErrorIs(t, err, errMgmtNoReply)

	_, err = mgmtExchange(&fakeMgmt{readErr: errors.New("boom")}, cmd, time.Second)
	require.ErrorContains(t, err, "boom")

	_, err = mgmtExchange(&fakeMgmt{writeErr: os.ErrPermission}, cmd, time.Second)
	require.ErrorIs(t, err, os.ErrPermission)

	_, err = mgmtExchange(&fakeMgmt{}, cmd[:3], time.Second)
	assert.Error(t, err)
}

func TestReloadLTKs(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := path.Join(bluezStorageDir, testAdapterAddr)
	writeInfo(t, fs, dir, testPeerAddr, testInfo)

	conn := &fakeMgmt{reads: [][]byte{mgmtEvent(mgmtEvCmdComplete, 0, mgmtOpLoadLongTermKeys, 0)}}
	n, err := reloadLTKs(fs, bluezStorageDir, testAdapterAddr, "/org/bluez/hci0",
		func() (mgmtConn, error) { return conn, nil })
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.True(t, conn.closed)
	require.Len(t, conn.written, 1)
	assert.Len(t, conn.written[0], mgmtHeaderSize+2+mgmtLTKInfoSize)

	conn = &fakeMgmt{reads: [][]byte{mgmtEvent(mgmtEvCmdComplete, 0, mgmtOpLoadLongTermKeys, 0x0d)}}
	_, err = reloadLTKs(fs, bluezStorageDir, testAdapterAddr, "/org/bluez/hci0",
		func() (mgmtConn, error) { return conn, nil })
	require.ErrorContains(t, err, "status 0x0d")

	_, err = reloadLTKs(fs, bluezStorageDir, testAdapterAddr, "/org/bluez/hci0",
		func() (mgmtConn, error) { return nil, os.ErrPermission })
	require.ErrorIs(t, err, os.ErrPermission)

	_, err = reloadLTKs(fs, bluezStorageDir, testAdapterAddr, "/org/bluez/nothci",
		func() (mgmtConn, error) { t.Fatal("must not open"); return nil, os.ErrInvalid })
	assert.Error(t, err)
}

func TestReloadLTKs_NothingToLoadLeavesTheKernelAlone(t *testing.T) {
	t.Parallel()

	opened := false
	open := func() (mgmtConn, error) {
		opened = true
		return &fakeMgmt{}, nil
	}

	fs := afero.NewMemMapFs()
	_, err := reloadLTKs(fs, bluezStorageDir, testAdapterAddr, "/org/bluez/hci0", open)
	require.Error(t, err, "an unreadable store is an error")

	require.NoError(t, fs.MkdirAll(path.Join(bluezStorageDir, testAdapterAddr), 0o700))
	n, err := reloadLTKs(fs, bluezStorageDir, testAdapterAddr, "/org/bluez/hci0", open)
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.False(t, opened, "an empty list would wipe the kernel's keys")
}
