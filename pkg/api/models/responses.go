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

type UIChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type UIEvent struct {
	CreatedAt        time.Time   `json:"createdAt"`
	ExpiresAt        *time.Time  `json:"expiresAt,omitempty"`
	ID               string      `json:"id"`
	Kind             UIEventKind `json:"kind"`
	Title            string      `json:"title,omitempty"`
	Message          string      `json:"message,omitempty"`
	SelectedChoiceID string      `json:"selectedChoiceId,omitempty"`
	Choices          []UIChoice  `json:"choices,omitempty"`
	Dismissible      bool        `json:"dismissible"`
}

type UIResolution struct {
	ID       string    `json:"id"`
	Outcome  UIOutcome `json:"outcome"`
	ChoiceID string    `json:"choiceId,omitempty"`
}

type UIStateResponse struct {
	Events   []UIEvent      `json:"events"`
	Resolved []UIResolution `json:"resolved"`
	Revision uint64         `json:"revision"`
}

type SearchResultMedia struct {
	RelPath            *string            `json:"relativePath,omitempty"`
	System             System             `json:"system"`
	Name               string             `json:"name"`
	Path               string             `json:"path"`
	ZapScript          string             `json:"zapScript"`
	Tags               []database.TagInfo `json:"tags"`
	DisambiguatingTags []database.TagInfo `json:"disambiguatingTags,omitempty"`
	MediaID            int64              `json:"mediaId,omitempty"`
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
	SystemID           *string            `json:"systemId,omitempty"`
	RelPath            *string            `json:"relativePath,omitempty"`
	ZapScript          *string            `json:"zapScript,omitempty"`
	FileCount          *int               `json:"fileCount,omitempty"`
	Group              *string            `json:"group,omitempty"`
	Path               string             `json:"path"`
	Type               string             `json:"type"`
	Name               string             `json:"name"`
	SystemIDs          []string           `json:"systemIds,omitempty"`
	Tags               []database.TagInfo `json:"tags,omitempty"`
	DisambiguatingTags []database.TagInfo `json:"disambiguatingTags,omitempty"`
	MediaID            int64              `json:"mediaId,omitempty"`
	HasCover           bool               `json:"hasCover"`
}

type BrowseResults struct {
	Pagination *PaginationInfo `json:"pagination,omitempty"`
	Path       string          `json:"path"`
	Entries    []BrowseEntry   `json:"entries"`
	TotalFiles int             `json:"totalFiles"`
	TotalDirs  int             `json:"totalDirs"`
}

// BrowseIndexGroup is one first-character section of a browse list. Key is the
// stable bucket identifier and Label is what to display (equal for the Latin
// scheme; separated so a future locale scheme can show a glyph differing from
// the key). Cursor is an opaque media.browse cursor positioned just before the
// bucket's first row: passing it to media.browse with the same scope returns a
// continuous page that begins at the bucket. Clients must treat Key and Cursor
// as opaque. Offset is the 0-based position of the bucket's first item among
// the scope's media files (excluding any leading directories), for clients that
// jump to a position in the full list rather than reload from the cursor.
type BrowseIndexGroup struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Cursor string `json:"cursor"`
	Count  int    `json:"count"`
	Offset int    `json:"offset"`
}

// BrowseIndexResults is the response for media.browse.index. Scheme reports the
// collation used to derive buckets ("latin"), or "none" when no letter rail
// applies to the scope (non-alphabetical sort, or a root listing); Groups is
// then empty. Groups is authoritative and already ordered for the active sort;
// clients render it as-is without assuming any alphabet.
type BrowseIndexResults struct {
	Scheme     string             `json:"scheme"`
	Groups     []BrowseIndexGroup `json:"groups"`
	TotalFiles int                `json:"totalFiles"`
}

