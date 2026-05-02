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

package tui

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
)

func TestNewProgressBar(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	assert.NotNil(t, pb)
	assert.NotNil(t, pb.Box)
	assert.InDelta(t, 0.0, pb.progress, 0.001)
}

func TestProgressBar_SetProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{
			name:     "zero progress",
			input:    0.0,
			expected: 0.0,
		},
		{
			name:     "half progress",
			input:    0.5,
			expected: 0.5,
		},
		{
			name:     "full progress",
			input:    1.0,
			expected: 1.0,
		},
		{
			name:     "negative clamped to zero",
			input:    -0.5,
			expected: 0.0,
		},
		{
			name:     "over one clamped to one",
			input:    1.5,
			expected: 1.0,
		},
		{
			name:     "quarter progress",
			input:    0.25,
			expected: 0.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pb := NewProgressBar()
			result := pb.SetProgress(tt.input)

			assert.InDelta(t, tt.expected, pb.GetProgress(), 0.001)
			assert.Equal(t, pb, result, "SetProgress should return self for chaining")
		})
	}
}

func TestProgressBar_GetProgress(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	// Initial progress
	assert.InDelta(t, 0.0, pb.GetProgress(), 0.001)

	// After setting
	pb.SetProgress(0.75)
	assert.InDelta(t, 0.75, pb.GetProgress(), 0.001)
}

func TestProgressBar_Chaining(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	// Verify chaining works
	result := pb.SetProgress(0.5)
	assert.Equal(t, pb, result)

	// Multiple chains
	pb.SetProgress(0.1).SetProgress(0.2).SetProgress(0.3)
	assert.InDelta(t, 0.3, pb.GetProgress(), 0.001)
}

func TestFormatDBMenuLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		db       models.IndexingStatusResponse
		expected string
	}{
		{
			name: "no database exists",
			db: models.IndexingStatusResponse{
				Exists: false,
			},
			expected: "Update index: not indexed",
		},
		{
			name: "database is indexing",
			db: models.IndexingStatusResponse{
				Exists:   true,
				Indexing: true,
			},
			expected: "Update index: in progress",
		},
		{
			name: "database is paused",
			db: models.IndexingStatusResponse{
				Exists:   true,
				Indexing: true,
				Paused:   true,
			},
			expected: "Update index: paused",
		},
		{
			name: "database exists with media",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: intPtr(100),
			},
			expected: "Update index: 100 media",
		},
		{
			name: "database exists with zero media",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: intPtr(0),
			},
			expected: "Update index: 0 media",
		},
		{
			name: "database exists with nil TotalMedia",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: nil,
			},
			expected: "Update index: 0 media",
		},
		{
			name: "database exists with large count",
			db: models.IndexingStatusResponse{
				Exists:     true,
				TotalMedia: intPtr(12345),
			},
			expected: "Update index: 12345 media",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatDBMenuLabel(tt.db)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatScrapeMenuLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Scrape metadata", formatScrapeMenuLabel(&models.ScrapingStatusResponse{}))
	assert.Equal(t, "Scrape metadata: 42 scraped", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		TotalScraped: 42,
	}))
	assert.Equal(t, "Scrape metadata: in progress", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		ScraperID:    "screenscraper",
		Scraping:     true,
		Processed:    12,
		TotalScraped: 42,
	}))
	assert.Equal(t, "Scrape metadata: paused", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		ScraperID: "screenscraper",
		Scraping:  true,
		Paused:    true,
	}))
	assert.Equal(t, "Scrape metadata: 42 scraped", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		ScraperID:    "screenscraper",
		Done:         true,
		Matched:      8,
		TotalScraped: 42,
	}))
	assert.Equal(t, "Scrape metadata: 8 matched", formatScrapeMenuLabel(&models.ScrapingStatusResponse{
		ScraperID: "screenscraper",
		Done:      true,
		Matched:   8,
	}))
}

func TestFormatScrapeProgress(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		"ScreenScraper - NES\nRecords: 3 / 10\nMatched: 2  Skipped: 1",
		formatScrapeProgress(&models.ScrapingStatusResponse{
			ScraperID: "screenscraper",
			SystemID:  "NES",
			Processed: 3,
			Total:     10,
			Matched:   2,
			Skipped:   1,
		}, "ScreenScraper"),
	)
	assert.Equal(t,
		"screenscraper\nRecords: 3 processed\nMatched: 2  Skipped: 1",
		formatScrapeProgress(&models.ScrapingStatusResponse{
			ScraperID: "screenscraper",
			Processed: 3,
			Matched:   2,
			Skipped:   1,
		}, ""),
	)
	assert.Equal(t,
		"screenscraper\nPaused: 3 / 10\nMatched: 2  Skipped: 1",
		formatScrapeProgress(&models.ScrapingStatusResponse{
			ScraperID: "screenscraper",
			Processed: 3,
			Total:     10,
			Matched:   2,
			Skipped:   1,
			Paused:    true,
		}, ""),
	)
}

func TestBlockedMediaOperationMenuLabels(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Update index: scrape running", mediaIndexBlockedByScrapeLabel())
	assert.Equal(t, "Scrape metadata: index running", mediaScrapeBlockedByIndexLabel())
}

func TestMediaIndexProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		current  int
		total    int
		expected float64
	}{
		{
			name:     "invalid current",
			current:  0,
			total:    10,
			expected: 0,
		},
		{
			name:     "uses current step like web UI",
			current:  3,
			total:    10,
			expected: 0.3,
		},
		{
			name:     "writing database uses current step",
			current:  10,
			total:    10,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := mediaIndexProgress(tt.current, tt.total)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

// intPtr is a helper to create *int.
func intPtr(v int) *int {
	return &v
}

func TestProgressBar_Draw_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 40, 3)
	defer runner.Stop()

	pb := NewProgressBar()
	pb.SetBorder(true)
	pb.SetProgress(0.5)

	runner.Start(pb)
	runner.Draw()

	// The progress bar should render with some filled and empty characters
	// We can't easily assert on specific characters, but we verify it doesn't panic
	// and renders something
}

func TestProgressBar_Draw_Empty(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 40, 3)
	defer runner.Stop()

	pb := NewProgressBar()
	pb.SetBorder(true)
	pb.SetProgress(0)

	runner.Start(pb)
	runner.Draw()

	// Verify no panic with 0% progress
}

func TestProgressBar_Draw_Full(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 40, 3)
	defer runner.Stop()

	pb := NewProgressBar()
	pb.SetBorder(true)
	pb.SetProgress(1.0)

	runner.Start(pb)
	runner.Draw()

	// Verify no panic with 100% progress
}

func TestProgressBar_BoundaryValues(t *testing.T) {
	t.Parallel()

	pb := NewProgressBar()

	// Values at boundaries
	pb.SetProgress(0)
	assert.InDelta(t, 0.0, pb.GetProgress(), 0.001)

	pb.SetProgress(1)
	assert.InDelta(t, 1.0, pb.GetProgress(), 0.001)

	// Just inside boundaries
	pb.SetProgress(0.001)
	assert.InDelta(t, 0.001, pb.GetProgress(), 0.001)

	pb.SetProgress(0.999)
	assert.InDelta(t, 0.999, pb.GetProgress(), 0.001)
}
