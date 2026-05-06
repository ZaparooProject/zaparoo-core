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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMediaDB_ConfigSetters_ConcurrentWithTransactions verifies that the
// DBConfig setters can be called concurrently with BeginTransaction /
// CommitTransaction / RollbackTransaction without racing on the db.tx
// pointer. Run with -race to catch unsynchronized reads of db.tx inside
// db.conn().
func TestMediaDB_ConfigSetters_ConcurrentWithTransactions(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	const iterations = 50
	var wg sync.WaitGroup

	// Goroutine 1: continually open and close transactions (writes db.tx).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			if err := mediaDB.BeginTransaction(false); err != nil {
				continue
			}
			_ = mediaDB.CommitTransaction()
		}
	}()

	// Goroutines 2-6: hammer each unlocked setter (reads db.tx via db.conn()).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			_ = mediaDB.SetOptimizationStatus("running")
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			_ = mediaDB.SetOptimizationStep("analyze")
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			_ = mediaDB.SetIndexingStatus("running")
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			_ = mediaDB.SetLastIndexedSystem("snes")
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			_ = mediaDB.SetIndexingSystems([]string{"snes", "genesis"})
		}
	}()

	wg.Wait()
}

// TestMediaDB_BumpIndexGeneration_ConcurrentNoLostUpdates verifies that
// concurrent BumpIndexGeneration calls produce N distinct values (no lost
// updates) when N goroutines each call it once. Without the sqlMu lock,
// the read-then-write inside sqlBumpIndexGeneration could race and drop
// increments.
func TestMediaDB_BumpIndexGeneration_ConcurrentNoLostUpdates(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	const goroutines = 20
	var wg sync.WaitGroup
	results := make([]int64, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gen, err := mediaDB.BumpIndexGeneration()
			if err != nil {
				t.Errorf("BumpIndexGeneration failed: %v", err)
				return
			}
			results[i] = gen
		}()
	}
	wg.Wait()

	// Every result must be distinct: collect into a set.
	seen := make(map[int64]struct{}, goroutines)
	var maxGen int64
	for _, r := range results {
		assert.NotZero(t, r, "BumpIndexGeneration returned zero")
		_, dup := seen[r]
		assert.False(t, dup, "duplicate generation value %d (lost update)", r)
		seen[r] = struct{}{}
		if r > maxGen {
			maxGen = r
		}
	}
	assert.Len(t, seen, goroutines, "expected %d distinct values, got %d", goroutines, len(seen))
	// Final stored value must equal the highest observed generation.
	finalGen, err := mediaDB.IndexGeneration()
	require.NoError(t, err)
	assert.Equal(t, maxGen, finalGen, "stored generation should match max returned")
}

// TestMediaDB_ConfigSetters_DuringActiveTransaction verifies that setters
// route through the active transaction (db.tx) when one is open and don't
// deadlock against it. Under SetMaxOpenConns(1), if a setter tried to use
// db.sql while a transaction held the only connection, it would block
// forever; the db.conn() routing prevents that.
func TestMediaDB_ConfigSetters_DuringActiveTransaction(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, mediaDB.BeginTransaction(false))

	// These all call db.conn() which should return db.tx.
	require.NoError(t, mediaDB.SetOptimizationStatus("running"))
	require.NoError(t, mediaDB.SetOptimizationStep("analyze"))
	require.NoError(t, mediaDB.SetIndexingStatus("running"))
	require.NoError(t, mediaDB.SetLastIndexedSystem("snes"))
	require.NoError(t, mediaDB.SetIndexingSystems([]string{"snes"}))
	gen, err := mediaDB.BumpIndexGeneration()
	require.NoError(t, err)
	assert.Positive(t, gen)

	require.NoError(t, mediaDB.CommitTransaction())

	// Values written inside the transaction should be visible after commit.
	status, err := mediaDB.GetOptimizationStatus()
	require.NoError(t, err)
	assert.Equal(t, "running", status)

	finalGen, err := mediaDB.IndexGeneration()
	require.NoError(t, err)
	assert.Equal(t, gen, finalGen)
}

// TestMediaDB_BumpIndexGeneration_StressMonotonic stresses BumpIndexGeneration
// from multiple goroutines and verifies the final stored value equals the
// total number of bumps (no lost updates under contention).
func TestMediaDB_BumpIndexGeneration_StressMonotonic(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	const (
		goroutines      = 8
		bumpsPerRoutine = 25
	)
	var wg sync.WaitGroup
	var failures atomic.Int32

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range bumpsPerRoutine {
				if _, err := mediaDB.BumpIndexGeneration(); err != nil {
					failures.Add(1)
					return
				}
			}
		}()
	}
	wg.Wait()

	require.Zero(t, failures.Load(), "BumpIndexGeneration returned errors under contention")

	finalGen, err := mediaDB.IndexGeneration()
	require.NoError(t, err)
	assert.Equal(t, int64(goroutines*bumpsPerRoutine), finalGen,
		"final generation should equal total bumps; lost updates indicate the read+write is not serialized")
}