type SettingsResponse struct {
	UpdateChannel             string             `json:"updateChannel"`
	ReadersScanMode           string             `json:"readersScanMode"`
	ReadersScanIgnoreSystem   []string           `json:"readersScanIgnoreSystems"`
	ReadersConnect            []ReaderConnection `json:"readersConnect"`
	SystemDefaults            []SystemDefault    `json:"systemDefaults"`
	AudioVolume               int                `json:"audioVolume"`
	LaunchGuardTimeout        float32            `json:"launchGuardTimeout"`
	LaunchGuardDelay          float32            `json:"launchGuardDelay"`
	ReadersScanExitDelay      float32            `json:"readersScanExitDelay"`
	RunZapScript              bool               `json:"runZapScript"`
	DebugLogging              bool               `json:"debugLogging"`
	AudioScanFeedback         bool               `json:"audioScanFeedback"`
	ReadersAutoDetect         bool               `json:"readersAutoDetect"`
	ErrorReporting            bool               `json:"errorReporting"`
	Encryption                bool               `json:"encryption"`
	LaunchGuardEnabled        bool               `json:"launchGuardEnabled"`
	LaunchGuardRequireConfirm bool               `json:"launchGuardRequireConfirm"`
	ProfilesRequireForLaunch  bool               `json:"profilesRequireForLaunch"`
	ProfilesSwapData          bool               `json:"profilesSwapData"`
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
	ZapScript    string  `json:"zapScript,omitempty"`
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
	// Source identifies where the mapping came from: "database" or "file".
	Source string `json:"source"`
	// ReadOnly is true for mappings that can't be edited via the API (file mappings).
	ReadOnly bool `json:"readOnly"`
	Enabled  bool `json:"enabled"`
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
	// MissingMedia is the count of indexed media flagged missing on disk —
	// what media.clean.orphans would remove.
	MissingMedia *int `json:"missingMedia,omitempty"`
	// SystemsCompleted/SystemsTotal report per-system indexing coverage so
	// clients can serve partial results while a scan is still running.
	SystemsCompleted *int `json:"systemsCompleted,omitempty"`
	SystemsTotal     *int `json:"systemsTotal,omitempty"`
	Exists           bool `json:"exists"`
	Indexing         bool `json:"indexing"`
	Optimizing       bool `json:"optimizing"`
	Paused           bool `json:"paused"`
	// Throttled reports that indexing is running at reduced speed while
	// media plays.
	Throttled bool `json:"throttled,omitempty"`
}

type ReaderResponse struct {
	Driver    string `json:"driver"`
	Path      string `json:"path"`
	Connected bool   `json:"connected"`
}

type MediaHistoryResponseEntry struct {
	RelPath    *string `json:"relativePath,omitempty"`
	EndedAt    *string `json:"endedAt,omitempty"`
	SystemID   string  `json:"systemId"`
	SystemName string  `json:"systemName"`
	MediaName  string  `json:"mediaName"`
	MediaPath  string  `json:"mediaPath"`
	LauncherID string  `json:"launcherId"`
	StartedAt  string  `json:"startedAt"`
	PlayTime   int     `json:"playTime"`
	MediaID    int64   `json:"mediaId,omitempty"`
}

type MediaHistoryResponse struct {
	Pagination *PaginationInfo             `json:"pagination,omitempty"`
	Entries    []MediaHistoryResponseEntry `json:"entries"`
}

type MediaHistoryLatestEntry struct {
	SystemID   string `json:"systemId"`
	SystemName string `json:"systemName"`
	MediaName  string `json:"mediaName"`
	MediaPath  string `json:"mediaPath"`
	LauncherID string `json:"launcherId"`
	StartedAt  string `json:"startedAt"`
}

type MediaHistoryLatestResponse struct {
	Entry *MediaHistoryLatestEntry `json:"entry"`
}

type MediaHistoryTopEntry struct {
	RelPath       *string `json:"relativePath,omitempty"`
	SystemID      string  `json:"systemId"`
	SystemName    string  `json:"systemName"`
	MediaName     string  `json:"mediaName"`
	MediaPath     string  `json:"mediaPath"`
	LastPlayedAt  string  `json:"lastPlayedAt"`
	TotalPlayTime int     `json:"totalPlayTime"`
	SessionCount  int     `json:"sessionCount"`
	MediaID       int64   `json:"mediaId,omitempty"`
}

type MediaHistoryTopResponse struct {
	Entries []MediaHistoryTopEntry `json:"entries"`
}

// MediaMetaPropertyItem represents a single property value in a media.meta response.
// Binary payloads are intentionally not included; use media.image to fetch image bytes.
type MediaMetaPropertyItem struct {
	Extension   *string `json:"extension,omitempty"`
	Text        string  `json:"text"`
	ContentType string  `json:"contentType"`
	BlobSize    int64   `json:"blobSize,omitempty"`
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
	SecondarySlug       *string                          `json:"secondarySlug,omitempty"`
	Properties          map[string]MediaMetaPropertyItem `json:"properties"`
	System              MediaMetaSystemResponse          `json:"system"`
	Slug                string                           `json:"slug"`
	Name                string                           `json:"name"`
	Tags                []database.TagInfo               `json:"tags"`
	AvailableImageTypes []string                         `json:"availableImageTypes,omitempty"`
	SlugLength          int                              `json:"slugLength"`
	SlugWordCount       int                              `json:"slugWordCount"`
}

