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
	ScrapedAt      time.Time
	ScraperSource  string
	Description    string
	Genre          string
	Players        string
	ReleaseDate    string
	Developer      string
	Publisher      string
	DBID           int64
	MediaTitleDBID int64
	Rating         float64
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
	Status          string     `json:"status"`          // "idle", "running", "completed", "failed", "cancelled"
	LastError       string     `json:"lastError"`       // Last critical error message
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
