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

package mediascanner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	platformsshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

type PathFragmentKey struct {
	Path                string
	FilenameTags        bool
	StripLeadingNumbers bool
}

// PathFragmentParams contains parameters for GetPathFragments.
type PathFragmentParams struct {
	Config              *config.Instance
	Path                string
	SystemID            string
	NoExt               bool
	StripLeadingNumbers bool
}

// We can't batch effectively without a sense of relationships
// Instead of indexing string columns, use an in-memory map to track records to
// insert IDs.
// Batching should be able to run with an assumed IDs
// database.ScanState and DB transactions allow accumulation

// FlushScanStateMaps clears the in-memory maps for titles and media to free memory
// between transaction commits during batch indexing.
//
// IMPORTANT: SystemIDs, TagIDs, and TagTypeIDs are NOT cleared because:
//   - They are global entities reused across all batches/systems
//   - There are relatively few of them (~100-200 systems, ~40 tag types)
//   - Clearing SystemIDs causes duplicate insert attempts with batch inserts enabled
//   - Preserving them prevents UNIQUE constraint violations on subsequent inserts
func FlushScanStateMaps(ss *database.ScanState) {
	// Clear maps by deleting all keys instead of reallocating
	// This reuses the underlying memory allocation

	// SystemIDs preserved - DO NOT clear (causes UNIQUE constraint violations with batch inserts)
	// TagIDs preserved - reused across systems
	// TagTypeIDs preserved - reused across systems

	for k := range ss.TitleIDs {
		delete(ss.TitleIDs, k)
	}
	for k := range ss.MediaIDs {
		delete(ss.MediaIDs, k)
	}
}

