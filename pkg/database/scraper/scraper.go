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

// Package scraper defines the metadata scraper interface and generic run loop.
//
// Each concrete scraper (gamelist.xml, ScreenScraper, TheGamesDB, etc.) implements
// [ScraperLoop] for its record type and delegates its Scrape method to [RunScraper].
//
// The sentinel tag pattern (scraper.<id>:scraped on the Media record) ensures that
// a crashed mid-write run is safely retried on the next invocation.
package scraper

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// Scraper is the public interface all metadata scrapers implement.
// Each scraper owns one source: a local file format, a REST API, etc.
type Scraper interface {
	// ID returns the stable scraper identifier used in sentinel tag names.
	// Must be globally unique. Examples: "gamelist.xml", "screenscraper".
	ID() string

	// Scrape starts the goroutine and returns a channel of progress updates.
	// The channel is closed when the goroutine exits (done or cancelled).
	Scrape(ctx context.Context, opts ScrapeOptions) (<-chan ScrapeUpdate, error)
}

// ScraperLoop is the internal interface that concrete scrapers implement to plug
// into [RunScraper]. T is the source-specific record type (e.g. GamelistRecord).
//
// Concrete scrapers also implement [Scraper]; their Scrape method stores the db
// and system list, then delegates to RunScraper with self as the ScraperLoop.
type ScraperLoop[T any] interface {
	// ID returns the same stable identifier as Scraper.ID.
	ID() string

	// LoadRecords returns all source records for the given system.
	// For file-based scrapers this reads local files; for REST scrapers this
	// queries the DB for titles that need enrichment.
	LoadRecords(ctx context.Context, system ScrapeSystem) ([]T, error)

	// Match attempts to bind a source record to a Zaparoo Media/MediaTitle row.
	// Returns nil when the record cannot be matched (not an error; loop skips it).
	Match(ctx context.Context, record T, system ScrapeSystem, db database.MediaDBI) (*MatchResult, error)

	// MapToDB converts a source record into the tag and property writes to apply.
	MapToDB(record T) MapResult
}

// MapResult holds the tag and property writes produced by a MapToDB call.
type MapResult struct {
	MediaTags  []database.TagInfo
	TitleTags  []database.TagInfo
	TitleProps []database.MediaProperty
	MediaProps []database.MediaProperty
}

// ScrapeOptions configures a scrape run.
type ScrapeOptions struct {
	// Systems limits scraping to these system IDs. Nil or empty means all systems.
	Systems []string

	// Force re-processes records that already have a sentinel tag.
	Force bool
}

// ScrapeUpdate is one progress event emitted on the channel returned by Scrape.
type ScrapeUpdate struct {
	Err       error
	FatalErr  error
	SystemID  string
	Processed int
	Total     int
	Matched   int
	Skipped   int
	Done      bool
}

// MatchResult is the output of a successful Match call.
type MatchResult struct {
	MediaDBID      int64
	MediaTitleDBID int64
}

// ScrapeSystem carries the DB identity and filesystem paths needed by the
// scrape loop and concrete scraper implementations.
type ScrapeSystem struct {
	ID       string
	ROMPaths []string
	DBID     int64
}
