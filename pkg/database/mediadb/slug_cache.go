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

package mediadb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// generateSlugCacheKey creates a consistent hash for a slug resolution query.
// The cache key is based on systemID, slug, and sorted tag filters to ensure
// deterministic caching regardless of input order.
func generateSlugCacheKey(systemID, slug string, tagFilters []database.TagFilter) (string, error) {
	// Normalize inputs to ensure consistent hashing
	type cacheKeyInput struct {
		SystemID string               `json:"systemId"`
		Slug     string               `json:"slug"`
		Tags     []database.TagFilter `json:"tags"`
	}

	normalized := cacheKeyInput{
		SystemID: strings.ToLower(strings.TrimSpace(systemID)),
		Slug:     strings.ToLower(strings.TrimSpace(slug)),
		Tags:     make([]database.TagFilter, len(tagFilters)),
	}

	// Copy and sort tags for consistent ordering (by Type, then Value, then Operator)
	copy(normalized.Tags, tagFilters)
	sort.Slice(normalized.Tags, func(i, j int) bool {
		if normalized.Tags[i].Type != normalized.Tags[j].Type {
			return normalized.Tags[i].Type < normalized.Tags[j].Type
		}
		if normalized.Tags[i].Value != normalized.Tags[j].Value {
			return normalized.Tags[i].Value < normalized.Tags[j].Value
		}
		return normalized.Tags[i].Operator < normalized.Tags[j].Operator
	})

	// Marshal to JSON with consistent ordering
	keyBytes, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("failed to marshal slug cache key: %w", err)
	}

	// Generate SHA256 hash
	hash := sha256.Sum256(keyBytes)
	return hex.EncodeToString(hash[:]), nil
}

// GetCachedSlugResolution retrieves a cached slug resolution result.
// Returns the MediaDBID, strategy name, and true if found; otherwise returns 0, "", false.
func (db *MediaDB) GetCachedSlugResolution(
	ctx context.Context, systemID, slug string, tagFilters []database.TagFilter,
) (mediaDBID int64, strategy string, found bool) {
	if db.sql == nil {
		return 0, "", false
	}

	cacheKey, err := generateSlugCacheKey(systemID, slug, tagFilters)
	if err != nil {
		log.Warn().Err(err).Msg("failed to generate slug cache key for lookup")
		return 0, "", false
	}

	err = db.sql.QueryRowContext(ctx,
		"SELECT MediaDBID, Strategy FROM SlugResolutionCache WHERE CacheKey = ?",
		cacheKey).Scan(&mediaDBID, &strategy)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", false
	}
	if err != nil {
		log.Warn().Err(err).Str("cacheKey", cacheKey).Msg("failed to get cached slug resolution")
		return 0, "", false
	}

	log.Debug().
		Str("system_id", systemID).
		Str("slug", slug).
		Int64("media_dbid", mediaDBID).
		Str("strategy", strategy).
		Msg("slug resolution cache hit")

	return mediaDBID, strategy, true
}

