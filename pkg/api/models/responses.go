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

type BrowseEntry struct {
	SystemID  *string            `json:"systemId,omitempty"`
	RelPath   *string            `json:"relativePath,omitempty"`
	ZapScript *string            `json:"zapScript,omitempty"`
	FileCount *int               `json:"fileCount,omitempty"`
	Group     *string            `json:"group,omitempty"`
	Name      string             `json:"name"`
	Path      string             `json:"path"`
	Type      string             `json:"type"`
	Tags      []database.TagInfo `json:"tags,omitempty"`
}

type BrowseResults struct {
	Pagination *PaginationInfo `json:"pagination,omitempty"`
	Path       string          `json:"path"`
	Entries    []BrowseEntry   `json:"entries"`
	TotalFiles int             `json:"totalFiles"`
}

type SettingsResponse struct {
	UpdateChannel             string             `json:"updateChannel"`
	ReadersScanMode           string             `json:"readersScanMode"`
	ReadersScanIgnoreSystem   []string           `json:"readersScanIgnoreSystems"`
	ReadersConnect            []ReaderConnection `json:"readersConnect"`
	ReadersScanExitDelay      float32            `json:"readersScanExitDelay"`
	LaunchGuardTimeout        float32            `json:"launchGuardTimeout"`
	LaunchGuardDelay          float32            `json:"launchGuardDelay"`
	AudioVolume               int                `json:"audioVolume"`
	RunZapScript              bool               `json:"runZapScript"`
	DebugLogging              bool               `json:"debugLogging"`
	AudioScanFeedback         bool               `json:"audioScanFeedback"`
	ReadersAutoDetect         bool               `json:"readersAutoDetect"`
	ErrorReporting            bool               `json:"errorReporting"`
	LaunchGuardEnabled        bool               `json:"launchGuardEnabled"`
	LaunchGuardRequireConfirm bool               `json:"launchGuardRequireConfirm"`
}

