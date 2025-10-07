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

package mediascanner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// PathFragmentKey represents a unique key for caching path fragments
type PathFragmentKey struct {
	Path         string
	FilenameTags bool
}

// pathFragmentCache provides a simple LRU cache for parsed path fragments
// to avoid redundant regex operations and string processing
type pathFragmentCache struct {
	cache   map[PathFragmentKey]*MediaPathFragments
	keys    []PathFragmentKey
	maxSize int
	mu      sync.RWMutex
}

// globalPathFragmentCache is a package-level cache for path fragments
var globalPathFragmentCache = &pathFragmentCache{
	cache:   make(map[PathFragmentKey]*MediaPathFragments, 5000),
	keys:    make([]PathFragmentKey, 0, 5000),
	maxSize: 5000,
}

// get retrieves a cached path fragment if it exists
func (c *pathFragmentCache) get(key PathFragmentKey) (MediaPathFragments, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	frag, ok := c.cache[key]
	if !ok {
		return MediaPathFragments{}, false
	}
	return *frag, true
}

// put stores a path fragment in the cache, evicting oldest entry if cache is full
func (c *pathFragmentCache) put(key PathFragmentKey, frag *MediaPathFragments) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if _, exists := c.cache[key]; exists {
		return
	}

	// Evict oldest if at capacity
	if len(c.keys) >= c.maxSize {
		oldest := c.keys[0]
		delete(c.cache, oldest)
		c.keys = c.keys[1:]
	}

	c.cache[key] = frag
	c.keys = append(c.keys, key)
}

// We can't batch effectively without a sense of relationships
// Instead of indexing string columns, use an in-memory map to track records to
// insert IDs.
// Batching should be able to run with an assumed IDs
// database.ScanState and DB transactions allow accumulation

func FlushScanStateMaps(ss *database.ScanState) {
	// Clear maps by deleting all keys instead of reallocating
	// This reuses the underlying memory allocation
	for k := range ss.SystemIDs {
		delete(ss.SystemIDs, k)
	}
	for k := range ss.TitleIDs {
		delete(ss.TitleIDs, k)
	}
	for k := range ss.MediaIDs {
		delete(ss.MediaIDs, k)
	}
	// Note: TagIDs and TagTypeIDs are preserved across batches for performance
	// since tags are typically reused across different systems
}

