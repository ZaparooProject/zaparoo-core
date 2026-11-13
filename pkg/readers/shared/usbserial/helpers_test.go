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
	"encoding/binary"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// ctrlCall is one control transfer seen by fakeDevice.
type ctrlCall struct {
	data    []byte
	length  int
	value   uint16
	index   uint16
	reqType uint8
	request uint8
}

// fakeDevice is a usbDevice that records control transfers and answers them
// and data transfers through scripted functions.
type fakeDevice struct {
	// onControl answers a control transfer. IN replies are written to buf.
	// When nil, IN requests are answered with zeros and OUT ones succeed.
	onControl func(call ctrlCall, buf []byte) (int, error)
	// onTransfer answers a bulk or interrupt transfer.
	onTransfer func(ep uint8, data []byte, timeout time.Duration) (int, error)
	calls      []ctrlCall
	halts      []uint8
	mu         syncutil.Mutex
	closed     int
}

func (d *fakeDevice) control(
	reqType, request uint8, value, index uint16, data []byte, _ time.Duration,
) (int, error) {
	call := ctrlCall{reqType: reqType, request: request, value: value, index: index, length: len(data)}
	if reqType&0x80 == 0 && len(data) > 0 {
		call.data = append([]byte(nil), data...)
	}
	d.mu.Lock()
	d.calls = append(d.calls, call)
	onControl := d.onControl
	d.mu.Unlock()
	if onControl != nil {
		return onControl(call, data)
	}
	if reqType&0x80 != 0 {
		clear(data)
	}
	return len(data), nil
}

func (d *fakeDevice) transfer(ep uint8, data []byte, timeout time.Duration) (int, error) {
	if d.onTransfer == nil {
		return 0, errTimeout
	}
	return d.onTransfer(ep, data, timeout)
}

func (d *fakeDevice) clearHalt(ep uint8) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.halts = append(d.halts, ep)
	return nil
}

func (d *fakeDevice) close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed++
	return nil
}

func (d *fakeDevice) takeCalls() []ctrlCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	calls := d.calls
	d.calls = nil
	return calls
}

// out is an expected OUT control transfer with no data stage.
func out(reqType, request uint8, value, index uint16) ctrlCall {
	return ctrlCall{reqType: reqType, request: request, value: value, index: index}
}

// in is an expected IN control transfer of length bytes.
func in(reqType, request uint8, value, index uint16, length int) ctrlCall {
	return ctrlCall{reqType: reqType, request: request, value: value, index: index, length: length}
}

// usbDescriptors builds the sysfs descriptors of a single-interface adapter
// with an interrupt IN, bulk OUT and bulk IN endpoint.
func usbDescriptors(vid, pid, bcdUSB, bcdDevice uint16, class, maxPacket0 uint8) []byte {
	config := []byte{
		9, descTypeConfig, 39, 0, 1, 1, 0, 0x80, 50,
		9, descTypeInterface, 0, 0, 3, 0xFF, 0, 0, 0,
		7, descTypeEndpoint, 0x81, endpointIntr, 10, 0, 1,
		7, descTypeEndpoint, 0x02, endpointBulk, 64, 0, 0,
		7, descTypeEndpoint, 0x83, endpointBulk, 64, 0, 0,
	}
	raw := make([]byte, 0, 18+len(config))
	raw = append(raw, 18, descTypeDevice)
	raw = binary.LittleEndian.AppendUint16(raw, bcdUSB)
	raw = append(raw, class, 0, 0, maxPacket0)
	raw = binary.LittleEndian.AppendUint16(raw, vid)
	raw = binary.LittleEndian.AppendUint16(raw, pid)
	raw = binary.LittleEndian.AppendUint16(raw, bcdDevice)
	raw = append(raw, 1, 2, 3, 1)
	return append(raw, config...)
}