type PlaytimeLimitsResponse struct {
	Daily        *string  `json:"daily,omitempty"`
	Session      *string  `json:"session,omitempty"`
	SessionReset *string  `json:"sessionReset,omitempty"`
	Retention    *int     `json:"retention,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	Enabled      bool     `json:"enabled"`
}

type PlaytimeStatusResponse struct {
	SessionStarted        *string `json:"sessionStarted,omitempty"`
	SessionDuration       *string `json:"sessionDuration,omitempty"`
	SessionCumulativeTime *string `json:"sessionCumulativeTime,omitempty"`
	SessionRemaining      *string `json:"sessionRemaining,omitempty"`
	CooldownRemaining     *string `json:"cooldownRemaining,omitempty"`
	DailyUsageToday       *string `json:"dailyUsageToday,omitempty"`
	DailyRemaining        *string `json:"dailyRemaining,omitempty"`
	State                 string  `json:"state"`
	SessionActive         bool    `json:"sessionActive"`
	LimitsEnabled         bool    `json:"limitsEnabled"`
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
	ReaderID string    `json:"readerId,omitempty"`
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
	Paused             bool    `json:"paused"`
}

type ReaderResponse struct {
	Driver    string `json:"driver"`
	Path      string `json:"path"`
	Connected bool   `json:"connected"`
}

type MediaHistoryResponseEntry struct {
	EndedAt    *string `json:"endedAt,omitempty"`
	SystemID   string  `json:"systemId"`
	SystemName string  `json:"systemName"`
	MediaName  string  `json:"mediaName"`
	MediaPath  string  `json:"mediaPath"`
	LauncherID string  `json:"launcherId"`
	StartedAt  string  `json:"startedAt"`
	PlayTime   int     `json:"playTime"`
}

type MediaHistoryResponse struct {
	Pagination *PaginationInfo             `json:"pagination,omitempty"`
	Entries    []MediaHistoryResponseEntry `json:"entries"`
}

type MediaHistoryTopEntry struct {
	SystemID      string `json:"systemId"`
	SystemName    string `json:"systemName"`
	MediaName     string `json:"mediaName"`
	MediaPath     string `json:"mediaPath"`
	LastPlayedAt  string `json:"lastPlayedAt"`
	TotalPlayTime int    `json:"totalPlayTime"`
	SessionCount  int    `json:"sessionCount"`
}

type MediaHistoryTopResponse struct {
	Entries []MediaHistoryTopEntry `json:"entries"`
}

// MediaMetaPropertyItem represents a single property value in a media.meta response.
// Data is nil when the property is text-only; otherwise it contains the base64-encoded binary.
type MediaMetaPropertyItem struct {
	Text        string  `json:"text"`
	ContentType string  `json:"contentType"`
	Data        *string `json:"data"`
}

// MediaMetaSystemResponse is the System sub-object within a media.meta response.
// Contains only DB-stored fields (id, name) with no static asset enrichment.
type MediaMetaSystemResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MediaMetaTitleResponse is the MediaTitle sub-object within a media.meta response,
// with its own level-separated tags and properties.
type MediaMetaTitleResponse struct {
	SecondarySlug *string                          `json:"secondarySlug,omitempty"`
	Slug          string                           `json:"slug"`
	Name          string                           `json:"name"`
	System        MediaMetaSystemResponse          `json:"system"`
	Tags          []database.TagInfo               `json:"tags"`
	Properties    map[string]MediaMetaPropertyItem `json:"properties"`
	ID            int64                            `json:"id"`
	SlugLength    int                              `json:"slugLength"`
	SlugWordCount int                              `json:"slugWordCount"`
}

// MediaMetaMediaResponse is the top-level Media object in a media.meta response.
type MediaMetaMediaResponse struct {
	Title      MediaMetaTitleResponse           `json:"title"`
	Path       string                           `json:"path"`
	ParentDir  string                           `json:"parentDir"`
	Tags       []database.TagInfo               `json:"tags"`
	Properties map[string]MediaMetaPropertyItem `json:"properties"`
	ID         int64                            `json:"id"`
	IsMissing  bool                             `json:"isMissing"`
}

// MediaMetaResponse is the response envelope for the media.meta method.
type MediaMetaResponse struct {
	Media MediaMetaMediaResponse `json:"media"`
}

// MediaImageResponse is the response for the media.image method.
// It contains the best-match image for a media record, base64-encoded.
type MediaImageResponse struct {
	ContentType string `json:"contentType"`
	Data        string `json:"data"`    // base64-encoded blob
	TypeTag     string `json:"typeTag"` // e.g. "property:image-boxart"
}

// ScrapingStatusResponse is broadcast as a "media.scraping" notification for
// each ScrapeUpdate received from the scraper and on completion/cancellation.
type ScrapingStatusResponse struct {
	ScraperID string `json:"scraperId,omitempty"`
	SystemID  string `json:"systemId,omitempty"`
	Processed int    `json:"processed"`
	Total     int    `json:"total"`
	Matched   int    `json:"matched"`
	Skipped   int    `json:"skipped"`
	Scraping  bool   `json:"scraping"`
	Done      bool   `json:"done"`
}

type MediaLookupMatch struct {
	System     System             `json:"system"`
	Name       string             `json:"name"`
	Path       string             `json:"path"`
	ZapScript  string             `json:"zapScript"`
	Tags       []database.TagInfo `json:"tags"`
	Confidence float64            `json:"confidence"`
}

type MediaLookupResponse struct {
	Match *MediaLookupMatch `json:"match"`
}

type ActiveMedia struct {
	Started          time.Time `json:"started"`
	LauncherID       string    `json:"launcherId"`
	SystemID         string    `json:"systemId"`
	SystemName       string    `json:"systemName"`
	Path             string    `json:"mediaPath"`
	Name             string    `json:"mediaName"`
	LauncherControls []string  `json:"launcherControls,omitempty"`
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

// ActiveMediaResponse is the API response type for active media, including ZapScript.
type ActiveMediaResponse struct {
	ZapScript string `json:"zapScript"`
	ActiveMedia
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

type HealthCheckResponse struct {
	Status string `json:"status"`
}

type MediaResponse struct {
	Database IndexingStatusResponse `json:"database"`
	Active   []ActiveMediaResponse  `json:"active"`
}

type TokensResponse struct {
	Last   *TokenResponse  `json:"last,omitempty"`
	Active []TokenResponse `json:"active"`
}

type ClientResponse struct {
	Name    string    `json:"name"`
	Address string    `json:"address"`
	Secret  string    `json:"secret"` //nolint:gosec // G117: pairing secret, not a credential
	ID      uuid.UUID `json:"id"`
}

type LogDownloadResponse struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
	Size     int    `json:"size"`
}

type ReaderInfo struct {
	ID           string   `json:"id"`
	ReaderID     string   `json:"readerId"`
	Driver       string   `json:"driver"`
	Info         string   `json:"info"`
	Capabilities []string `json:"capabilities"`
	Connected    bool     `json:"connected"`
}

type ReadersResponse struct {
	Readers []ReaderInfo `json:"readers"`
}

type InboxMessage struct {
	CreatedAt time.Time `json:"createdAt"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	Category  string    `json:"category,omitempty"`
	ID        int64     `json:"id"`
	Severity  int       `json:"severity"`
	ProfileID int64     `json:"profileId,omitempty"`
}

type InboxResponse struct {
	Messages []InboxMessage `json:"messages"`
}

// PairedClient represents a client paired via the API encryption flow.
// PairingKey and AuthToken are intentionally omitted from the public API
// surface — only the metadata identifying the client is exposed.
type PairedClient struct {
	ClientID   string `json:"clientId"`
	ClientName string `json:"clientName"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
}

// ClientsResponse is the response for the clients RPC method.
type ClientsResponse struct {
	Clients []PairedClient `json:"clients"`
}

// ClientsDeleteParams is the parameters object for the clients.delete RPC method.
type ClientsDeleteParams struct {
	ClientID string `json:"clientId"`
}

// ClientsPairStartResponse is the response for the clients.pair.start RPC method.
type ClientsPairStartResponse struct {
	PIN       string `json:"pin"`
	ExpiresAt int64  `json:"expiresAt"`
}

// ClientsPairedNotification is the payload for the clients.paired notification,
// broadcast when a client successfully completes the PAKE pairing flow.
type ClientsPairedNotification struct {
	ClientID   string `json:"clientId"`
	ClientName string `json:"clientName"`
}

type SettingsAuthClaimResponse struct {
	Domains []string `json:"domains"`
}

type UpdateCheckResponse struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	ReleaseNotes    string `json:"releaseNotes,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

type UpdateApplyResponse struct {
	PreviousVersion string `json:"previousVersion"`
	NewVersion      string `json:"newVersion"`
}

type ScreenshotResponse struct {
	Path string `json:"path"`
	Data string `json:"data"` // base64 encoded
	Size int    `json:"size"` // original byte count
}