func AddMediaPath(
	db database.MediaDBI,
	ss *database.ScanState,
	systemID string,
	path string,
	noExt bool,
	cfg *config.Instance,
) (titleIndex, mediaIndex int, err error) {
	pf := GetPathFragments(cfg, path, noExt)

	systemIndex := 0
	if foundSystemIndex, ok := ss.SystemIDs[systemID]; !ok {
		ss.SystemsIndex++
		systemIndex = ss.SystemsIndex
		_, err := db.InsertSystem(database.System{
			DBID:     int64(systemIndex),
			SystemID: systemID,
			Name:     systemID,
		})
		if err != nil {
			ss.SystemsIndex-- // Rollback index increment on failure

			// Only attempt recovery for UNIQUE constraint violations
			// Other errors (connection issues, etc.) should fail fast
			var sqliteErr sqlite3.Error
			if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
				return 0, 0, fmt.Errorf("error inserting system %s: %w", systemID, err)
			}

			log.Trace().Err(err).Msgf("system already exists: %s", systemID)

			// Try to get existing system ID from database when constraint violated
			existingSystem, getErr := db.FindSystemBySystemID(systemID)
			if getErr != nil || existingSystem.DBID == 0 {
				// If we can't get the system, we must fail properly
				return 0, 0, fmt.Errorf("failed to get existing system %s after insert failed: %w", systemID, getErr)
			}
			systemIndex = int(existingSystem.DBID)
			ss.SystemIDs[systemID] = systemIndex // Update cache with existing ID
			log.Trace().Msgf("using existing system %s with DBID %d", systemID, systemIndex)
		} else {
			ss.SystemIDs[systemID] = systemIndex // Only update cache on success
		}
	} else {
		systemIndex = foundSystemIndex
	}

	titleKey := systemID + ":" + pf.Slug
	if foundTitleIndex, ok := ss.TitleIDs[titleKey]; !ok {
		ss.TitlesIndex++
		titleIndex = ss.TitlesIndex
		_, err := db.InsertMediaTitle(database.MediaTitle{
			DBID:       int64(titleIndex),
			Slug:       pf.Slug,
			Name:       pf.Title,
			SystemDBID: int64(systemIndex),
		})
		if err != nil {
			ss.TitlesIndex-- // Rollback index increment on failure

			// Handle UNIQUE constraint violations gracefully - data may already exist from previous batches
			var sqliteErr sqlite3.Error
			if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
				return 0, 0, fmt.Errorf("error inserting media title %s: %w", pf.Title, err)
			}
			log.Debug().Err(err).Msgf("media title already exists: %s", pf.Title)
			// Recover by finding the existing title's DBID
			existingTitle, getErr := db.FindMediaTitle(database.MediaTitle{
				Slug: pf.Slug, SystemDBID: int64(systemIndex),
			})
			if getErr != nil || existingTitle.DBID == 0 {
				return 0, 0, fmt.Errorf(
					"failed to get existing media title %s after insert failed: %w",
					pf.Title,
					getErr,
				)
			}
			titleIndex = int(existingTitle.DBID)
			ss.TitleIDs[titleKey] = titleIndex // Update cache with correct ID
			log.Debug().Msgf("using existing media title %s with DBID %d", pf.Title, titleIndex)
		} else {
			ss.TitleIDs[titleKey] = titleIndex // Only update cache on success
		}
	} else {
		titleIndex = foundTitleIndex
	}

	mediaKey := systemID + ":" + pf.Path
	if foundMediaIndex, ok := ss.MediaIDs[mediaKey]; !ok {
		ss.MediaIndex++
		mediaIndex = ss.MediaIndex
		_, err := db.InsertMedia(database.Media{
			DBID:           int64(mediaIndex),
			Path:           pf.Path,
			MediaTitleDBID: int64(titleIndex),
			SystemDBID:     int64(systemIndex),
		})
		if err != nil {
			ss.MediaIndex-- // Rollback index increment on failure

			// Handle UNIQUE constraint violations gracefully - data may already exist from previous batches
			var sqliteErr sqlite3.Error
			if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
				log.Debug().Err(err).Msgf("media already exists: %s", pf.Path)

				// Recover by finding the existing media's DBID using only the UNIQUE constraint fields
				// (SystemDBID, Path) to ensure we find the right row
				existingMedia, getErr := db.FindMedia(database.Media{
					Path:       pf.Path,
					SystemDBID: int64(systemIndex),
				})
				if getErr != nil || existingMedia.DBID == 0 {
					return 0, 0, fmt.Errorf(
						"failed to get existing media %s after insert failed: %w",
						pf.Path,
						getErr,
					)
				}
				mediaIndex = int(existingMedia.DBID)
				ss.MediaIDs[mediaKey] = mediaIndex // Update cache with correct ID
				log.Debug().Msgf("using existing media %s with DBID %d", pf.Path, mediaIndex)
			} else {
				log.Error().Err(err).Msgf("error inserting media: %s", pf.Path)
			}
		} else {
			ss.MediaIDs[mediaKey] = mediaIndex // Only update cache on success
		}
	} else {
		mediaIndex = foundMediaIndex
	}

	// Extract extension tag only if filename tags are enabled
	if pf.Ext != "" && (cfg == nil || cfg.FilenameTags()) {
		// Remove leading dot from extension for tag storage
		extWithoutDot := strings.TrimPrefix(pf.Ext, ".")
		// Use composite key for extension tags to avoid collisions
		extensionKey := string(tags.TagTypeExtension) + ":" + extWithoutDot
		if _, ok := ss.TagIDs[extensionKey]; !ok {
			// Get or create the Extension tag type ID dynamically
			extensionTypeID, found := ss.TagTypeIDs[string(tags.TagTypeExtension)]
			if !found {
				// Extension tag type doesn't exist in cache, try to look it up
				existingTagType, getErr := db.FindTagType(database.TagType{Type: string(tags.TagTypeExtension)})
				if getErr != nil || existingTagType.DBID == 0 {
					return 0, 0, fmt.Errorf(
						"extension tag type not found and not in cache "+
							"(should not happen after SeedCanonicalTags): %w",
						getErr,
					)
				}
				extensionTypeID = int(existingTagType.DBID)
				ss.TagTypeIDs[string(tags.TagTypeExtension)] = extensionTypeID
			}

			ss.TagsIndex++
			tagIndex := ss.TagsIndex
			_, err := db.InsertTag(database.Tag{
				DBID:     int64(tagIndex),
				Tag:      extWithoutDot,
				TypeDBID: int64(extensionTypeID),
			})
			if err != nil {
				ss.TagsIndex-- // Rollback index increment on failure

				// Handle UNIQUE constraint violations gracefully - find existing tag and add to map
				var sqliteErr sqlite3.Error
				if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
					log.Debug().Err(err).Msgf("tag extension already exists: %s", extWithoutDot)

					// Look up existing tag and add it to the map to prevent repeated insertion attempts
					existingTag, getErr := db.FindTag(database.Tag{Tag: extWithoutDot})
					if getErr != nil || existingTag.DBID == 0 {
						log.Error().Err(getErr).Msgf("Failed to get existing tag %s after insert failed", extWithoutDot)
					} else {
						ss.TagIDs[extensionKey] = int(existingTag.DBID)
						log.Debug().Msgf("using existing tag %s with DBID %d", extWithoutDot, existingTag.DBID)
					}
				} else {
					log.Error().Err(err).Msgf("error inserting tag extension: %s", extWithoutDot)
				}
			} else {
				ss.TagIDs[extensionKey] = tagIndex // Only update cache on success
			}
		}

		// Link the extension tag to the media
		extensionTagIndex := ss.TagIDs[extensionKey]
		if extensionTagIndex > 0 {
			_, err := db.InsertMediaTag(database.MediaTag{
				TagDBID:   int64(extensionTagIndex),
				MediaDBID: int64(mediaIndex),
			})
			if err != nil {
				var sqliteErr sqlite3.Error
				if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
					log.Trace().Err(err).Msgf("media tag relationship already exists for extension: %s", extWithoutDot)
				} else {
					log.Error().Err(err).Msgf("error inserting media tag relationship for extension: %s", extWithoutDot)
				}
			}
		}
	}

	for _, tagStr := range pf.Tags {
		tagIndex := 0

		if foundTagIndex, ok := ss.TagIDs[tagStr]; ok {
			tagIndex = foundTagIndex
		}

		if tagIndex == 0 {
			// Don't insert unknown tags for now
			// log.Error().Msgf("error inserting media tag relationship: %s", tagStr)
			continue
		}

		_, err := db.InsertMediaTag(database.MediaTag{
			TagDBID:   int64(tagIndex),
			MediaDBID: int64(mediaIndex),
		})
		if err != nil {
			log.Debug().Err(err).Msgf("media tag relationship already exists: %s", tagStr)
		}
	}
	return titleIndex, mediaIndex, nil
}

