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

package models

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/google/uuid"
)

type SearchResultMedia struct {
	System    System             `json:"system"`
	Name      string             `json:"name"`
	Path      string             `json:"path"`
	ZapScript string             `json:"zapScript"`
	Tags      []database.TagInfo `json:"tags"`
}

type PaginationInfo struct {
	NextCursor  *string `json:"nextCursor,omitempty"`
	HasNextPage bool    `json:"hasNextPage"`
	PageSize    int     `json:"pageSize"`
}

type SearchResults struct {
	Pagination *PaginationInfo     `json:"pagination,omitempty"`
	Results    []SearchResultMedia `json:"results"`
	Total      int                 `json:"total"`
}

type TagsResponse struct {
	Tags []database.TagInfo `json:"tags"`
}

type SettingsResponse struct {
	ReadersScanMode         string   `json:"readersScanMode"`
	ReadersScanIgnoreSystem []string `json:"readersScanIgnoreSystems"`
	ReadersScanExitDelay    float32  `json:"readersScanExitDelay"`
	RunZapScript            bool     `json:"runZapScript"`
	DebugLogging            bool     `json:"debugLogging"`
	AudioScanFeedback       bool     `json:"audioScanFeedback"`
	ReadersAutoDetect       bool     `json:"readersAutoDetect"`
}

type System struct {
	ReleaseDate  *string `json:"releaseDate,omitempty"`
	Manufacturer *string `json:"manufacturer,omitempty"`
	ID           string  `json:"id,omitempty"`
	Name         string  `json:"name,omitempty"`
	Category     string  `json:"category,omitempty"`
}

type SystemsResponse struct {
	Systems []System `json:"systems"`
}

type HistoryResponseEntry struct {
	Time    time.Time `json:"time"`
	Type    string    `json:"type"`
	UID     string    `json:"uid"`
	Text    string    `json:"text"`
	Data    string    `json:"data"`
	Success bool      `json:"success"`
}

type HistoryResponse struct {
	Entries []HistoryResponseEntry `json:"entries"`
}

type AllMappingsResponse struct {
	Mappings []MappingResponse `json:"mappings"`
}

type MappingResponse struct {
	ID       string `json:"id"`
	Added    string `json:"added"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Match    string `json:"match"`
	Pattern  string `json:"pattern"`
	Override string `json:"override"`
	Enabled  bool   `json:"enabled"`
}

type TokenResponse struct {
	ScanTime time.Time `json:"scanTime"`
	Type     string    `json:"type"`
	UID      string    `json:"uid"`
	Text     string    `json:"text"`
	Data     string    `json:"data"`
}

type PlaytimeLimitReachedParams struct {
	Reason string `json:"reason"`
}

type PlaytimeLimitWarningParams struct {
	Interval  string `json:"interval"`
	Remaining string `json:"remaining"`
}

type IndexingStatusResponse struct {
	TotalSteps         *int    `json:"totalSteps,omitempty"`
	CurrentStep        *int    `json:"currentStep,omitempty"`
	CurrentStepDisplay *string `json:"currentStepDisplay,omitempty"`
	TotalFiles         *int    `json:"totalFiles,omitempty"`
	TotalMedia         *int    `json:"totalMedia,omitempty"`
	Exists             bool    `json:"exists"`
	Indexing           bool    `json:"indexing"`
	Optimizing         bool    `json:"optimizing"`
}

type ReaderResponse struct {
	Driver    string `json:"driver"`
	Path      string `json:"path"`
	Connected bool   `json:"connected"`
}

type ActiveMedia struct {
	Started    time.Time `json:"started"`
	LauncherID string    `json:"launcherId"`
	SystemID   string    `json:"systemId"`
	SystemName string    `json:"systemName"`
	Path       string    `json:"mediaPath"`
	Name       string    `json:"mediaName"`
}

// NewActiveMedia creates a new ActiveMedia with the current timestamp.
func NewActiveMedia(systemID, systemName, path, name, launcherID string) *ActiveMedia {
	return &ActiveMedia{
		Started:    time.Now(),
		LauncherID: launcherID,
		SystemID:   systemID,
		SystemName: systemName,
		Path:       path,
		Name:       name,
	}
}

func (a *ActiveMedia) Equal(with *ActiveMedia) bool {
	if with == nil {
		return false
	}
	if a.SystemID != with.SystemID {
		return false
	}
	if a.SystemName != with.SystemName {
		return false
	}

	// Get the MediaType from each system
	mediaTypeA := slugs.MediaTypeGame // Default
	if a.SystemID != "" {
		if system, err := systemdefs.GetSystem(a.SystemID); err == nil {
			mediaTypeA = system.GetMediaType()
		}
	}

	mediaTypeB := slugs.MediaTypeGame // Default
	if with.SystemID != "" {
		if system, err := systemdefs.GetSystem(with.SystemID); err == nil {
			mediaTypeB = system.GetMediaType()
		}
	}

	// Compare names by slugifying them to handle minor formatting differences
	// (e.g., "Game Name" vs "game-name", "S01E02" vs "1x02" are considered equal)
	slugA := slugs.Slugify(mediaTypeA, a.Name)
	slugB := slugs.Slugify(mediaTypeB, with.Name)

	// If names match (after slugification), consider them equal regardless of path
	// This handles cases where launcher uses virtual paths (kodi-episode://123)
	// but tracker detects real paths (smb://server/file.mkv)
	if slugA == slugB {
		return true
	}

	// If names don't match, require exact path match for equality
	// (different content that happens to have same system)
	if a.Path == with.Path {
		return true
	}

	return false
}

type VersionResponse struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

type MediaResponse struct {
	Database IndexingStatusResponse `json:"database"`
	Active   []ActiveMedia          `json:"active"`
}

type TokensResponse struct {
	Last   *TokenResponse  `json:"last,omitempty"`
	Active []TokenResponse `json:"active"`
}

type ClientResponse struct {
	Name    string    `json:"name"`
	Address string    `json:"address"`
	Secret  string    `json:"secret"`
	ID      uuid.UUID `json:"id"`
}

type LogDownloadResponse struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
	Size     int    `json:"size"`
}

type ReaderInfo struct {
	ID           string   `json:"id"`
	Info         string   `json:"info"`
	Capabilities []string `json:"capabilities"`
	Connected    bool     `json:"connected"`
}

type ReadersResponse struct {
	Readers []ReaderInfo `json:"readers"`
}
