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
	"encoding/hex"
	"errors"
	"path"
	"strconv"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePCSC is a PC/SC service with a fixed set of readers.
type fakePCSC struct {
	establishErr error
	listErr      error
	connectErr   error
	status       map[string]pcscStatus
	names        []string
	connects     []string
	cards        []*fakePCSCCard
	released     int
}

type fakePCSCCard struct {
	transmitErr   error
	transmits     int
	disconnected  bool
	resetOnRemove bool
}

func (c *fakePCSCCard) Transmit([]byte) ([]byte, error) {
	c.transmits++
	if c.transmitErr != nil {
		return nil, c.transmitErr
	}
	return []byte{0x90, 0x00}, nil
}

func (c *fakePCSCCard) Disconnect(reset bool) error {
	c.disconnected = true
	c.resetOnRemove = reset
	return nil
}

func (f *fakePCSC) establish() (pcscContext, error) {
	if f.establishErr != nil {
		return nil, f.establishErr
	}
	return f, nil
}

func (f *fakePCSC) ListReaders() ([]string, error) {
	return f.names, f.listErr
}

func (f *fakePCSC) Status(name string) (pcscStatus, error) {
	status, ok := f.status[name]
	if !ok {
		return pcscStatus{}, errReaderGone
	}
	return status, nil
}

func (f *fakePCSC) Connect(name string) (pcscCard, error) {
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	f.connects = append(f.connects, name)
	card := &fakePCSCCard{}
	f.cards = append(f.cards, card)
	return card, nil
}

func (f *fakePCSC) Release() error {
	f.released++
	return nil
}

const (
	pcscACR122 = "ACS ACR122U PICC Interface 00 00"
	pcscSAM    = "ACS ACR1552 1S CL Reader [ACR1552 1S CL Reader SAM] (RR620-032040) 02 00"
	pcscYubi   = "Yubico YubiKey OTP+FIDO+CCID 03 00"
)

func TestPCSCBackend_List(t *testing.T) {
	t.Parallel()
	service := &fakePCSC{
		names: []string{pcscACR122, pcscSAM, pcscYubi, "Unplugged Reader 04 00"},
		status: map[string]pcscStatus{
			pcscACR122: {},
			pcscSAM:    {Present: true},
			pcscYubi:   {Present: true, ATR: atrSecurityKey},
		},
	}
	b := newPCSCBackend(service.establish)

	infos, err := b.List(context.Background())

	require.NoError(t, err)
	require.Len(t, infos, 3, "a reader that vanished between listing and status is left out")

	assert.Equal(t, readerInfo{Path: pcscACR122, Name: pcscACR122, Known: true}, infos[0])
	assert.False(t, infos[1].Known)
	assert.NotNil(t, infos[1].ATR, "a card with no answer to reset is still a card")
	assert.Empty(t, infos[1].ATR)
	assert.Equal(t, atrSecurityKey, infos[2].ATR)

	for i, want := range []bool{false, true, true} {
		present, presentErr := b.CardPresent(context.Background(), &infos[i])
		require.NoError(t, presentErr)
		assert.Equal(t, want, present, infos[i].Name)
	}

	require.NoError(t, b.Close())
	require.NoError(t, b.Close())
	assert.Equal(t, 1, service.released)
}

func TestPCSCBackend_ServiceNotAvailable(t *testing.T) {
	t.Parallel()
	errNoService := errors.New("no service")

	b := newPCSCBackend((&fakePCSC{establishErr: errNoService}).establish)
	_, err := b.List(context.Background())
	require.ErrorIs(t, err, errNoService)
	_, err = b.Open(context.Background(), &readerInfo{Path: pcscACR122})
	require.ErrorIs(t, err, errNoService)
	require.NoError(t, b.Close())

	b = newPCSCBackend((&fakePCSC{listErr: errNoService}).establish)
	_, err = b.List(context.Background())
	require.ErrorIs(t, err, errNoService)
}

func openPCSC(t *testing.T, service *fakePCSC, name string) device {
	t.Helper()
	dev, err := newPCSCBackend(service.establish).Open(context.Background(), &readerInfo{Path: name})
	require.NoError(t, err)
	return dev
}