type MediaPathFragments struct {
	Path     string
	FileName string
	Title    string
	Slug     string
	Ext      string
	Tags     []string
}

func getTagsFromFileName(filename string) []string {
	canonicalStructs := tags.ParseFilenameToCanonicalTags(filename)

	// Convert CanonicalTag structs to "type:value" format for database compatibility
	// This matches the composite keys used in the TagIDs map
	canonicalTags := make([]string, 0, len(canonicalStructs))
	for _, ct := range canonicalStructs {
		canonicalTags = append(canonicalTags, ct.String())
	}

	return canonicalTags
}

func getTitleFromFilename(filename string) string {
	r := helpers.CachedMustCompile(`^([^(\[]*)`)
	title := r.FindString(filename)
	return strings.TrimSpace(title)
}

// SeedCanonicalTags seeds the database with canonical GameDataBase-style hierarchical tags.
// Tags follow the format: category:subcategory:value (e.g., "genre:sports:wrestling", "players:2:vs")
// Tag definitions are in tags.go for centralized management.
func SeedCanonicalTags(db database.MediaDBI, ss *database.ScanState) error {
	if err := db.BeginTransaction(); err != nil {
		return fmt.Errorf("failed to begin transaction for seeding tags: %w", err)
	}

	// Use canonical tag definitions from tags.go
	typeMatches := make(map[string][]string)
	for tagType, tagValues := range tags.CanonicalTagDefinitions {
		// Convert []TagValue to []string
		strTags := make([]string, len(tagValues))
		for i, tag := range tagValues {
			strTags[i] = string(tag)
		}
		typeMatches[string(tagType)] = strTags
	}

	ss.TagTypesIndex++
	_, err := db.InsertTagType(database.TagType{
		DBID: int64(ss.TagTypesIndex),
		Type: "unknown",
	})
	if err != nil {
		ss.TagTypesIndex-- // Rollback index increment on failure

		// Handle UNIQUE constraint violations gracefully - data may already exist
		var sqliteErr sqlite3.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
			return fmt.Errorf("error inserting tag type unknown: %w", err)
		}
		log.Debug().Msg("tag type 'unknown' already exists, continuing")
		// Try to get the existing tag type to update our index
		existingTagType, getErr := db.FindTagType(database.TagType{Type: "unknown"})
		if getErr == nil && existingTagType.DBID > 0 {
			// Update index only if this existing tag has a higher DBID
			if int(existingTagType.DBID) > ss.TagTypesIndex {
				ss.TagTypesIndex = int(existingTagType.DBID)
			}
			ss.TagTypeIDs["unknown"] = int(existingTagType.DBID)
		}
	} else {
		ss.TagTypeIDs["unknown"] = ss.TagTypesIndex
	}

	ss.TagsIndex++
	_, err = db.InsertTag(database.Tag{
		DBID:     int64(ss.TagsIndex),
		Tag:      "unknown",
		TypeDBID: int64(ss.TagTypesIndex),
	})
	if err != nil {
		ss.TagsIndex-- // Rollback index increment on failure

		// Handle UNIQUE constraint violations gracefully
		var sqliteErr sqlite3.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
			return fmt.Errorf("error inserting tag unknown: %w", err)
		}
		log.Debug().Msg("tag 'unknown' already exists, continuing")
		// Try to get the existing tag to update our index
		existingTag, getErr := db.FindTag(database.Tag{Tag: "unknown"})
		if getErr == nil && existingTag.DBID > 0 {
			// Update index only if this existing tag has a higher DBID
			if int(existingTag.DBID) > ss.TagsIndex {
				ss.TagsIndex = int(existingTag.DBID)
			}
			// Use composite key for consistency
			ss.TagIDs["unknown:unknown"] = int(existingTag.DBID)
		}
	} else {
		// Use composite key for consistency
		ss.TagIDs["unknown:unknown"] = ss.TagsIndex
	}

	ss.TagTypesIndex++
	_, err = db.InsertTagType(database.TagType{
		DBID: int64(ss.TagTypesIndex),
		Type: "extension",
	})
	if err != nil {
		ss.TagTypesIndex-- // Rollback index increment on failure

		// Handle UNIQUE constraint violations gracefully
		var sqliteErr sqlite3.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
			return fmt.Errorf("error inserting tag type extension: %w", err)
		}
		log.Debug().Msg("tag type 'extension' already exists, continuing")
		// Try to get the existing tag type to update our index
		existingTagType, getErr := db.FindTagType(database.TagType{Type: "extension"})
		if getErr == nil && existingTagType.DBID > 0 {
			// Update index only if this existing tag has a higher DBID
			if int(existingTagType.DBID) > ss.TagTypesIndex {
				ss.TagTypesIndex = int(existingTagType.DBID)
			}
			ss.TagTypeIDs["extension"] = int(existingTagType.DBID)
		}
	} else {
		ss.TagTypeIDs["extension"] = ss.TagTypesIndex
	}

	// Seed canonical tag types and values
	for typeStr, tagValues := range typeMatches {
		ss.TagTypesIndex++
		_, err := db.InsertTagType(database.TagType{
			DBID: int64(ss.TagTypesIndex),
			Type: typeStr,
		})
		if err != nil {
			ss.TagTypesIndex-- // Rollback index increment on failure

			// Handle UNIQUE constraint violations gracefully
			var sqliteErr sqlite3.Error
			if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
				return fmt.Errorf("error inserting tag type %s: %w", typeStr, err)
			}
			log.Debug().Msgf("tag type '%s' already exists, continuing", typeStr)
			// Try to get the existing tag type to update our index
			existingTagType, getErr := db.FindTagType(database.TagType{Type: typeStr})
			if getErr == nil && existingTagType.DBID > 0 {
				// Update index only if this existing tag has a higher DBID
				if int(existingTagType.DBID) > ss.TagTypesIndex {
					ss.TagTypesIndex = int(existingTagType.DBID)
				}
				ss.TagTypeIDs[typeStr] = int(existingTagType.DBID)
			}
		} else {
			ss.TagTypeIDs[typeStr] = ss.TagTypesIndex
		}

		for _, tag := range tagValues {
			ss.TagsIndex++
			tagValue := strings.ToLower(tag)
			_, err := db.InsertTag(database.Tag{
				DBID:     int64(ss.TagsIndex),
				Tag:      tagValue,
				TypeDBID: int64(ss.TagTypesIndex),
			})
			if err != nil {
				ss.TagsIndex-- // Rollback index increment on failure

				// Handle UNIQUE constraint violations gracefully
				var sqliteErr sqlite3.Error
				if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
					return fmt.Errorf("error inserting tag %s: %w", tag, err)
				}
				log.Debug().Msgf("tag '%s' already exists, continuing", tag)
				// Try to get the existing tag to update our index
				existingTag, getErr := db.FindTag(database.Tag{Tag: tagValue})
				if getErr == nil && existingTag.DBID > 0 {
					// Update index only if this existing tag has a higher DBID
					if int(existingTag.DBID) > ss.TagsIndex {
						ss.TagsIndex = int(existingTag.DBID)
					}
					// Use composite key "type:value" to avoid collisions (e.g., disc:1 vs rev:1)
					compositeKey := typeStr + ":" + tagValue
					ss.TagIDs[compositeKey] = int(existingTag.DBID)
				}
			} else {
				// Use composite key "type:value" to avoid collisions (e.g., disc:1 vs rev:1)
				compositeKey := typeStr + ":" + tagValue
				ss.TagIDs[compositeKey] = ss.TagsIndex
			}
		}
	}

	if err := db.CommitTransaction(); err != nil {
		if rbErr := db.RollbackTransaction(); rbErr != nil {
			log.Error().Err(rbErr).Msg("failed to rollback transaction after commit failure")
		}
		return fmt.Errorf("failed to commit tag seeding transaction: %w", err)
	}

	return nil
}

