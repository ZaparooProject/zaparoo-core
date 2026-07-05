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

package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// This file contains shared database types and interfaces.
// Types are defined here to avoid circular imports between userdb and mediadb packages.

// Database is a portable interface for ENV bindings
type Database struct {
	UserDB  UserDBI
	MediaDB MediaDBI
}

type ScrapingOperation struct {
	ScraperID string   `json:"scraperId"`
	RunID     string   `json:"runId,omitempty"`
	Systems   []string `json:"systems"`
	Force     bool     `json:"force"`
}

// Structs for SQL records

type HistoryEntry struct {
	Time           time.Time  `json:"time"`
	CreatedAt      time.Time  `json:"createdAt,omitempty"`
	SyncedAt       *time.Time `json:"syncedAt,omitempty"`
	DeviceID       *string    `json:"deviceId,omitempty"`
	BootUUID       string     `json:"bootUuid,omitempty"`
	ID             string     `json:"uuid,omitempty"`
	TokenData      string     `json:"tokenData"`
	TokenValue     string     `json:"tokenValue"`
	TokenID        string     `json:"tokenId"`
	Type           string     `json:"type"`
	DBID           int64      `db:"DBID" json:"id"`
	MonotonicStart int64      `json:"monotonicStart,omitempty"`
	Success        bool       `json:"success"`
	ClockReliable  bool       `json:"clockReliable"`
	IsDeleted      bool       `json:"isDeleted,omitempty"`
}

type MediaHistoryEntry struct {
	StartTime      time.Time  `json:"startTime"`
	UpdatedAt      time.Time  `json:"updatedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt,omitempty"`
	EndTime        *time.Time `json:"endTime,omitempty"`
	SyncedAt       *time.Time `json:"syncedAt,omitempty"`
	DeviceID       *string    `json:"deviceId,omitempty"`
	BootUUID       string     `json:"bootUuid,omitempty"`
	ClockSource    string     `json:"clockSource,omitempty"`
	SystemID       string     `json:"systemId"`
	ID             string     `json:"uuid,omitempty"`
	LauncherID     string     `json:"launcherId"`
	SystemName     string     `json:"systemName"`
	MediaPath      string     `json:"mediaPath"`
	MediaName      string     `json:"mediaName"`
	DBID           int64      `db:"DBID" json:"id"`
	WallDuration   int        `json:"wallDuration"`
	DurationSec    int        `json:"durationSec"`
	MonotonicStart int64      `json:"monotonicStart,omitempty"`
	PlayTime       int        `json:"playTime"`
	TimeSkewFlag   bool       `json:"timeSkewFlag"`
	ClockReliable  bool       `json:"clockReliable"`
	IsDeleted      bool       `json:"isDeleted,omitempty"`
}

type MediaHistoryTopEntry struct {
	LastPlayedAt  time.Time
	SystemID      string
	SystemName    string
	MediaName     string
	MediaPath     string
	TotalPlayTime int
	SessionCount  int
}

type Mapping struct {
	Label    string `json:"label"`
	Type     string `json:"type"`
	Match    string `json:"match"`
	Pattern  string `json:"pattern"`
	Override string `json:"override"`
	DBID     int64
	Added    int64 `json:"added"`
	Enabled  bool  `json:"enabled"`
}

type InboxMessage struct {
	CreatedAt time.Time `json:"createdAt"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	Category  string    `json:"category,omitempty"`
	DBID      int64     `json:"id"`
	Severity  int       `json:"severity"`
	ProfileID int64     `json:"profileId"`
}

// Client represents a paired API client. AuthToken and PairingKey are
// hidden from JSON (API uses models.PairedClient instead).
type Client struct {
	ClientID   string `json:"clientId"`
	ClientName string `json:"clientName"`
	AuthToken  string `json:"-"`
	PairingKey []byte `json:"-"`
	DBID       int64  `json:"-"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
}

type System struct {
	SystemID string
	Name     string
	DBID     int64
}

type MediaTitle struct {
	Slug string
	Name string
	// DisambiguationTypes is the title's stored comma-separated set of tag types
	// whose values differ across its non-missing media (see RecomputeTitleDisambiguation).
	DisambiguationTypes string
	SecondarySlug       sql.NullString
	DBID                int64
	SystemDBID          int64
	SlugLength          int
	SlugWordCount       int
}

type Media struct {
	Path           string
	ParentDir      string
	SortName       string // write-once copy of MediaTitles.Name; titles never update so no propagation is needed
	DBID           int64
	MediaTitleDBID int64
	SystemDBID     int64
	IsMissing      bool
}

// MediaFullRow is the result of a joined query fetching a Media record together
// with its parent MediaTitle and System in a single round-trip.
type MediaFullRow struct {
	System System
	Media
	Title MediaTitle
}

// MediaUserData is the source-of-truth record for user-authored data about a
// single media path: whether it is a favourite and any per-game launcher
// override. It lives in UserDB (durable, power-loss safe) and is materialized
// into media.db's MediaTags/MediaProperties projection both on edit and on
// reindex. Keyed by (SystemID, Path) because a Media row's DBID is not stable
// across a full media.db rebuild. A row with IsFavorite false and an empty
// LauncherOverride carries no user intent and should be deleted rather than kept.
type MediaUserData struct {
	SystemID         string
	Path             string
	LauncherOverride string
	DBID             int64
	CreatedAt        int64
	UpdatedAt        int64
	IsFavorite       bool
}

// MediaPathID identifies a Media row by its system ID and path, used for batch
// media-ID resolution of API responses.
type MediaPathID struct {
	SystemID string
	Path     string
	DBID     int64
}

type TagType struct {
	Type        string
	DBID        int64
	IsExclusive bool
}

// MaxMediaPropertyBinaryBytes caps decoded binary property payloads hydrated
// into memory. Larger blobs remain addressable via BlobDBID/BlobSize but Binary
// is left nil so API handlers can return a controlled error instead of OOMing.
const MaxMediaPropertyBinaryBytes = 16 * 1024 * 1024

