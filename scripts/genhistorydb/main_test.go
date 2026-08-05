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

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readGeneratedHistory reads the tool's output back through the production
// userdb reader the sweep itself uses, so a row shape the sweep would reject
// fails the test.
func readGeneratedHistory(t *testing.T, outDir string) []database.MediaHistoryEntry {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: outDir})
	db, err := userdb.OpenUserDB(context.Background(), pl)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	entries, err := db.GetMediaHistoryIdentityBackfillBatch(
		0, database.CurrentMediaIdentityPolicyVersion, 1000,
	)
	require.NoError(t, err)
	return entries
}

// indexedMediaDB returns the path to a closed media.db holding the given
// indexed paths, ready for the tool's read-only handle.
func indexedMediaDB(t *testing.T, systemID string, paths ...string) string {
	t.Helper()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	if len(paths) > 0 {
		scantest.IndexMediaPaths(t, mediaDB, systemID, paths...)
	}
	dbPath := mediaDB.GetDBPath()
	cleanup()
	return dbPath
}

// Without a media.db every row takes the fabricated-path branch, so a handful
// of rows exercises generation, UUID assignment and both counters cheaply.
func TestRun_GeneratesUnresolvableHistory(t *testing.T) {
	t.Parallel()
	out := t.TempDir()

	require.NoError(t, run(out, 8, "", 0.7, 0.5, 30, 1))

	info, err := os.Stat(filepath.Join(out, "user.db"))
	require.NoError(t, err)
	assert.Positive(t, info.Size())
}

func TestRun_RefusesToOverwriteExistingDB(t *testing.T) {
	t.Parallel()
	out := t.TempDir()

	require.NoError(t, run(out, 1, "", 0, 0, 1, 1))

	err := run(out, 1, "", 0, 0, 1, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to overwrite")
}

func TestRun_RejectsOutOfRangeFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		wantErr    string
		rows       int
		resolvable float64
		noUUID     float64
		spanDays   int
	}{
		{name: "negative rows", rows: -1, resolvable: 0.5, noUUID: 0.5, spanDays: 1, wantErr: "-rows"},
		{name: "zero span", rows: 1, resolvable: 0.5, noUUID: 0.5, spanDays: 0, wantErr: "-span-days"},
		{name: "negative span", rows: 1, resolvable: 0.5, noUUID: 0.5, spanDays: -5, wantErr: "-span-days"},
		{name: "resolvable above one", rows: 1, resolvable: 1.5, noUUID: 0.5, spanDays: 1, wantErr: "-resolvable"},
		{name: "resolvable below zero", rows: 1, resolvable: -0.1, noUUID: 0.5, spanDays: 1, wantErr: "-resolvable"},
		{name: "nouuid above one", rows: 1, resolvable: 0.5, noUUID: 2, spanDays: 1, wantErr: "-nouuid"},
		{name: "nouuid below zero", rows: 1, resolvable: 0.5, noUUID: -1, spanDays: 1, wantErr: "-nouuid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := t.TempDir()

			err := run(out, tt.rows, "", tt.resolvable, tt.noUUID, tt.spanDays, 1)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)

			// Rejected before any output exists, so nothing is left behind.
			_, statErr := os.Stat(filepath.Join(out, "user.db"))
			assert.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

// Sampling a real index is the tool's whole purpose: rows must carry the
// indexed path verbatim, since a rewritten path resolves to nothing and the
// generated database silently stresses only the skip path.
func TestRun_SamplesIndexedMediaPaths(t *testing.T) {
	t.Parallel()
	gamePath := "/media/fat/games/SNES/Super Game (USA).sfc"
	mediaDBPath := indexedMediaDB(t, "SNES", gamePath)
	out := t.TempDir()

	require.NoError(t, run(out, 6, mediaDBPath, 1, 0, 30, 1))

	entries := readGeneratedHistory(t, out)
	require.Len(t, entries, 6)
	for _, entry := range entries {
		assert.Equal(t, gamePath, entry.MediaPath)
		assert.Equal(t, "SNES", entry.SystemID)
	}
}

// A media.db with nothing present is not the same as no -mediadb, but the
// generated rows must be: fully unresolvable, not silently zero-row.
func TestRun_EmptyMediaDBStillGeneratesUnresolvableRows(t *testing.T) {
	t.Parallel()
	mediaDBPath := indexedMediaDB(t, "SNES")
	out := t.TempDir()

	require.NoError(t, run(out, 4, mediaDBPath, 1, 0, 30, 1))

	entries := readGeneratedHistory(t, out)
	require.Len(t, entries, 4)
	for _, entry := range entries {
		assert.True(t, strings.HasPrefix(entry.MediaPath, missingMediaRoot+"/"),
			"expected a fabricated path, got %q", entry.MediaPath)
	}
}

// An unreadable -mediadb must fail before any user.db is written, rather than
// quietly producing an all-unresolvable database that looks like a valid run.
func TestRun_ReportsUnreadableMediaDB(t *testing.T) {
	t.Parallel()
	out := t.TempDir()

	err := run(out, 4, filepath.Join(t.TempDir(), "missing.db"), 1, 0, 30, 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "media db")
	_, statErr := os.Stat(filepath.Join(out, "user.db"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}
