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

package userdb

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureInfoLogs records zerolog output at Info, which is the level a stock
// install runs at. Anything these tests assert on has to survive that.
func captureInfoLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf strings.Builder
	prevLogger := log.Logger
	prevLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = zerolog.New(&buf).Level(zerolog.InfoLevel)
	t.Cleanup(func() {
		log.Logger = prevLogger
		zerolog.SetGlobalLevel(prevLevel)
	})
	fn()
	return buf.String()
}

// logRecord returns the first captured record with the given message.
func logRecord(t *testing.T, out, message string) (map[string]any, bool) {
	t.Helper()
	for line := range strings.Lines(out) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["message"] == message {
			return rec, true
		}
	}
	return nil, false
}

// A migration that takes minutes on a large library used to be
// indistinguishable from a hang: everything in the migration path logged at
// Debug, which is off by default, and goose's own line only arrived once the
// work was already done. The names have to be in the log at Info before the
// work starts, or there is nothing to look at while waiting.
func TestMigrateUp_NamesPendingMigrationsBeforeRunning(t *testing.T) {
	// Not parallel: capturing logs swaps the global logger, so another test
	// migrating at the same time would write into this one's buffer.

	dbPath := filepath.Join(t.TempDir(), "user.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	out := captureInfoLogs(t, func() {
		require.NoError(t, sqlMigrateUp(db, dbPath))
	})

	rec, found := logRecord(t, out, "applying database migrations")
	require.True(t, found, "pending migrations must be named at Info before they run:\n%s", out)

	assert.Equal(t, "user.db", rec["db"], "the line has to say which database")

	count, ok := rec["count"].(float64)
	require.True(t, ok, "count must be numeric")
	assert.Positive(t, count, "a fresh database has migrations to apply")

	migrations, ok := rec["migrations"].([]any)
	require.True(t, ok, "the line has to name the migrations")
	require.NotEmpty(t, migrations)
	for _, name := range migrations {
		assert.True(t,
			strings.HasSuffix(name.(string), ".sql"),
			"each entry should be a migration file name, got %v", name,
		)
	}

	done, found := logRecord(t, out, "database migrations applied")
	require.True(t, found, "completion must also be reported at Info:\n%s", out)
	assert.InDelta(t, count, done["count"], 0, "the same set of migrations has to be reported both times")
}

// An already-migrated database has no work to report, so it must stay quiet.
// Startup logs are read by people looking for a problem; a line every boot
// saying nothing happened makes that harder.
func TestMigrateUp_SaysNothingWhenThereIsNoWork(t *testing.T) {
	// Not parallel: see above.

	dbPath := filepath.Join(t.TempDir(), "user.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	require.NoError(t, sqlMigrateUp(db, dbPath))

	out := captureInfoLogs(t, func() {
		require.NoError(t, sqlMigrateUp(db, dbPath))
	})

	_, found := logRecord(t, out, "applying database migrations")
	assert.False(t, found, "a database with nothing to do must not announce migrations:\n%s", out)
}