// ErrMediaBlobTooLarge indicates a blob exists but exceeds a caller-provided read cap.
var ErrMediaBlobTooLarge = errors.New("media blob too large")

// MediaProperty is a static content property attached to a MediaTitle or Media
// record. Properties are fetched for display, not filtered by value.
//
// For writes: set TypeTag to the full "type:value" string (e.g. "property:description").
// The database layer resolves the TypeTagDBID from TypeTag automatically.
// To associate binary data with a property, call UpsertMediaBlob first to obtain
// a BlobDBID, then set that field before upserting the property.
// For reads: TypeTag is populated from the joined Tags row; TypeTagDBID is also
// set. ContentType, BlobSize, and Binary are hydrated from the MediaBlobs JOIN
// and are read-only — do not set them for writes.
type MediaProperty struct {
	BlobDBID    *int64
	TypeTag     string
	Text        string
	ContentType string
	Binary      []byte
	TypeTagDBID int64
	BlobSize    int64
}

// MediaBlob is a row from the MediaBlobs content-addressed store.
// Data is identified by the hex-encoded SHA-256 of its framed content type and bytes.
type MediaBlob struct {
	Hash        string
	ContentType string
	Data        []byte
	DBID        int64
}

type Tag struct {
	Tag         string
	DisplayName string
	DBID        int64
	TypeDBID    int64
}

type MediaTag struct {
	DBID      int64
	MediaDBID int64
	TagDBID   int64
}

type MediaTagLink struct {
	MediaDBID int64
	TagDBID   int64
}

type SearchResult struct {
	SystemID string
	Name     string
	Path     string
	MediaID  int64
}

type TagInfo struct {
	Tag   string `json:"tag"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
	Count int64  `json:"count,omitempty"`
}

type BackupInfo struct {
	CreatedAt  time.Time `json:"createdAt"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	QuickCheck string    `json:"quickCheck"`
	Reason     string    `json:"reason,omitempty"`
	Size       int64     `json:"size"`
	Valid      bool      `json:"valid"`
	Manual     bool      `json:"manual"`
}

type RestoreInfo struct {
	PreRestoreBackup *BackupInfo `json:"preRestoreBackup,omitempty"`
	RestoredFrom     BackupInfo  `json:"restoredFrom"`
}

type WALCheckpointMode int

const (
	WALCheckpointAuto WALCheckpointMode = iota
	WALCheckpointSkip
	WALCheckpointForce
)

type TransactionOptions struct {
	WALCheckpoint WALCheckpointMode
}

// GroupTagFiltersByOperator groups tag filters by operator type for consistent processing.
// Returns (andFilters, notFilters, orFilters) to enable both SQL generation and in-memory filtering
// to use the same grouping logic.
func GroupTagFiltersByOperator(filters []zapscript.TagFilter) (and, not, or []zapscript.TagFilter) {
	for _, f := range filters {
		switch f.Operator {
		case zapscript.TagOperatorNOT:
			not = append(not, f)
		case zapscript.TagOperatorOR:
			or = append(or, f)
		default: // AND is the default
			and = append(and, f)
		}
	}
	return and, not, or
}

// BrowseDirectoryResult represents a subdirectory found during browse navigation.
type BrowseDirectoryResult struct {
	Name      string
	SystemIDs []string
	FileCount int
}

// SingletonContainerAlias is the resolved launch media for a child directory
// whose contents collapse to a single logical launch target.
type SingletonContainerAlias struct {
	ChildDir      string
	Tags          []TagInfo
	ZapScriptTags []TagInfo
	Row           MediaFullRow
	HasCover      bool
}

// SingletonAliasCandidate identifies a child directory to consider for
// singleton-container alias resolution. ChildDir must end with a trailing
// slash. FileCount is the recursive per-system media count for the directory
// (from the browse cache) — when it exceeds the number of direct media rows,
// the directory contains nested subdirectories and is not a singleton
// container.
type SingletonAliasCandidate struct {
	ChildDir  string
	FileCount int
}

// BrowseDirectoriesOptions contains parameters for the BrowseDirectories query.
// AfterName is the keyset cursor for directory pagination: only directories
// whose Name sorts strictly after it are returned (directory names are unique
// within a parent, so Name alone is a stable keyset). Limit caps the number of
// directories returned; 0 means no limit (full listing).
type BrowseDirectoriesOptions struct {
	PathPrefix string
	AfterName  string
	Systems    []systemdefs.System
	Limit      int
}

// BrowseDirCountOptions contains parameters for the BrowseDirCount query.
type BrowseDirCountOptions struct {
	PathPrefix string
	Systems    []systemdefs.System
}

// BrowseCursor holds the keyset pagination state for browse queries.
//
// media.browse pages directories first (ordered by Name), then files, under a
// single cursor. Phase selects which stream the cursor resumes: "dirs" uses
// DirName as the keyset, "files" (or empty, for legacy file-only cursors) uses
// SortValue/SortMode/LastID. TotalFiles and TotalDirs carry the first-page
// counts so cursor pages do not rerun the count queries.
type BrowseCursor struct {
	SortValue  string
	SortMode   string
	Phase      string
	DirName    string
	LastID     int64
	TotalFiles int
	TotalDirs  int
}

// BrowseFilesOptions contains parameters for the BrowseFiles query.
type BrowseFilesOptions struct {
	Cursor     *BrowseCursor
	Letter     *string
	PathPrefix string
	Sort       string
	Systems    []systemdefs.System
	Limit      int
}

// BrowseFileCountOptions contains parameters for the BrowseFileCount query.
type BrowseFileCountOptions struct {
	Letter     *string
	PathPrefix string
	Systems    []systemdefs.System
}