// PopulateScanStateFromDB initializes the scan state with existing database IDs
// when resuming an interrupted indexing operation
func PopulateScanStateFromDB(ctx context.Context, db database.MediaDBI, ss *database.ScanState) error {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Get max IDs from existing data to continue indexing from the right point
	maxSystemID, err := db.GetMaxSystemID()
	if err != nil {
		return fmt.Errorf("failed to get max system ID: %w", err)
	}
	ss.SystemsIndex = int(maxSystemID)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxTitleID, err := db.GetMaxTitleID()
	if err != nil {
		return fmt.Errorf("failed to get max title ID: %w", err)
	}
	ss.TitlesIndex = int(maxTitleID)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxMediaID, err := db.GetMaxMediaID()
	if err != nil {
		return fmt.Errorf("failed to get max media ID: %w", err)
	}
	ss.MediaIndex = int(maxMediaID)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxTagTypeID, err := db.GetMaxTagTypeID()
	if err != nil {
		return fmt.Errorf("failed to get max tag type ID: %w", err)
	}
	ss.TagTypesIndex = int(maxTagTypeID)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxTagID, err := db.GetMaxTagID()
	if err != nil {
		return fmt.Errorf("failed to get max tag ID: %w", err)
	}
	ss.TagsIndex = int(maxTagID)

	// Populate maps with existing data to prevent duplicate insertion attempts
	// This is crucial for resuming indexing operations

	// Check for cancellation before loading systems
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// First populate systems map
	systems, err := db.GetAllSystems()
	if err != nil {
		return fmt.Errorf("failed to get existing systems: %w", err)
	}
	for _, system := range systems {
		ss.SystemIDs[system.SystemID] = int(system.DBID)
	}

	// Check for cancellation before loading titles
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	titlesWithSystems, err := db.GetTitlesWithSystems()
	if err != nil {
		return fmt.Errorf("failed to get existing titles with systems: %w", err)
	}
	for _, title := range titlesWithSystems {
		// Direct construction - SystemID is already available from the JOIN
		titleKey := title.SystemID + ":" + title.Slug
		ss.TitleIDs[titleKey] = int(title.DBID)
	}

	// Check for cancellation before loading media
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	mediaWithFullPath, err := db.GetMediaWithFullPath()
	if err != nil {
		return fmt.Errorf("failed to get existing media with full path: %w", err)
	}
	for _, m := range mediaWithFullPath {
		// Direct construction - SystemID is already available from the JOIN
		mediaKey := m.SystemID + ":" + m.Path
		ss.MediaIDs[mediaKey] = int(m.DBID)
	}

	// Check for cancellation before loading tag types
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Populate tag types map
	tagTypes, err := db.GetAllTagTypes()
	if err != nil {
		return fmt.Errorf("failed to get existing tag types: %w", err)
	}
	for _, tagType := range tagTypes {
		ss.TagTypeIDs[tagType.Type] = int(tagType.DBID)
	}

	// Check for cancellation before loading tags
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Populate tags map
	allTags, err := db.GetAllTags()
	if err != nil {
		return fmt.Errorf("failed to get existing tags: %w", err)
	}
	for _, tag := range allTags {
		ss.TagIDs[tag.Tag] = int(tag.DBID)
	}

	log.Debug().Msgf("populated scan state from DB: Sys=%d, Titles=%d, Media=%d, TagTypes=%d, Tags=%d "+
		"(maps: TagTypes=%d, Tags=%d)",
		ss.SystemsIndex, ss.TitlesIndex, ss.MediaIndex,
		ss.TagTypesIndex, ss.TagsIndex, len(ss.TagTypeIDs), len(ss.TagIDs))

	return nil
}

