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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewNamesIndex_CancellationPreservesCancelledStatus verifies that a cancellation
// observed inside the per-system loop (after the failure-tracking defer is registered)
// leaves the indexing status as Cancelled. The deferred handler marks Failed on a genuine
// error; this guards against it overwriting the Cancelled status set by the cancellation
// path. Cannot use t.Parallel() - setupCustomLauncherSystems mutates GlobalLauncherCache.
func TestNewNamesIndex_CancellationPreservesCancelledStatus(t *testing.T) {
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	systemFiles := map[string][]string{
		systemdefs.SystemNES:     {"a.bin", "b.bin"},
		systemdefs.SystemSNES:    {"c.bin", "d.bin"},
		systemdefs.SystemGenesis: {"e.bin", "f.bin"},
	}
	platform, cfg, systems := setupCustomLauncherSystems(t, systemFiles)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// SystemID is only set once indexing reaches the per-system loop, which runs after the
	// failure-tracking defer is registered. Cancelling there forces a post-defer cancellation.
	update := func(s IndexStatus) {
		if s.SystemID != "" {
			cancel()
		}
	}

	_, err := NewNamesIndex(ctx, platform, cfg, systems, db, update, nil)
	require.ErrorIs(t, err, context.Canceled)

	status, statusErr := db.MediaDB.GetIndexingStatus()
	require.NoError(t, statusErr)
	assert.Equal(t, mediadb.IndexingStatusCancelled, status,
		"cancellation must leave Cancelled status, not be overwritten to Failed by the deferred handler")
}