// BrowseIndexOptions contains parameters for the BrowseIndex facet query. It
// mirrors the scoping fields of BrowseFilesOptions so the index describes the
// exact list a media.browse call would return.
type BrowseIndexOptions struct {
	PathPrefix string
	Sort       string
	Systems    []systemdefs.System
}

// BrowseIndexBucket is one first-character bucket of a browse scope. SortValue
// and LastID are the keyset of the row immediately before the bucket's first
// row, so a media.browse cursor built from them lands a page on the bucket's
// first item. Offset is the bucket's 0-based position among the scope's files
// (its row number in the ordered query, so it can't drift from the browse
// order); it excludes leading directories, which the caller adds. AtStart is
// true for the bucket that begins the list (no preceding row), in which case
// the caller should produce an empty cursor.
type BrowseIndexBucket struct {
	Key       string
	SortValue string
	LastID    int64
	Count     int
	Offset    int
	AtStart   bool
}

// BrowseIndexResult is the ordered set of first-character buckets for a browse
// scope. Buckets are ordered to match the active sort. Scheme reports the
// collation used to derive the buckets ("latin"); it is "none" when the
// directory's effective sort is not alphabetical, in which case Buckets is
// empty and no rail applies. SortMode is the resolved browse sort mode the
// buckets were computed under and must be embedded into the seek cursors so the
// subsequent media.browse page continues in the same order.
type BrowseIndexResult struct {
	Scheme     string
	SortMode   string
	Buckets    []BrowseIndexBucket
	TotalFiles int
}

// BrowseVirtualScheme represents a virtual URI scheme with indexed content.
type BrowseVirtualScheme struct {
	Scheme    string
	SystemIDs []string
	FileCount int
}

// BrowseVirtualSchemesOptions contains parameters for BrowseVirtualSchemes.
type BrowseVirtualSchemesOptions struct {
	Systems []systemdefs.System
}

// BrowseRouteCountsOptions contains candidate route paths to resolve against
// indexed media for system-scoped browse root discovery.
type BrowseRouteCountsOptions struct {
	Systems []systemdefs.System
	Routes  []string
}

// BrowseRouteCount represents a populated browse route and its media count.
// CountUnknown is set when the route is known to contain media but the exact
// FileCount could not be computed within the deadline (degraded fallback);
// callers should treat such routes as present with an unknown count rather than
// as empty.
type BrowseRouteCount struct {
	Path         string
	SystemIDs    []string
	FileCount    int
	CountUnknown bool
}

// BrowseSystemRootCandidatesOptions parameterises the batched lookup used
// to build `media.browse({systems:[...], path:""})` candidates in two
// queries against the BrowseDirCounts cache.
type BrowseSystemRootCandidatesOptions struct {
	Roots   []string
	Systems []systemdefs.System
}

// BrowseSystemRootCandidates is the cache-backed result of resolving a list of
// filesystem roots against system-scoped browse data. Children holds the
// immediate subdirectory names of each root that contain media for the
// requested systems; HasMedia is true when a root has any media in its
// subtree (even purely via descendants).
type BrowseSystemRootCandidates struct {
	Children map[string][]string
	HasMedia map[string]bool
}

type SearchResultWithCursor struct {
	SystemID string
	Name     string
	Path     string
	// DisambiguationTypes is the title's stored comma-separated set of tag types
	// that distinguish its variants (see RecomputeTitleDisambiguation). Empty for
	// titles with no variants, which lets the read path skip the tag lookup.
	DisambiguationTypes string
	SortValue           string
	SortMode            string
	Tags                []TagInfo
	ZapScriptTags       []TagInfo
	MediaID             int64
	MediaTitleID        int64 `json:"-"`
	HasCover            bool
}

// TagTypeDisplayPriority orders the eligible disambiguation tag types from most to least
// important for display. Clients render the emitted disambiguating tags left-to-right and
// truncate when space runs out, so the most decisive distinctions come first: variant
// flags (beta/proto/hack) before region, then the specific-variant markers, then extra
// context. A tag type only appears on an entry when it actually differs across the title's
// siblings, so a sole differentiator always survives truncation regardless of its rank.
// Rank is the slice index.
var TagTypeDisplayPriority = []string{
	"unfinished", "unlicensed", "region", "video", "disc", "disctotal", "edition",
	"rev", "arcadeboard", "cabinet", "protection", "set", "input", "dump", "alt", "compatibility", "builddate",
	"lang", "distribution", "media", "addon", "release", "year",
	"players", "developer", "publisher", "copyright", "credit",
	"track",
}

// ZapScriptTagTypes is the allowlist of tag types eligible for sibling disambiguation:
// only these types are considered when deciding whether a title's media differ. It is the
// same set as TagTypeDisplayPriority (order is irrelevant here, used only for SQL
// membership), so it aliases the priority list to keep the two in sync. "unknown" is
// deliberately absent — unclassified tokens never disambiguate.
var ZapScriptTagTypes = TagTypeDisplayPriority

// TagTypeDisplayRank returns the display-importance rank of a tag type (lower is more
// important). Unknown types sort last. Used to order emitted disambiguating tags.
func TagTypeDisplayRank(tagType string) int {
	for i, t := range TagTypeDisplayPriority {
		if t == tagType {
			return i
		}
	}
	return len(TagTypeDisplayPriority)
}

