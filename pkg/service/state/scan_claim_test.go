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

package state

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newClaimState(t *testing.T) *State {
	t.Helper()
	st, _ := NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	return st
}

func TestDeliverScanToClaims(t *testing.T) {
	t.Parallel()
	st := newClaimState(t)

	assert.False(t, st.DeliverScanToClaims(tokens.Token{UID: "04a1"}), "nobody waiting")

	first, releaseFirst := st.ClaimNextScan("a")
	defer releaseFirst()
	second, releaseSecond := st.ClaimNextScan("b")
	defer releaseSecond()
	require.True(t, st.HasScanClaims())

	require.True(t, st.DeliverScanToClaims(tokens.Token{UID: "04a1"}))
	assert.False(t, st.HasScanClaims(), "a claim takes one scan")

	for _, c := range []*ScanClaim{first, second} {
		got, ok := <-c.Scans()
		require.True(t, ok)
		assert.Equal(t, "04a1", got.UID)
		_, ok = <-c.Scans()
		assert.False(t, ok, "the claim closes after its scan")
	}

	assert.False(t, st.DeliverScanToClaims(tokens.Token{UID: "04a2"}), "the next scan runs as usual")
}

func TestCancelScanClaims(t *testing.T) {
	t.Parallel()
	st := newClaimState(t)

	mine, releaseMine := st.ClaimNextScan("a")
	defer releaseMine()
	theirs, releaseTheirs := st.ClaimNextScan("b")
	defer releaseTheirs()

	assert.Equal(t, 1, st.CancelScanClaims("a"))
	_, ok := <-mine.Scans()
	assert.False(t, ok, "a cancelled claim closes with no token")
	assert.Zero(t, st.CancelScanClaims("a"))

	require.True(t, st.DeliverScanToClaims(tokens.Token{UID: "04a1"}))
	got := <-theirs.Scans()
	assert.Equal(t, "04a1", got.UID)
}

func TestClaimNextScanRelease(t *testing.T) {
	t.Parallel()
	st := newClaimState(t)

	_, release := st.ClaimNextScan("a")
	release()
	release()
	assert.False(t, st.HasScanClaims())
	assert.False(t, st.DeliverScanToClaims(tokens.Token{UID: "04a1"}))
}

func TestReaderConnectError(t *testing.T) {
	t.Parallel()
	st := newClaimState(t)

	assert.Empty(t, st.ReaderConnectError("pn532:/dev/ttyUSB0"))
	st.SetReaderConnectError("pn532:/dev/ttyUSB0", "no such device")
	assert.Equal(t, "no such device", st.ReaderConnectError("pn532:/dev/ttyUSB0"))
	st.SetReaderConnectError("pn532:/dev/ttyUSB0", "")
	assert.Empty(t, st.ReaderConnectError("pn532:/dev/ttyUSB0"))
}
