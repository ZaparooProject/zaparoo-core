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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// readsBlockedByWriterTimeout bounds how long a client read may wait on the
// writer. It is generous on purpose: the point is to separate "does not wait on
// the writer at all" from "waits for the whole commit", and an indexing commit
// on the #1279 device reaches 25 s.
const readsBlockedByWriterTimeout = 5 * time.Second

// clientRead names a MediaDB read that a client request can reach while an
// index is running.
type clientRead struct {
	run  func(*MediaDB) error
	name string
}

func clientReadsReachableDuringIndexing() []clientRead {
	return []clientRead{
		{name: "GetIndexingStatus", run: func(db *MediaDB) error {
			_, err := db.GetIndexingStatus()
			return err
		}},
		{name: "GetOptimizationStatus", run: func(db *MediaDB) error {
			_, err := db.GetOptimizationStatus()
			return err
		}},
		{name: "GetOptimizationStep", run: func(db *MediaDB) error {
			_, err := db.GetOptimizationStep()
			return err
		}},
		{name: "GetScrapingStatus", run: func(db *MediaDB) error {
			_, err := db.GetScrapingStatus()
			return err
		}},
		{name: "GetScrapingOperation", run: func(db *MediaDB) error {
			_, _, err := db.GetScrapingOperation()
			return err
		}},
		{name: "GetLastGenerated", run: func(db *MediaDB) error {
			_, err := db.GetLastGenerated()
			return err
		}},
		{name: "GetLastIndexedSystem", run: func(db *MediaDB) error {
			_, err := db.GetLastIndexedSystem()
			return err
		}},
		{name: "GetIndexingSystems", run: func(db *MediaDB) error {
			_, err := db.GetIndexingSystems()
			return err
		}},
		{name: "GetIndexingPlanSystems", run: func(db *MediaDB) error {
			_, err := db.GetIndexingPlanSystems()
			return err
		}},
		{name: "GetIndexResumeAttempts", run: func(db *MediaDB) error {
			_, err := db.GetIndexResumeAttempts()
			return err
		}},
		{name: "GetIndexResumeCheckpoint", run: func(db *MediaDB) error {
			_, err := db.GetIndexResumeCheckpoint()
			return err
		}},
		{name: "IndexGeneration", run: func(db *MediaDB) error {
			_, err := db.IndexGeneration()
			return err
		}},
		{name: "GetLaunchCommandForMedia", run: func(db *MediaDB) error {
			_, err := db.GetLaunchCommandForMedia(context.Background(), "NES", "/games/NES/x.nes")
			return err
		}},
		{name: "QuickCheck", run: func(db *MediaDB) error {
			_, err := db.QuickCheck()
			return err
		}},
		{name: "CheckForDuplicateMediaTitles", run: func(db *MediaDB) error {
			_, err := db.CheckForDuplicateMediaTitles()
			return err
		}},
		{name: "IntegrityReport", run: func(db *MediaDB) error {
			db.IntegrityReport()
			return nil
		}},
	}
}

// TestMediaDB_ClientReadsDoNotWaitOnWriterLock pins that a client-reachable
// read never queues behind the indexing writer.
//
// CommitTransactionWithOptions holds sqlMu exclusively for the entire commit,
// and Go's RWMutex is writer-preferring, so any read taking RLock waits for the
// whole thing. Measured on the #1279 device mid-index, a media.scrape.status
// call starting at 13:25:44 took 25,030 ms while the C64 batch commit starting
// the same second took 25,123 ms — with the API timeout at 30 s that left no
// headroom.
//
// Holding sqlMu directly rather than driving a real commit is deliberate: it
// reproduces the exact condition these reads must survive, without depending on
// how long a commit happens to take on the machine running the test.
func TestMediaDB_ClientReadsDoNotWaitOnWriterLock(t *testing.T) {
	t.Parallel()

	for _, read := range clientReadsReachableDuringIndexing() {
		t.Run(read.name, func(t *testing.T) {
			t.Parallel()

			mediaDB, cleanup := setupTempMediaDB(t)
			defer cleanup()

			// Stand in for a commit in flight.
			mediaDB.sqlMu.Lock()
			defer mediaDB.sqlMu.Unlock()

			done := make(chan error, 1)
			go func() { done <- read.run(mediaDB) }()

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(readsBlockedByWriterTimeout):
				t.Fatalf("%s blocked on the writer lock; a client read must not wait "+
					"for an indexing commit, which reaches 25s on device", read.name)
			}
		})
	}
}

// TestMediaDB_ClientReadsSucceedDuringOpenTransaction is the counterpart with a
// real writer: a batch transaction is open and holds the single SQLite writer,
// and the reads must still answer from the pool rather than joining it.
func TestMediaDB_ClientReadsSucceedDuringOpenTransaction(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, mediaDB.SetIndexingStatus(IndexingStatusRunning))
	require.NoError(t, mediaDB.BeginTransaction(true))
	t.Cleanup(func() {
		if mediaDB.inTransaction {
			_ = mediaDB.RollbackTransaction()
		}
	})

	for _, read := range clientReadsReachableDuringIndexing() {
		done := make(chan error, 1)
		go func() { done <- read.run(mediaDB) }()

		select {
		case err := <-done:
			require.NoError(t, err, "%s failed while an indexing transaction was open", read.name)
		case <-time.After(readsBlockedByWriterTimeout):
			t.Fatalf("%s did not complete while an indexing transaction was open", read.name)
		}
	}
}
