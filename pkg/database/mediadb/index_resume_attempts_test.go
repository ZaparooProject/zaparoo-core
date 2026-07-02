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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMediaDB_IndexResumeAttempts_RoundTrip exercises the persisted resume-attempt
// counter that bounds automatic reindex resumes: a missing key reads as zero,
// Increment returns and persists successive values, and Reset clears it.
func TestMediaDB_IndexResumeAttempts_RoundTrip(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// A fresh DB has no stored counter; it must default to zero rather than error.
	attempts, err := mediaDB.GetIndexResumeAttempts()
	require.NoError(t, err)
	assert.Equal(t, 0, attempts, "unset counter defaults to zero")

	// Each increment returns the new value and persists it.
	next, err := mediaDB.IncrementIndexResumeAttempts()
	require.NoError(t, err)
	assert.Equal(t, 1, next)

	next, err = mediaDB.IncrementIndexResumeAttempts()
	require.NoError(t, err)
	assert.Equal(t, 2, next)

	got, err := mediaDB.GetIndexResumeAttempts()
	require.NoError(t, err)
	assert.Equal(t, 2, got, "incremented value survives a re-read")

	// Reset returns the counter to zero.
	require.NoError(t, mediaDB.ResetIndexResumeAttempts())
	got, err = mediaDB.GetIndexResumeAttempts()
	require.NoError(t, err)
	assert.Equal(t, 0, got, "reset clears the counter")

	// Incrementing after a reset starts from one again, giving a future
	// interruption a fresh resume budget.
	next, err = mediaDB.IncrementIndexResumeAttempts()
	require.NoError(t, err)
	assert.Equal(t, 1, next)
}

// TestMediaDB_IndexResumeAttempts_CorruptValue verifies the counter surfaces a
// parse error instead of panicking or silently returning zero when the stored
// value is not an integer (e.g. a corrupted DBConfig row).
func TestMediaDB_IndexResumeAttempts_CorruptValue(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	_, err := mediaDB.sql.Load().ExecContext(context.Background(),
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigIndexResumeAttempts, "not-a-number",
	)
	require.NoError(t, err)

	_, err = mediaDB.GetIndexResumeAttempts()
	require.Error(t, err, "a non-integer stored value must be reported as an error")
}
