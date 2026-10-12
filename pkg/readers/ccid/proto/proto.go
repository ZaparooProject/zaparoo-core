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

// Package proto speaks the USB CCID message protocol to one card slot.
//
// It covers what a contactless reader needs: slot status, power on and off,
// and APDU exchange. It knows nothing about USB. Messages travel over a
// Transport, so the same code serves any link that carries CCID messages.
package proto

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	headerLen = 10

	msgPowerOn       = 0x62
	msgPowerOff      = 0x63
	msgGetSlotStatus = 0x65
	msgXfrBlock      = 0x6F
	msgDataBlock     = 0x80
	msgSlotStatus    = 0x81

	cmdStatusFailed        = 1
	cmdStatusTimeExtension = 2

	// maxPayload bounds a response: the length field comes from the reader.
	maxPayload = 65544
	// maxTimeExtensions bounds how long a reader may ask the host to wait.
	maxTimeExtensions = 64
	// maxStaleResponses is how many answers to earlier, abandoned commands
	// are discarded before giving up.
	maxStaleResponses = 4
	// maxEmptyReceives bounds a transport that keeps returning no data.
	maxEmptyReceives = 8
)

// ICCStatus is the card state a slot reports.
type ICCStatus uint8

const (
	// ICCActive is a card that is present and powered.
	ICCActive ICCStatus = 0
	// ICCInactive is a card that is present and not powered. Some readers
	// also report this for a card that has just left.
	ICCInactive ICCStatus = 1
	ICCAbsent   ICCStatus = 2
)

func (s ICCStatus) String() string {
	switch s {
	case ICCActive:
		return "active"
	case ICCInactive:
		return "inactive"
	case ICCAbsent:
		return "absent"
	default:
		return fmt.Sprintf("status(%d)", uint8(s))
	}
}

var (
	ErrShortMessage = errors.New("ccid: message shorter than its header")
	ErrOversized    = errors.New("ccid: message length exceeds the limit")
	ErrSequence     = errors.New("ccid: no response with the expected sequence number")
	ErrMessageType  = errors.New("ccid: unexpected response type")
	ErrTimeExtended = errors.New("ccid: reader kept asking for more time")
	ErrNoData       = errors.New("ccid: reader returned no data")
)

// CommandError is a command the reader reported as failed.
type CommandError struct {
	// ICC is the card state reported alongside the failure.
	ICC ICCStatus
	// Code is the reader's error byte. It is only meaningful on a failure:
	// some readers leave it non-zero on success.
	Code uint8
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("ccid: command failed (card %s, error 0x%02X)", e.ICC, e.Code)
}

// Transport carries CCID messages to and from one reader interface.
type Transport interface {
	// Send writes one complete message.
	Send(ctx context.Context, msg []byte) error
	// Receive returns the next bytes of a response. want is the number of
	// bytes the current message still needs, or 0 when that is not known yet.
	// It may return fewer or more bytes than asked for.
	Receive(ctx context.Context, want int) ([]byte, error)
}

// Conn is a CCID conversation with one slot. It is not safe for concurrent use.
type Conn struct {
	transport Transport
	slot      uint8
	seq       uint8
}

func New(transport Transport, slot uint8) *Conn {
	return &Conn{transport: transport, slot: slot}
}

type response struct {
	payload []byte
	msgType uint8
	status  uint8
	code    uint8
}

func (r response) icc() ICCStatus {
	return ICCStatus(r.status & 0x03)
}

func (r response) cmdStatus() uint8 {
	return r.status >> 6
}

// SlotStatus reports the card state. A reader that answers "no card" as a
// failed command is treated the same as one that answers it as a status.
func (c *Conn) SlotStatus(ctx context.Context) (ICCStatus, error) {
	resp, err := c.command(ctx, msgGetSlotStatus, msgSlotStatus, nil)
	if err != nil {
		return ICCAbsent, err
	}
	return resp.icc(), nil
}

// PowerOn activates the card and returns its answer to reset.
func (c *Conn) PowerOn(ctx context.Context) ([]byte, error) {
	resp, err := c.command(ctx, msgPowerOn, msgDataBlock, nil)
	if err != nil {
		return nil, err
	}
	if resp.cmdStatus() == cmdStatusFailed {
		return nil, &CommandError{ICC: resp.icc(), Code: resp.code}
	}
	return resp.payload, nil
}

// PowerOff deactivates the card.
func (c *Conn) PowerOff(ctx context.Context) error {
	_, err := c.command(ctx, msgPowerOff, msgSlotStatus, nil)
	return err
}

// Transmit sends an APDU to the card and returns its response.
func (c *Conn) Transmit(ctx context.Context, apdu []byte) ([]byte, error) {
	resp, err := c.command(ctx, msgXfrBlock, msgDataBlock, apdu)
	if err != nil {
		return nil, err
	}
	if resp.cmdStatus() == cmdStatusFailed {
		return nil, &CommandError{ICC: resp.icc(), Code: resp.code}
	}
	return resp.payload, nil
}

func (c *Conn) command(ctx context.Context, msgType, wantType uint8, data []byte) (response, error) {
	c.seq++
	msg := make([]byte, headerLen+len(data))
	msg[0] = msgType
	binary.LittleEndian.PutUint32(msg[1:5], uint32(len(data))) //nolint:gosec // APDUs are a few hundred bytes
	msg[5] = c.slot
	msg[6] = c.seq
	copy(msg[headerLen:], data)

	if err := c.transport.Send(ctx, msg); err != nil {
		return response{}, fmt.Errorf("ccid: send: %w", err)
	}

	stale := 0
	for extensions := 0; extensions <= maxTimeExtensions; {
		resp, seq, err := c.receive(ctx)
		if err != nil {
			return response{}, err
		}
		if seq != c.seq {
			// The answer to a command that was abandoned earlier.
			stale++
			if stale > maxStaleResponses {
				return response{}, ErrSequence
			}
			continue
		}
		if resp.cmdStatus() == cmdStatusTimeExtension {
			extensions++
			continue
		}
		if resp.msgType != wantType {
			return response{}, fmt.Errorf("%w: 0x%02X", ErrMessageType, resp.msgType)
		}
		return resp, nil
	}
	return response{}, ErrTimeExtended
}

func (c *Conn) receive(ctx context.Context) (response, uint8, error) {
	var buf []byte
	want := headerLen
	known := false
	empty := 0
	for len(buf) < want {
		ask := 0
		if known {
			ask = want - len(buf)
		}
		chunk, err := c.transport.Receive(ctx, ask)
		if err != nil {
			return response{}, 0, fmt.Errorf("ccid: receive: %w", err)
		}
		if len(chunk) == 0 {
			empty++
			if empty > maxEmptyReceives {
				return response{}, 0, ErrNoData
			}
			continue
		}
		buf = append(buf, chunk...)
		if !known && len(buf) >= headerLen {
			length := binary.LittleEndian.Uint32(buf[1:5])
			if length > maxPayload {
				return response{}, 0, fmt.Errorf("%w: %d bytes", ErrOversized, length)
			}
			want = headerLen + int(length)
			known = true
		}
	}
	return parseResponse(buf[:want])
}

func parseResponse(msg []byte) (response, uint8, error) {
	if len(msg) < headerLen {
		return response{}, 0, ErrShortMessage
	}
	return response{
		msgType: msg[0],
		status:  msg[7],
		code:    msg[8],
		payload: msg[headerLen:],
	}, msg[6], nil
}