func TestPCSCDevice_ConnectsOnlyToAContactlessCard(t *testing.T) {
	t.Parallel()
	service := &fakePCSC{status: map[string]pcscStatus{pcscYubi: {Present: true, ATR: atrSecurityKey}}}
	dev := openPCSC(t, service, pcscYubi)

	for range 3 {
		state, err := dev.Poll(context.Background())
		require.NoError(t, err)
		assert.False(t, state.Present)
		assert.Equal(t, atrSecurityKey, state.ATR, "the answer to reset is reported so the reader can be ruled out")
	}

	assert.Empty(t, service.connects, "a security key is never connected to")
	_, err := dev.Transmit(context.Background(), []byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
	require.ErrorIs(t, err, errNoCard)
}

func TestPCSCDevice_TagLifecycle(t *testing.T) {
	t.Parallel()
	service := &fakePCSC{status: map[string]pcscStatus{pcscACR122: {}}}
	dev := openPCSC(t, service, pcscACR122)
	ctx := context.Background()

	state, err := dev.Poll(ctx)
	require.NoError(t, err)
	assert.False(t, state.Present)

	service.status[pcscACR122] = pcscStatus{Present: true, ATR: atrNTAG}
	for range 3 {
		state, err = dev.Poll(ctx)
		require.NoError(t, err)
		assert.True(t, state.Present)
		assert.Equal(t, atrNTAG, state.ATR)
	}
	require.Len(t, service.connects, 1, "one connection lasts for as long as the tag stays")

	resp, err := dev.Transmit(ctx, []byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
	require.NoError(t, err)
	assert.Equal(t, []byte{0x90, 0x00}, resp)

	dev.Reactivate(ctx)
	assert.True(t, service.cards[0].disconnected)
	assert.True(t, service.cards[0].resetOnRemove)
	_, err = dev.Poll(ctx)
	require.NoError(t, err)
	assert.Len(t, service.connects, 2)

	service.status[pcscACR122] = pcscStatus{}
	state, err = dev.Poll(ctx)
	require.NoError(t, err)
	assert.False(t, state.Present)
	assert.True(t, service.cards[1].disconnected)
	assert.False(t, service.cards[1].resetOnRemove)

	require.NoError(t, dev.Close())
}

func TestPCSCDevice_Failures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("reader unplugged", func(t *testing.T) {
		t.Parallel()
		service := &fakePCSC{status: map[string]pcscStatus{pcscACR122: {Present: true, ATR: atrNTAG}}}
		dev := openPCSC(t, service, pcscACR122)
		_, err := dev.Poll(ctx)
		require.NoError(t, err)

		delete(service.status, pcscACR122)
		_, err = dev.Poll(ctx)

		require.ErrorIs(t, err, errReaderGone)
		assert.True(t, service.cards[0].disconnected)
	})

	t.Run("tag left before the connect", func(t *testing.T) {
		t.Parallel()
		service := &fakePCSC{
			status:     map[string]pcscStatus{pcscACR122: {Present: true, ATR: atrNTAG}},
			connectErr: errors.New("card removed"),
		}
		state, err := openPCSC(t, service, pcscACR122).Poll(ctx)
		require.NoError(t, err)
		assert.False(t, state.Present)
	})

	t.Run("reader gone at the connect", func(t *testing.T) {
		t.Parallel()
		service := &fakePCSC{
			status:     map[string]pcscStatus{pcscACR122: {Present: true, ATR: atrNTAG}},
			connectErr: errReaderGone,
		}
		_, err := openPCSC(t, service, pcscACR122).Poll(ctx)
		require.ErrorIs(t, err, errReaderGone)
	})

	t.Run("transmit error", func(t *testing.T) {
		t.Parallel()
		service := &fakePCSC{status: map[string]pcscStatus{pcscACR122: {Present: true, ATR: atrNTAG}}}
		dev := openPCSC(t, service, pcscACR122)
		_, err := dev.Poll(ctx)
		require.NoError(t, err)
		errRemoved := errors.New("card was removed")
		service.cards[0].transmitErr = errRemoved

		_, err = dev.Transmit(ctx, []byte{0xFF, 0xCA, 0x00, 0x00, 0x00})

		require.ErrorIs(t, err, errRemoved)
	})

	t.Run("open a reader that is not there", func(t *testing.T) {
		t.Parallel()
		_, err := newPCSCBackend((&fakePCSC{}).establish).Open(ctx, &readerInfo{Path: pcscACR122})
		require.ErrorIs(t, err, errReaderGone)
	})
}

// Raw descriptors read from sysfs on the three readers.
const (
	descACR122U = "12011001000000082f07002214020102000109025d00010100806409040000030b00000036210001000702" +
		"000000a00f0000a00f000000002a000090d0030000000100000000000000000000400002000f010000000000000001" +
		"070581030800320705020240000107058202400001"
	descACR1552 = "12010002000000402f0703230501010203010902c30003010080fa09040000030b0000043621100100070300" +
		"000088130000f834000000d29d01008cee0c0000fa0100000000000000000000ba04040004020000ffff0000000107" +
		"050102400000070581024000000705840308000a09040100020b000005362110010001030000008813000088130000" +
		"00813400006889090000fa0100000000000000000000ba04040004020000ffff000000010705030240000007058302" +
		"40000009040200010300000609211101210122150007058603080007"
	descBR500 = "12011001000000086e09230602020102030109025d00010100809609040000030b0000003621100100070300" +
		"0000740e0000740e000000da26000048db040011040200000700000000000000be04040004020000ffff0000000107" +
		"0583024000000705040240000007058103080018"
)

// sysDevice adds one USB device to a fake sysfs.
func sysDevice(t *testing.T, fs afero.Fs, name, descriptors, manufacturer, product string, bus, dev int) {
	t.Helper()
	raw, err := hex.DecodeString(descriptors)
	require.NoError(t, err)
	dir := path.Join(sysfsUSBDevices, name)
	files := map[string][]byte{
		"descriptors":  raw,
		"manufacturer": []byte(manufacturer + "\n"),
		"product":      []byte(product + "\n"),
		"busnum":       []byte(strconv.Itoa(bus) + "\n"),
		"devnum":       []byte(strconv.Itoa(dev) + "\n"),
	}
	for file, data := range files {
		require.NoError(t, afero.WriteFile(fs, path.Join(dir, file), data, 0o644))
	}
}

// sysInterface adds an interface directory with the given kernel driver.
func sysInterface(t *testing.T, fs afero.Fs, device, iface, driver string) {
	t.Helper()
	uevent := "DEVTYPE=usb_interface\n"
	if driver != "" {
		uevent += "DRIVER=" + driver + "\n"
	}
	uevent += "INTERFACE=11/0/0\n"
	file := path.Join(sysfsUSBDevices, device, device+":1."+iface, "uevent")
	require.NoError(t, afero.WriteFile(fs, file, []byte(uevent), 0o644))
}

// fakeLink is a claimed interface that answers every command with one status.
type fakeLink struct {
	sent   [][]byte
	status byte
	closed bool
}

func (l *fakeLink) Send(_ context.Context, msg []byte) error {
	l.sent = append(l.sent, msg)
	return nil
}

func (l *fakeLink) Receive(context.Context, int) ([]byte, error) {
	last := l.sent[len(l.sent)-1]
	reply := make([]byte, 10)
	reply[0] = 0x81
	binary.LittleEndian.PutUint32(reply[1:5], 0)
	reply[6] = last[6]
	reply[7] = l.status
	return reply, nil
}

func (l *fakeLink) Close() error {
	l.closed = true
	return nil
}

func newUSBBackend(fs afero.Fs, link *fakeLink, opened *[]usbTarget) *usbBackend {
	return &usbBackend{
		fs: fs,
		open: func(target *usbTarget) (usbLink, error) {
			if opened != nil {
				*opened = append(*opened, *target)
			}
			return link, nil
		},
	}
}

func TestUSBBackend_List(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	sysDevice(t, fs, "1-1", descACR122U, "ACS", "ACR122U PICC Interface", 1, 5)
	sysInterface(t, fs, "1-1", "0", "pn533_usb")
	sysDevice(t, fs, "1-2", descACR1552, "ACS", "ACR1552", 1, 6)
	sysInterface(t, fs, "1-2", "0", "")
	sysInterface(t, fs, "1-2", "1", "")
	sysInterface(t, fs, "1-2", "2", "usbhid")
	sysDevice(t, fs, "1-1.2", descBR500, "Feitian", "bR500", 1, 7)
	sysInterface(t, fs, "1-1.2", "0", "usbfs")
	// A hub: descriptors with no smart card interface.
	hub := []byte{
		0x12, 0x01, 0x00, 0x02, 0x09, 0x00, 0x01, 0x40, 0x6b,
		0x1d, 0x02, 0x00, 0x18, 0x06, 0x03, 0x02, 0x01, 0x01,
	}
	require.NoError(t, afero.WriteFile(fs, path.Join(sysfsUSBDevices, "usb1", "descriptors"), hub, 0o644))

	infos, err := newUSBBackend(fs, &fakeLink{}, nil).List(context.Background())

	require.NoError(t, err)
	require.Len(t, infos, 4)

	assert.Equal(t, "usb:1-1.2:0", infos[0].Path)
	assert.Equal(t, "Feitian bR500", infos[0].Name)
	assert.True(t, infos[0].Busy, "an interface a program has claimed is not free to open")
	assert.NotEmpty(t, infos[0].Unusable)

	assert.Equal(t, "usb:1-1:0", infos[1].Path)
	assert.True(t, infos[1].Known)
	assert.Empty(t, infos[1].Unusable, "a kernel driver is detached on open, so the reader is usable")
	assert.Equal(t, map[string]string{
		"usbID":         "072f:2200",
		"interface":     "0",
		"exchangeLevel": "short APDU",
		"features":      "00020040",
		"protocols":     "00000002",
		"maxMessage":    "271",
		"slots":         "1",
		"kernelDriver":  "pn533_usb",
	}, infos[1].Details)

	assert.Equal(t, "usb:1-2:0", infos[2].Path)
	assert.True(t, infos[2].Known)
	assert.Equal(t, "usb:1-2:1", infos[3].Path)
	assert.False(t, infos[3].Known, "the SAM interface is not on the contactless list")
}

func TestUSBBackend_SkipsReadersThatNeedTheHostToRunTheCardProtocol(t *testing.T) {
	t.Parallel()
	raw, err := hex.DecodeString(descBR500)
	require.NoError(t, err)
	// Clear the exchange level bits of dwFeatures to make it a TPDU reader.
	classDesc := 18 + 9 + 9
	raw[classDesc+42] = 0x01
	fs := afero.NewMemMapFs()
	sysDevice(t, fs, "1-3", hex.EncodeToString(raw), "", "", 1, 8)

	infos, err := newUSBBackend(fs, &fakeLink{}, nil).List(context.Background())

	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "096e:0623", infos[0].Name, "a reader with no name strings is named by its USB ID")
	assert.Equal(t, "reader does not exchange whole APDUs", infos[0].Unusable)
	assert.False(t, infos[0].Busy)
}

