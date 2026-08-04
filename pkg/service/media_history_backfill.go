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

package service

import (
	"context"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/rs/zerolog/log"
)

const (
	mediaHistoryIdentityBackfillBatchSize = 100
	// Batches trickle so the sweep's MediaDB reads never saturate its
	// 2-connection pool against foreground browse/launch queries.
	mediaHistoryBackfillBatchDelay = 500 * time.Millisecond
	// The startup check is delayed past the boot burst (index resume, cache
	// rebuilds) so the sweep never competes with a launcher's first requests.
	mediaHistoryBackfillStartupDelay = 2 * time.Minute
	// Poll fallback: the temporary-repair path starts optimization with a nil
	// status callback and emits no indexing notification on completion.
	mediaHistoryBackfillPollInterval = 15 * time.Minute
)

// watchMediaHistoryBackfill runs the media history identity backfill as part
// of the end of the media update process, never as unguarded boot work: once
// shortly after startup when the media database is already settled, and again
// whenever an indexing/optimization cycle completes (which is also when the
// disambiguation backfill inside the optimization pipeline finishes). It
// emits no client-visible notifications and cannot block boot or the API.
func watchMediaHistoryBackfill(
	ctx context.Context,
	b *broker.Broker,
	db *database.Database,
	pauser *syncutil.Pauser,
	requestPlaySync func(),
) {
	watchMediaHistoryBackfillAtInterval(
		ctx, b, db, pauser, requestPlaySync,
		mediaHistoryBackfillStartupDelay,
		mediaHistoryBackfillPollInterval,
		mediaHistoryBackfillBatchDelay,
	)
}

func watchMediaHistoryBackfillAtInterval(
	ctx context.Context,
	b *broker.Broker,
	db *database.Database,
	pauser *syncutil.Pauser,
	requestPlaySync func(),
	startupDelay time.Duration,
	pollInterval time.Duration,
	batchDelay time.Duration,
) {
	if db == nil || db.UserDB == nil || db.MediaDB == nil {
		return
	}

	// UUID assignment is UserDB-only, one idempotent transaction, and rows
	// without a UUID are ineligible for sync — ensure it before every sweep,
	// retrying until one clean success. It must NOT run at watcher start:
	// that lands in the boot burst, where it systematically loses a write
	// race against the idle scheduler's history-retention cleanup (observed
	// on MiSTer: "database is locked" after the full busy timeout), and a
	// single boot-time attempt would then leave UUID-less rows unsyncable
	// until the next restart. Deferring to the first trigger sidesteps the
	// boot window; retrying converts any remaining loss into a bounded delay.
	uuidBackfillDone := false
	ensureUUIDBackfill := func() {
		if uuidBackfillDone {
			return
		}
		backfilled, err := backfillMediaHistoryUUIDs(db.UserDB)
		if err != nil {
			return
		}
		uuidBackfillDone = true
		if backfilled > 0 && requestPlaySync != nil {
			requestPlaySync()
		}
	}

	notifChan, subID := b.Subscribe(32, models.NotificationMediaIndexing)
	defer b.Unsubscribe(subID)
	startupTimer := time.NewTimer(startupDelay)
	defer startupTimer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-startupTimer.C:
		case _, ok := <-notifChan:
			if !ok {
				return
			}
		case <-ticker.C:
		}
		ensureUUIDBackfill()
		runMediaHistoryIdentitySweep(ctx, db, pauser, requestPlaySync, batchDelay, mediaIdentityRetryDelays)
	}
}

// mediaHistoryIdentitySweepMarker is the DeviceState value recorded after a
// completed sweep. It changes when the identity policy version bumps or when
// a media index completes (LastGeneratedAt moves), which are exactly the two
// events that can let previously unresolvable rows resolve — so a matching
// marker means a sweep would find nothing new and the table walk is skipped.
func mediaHistoryIdentitySweepMarker(mediaDB database.MediaDBI) string {
	var generatedAt int64
	lastGenerated, err := mediaDB.GetLastGenerated()
	switch {
	case err != nil:
		log.Debug().Err(err).Msg("failed to read media last generated time for identity sweep marker")
	case !lastGenerated.IsZero():
		generatedAt = lastGenerated.Unix()
	}
	return strconv.Itoa(database.CurrentMediaIdentityPolicyVersion) + ":" +
		strconv.FormatInt(generatedAt, 10)
}

// mediaDBSettledForIdentitySweep reports whether the media database is idle
// enough to sweep: no indexing under way or owed, and no optimization (which
// includes the disambiguation backfill) under way or owed. Status read
// failures count as unsettled.
func mediaDBSettledForIdentitySweep(mediaDB database.MediaDBI) bool {
	indexingStatus, err := mediaDB.GetIndexingStatus()
	if err != nil {
		log.Debug().Err(err).Msg("failed to check indexing status before identity sweep")
		return false
	}
	if indexingStatus == mediadb.IndexingStatusRunning || indexingStatus == mediadb.IndexingStatusPending {
		return false
	}
	optimizationStatus, err := mediaDB.GetOptimizationStatus()
	if err != nil {
		log.Debug().Err(err).Msg("failed to check optimization status before identity sweep")
		return false
	}
	if optimizationStatus == mediadb.IndexingStatusRunning || optimizationStatus == mediadb.IndexingStatusPending {
		return false
	}
	return true
}

