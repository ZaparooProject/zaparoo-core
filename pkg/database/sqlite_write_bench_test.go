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

package database_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// BenchmarkSQLiteWritePattern prices the write patterns available to a bulk
// load on the storage under TMPDIR. On MiSTer the SD card is mounted
// sync,dirsync, so every write call is flushed before it returns and the
// number of calls, not the number of bytes, sets the cost. Each run loads six
// batches of 2,000 rows into a Media-shaped table with its indexes.
func BenchmarkSQLiteWritePattern(b *testing.B) {
	for _, pageSize := range []int{4096, 16384, 65536} {
		for _, checkpoint := range []string{"TRUNCATE", "RESTART"} {
			b.Run(fmt.Sprintf("page_%d/%s", pageSize, checkpoint), func(b *testing.B) {
				var commit, checkpointing time.Duration
				var dbBytes int64
				for b.Loop() {
					c, k, size := runWritePattern(b, pageSize, checkpoint)
					commit += c
					checkpointing += k
					dbBytes = size
				}
				b.ReportMetric(float64(commit.Milliseconds())/float64(b.N), "commit-ms/op")
				b.ReportMetric(float64(checkpointing.Milliseconds())/float64(b.N), "checkpoint-ms/op")
				b.ReportMetric(float64(dbBytes)/(1<<20), "db-MiB")
			})
		}
	}
}

func runWritePattern(b *testing.B, pageSize int, checkpoint string) (commit, checkpointing time.Duration, size int64) {
	b.Helper()
	const (
		batches      = 6
		rowsPerBatch = 2000
		walThreshold = 8 << 20
	)
	ctx := context.Background()
	b.StopTimer()
	dir := b.TempDir()
	path := filepath.Join(dir, "pattern.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		fmt.Sprintf("PRAGMA page_size=%d", pageSize),
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-32768",
		"PRAGMA wal_autocheckpoint=0",
		`CREATE TABLE Media(DBID INTEGER PRIMARY KEY, MediaTitleDBID INTEGER, SystemDBID INTEGER,
			Path TEXT, ParentDir TEXT, IsMissing INTEGER, SortName TEXT, UNIQUE(SystemDBID, Path))`,
		"CREATE INDEX media_title ON Media(MediaTitleDBID)",
		"CREATE INDEX media_parent ON Media(ParentDir, SystemDBID)",
		"CREATE INDEX media_path ON Media(Path)",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			b.Fatalf("%s: %v", stmt, err)
		}
	}
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // deterministic benchmark data
	b.StartTimer()

	id := 0
	for batch := range batches {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			b.Fatal(err)
		}
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO Media VALUES(?,?,?,?,?,?,?)")
		if err != nil {
			b.Fatal(err)
		}
		for i := range rowsPerBatch {
			id++
			name := fmt.Sprintf("Game %06d (USA) (Rev %d)", rng.IntN(1_000_000), i%3)
			parent := fmt.Sprintf("/media/fat/games/Sys%d/dir%d/", batch%4, i%20)
			_, err := stmt.ExecContext(ctx, id, rng.IntN(50_000), batch%4, parent+name+".bin", parent, 0, name)
			if err != nil {
				b.Fatal(err)
			}
		}
		//nolint:sqlclosecheck // closed before the commit that is being timed
		_ = stmt.Close()
		started := time.Now()
		if err := tx.Commit(); err != nil {
			b.Fatal(err)
		}
		commit += time.Since(started)

		info, statErr := os.Stat(path + "-wal")
		due := statErr == nil && info.Size() >= walThreshold
		if checkpoint == "RESTART" {
			// A restarted WAL keeps its size, so the file size says nothing
			// about how much is waiting; checkpoint on a fixed cadence.
			due = batch%3 == 2
		}
		if due {
			started = time.Now()
			if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint("+checkpoint+")"); err != nil {
				b.Fatal(err)
			}
			checkpointing += time.Since(started)
		}
	}
	started := time.Now()
	if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		b.Fatal(err)
	}
	checkpointing += time.Since(started)
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}
	return commit, checkpointing, size
}
