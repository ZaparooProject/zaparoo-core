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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/ccid/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSlot is a CCID slot whose status and power-on results a test sets.
type fakeSlot struct {
	statusErr  error
	powerErr   error
	atr        []byte
	powerOns   int
	transmits  int
	status     proto.ICCStatus
	afterFail  proto.ICCStatus
	failsPower bool
}

func (s *fakeSlot) SlotStatus(context.Context) (proto.ICCStatus, error) {
	return s.status, s.statusErr
}

func (s *fakeSlot) PowerOn(context.Context) ([]byte, error) {
	s.powerOns++
	if s.powerErr != nil {
		return nil, s.powerErr
	}
	if s.failsPower {
		s.status = s.afterFail
		return nil, &proto.CommandError{ICC: s.afterFail, Code: 0xF6}
	}
	s.status = proto.ICCActive
	return s.atr, nil
}

func (s *fakeSlot) Transmit(context.Context, []byte) ([]byte, error) {
	s.transmits++
	return []byte{0x90, 0x00}, nil
}

func poll(t *testing.T, d *slotDevice) cardState {
	t.Helper()
	state, err := d.Poll(context.Background())
	require.NoError(t, err)
	return state
}

// The ACR122U and bR500 report a new tag as active before the host has
// powered it on.
func TestSlotDevice_TagThatArrivesActive(t *testing.T) {
	t.Parallel()
	slot := &fakeSlot{status: proto.ICCAbsent, atr: atrNTAG}
	d := newSlotDevice(slot, nil)

	assert.False(t, poll(t, d).Present)
	assert.Zero(t, slot.powerOns)

	slot.status = proto.ICCActive
	state := poll(t, d)
	assert.True(t, state.Present)
	assert.Equal(t, atrNTAG, state.ATR)

	poll(t, d)
	poll(t, d)
	assert.Equal(t, 1, slot.powerOns, "a tag that stays is never powered on again")

	slot.status = proto.ICCAbsent
	assert.False(t, poll(t, d).Present)
}

// The ACR1552 reports a new tag as inactive, and reports a tag that has left
// the same way until a power-on fails.
func TestSlotDevice_TagThatArrivesAndLeavesInactive(t *testing.T) {
	t.Parallel()
	slot := &fakeSlot{status: proto.ICCInactive, atr: atrNTAG}
	d := newSlotDevice(slot, nil)

	assert.True(t, poll(t, d).Present, "an inactive slot is given a power-on")
	assert.Equal(t, 1, slot.powerOns)

	slot.status = proto.ICCInactive
	slot.failsPower = true
	slot.afterFail = proto.ICCAbsent
	assert.False(t, poll(t, d).Present, "a failed power-on means the tag has gone")
	assert.False(t, poll(t, d).Present)

	slot.status = proto.ICCInactive
	slot.failsPower = false
	assert.True(t, poll(t, d).Present, "the next tag is picked up")
}

// The ACR1552 SAM slot claims a card and never powers it.
func TestSlotDevice_SlotThatNeverActivatesIsAskedOnce(t *testing.T) {
	t.Parallel()
	slot := &fakeSlot{status: proto.ICCInactive, failsPower: true, afterFail: proto.ICCInactive}
	d := newSlotDevice(slot, nil)

	for range 5 {
		state := poll(t, d)
		assert.False(t, state.Present)
		assert.Nil(t, state.ATR)
	}

	assert.Equal(t, 1, slot.powerOns)
}

func TestSlotDevice_ReactivatePowersOnAgain(t *testing.T) {
	t.Parallel()
	slot := &fakeSlot{status: proto.ICCActive, atr: atrPlaceholder}
	d := newSlotDevice(slot, nil)
	require.Equal(t, atrPlaceholder, poll(t, d).ATR)

	slot.atr = atrNTAG
	d.Reactivate(context.Background())

	assert.Equal(t, atrNTAG, poll(t, d).ATR)
	assert.Equal(t, 2, slot.powerOns)
}

func TestSlotDevice_TransmitNeedsAnActivatedCard(t *testing.T) {
	t.Parallel()
	slot := &fakeSlot{status: proto.ICCAbsent, atr: atrNTAG}
	d := newSlotDevice(slot, nil)

	_, err := d.Transmit(context.Background(), []byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
	require.ErrorIs(t, err, errNoCard)
	assert.Zero(t, slot.transmits)

	slot.status = proto.ICCActive
	poll(t, d)
	resp, err := d.Transmit(context.Background(), []byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
	require.NoError(t, err)
	assert.Equal(t, []byte{0x90, 0x00}, resp)
}

func TestSlotDevice_TransportErrorsAreReported(t *testing.T) {
	t.Parallel()

	t.Run("status", func(t *testing.T) {
		t.Parallel()
		d := newSlotDevice(&fakeSlot{statusErr: errReaderGone}, nil)
		_, err := d.Poll(context.Background())
		require.ErrorIs(t, err, errReaderGone)
	})

	t.Run("power on", func(t *testing.T) {
		t.Parallel()
		d := newSlotDevice(&fakeSlot{status: proto.ICCActive, powerErr: errReaderGone}, nil)
		_, err := d.Poll(context.Background())
		require.ErrorIs(t, err, errReaderGone, "a transport failure is not a card that would not power on")
	})
}

func TestSlotDevice_Close(t *testing.T) {
	t.Parallel()
	errClose := errors.New("close failed")

	require.NoError(t, newSlotDevice(&fakeSlot{}, nil).Close())
	require.ErrorIs(t, newSlotDevice(&fakeSlot{}, func() error { return errClose }).Close(), errClose)
}
