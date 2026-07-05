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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

func TestNewNamesIndex_ResumeUsesPersistedPlanWhenCurrentRunnableSetShrinks(t *testing.T) {
	t.Parallel()

	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", cfg).Return([]string{})

	planIDs := []string{systemdefs.SystemGenesis, systemdefs.SystemNES, systemdefs.SystemSNES}
	require.NoError(t, db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusRunning))
	require.NoError(t, db.MediaDB.SetLastIndexedSystem(systemdefs.SystemGenesis))
	require.NoError(t, db.MediaDB.SetIndexingSystems(planIDs))
	planStore, ok := db.MediaDB.(interface{ SetIndexingPlanSystems([]string) error })
	require.True(t, ok)
	require.NoError(t, planStore.SetIndexingPlanSystems(planIDs))

	systems, missing := systemDefsFromIDs(planIDs)
	require.Empty(t, missing)

	seen := make(map[string]bool)
	var total int
	_, err = NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(status IndexStatus) {
		if status.SystemID != "" {
			seen[status.SystemID] = true
			total = status.Total
		}
	}, nil)
	require.NoError(t, err)

	require.Equal(t, len(planIDs)+1, total)
	for _, systemID := range planIDs {
		require.Truef(t, seen[systemID], "expected resume plan system %s to be processed", systemID)
	}
}