func AddMediaPath(
	db database.MediaDBI,
	ss *database.ScanState,
	systemID string,
	path string,
	noExt bool,
	stripLeadingNumbers bool,
	cfg *config.Instance,
) (titleIndex, mediaIndex int, err error) {
	pf := GetPathFragments(PathFragmentParams{
		Config:              cfg,
		Path:                path,
		NoExt:               noExt,
		StripLeadingNumbers: stripLeadingNumbers,
		SystemID:            systemID,
	})

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
			return 0, 0, fmt.Errorf("error inserting system %s: %w", systemID, err)
		}
		ss.SystemIDs[systemID] = systemIndex
	} else {
		systemIndex = foundSystemIndex
	}

	titleKey := database.TitleKey(systemID, pf.Slug)
	if foundTitleIndex, ok := ss.TitleIDs[titleKey]; !ok {
		ss.TitlesIndex++
		titleIndex = ss.TitlesIndex

		// Look up mediaType for consistent slugification
		mediaType := slugs.MediaTypeGame // Default
		if system, err := systemdefs.GetSystem(systemID); err == nil && system != nil {
			mediaType = system.GetMediaType()
		}

		// Generate slug metadata for fuzzy matching prefilter
		metadata := mediadb.GenerateSlugWithMetadata(mediaType, pf.Title)

		_, err := db.InsertMediaTitle(&database.MediaTitle{
			DBID:          int64(titleIndex),
			Slug:          pf.Slug,
			Name:          pf.Title,
			SystemDBID:    int64(systemIndex),
			SlugLength:    metadata.SlugLength,
			SlugWordCount: metadata.SlugWordCount,
			SecondarySlug: sql.NullString{String: metadata.SecondarySlug, Valid: metadata.SecondarySlug != ""},
		})
		if err != nil {
			ss.TitlesIndex-- // Rollback index increment on failure
			return 0, 0, fmt.Errorf("error inserting media title %s: %w", pf.Title, err)
		}
		ss.TitleIDs[titleKey] = titleIndex
	} else {
		titleIndex = foundTitleIndex
	}

	mediaKey := database.MediaKey(systemID, pf.Path)
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
			return 0, 0, fmt.Errorf("error inserting media %s: %w", pf.Path, err)
		}
		ss.MediaIDs[mediaKey] = mediaIndex
	} else {
		mediaIndex = foundMediaIndex
	}

	// Extract extension tag only if filename tags are enabled
	if pf.Ext != "" && (cfg == nil || cfg.FilenameTags()) {
		// Remove leading dot from extension for tag storage
		extWithoutDot := strings.TrimPrefix(pf.Ext, ".")
		// Use composite key for extension tags to avoid collisions
		extensionKey := database.TagKey(string(tags.TagTypeExtension), extWithoutDot)
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
				log.Error().Err(err).Msgf("error inserting tag extension: %s", extWithoutDot)
			} else {
				ss.TagIDs[extensionKey] = tagIndex
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

		// Dynamically create revision tags if they don't exist
		// This allows version numbers like "v7.2502" to be stored as "rev:7-2502"
		if tagIndex == 0 && strings.HasPrefix(tagStr, string(tags.TagTypeRev)+":") {
			// Extract the revision value (everything after "rev:")
			revValue := strings.TrimPrefix(tagStr, string(tags.TagTypeRev)+":")

			// Get or create the Rev tag type ID dynamically
			revTypeID, found := ss.TagTypeIDs[string(tags.TagTypeRev)]
			if !found {
				// Rev tag type doesn't exist in cache, try to look it up
				existingTagType, getErr := db.FindTagType(database.TagType{Type: string(tags.TagTypeRev)})
				if getErr != nil || existingTagType.DBID == 0 {
					log.Error().Err(getErr).Msgf(
						"rev tag type not found and not in cache " +
							"(should not happen after SeedCanonicalTags)",
					)
					continue
				}
				revTypeID = int(existingTagType.DBID)
				ss.TagTypeIDs[string(tags.TagTypeRev)] = revTypeID
			}

			// Create the new revision tag
			ss.TagsIndex++
			tagIndex = ss.TagsIndex
			_, insertErr := db.InsertTag(database.Tag{
				DBID:     int64(tagIndex),
				Tag:      revValue,
				TypeDBID: int64(revTypeID),
			})
			if insertErr != nil {
				ss.TagsIndex-- // Rollback index increment on failure
				log.Error().Err(insertErr).Msgf("error inserting revision tag: %s", revValue)
				continue
			}
			ss.TagIDs[tagStr] = tagIndex
		}

		if tagIndex == 0 {
			// Don't insert unknown tags for other tag types
			log.Trace().Msgf("skipping unknown tag: %s", tagStr)
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

// SeedCanonicalTags seeds the database with canonical GameDataBase-style hierarchical tags.
// Tags follow the format: category:subcategory:value (e.g., "genre:sports:wrestling", "players:2:vs")
// Tag definitions are in tags.go for centralized management.
//
// NOTE: This function ALWAYS uses non-batch mode (prepared statements) because the canonical
// tag dataset contains many entries and using batch mode with fail-fast behavior would cause issues.
// Prepared statements handle this better.
func SeedCanonicalTags(db database.MediaDBI, ss *database.ScanState) error {
	// Always use non-batch mode for seeding canonical tags
	// This prevents issues with the large dataset and provides better error handling
	if err := db.BeginTransaction(false); err != nil {
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

	if _, exists := ss.TagTypeIDs["unknown"]; !exists {
		ss.TagTypesIndex++
		_, err := db.InsertTagType(database.TagType{
			DBID: int64(ss.TagTypesIndex),
			Type: "unknown",
		})
		if err != nil {
			ss.TagTypesIndex-- // Rollback index increment on failure
			return fmt.Errorf("error inserting tag type unknown: %w", err)
		}
		ss.TagTypeIDs["unknown"] = ss.TagTypesIndex
	}

	unknownKey := database.TagKey("unknown", "unknown")
	if _, exists := ss.TagIDs[unknownKey]; !exists {
		ss.TagsIndex++
		_, err := db.InsertTag(database.Tag{
			DBID:     int64(ss.TagsIndex),
			Tag:      "unknown",
			TypeDBID: int64(ss.TagTypeIDs["unknown"]),
		})
		if err != nil {
			ss.TagsIndex-- // Rollback index increment on failure
			return fmt.Errorf("error inserting tag unknown: %w", err)
		}
		ss.TagIDs[unknownKey] = ss.TagsIndex
	}

	if _, exists := ss.TagTypeIDs["extension"]; !exists {
		ss.TagTypesIndex++
		_, err := db.InsertTagType(database.TagType{
			DBID: int64(ss.TagTypesIndex),
			Type: "extension",
		})
		if err != nil {
			ss.TagTypesIndex-- // Rollback index increment on failure
			return fmt.Errorf("error inserting tag type extension: %w", err)
		}
		ss.TagTypeIDs["extension"] = ss.TagTypesIndex
	}

	for typeStr, tagValues := range typeMatches {
		typeID, exists := ss.TagTypeIDs[typeStr]
		if !exists {
			ss.TagTypesIndex++
			_, err := db.InsertTagType(database.TagType{
				DBID: int64(ss.TagTypesIndex),
				Type: typeStr,
			})
			if err != nil {
				ss.TagTypesIndex-- // Rollback index increment on failure
				return fmt.Errorf("error inserting tag type %s: %w", typeStr, err)
			}
			typeID = ss.TagTypesIndex
			ss.TagTypeIDs[typeStr] = typeID
		}

		for _, tag := range tagValues {
			tagValue := strings.ToLower(tag)
			compositeKey := database.TagKey(typeStr, tagValue)

			if _, exists := ss.TagIDs[compositeKey]; exists {
				continue
			}

			ss.TagsIndex++
			_, err := db.InsertTag(database.Tag{
				DBID:     int64(ss.TagsIndex),
				Tag:      tagValue,
				TypeDBID: int64(typeID),
			})
			if err != nil {
				ss.TagsIndex-- // Rollback index increment on failure
				return fmt.Errorf("error inserting tag %s: %w", tag, err)
			}
			// Use composite key "type:value" to avoid collisions (e.g., disc:1 vs rev:1)
			ss.TagIDs[compositeKey] = ss.TagsIndex
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

	// NOTE: TitleIDs and MediaIDs are NOT loaded here for resume optimization.
	// Instead, they are lazy-loaded per-system using PopulateScanStateForSystem()
	// before processing each system.

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
	// Build reverse lookup from TypeDBID -> type string for composite key construction
	tagTypeByDBID := make(map[int64]string, len(tagTypes))
	for _, tagType := range tagTypes {
		ss.TagTypeIDs[tagType.Type] = int(tagType.DBID)
		tagTypeByDBID[tagType.DBID] = tagType.Type
	}

	// Check for cancellation before loading tags
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Populate tags map with composite keys (type:value format)
	// This must match the key format used in AddMediaPath and SeedCanonicalTags
	allTags, err := db.GetAllTags()
	if err != nil {
		return fmt.Errorf("failed to get existing tags: %w", err)
	}
	for _, tag := range allTags {
		tagType := tagTypeByDBID[tag.TypeDBID]
		compositeKey := database.TagKey(tagType, tag.Tag)
		ss.TagIDs[compositeKey] = int(tag.DBID)
	}

	log.Debug().
		Int("maxSystemID", ss.SystemsIndex).
		Int("maxTitleID", ss.TitlesIndex).
		Int("maxMediaID", ss.MediaIndex).
		Int("maxTagTypeID", ss.TagTypesIndex).
		Int("maxTagID", ss.TagsIndex).
		Int("systemsMapSize", len(ss.SystemIDs)).
		Int("tagTypesMapSize", len(ss.TagTypeIDs)).
		Int("tagsMapSize", len(ss.TagIDs)).
		Msg("populated scan state")

	return nil
}

// PopulateScanStateForSystem loads the existing titles and media for a single system into the scan state.
// This is called lazily during resume, just before processing each system.
//
// This function is safe to call multiple times for different systems - it appends to the
// existing TitleIDs and MediaIDs maps.
func PopulateScanStateForSystem(
	ctx context.Context, db database.MediaDBI, ss *database.ScanState, systemID string,
) error {
	startTime := time.Now()

	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Load titles for this system
	titles, err := db.GetTitlesBySystemID(systemID)
	if err != nil {
		return fmt.Errorf("failed to get titles for system %s: %w", systemID, err)
	}
	for _, title := range titles {
		titleKey := database.TitleKey(title.SystemID, title.Slug)
		ss.TitleIDs[titleKey] = int(title.DBID)
	}

	// Check for cancellation between operations
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Load media for this system
	media, err := db.GetMediaBySystemID(systemID)
	if err != nil {
		return fmt.Errorf("failed to get media for system %s: %w", systemID, err)
	}
	for _, m := range media {
		mediaKey := database.MediaKey(m.SystemID, m.Path)
		ss.MediaIDs[mediaKey] = int(m.DBID)
	}

	log.Debug().
		Str("system", systemID).
		Int("titles", len(titles)).
		Int("media", len(media)).
		Dur("elapsed", time.Since(startTime)).
		Msg("loaded existing data for system resume")

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

	// SystemIDs must be pre-populated because keys are NOT system-scoped ("pc", "nes"),
	// so multiple folders can map to the same SystemID (e.g., Batocera: 50+ folders → "pc").
	//
	// TagTypeIDs and TagIDs must be pre-populated because AddMediaPath uses them to
	// create MediaTag associations — empty maps cause tags to be silently dropped.
	//
	// TitleIDs and MediaIDs can remain empty because their keys ARE system-scoped
	// and TruncateSystems CASCADE deleted all data for reindexed systems.

	// Check for cancellation before loading systems
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	systems, err := db.GetAllSystems()
	if err != nil {
		return fmt.Errorf("failed to get existing systems for selective indexing: %w", err)
	}
	ss.SystemIDs = make(map[string]int, len(systems))
	for _, system := range systems {
		ss.SystemIDs[system.SystemID] = int(system.DBID)
	}

	// Check for cancellation before loading tag types
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	tagTypes, err := db.GetAllTagTypes()
	if err != nil {
		return fmt.Errorf("failed to get existing tag types for selective indexing: %w", err)
	}
	ss.TagTypeIDs = make(map[string]int, len(tagTypes))
	tagTypeByDBID := make(map[int64]string, len(tagTypes))
	for _, tagType := range tagTypes {
		ss.TagTypeIDs[tagType.Type] = int(tagType.DBID)
		tagTypeByDBID[tagType.DBID] = tagType.Type
	}

	// Check for cancellation before loading tags
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	allTags, err := db.GetAllTags()
	if err != nil {
		return fmt.Errorf("failed to get existing tags for selective indexing: %w", err)
	}
	ss.TagIDs = make(map[string]int, len(allTags))
	for _, tag := range allTags {
		tagType := tagTypeByDBID[tag.TypeDBID]
		compositeKey := database.TagKey(tagType, tag.Tag)
		ss.TagIDs[compositeKey] = int(tag.DBID)
	}

	// TitleIDs and MediaIDs remain empty (system-scoped keys, safe for selective indexing)
	ss.TitleIDs = make(map[string]int)
	ss.MediaIDs = make(map[string]int)

	// Check for cancellation before re-seeding
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// TruncateSystems orphan-cleans tags only used by truncated systems,
	// so re-seed any missing canonical tags before indexing.
	if err := SeedCanonicalTags(db, ss); err != nil {
		return fmt.Errorf("failed to re-seed canonical tags for selective indexing: %w", err)
	}

	log.Debug().Msgf("populated scan state for selective indexing: "+
		"maxIDs: Sys=%d, Titles=%d, Media=%d, TagTypes=%d, Tags=%d; "+
		"maps: Sys=%d, Titles=%d, Media=%d, TagTypes=%d, Tags=%d; systems to reindex: %v",
		ss.SystemsIndex, ss.TitlesIndex, ss.MediaIndex,
		ss.TagTypesIndex, ss.TagsIndex, len(ss.SystemIDs), len(ss.TitleIDs), len(ss.MediaIDs),
		len(ss.TagTypeIDs), len(ss.TagIDs), systemsToReindex)

	return nil
}

func GetPathFragments(params PathFragmentParams) MediaPathFragments {
	f := MediaPathFragments{}

	// don't clean the :// in custom scheme paths
	if helpers.ReURI.MatchString(params.Path) {
		f.Path = params.Path
	} else {
		// Clean and normalize to forward slashes for cross-platform consistency
		f.Path = filepath.ToSlash(filepath.Clean(params.Path))
	}

	// Use FilenameFromPath for virtual paths to get URL-decoded names
	// For regular paths, extract basename manually
	if helpers.ReURI.MatchString(params.Path) {
		// For URIs, FilenameFromPath returns the decoded last path segment, which may include an extension for http/s
		f.FileName = helpers.FilenameFromPath(f.Path)

		// Check the scheme to decide if we should extract an extension
		schemeEnd := strings.Index(f.Path, "://")
		scheme := ""
		if schemeEnd > 0 {
			scheme = strings.ToLower(f.Path[:schemeEnd])
		}

		if platformsshared.IsStandardSchemeForDecoding(scheme) {
			// For http/https, extract the extension for tag creation
			// ParseTitleFromFilename will strip it from the display title later
			ext := strings.ToLower(filepath.Ext(f.FileName))
			if helpers.IsValidExtension(ext) {
				f.Ext = ext
			} else {
				f.Ext = ""
			}
		} else {
			// For custom schemes (steam, kodi, etc.), there is no extension
			f.Ext = ""
		}
	} else {
		fileBase := filepath.Base(f.Path)
		// Skip extension extraction if params.NoExt is true or extract normally
		if params.NoExt {
			f.Ext = ""
		} else {
			f.Ext = strings.ToLower(filepath.Ext(f.Path))
			if !helpers.IsValidExtension(f.Ext) {
				f.Ext = ""
			}
		}
		f.FileName, _ = strings.CutSuffix(fileBase, f.Ext)
	}

	f.Title = tags.ParseTitleFromFilename(f.FileName, params.StripLeadingNumbers)

	// Look up the media type for this system to enable media-type-aware slugification
	mediaType := slugs.MediaTypeGame // Default to Game
	if params.SystemID != "" {
		if system, err := systemdefs.GetSystem(params.SystemID); err == nil {
			mediaType = system.GetMediaType()
		}
	}

	// Use media-type-aware slugification for TV shows, movies, music, etc.
	f.Slug = slugs.Slugify(mediaType, f.Title)

	// For non-Latin titles that don't produce a slug, store the lowercase
	// original title. This ensures Slug is never empty while the search
	// logic (mediadb.go) falls back to the Name field for these cases.
	if f.Slug == "" {
		f.Slug = strings.ToLower(f.FileName)
	}

	// Extract tags from filename only if enabled in config (default to enabled for nil config)
	if params.Config == nil || params.Config.FilenameTags() {
		f.Tags = getTagsFromFileName(f.FileName)
	} else {
		f.Tags = []string{}
	}

	return f
}
