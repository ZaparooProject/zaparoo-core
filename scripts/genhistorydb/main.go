/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

// Command genhistorydb builds a user.db full of legacy MediaHistory rows
// (no identity snapshot, policy version 0) for stress-testing the media
// history identity backfill sweep on real hardware. Rows are written through
// the production userdb code path so the schema and row shape are exactly
// what a device that recorded history before the identity upgrade holds.
//
// Point -mediadb at a copy of the target device's media.db to make a
// fraction of rows resolvable against its real index (exercising identity
// construction, row updates, and re-sync marking); the rest use fabricated
// paths that can never resolve (exercising the skip path). Copy the result
// over the device's user.db (service stopped, original backed up) and watch
// the sweep.
//
// Usage:
//
//	go run ./scripts/genhistorydb -out ./tmp/stress -rows 50000 \
//	  [-mediadb ./tmp/media.db] [-resolvable 0.7] [-nouuid 0.1] [-span-days 365]
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// missingMediaRoot is the device-side games root fabricated paths hang off.
// Built with path.Join, never filepath.Join: these are target-device paths and
// must keep Linux separators even when generated on another OS.
const missingMediaRoot = "/media/fat/games"

// say prints progress to stdout; a dev tool ignores print errors.
func say(format string, a ...any) {
	_, _ = fmt.Printf(format+"\n", a...)
}

type mediaRef struct {
	systemID string
	path     string
	name     string
}

