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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// SentinelTagInfo returns the sentinel TagInfo for the given scraper ID.
// Scrape callbacks write this after a successful record write to mark it done.
func SentinelTagInfo(scraperID string) database.TagInfo {
	return database.TagInfo{
		Type: string(tags.ScraperType(scraperID)),
		Tag:  string(tags.TagScraperScraped),
	}
}

// RunScraper creates a ScrapeUpdate channel, calls fn (which must start its own
// goroutine and close the channel when done), and returns the read end.
// If fn returns an error synchronously, a terminal FatalErr update is emitted
// and the channel is closed.
func RunScraper(fn func(chan<- ScrapeUpdate) error) <-chan ScrapeUpdate {
	ch := make(chan ScrapeUpdate, 32)
	if err := fn(ch); err != nil {
		ch <- ScrapeUpdate{FatalErr: err, Done: true}
		close(ch)
	}
	return ch
}
