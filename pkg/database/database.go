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

type MediaTitleTag struct {
	DBID           int64
	MediaTitleDBID int64
	TagDBID        int64
}

type SearchResult struct {
	SystemID string
	Name     string
	Path     string
}

type FileInfo struct {
	SystemID string
	Path     string
	Name     string
}

type ScanState struct {
	SystemIDs      map[string]int
	TitleIDs       map[string]int
	MediaIDs       map[string]int
	TagTypeIDs     map[string]int
	TagIDs         map[string]int
	SystemsIndex   int
	TitlesIndex    int
	MediaIndex     int
	TagTypesIndex  int
	TagsIndex      int
	MediaTagsIndex int
}


type GameHashes struct {
	DBID       int64
	SystemID   string
	MediaPath  string
	ComputedAt time.Time
	FileSize   int64
	CRC32      string
	MD5        string
	SHA1       string
}

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
	BeginTransaction() error
	CommitTransaction() error
	RollbackTransaction() error
	Exists() bool
	UpdateLastGenerated() error
	GetLastGenerated() (time.Time, error)

	ReindexTables() error

	SearchMediaPathExact(systems []systemdefs.System, query string) ([]SearchResult, error)
	SearchMediaPathWords(systems []systemdefs.System, query string) ([]SearchResult, error)
	SearchMediaPathGlob(systems []systemdefs.System, query string) ([]SearchResult, error)
	IndexedSystems() ([]string, error)
	SystemIndexed(system systemdefs.System) bool
	RandomGame(systems []systemdefs.System) (SearchResult, error)

	FindSystem(row System) (System, error)
	InsertSystem(row System) (System, error)
	FindOrInsertSystem(row System) (System, error)
	GetSystemByID(systemDBID int64) (*System, error)

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

	FindMediaTitleTag(row MediaTitleTag) (MediaTitleTag, error)
	InsertMediaTitleTag(row MediaTitleTag) (MediaTitleTag, error)
	FindOrInsertMediaTitleTag(row MediaTitleTag) (MediaTitleTag, error)

	// Scraper metadata methods
	GetGamesWithoutMetadata(systemID string, limit int) ([]MediaTitle, error)
	GetMediaTitlesBySystem(systemID string) ([]MediaTitle, error)
	GetMediaByID(mediaDBID int64) (*Media, error)
	GetMediaTitleByID(mediaTitleDBID int64) (*MediaTitle, error)
	HasScraperMetadata(mediaTitleDBID int64) (bool, error)
	GetTagsForMediaTitle(mediaTitleDBID int64) (map[string]string, error)

	// Game hash methods
	SaveGameHashes(hashes *GameHashes) error
	GetGameHashes(systemID, mediaPath string) (*GameHashes, error)
	FindGameByHash(crc32, md5, sha1 string) ([]Media, error)
}
