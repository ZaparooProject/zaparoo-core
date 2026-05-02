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
	"errors"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

const scrapeProgressMinInterval = 250 * time.Millisecond

// sentinelTagInfo returns the canonical TagInfo representation of the sentinel
// for the given scraper ID, used for write-side operations (e.g. db.UpsertMediaTags).
func sentinelTagInfo(scraperID string) database.TagInfo {
	return database.TagInfo{
		Type: string(tags.ScraperType(scraperID)),
		Tag:  string(tags.TagScraperScraped),
	}
}

// RunScraper runs the generic scrape loop for scraper s across the given systems.
// It emits [ScrapeUpdate] events on the returned channel, which is closed when
// the loop finishes or the context is cancelled. The channel always receives a
// final update with Done=true (or FatalErr set) before it is closed.
//
// systems must be pre-resolved by the caller (DBID, ID, ROMPaths all populated).
func RunScraper[T any](
	ctx context.Context,
	opts ScrapeOptions,
	systems []ScrapeSystem,
	db database.MediaDBI,
	s ScraperLoop[T],
) <-chan ScrapeUpdate {
	ch := make(chan ScrapeUpdate, 32)

	go func() {
		defer close(ch)

		var totalProcessed, totalMatched, totalSkipped int
		waitForResume := func(systemID string, processed, matched, skipped int) bool {
			if waitErr := opts.Pauser.Wait(ctx); waitErr != nil {
				ch <- ScrapeUpdate{
					SystemID:  systemID,
					Processed: totalProcessed + processed,
					Matched:   totalMatched + matched,
					Skipped:   totalSkipped + skipped,
					Done:      true,
				}
				return false
			}
			return true
		}

		for _, system := range systems {
			if !waitForResume(system.ID, 0, 0, 0) {
				return
			}

			// Emit start-of-system update with unknown total.
			select {
			case <-ctx.Done():
				ch <- ScrapeUpdate{Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped}
				return
			case ch <- ScrapeUpdate{SystemID: system.ID, Total: 0}:
			}

			// Step 1: load records for this system from the source.
			records, err := s.LoadRecords(ctx, system)
			if err != nil {
				// If LoadRecords failed because the context was cancelled, treat it
				// as a clean cancellation (no FatalErr) rather than a fatal error.
				if errors.Is(err, ctx.Err()) {
					ch <- ScrapeUpdate{
						Done:      true,
						Processed: totalProcessed,
						Matched:   totalMatched,
						Skipped:   totalSkipped,
					}
					return
				}
				ch <- ScrapeUpdate{
					SystemID:  system.ID,
					FatalErr:  err,
					Done:      true,
					Processed: totalProcessed,
					Matched:   totalMatched,
					Skipped:   totalSkipped,
				}
				return
			}

			// Emit updated total.
			select {
			case <-ctx.Done():
				ch <- ScrapeUpdate{Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped}
				return
			case ch <- ScrapeUpdate{SystemID: system.ID, Total: len(records)}:
			}

			var processed, matched, skipped int
			totalRecords := len(records)
			if !waitForResume(system.ID, processed, matched, skipped) {
				return
			}
			lastProgress := time.Now()
			emitProgress := func(update ScrapeUpdate, force bool) bool {
				if !force && time.Since(lastProgress) < scrapeProgressMinInterval {
					return true
				}
				update.SystemID = system.ID
				update.Total = totalRecords
				select {
				case <-ctx.Done():
					ch <- ScrapeUpdate{
						SystemID:  system.ID,
						Processed: totalProcessed + processed,
						Matched:   totalMatched + matched,
						Skipped:   totalSkipped + skipped,
						Done:      true,
					}
					return false
				case ch <- update:
					lastProgress = time.Now()
					return true
				}
			}

			scrapedMediaIDs := map[int64]struct{}{}
			if !opts.Force {
				var err error
				scrapedMediaIDs, err = db.GetScrapedMediaIDs(ctx, s.ID(), system.DBID)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						ch <- ScrapeUpdate{
							SystemID:  system.ID,
							Total:     totalRecords,
							Done:      true,
							Processed: totalProcessed,
							Matched:   totalMatched,
							Skipped:   totalSkipped,
						}
						return
					}
					ch <- ScrapeUpdate{
						SystemID:  system.ID,
						Total:     totalRecords,
						FatalErr:  err,
						Done:      true,
						Processed: totalProcessed,
						Matched:   totalMatched,
						Skipped:   totalSkipped,
					}
					return
				}
			}

			for _, record := range records {
				if !waitForResume(system.ID, processed, matched, skipped) {
					return
				}

				// Respect cancellation on every record.
				select {
				case <-ctx.Done():
					ch <- ScrapeUpdate{
						SystemID:  system.ID,
						Processed: totalProcessed + processed,
						Matched:   totalMatched + matched,
						Skipped:   totalSkipped + skipped,
						Done:      true,
					}
					return
				default:
				}

				// Step 3: match source record to a Zaparoo Media/MediaTitle.
				match, matchErr := s.Match(ctx, record, system, db)
				processed++
				if matchErr != nil {
					log.Warn().Err(matchErr).Str("system", system.ID).Msg("scraper: non-fatal match error")
					skipped++
					if !emitProgress(ScrapeUpdate{
						Processed: processed,
						Matched:   matched,
						Skipped:   skipped,
						Err:       matchErr,
					}, true) {
						return
					}
					continue
				}
				if match == nil {
					skipped++
					if !emitProgress(ScrapeUpdate{
						Processed: processed,
						Matched:   matched,
						Skipped:   skipped,
					}, false) {
						return
					}
					continue
				}
				if match.MediaDBID <= 0 || match.MediaTitleDBID <= 0 {
					log.Warn().
						Int64("mediaDBID", match.MediaDBID).
						Int64("mediaTitleDBID", match.MediaTitleDBID).
						Str("system", system.ID).
						Msg("scraper: Match returned non-positive IDs, skipping record")
					skipped++
					if !emitProgress(ScrapeUpdate{
						Processed: processed,
						Matched:   matched,
						Skipped:   skipped,
					}, true) {
						return
					}
					continue
				}

				if !opts.Force {
					if _, exists := scrapedMediaIDs[match.MediaDBID]; exists {
						skipped++
						if !emitProgress(ScrapeUpdate{
							Processed: processed,
							Matched:   matched,
							Skipped:   skipped,
						}, false) {
							return
						}
						continue
					}
				}

				// Step 4: map source record to tag/property writes.
				mapped := s.MapToDB(record)
				if !waitForResume(system.ID, processed, matched, skipped) {
					return
				}

				writeErr := db.ApplyScrapeResult(ctx, match.MediaDBID, match.MediaTitleDBID, &database.ScrapeWrite{
					Sentinel:   sentinelTagInfo(s.ID()),
					MediaTags:  mapped.MediaTags,
					TitleTags:  mapped.TitleTags,
					TitleProps: mapped.TitleProps,
					MediaProps: mapped.MediaProps,
				})
				if writeErr != nil {
					log.Warn().Err(writeErr).
						Int64("mediaDBID", match.MediaDBID).
						Int64("mediaTitleDBID", match.MediaTitleDBID).
						Str("system", system.ID).
						Msg("scraper: write failed")
					skipped++
					if !emitProgress(ScrapeUpdate{
						Processed: processed,
						Matched:   matched,
						Skipped:   skipped,
						Err:       writeErr,
					}, true) {
						return
					}
					continue
				}

				matched++
				if !opts.Force {
					scrapedMediaIDs[match.MediaDBID] = struct{}{}
				}
				if !emitProgress(ScrapeUpdate{
					Processed: processed,
					Matched:   matched,
					Skipped:   skipped,
				}, false) {
					return
				}
			}
			if !emitProgress(ScrapeUpdate{Processed: processed, Matched: matched, Skipped: skipped}, true) {
				return
			}

			totalProcessed += processed
			totalMatched += matched
			totalSkipped += skipped
		}

		ch <- ScrapeUpdate{Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped}
	}()

	return ch
}