func TestUSBBackend_NoSysfs(t *testing.T) {
	t.Parallel()
	_, err := newUSBBackend(afero.NewMemMapFs(), &fakeLink{}, nil).List(context.Background())
	require.Error(t, err)
}

func TestUSBBackend_OpenAndCardPresent(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	sysDevice(t, fs, "1-2", descACR1552, "ACS", "ACR1552", 1, 6)
	link := &fakeLink{status: 0x02}
	var opened []usbTarget
	b := newUSBBackend(fs, link, &opened)
	ctx := context.Background()
	info := &readerInfo{Path: "usb:1-2:1"}

	present, err := b.CardPresent(ctx, info)
	require.NoError(t, err)
	assert.False(t, present)
	assert.True(t, link.closed, "the interface is released after the check")

	link.status = 0x01
	present, err = b.CardPresent(ctx, info)
	require.NoError(t, err)
	assert.True(t, present, "an inactive card counts: only power-on shows whether it is real")

	dev, err := b.Open(ctx, info)
	require.NoError(t, err)
	require.NotNil(t, dev)
	require.Len(t, opened, 3)
	assert.Equal(t, uint8(1), opened[2].iface.Number)
	assert.Equal(t, uint8(0x83), opened[2].iface.BulkIn)
	assert.Equal(t, 1, opened[2].bus)
	assert.Equal(t, 6, opened[2].dev)
	assert.Equal(t, uint16(0x072F), opened[2].vendor)
	assert.Equal(t, uint16(0x2303), opened[2].product)

	link.closed = false
	require.NoError(t, dev.Close())
	assert.True(t, link.closed)
	require.NoError(t, b.Close())
}

