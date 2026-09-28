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

package mediascanner

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A system auto-resume skipped is left out of the run, which still completes
// and indexes everything else. Completion clears the skip list and the current
// system, so the next index covers the skipped system again. Cannot use
// t.Parallel() - setupCustomLauncherSystems mutates GlobalLauncherCache.
func TestNewNamesIndex_LeavesOutSkippedSystems(t *testing.T) {
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()
	platform, cfg, systems := setupCustomLauncherSystems(t, map[string][]string{
		systemdefs.SystemNES:     {"a.bin", "b.bin"},
		systemdefs.SystemSNES:    {"c.bin", "d.bin"},
		systemdefs.SystemGenesis: {"e.bin"},
	})
	require.NoError(t, db.MediaDB.SetIndexingSkippedSystems([]string{systemdefs.SystemSNES}))

	_, err := NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {}, nil)
	require.NoError(t, err)

	for id, want := range map[string]int{
		systemdefs.SystemNES: 2, systemdefs.SystemSNES: 0, systemdefs.SystemGenesis: 1,
	} {
		media, mediaErr := db.MediaDB.GetMediaBySystemID(id)
		require.NoError(t, mediaErr)
		assert.Len(t, media, want, id)
	}
	skipped, err := db.MediaDB.GetIndexingSkippedSystems()
	require.NoError(t, err)
	assert.Empty(t, skipped)
	current, err := db.MediaDB.GetIndexingCurrentSystem()
	require.NoError(t, err)
	assert.Empty(t, current)
}

// A run stopped partway records the system it was working in, which is what
// auto-resume skips when the run keeps stopping there.
func TestNewNamesIndex_RecordsSystemInProgress(t *testing.T) {
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()
	platform, cfg, systems := setupCustomLauncherSystems(t, map[string][]string{
		systemdefs.SystemNES:     {"a.bin"},
		systemdefs.SystemSNES:    {"c.bin"},
		systemdefs.SystemGenesis: {"e.bin"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	update := func(s IndexStatus) {
		if s.SystemID == systemdefs.SystemNES {
			cancel()
		}
	}
	_, err := NewNamesIndex(ctx, platform, cfg, systems, db, update, nil)
	require.ErrorIs(t, err, context.Canceled)

	current, err := db.MediaDB.GetIndexingCurrentSystem()
	require.NoError(t, err)
	assert.Equal(t, systemdefs.SystemNES, current)
}

// Issue #1572: a system's folder scan reports how far it has read, so clients
// can show a long scan moving, and the count clears once the scan is done.
func TestNewNamesIndex_ReportsFolderScanProgress(t *testing.T) {
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()
	platform, cfg, systems := setupCustomLauncherSystems(t, map[string][]string{
		systemdefs.SystemNES: {"a.bin", "b.bin", "c.bin"},
	})

	var statuses []IndexStatus
	_, err := NewNamesIndex(context.Background(), platform, cfg, systems, db,
		func(s IndexStatus) { statuses = append(statuses, s) }, nil)
	require.NoError(t, err)

	scanned := -1
	for i, s := range statuses {
		if s.SystemID != systemdefs.SystemNES || s.WalkEntries == 0 {
			continue
		}
		scanned = s.WalkEntries
		assert.NotEmpty(t, s.WalkPath)
		require.Less(t, i+1, len(statuses))
		assert.Zero(t, statuses[i+1].WalkEntries, "the scan fields clear once the scan is done")
		assert.Empty(t, statuses[i+1].WalkPath)
		break
	}
	assert.GreaterOrEqual(t, scanned, 3, "the three files are counted")
}
