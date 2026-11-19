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

package librarysync

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
)

// The inventory is only read from a database nothing is rewriting: not an
// index, not background optimization, and not a scrape.
func TestMediaDBSettled_WaitsForIndexOptimizationAndScrape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                           string
		indexing, optimizing, scraping string
		want                           bool
	}{
		{
			name: "quiet", want: true,
			indexing: mediadb.IndexingStatusCompleted, optimizing: mediadb.IndexingStatusCompleted,
		},
		{name: "indexing", indexing: mediadb.IndexingStatusRunning},
		{name: "optimization queued", optimizing: mediadb.IndexingStatusPending},
		{name: "scrape running", scraping: mediadb.IndexingStatusRunning},
		{name: "scrape queued", scraping: mediadb.IndexingStatusPending},
		{name: "scrape cancelled", scraping: mediadb.IndexingStatusCancelled, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mediaDB := testhelpers.NewMockMediaDBI()
			mediaDB.On("GetIndexingStatus").Return(tt.indexing, nil)
			mediaDB.On("GetOptimizationStatus").Return(tt.optimizing, nil)
			mediaDB.On("GetScrapingStatus").Return(tt.scraping, nil)
			assert.Equal(t, tt.want, mediaDBSettled(mediaDB))
		})
	}
}
