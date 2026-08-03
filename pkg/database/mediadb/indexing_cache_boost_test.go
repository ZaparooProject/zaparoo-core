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
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SQLite temp_store values as reported by `PRAGMA temp_store`.
const (
	tempStoreFile   = 1
	tempStoreMemory = 2
)

// TestBeginTransactionAppliesIndexingCacheBoost pins that the per-connection
// cache pragmas reach the connection the transaction actually pins. The pool
// hands PRAGMA execs to whichever connection is free, so SetIndexingCacheSize
// alone cannot guarantee the bulk writer runs with the boosted settings.
func TestBeginTransactionAppliesIndexingCacheBoost(t *testing.T) {
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "zaparoo-cache-boost-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: tempDir})
	mediaDB, err := OpenMediaDB(context.Background(), mockPlatform)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mediaDB.Close() })

	ctx := context.Background()
	readTxPragmas := func() (cacheSize, tempStore int) {
		require.NoError(t, mediaDB.tx.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize))
		require.NoError(t, mediaDB.tx.QueryRowContext(ctx, "PRAGMA temp_store").Scan(&tempStore))
		return cacheSize, tempStore
	}

	mediaDB.SetIndexingCacheSize(true)
	require.NoError(t, mediaDB.BeginTransaction(false))
	cacheSize, tempStore := readTxPragmas()
	assert.Equal(t, -32768, cacheSize, "indexing transaction runs with the boosted page cache")
	assert.Equal(t, tempStoreMemory, tempStore)
	require.NoError(t, mediaDB.RollbackTransaction())

	mediaDB.SetIndexingCacheSize(false)
	require.NoError(t, mediaDB.BeginTransaction(false))
	cacheSize, tempStore = readTxPragmas()
	assert.Equal(t, -8192, cacheSize, "steady-state transactions return to the default cache")
	assert.Equal(t, tempStoreFile, tempStore)
	require.NoError(t, mediaDB.RollbackTransaction())
}
