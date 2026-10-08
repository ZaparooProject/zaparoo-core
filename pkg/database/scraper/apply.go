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

package scraper

import (
	"context"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// WriteBatchSize is how many targets one write transaction carries.
const WriteBatchSize = 100

// Wait returns once the run may continue: straight away when nothing holds it,
// after the pauser's pacing or pause otherwise. It returns the context's error
// when the run was cancelled.
func Wait(ctx context.Context, opts ScrapeOptions, scraperID string) error {
	if err := ctx.Err(); err != nil {
		return err //nolint:wrapcheck // callers match context errors directly
	}
	if opts.Pauser != nil {
		if err := opts.Pauser.Wait(ctx); err != nil {
			return fmt.Errorf("%s: wait while paused: %w", scraperID, err)
		}
	}
	return nil
}

// ApplyTargets writes targets in batches of WriteBatchSize, calling onBatch
// with the half-open index range of each batch once it has committed. The run
// yields to the pauser before every batch.
//
// A write failure is fatal: work that committed only part of a batch must not
// be reported as complete, or the sentinel would keep the remaining rows from
// ever being filled.
func ApplyTargets(
	ctx context.Context,
	db database.MediaDBI,
	opts ScrapeOptions,
	scraperID string,
	targets []database.ScrapeWriteTarget,
	onBatch func(from, to int),
) error {
	batcher, canBatch := db.(database.ScrapeResultBatchApplier)
	var waited, wrote time.Duration
	defer func() {
		if len(targets) == 0 {
			return
		}
		// Waiting is the pauser's pacing, which is deliberate; writing is the
		// part a scraper or the database can make cheaper.
		log.Debug().Str("scraper", scraperID).Int("targets", len(targets)).
			Dur("waited", waited).Dur("wrote", wrote).Msg("scraper: applied write targets")
	}()
	for start := 0; start < len(targets); start += WriteBatchSize {
		waitStart := time.Now()
		if err := Wait(ctx, opts, scraperID); err != nil {
			return err
		}
		waited += time.Since(waitStart)
		end := min(start+WriteBatchSize, len(targets))
		writeStart := time.Now()
		err := applyBatch(ctx, db, batcher, canBatch, scraperID, targets[start:end])
		wrote += time.Since(writeStart)
		if err != nil {
			return err
		}
		if onBatch != nil {
			onBatch(start, end)
		}
	}
	return nil
}

func applyBatch(
	ctx context.Context,
	db database.MediaDBI,
	batcher database.ScrapeResultBatchApplier,
	canBatch bool,
	scraperID string,
	batch []database.ScrapeWriteTarget,
) error {
	if canBatch {
		batchErr := batcher.ApplyScrapeResults(ctx, batch)
		if batchErr == nil {
			return nil
		}
		log.Warn().Err(batchErr).Int("targets", len(batch)).
			Msgf("%s: batch write failed, falling back to per-record writes", scraperID)
	}
	for _, target := range batch {
		if err := db.ApplyScrapeResult(ctx, target.MediaDBID, target.MediaTitleDBID, target.Write); err != nil {
			return fmt.Errorf("%s: write media %d: %w", scraperID, target.MediaDBID, err)
		}
	}
	return nil
}
