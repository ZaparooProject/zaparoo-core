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
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// pathFragmentCache provides a simple LRU cache for parsed path fragments
// to avoid redundant regex operations and string processing
type pathFragmentCache struct {
	cache   map[string]*MediaPathFragments
	keys    []string
	maxSize int
	mu      sync.RWMutex
}

// globalPathFragmentCache is a package-level cache for path fragments
var globalPathFragmentCache = &pathFragmentCache{
	cache:   make(map[string]*MediaPathFragments, 5000),
	keys:    make([]string, 0, 5000),
	maxSize: 5000,
}

// get retrieves a cached path fragment if it exists
func (c *pathFragmentCache) get(path string) (MediaPathFragments, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	frag, ok := c.cache[path]
	if !ok {
		return MediaPathFragments{}, false
	}
	return *frag, true
}

// put stores a path fragment in the cache, evicting oldest entry if cache is full
func (c *pathFragmentCache) put(path string, frag *MediaPathFragments) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if _, exists := c.cache[path]; exists {
		return
	}

	// Evict oldest if at capacity
	if len(c.keys) >= c.maxSize {
		oldest := c.keys[0]
		delete(c.cache, oldest)
		c.keys = c.keys[1:]
	}

	c.cache[path] = frag
	c.keys = append(c.keys, path)
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
) (titleIndex, mediaIndex int, err error) {
	pf := GetPathFragments(path)

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

			log.Debug().Err(err).Msgf("system already exists: %s", systemID)

			// Try to get existing system ID from database when constraint violated
			existingSystem, getErr := db.FindSystemBySystemID(systemID)
			if getErr != nil || existingSystem.DBID == 0 {
				// If we can't get the system, we must fail properly
				return 0, 0, fmt.Errorf("failed to get existing system %s after insert failed: %w", systemID, getErr)
			}
			systemIndex = int(existingSystem.DBID)
			ss.SystemIDs[systemID] = systemIndex // Update cache with existing ID
			log.Debug().Msgf("Using existing system %s with DBID %d", systemID, systemIndex)
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
			log.Debug().Msgf("Using existing media title %s with DBID %d", pf.Title, titleIndex)
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
		})
		if err != nil {
			ss.MediaIndex-- // Rollback index increment on failure

			// Handle UNIQUE constraint violations gracefully - data may already exist from previous batches
			var sqliteErr sqlite3.Error
			if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
				log.Debug().Err(err).Msgf("media already exists: %s", pf.Path)
			} else {
				log.Error().Err(err).Msgf("error inserting media: %s", pf.Path)
			}
		} else {
			ss.MediaIDs[mediaKey] = mediaIndex // Only update cache on success
		}
	} else {
		mediaIndex = foundMediaIndex
	}

	if pf.Ext != "" {
		if _, ok := ss.TagIDs[pf.Ext]; !ok {
			// Get or create the Extension tag type ID dynamically
			extensionTypeID, found := ss.TagTypeIDs["Extension"]
			if !found {
				// Extension tag type doesn't exist in cache, try to look it up
				existingTagType, getErr := db.FindTagType(database.TagType{Type: "Extension"})
				if getErr != nil || existingTagType.DBID == 0 {
					return 0, 0, fmt.Errorf(
						"extension tag type not found and not in cache "+
							"(should not happen after SeedKnownTags): %w",
						getErr,
					)
				}
				extensionTypeID = int(existingTagType.DBID)
				ss.TagTypeIDs["Extension"] = extensionTypeID
			}

			ss.TagsIndex++
			tagIndex := ss.TagsIndex
			_, err := db.InsertTag(database.Tag{
				DBID:     int64(tagIndex),
				Tag:      pf.Ext,
				TypeDBID: int64(extensionTypeID),
			})
			if err != nil {
				ss.TagsIndex-- // Rollback index increment on failure

				// Handle UNIQUE constraint violations gracefully - find existing tag and add to map
				var sqliteErr sqlite3.Error
				if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
					log.Debug().Err(err).Msgf("tag Extension already exists: %s", pf.Ext)

					// Look up existing tag and add it to the map to prevent repeated insertion attempts
					existingTag, getErr := db.FindTag(database.Tag{Tag: pf.Ext})
					if getErr != nil || existingTag.DBID == 0 {
						log.Error().Err(getErr).Msgf("Failed to get existing tag %s after insert failed", pf.Ext)
					} else {
						ss.TagIDs[pf.Ext] = int(existingTag.DBID)
						log.Debug().Msgf("Using existing tag %s with DBID %d", pf.Ext, existingTag.DBID)
					}
				} else {
					log.Error().Err(err).Msgf("error inserting tag Extension: %s", pf.Ext)
				}
			} else {
				ss.TagIDs[pf.Ext] = tagIndex // Only update cache on success
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
	re := helpers.CachedMustCompile(`\(([\w,\- ]*)\)|\[([\w,\- ]*)]`)
	matches := re.FindAllString(filename, -1)
	tags := make([]string, 0)
	for _, padded := range matches {
		unpadded := padded[1 : len(padded)-1]
		split := strings.Split(unpadded, ",")
		for _, tag := range split {
			tags = append(tags, strings.ToLower(strings.TrimSpace(tag)))
		}
	}
	return tags
}

func getTitleFromFilename(filename string) string {
	r := helpers.CachedMustCompile(`^([^(\[]*)`)
	title := r.FindString(filename)
	return strings.TrimSpace(title)
}

func SeedKnownTags(db database.MediaDBI, ss *database.ScanState) error {
	typeMatches := map[string][]string{
		"Version": {
			"rev", "v",
		},
		"Language": {
			"ar", "bg", "bs", "cs", "cy", "da", "de", "el", "en", "eo", "es", "et",
			"fa", "fi", "fr", "ga", "gu", "he", "hi", "hr", "hu", "is", "it", "ja",
			"ko", "lt", "lv", "ms", "nl", "no", "pl", "pt", "ro", "ru", "sk", "sl",
			"sq", "sr", "sv", "th", "tr", "ur", "vi", "yi", "zh",
		},
		"Region": {
			// NOINTRO
			"world", "europe", "asia", "australia", "brazil", "canada", "china", "france",
			"germany", "hong kong", "italy", "japan", "korea", "netherlands", "spain",
			"sweden", "usa", "poland", "finland", "denmark", "portugal", "norway",
			// TOSEC
			"AE", "AL", "AS", "AT", "AU", "BA", "BE", "BG", "BR", "CA", "CH", "CL", "CN",
			"CS", "CY", "CZ", "DE", "DK", "EE", "EG", "ES", "EU", "FI", "FR", "GB", "GR",
			"HK", "HR", "HU", "ID", "IE", "IL", "IN", "IR", "IS", "IT", "JO", "JP", "KR",
			"LT", "LU", "LV", "MN", "MX", "MY", "NL", "NO", "NP", "NZ", "OM", "PE", "PH",
			"PL", "PT", "QA", "RO", "RU", "SE", "SG", "SI", "SK", "TH", "TR", "TW", "US",
			"VN", "YU", "ZA",
		},
		"Year": {
			"1970", "1971", "1972", "1973", "1974", "1975", "1976", "1977", "1978", "1979",
			"1980", "1981", "1982", "1983", "1984", "1985", "1986", "1987", "1988", "1989",
			"1990", "1991", "1992", "1993", "1994", "1995", "1996", "1997", "1998", "1999",
			"2000", "2001", "2002", "2003", "2004", "2005", "2006", "2007", "2008", "2009",
			"2010", "2011", "2012", "2013", "2014", "2015", "2016", "2017", "2018", "2019",
			"2020", "2021", "2022", "2023", "2024", "2025", "2026", "2027", "2028", "2029",
			"19xx", "197x", "198x", "199x", "20xx", "200x", "201x", "202x",
		},
		"Dev Status": {
			"alpha", "beta", "preview", "pre-release", "proto", "sample",
			"demo", "demo-kiosk", "demo-playable", "demo-rolling", "demo-slideshow",
		},
		"Media Type": {
			"disc", "disk", "file", "part", "side", "tape",
		},
		"TOSEC System": {
			"+2", "+2a", "+3", "130XE", "A1000", "A1200", "A1200-A4000", "A2000",
			"A2000-A3000", "A2024", "A2500-A3000UX", "A3000", "A4000", "A4000T",
			"A500", "A500+", "A500-A1000-A2000", "A500-A1000-A2000-CDTV", "A500-A1200",
			"A500-A1200-A2000-A4000", "A500-A2000", "A500-A600-A2000", "A570", "A600",
			"A600HD", "AGA", "AGA-CD32", "Aladdin Deck Enhancer", "CD32", "CDTV",
			"Computrainer", "Doctor PC Jr.", "ECS", "ECS-AGA", "Executive", "Mega ST",
			"Mega-STE", "OCS", "OCS-AGA", "ORCH80", "Osbourne 1", "PIANO90",
			"PlayChoice-10", "Plus4", "Primo-A", "Primo-A64", "Primo-B", "Primo-B64",
			"Pro-Primo", "ST", "STE", "STE-Falcon", "TT", "TURBO-R GT", "TURBO-R ST",
			"VS DualSystem", "VS UniSystem",
		},
		"TOSEC Video": {
			"CGA", "EGA", "HGC", "MCGA", "MDA", "NTSC", "NTSC-PAL", "PAL", "PAL-60",
			"PAL-NTSC", "SVGA", "VGA", "XGA",
		},
		"TOSEC Copyright": {
			"CW", "CW-R", "FW", "GW", "GW-R", "LW", "PD", "SW", "SW-R",
		},
		"TOSEC Dump Info": {
			"cr", "f", "h", "m", "p", "t", "tr", "o", "u", "v", "b", "a", "!",
		},
	}

	ss.TagTypesIndex++
	_, err := db.InsertTagType(database.TagType{
		DBID: int64(ss.TagTypesIndex),
		Type: "Unknown",
	})
	if err != nil {
		ss.TagTypesIndex-- // Rollback index increment on failure

		// Handle UNIQUE constraint violations gracefully - data may already exist
		var sqliteErr sqlite3.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
			return fmt.Errorf("error inserting tag type Unknown: %w", err)
		}
		log.Debug().Msg("Tag type 'Unknown' already exists, continuing")
		// Try to get the existing tag type to update our index
		existingTagType, getErr := db.FindTagType(database.TagType{Type: "Unknown"})
		if getErr == nil && existingTagType.DBID > 0 {
			ss.TagTypesIndex = int(existingTagType.DBID)
			ss.TagTypeIDs["Unknown"] = ss.TagTypesIndex
		}
	} else {
		ss.TagTypeIDs["Unknown"] = ss.TagTypesIndex
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
		log.Debug().Msg("Tag 'unknown' already exists, continuing")
		// Try to get the existing tag to update our index
		existingTag, getErr := db.FindTag(database.Tag{Tag: "unknown"})
		if getErr == nil && existingTag.DBID > 0 {
			ss.TagsIndex = int(existingTag.DBID)
			ss.TagIDs["unknown"] = ss.TagsIndex
		}
	} else {
		ss.TagIDs["unknown"] = ss.TagsIndex
	}

	ss.TagTypesIndex++
	_, err = db.InsertTagType(database.TagType{
		DBID: int64(ss.TagTypesIndex),
		Type: "Extension",
	})
	if err != nil {
		ss.TagTypesIndex-- // Rollback index increment on failure

		// Handle UNIQUE constraint violations gracefully
		var sqliteErr sqlite3.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
			return fmt.Errorf("error inserting tag type Extension: %w", err)
		}
		log.Debug().Msg("Tag type 'Extension' already exists, continuing")
		// Try to get the existing tag type to update our index
		existingTagType, getErr := db.FindTagType(database.TagType{Type: "Extension"})
		if getErr == nil && existingTagType.DBID > 0 {
			ss.TagTypesIndex = int(existingTagType.DBID)
			ss.TagTypeIDs["Extension"] = ss.TagTypesIndex
		}
	} else {
		ss.TagTypeIDs["Extension"] = ss.TagTypesIndex
	}

	ss.TagsIndex++
	_, err = db.InsertTag(database.Tag{
		DBID:     int64(ss.TagsIndex),
		Tag:      ".ext",
		TypeDBID: int64(ss.TagTypesIndex),
	})
	if err != nil {
		ss.TagsIndex-- // Rollback index increment on failure

		// Handle UNIQUE constraint violations gracefully
		var sqliteErr sqlite3.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
			return fmt.Errorf("error inserting tag .ext: %w", err)
		}
		log.Debug().Msg("Tag '.ext' already exists, continuing")
		// Try to get the existing tag to update our index
		existingTag, getErr := db.FindTag(database.Tag{Tag: ".ext"})
		if getErr == nil && existingTag.DBID > 0 {
			ss.TagsIndex = int(existingTag.DBID)
			ss.TagIDs[".ext"] = ss.TagsIndex
		}
	} else {
		ss.TagIDs[".ext"] = ss.TagsIndex
	}

	for typeStr, tags := range typeMatches {
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
			log.Debug().Msgf("Tag type '%s' already exists, continuing", typeStr)
			// Try to get the existing tag type to update our index
			existingTagType, getErr := db.FindTagType(database.TagType{Type: typeStr})
			if getErr == nil && existingTagType.DBID > 0 {
				ss.TagTypesIndex = int(existingTagType.DBID)
				ss.TagTypeIDs[typeStr] = ss.TagTypesIndex
			}
		} else {
			ss.TagTypeIDs[typeStr] = ss.TagTypesIndex
		}

		for _, tag := range tags {
			ss.TagsIndex++
			_, err := db.InsertTag(database.Tag{
				DBID:     int64(ss.TagsIndex),
				Tag:      strings.ToLower(tag),
				TypeDBID: int64(ss.TagTypesIndex),
			})
			if err != nil {
				ss.TagsIndex-- // Rollback index increment on failure

				// Handle UNIQUE constraint violations gracefully
				var sqliteErr sqlite3.Error
				if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
					return fmt.Errorf("error inserting tag %s: %w", tag, err)
				}
				log.Debug().Msgf("Tag '%s' already exists, continuing", tag)
				// Try to get the existing tag to update our index
				existingTag, getErr := db.FindTag(database.Tag{Tag: strings.ToLower(tag)})
				if getErr == nil && existingTag.DBID > 0 {
					ss.TagsIndex = int(existingTag.DBID)
					ss.TagIDs[strings.ToLower(tag)] = ss.TagsIndex
				}
			} else {
				ss.TagIDs[strings.ToLower(tag)] = ss.TagsIndex
			}
		}
	}
	return nil
}