// PopulateScanStateForSelectiveIndexing populates scan state for selective indexing with optimized loading.
// Uses true lazy loading for Systems/TagTypes (via UNIQUE constraints) and minimal data loading
// for MediaTitles/Media (only systems NOT being reindexed) to dramatically improve performance.
func PopulateScanStateForSelectiveIndexing(
	ctx context.Context, db database.MediaDBI, ss *database.ScanState, systemsToReindex []string,
) error {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Get max IDs from existing data to continue indexing from the right point
	maxSystemID, err := db.GetMaxSystemID()
	if err != nil {
		return fmt.Errorf("failed to get max system ID: %w", err)
	}
	ss.SystemsIndex = int(maxSystemID)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxTitleID, err := db.GetMaxTitleID()
	if err != nil {
		return fmt.Errorf("failed to get max title ID: %w", err)
	}
	ss.TitlesIndex = int(maxTitleID)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxMediaID, err := db.GetMaxMediaID()
	if err != nil {
		return fmt.Errorf("failed to get max media ID: %w", err)
	}
	ss.MediaIndex = int(maxMediaID)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxTagTypeID, err := db.GetMaxTagTypeID()
	if err != nil {
		return fmt.Errorf("failed to get max tag type ID: %w", err)
	}
	ss.TagTypesIndex = int(maxTagTypeID)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	maxTagID, err := db.GetMaxTagID()
	if err != nil {
		return fmt.Errorf("failed to get max tag ID: %w", err)
	}
	ss.TagsIndex = int(maxTagID)

	// Use pure lazy loading for Systems and TagTypes (they have UNIQUE constraints)
	// Empty maps will be populated on-demand when UNIQUE constraint violations occur
	// This is handled automatically in AddMediaPath for these tables

	// For MediaTitles and Media, we need to maintain maps since they don't have
	// unique constraints on the columns we insert (no constraint on SystemDBID+Slug or Path)
	// But we optimize by only loading data for systems NOT being reindexed

	// Check for cancellation before loading titles
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Populate titles map (only for systems not being reindexed)
	// Use optimized query that excludes systems being reindexed
	titlesWithSystems, err := db.GetTitlesWithSystemsExcluding(systemsToReindex)
	if err != nil {
		return fmt.Errorf("failed to get existing titles with systems (excluding reindexed): %w", err)
	}
	for _, title := range titlesWithSystems {
		titleKey := title.SystemID + ":" + title.Slug
		ss.TitleIDs[titleKey] = int(title.DBID)
	}

	// Check for cancellation before loading media
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Populate media map (only for systems not being reindexed)
	// Use optimized query that excludes systems being reindexed
	mediaWithFullPath, err := db.GetMediaWithFullPathExcluding(systemsToReindex)
	if err != nil {
		return fmt.Errorf("failed to get existing media with full path (excluding reindexed): %w", err)
	}
	for _, m := range mediaWithFullPath {
		mediaKey := m.SystemID + ":" + m.Path
		ss.MediaIDs[mediaKey] = int(m.DBID)
	}

	// For selective indexing, use true lazy loading for Systems, TagTypes, and Tags
	// This dramatically reduces memory usage and startup time by:
	// 1. Not pre-loading Systems/TagTypes (use UNIQUE constraint handling)
	// 2. Not pre-loading any Tags (handled by constraint violations)
	// 3. Only loading MediaTitles/Media for systems NOT being reindexed

	log.Debug().Msgf("populated scan state for selective indexing: "+
		"maxIDs: Sys=%d, Titles=%d, Media=%d, TagTypes=%d, Tags=%d; "+
		"maps: Sys=%d, Titles=%d, Media=%d, TagTypes=%d, Tags=%d; systems to reindex: %v",
		ss.SystemsIndex, ss.TitlesIndex, ss.MediaIndex,
		ss.TagTypesIndex, ss.TagsIndex, len(ss.SystemIDs), len(ss.TitleIDs), len(ss.MediaIDs),
		len(ss.TagTypeIDs), len(ss.TagIDs), systemsToReindex)

	return nil
}

