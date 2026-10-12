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
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag/tagsim"
)

// Answers to reset as the three readers reported them.
var (
	atrNTAG      = storageATR(0x00, 0x03, 0x68)
	atrClassic1K = storageATR(0x00, 0x01, 0x6A)
	atrClassic4K = storageATR(0x00, 0x02, 0x69)
	// atrPlaceholder is what the bR500 reports before it has identified a tag.
	atrPlaceholder = storageATR(0x00, 0xFF, 0x94)
	// atrDESFire is an ISO 14443-4 card: contactless, but not a storage card.
	atrDESFire = []byte{0x3B, 0x81, 0x80, 0x01, 0x80, 0x80}
	// atrSecurityKey is a contact-interface device, as a YubiKey reports over USB.
	atrSecurityKey = []byte{
		0x3B, 0xFD, 0x13, 0x00, 0x00, 0x81, 0x31, 0xFE, 0x15, 0x80, 0x73, 0xC0,
		0x21, 0xC0, 0x57, 0x59, 0x75, 0x62, 0x69, 0x4B, 0x65, 0x79, 0x40,
	}
)

// storageATR builds the PC/SC storage card answer to reset for a Type A tag
// with the given card name and checksum.
func storageATR(nameHigh, nameLow, checksum byte) []byte {
	return []byte{
		0x3B, 0x8F, 0x80, 0x01, 0x80, 0x4F, 0x0C, 0xA0, 0x00, 0x00, 0x03, 0x06,
		0x03, nameHigh, nameLow, 0x00, 0x00, 0x00, 0x00, checksum,
	}
}

// simProfile is how one reader's firmware answers the storage card commands.
// The values are what each reader did on the bench.
type simProfile struct {
	name string
	// maxRead and maxWrite are the largest transfers accepted on a Type 2 tag.
	maxRead  int
	maxWrite int
	// swFail is the status for a rejected command, swRange for a read past
	// the end of the tag.
	swFail  uint16
	swRange uint16
	// emptyOnZeroLength answers a read of length 00 with success and no data.
	emptyOnZeroLength bool
}

var (
	profileACR1552 = simProfile{name: "ACR1552", maxRead: 255, maxWrite: 16, swFail: 0x6982, swRange: 0x6982}
	profileBR500   = simProfile{
		name: "bR500", maxRead: 16, maxWrite: 4, swFail: 0x6300, swRange: 0x6A82, emptyOnZeroLength: true,
	}
	profileACR122U = simProfile{
		name: "ACR122U", maxRead: 16, maxWrite: 4, swFail: 0x6300, swRange: 0x6300, emptyOnZeroLength: true,
	}
	simProfiles = []simProfile{profileACR1552, profileBR500, profileACR122U}
)

// simTag is a tag the simulated reader can hold.
type simTag interface {
	nfctag.BlockIO
	UID() []byte
}

// simDevice is a reader with a tag on it, behind the driver's device seam.
type simDevice struct {
	pollErr error
	tag     simTag
	atr     []byte
	// transmits is every APDU the driver sent.
	transmits [][]byte
	profile   simProfile
	// notReady is how many UID requests still get the placeholder answer.
	notReady      int
	reactivations int
	failReadsFrom int
	reads         int
	key           nfctag.Key
	// attached is false for a card the device will not talk to.
	attached bool
	closed   bool
}

var _ device = (*simDevice)(nil)

func newSimDevice(profile simProfile) *simDevice {
	return &simDevice{profile: profile}
}

// place puts a tag on the reader.
func (d *simDevice) place(tag simTag) {
	d.tag = tag
	d.attached = true
	switch t := tag.(type) {
	case *tagsim.Type2:
		d.atr = atrNTAG
	case *tagsim.Classic:
		d.atr = atrClassic1K
		if len(t.Blocks) > 64 {
			d.atr = atrClassic4K
		}
	}
}

// placeCard puts a card on the reader that reports only an answer to reset.
func (d *simDevice) placeCard(atr []byte, attached bool) {
	d.tag = nil
	d.atr = atr
	d.attached = attached
}

func (d *simDevice) remove() {
	d.tag = nil
	d.atr = nil
	d.attached = false
}

func (d *simDevice) Poll(context.Context) (cardState, error) {
	if d.pollErr != nil {
		return cardState{}, d.pollErr
	}
	if d.atr == nil {
		return cardState{}, nil
	}
	return cardState{ATR: d.atr, Present: d.attached}, nil
}

func (d *simDevice) Reactivate(context.Context) {
	d.reactivations++
}

func (d *simDevice) Close() error {
	d.closed = true
	return nil
}

func sw(code uint16, data ...byte) []byte {
	return append(data, byte(code>>8), byte(code&0xFF))
}

func (d *simDevice) Transmit(ctx context.Context, apdu []byte) ([]byte, error) {
	d.transmits = append(d.transmits, append([]byte(nil), apdu...))
	if !d.attached {
		return nil, errNoCard
	}
	if len(apdu) < apduHeaderLen || apdu[0] != claPCSC {
		return sw(0x6E00), nil
	}

	switch apdu[1] {
	case insGetData:
		return d.getUID(), nil
	case insReadBinary:
		return d.read(ctx, int(apdu[3]), int(apdu[4])), nil
	case insUpdateBinary:
		return d.write(ctx, int(apdu[3]), apdu[apduHeaderLen:]), nil
	case insLoadKey:
		copy(d.key[:], apdu[apduHeaderLen:])
		return sw(swSuccess), nil
	case insAuthenticate:
		return d.authenticate(ctx, apdu), nil
	default:
		return sw(d.profile.swFail), nil
	}
}

