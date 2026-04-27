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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// sentinelTag returns the sentinel tag value for the given scraper ID.
// The sentinel is written to the Media record after a successful scrape so that
// subsequent runs can skip already-processed records.
func sentinelTag(scraperID string) string {
	return "scraper." + scraperID + ":scraped"
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

		sentinel := sentinelTag(s.ID())

		var totalProcessed, totalMatched, totalSkipped int

		for _, system := range systems {
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
				ch <- ScrapeUpdate{SystemID: system.ID, FatalErr: err, Done: true}
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

			for _, record := range records {
				// Respect cancellation on every record.
				select {
				case <-ctx.Done():
					ch <- ScrapeUpdate{
						SystemID:  system.ID,
						Processed: processed,
						Matched:   matched,
						Skipped:   skipped,
						Done:      true,
					}
					return
				default:
				}

				// Step 3: match source record to a Zaparoo Media/MediaTitle.
				match, matchErr := s.Match(ctx, record, system, db)
				if matchErr != nil {
					log.Warn().Err(matchErr).Str("system", system.ID).Msg("scraper: non-fatal match error")
					skipped++
					ch <- ScrapeUpdate{
						SystemID:  system.ID,
						Processed: processed,
						Matched:   matched,
						Skipped:   skipped,
						Err:       matchErr,
					}
					continue
				}
				if match == nil {
					skipped++
					continue
				}

				// Step 2: skip if already scraped (checked after match so we have the MediaDBID).
				if !opts.Force {
					has, sentinelErr := db.MediaHasTag(ctx, match.MediaDBID, sentinel)
					if sentinelErr != nil {
						log.Warn().Err(sentinelErr).
							Int64("mediaDBID", match.MediaDBID).
							Msg("scraper: sentinel check error")
						skipped++
						ch <- ScrapeUpdate{
							SystemID:  system.ID,
							Processed: processed,
							Matched:   matched,
							Skipped:   skipped,
							Err:       sentinelErr,
						}
						continue
					}
					if has {
						skipped++
						continue
					}
				}

				// Step 4: map source record to tag/property writes.
				mapped := s.MapToDB(record)

				// Step 5: write tags.
				if len(mapped.MediaTags) > 0 {
					if err := db.UpsertMediaTags(ctx, match.MediaDBID, mapped.MediaTags); err != nil {
						log.Warn().Err(err).Int64("mediaDBID", match.MediaDBID).
							Msg("scraper: failed to upsert media tags")
						skipped++
						ch <- ScrapeUpdate{
							SystemID:  system.ID,
							Processed: processed,
							Matched:   matched,
							Skipped:   skipped,
							Err:       err,
						}
						continue
					}
				}
				if len(mapped.TitleTags) > 0 {
					if err := db.UpsertMediaTitleTags(ctx, match.MediaTitleDBID, mapped.TitleTags); err != nil {
						log.Warn().Err(err).Int64("mediaTitleDBID", match.MediaTitleDBID).
							Msg("scraper: failed to upsert title tags")
						skipped++
						ch <- ScrapeUpdate{
							SystemID:  system.ID,
							Processed: processed,
							Matched:   matched,
							Skipped:   skipped,
							Err:       err,
						}
						continue
					}
				}

				// Step 6: write properties.
				if len(mapped.TitleProps) > 0 {
					if err := db.UpsertMediaTitleProperties(ctx, match.MediaTitleDBID, mapped.TitleProps); err != nil {
						log.Warn().Err(err).Int64("mediaTitleDBID", match.MediaTitleDBID).
							Msg("scraper: failed to upsert title properties")
						skipped++
						ch <- ScrapeUpdate{
							SystemID:  system.ID,
							Processed: processed,
							Matched:   matched,
							Skipped:   skipped,
							Err:       err,
						}
						continue
					}
				}
				if len(mapped.MediaProps) > 0 {
					if err := db.UpsertMediaProperties(ctx, match.MediaDBID, mapped.MediaProps); err != nil {
						log.Warn().Err(err).Int64("mediaDBID", match.MediaDBID).
							Msg("scraper: failed to upsert media properties")
						skipped++
						ch <- ScrapeUpdate{
							SystemID:  system.ID,
							Processed: processed,
							Matched:   matched,
							Skipped:   skipped,
							Err:       err,
						}
						continue
					}
				}

				// Step 7: write sentinel last — absent sentinel means safe to retry.
				sentinelTagInfo := database.TagInfo{
					Type: "scraper." + s.ID(),
					Tag:  "scraped",
				}
				if err := db.UpsertMediaTags(ctx, match.MediaDBID, []database.TagInfo{sentinelTagInfo}); err != nil {
					log.Warn().Err(err).Int64("mediaDBID", match.MediaDBID).Msg("scraper: failed to write sentinel tag")
					skipped++
					ch <- ScrapeUpdate{
						SystemID:  system.ID,
						Processed: processed,
						Matched:   matched,
						Skipped:   skipped,
						Err:       err,
					}
					continue
				}

				processed++
				matched++
				ch <- ScrapeUpdate{
					SystemID:  system.ID,
					Processed: processed,
					Matched:   matched,
					Skipped:   skipped,
				}
			}

			totalProcessed += processed
			totalMatched += matched
			totalSkipped += skipped
		}

		ch <- ScrapeUpdate{Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped}
	}()

	return ch
}
