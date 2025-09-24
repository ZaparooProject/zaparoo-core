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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// We can't batch effectively without a sense of relationships
// Instead of indexing string columns, use an in-memory map to track records to
// insert IDs.
// Batching should be able to run with an assumed IDs
// database.ScanState and DB transactions allow accumulation

func FlushScanStateMaps(ss *database.ScanState) {
	ss.SystemIDs = make(map[string]int)
	ss.TitleIDs = make(map[string]int)
	ss.MediaIDs = make(map[string]int)
	// Note: TagIDs and TagTypeIDs are preserved across batches for performance
	// since tags are typically reused across different systems
}

func AddMediaPath(
	db database.MediaDBI,
	ss *database.ScanState,
	systemID string,
	path string,
) (titleIndex, mediaIndex int) {
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
			log.Error().Err(err).Msgf("error inserting system: %s", systemID)

			// Only attempt recovery for UNIQUE constraint violations
			// Other errors (connection issues, etc.) should fail fast
			var sqliteErr sqlite3.Error
			if !errors.As(err, &sqliteErr) || sqliteErr.ExtendedCode != sqlite3.ErrConstraintUnique {
				return 0, 0
			}

			// Try to get existing system ID from database when constraint violated
			// Set DBID to -1 to ensure we only search by SystemID, not DBID=0
			existingSystem, getErr := db.FindSystem(database.System{
				DBID:     -1, // Use -1 to avoid matching DBID=0
				SystemID: systemID,
			})
			if getErr != nil || existingSystem.DBID == 0 {
				// If we can't get the system, we must fail properly
				log.Error().Err(getErr).Msgf("Failed to get existing system %s after insert failed", systemID)
				return 0, 0 // Return early to prevent invalid data
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

	titleKey := fmt.Sprintf("%v:%v", systemID, pf.Slug)
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
			log.Error().Err(err).Msgf("error inserting media title: %s", pf.Title)
		} else {
			ss.TitleIDs[titleKey] = titleIndex // Only update cache on success
		}
	} else {
		titleIndex = foundTitleIndex
	}

	mediaKey := fmt.Sprintf("%v:%v", systemID, pf.Path)
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
			log.Error().Err(err).Msgf("error inserting media: %s", pf.Path)
		} else {
			ss.MediaIDs[mediaKey] = mediaIndex // Only update cache on success
		}
	} else {
		mediaIndex = foundMediaIndex
	}

	if pf.Ext != "" {
		if _, ok := ss.TagIDs[pf.Ext]; !ok {
			ss.TagsIndex++
			tagIndex := ss.TagsIndex
			_, err := db.InsertTag(database.Tag{
				DBID:     int64(tagIndex),
				Tag:      pf.Ext,
				TypeDBID: int64(2),
			})
			if err != nil {
				ss.TagsIndex-- // Rollback index increment on failure
				log.Error().Err(err).Msgf("error inserting tag Extension: %s", pf.Ext)
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
	return titleIndex, mediaIndex
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
			titleKey := fmt.Sprintf("%s:%s", title.SystemID, title.Slug)
			ss.TitleIDs[titleKey] = int(title.DBID)
		}
	}

	mediaWithFullPath, err := db.GetMediaWithFullPath()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get existing media with full path, maps may be incomplete")
	} else {
		for _, m := range mediaWithFullPath {
			// Direct construction - SystemID is already available from the JOIN
			mediaKey := fmt.Sprintf("%s:%s", m.SystemID, m.Path)
			ss.MediaIDs[mediaKey] = int(m.DBID)
		}
	}

	log.Debug().Msgf("Populated scan state from DB: Sys=%d, Titles=%d, Media=%d, TagTypes=%d, Tags=%d",
		ss.SystemsIndex, ss.TitlesIndex, ss.MediaIndex,
		ss.TagTypesIndex, ss.TagsIndex)

	return nil
}

func GetPathFragments(path string) MediaPathFragments {
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

	return f
}
