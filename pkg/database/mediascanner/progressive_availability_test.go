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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// midScanProbe holds results of queries executed from the status callback
// while the scan was still running (a later system had not yet started
// staging), proving progressive availability on a fresh database.
type midScanProbe struct {
	searchErr     error
	browseErr     error
	hasMediaErr   error
	searchResults int
	browseDirs    int
	hasMedia      bool
	ran           bool
}

// TestNewNamesIndex_FreshIndexServesCommittedSystemsMidScan indexes two
// systems into an empty database and, the moment the second system starts,
// verifies the first system's media is already searchable, browsable, and
// reported as present — the core guarantee that clients need not wait for a
// full index to finish.
func TestNewNamesIndex_FreshIndexServesCommittedSystemsMidScan(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	systemFiles := map[string][]string{
		// Sorted system order is Gameboy < SNES, so Gameboy commits first.
		systemdefs.SystemGameboy: {"pocket_quest.bin"},
		systemdefs.SystemSNES:    {"super_quest.bin"},
	}
	platform, cfg, systems := setupCustomLauncherSystems(t, systemFiles)

	probe := &midScanProbe{}
	ctx := context.Background()

	update := func(status IndexStatus) {
		if status.SystemID != systemdefs.SystemSNES || probe.ran {
			return
		}
		// SNES is starting, so Gameboy's system-boundary commit and mid-scan
		// cache refresh have completed. All reads go through the read pool,
		// mirroring what API handlers do while indexing runs.
		probe.ran = true

		gameboy := systemdefs.System{ID: systemdefs.SystemGameboy}
		results, err := db.MediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
			Systems: []systemdefs.System{gameboy},
			Query:   "pocket quest",
			Limit:   10,
		})
		probe.searchResults = len(results)
		probe.searchErr = err

		dirs, err := db.MediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
			PathPrefix: "/",
		})
		probe.browseDirs = len(dirs)
		probe.browseErr = err

		probe.hasMedia, probe.hasMediaErr = db.MediaDB.HasAnyMedia()
	}

	filesIndexed, err := NewNamesIndex(ctx, platform, cfg, systems, db, update, nil)
	require.NoError(t, err)
	require.Equal(t, 2, filesIndexed)

	require.True(t, probe.ran, "status callback never observed the second system starting")

	require.NoError(t, probe.searchErr)
	assert.Equal(t, 1, probe.searchResults,
		"committed system's media must be searchable while the scan continues")

	require.NoError(t, probe.browseErr)
	assert.NotZero(t, probe.browseDirs,
		"committed system's directories must be browsable while the scan continues")

	require.NoError(t, probe.hasMediaErr)
	assert.True(t, probe.hasMedia, "HasAnyMedia must report data after the first system commit")
}
