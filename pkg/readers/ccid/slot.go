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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/ccid/proto"
)

// slotConn is the CCID conversation a slot is driven through.
type slotConn interface {
	SlotStatus(ctx context.Context) (proto.ICCStatus, error)
	PowerOn(ctx context.Context) ([]byte, error)
	Transmit(ctx context.Context, apdu []byte) ([]byte, error)
}

// slotDevice turns a CCID slot's status reports into "is there a tag".
//
// Readers report presence differently. Some show a new tag as active straight
// away; one shows it as inactive until it is powered on, and shows a tag that
// has left the same way. So an inactive slot is always given one power-on
// attempt: success is a tag and failure is none. A slot that keeps claiming a
// card it will not power, such as an empty SAM socket, is asked once and then
// left alone until its status changes.
//
// A tag that is present is never powered off and on again: at least one
// reader answers with a placeholder identity straight after that.
type slotDevice struct {
	conn       slotConn
	closer     func() error
	atr        []byte
	lastStatus proto.ICCStatus
	powered    bool
	gaveUp     bool
}

var _ device = (*slotDevice)(nil)

func newSlotDevice(conn slotConn, closer func() error) *slotDevice {
	return &slotDevice{conn: conn, closer: closer}
}

func (d *slotDevice) Poll(ctx context.Context) (cardState, error) {
	status, err := d.conn.SlotStatus(ctx)
	if err != nil {
		return cardState{}, fmt.Errorf("slot status: %w", err)
	}

	if status == proto.ICCAbsent {
		d.forget()
		d.gaveUp = false
		return cardState{}, nil
	}
	if d.powered && status == proto.ICCActive {
		return cardState{ATR: d.atr, Present: true}, nil
	}
	if d.gaveUp && status == d.lastStatus {
		return cardState{}, nil
	}

	atr, err := d.conn.PowerOn(ctx)
	if err != nil {
		var cmdErr *proto.CommandError
		if !errors.As(err, &cmdErr) {
			return cardState{}, fmt.Errorf("power on: %w", err)
		}
		d.forget()
		d.gaveUp = true
		d.lastStatus = cmdErr.ICC
		return cardState{}, nil
	}

	d.atr = atr
	d.powered = true
	d.gaveUp = false
	return cardState{ATR: atr, Present: true}, nil
}

func (d *slotDevice) forget() {
	d.atr = nil
	d.powered = false
}

func (d *slotDevice) Transmit(ctx context.Context, apdu []byte) ([]byte, error) {
	if !d.powered {
		return nil, errNoCard
	}
	resp, err := d.conn.Transmit(ctx, apdu)
	if err != nil {
		return nil, fmt.Errorf("transmit: %w", err)
	}
	return resp, nil
}

func (d *slotDevice) Reactivate(context.Context) {
	d.forget()
	d.gaveUp = false
}

func (d *slotDevice) Close() error {
	if d.closer == nil {
		return nil
	}
	return d.closer()
}