// SetCachedSlugResolution stores a successful slug resolution in the cache.
func (db *MediaDB) SetCachedSlugResolution(
	ctx context.Context, systemID, slug string, tagFilters []database.TagFilter, mediaDBID int64, strategy string,
) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	cacheKey, err := generateSlugCacheKey(systemID, slug, tagFilters)
	if err != nil {
		return fmt.Errorf("failed to generate slug cache key: %w", err)
	}

	tagFiltersJSON, err := json.Marshal(tagFilters)
	if err != nil {
		return fmt.Errorf("failed to marshal tag filters: %w", err)
	}

	_, err = db.sql.ExecContext(ctx, `
		INSERT OR REPLACE INTO SlugResolutionCache
		(CacheKey, SystemID, Slug, TagFilters, MediaDBID, Strategy, LastUpdated)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, cacheKey, systemID, slug, string(tagFiltersJSON), mediaDBID, strategy, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to cache slug resolution: %w", err)
	}

	log.Debug().
		Str("system_id", systemID).
		Str("slug", slug).
		Int64("media_dbid", mediaDBID).
		Str("strategy", strategy).
		Msg("cached slug resolution")

	return nil
}

// InvalidateSlugCache clears all cached slug resolutions.
// This should be called after any operation that changes the media database content.
func (db *MediaDB) InvalidateSlugCache(ctx context.Context) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	_, err := db.sql.ExecContext(ctx, "DELETE FROM SlugResolutionCache")
	if err != nil {
		return fmt.Errorf("failed to invalidate slug resolution cache: %w", err)
	}

	log.Debug().Msg("invalidated slug resolution cache")
	return nil
}

// InvalidateSlugCacheForSystems clears cached slug resolutions for specific systems.
// This is used during selective system reindexing to avoid invalidating the entire cache.
func (db *MediaDB) InvalidateSlugCacheForSystems(ctx context.Context, systemIDs []string) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	if len(systemIDs) == 0 {
		return nil // No-op for empty systems list
	}

	// Create placeholders for IN clause
	placeholders := prepareVariadic("?", ",", len(systemIDs))

	// Convert systemIDs to interface slice for query parameters
	args := make([]any, len(systemIDs))
	for i, id := range systemIDs {
		args[i] = id
	}

	//nolint:gosec // Safe: prepareVariadic only generates SQL placeholders like "?, ?, ?", no user data interpolated
	deleteStmt := fmt.Sprintf("DELETE FROM SlugResolutionCache WHERE SystemID IN (%s)", placeholders)
	_, err := db.sql.ExecContext(ctx, deleteStmt, args...)
	if err != nil {
		return fmt.Errorf("failed to invalidate slug cache for systems: %w", err)
	}

	log.Debug().Strs("system_ids", systemIDs).Msg("invalidated slug resolution cache for systems")
	return nil
}

// GetMediaByDBID retrieves a single SearchResultWithCursor by Media DBID.
// This is used to reconstruct the full result from a cached Media DBID.
func (db *MediaDB) GetMediaByDBID(ctx context.Context, mediaDBID int64) (database.SearchResultWithCursor, error) {
	if db.sql == nil {
		return database.SearchResultWithCursor{}, ErrNullSQL
	}

	result := database.SearchResultWithCursor{}

	// Query for media information
	err := db.sql.QueryRowContext(ctx, `
		SELECT
			Systems.SystemID,
			MediaTitles.Name,
			Media.Path,
			Media.DBID as MediaID
		FROM Media
		INNER JOIN MediaTitles ON Media.MediaTitleDBID = MediaTitles.DBID
		INNER JOIN Systems ON MediaTitles.SystemDBID = Systems.DBID
		WHERE Media.DBID = ?
	`, mediaDBID).Scan(
		&result.SystemID,
		&result.Name,
		&result.Path,
		&result.MediaID,
	)
	if err != nil {
		return result, fmt.Errorf("failed to get media by DBID: %w", err)
	}

	// Fetch tags for this media
	rows, err := db.sql.QueryContext(ctx, `
		SELECT
			Tags.Tag,
			TagTypes.Type
		FROM MediaTags
		INNER JOIN Tags ON MediaTags.TagDBID = Tags.DBID
		INNER JOIN TagTypes ON Tags.TypeDBID = TagTypes.DBID
		WHERE MediaTags.MediaDBID = ?
		ORDER BY TagTypes.Type, Tags.Tag
	`, mediaDBID)
	if err != nil {
		return result, fmt.Errorf("failed to query tags: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close tags rows")
		}
	}()

	result.Tags = make([]database.TagInfo, 0)
	for rows.Next() {
		var tag database.TagInfo
		if scanErr := rows.Scan(&tag.Tag, &tag.Type); scanErr != nil {
			return result, fmt.Errorf("failed to scan tag: %w", scanErr)
		}
		result.Tags = append(result.Tags, tag)
	}

	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("error iterating tags: %w", err)
	}

	return result, nil
}
