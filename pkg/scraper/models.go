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
type ScrapedMetadata struct {
	DBID           int64
	MediaTitleDBID int64
	ScraperSource  string
	Description    string
	Genre          string
	Players        string
	ReleaseDate    string
	Developer      string
	Publisher      string
	Rating         float64
	ScrapedAt      time.Time
}

// GameHashes represents file hash information for scraper matching
type GameHashes struct {
	DBID       int64
	MediaDBID  int64
	CRC32      string
	MD5        string
	SHA1       string
	FileSize   int64
	ComputedAt time.Time
}

// ScraperProgress represents the current scraping progress
type ScraperProgress struct {
	IsRunning       bool   `json:"isRunning"`
	CurrentGame     string `json:"currentGame"`
	ProcessedGames  int    `json:"processedGames"`
	TotalGames      int    `json:"totalGames"`
	DownloadedFiles int    `json:"downloadedFiles"`
	SkippedFiles    int    `json:"skippedFiles"`
	ErrorCount      int    `json:"errorCount"`
	StartTime       *time.Time `json:"startTime"`
	EstimatedEnd    *time.Time `json:"estimatedEnd"`
}

// ScraperJob represents a scraping job in the queue
type ScraperJob struct {
	MediaDBID   int64
	MediaTitle  string
	SystemID    string
	GamePath    string
	MediaTypes  []MediaType
	Overwrite   bool
	Priority    int
}

// ScraperConfig represents scraper configuration
type ScraperConfig struct {
	DefaultScraper     string      `toml:"default"`
	Region             string      `toml:"region"`
	Language           string      `toml:"language"`
	DownloadCovers     bool        `toml:"download_covers"`
	DownloadScreenshots bool       `toml:"download_screenshots"`
	DownloadVideos     bool        `toml:"download_videos"`
	MaxConcurrent      int         `toml:"max_concurrent"`
	RateLimit          int         `toml:"rate_limit"`
	DefaultMediaTypes  []MediaType `toml:"-"`
}

// DefaultScraperConfig returns the default configuration
func DefaultScraperConfig() *ScraperConfig {
	return &ScraperConfig{
		DefaultScraper:      "screenscraper",
		Region:              "us",
		Language:            "en",
		DownloadCovers:      true,
		DownloadScreenshots: true,
		DownloadVideos:      false,
		MaxConcurrent:       3,
		RateLimit:           1000, // milliseconds between requests
		DefaultMediaTypes:   []MediaType{MediaTypeCover, MediaTypeScreenshot},
	}
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