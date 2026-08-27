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

package mediadb

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SQLite PRAGMA optimize mask bits, from the pragma documentation.
const (
	optimizeBitAnalyze      = 0x00002 // run ANALYZE on tables that might benefit
	optimizeBitAnalysisCap  = 0x00010 // apply SQLITE_DEFAULT_OPTIMIZE_LIMIT as the analysis limit
	optimizeBitUnqueriedTbl = 0x10000 // consider tables not queried on this connection
)

func parseAnalyzeMask(t *testing.T) int64 {
	t.Helper()
	raw := strings.TrimPrefix(analyzeApproximateMask, "0x")
	mask, err := strconv.ParseInt(raw, 16, 64)
	require.NoError(t, err, "analyzeApproximateMask must be parseable hex")
	return mask
}

// TestAnalyzeApproximateMaskCapsAnalysisWork pins the three bits the mask must
// carry. The mask used to be 0x10002, which omits 0x10; on the MiSTer test
// device that produced silent stalls of up to 117 seconds on a single system,
// with no log output at all.
//
// Do not read the name of this test as a guarantee. Bit 0x10 applies
// SQLITE_DEFAULT_OPTIMIZE_LIMIT (2000) as the analysis limit, but the limit
// does not stop a scan — sqlite3-binding.c statPush makes it seek past the
// current distinct value of the index's leading column instead. On a
// high-cardinality leading column that advances roughly one row per skip and
// the scan walks the whole index anyway. Round 9 of #1279 measured a single
// call at 54,442 ms with this mask in place. The bit is still worth setting;
// it just is not a bound. See analyzeApproximateMask.
func TestAnalyzeApproximateMaskCapsAnalysisWork(t *testing.T) {
	t.Parallel()

	mask := parseAnalyzeMask(t)

	assert.NotZero(t, mask&optimizeBitAnalysisCap,
		"mask %s must set 0x10 so SQLITE_DEFAULT_OPTIMIZE_LIMIT applies at all; without "+
			"it nLimit is 0 and every qualifying ANALYZE is a plain full-index scan (see #1279)",
		analyzeApproximateMask)
	assert.NotZero(t, mask&optimizeBitAnalyze,
		"mask %s must set 0x02 or PRAGMA optimize does nothing", analyzeApproximateMask)
	assert.NotZero(t, mask&optimizeBitUnqueriedTbl,
		"mask %s must set 0x10000 so tables that grew without being queried on "+
			"this connection are still refreshed", analyzeApproximateMask)
}

// TestAnalyzeApproximateMaskValue pins the exact mask so a future edit has to
// be deliberate rather than incidental.
func TestAnalyzeApproximateMaskValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "0x10012", analyzeApproximateMask)

	mask := parseAnalyzeMask(t)
	want := int64(optimizeBitAnalyze | optimizeBitAnalysisCap | optimizeBitUnqueriedTbl)
	assert.Equal(t, want, mask, "mask should be exactly analyze|cap|unqueried")
}
