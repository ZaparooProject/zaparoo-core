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

package boltmigration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

// A tapto.db that never stored a mapping has no mappings bucket. It must be
// retired like any other migrated file, not fail the migration on every start.
func TestMaybeMigrate_NoMappingsBucketRetiresFile(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir})

	boltPath := filepath.Join(dataDir, "tapto.db")
	bdb, err := bolt.Open(boltPath, 0o600, &bolt.Options{})
	require.NoError(t, err)
	require.NoError(t, bdb.Close())

	newDB, err := userdb.OpenUserDB(t.Context(), pl)
	require.NoError(t, err)
	t.Cleanup(func() { _ = newDB.Close() })

	require.NoError(t, MaybeMigrate(pl, newDB))

	_, statErr := os.Stat(boltPath)
	require.ErrorIs(t, statErr, os.ErrNotExist, "old database should be renamed away")
	_, statErr = os.Stat(boltPath + ".migrated")
	require.NoError(t, statErr)

	mappings, err := newDB.GetAllMappings()
	require.NoError(t, err)
	assert.Empty(t, mappings)
}
