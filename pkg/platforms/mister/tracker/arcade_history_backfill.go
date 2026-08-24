//go:build linux

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

package tracker

import (
	"context"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/rs/zerolog/log"
)

const (
	// arcadeHistoryBackfillDeviceStateKey marks the one-time arcade history
	// backfill complete. Versioned so a future change to the resolution
	// policy can revisit rows a prior version left unresolved.
	arcadeHistoryBackfillDeviceStateKey = "mister_arcade_history_backfill_v1"
	// arcadeHistoryBackfillRetryInterval paces retries when a pass can't
	// fully resolve every candidate row yet (MediaDB still indexing, or a
	// transient write failure). Not urgent, no backoff needed - this isn't
	// hitting an external service and the cost of a no-op pass is negligible.
	arcadeHistoryBackfillRetryInterval = 5 * time.Minute
	arcadeHistoryBackfillPageSize      = 100
)

// RunArcadeHistoryBackfill resolves every legacy MediaHistory row recorded
// under a bare arcade set name (MediaPath = "pooyan", not a path - from a
// launch the tracker detected externally, before ResolveArcadeSetName
// existed) to its canonical .mra path and identity, then never runs again.
// Intended to run once as an idle-scheduler task for the life of the
// platform; blocks until fully done or ctx is cancelled.
func RunArcadeHistoryBackfill(ctx context.Context, db *database.Database, tr *Tracker) {
	runArcadeHistoryBackfillAtInterval(ctx, db, tr, arcadeHistoryBackfillRetryInterval)
}

func runArcadeHistoryBackfillAtInterval(
	ctx context.Context, db *database.Database, tr *Tracker, retryInterval time.Duration,
) {
	if db == nil || db.UserDB == nil || db.MediaDB == nil || tr == nil {
		return
	}

	for {
		if ctx.Err() != nil {
			return
		}
		if _, found, _ := db.UserDB.GetDeviceState(arcadeHistoryBackfillDeviceStateKey); found {
			return
		}

		if mediaDBReadyForArcadeBackfill(db.MediaDB) {
			log.Debug().Msg("arcade history backfill: starting pass")
			if runArcadeHistoryBackfillPass(ctx, db, tr) {
				err := db.UserDB.SetDeviceState(arcadeHistoryBackfillDeviceStateKey, "done")
				if err == nil {
					log.Info().Msg("arcade history backfill complete")
					return
				}
				log.Warn().Err(err).Msg("failed to record arcade history backfill completion")
				// Not fatal: the next retry will simply redo a pass that
				// finds nothing left to fix and try to mark done again.
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
		}
	}
}

// mediaDBReadyForArcadeBackfill reports whether MediaDB is idle enough to
// read from: no indexing or optimization under way. Status read failures
// count as unready, matching the general media history identity sweep's
// caution (pkg/service/media_history_backfill.go).
func mediaDBReadyForArcadeBackfill(mediaDB database.MediaDBI) bool {
	indexingStatus, err := mediaDB.GetIndexingStatus()
	if err != nil {
		return false
	}
	if indexingStatus == mediadb.IndexingStatusRunning || indexingStatus == mediadb.IndexingStatusPending {
		return false
	}
	optimizationStatus, err := mediaDB.GetOptimizationStatus()
	if err != nil {
		return false
	}
	return optimizationStatus != mediadb.IndexingStatusRunning && optimizationStatus != mediadb.IndexingStatusPending
}

// runArcadeHistoryBackfillPass walks every Arcade MediaHistory row once,
// resolving what it can. Returns true only when every candidate row seen
// this pass ended up resolved - the only condition under which the caller
// marks the task permanently done.
func runArcadeHistoryBackfillPass(ctx context.Context, db *database.Database, tr *Tracker) bool {
	var cursor int64
	clean := true
	for {
		if ctx.Err() != nil {
			return false
		}
		batch, err := db.UserDB.GetMediaHistory([]string{ArcadeSystem}, cursor, arcadeHistoryBackfillPageSize)
		if err != nil {
			log.Warn().Err(err).Msg("failed to read arcade media history for backfill")
			return false
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			entry := &batch[i]
			cursor = entry.DBID
			if entry.MediaIdentity != nil || filepath.IsAbs(entry.MediaPath) {
				continue // already resolved, or already a real path
			}
			if !resolveAndFixArcadeHistoryRow(ctx, db, tr, entry) {
				clean = false
			}
		}
		if len(batch) < arcadeHistoryBackfillPageSize {
			break
		}
	}
	return clean
}

// resolveAndFixArcadeHistoryRow resolves one row's set-name MediaPath to its
// canonical .mra path and identity, then persists both atomically. Returns
// false for any outcome that leaves the row unresolved - unconfirmed set
// name, no matching indexed entry, or a write failure - so the caller
// retries it on a later pass.
func resolveAndFixArcadeHistoryRow(
	ctx context.Context, db *database.Database, tr *Tracker, entry *database.MediaHistoryEntry,
) bool {
	rowCtx, cancel := context.WithTimeout(ctx, mediaLookupTimeout)
	defer cancel()

	path, ok := tr.ResolveExternalMediaPath(rowCtx, entry.SystemID, entry.MediaPath)
	if !ok {
		return false
	}
	identity, found, err := database.LookupMediaIdentity(rowCtx, db.MediaDB, entry.SystemID, path)
	if err != nil || !found {
		return false
	}
	updated, err := db.UserDB.UpdateMediaHistoryIdentityAndPath(entry.DBID, path, &identity)
	if err != nil {
		log.Warn().Err(err).Int64("dbid", entry.DBID).
			Msg("failed to backfill arcade history identity and path")
		return false
	}
	if !updated {
		log.Debug().Int64("dbid", entry.DBID).Msg("arcade history row already had current identity")
	}
	return true
}