// PopulateScanStateFromDB initializes the scan state with existing database IDs
// when resuming an interrupted indexing operation
func PopulateScanStateFromDB(db database.MediaDBI, ss *database.ScanState) error {
	// Get max IDs from existing data to continue indexing from the right point
	maxSystemID, err := db.GetMaxSystemID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max system ID, starting from 0")
		maxSystemID = 0
	}
	ss.SystemsIndex = int(maxSystemID)

	maxTitleID, err := db.GetMaxTitleID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max title ID, starting from 0")
		maxTitleID = 0
	}
	ss.TitlesIndex = int(maxTitleID)

	maxMediaID, err := db.GetMaxMediaID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max media ID, starting from 0")
		maxMediaID = 0
	}
	ss.MediaIndex = int(maxMediaID)

	maxTagTypeID, err := db.GetMaxTagTypeID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max tag type ID, starting from 0")
		maxTagTypeID = 0
	}
	ss.TagTypesIndex = int(maxTagTypeID)

	maxTagID, err := db.GetMaxTagID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max tag ID, starting from 0")
		maxTagID = 0
	}
	ss.TagsIndex = int(maxTagID)

	// Populate maps with existing data to prevent duplicate insertion attempts
	// This is crucial for resuming indexing operations

	// First populate systems map
	systems, err := db.GetAllSystems()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get existing systems, maps may be incomplete")
	} else {
		for _, system := range systems {
			ss.SystemIDs[system.SystemID] = int(system.DBID)
		}
	}

	titlesWithSystems, err := db.GetTitlesWithSystems()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get existing titles with systems, maps may be incomplete")
	} else {
		for _, title := range titlesWithSystems {
			// Direct construction - SystemID is already available from the JOIN
			titleKey := title.SystemID + ":" + title.Slug
			ss.TitleIDs[titleKey] = int(title.DBID)
		}
	}

	mediaWithFullPath, err := db.GetMediaWithFullPath()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get existing media with full path, maps may be incomplete")
	} else {
		for _, m := range mediaWithFullPath {
			// Direct construction - SystemID is already available from the JOIN
			mediaKey := m.SystemID + ":" + m.Path
			ss.MediaIDs[mediaKey] = int(m.DBID)
		}
	}

	// Populate tag types map
	tagTypes, err := db.GetAllTagTypes()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get existing tag types, maps may be incomplete")
	} else {
		for _, tagType := range tagTypes {
			ss.TagTypeIDs[tagType.Type] = int(tagType.DBID)
		}
	}

	// Populate tags map
	tags, err := db.GetAllTags()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get existing tags, maps may be incomplete")
	} else {
		for _, tag := range tags {
			ss.TagIDs[tag.Tag] = int(tag.DBID)
		}
	}

	log.Debug().Msgf("Populated scan state from DB: Sys=%d, Titles=%d, Media=%d, TagTypes=%d, Tags=%d "+
		"(maps: TagTypes=%d, Tags=%d)",
		ss.SystemsIndex, ss.TitlesIndex, ss.MediaIndex,
		ss.TagTypesIndex, ss.TagsIndex, len(ss.TagTypeIDs), len(ss.TagIDs))

	return nil
}

