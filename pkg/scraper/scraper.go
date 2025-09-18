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
	"context"
)

// Scraper is the main interface for all scraper implementations
type Scraper interface {
	// Search for games matching the query
	Search(ctx context.Context, query ScraperQuery) ([]ScraperResult, error)

	// Get detailed game information including media URLs
	GetGameInfo(ctx context.Context, gameID string) (*GameInfo, error)

	// Download media files for a game
	DownloadMedia(ctx context.Context, media MediaItem) error

	// Check if scraper supports the given platform
	IsSupportedPlatform(systemID string) bool

	// Get supported media types
	GetSupportedMediaTypes() []MediaType

	// Get scraper name and version
	GetInfo() ScraperInfo
}

// MediaType represents different types of game media
type MediaType string

const (
	MediaTypeCover      MediaType = "cover"      // Box art front
	MediaTypeBoxBack    MediaType = "boxback"    // Box art back
	MediaTypeScreenshot MediaType = "screenshot" // In-game screenshot
	MediaTypeTitleShot  MediaType = "titleshot"  // Title screen
	MediaTypeVideo      MediaType = "video"      // Gameplay video
	MediaTypeFanArt     MediaType = "fanart"     // Fan artwork
	MediaTypeMarquee    MediaType = "marquee"    // Arcade marquee
	MediaTypeWheel      MediaType = "wheel"      // Wheel/logo art
	MediaTypeCartridge  MediaType = "cartridge"  // Cartridge/media
	MediaTypeManual     MediaType = "manual"     // Game manual
	MediaTypeBezel      MediaType = "bezel"      // Screen bezel
	MediaTypeMap        MediaType = "map"        // Game map
)

// ScraperQuery contains search parameters
type ScraperQuery struct {
	Name     string
	SystemID string
	Hash     *FileHash
	Region   string
	Language string
}

// ScraperResult represents a game search result
type ScraperResult struct {
	ID          string
	Name        string
	Description string
	SystemID    string
	Region      string
	Language    string
	Relevance   float64 // 0.0 to 1.0 relevance score
}

// GameInfo contains detailed game information
type GameInfo struct {
	ID          string
	Name        string
	Description string
	Genre       string
	Players     string
	ReleaseDate string
	Developer   string
	Publisher   string
	Region      string
	Language    string
	Media       []MediaItem
	Rating      float64
}

// MediaItem represents a downloadable media file
type MediaItem struct {
	Type        MediaType
	URL         string
	Format      string
	Region      string
	Description string
	Width       int
	Height      int
	Size        int64
}

// ScraperInfo contains scraper metadata
type ScraperInfo struct {
	Name         string
	Version      string
	Description  string
	Website      string
	RequiresAuth bool
}

// FileHash contains file hash information for matching
type FileHash struct {
	CRC32    string
	MD5      string
	SHA1     string
	FileSize int64
}