func main() {
	out := flag.String("out", "./tmp/stress", "output directory (user.db is written inside)")
	rows := flag.Int("rows", 50000, "number of history rows to generate")
	mediaDBPath := flag.String("mediadb", "", "optional media.db to sample real indexed paths from")
	resolvable := flag.Float64("resolvable", 0.7,
		"fraction of rows using real indexed paths (requires -mediadb; rest are unresolvable)")
	noUUID := flag.Float64("nouuid", 0.1,
		"fraction of rows without a session UUID, exercising the UUID backfill")
	spanDays := flag.Int("span-days", 365, "spread session start times over this many past days")
	seed := flag.Int64("seed", 1, "PRNG seed for reproducible output")
	flag.Parse()

	if err := run(*out, *rows, *mediaDBPath, *resolvable, *noUUID, *spanDays, *seed); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(
	out string, rows int, mediaDBPath string,
	resolvable, noUUID float64, spanDays int, seed int64,
) error {
	// Checked before anything is generated: a non-positive span panics in
	// rng.Intn, and out-of-range fractions silently produce a mix nobody asked
	// for after minutes of writing rows.
	switch {
	case rows < 0:
		return fmt.Errorf("-rows must not be negative, got %d", rows)
	case spanDays < 1:
		return fmt.Errorf("-span-days must be at least 1, got %d", spanDays)
	case resolvable < 0 || resolvable > 1:
		return fmt.Errorf("-resolvable must be a fraction between 0 and 1, got %g", resolvable)
	case noUUID < 0 || noUUID > 1:
		return fmt.Errorf("-nouuid must be a fraction between 0 and 1, got %g", noUUID)
	}

	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // Deterministic test data, not crypto.

	absOut, err := filepath.Abs(out)
	if err != nil {
		return fmt.Errorf("resolve output dir: %w", err)
	}
	if mkErr := os.MkdirAll(absOut, 0o750); mkErr != nil {
		return fmt.Errorf("create output dir: %w", mkErr)
	}
	dbFile := filepath.Join(absOut, "user.db")
	if _, statErr := os.Stat(dbFile); statErr == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite", dbFile)
	}

	var indexed []mediaRef
	if mediaDBPath != "" {
		indexed, err = loadIndexedMedia(mediaDBPath)
		if err != nil {
			return err
		}
		say("sampled %d indexed media entries from %s", len(indexed), mediaDBPath)
	}
	if len(indexed) == 0 {
		resolvable = 0
		say("no media.db provided: every row will be unresolvable (skip-path stress only)")
	}

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: absOut})

	db, err := userdb.OpenUserDB(context.Background(), pl)
	if err != nil {
		return fmt.Errorf("open user db: %w", err)
	}
	defer db.Close() //nolint:errcheck // Best-effort close after explicit close below.

	// Generation-only speedup; the copied file is a plain checkpointed
	// database, so the target device is unaffected by this pragma.
	if _, pragmaErr := db.UnsafeGetSQLDb().ExecContext(
		context.Background(), "PRAGMA synchronous=OFF",
	); pragmaErr != nil {
		return fmt.Errorf("set synchronous off: %w", pragmaErr)
	}

	systems := []string{"SNES", "NES", "Genesis", "PSX", "Arcade", "GBA", "MegaDrive"}
	started := time.Now()
	var withUUID, withoutUUID, resolvableRows, unresolvableRows int
	for i := range rows {
		var ref mediaRef
		if len(indexed) > 0 && rng.Float64() < resolvable {
			ref = indexed[rng.Intn(len(indexed))]
			resolvableRows++
		} else {
			system := systems[rng.Intn(len(systems))]
			name := fmt.Sprintf("Missing Game %06d", i)
			ref = mediaRef{
				systemID: system,
				path:     path.Join(missingMediaRoot, system, name+".bin"),
				name:     name,
			}
			unresolvableRows++
		}

		id := ""
		if rng.Float64() >= noUUID {
			id = uuid.New().String()
			withUUID++
		} else {
			withoutUUID++
		}

		startTime := time.Now().
			Add(-time.Duration(rng.Intn(spanDays*24*60)) * time.Minute)
		playSecs := 60 + rng.Intn(7200)
		entry := &database.MediaHistoryEntry{
			ID:            id,
			StartTime:     startTime,
			SystemID:      ref.systemID,
			SystemName:    ref.systemID,
			MediaPath:     ref.path,
			MediaName:     ref.name,
			LauncherID:    "mister",
			BootUUID:      "stress-boot",
			ClockReliable: true,
			ClockSource:   "system",
			CreatedAt:     startTime,
			UpdatedAt:     startTime.Add(time.Duration(playSecs) * time.Second),
		}
		dbid, addErr := db.AddMediaHistory(entry)
		if addErr != nil {
			return fmt.Errorf("add history row %d: %w", i, addErr)
		}
		endTime := startTime.Add(time.Duration(playSecs) * time.Second)
		if closeErr := db.CloseMediaHistory(dbid, endTime, playSecs); closeErr != nil {
			return fmt.Errorf("close history row %d: %w", i, closeErr)
		}
		if (i+1)%10000 == 0 {
			say("  %d/%d rows (%.0fs)", i+1, rows, time.Since(started).Seconds())
		}
	}

	if closeErr := db.Close(); closeErr != nil {
		return fmt.Errorf("close user db: %w", closeErr)
	}

	info, err := os.Stat(dbFile)
	if err != nil {
		return fmt.Errorf("stat generated db: %w", err)
	}
	say("wrote %s (%.1f MB) in %.0fs",
		dbFile, float64(info.Size())/1024/1024, time.Since(started).Seconds())
	say("rows: %d total, %d resolvable, %d unresolvable, %d with UUID, %d needing UUID backfill",
		rows, resolvableRows, unresolvableRows, withUUID, withoutUUID)
	return nil
}

// loadIndexedMedia reads (system, path, name) for every present media entry.
// Read-only access to a copy of the device's media.db; the query mirrors the
// join used by SearchMediaPathExact so sampled rows resolve identically.
func loadIndexedMedia(dbPath string) ([]mediaRef, error) {
	mdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open media db: %w", err)
	}
	defer mdb.Close() //nolint:errcheck // Read-only handle.

	rows, err := mdb.QueryContext(context.Background(), `
		SELECT Systems.SystemID, Media.Path, MediaTitles.Name
		FROM Systems
		INNER JOIN MediaTitles ON Systems.DBID = MediaTitles.SystemDBID
		INNER JOIN Media ON MediaTitles.DBID = Media.MediaTitleDBID
		WHERE Media.IsMissing = 0`)
	if err != nil {
		return nil, fmt.Errorf("query media db: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only cursor.

	var refs []mediaRef
	for rows.Next() {
		var ref mediaRef
		if scanErr := rows.Scan(&ref.systemID, &ref.path, &ref.name); scanErr != nil {
			return nil, fmt.Errorf("scan media row: %w", scanErr)
		}
		refs = append(refs, ref)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate media rows: %w", rowsErr)
	}
	return refs, nil
}
