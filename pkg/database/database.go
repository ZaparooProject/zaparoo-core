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

package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

/*
 * In attempting to correct circular import deps, these non-concrete
 * interfaces were moves to this generic package level.
 * Actual implementations found in userdb/mediadb
 */

// Database is a portable interface for ENV bindings
type Database struct {
	UserDB  UserDBI
	MediaDB MediaDBI
}

/*
 * Structs for SQL records
 */

type HistoryEntry struct {
	Time       time.Time `json:"time"`
	Type       string    `json:"type"`
	TokenID    string    `json:"tokenId"`
	TokenValue string    `json:"tokenValue"`
	TokenData  string    `json:"tokenData"`
	DBID       int64     `db:"DBID" json:"id"`
	Success    bool      `json:"success"`
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

type System struct {
	SystemID string
	Name     string
	DBID     int64
}

type MediaTitle struct {
	Slug       string
	Name       string
	DBID       int64
	SystemDBID int64
}

type Media struct {
	Path           string
	DBID           int64
	MediaTitleDBID int64
	SystemDBID     int64
}

type TagType struct {
	Type string
	DBID int64
}

type Tag struct {
	Tag      string
	DBID     int64
	TypeDBID int64
}

type MediaTag struct {
	DBID      int64
	MediaDBID int64
	TagDBID   int64
}

type SearchResult struct {
	SystemID string
	Name     string
	Path     string
}

type TagInfo struct {
	Tag  string `json:"tag"`
	Type string `json:"type"`
}

// TagOperator represents the logical operator for tag filtering
type TagOperator string

// Tag filter operator constants
const (
	TagOperatorAND TagOperator = "AND" // Default: must have tag
	TagOperatorNOT TagOperator = "NOT" // Must not have tag (-)
	TagOperatorOR  TagOperator = "OR"  // At least one OR tag must match (~)
)

// TagFilter represents a tag type/value filter for queries
type TagFilter struct {
	Type     string      // Tag type (e.g., "lang", "year", "players")
	Value    string      // Tag value (e.g., "en", "1991", "2")
	Operator TagOperator // Operator: AND (default), NOT (-), OR (~)
}

// GroupTagFiltersByOperator groups tag filters by operator type for consistent processing.
// Returns (andFilters, notFilters, orFilters) to enable both SQL generation and in-memory filtering
// to use the same grouping logic.
func GroupTagFiltersByOperator(filters []TagFilter) (and, not, or []TagFilter) {
	for _, f := range filters {
		switch f.Operator {
		case TagOperatorNOT:
			not = append(not, f)
		case TagOperatorOR:
			or = append(or, f)
		default: // AND is the default
			and = append(and, f)
		}
	}
	return and, not, or
}

type SearchResultWithCursor struct {
	SystemID string
	Name     string
	Path     string
	Tags     []TagInfo
	MediaID  int64
}

// TitleWithSystem represents a MediaTitle with its associated System information
type TitleWithSystem struct {
	Slug       string
	Name       string
	SystemID   string
	DBID       int64
	SystemDBID int64
}

// MediaWithFullPath represents a Media item with its associated title and system information
type MediaWithFullPath struct {
	Path           string
	TitleSlug      string
	SystemID       string
	DBID           int64
	MediaTitleDBID int64
}

type FileInfo struct {
	SystemID string
	Path     string
	Name     string
}

// MediaQuery represents parameters for querying media counts used in random selection
type MediaQuery struct {
	PathGlob   string      `json:"pathGlob,omitempty"`
	PathPrefix string      `json:"pathPrefix,omitempty"`
	Systems    []string    `json:"systems,omitempty"`
	Tags       []TagFilter `json:"tags,omitempty"`
}

// SearchFilters represents parameters for filtered media search
type SearchFilters struct {
	Cursor  *int64              `json:"cursor,omitempty"`
	Letter  *string             `json:"letter,omitempty"`
	Query   string              `json:"query"`
	Systems []systemdefs.System `json:"systems,omitempty"`
	Tags    []TagFilter         `json:"tags,omitempty"`
	Limit   int                 `json:"limit"`
}

type ScanState struct {
	SystemIDs     map[string]int
	TitleIDs      map[string]int
	MediaIDs      map[string]int
	TagTypeIDs    map[string]int
	TagIDs        map[string]int
	SystemsIndex  int
	TitlesIndex   int
	MediaIndex    int
	TagTypesIndex int
	TagsIndex     int
}

// JournalMode represents SQLite journal mode
type JournalMode string

// Journal mode constants
const (
	JournalModeWAL    JournalMode = "WAL"
	JournalModeDELETE JournalMode = "DELETE"
)

/*
 * Interfaces for external deps
 */

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
	GetHistory(lastID int) ([]HistoryEntry, error)
	AddMapping(m *Mapping) error
	GetMapping(id int64) (Mapping, error)
	DeleteMapping(id int64) error
	UpdateMapping(id int64, m *Mapping) error
	GetAllMappings() ([]Mapping, error)
	GetEnabledMappings() ([]Mapping, error)
	UpdateZapLinkHost(host string, zapscript int) error
	GetZapLinkHost(host string) (bool, bool, error)
	UpdateZapLinkCache(url string, zapscript string) error
	GetZapLinkCache(url string) (string, error)
}

