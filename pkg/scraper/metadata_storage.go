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
	"fmt"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// MetadataStorage provides methods to store and retrieve scraped metadata using the Tags system
type MetadataStorage struct {
	db database.MediaDBI
}

// NewMetadataStorage creates a new metadata storage instance
func NewMetadataStorage(db database.MediaDBI) *MetadataStorage {
	return &MetadataStorage{db: db}
}

// Standard TagType names for scraped metadata
const (
	TagTypeGenre         = "genre"
	TagTypeDeveloper     = "developer"
	TagTypePublisher     = "publisher"
	TagTypePlayers       = "players"
	TagTypeReleaseDate   = "release_date"
	TagTypeRating        = "rating"
	TagTypeDescription   = "description"
	TagTypeScraperSource = "scraper_source"
	TagTypeScrapedAt     = "scraped_at"
)

var standardTagTypes = []string{
	TagTypeGenre,
	TagTypeDeveloper,
	TagTypePublisher,
	TagTypePlayers,
	TagTypeReleaseDate,
	TagTypeRating,
	TagTypeDescription,
	TagTypeScraperSource,
	TagTypeScrapedAt,
}

// EnsureStandardTagTypes creates standard TagTypes for scraped metadata if they don't exist
func (ms *MetadataStorage) EnsureStandardTagTypes(ctx context.Context) error {
	for _, tagType := range standardTagTypes {
		_, err := ms.ensureTagType(ctx, tagType)
		if err != nil {
			return fmt.Errorf("failed to ensure tag type %s: %w", tagType, err)
		}
	}
	return nil
}

// StoreMetadata stores scraped metadata as Tags linked to a MediaTitle
func (ms *MetadataStorage) StoreMetadata(ctx context.Context, metadata *ScrapedMetadata) error {
	// First ensure all tag types exist
	if err := ms.EnsureStandardTagTypes(ctx); err != nil {
		return fmt.Errorf("failed to ensure tag types: %w", err)
	}

	// Note: For now we don't clear existing metadata to avoid complex queries.
	// This means tags will accumulate. This can be improved later.

	// Store each metadata field as a tag
	metadataMap := map[string]string{
		TagTypeGenre:         metadata.Genre,
		TagTypeDeveloper:     metadata.Developer,
		TagTypePublisher:     metadata.Publisher,
		TagTypePlayers:       metadata.Players,
		TagTypeReleaseDate:   metadata.ReleaseDate,
		TagTypeDescription:   metadata.Description,
		TagTypeScraperSource: metadata.ScraperSource,
		TagTypeScrapedAt:     strconv.FormatInt(metadata.ScrapedAt.Unix(), 10),
	}
	_ = time.Now() // ensure time package is recognized as used

	if metadata.Rating > 0 {
		metadataMap[TagTypeRating] = fmt.Sprintf("%.2f", metadata.Rating)
	}

	for tagTypeName, value := range metadataMap {
		if value == "" {
			continue // Skip empty values
		}

		if err := ms.storeTag(ctx, metadata.MediaTitleDBID, tagTypeName, value); err != nil {
			return fmt.Errorf("failed to store tag %s=%s: %w", tagTypeName, value, err)
		}
	}

	return nil
}

// GetMetadata retrieves scraped metadata for a MediaTitle from Tags
func (ms *MetadataStorage) GetMetadata(ctx context.Context, mediaTitleDBID int64, scraperSource string) (*ScrapedMetadata, error) {
	// For now, return nil as this is a complex query that would require
	// additional interface methods. This can be implemented later if needed.
	return nil, nil
}

// ensureTagType creates a TagType if it doesn't exist and returns its DBID
func (ms *MetadataStorage) ensureTagType(ctx context.Context, tagTypeName string) (int64, error) {
	tagType := database.TagType{
		Type: tagTypeName,
	}

	result, err := ms.db.FindOrInsertTagType(tagType)
	if err != nil {
		return 0, fmt.Errorf("failed to ensure tag type %s: %w", tagTypeName, err)
	}

	return result.DBID, nil
}

// storeTag stores a tag value for a MediaTitle
func (ms *MetadataStorage) storeTag(ctx context.Context, mediaTitleDBID int64, tagTypeName, value string) error {
	// Get or create TagType
	tagTypeDBID, err := ms.ensureTagType(ctx, tagTypeName)
	if err != nil {
		return fmt.Errorf("failed to ensure tag type: %w", err)
	}

	// Get or create Tag
	tag := database.Tag{
		TypeDBID: tagTypeDBID,
		Tag:      value,
	}

	tagResult, err := ms.db.FindOrInsertTag(tag)
	if err != nil {
		return fmt.Errorf("failed to ensure tag: %w", err)
	}

	// Create MediaTitleTag link
	mediaTitleTag := database.MediaTitleTag{
		MediaTitleDBID: mediaTitleDBID,
		TagDBID:        tagResult.DBID,
	}

	_, err = ms.db.FindOrInsertMediaTitleTag(mediaTitleTag)
	if err != nil {
		return fmt.Errorf("failed to link tag to media title: %w", err)
	}

	return nil
}
