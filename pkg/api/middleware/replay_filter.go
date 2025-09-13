// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package middleware

/*
ATTRIBUTION NOTICE:

This file contains code derived from WireGuard's anti-replay filter implementation.

Original work: Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
Original license: MIT License
Source: https://github.com/WireGuard/wireguard-go/blob/master/replay/replay.go

The derived portions remain under their original MIT license as required.
The combined work is distributed under GPL-3.0-or-later as indicated in the file header.
*/

type block uint64

const (
	blockBitLog = 6
	blockBits   = 1 << blockBitLog
	ringBlocks  = 1 << 7
	windowSize  = (ringBlocks - 1) * blockBits
	blockMask   = ringBlocks - 1
	bitMask     = blockBits - 1
)

// Filter implements an anti-replay mechanism as specified in RFC 6479.
// It rejects replayed messages by checking if message counter value is within
// a sliding window of previously received messages.
type Filter struct {
	last uint64
	ring [ringBlocks]block
}

// Reset resets the filter to an empty state.
func (f *Filter) Reset() {
	f.last = 0
	for i := range f.ring {
		f.ring[i] = 0
	}
}

// ValidateCounter checks if a given counter should be accepted.
// It automatically rejects counters >= the specified limit.
func (f *Filter) ValidateCounter(counter, limit uint64) bool {
	if counter >= limit {
		return false
	}

	indexBlock := counter >> blockBitLog

	if counter > f.last {
		// New highest counter - shift window
		current := f.last >> blockBitLog
		diff := indexBlock - current
		if diff > ringBlocks {
			diff = ringBlocks
		}
		for i := current + 1; i <= current+diff; i++ {
			f.ring[i&blockMask] = 0
		}
		f.last = counter
	} else if f.last-counter > windowSize {
		// Too old
		return false
	}

	indexBlock &= blockMask
	indexBit := counter & bitMask
	old := f.ring[indexBlock]
	updated := old | 1<<indexBit
	f.ring[indexBlock] = updated
	return old != updated
}