func GetPathFragments(cfg *config.Instance, path string, noExt bool) MediaPathFragments {
	filenameTagsEnabled := cfg == nil || cfg.FilenameTags()
	cacheKey := PathFragmentKey{
		Path:         path,
		FilenameTags: filenameTagsEnabled,
	}

	// Check cache first
	if frag, ok := globalPathFragmentCache.get(cacheKey); ok {
		return frag
	}

	// Cache miss - compute fragments
	f := MediaPathFragments{}

	// don't clean the :// in custom scheme paths
	if helpers.ReURI.MatchString(path) {
		f.Path = path
	} else {
		f.Path = filepath.Clean(path)
	}

	fileBase := filepath.Base(f.Path)

	// Skip extension extraction for virtual paths to avoid extracting garbage
	// like ".)(ocs)" from "/games/file.txt/Game (v1.0)(ocs)" or
	// ". Strange" from "kodi://123/Dr. Strange"
	if noExt || helpers.ReURI.MatchString(path) {
		f.Ext = ""
	} else {
		f.Ext = strings.ToLower(filepath.Ext(f.Path))
		if helpers.HasSpace(f.Ext) {
			f.Ext = ""
		}
	}

	f.FileName, _ = strings.CutSuffix(fileBase, f.Ext)

	f.Title = getTitleFromFilename(f.FileName)
	f.Slug = slugs.SlugifyString(f.Title)

	// For non-Latin titles that don't produce a slug, store the lowercase
	// original title. This ensures Slug is never empty while the search
	// logic (mediadb.go) falls back to the Name field for these cases.
	if f.Slug == "" {
		f.Slug = strings.ToLower(f.FileName)
	}

	// Extract tags from filename only if enabled in config (default to enabled for nil config)
	if cfg == nil || cfg.FilenameTags() {
		f.Tags = getTagsFromFileName(f.FileName)
	} else {
		f.Tags = []string{}
	}

	// Store in cache for future use
	globalPathFragmentCache.put(cacheKey, &f)

	return f
}
