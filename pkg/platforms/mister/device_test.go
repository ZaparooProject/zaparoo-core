//go:build linux && !android

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

package mister

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSMBus answers word reads from a queue; an empty queue fails the read.
type fakeSMBus struct {
	words  []uint16
	reads  int
	closed bool
}

func (b *fakeSMBus) ReadWord(register byte) (uint16, bool) {
	b.reads++
	if register != batteryCapacity || len(b.words) == 0 {
		return 0, false
	}
	word := b.words[0]
	b.words = b.words[1:]
	return word, true
}

func (b *fakeSMBus) Close() { b.closed = true }

func newTestBoard(buses map[int]*fakeSMBus) (board *batteryBoard, opened *[]int, now *time.Time) {
	opened = &[]int{}
	current := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	now = &current
	board = &batteryBoard{
		open: func(bus int) (smbusDevice, bool) {
			*opened = append(*opened, bus)
			device, ok := buses[bus]
			if !ok {
				return nil, false
			}
			return device, true
		},
		sleep: func(time.Duration) {},
		now:   func() time.Time { return *now },
	}
	return board, opened, now
}

func TestValidCapacity(t *testing.T) {
	t.Parallel()

	for _, word := range []uint16{0, 50, 100} {
		percent, ok := validCapacity(word)
		assert.True(t, ok)
		assert.Equal(t, int(word), percent)
	}
	// Above range, and the error words a waking board answers with, which are
	// negative when read as signed.
	for _, word := range []uint16{101, 255, 0xFFFF, 0x8000} {
		_, ok := validCapacity(word)
		assert.False(t, ok, word)
	}
}

func TestBatteryBoard_ReadsTheBusThatAnswers(t *testing.T) {
	t.Parallel()

	bus := &fakeSMBus{words: []uint16{0xFFFF, 0xFFFF, 73}}
	board, opened, _ := newTestBoard(map[int]*fakeSMBus{1: bus})

	percent, ok := board.percent()
	require.True(t, ok)
	assert.Equal(t, 73, percent)
	assert.Equal(t, []int{0, 1}, *opened, "the search stops at the bus the battery is on")
	assert.Equal(t, 3, bus.reads, "a board that is waking up is retried")
	assert.True(t, bus.closed)
}

func TestBatteryBoard_GivesUpOnABoardThatNeverAnswers(t *testing.T) {
	t.Parallel()

	bus := &fakeSMBus{}
	board, _, _ := newTestBoard(map[int]*fakeSMBus{0: bus})

	_, ok := board.percent()
	assert.False(t, ok)
	assert.Equal(t, batteryReadTries, bus.reads)
	assert.True(t, bus.closed)
}

func TestBatteryBoard_LeavesTheBusAloneAfterFindingNothing(t *testing.T) {
	t.Parallel()

	board, opened, now := newTestBoard(map[int]*fakeSMBus{})

	_, ok := board.percent()
	assert.False(t, ok)
	assert.Equal(t, []int{0, 1, 2}, *opened)

	*now = now.Add(batteryAbsentRetry - time.Second)
	_, ok = board.percent()
	assert.False(t, ok)
	assert.Len(t, *opened, 3, "no bus is opened again inside the retry window")

	*now = now.Add(time.Second)
	_, _ = board.percent()
	assert.Len(t, *opened, 6)
}

func TestPlatform_DevicePower(t *testing.T) {
	t.Parallel()

	t.Run("no battery board", func(t *testing.T) {
		t.Parallel()
		board, _, _ := newTestBoard(map[int]*fakeSMBus{})
		detail, err := (&Platform{battery: board}).DevicePower()
		require.NoError(t, err)
		assert.Equal(t, power.Detail{Source: power.SourceExternal}, detail)
	})

	t.Run("a platform built without one", func(t *testing.T) {
		t.Parallel()
		detail, err := (&Platform{}).DevicePower()
		require.NoError(t, err)
		assert.False(t, detail.Present)
	})

	t.Run("battery board reports only its charge", func(t *testing.T) {
		t.Parallel()
		board, _, _ := newTestBoard(map[int]*fakeSMBus{0: {words: []uint16{64}}})
		detail, err := (&Platform{battery: board}).DevicePower()
		require.NoError(t, err)
		assert.True(t, detail.Present)
		assert.Equal(t, 64, *detail.Percent)
		assert.Empty(t, detail.Source)
		assert.Empty(t, detail.State)
		require.Len(t, detail.Batteries, 1)
	})
}

func TestPlatform_PowerControlIsRebootOnly(t *testing.T) {
	t.Parallel()

	p := &Platform{}
	assert.Equal(t, map[hoststatus.PowerAction]hoststatus.Availability{
		hoststatus.PowerReboot: hoststatus.Supported,
	}, p.PowerActions(context.Background()))

	commit, err := p.PreparePowerAction(context.Background(), hoststatus.PowerReboot)
	require.NoError(t, err)
	assert.NotNil(t, commit)

	_, err = p.PreparePowerAction(context.Background(), hoststatus.PowerShutdown)
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = p.PreparePowerAction(context.Background(), hoststatus.PowerSuspend)
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
}
