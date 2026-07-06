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
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/jonboulle/clockwork"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// TestFindSystemBySystemID_ConcurrentDuringIndexingTransaction reproduces a live
// bug: external readers (API handlers, zapscript launch, scrapers) call the
// Find* lookups on MediaDB concurrently with the indexer's own long-lived write
// transaction. Those lookups must never borrow the indexer's *sql.Tx via
// db.conn() — doing so intermittently fails with "transaction has already been
// committed or rolled back" once the indexer commits mid-read, and races on the
// unsynchronized db.tx field across goroutines.
func TestFindSystemBySystemID_ConcurrentDuringIndexingTransaction(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	sqlDB, err := sql.Open("sqlite3", dbPath+getSqliteConnParams())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, sqlDB.Close())
	}()

	mediaDB := &MediaDB{
		ctx:   ctx,
		clock: clockwork.NewRealClock(),
	}
	mediaDB.sql.Store(sqlDB)
	require.NoError(t, mediaDB.Allocate())

	system, err := mediaDB.InsertSystem(database.System{SystemID: "TestSystem", Name: "Test System"})
	require.NoError(t, err)

	const writerCycles = 30
	stop := make(chan struct{})
	writerErrs := make(chan error, writerCycles*3)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := range writerCycles {
			if beginErr := mediaDB.BeginTransaction(false); beginErr != nil {
				writerErrs <- beginErr
				return
			}
			_, insErr := mediaDB.InsertMediaTitle(&database.MediaTitle{
				SystemDBID: system.DBID,
				Name:       fmt.Sprintf("Game %d", i),
				Slug:       fmt.Sprintf("game-%d", i),
			})
			if insErr != nil {
				writerErrs <- insErr
				return
			}
			if commitErr := mediaDB.CommitTransaction(); commitErr != nil {
				writerErrs <- commitErr
				return
			}
		}
	}()

	var badErrors atomic.Int64
	const readers = 8
	wg.Add(readers)
	for range readers {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, findErr := mediaDB.FindSystemBySystemID("TestSystem")
				if findErr != nil && strings.Contains(findErr.Error(), "transaction has already been committed") {
					badErrors.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	close(writerErrs)
	for writerErr := range writerErrs {
		require.NoError(t, writerErr)
	}
	require.Equal(t, int64(0), badErrors.Load(),
		"FindSystemBySystemID must never borrow the indexer's in-flight transaction")
}
