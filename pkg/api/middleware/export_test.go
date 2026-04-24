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

package middleware

// SetSendCounterForTest simulates counter exhaustion for testing.
func (cs *ClientSession) SetSendCounterForTest(n uint64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.sendCounter = n
}

// FailureBlockCountForTest returns the backoff multiplier (or -1 if not found).
func (m *EncryptionGateway) FailureBlockCountForTest(authToken, sourceIP string) int {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	t, ok := m.failSeen[failKey(authToken, sourceIP)]
	if !ok {
		return -1
	}
	return t.blockCount
}

// CleanupExpiredForTest invokes cleanupExpired for testing.
func (m *EncryptionGateway) CleanupExpiredForTest() {
	m.cleanupExpired()
}

// SaltSeenSizeForTest returns the count of tracked clients in saltSeen.
func (m *EncryptionGateway) SaltSeenSizeForTest() int {
	m.saltMu.Lock()
	defer m.saltMu.Unlock()
	return len(m.saltSeen)
}

// FailSeenSizeForTest returns the count of entries in failSeen.
func (m *EncryptionGateway) FailSeenSizeForTest() int {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	return len(m.failSeen)
}
