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

import (
	"encoding/binary"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

const (
	MaxSequenceNumber = 1<<64 - 1<<13 - 1 // Same as WireGuard's RejectAfterMessages
	NonceCacheSize    = 100               // Maximum number of cached nonces
)

// ReplayProtector combines WireGuard's sequence number validation with nonce replay protection
type ReplayProtector struct {
	nonceCache []string
	filter     Filter
	lastSeq    uint64
}

// NewReplayProtector creates a new replay protector from client state
func NewReplayProtector(client *database.Client) *ReplayProtector {
	rp := &ReplayProtector{
		nonceCache: make([]string, len(client.NonceCache)),
		lastSeq:    client.CurrentSeq,
	}
	copy(rp.nonceCache, client.NonceCache)

	// Initialize filter from stored ring buffer state
	if len(client.SeqWindow) >= 8+len(rp.filter.ring)*8 {
		storedLastSeq := binary.LittleEndian.Uint64(client.SeqWindow[0:8])
		if storedLastSeq > 0 {
			// Load stored state: first 8 bytes = last sequence, then 8 bytes per ring block
			rp.filter.last = storedLastSeq
			for i := range rp.filter.ring {
				offset := 8 + i*8
				rp.filter.ring[i] = block(binary.LittleEndian.Uint64(client.SeqWindow[offset : offset+8]))
			}
		} else {
			// Buffer exists but uninitialized - treat as new client
			rp.filter.Reset()
			if client.CurrentSeq > 0 {
				rp.filter.ValidateCounter(client.CurrentSeq, MaxSequenceNumber)
			}
		}
	} else {
		// Buffer too small - initialize fresh
		rp.filter.Reset()
		if client.CurrentSeq > 0 {
			rp.filter.ValidateCounter(client.CurrentSeq, MaxSequenceNumber)
		}
	}

	return rp
}

// ValidateSequenceAndNonce validates both sequence number and nonce for replay protection
func (rp *ReplayProtector) ValidateSequenceAndNonce(seq uint64, nonce string) bool {
	// Check nonce first (replay protection)
	if slices.Contains(rp.nonceCache, nonce) {
		return false
	}

	// Then validate sequence number using WireGuard's algorithm
	return rp.filter.ValidateCounter(seq, MaxSequenceNumber)
}

// UpdateSequenceAndNonce updates the replay protector state after successful validation
func (rp *ReplayProtector) UpdateSequenceAndNonce(seq uint64, nonce string) {
	// Update nonce cache (keep last NonceCacheSize nonces)
	rp.nonceCache = append(rp.nonceCache, nonce)
	if len(rp.nonceCache) > NonceCacheSize {
		rp.nonceCache = rp.nonceCache[1:] // Remove oldest
	}

	// Sequence is already updated by ValidateCounter if it was accepted
	rp.lastSeq = max(rp.lastSeq, seq)
}

// GetStateForDatabase returns the state that should be stored in the database
func (rp *ReplayProtector) GetStateForDatabase() (currentSeq uint64, seqWindow []byte, nonceCache []string) {
	// Serialize ring buffer state
	// Format: [8 bytes last seq][8 bytes per ring block]
	seqWindow = make([]byte, 8+len(rp.filter.ring)*8)
	binary.LittleEndian.PutUint64(seqWindow[0:8], rp.filter.last)

	for i, block := range rp.filter.ring[:] {
		offset := 8 + i*8
		binary.LittleEndian.PutUint64(seqWindow[offset:offset+8], uint64(block))
	}

	return rp.filter.last, seqWindow, rp.nonceCache
}
