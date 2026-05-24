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

// Package scraper defines the metadata scraper types and generic run loop.
//
// Concrete scrapers (gamelist.xml, ScreenScraper, TheGamesDB, etc.) call
// [RunScraper] from their [platforms.Scraper].Scrape callback, passing their
// record-specific load/match/map functions directly.
//
// The sentinel tag pattern (scraper.<id>:scraped on the Media record) ensures that
// a crashed mid-write run is safely retried on the next invocation.
package scraper

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// MapResult holds the tag and property writes produced by a MapToDB call.
type MapResult struct {
	MediaTags  []database.TagInfo
	TitleTags  []database.TagInfo
	TitleProps []database.MediaProperty
	MediaProps []database.MediaProperty
}

// ScrapeOptions configures a scrape run.
type ScrapeOptions struct {
	// Pauser pauses scrape work while another foreground activity needs the system.
	Pauser *syncutil.Pauser

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

// MatchResult is the output of a successful Match call. Both IDs must be
// positive database IDs; invalid IDs are treated as an implementation error and
// skipped by the generic run loop before any writes are attempted.
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