func (d *simDevice) getUID() []byte {
	if d.notReady > 0 {
		d.notReady--
		return sw(swSuccess)
	}
	if d.tag == nil {
		// A card that is not a storage card still has a UID.
		return sw(swSuccess, 0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66)
	}
	return sw(swSuccess, d.tag.UID()...)
}

func (d *simDevice) read(ctx context.Context, addr, length int) []byte {
	if d.tag == nil {
		return sw(d.profile.swFail)
	}
	d.reads++
	if d.failReadsFrom != 0 && d.reads >= d.failReadsFrom {
		return sw(d.profile.swFail)
	}
	if length == 0 && d.profile.emptyOnZeroLength {
		return sw(swSuccess)
	}
	if length > d.profile.maxRead || length > nfctag.BlockSize {
		return sw(d.profile.swFail)
	}
	block, err := d.tag.ReadBlock(ctx, addr)
	if err != nil {
		if errors.Is(err, tagsim.ErrOutOfRange) {
			return sw(d.profile.swRange)
		}
		return sw(d.profile.swFail)
	}
	return sw(swSuccess, block[:length]...)
}

func (d *simDevice) write(ctx context.Context, addr int, data []byte) []byte {
	if d.tag == nil {
		return sw(d.profile.swFail)
	}
	if _, isType2 := d.tag.(*tagsim.Type2); isType2 && len(data) > d.profile.maxWrite {
		return sw(d.profile.swFail)
	}
	if err := d.tag.WriteBlock(ctx, addr, data); err != nil {
		return sw(d.profile.swFail)
	}
	return sw(swSuccess)
}

func (d *simDevice) authenticate(ctx context.Context, apdu []byte) []byte {
	classic, ok := d.tag.(*tagsim.Classic)
	if !ok || len(apdu) < 10 {
		return sw(d.profile.swFail)
	}
	if err := classic.Authenticate(ctx, int(apdu[7]), nfctag.KeyType(apdu[8]), d.key); err != nil {
		return sw(d.profile.swFail)
	}
	return sw(swSuccess)
}

// fakeBackend is a backend with a fixed set of readers.
type fakeBackend struct {
	listErr error
	devices map[string]*simDevice
	// cards is which readers report a card to CardPresent.
	cards  map[string]bool
	name   string
	infos  []readerInfo
	opened []string
	closed int
}

var _ backend = (*fakeBackend)(nil)

func newFakeBackend(name string) *fakeBackend {
	return &fakeBackend{name: name, devices: make(map[string]*simDevice), cards: make(map[string]bool)}
}

// add registers a reader and returns the device behind it.
func (b *fakeBackend) add(info *readerInfo, profile simProfile) *simDevice {
	b.infos = append(b.infos, *info)
	dev := newSimDevice(profile)
	b.devices[info.Path] = dev
	return dev
}

func (b *fakeBackend) Name() string {
	return b.name
}

func (b *fakeBackend) List(context.Context) ([]readerInfo, error) {
	if b.listErr != nil {
		return nil, b.listErr
	}
	return append([]readerInfo(nil), b.infos...), nil
}

func (b *fakeBackend) CardPresent(_ context.Context, info *readerInfo) (bool, error) {
	return b.cards[info.Path], nil
}

func (b *fakeBackend) Open(_ context.Context, info *readerInfo) (device, error) {
	dev, ok := b.devices[info.Path]
	if !ok {
		return nil, errReaderGone
	}
	b.opened = append(b.opened, info.Path)
	return dev, nil
}

func (b *fakeBackend) Close() error {
	b.closed++
	return nil
}

// newTestReader returns a reader on the given backends with its own registry.
func newTestReader(backends ...backend) *Reader {
	return &Reader{
		newBackends: func() []backend { return backends },
		registry:    newRegistry(),
		writeWait:   writeTimeout,
	}
}

// attach returns a reader already open on dev, with no polling goroutine, so
// a test drives it one tick at a time.
func attach(t *testing.T, dev device, info *readerInfo) (r *Reader, iq chan readers.Scan) {
	t.Helper()
	r = newTestReader()
	r.dev = dev
	r.info = *info
	r.open = true
	return r, make(chan readers.Scan, 16)
}

// ticks polls the reader n times.
func ticks(t *testing.T, r *Reader, iq chan readers.Scan, n int) {
	t.Helper()
	for range n {
		if r.tick(context.Background(), iq) {
			return
		}
	}
}

// drain returns every scan waiting on the queue.
func drain(iq chan readers.Scan) []readers.Scan {
	var scans []readers.Scan
	for {
		select {
		case scan := <-iq:
			scans = append(scans, scan)
		default:
			return scans
		}
	}
}

// textTLV is the NDEF data a tag holds for a short text record.
//
//nolint:gosec // test text is far shorter than 255 bytes
func textTLV(text string) []byte {
	record := append([]byte{0xD1, 0x01, byte(len(text) + 3), 'T', 0x02, 'e', 'n'}, text...)
	out := append([]byte{0x03, byte(len(record))}, record...)
	return append(out, 0xFE)
}

func ntagWithText(text string) *tagsim.Type2 {
	tag := tagsim.NewType2(tagsim.PagesNTAG215)
	tag.SetUser(textTLV(text))
	return tag
}

func classicWithText(text string) *tagsim.Classic {
	tag := tagsim.NewClassic(16)
	tag.Format()
	tag.SetData(textTLV(text))
	return tag
}