// isFourDigitYear reports whether s is exactly four ASCII digits, the only form
// accepted for a year value in a ZapScript title command.
func isFourDigitYear(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// BuildTitleZapScript builds a ZapScript title command string from a system ID,
// media name, and disambiguating tags. Format: @SystemID/Name (year:YYYY) (type:value)
// Multiple values of the same type are grouped into one parens as a comma-separated
// shorthand: (region:eu, region:us). Types are emitted in the order they first appear in
// the input (callers pass tags pre-sorted by display priority). Only non-empty tags are
// included; year values must be exactly 4 digits.
func BuildTitleZapScript(systemID, name string, tags []TagInfo) string {
	var sb strings.Builder
	_, _ = sb.WriteString("@" + systemID + "/" + name)

	typeOrder := make([]string, 0, len(tags))
	valuesByType := make(map[string][]string, len(tags))
	for _, tag := range tags {
		if tag.Tag == "" {
			continue
		}
		if tag.Type == "year" && !isFourDigitYear(tag.Tag) {
			continue
		}
		if _, seen := valuesByType[tag.Type]; !seen {
			typeOrder = append(typeOrder, tag.Type)
		}
		valuesByType[tag.Type] = append(valuesByType[tag.Type], tag.Tag)
	}

	for _, tagType := range typeOrder {
		values := valuesByType[tagType]
		if len(values) == 0 {
			continue
		}
		_, _ = sb.WriteString(" (")
		for k, v := range values {
			if k > 0 {
				_, _ = sb.WriteString(", ")
			}
			_, _ = sb.WriteString(tagType + ":" + v)
		}
		_, _ = sb.WriteString(")")
	}
	return sb.String()
}

// ZapScript returns the ZapScript title command string for this search result.
// Uses ZapScriptTags (disambiguating tags only). If ZapScriptTags has not been
// populated (nil), no tags are emitted — callers that need disambiguation must
// run the result through attachZapScriptTags, which reads the title's stored
// DisambiguationTypes (see RecomputeTitleDisambiguation).
func (r *SearchResultWithCursor) ZapScript() string {
	return BuildTitleZapScript(r.SystemID, r.Name, r.ZapScriptTags)
}

// TitleWithSystem represents a MediaTitle with its associated System information
type TitleWithSystem struct {
	Slug       string
	Name       string
	SystemID   string
	DBID       int64
	SystemDBID int64
}

// MediaWithFullPath represents a Media item with its associated title and system information.
type MediaWithFullPath struct {
	Path           string
	ParentDir      string
	TitleSlug      string
	SystemID       string
	SortName       string
	DBID           int64
	MediaTitleDBID int64
	IsMissing      bool
}

// ScrapeWrite is the database-level write payload produced by a scraper for a
// matched Media row. Sentinel is written after all metadata so interrupted runs
// can safely retry the record.
type ScrapeWrite struct {
	Sentinel   TagInfo
	MediaTags  []TagInfo
	TitleTags  []TagInfo
	TitleProps []MediaProperty
	MediaProps []MediaProperty
}

// ScrapeWriteTarget pairs a scraper write payload with the existing Media and
// MediaTitle rows it should enrich.
type ScrapeWriteTarget struct {
	Write          *ScrapeWrite
	MediaDBID      int64
	MediaTitleDBID int64
}

// ScrapeResultBatchApplier optionally batches scrape writes for DB
// implementations that can keep multiple targets in one transaction.
type ScrapeResultBatchApplier interface {
	ApplyScrapeResults(ctx context.Context, targets []ScrapeWriteTarget) error
}

type FileInfo struct {
	SystemID string
	Path     string
	Name     string
}

// MediaQuery represents parameters for querying media counts used in random selection
type MediaQuery struct {
	PathGlob   string                `json:"pathGlob,omitempty"`
	PathPrefix string                `json:"pathPrefix,omitempty"`
	Systems    []string              `json:"systems,omitempty"`
	Tags       []zapscript.TagFilter `json:"tags,omitempty"`
}

// SearchFilters represents parameters for filtered media search
type SearchFilters struct {
	Cursor  *int64                `json:"cursor,omitempty"`
	Letter  *string               `json:"letter,omitempty"`
	Query   string                `json:"query"`
	Systems []systemdefs.System   `json:"systems,omitempty"`
	Tags    []zapscript.TagFilter `json:"tags,omitempty"`
	Limit   int                   `json:"limit"`
}

// ScanStagedTag is one tag derived from a scanned file, staged for set-based
// reconcile. Value is the natural (unpadded) form; the DB layer applies
// tags.PadTagValue when writing the staging row.
type ScanStagedTag struct {
	Type  string
	Value string
}

// ScanStagedMedia is one scanned file's parsed fragments, staged into the
// ScanStage/ScanStageTags tables for set-based reconcile against the media
// tables. SecondarySlug is empty when the title has none.
type ScanStagedMedia struct {
	Path          string
	ParentDir     string
	Slug          string
	TitleName     string
	SortName      string
	SecondarySlug string
	Tags          []ScanStagedTag
	SlugLength    int
	SlugWordCount int
}

// ScanReconcileOpts adjusts how a staged-system reconcile treats the staged
// file set.
type ScanReconcileOpts struct {
	// IncompleteScan means file collection for this system hit errors (an
	// unreadable path, a failed launcher scanner), so the staged set may be a
	// subset of what actually exists. Staged files are still upserted and
	// re-found rows still clear their missing flag, but media absent from the
	// stage keep their current missing state instead of being flagged missing.
	IncompleteScan bool
}

// ScanReconcileStats reports what a staged-system reconcile changed. Counts are
// per-statement sqlite changes() values, for logging and tests.
type ScanReconcileStats struct {
	SystemDBID      int64
	TitlesInserted  int64
	TitlesRenamed   int64
	MediaUpserted   int64
	MediaMissing    int64
	TagsInserted    int64
	TagLinksAdded   int64
	TagLinksDeleted int64
	TouchedTitles   int64
	// SystemKnown is false when the system has no DB row and nothing was staged,
	// meaning the reconcile was a no-op and no Systems row was created.
	SystemKnown bool
}

// JournalMode represents SQLite journal mode
type JournalMode string

// Journal mode constants
const (
	JournalModeWAL    JournalMode = "WAL"
	JournalModeDELETE JournalMode = "DELETE"
)

// Interfaces for external deps

type GenericDBI interface {
	Open() error
	UnsafeGetSQLDb() *sql.DB
	Truncate() error
	Allocate() error
	MigrateUp() error
	Vacuum() error
	Close() error
	GetDBPath() string
}

type UserDBI interface {
	GenericDBI
	AddHistory(entry *HistoryEntry) error
	GetHistory(lastID int64) ([]HistoryEntry, error)
	CleanupHistory(retentionDays int) (int64, error)
	AddMediaHistory(entry *MediaHistoryEntry) (int64, error)
	UpdateMediaHistoryTime(dbid int64, playTime int) error
	CloseMediaHistory(dbid int64, endTime time.Time, playTime int) error
	GetMediaHistory(systemIDs []string, lastID int64, limit int) ([]MediaHistoryEntry, error)
	GetLatestMediaHistory() (MediaHistoryEntry, bool, error)
	GetMediaHistoryTop(systemIDs []string, since *time.Time, limit int) ([]MediaHistoryTopEntry, error)
	CloseHangingMediaHistory() error
	CleanupMediaHistory(retentionDays int) (int64, error)
	HealTimestamps(bootUUID string, trueBootTime time.Time) (int64, error)
	SumMediaPlayTimeForDay(dayStart time.Time) (int64, error)
	AddMapping(m *Mapping) error
	GetMapping(id int64) (Mapping, error)
	DeleteMapping(id int64) error
	UpdateMapping(id int64, m *Mapping) error
	GetAllMappings() ([]Mapping, error)
	GetEnabledMappings() ([]Mapping, error)
	GetMediaUserData(systemID, path string) (MediaUserData, bool, error)
	SetMediaUserFavorite(systemID, path string, favorite bool) error
	SetMediaUserLauncherOverride(systemID, path, launcherID string) error
	UpsertMediaUserData(data *MediaUserData) error
	DeleteMediaUserData(systemID, path string) error
	ListMediaUserData() ([]MediaUserData, error)
	UpdateZapLinkHost(host string, zapscript int) error
	GetZapLinkHost(host string) (bool, bool, error)
	GetSupportedZapLinkHosts() ([]string, error)
	PruneExpiredZapLinkHosts(olderThan time.Duration) (int64, error)
	UpdateZapLinkCache(url string, zapscript string) error
	GetZapLinkCache(url string) (string, error)
	AddInboxMessage(msg *InboxMessage) (*InboxMessage, error)
	GetInboxMessages() ([]InboxMessage, error)
	DeleteInboxMessage(id int64) error
	DeleteAllInboxMessages() (int64, error)
	CreateClient(c *Client) error
	GetClientByToken(authToken string) (*Client, error)
	ListClients() ([]Client, error)
	DeleteClient(clientID string) error
	UpdateClientLastSeen(authToken string, lastSeenAt int64) error
	CountClients() (int, error)
	Backup(reason string, manual bool) (BackupInfo, error)
	EnsureRecentBackup(maxAge time.Duration) (BackupInfo, bool, error)
	ListBackups() ([]BackupInfo, error)
	RestoreBackup(name string) (RestoreInfo, error)
	IntegrityReport() []string
	MarkCorrupt(reason string)
	IsMarkedCorrupt() bool
	ClearCorruptMarker() error
	NoteCorruption(err error) bool
	RecoverFromCorruption() (RestoreInfo, error)
}

type MediaDBI interface {
	GenericDBI
	BeginTransaction(batchEnabled bool) error
	CommitTransaction() error
	CommitTransactionWithOptions(options TransactionOptions) error
	FlushBatchInserters() error
	RollbackTransaction() error
	Exists() bool
	UpdateLastGenerated() error
	GetLastGenerated() (time.Time, error)

	SetOptimizationStatus(status string) error
	GetOptimizationStatus() (string, error)
	SetOptimizationStep(step string) error
	GetOptimizationStep() (string, error)
	IsOptimizing() bool
	BeginBrowseCacheRebuild()
	EndBrowseCacheRebuild()
	RunBackgroundOptimization(statusCallback func(optimizing bool), pauser *syncutil.Pauser)
	WaitForBackgroundOperations()
	TrackBackgroundOperation()
	BackgroundOperationDone()

	InvalidateCountCache() error
	RebuildSlugSearchCache() error
	RebuildTagCache() error
	WALCheckpoint() error
	QuickCheck() (bool, error)
	IntegrityReport() []string
	MarkCorrupt(reason string)
	IsMarkedCorrupt() bool
	ClearCorruptMarker() error
	NoteCorruption(err error) bool
	Recreate(keepBackup bool) error

	// On-disk persistence for the rebuilt caches. Persist* writes the
	// current in-memory cache atomically; LoadCached* reads it back at
	// startup and returns (false, nil) on missing/stale/version-mismatch
	// so the caller can fall through to a SQL rebuild.
	PersistTagCache() error
	LoadCachedTagCache() (bool, error)
	PersistSlugSearchCache() error
	LoadCachedSlugSearchCache() (bool, error)

	// IndexGeneration is bumped at the end of every successful indexing
	// run. Persisted cache files embed the value they were built against
	// so a stale cache from a previous run is rejected on next load.
	IndexGeneration() (int64, error)
	BumpIndexGeneration() (int64, error)

	// Slug resolution cache methods
	GetCachedSlugResolution(
		ctx context.Context, systemID, slug string, tagFilters []zapscript.TagFilter,
	) (int64, string, bool)
	SetCachedSlugResolution(
		ctx context.Context, systemID, slug string, tagFilters []zapscript.TagFilter, mediaDBID int64, strategy string,
	) error
	InvalidateSlugCache(ctx context.Context) error
	InvalidateSlugCacheForSystems(ctx context.Context, systemIDs []string) error
	GetMediaByDBID(ctx context.Context, mediaDBID int64) (SearchResultWithCursor, error)
	GetZapScriptTagsBySystemAndPath(ctx context.Context, systemID, path string) ([]TagInfo, error)

	SetIndexingCacheSize(enable bool)
	DropSecondaryIndexes() error
	CreateSecondaryIndexes() error
	SetIndexingStatus(status string) error
	GetIndexingStatus() (string, error)
	GetIndexResumeAttempts() (int, error)
	IncrementIndexResumeAttempts() (int, error)
	ResetIndexResumeAttempts() error
	SetScrapingStatus(status string) error
	GetScrapingStatus() (string, error)
	SetScrapingOperation(operation ScrapingOperation) error
	GetScrapingOperation() (ScrapingOperation, bool, error)
	ClearScrapingOperation() error
	SetLastIndexedSystem(systemID string) error
	GetLastIndexedSystem() (string, error)
	SetIndexingSystems(systemIDs []string) error
	GetIndexingSystems() ([]string, error)
	TruncateSystems(systemIDs []string) error

	// Scanner staging: files are streamed into staging tables inside the open
	// batch transaction, then folded into the media tables with set-based SQL
	// so indexing memory does not scale with database size.
	StageScannedMedia(media *ScanStagedMedia) error
	ReconcileStagedSystem(ctx context.Context, systemID string, opts ScanReconcileOpts) (ScanReconcileStats, error)
	ClearScanStage() error
	SeedCanonicalTagDefinitions(ctx context.Context) error

	SearchMediaPathExact(ctx context.Context, systems []systemdefs.System, query string) ([]SearchResult, error)
	SearchMediaWithFilters(ctx context.Context, filters *SearchFilters) ([]SearchResultWithCursor, error)
	SearchMediaBySlug(
		ctx context.Context, systemID string, slug string, tags []zapscript.TagFilter,
	) ([]SearchResultWithCursor, error)
	SearchMediaBySecondarySlug(
		ctx context.Context, systemID string, secondarySlug string, tags []zapscript.TagFilter,
	) ([]SearchResultWithCursor, error)
	SearchMediaBySlugPrefix(
		ctx context.Context, systemID string, slugPrefix string, tags []zapscript.TagFilter,
	) ([]SearchResultWithCursor, error)
	SearchMediaBySlugIn(
		ctx context.Context, systemID string, slugs []string, tags []zapscript.TagFilter,
	) ([]SearchResultWithCursor, error)
	GetTitlesWithPreFilter(
		ctx context.Context, systemID string, minLength, maxLength, minWordCount, maxWordCount int,
	) ([]MediaTitle, error)
	GetLaunchCommandForMedia(ctx context.Context, systemID, path string) (string, error)
	GetTags(ctx context.Context, systems []systemdefs.System) ([]TagInfo, error)
	GetAllUsedTags(ctx context.Context) ([]TagInfo, error)
	PopulateSystemTagsCache(ctx context.Context) error
	PopulateSystemTagsCacheForSystems(ctx context.Context, systems []systemdefs.System) error
	AnalyzeApproximate() error
	RefreshSlugSearchCacheForSystems(ctx context.Context, systemIDs []string) error
	GetSystemTagsCached(ctx context.Context, systems []systemdefs.System) ([]TagInfo, error)
	InvalidateSystemTagsCache(ctx context.Context, systems []systemdefs.System) error
	SearchMediaPathGlob(systems []systemdefs.System, query string) ([]SearchResult, error)

	// Browse methods for directory-style navigation of indexed content
	BrowseDirectories(ctx context.Context, opts BrowseDirectoriesOptions) ([]BrowseDirectoryResult, error)
	BrowseDirCount(ctx context.Context, opts BrowseDirCountOptions) (int, error)
	BrowseFiles(ctx context.Context, opts *BrowseFilesOptions) ([]SearchResultWithCursor, error)
	BrowseFileCount(ctx context.Context, opts BrowseFileCountOptions) (int, error)
	BrowseIndex(ctx context.Context, opts BrowseIndexOptions) (BrowseIndexResult, error)
	BrowseVirtualSchemes(ctx context.Context, opts BrowseVirtualSchemesOptions) ([]BrowseVirtualScheme, error)
	BrowseRootCounts(ctx context.Context, rootDirs []string) (map[string]*int, error)
	BrowseRouteCounts(ctx context.Context, opts BrowseRouteCountsOptions) (map[string]BrowseRouteCount, error)
	BrowseSystemRootCandidates(
		ctx context.Context, opts BrowseSystemRootCandidatesOptions,
	) (result BrowseSystemRootCandidates, cacheReady bool, err error)
	PopulateBrowseCache(ctx context.Context) error
	BrowseCacheNeedsRebuild(ctx context.Context) (bool, error)

	IndexedSystems() ([]string, error)
	SystemIndexed(system *systemdefs.System) bool
	RandomGame(ctx context.Context, systems []systemdefs.System) (SearchResult, error)
	RandomGameWithQuery(ctx context.Context, query *MediaQuery) (SearchResult, error)
	GetTotalMediaCount() (int, error)
	GetMissingMediaCount() (int, error)
	GetScrapedMediaCount(ctx context.Context, scraperID string) (int, error)
	GetTotalScrapedMediaCount(ctx context.Context) (int, error)

	FindSystem(row System) (System, error)
	FindSystemBySystemID(systemID string) (System, error)
	InsertSystem(row System) (System, error)
	FindOrInsertSystem(row System) (System, error)

	FindMediaTitle(row *MediaTitle) (MediaTitle, error)
	InsertMediaTitle(row *MediaTitle) (MediaTitle, error)
	FindOrInsertMediaTitle(row *MediaTitle) (MediaTitle, error)

	FindMedia(row Media) (Media, error)
	InsertMedia(row Media) (Media, error)
	FindOrInsertMedia(row Media) (Media, error)
	DeleteMediaTag(mediaDBID, tagDBID int64) error
	TemporaryRepairJobsPending(ctx context.Context) (bool, error)

	FindTagType(row TagType) (TagType, error)
	InsertTagType(row TagType) (TagType, error)
	FindOrInsertTagType(row TagType) (TagType, error)

	FindTag(row Tag) (Tag, error)
	InsertTag(row Tag) (Tag, error)
	FindOrInsertTag(row Tag) (Tag, error)

	FindMediaTag(row MediaTag) (MediaTag, error)
	InsertMediaTag(row MediaTag) (MediaTag, error)
	FindOrInsertMediaTag(row MediaTag) (MediaTag, error)

	// CleanMediaOrphans removes Media rows where IsMissing=1 together with
	// their associated MediaTags and MediaProperties.  MediaTitles that are
	// no longer referenced by any Media row are also removed (including their
	// MediaTitleTags and MediaTitleProperties).  Tags unreferenced by any join
	// table are pruned.  A VACUUM is issued on success.
	//
	// Returns the count of Media rows deleted, or one of ErrIndexingInProgress,
	// ErrOptimizationInProgress, or ErrTransactionActive when the operation
	// cannot safely run.
	CleanMediaOrphans(ctx context.Context) (int64, error)

	GetAllSystems() ([]System, error)
	// GetExistingMediaUserData returns user-authored data (favourites, launcher
	// overrides) already stored in media.db, for the one-time UserDB backfill.
	GetExistingMediaUserData(ctx context.Context) ([]MediaUserData, error)

	// Per-system query methods for scrapers
	GetTitlesBySystemID(systemID string) ([]TitleWithSystem, error)
	GetMediaBySystemID(systemID string) ([]MediaWithFullPath, error)

	// Scraper support methods

	// FindMediaBySystemAndPath returns the Media row matching systemDBID and path,
	// or nil, nil when no row is found.
	FindMediaBySystemAndPath(ctx context.Context, systemDBID int64, path string) (*Media, error)
	FindMediaBySystemAndPaths(ctx context.Context, systemDBID int64, paths []string) (map[string]Media, error)
	// FindMediaIDsByPaths returns the system ID, path, and DBID of every Media
	// row whose Path is in paths, in a single query across all systems.
	FindMediaIDsByPaths(ctx context.Context, paths []string) ([]MediaPathID, error)
	// FindSingleContainerLaunchMedia returns the one logical launch target in the
	// direct contents of containerPath for systemDBID, or nil, nil when the
	// container is empty, nested-only, or ambiguous.
	FindSingleContainerLaunchMedia(ctx context.Context, systemDBID int64, containerPath string) (*Media, error)
	// ResolveSingletonContainerAliases resolves the given candidate child
	// directories for systemDBID in a single batch query, returning one
	// SingletonContainerAlias per candidate that collapses to a single launch
	// target. Candidates with nested subdirs (recursive FileCount exceeding
	// their direct media rows) or ambiguous contents are omitted.
	// ZapScriptTags are populated via in-memory disambiguation (same approach
	// as the search path) and will be empty for unambiguous titles.
	ResolveSingletonContainerAliases(
		ctx context.Context, systemDBID int64, candidates []SingletonAliasCandidate,
	) ([]SingletonContainerAlias, error)

	// FindMediaBySystemAndPathFold returns the Media row matching systemDBID and
	// path using a case-insensitive path comparison, or nil, nil when no row is
	// found. Intended for scrapers where the incoming path casing may differ from
	// what the indexer recorded (e.g. Windows filesystem with mixed-case system
	// directory names).
	FindMediaBySystemAndPathFold(ctx context.Context, systemDBID int64, path string) (*Media, error)

	// FindMediaBySystemAndPathSuffix returns all Media rows for the given system
	// whose Path ends with "/" + filename. Used when companion XML child paths are
	// root-relative (./file.rom) and the indexed path may be in any subdirectory.
	FindMediaBySystemAndPathSuffix(ctx context.Context, systemDBID int64, filename string) ([]Media, error)

	// MediaHasTag returns true when the Media row has a tag whose full string
	// (type:value) equals tagValue.
	MediaHasTag(ctx context.Context, mediaDBID int64, tagValue string) (bool, error)

	// GetScrapedMediaIDs returns media DBIDs in a system already marked as scraped
	// by scraperID.
	GetScrapedMediaIDs(ctx context.Context, scraperID string, systemDBID int64) (map[int64]struct{}, error)

	// GetScrapeRunMediaIDs returns media DBIDs in a system completed during a
	// specific scraper run.
	GetScrapeRunMediaIDs(ctx context.Context, scraperID, runID string, systemDBID int64) (map[int64]struct{}, error)

	// ClearScrapeRunMarkers removes per-run completion markers after a scraper
	// operation reaches a terminal state.
	ClearScrapeRunMarkers(ctx context.Context, scraperID, runID string) error

	// UpsertMediaTags writes tags to MediaTags for a specific Media row.
	// Exclusive types (TagTypes.IsExclusive=1) delete existing tags of that type
	// for the entity before inserting; additive types use INSERT OR IGNORE.
	UpsertMediaTags(ctx context.Context, mediaDBID int64, tags []TagInfo) error

	// UpsertMediaTitleTags writes tags to MediaTitleTags for a specific MediaTitle row.
	// Exclusive/additive behaviour is identical to UpsertMediaTags.
	UpsertMediaTitleTags(ctx context.Context, mediaTitleDBID int64, tags []TagInfo) error

	// RecomputeTitleDisambiguation recomputes the stored disambiguating tag types
	// for the given MediaTitle DBIDs. Called after writes that change a title's
	// media or tags so reads can rely on the stored, title-global value.
	RecomputeTitleDisambiguation(ctx context.Context, titleDBIDs []int64) error

	// RecomputeSystemDisambiguation recomputes the stored disambiguating tag types
	// for every MediaTitle belonging to the given system DBIDs. Used at index time.
	RecomputeSystemDisambiguation(ctx context.Context, systemDBIDs []int64) error

	// UpsertMediaTitleProperties upserts properties into MediaTitleProperties.
	// Conflicts on (MediaTitleDBID, TypeTagDBID) update data columns; DBID is preserved.
	UpsertMediaTitleProperties(ctx context.Context, mediaTitleDBID int64, props []MediaProperty) error

	// UpsertMediaProperties upserts properties into MediaProperties.
	// Conflicts on (MediaDBID, TypeTagDBID) update data columns; DBID is preserved.
	UpsertMediaProperties(ctx context.Context, mediaDBID int64, props []MediaProperty) error

	// ApplyScrapeResult atomically writes all scraper metadata for a Media row and
	// writes the sentinel tag last.
	ApplyScrapeResult(ctx context.Context, mediaDBID, mediaTitleDBID int64, write *ScrapeWrite) error

	// FindMediaTitlesWithoutSentinel returns MediaTitle rows for the given system
	// that have no Media row with the given sentinel tag value.
	FindMediaTitlesWithoutSentinel(ctx context.Context, systemDBID int64, sentinelTag string) ([]MediaTitle, error)

	// FindMediaTitleByDBID returns the MediaTitle with the given DBID,
	// or nil, nil when no row is found.
	FindMediaTitleByDBID(ctx context.Context, dbid int64) (*MediaTitle, error)

	// FindMediaTitleBySystemAndSlug returns the MediaTitle matching systemDBID and
	// slug, or nil, nil when no row is found.
	FindMediaTitleBySystemAndSlug(ctx context.Context, systemDBID int64, slug string) (*MediaTitle, error)

	// GetMediaTitleProperties returns all properties for a MediaTitle row,
	// with TypeTagDBID resolved to the tag value string. Binary blobs are capped
	// at MaxMediaPropertyBinaryBytes; larger blobs populate BlobSize but not Binary.
	GetMediaTitleProperties(ctx context.Context, mediaTitleDBID int64) ([]MediaProperty, error)
	GetMediaTitlePropertiesByMediaTitleDBIDs(
		ctx context.Context, mediaTitleDBIDs []int64,
	) (map[int64][]MediaProperty, error)
	GetMediaTitlePropertyMetadata(ctx context.Context, mediaTitleDBID int64) ([]MediaProperty, error)
	GetMediaTitlePropertyMetadataByMediaTitleDBIDs(
		ctx context.Context, mediaTitleDBIDs []int64,
	) (map[int64][]MediaProperty, error)

	// GetMediaProperties returns all properties for a Media row,
	// with TypeTagDBID resolved to the tag value string. Binary blobs are capped
	// at MaxMediaPropertyBinaryBytes; larger blobs populate BlobSize but not Binary.
	GetMediaProperties(ctx context.Context, mediaDBID int64) ([]MediaProperty, error)
	GetMediaPropertiesByMediaDBIDs(ctx context.Context, mediaDBIDs []int64) (map[int64][]MediaProperty, error)
	GetMediaPropertyMetadata(ctx context.Context, mediaDBID int64) ([]MediaProperty, error)
	GetMediaPropertyMetadataByMediaDBIDs(ctx context.Context, mediaDBIDs []int64) (map[int64][]MediaProperty, error)

	// DeleteMediaTitleProperty removes a single property row from MediaTitleProperties
	// identified by (mediaTitleDBID, typeTagDBID). A no-op if the row does not exist.
	DeleteMediaTitleProperty(ctx context.Context, mediaTitleDBID int64, typeTagDBID int64) error

	// DeleteMediaProperty removes a single property row from MediaProperties
	// identified by (mediaDBID, typeTagDBID). A no-op if the row does not exist.
	DeleteMediaProperty(ctx context.Context, mediaDBID int64, typeTagDBID int64) error

	// GetMediaWithTitleAndSystem fetches a Media record together with its parent
	// MediaTitle and System via a single JOIN query. Returns nil, nil when no
	// Media row with the given DBID exists. IsMissing is NOT filtered — metadata
	// remains accessible for missing files.
	GetMediaWithTitleAndSystem(ctx context.Context, mediaDBID int64) (*MediaFullRow, error)
	GetMediaWithTitleAndSystemByIDs(ctx context.Context, mediaDBIDs []int64) (map[int64]MediaFullRow, error)

	// GetMediaTagsByMediaDBID returns the file-level tags (MediaTags) for a
	// single Media row. Does not include title-level tags.
	GetMediaTagsByMediaDBID(ctx context.Context, mediaDBID int64) ([]TagInfo, error)
	GetMediaTagsByMediaDBIDs(ctx context.Context, mediaDBIDs []int64) (map[int64][]TagInfo, error)

	// GetMediaTitleTagsByMediaTitleDBID returns the title-level tags
	// (MediaTitleTags) for a single MediaTitle row.
	GetMediaTitleTagsByMediaTitleDBID(ctx context.Context, mediaTitleDBID int64) ([]TagInfo, error)
	GetMediaTitleTagsByMediaTitleDBIDs(ctx context.Context, mediaTitleDBIDs []int64) (map[int64][]TagInfo, error)

	// UpsertMediaBlob inserts a blob into MediaBlobs when no row with the same
	// SHA-256 hash of framed content type and bytes already exists, then returns its DBID.
	// Hash computation is performed internally; callers supply only contentType and raw data.
	UpsertMediaBlob(ctx context.Context, contentType string, data []byte) (int64, error)

	// GetMediaBlob returns the MediaBlob row for the given DBID,
	// or nil, nil when not found.
	GetMediaBlob(ctx context.Context, blobDBID int64) (*MediaBlob, error)
	GetMediaBlobDataCapped(ctx context.Context, blobDBID int64, maxBytes int64) ([]byte, string, error)

	// PruneOrphanedBlobs deletes MediaBlobs rows that are not referenced by
	// any MediaTitleProperties or MediaProperties row. Returns the count of
	// rows deleted. Safe to call from CleanMediaOrphans.
	PruneOrphanedBlobs(ctx context.Context) (int64, error)
}
