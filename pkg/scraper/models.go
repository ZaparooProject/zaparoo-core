// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"time"
)

// ScrapedMetadata represents scraped game metadata stored in database
// This struct provides a convenient interface while internally using the Tags system
type ScrapedMetadata struct {
	DBID           int64     // Not used - metadata stored as Tags
	MediaTitleDBID int64     // References MediaTitles.DBID
	ScraperSource  string    // Stored as scraper_source tag
	Description    string    // Stored as description tag
	Genre          string    // Stored as genre tag
	Players        string    // Stored as players tag
	ReleaseDate    string    // Stored as release_date tag
	Developer      string    // Stored as developer tag
	Publisher      string    // Stored as publisher tag
	Rating         float64   // Stored as rating tag
	ScrapedAt      time.Time // Stored as scraped_at tag
}

// GameHashes represents file hash information for scraper matching
type GameHashes struct {
	DBID       int64
	SystemID   string
	MediaPath  string
	CRC32      string
	MD5        string
	SHA1       string
	FileSize   int64
	ComputedAt time.Time
}

// ScraperProgress represents the current scraping progress
type ScraperProgress struct {
	StartTime       *time.Time `json:"startTime"`
	EstimatedEnd    *time.Time `json:"estimatedEnd"`
	CurrentGame     string     `json:"currentGame"`
	ProcessedGames  int        `json:"processedGames"`
	TotalGames      int        `json:"totalGames"`
	DownloadedFiles int        `json:"downloadedFiles"`
	SkippedFiles    int        `json:"skippedFiles"`
	ErrorCount      int        `json:"errorCount"`
	IsRunning       bool       `json:"isRunning"`
}

// ScraperJob represents a scraping job in the queue
type ScraperJob struct {
	MediaTitle string
	SystemID   string
	GamePath   string
	MediaTypes []MediaType
	MediaDBID  int64
	Priority   int
	Overwrite  bool
}

// ScraperConfig represents scraper configuration
type ScraperConfig struct {
	DefaultScraper      string      `toml:"default"`
	Region              string      `toml:"region"`
	Language            string      `toml:"language"`
	DefaultMediaTypes   []MediaType `toml:"-"`
	DownloadCovers      bool        `toml:"download_covers"`
	DownloadScreenshots bool        `toml:"download_screenshots"`
	DownloadVideos      bool        `toml:"download_videos"`
}


// UpdateDefaultMediaTypes updates the DefaultMediaTypes slice based on boolean flags
func (c *ScraperConfig) UpdateDefaultMediaTypes() {
	c.DefaultMediaTypes = []MediaType{}
	if c.DownloadCovers {
		c.DefaultMediaTypes = append(c.DefaultMediaTypes, MediaTypeCover)
	}
	if c.DownloadScreenshots {
		c.DefaultMediaTypes = append(c.DefaultMediaTypes, MediaTypeScreenshot)
	}
	if c.DownloadVideos {
		c.DefaultMediaTypes = append(c.DefaultMediaTypes, MediaTypeVideo)
	}
}
