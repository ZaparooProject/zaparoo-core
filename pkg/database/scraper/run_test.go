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

package scraper_test

import (
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func drainUpdates(ch <-chan scraper.ScrapeUpdate) []scraper.ScrapeUpdate {
	var updates []scraper.ScrapeUpdate
	for u := range ch {
		updates = append(updates, u)
	}
	return updates
}

func TestRunScraper_FnSuccess_ChannelOpen(t *testing.T) {
	t.Parallel()
	var received []scraper.ScrapeUpdate
	ch := scraper.RunScraper(func(ch chan<- scraper.ScrapeUpdate) error {
		go func() {
			defer close(ch)
			ch <- scraper.ScrapeUpdate{Done: true, Processed: 3}
		}()
		return nil
	})
	received = drainUpdates(ch)
	require.Len(t, received, 1)
	assert.True(t, received[0].Done)
	assert.Equal(t, 3, received[0].Processed)
}

func TestRunScraper_FnError_EmitsFatalErrAndCloses(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("setup failed")
	ch := scraper.RunScraper(func(_ chan<- scraper.ScrapeUpdate) error {
		return sentinel
	})
	updates := drainUpdates(ch)
	require.Len(t, updates, 1)
	assert.True(t, updates[0].Done)
	require.ErrorIs(t, updates[0].FatalErr, sentinel)
}

func TestSentinelTagInfo(t *testing.T) {
	t.Parallel()
	tag := scraper.SentinelTagInfo("myscr")
	assert.Equal(t, database.TagInfo{Type: "scraper.myscr", Tag: "scraped"}, tag)
}