// MediaMetaMediaResponse is the top-level Media object in a media.meta response.
type MediaMetaMediaResponse struct {
	Properties          map[string]MediaMetaPropertyItem `json:"properties"`
	LauncherOverride    *string                          `json:"launcherOverride,omitempty"`
	Path                string                           `json:"path"`
	ParentDir           string                           `json:"parentDir"`
	Tags                []database.TagInfo               `json:"tags"`
	AvailableImageTypes []string                         `json:"availableImageTypes,omitempty"`
	Title               MediaMetaTitleResponse           `json:"title"`
	IsMissing           bool                             `json:"isMissing"`
}

// MediaMetaResponse is the response envelope for the media.meta method.
type MediaMetaResponse struct {
	Media MediaMetaMediaResponse `json:"media"`
}

type MediaMetaBatchItemResponse struct {
	Media *MediaMetaMediaResponse `json:"media,omitempty"`
	Error *string                 `json:"error,omitempty"`
}

type MediaMetaBatchResponse struct {
	Items []MediaMetaBatchItemResponse `json:"items"`
}

// MediaImageResponse is the response for the media.image method.
// It contains the best-match image for a media record, base64-encoded.
type MediaImageResponse struct {
	Extension   *string `json:"extension,omitempty"`
	ContentType string  `json:"contentType"`
	Data        string  `json:"data"`    // base64-encoded blob
	TypeTag     string  `json:"typeTag"` // e.g. "property:image-boxart"
}

type ScrapeSystemProgressResponse struct {
	SystemID   string `json:"systemId"`
	SystemName string `json:"systemName,omitempty"`
	Processed  int    `json:"processed"`
	Total      int    `json:"total"`
	Matched    int    `json:"matched"`
	Skipped    int    `json:"skipped"`
}

// ScrapingStatusResponse is broadcast as a "media.scraping" notification for
// each ScrapeUpdate received from the scraper and on completion/cancellation.
type ScrapingStatusResponse struct {
	CurrentStep        *int                          `json:"currentStep,omitempty"`
	CurrentStepDisplay *string                       `json:"currentStepDisplay,omitempty"`
	TotalSteps         *int                          `json:"totalSteps,omitempty"`
	CurrentSystem      *ScrapeSystemProgressResponse `json:"currentSystem,omitempty"`
	ScraperID          string                        `json:"scraperId,omitempty"`
	SystemID           string                        `json:"systemId,omitempty"`
	State              string                        `json:"state,omitempty"`
	Error              string                        `json:"error,omitempty"`
	Processed          int                           `json:"processed"`
	Total              int                           `json:"total"`
	Matched            int                           `json:"matched"`
	Skipped            int                           `json:"skipped"`
	TotalScraped       int                           `json:"totalScraped"`
	Scraping           bool                          `json:"scraping"`
	Done               bool                          `json:"done"`
	Paused             bool                          `json:"paused"`
	// Throttled reports that scraping is running at reduced speed while
	// media plays.
	Throttled bool `json:"throttled,omitempty"`
	Force     bool `json:"force"`
}

type MediaLookupMatch struct {
	RelPath            *string            `json:"relativePath,omitempty"`
	System             System             `json:"system"`
	Name               string             `json:"name"`
	Path               string             `json:"path"`
	ZapScript          string             `json:"zapScript"`
	Tags               []database.TagInfo `json:"tags"`
	DisambiguatingTags []database.TagInfo `json:"disambiguatingTags,omitempty"`
	MediaID            int64              `json:"mediaId,omitempty"`
	Confidence         float64            `json:"confidence"`
}

type MediaLookupResponse struct {
	Match *MediaLookupMatch `json:"match"`
}

type MediaCleanOrphansResponse struct {
	Deleted int64 `json:"deleted"`
}

// ScraperInfo is one entry in the ScrapersResponse list.
type ScraperInfo struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	SupportedSystems []string `json:"supportedSystems"`
}

// ScrapersResponse is the result returned by the "scrapers" RPC method.
type ScrapersResponse struct {
	Scrapers []ScraperInfo `json:"scrapers"`
}