type MediaDBI interface {
	GenericDBI
	BeginTransaction(batchEnabled bool) error
	CommitTransaction() error
	RollbackTransaction() error
	Exists() bool
	UpdateLastGenerated() error
	GetLastGenerated() (time.Time, error)

	SetOptimizationStatus(status string) error
	GetOptimizationStatus() (string, error)
	SetOptimizationStep(step string) error
	GetOptimizationStep() (string, error)
	RunBackgroundOptimization(statusCallback func(optimizing bool))
	WaitForBackgroundOperations()

	InvalidateCountCache() error

	// Slug resolution cache methods
	GetCachedSlugResolution(
		ctx context.Context, systemID, slug string, tagFilters []TagFilter,
	) (int64, string, bool)
	SetCachedSlugResolution(
		ctx context.Context, systemID, slug string, tagFilters []TagFilter, mediaDBID int64, strategy string,
	) error
	InvalidateSlugCache(ctx context.Context) error
	InvalidateSlugCacheForSystems(ctx context.Context, systemIDs []string) error
	GetMediaByDBID(ctx context.Context, mediaDBID int64) (SearchResultWithCursor, error)

	SetIndexingStatus(status string) error
	GetIndexingStatus() (string, error)
	SetLastIndexedSystem(systemID string) error
	GetLastIndexedSystem() (string, error)
	SetIndexingSystems(systemIDs []string) error
	GetIndexingSystems() ([]string, error)
	TruncateSystems(systemIDs []string) error

	SearchMediaPathExact(systems []systemdefs.System, query string) ([]SearchResult, error)
	SearchMediaWithFilters(ctx context.Context, filters *SearchFilters) ([]SearchResultWithCursor, error)
	SearchMediaBySlug(
		ctx context.Context, systemID string, slug string, tags []TagFilter,
	) ([]SearchResultWithCursor, error)
	SearchMediaBySlugPrefix(
		ctx context.Context, systemID string, slugPrefix string, tags []TagFilter,
	) ([]SearchResultWithCursor, error)
	GetAllSlugsForSystem(ctx context.Context, systemID string) ([]string, error)
	GetTags(ctx context.Context, systems []systemdefs.System) ([]TagInfo, error)
	GetAllUsedTags(ctx context.Context) ([]TagInfo, error)
	PopulateSystemTagsCache(ctx context.Context) error
	GetSystemTagsCached(ctx context.Context, systems []systemdefs.System) ([]TagInfo, error)
	InvalidateSystemTagsCache(ctx context.Context, systems []systemdefs.System) error
	SearchMediaPathGlob(systems []systemdefs.System, query string) ([]SearchResult, error)
	IndexedSystems() ([]string, error)
	SystemIndexed(system *systemdefs.System) bool
	RandomGame(systems []systemdefs.System) (SearchResult, error)
	RandomGameWithQuery(query *MediaQuery) (SearchResult, error)
	GetTotalMediaCount() (int, error)

	FindSystem(row System) (System, error)
	FindSystemBySystemID(systemID string) (System, error)
	InsertSystem(row System) (System, error)
	FindOrInsertSystem(row System) (System, error)

	FindMediaTitle(row MediaTitle) (MediaTitle, error)
	InsertMediaTitle(row MediaTitle) (MediaTitle, error)
	FindOrInsertMediaTitle(row MediaTitle) (MediaTitle, error)

	FindMedia(row Media) (Media, error)
	InsertMedia(row Media) (Media, error)
	FindOrInsertMedia(row Media) (Media, error)

	FindTagType(row TagType) (TagType, error)
	InsertTagType(row TagType) (TagType, error)
	FindOrInsertTagType(row TagType) (TagType, error)

	FindTag(row Tag) (Tag, error)
	InsertTag(row Tag) (Tag, error)
	FindOrInsertTag(row Tag) (Tag, error)

	FindMediaTag(row MediaTag) (MediaTag, error)
	InsertMediaTag(row MediaTag) (MediaTag, error)
	FindOrInsertMediaTag(row MediaTag) (MediaTag, error)

	// GetMax*ID methods for resume functionality
	GetMaxSystemID() (int64, error)
	GetMaxTitleID() (int64, error)
	GetMaxMediaID() (int64, error)
	GetMaxTagTypeID() (int64, error)
	GetMaxTagID() (int64, error)
	GetMaxMediaTagID() (int64, error)

	// GetAll* methods for populating scan state maps
	GetAllSystems() ([]System, error)
	GetAllMediaTitles() ([]MediaTitle, error)
	GetAllMedia() ([]Media, error)
	GetAllTags() ([]Tag, error)
	GetAllTagTypes() ([]TagType, error)

	// Optimized JOIN query methods for populating scan state
	GetTitlesWithSystems() ([]TitleWithSystem, error)
	GetMediaWithFullPath() ([]MediaWithFullPath, error)

	// Optimized JOIN query methods for selective indexing (excluding specified systems)
	GetSystemsExcluding(excludeSystemIDs []string) ([]System, error)
	GetTitlesWithSystemsExcluding(excludeSystemIDs []string) ([]TitleWithSystem, error)
	GetMediaWithFullPathExcluding(excludeSystemIDs []string) ([]MediaWithFullPath, error)
}