func TestUSBBackend_OpenFailures(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	sysDevice(t, fs, "1-2", descACR1552, "ACS", "ACR1552", 1, 6)
	ctx := context.Background()
	errBusy := errors.New("device or resource busy")
	b := &usbBackend{fs: fs, open: func(*usbTarget) (usbLink, error) { return nil, errBusy }}

	_, err := b.Open(ctx, &readerInfo{Path: "usb:1-2:0"})
	require.ErrorIs(t, err, errBusy)
	_, err = b.CardPresent(ctx, &readerInfo{Path: "usb:1-2:0"})
	require.ErrorIs(t, err, errBusy)

	_, err = b.Open(ctx, &readerInfo{Path: "usb:1-2:7"})
	require.ErrorIs(t, err, errReaderGone, "an interface the device does not have")
	_, err = b.Open(ctx, &readerInfo{Path: "usb:9-9:0"})
	require.ErrorIs(t, err, errReaderGone, "a device that has been unplugged")

	for _, bad := range []string{"", "ACS ACR122 0", "usb:", "usb:1-2", "usb::0", "usb:1-2:x", "usb:1-2:999"} {
		_, err = b.Open(ctx, &readerInfo{Path: bad})
		require.ErrorIs(t, err, errBadUSBPath, "path %q", bad)
	}
}

func TestUSBPath_RoundTrip(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		sysName string
		iface   uint8
	}{{"1-1", 0}, {"1-1.2", 0}, {"3-2.4.1", 1}} {
		sysName, iface, err := parseUSBPath(usbPath(tt.sysName, tt.iface))
		require.NoError(t, err)
		assert.Equal(t, tt.sysName, sysName)
		assert.Equal(t, tt.iface, iface)
	}
}
