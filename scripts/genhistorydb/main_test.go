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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