// runMediaHistoryIdentitySweep enriches history rows below the current
// identity policy version with the scanner's complete identity observation.
// Interruptible at any point: the sweep's completion marker is only written
// after a full pass, so an aborted sweep is simply retried on the next
// trigger. Unresolvable rows (media not in the index, identity that cannot
// be built) are skipped and only revisited once the marker goes stale.
func runMediaHistoryIdentitySweep(
	ctx context.Context,
	db *database.Database,
	pauser *syncutil.Pauser,
	requestPlaySync func(),
	batchDelay time.Duration,
	retryDelays []time.Duration,
) {
	marker := mediaHistoryIdentitySweepMarker(db.MediaDB)
	stored, found, err := db.UserDB.GetDeviceState(database.DeviceStateKeyMediaHistoryIdentitySweep)
	if err != nil {
		log.Warn().Err(err).Msg("failed to read media history identity sweep marker")
	} else if found && stored == marker {
		return
	}
	if !mediaDBSettledForIdentitySweep(db.MediaDB) {
		return
	}

	log.Info().Str("marker", marker).Msg("starting media history identity sweep")
	var afterDBID int64
	var updatedRows, skippedRows, batches int
	// Enriched rows already have SyncedAt cleared, so an abort after one or
	// more successful updates must still request a play sync on the way out —
	// otherwise those rows wait for the next unrelated sync trigger. The
	// deferred flush covers every early return; the per-batch flush keeps
	// uploads starting while later batches are still being swept.
	pendingSync := false
	flushPlaySync := func() {
		if pendingSync && requestPlaySync != nil {
			requestPlaySync()
		}
		pendingSync = false
	}
	defer flushPlaySync()
	for {
		if ctx.Err() != nil {
			return
		}
		// Re-check every batch: an index or optimization starting mid-sweep
		// aborts without the marker, and the completion notification retries.
		if !mediaDBSettledForIdentitySweep(db.MediaDB) {
			log.Info().Int64("cursor", afterDBID).
				Msg("media history identity sweep deferring to media update")
			return
		}
		if waitErr := pauser.Wait(ctx); waitErr != nil {
			return
		}

		batch, batchErr := db.UserDB.GetMediaHistoryIdentityBackfillBatch(
			afterDBID,
			database.CurrentMediaIdentityPolicyVersion,
			mediaHistoryIdentityBackfillBatchSize,
		)
		if batchErr != nil {
			log.Warn().Err(batchErr).Msg("failed to read media history identity backfill batch")
			return
		}
		if len(batch) == 0 {
			break
		}

		for i := range batch {
			afterDBID = batch[i].DBID
			if ctx.Err() != nil {
				return
			}
			identity, resolved, lookupErr := lookupMediaIdentityWithRetry(
				ctx,
				db.MediaDB,
				batch[i].SystemID,
				batch[i].MediaPath,
				mediaIdentityLookupTimeout,
				retryDelays,
				database.LookupMediaIdentity,
			)
			if lookupErr != nil {
				if ctx.Err() == nil {
					log.Warn().Err(lookupErr).Int64("dbid", batch[i].DBID).
						Msg("pausing media history identity sweep after transient lookup failure")
				}
				return
			}
			if !resolved {
				skippedRows++
				continue
			}
			updated, updateErr := db.UserDB.UpdateMediaHistoryIdentity(batch[i].DBID, &identity)
			if updateErr != nil {
				log.Warn().Err(updateErr).Int64("dbid", batch[i].DBID).
					Msg("failed to backfill media history identity")
				return
			}
			if updated {
				updatedRows++
				pendingSync = true
			}
		}
		flushPlaySync()
		// A large legacy history sweeps for the better part of an hour on
		// device-class hardware (deliberately trickled); periodic progress
		// keeps it distinguishable from a hang.
		batches++
		if batches%50 == 0 {
			log.Info().Int64("cursor", afterDBID).Int("updated", updatedRows).
				Int("unresolved", skippedRows).Msg("media history identity sweep progress")
		}
		if len(batch) < mediaHistoryIdentityBackfillBatchSize {
			break
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(batchDelay):
		}
	}

	if setErr := db.UserDB.SetDeviceState(
		database.DeviceStateKeyMediaHistoryIdentitySweep, marker,
	); setErr != nil {
		log.Warn().Err(setErr).Msg("failed to record media history identity sweep marker")
		return
	}
	log.Info().Int("updated", updatedRows).Int("unresolved", skippedRows).Str("marker", marker).
		Msg("media history identity sweep completed")
}