type ActiveMedia struct {
	Started          time.Time `json:"started"`
	RelPath          *string   `json:"relativePath,omitempty"`
	PositionMs       *int64    `json:"positionMs,omitempty"`
	DurationMs       *int64    `json:"durationMs,omitempty"`
	LauncherID       string    `json:"launcherId"`
	SystemID         string    `json:"systemId"`
	SystemName       string    `json:"systemName"`
	Path             string    `json:"mediaPath"`
	Name             string    `json:"mediaName"`
	Slot             string    `json:"slot,omitempty"`
	LauncherControls []string  `json:"launcherControls,omitempty"`
	MediaID          int64     `json:"mediaId,omitempty"`
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
		Slot:       "primary",
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
	if a.Slot != with.Slot {
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

// PlaylistItemInfo is one entry in a PlaylistState.
type PlaylistItemInfo struct {
	Name      string `json:"name"`
	ZapScript string `json:"zapScript"`
}

// PlaylistState describes the current state of a playlist slot as exposed by
// the media response. Repeat is one of "none", "all", or "one".
type PlaylistState struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Slot    string             `json:"slot"`
	Repeat  string             `json:"repeat"`
	Items   []PlaylistItemInfo `json:"items"`
	Index   int                `json:"index"`
	Total   int                `json:"total"`
	Playing bool               `json:"playing"`
}

type MediaResponse struct {
	Database  IndexingStatusResponse `json:"database"`
	Active    []ActiveMediaResponse  `json:"active"`
	Playlists []PlaylistState        `json:"playlists,omitempty"`
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

type MediaTitleParseResponse struct {
	SecondarySlug *string `json:"secondarySlug,omitempty"`
	Slug          string  `json:"slug"`
	Name          string  `json:"name"`
	SlugLength    int     `json:"slugLength"`
	SlugWordCount int     `json:"slugWordCount"`
}

type Launcher struct {
	ID                 string   `json:"id"`
	SystemID           string   `json:"systemId,omitempty"`
	SystemName         string   `json:"systemName,omitempty"`
	AvailabilityReason string   `json:"availabilityReason,omitempty"`
	Groups             []string `json:"groups,omitempty"`
	Available          bool     `json:"available"`
}

type LaunchersResponse struct {
	Launchers []Launcher `json:"launchers"`
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
	Role       string `json:"role"`
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

// ProfileResponse represents a device profile in API responses. The PIN
// hash is never exposed — only whether a PIN is set. SwitchID is a bearer
// credential (presenting it authorizes a PIN-free switch on every path),
// so it is only included for privileged clients that need it for
// card-writing UX; for other clients it is omitted.
type ProfileResponse struct {
	LimitsEnabled *bool   `json:"limitsEnabled,omitempty"`
	DailyLimit    *string `json:"dailyLimit,omitempty"`
	SessionLimit  *string `json:"sessionLimit,omitempty"`
	LastUsedAt    *int64  `json:"lastUsedAt,omitempty"`
	ProfileID     string  `json:"profileId"`
	Name          string  `json:"name"`
	Role          string  `json:"role"`
	SwitchID      string  `json:"switchId,omitempty"`
	CreatedAt     int64   `json:"createdAt"`
	LastUpdatedAt int64   `json:"lastUpdatedAt"`
	HasPIN        bool    `json:"hasPin"`
}

// ProfilesResponse is the response for the profiles RPC method.
type ProfilesResponse struct {
	Profiles []ProfileResponse `json:"profiles"`
}

// ProfileVerifyResponse is the response for the profiles.verify RPC
// method: the identity of the profile whose credential was verified.
// Verification grants nothing server-side — the client owns whatever it
// unlocks with it.
type ProfileVerifyResponse struct {
	ProfileID string `json:"profileId"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	HasPIN    bool   `json:"hasPin"`
}

// ActiveProfile is a snapshot of the device's active profile, held in
// service state and broadcast on the profiles.active notification. It
// carries the resolved limit overrides so the playtime hot path never
// touches the database. Nil limit fields mean "inherit global config".
type ActiveProfile struct {
	LimitsEnabled *bool   `json:"limitsEnabled,omitempty"`
	DailyLimit    *string `json:"dailyLimit,omitempty"`
	SessionLimit  *string `json:"sessionLimit,omitempty"`
	ProfileID     string  `json:"profileId"`
	Name          string  `json:"name"`
	Role          string  `json:"role"`
	HasPIN        bool    `json:"hasPin"`
}

// ProfilesActiveNotification is the payload for the profiles.active
// notification. Profile is null when the device has no active profile.
type ProfilesActiveNotification struct {
	Profile *ActiveProfile `json:"profile"`
}

// ProfilesDataNotification is the payload for the profiles.data
// notification, reporting the state of profile data swapping (save files
// etc.) after a profile change. ProfileID is empty for the shared profile.
// Status is one of the ProfilesData* constants; Reason is a human-readable
// explanation for failed/unavailable statuses.
type ProfilesDataNotification struct {
	ProfileID string `json:"profileId"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
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