// PopulateScanStateForSelectiveIndexing populates scan state for selective indexing with optimized loading.
// Uses true lazy loading for Systems/TagTypes (via UNIQUE constraints) and minimal data loading
// for MediaTitles/Media (only systems NOT being reindexed) to dramatically improve performance.
func PopulateScanStateForSelectiveIndexing(
	db database.MediaDBI, ss *database.ScanState, systemsToReindex []string,
) error {
	// Get max IDs from existing data to continue indexing from the right point
	maxSystemID, err := db.GetMaxSystemID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max system ID, starting from 0")
		maxSystemID = 0
	}
	ss.SystemsIndex = int(maxSystemID)

	maxTitleID, err := db.GetMaxTitleID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max title ID, starting from 0")
		maxTitleID = 0
	}
	ss.TitlesIndex = int(maxTitleID)

	maxMediaID, err := db.GetMaxMediaID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max media ID, starting from 0")
		maxMediaID = 0
	}
	ss.MediaIndex = int(maxMediaID)

	maxTagTypeID, err := db.GetMaxTagTypeID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max tag type ID, starting from 0")
		maxTagTypeID = 0
	}
	ss.TagTypesIndex = int(maxTagTypeID)

	maxTagID, err := db.GetMaxTagID()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get max tag ID, starting from 0")
		maxTagID = 0
	}
	ss.TagsIndex = int(maxTagID)

	// Use pure lazy loading for Systems and TagTypes (they have UNIQUE constraints)
	// Empty maps will be populated on-demand when UNIQUE constraint violations occur
	// This is handled automatically in AddMediaPath for these tables

	// For MediaTitles and Media, we need to maintain maps since they don't have
	// unique constraints on the columns we insert (no constraint on SystemDBID+Slug or Path)
	// But we optimize by only loading data for systems NOT being reindexed

	// Populate titles map (only for systems not being reindexed)
	// Use optimized query that excludes systems being reindexed
	titlesWithSystems, err := db.GetTitlesWithSystemsExcluding(systemsToReindex)
	if err != nil {
		log.Warn().Err(err).Msg(
			"failed to get existing titles with systems (excluding reindexed), " +
				"maps may be incomplete",
		)
	} else {
		for _, title := range titlesWithSystems {
			titleKey := title.SystemID + ":" + title.Slug
			ss.TitleIDs[titleKey] = int(title.DBID)
		}
	}

	// Populate media map (only for systems not being reindexed)
	// Use optimized query that excludes systems being reindexed
	mediaWithFullPath, err := db.GetMediaWithFullPathExcluding(systemsToReindex)
	if err != nil {
		log.Warn().Err(err).Msg(
			"failed to get existing media with full path (excluding reindexed), " +
				"maps may be incomplete",
		)
	} else {
		for _, m := range mediaWithFullPath {
			mediaKey := m.SystemID + ":" + m.Path
			ss.MediaIDs[mediaKey] = int(m.DBID)
		}
	}

	// For selective indexing, use true lazy loading for Systems, TagTypes, and Tags
	// This dramatically reduces memory usage and startup time by:
	// 1. Not pre-loading Systems/TagTypes (use UNIQUE constraint handling)
	// 2. Not pre-loading any Tags (handled by constraint violations)
	// 3. Only loading MediaTitles/Media for systems NOT being reindexed

	log.Debug().Msgf("Populated scan state for selective indexing: "+
		"MaxIDs: Sys=%d, Titles=%d, Media=%d, TagTypes=%d, Tags=%d; "+
		"Maps: Sys=%d, Titles=%d, Media=%d, TagTypes=%d, Tags=%d; Systems to reindex: %v",
		ss.SystemsIndex, ss.TitlesIndex, ss.MediaIndex,
		ss.TagTypesIndex, ss.TagsIndex, len(ss.SystemIDs), len(ss.TitleIDs), len(ss.MediaIDs),
		len(ss.TagTypeIDs), len(ss.TagIDs), systemsToReindex)

	return nil
}

func GetPathFragments(path string) MediaPathFragments {
	// Check cache first
	if frag, ok := globalPathFragmentCache.get(path); ok {
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

	f.Ext = strings.ToLower(filepath.Ext(f.Path))
	if helpers.HasSpace(f.Ext) {
		f.Ext = ""
	}

	f.FileName, _ = strings.CutSuffix(fileBase, f.Ext)

	f.Title = getTitleFromFilename(f.FileName)
	f.Slug = helpers.SlugifyString(f.Title)
	f.Tags = getTagsFromFileName(f.FileName)

	// Store in cache for future use
	globalPathFragmentCache.put(path, &f)

	return f
}
