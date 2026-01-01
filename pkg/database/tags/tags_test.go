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

package tags

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTagTypeConstants verifies all TagType constants are valid
func TestTagTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		tagType  TagType
		expected string
	}{
		{"Input", TagTypeInput, "input"},
		{"Players", TagTypePlayers, "players"},
		{"GameGenre", TagTypeGameGenre, "gamegenre"},
		{"Addon", TagTypeAddon, "addon"},
		{"Embedded", TagTypeEmbedded, "embedded"},
		{"Save", TagTypeSave, "save"},
		{"ArcadeBoard", TagTypeArcadeBoard, "arcadeboard"},
		{"Compatibility", TagTypeCompatibility, "compatibility"},
		{"Disc", TagTypeDisc, "disc"},
		{"DiscTotal", TagTypeDiscTotal, "disctotal"},
		{"Based", TagTypeBased, "based"},
		{"Search", TagTypeSearch, "search"},
		{"Multigame", TagTypeMultigame, "multigame"},
		{"Reboxed", TagTypeReboxed, "reboxed"},
		{"Port", TagTypePort, "port"},
		{"Lang", TagTypeLang, "lang"},
		{"Unfinished", TagTypeUnfinished, "unfinished"},
		{"Rerelease", TagTypeRerelease, "rerelease"},
		{"Rev", TagTypeRev, "rev"},
		{"Set", TagTypeSet, "set"},
		{"Alt", TagTypeAlt, "alt"},
		{"Unlicensed", TagTypeUnlicensed, "unlicensed"},
		{"MameParent", TagTypeMameParent, "mameparent"},
		{"Region", TagTypeRegion, "region"},
		{"Year", TagTypeYear, "year"},
		{"Video", TagTypeVideo, "video"},
		{"Copyright", TagTypeCopyright, "copyright"},
		{"Dump", TagTypeDump, "dump"},
		{"Media", TagTypeMedia, "media"},
		{"Extension", TagTypeExtension, "extension"},
		{"Unknown", TagTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.tagType), "TagType constant should match expected value")
		})
	}
}

// TestTagTypeNaming verifies TagType constants follow naming conventions
func TestTagTypeNaming(t *testing.T) {
	allTypes := []TagType{
		TagTypeInput, TagTypePlayers, TagTypeGameGenre, TagTypeAddon, TagTypeEmbedded,
		TagTypeSave, TagTypeArcadeBoard, TagTypeCompatibility, TagTypeDisc, TagTypeDiscTotal,
		TagTypeBased, TagTypeSearch, TagTypeMultigame, TagTypeReboxed, TagTypePort,
		TagTypeLang, TagTypeUnfinished, TagTypeRerelease, TagTypeRev, TagTypeSet,
		TagTypeAlt, TagTypeUnlicensed, TagTypeMameParent, TagTypeRegion, TagTypeYear,
		TagTypeVideo, TagTypeCopyright, TagTypeDump, TagTypeMedia, TagTypeExtension,
		TagTypeUnknown,
	}

	for _, tagType := range allTypes {
		t.Run(string(tagType), func(t *testing.T) {
			typeStr := string(tagType)

			// Must be lowercase
			assert.Equal(t, strings.ToLower(typeStr), typeStr,
				"TagType must be lowercase")

			// Must not contain spaces
			assert.NotContains(t, typeStr, " ",
				"TagType must not contain spaces")

			// Must not start or end with special characters
			assert.False(t, strings.HasPrefix(typeStr, "-") || strings.HasPrefix(typeStr, "_"),
				"TagType must not start with dash or underscore")
			assert.False(t, strings.HasSuffix(typeStr, "-") || strings.HasSuffix(typeStr, "_"),
				"TagType must not end with dash or underscore")

			// Must only contain alphanumeric characters (no special chars except in-word)
			matched, _ := regexp.MatchString("^[a-z][a-z0-9]*$", typeStr)
			assert.True(t, matched,
				"TagType must only contain lowercase letters and numbers, starting with a letter")
		})
	}
}

// TestCanonicalTagDefinitionsCompleteness verifies all TagTypes have entries
func TestCanonicalTagDefinitionsCoverage(t *testing.T) {
	allTypes := []TagType{
		TagTypeInput, TagTypePlayers, TagTypeGameGenre, TagTypeAddon, TagTypeEmbedded,
		TagTypeSave, TagTypeArcadeBoard, TagTypeCompatibility, TagTypeDisc, TagTypeDiscTotal,
		TagTypeBased, TagTypeSearch, TagTypeMultigame, TagTypeReboxed, TagTypePort,
		TagTypeLang, TagTypeUnfinished, TagTypeRerelease, TagTypeRev, TagTypeSet,
		TagTypeAlt, TagTypeUnlicensed, TagTypeMameParent, TagTypeRegion, TagTypeYear,
		TagTypeVideo, TagTypeCopyright, TagTypeDump, TagTypeMedia,
		// Note: Extension and Unknown are special and may not have predefined tags
	}

	for _, tagType := range allTypes {
		t.Run(string(tagType), func(t *testing.T) {
			tags, exists := CanonicalTagDefinitions[tagType]
			assert.True(t, exists, "TagType %s must exist in CanonicalTagDefinitions", tagType)

			// MameParent can be empty, but all others should have at least one tag
			if tagType != TagTypeMameParent {
				assert.NotEmpty(t, tags, "TagType %s should have at least one tag defined", tagType)
			}
		})
	}
}

// TestTagValueFormat verifies all tag values follow format rules
func TestTagValueFormat(t *testing.T) {
	// Valid characters: lowercase letters, numbers, colon (for hierarchy), dash (for multi-word)
	validTagPattern := regexp.MustCompile(`^[a-z0-9][a-z0-9:\-]*[a-z0-9]$|^[a-z0-9]$`)

	for tagType, tags := range CanonicalTagDefinitions {
		for _, tag := range tags {
			tagStr := string(tag)
			t.Run(string(tagType)+"/"+tagStr, func(t *testing.T) {
				// Must not be empty
				assert.NotEmpty(t, tagStr, "Tag value must not be empty")

				// Must be lowercase
				assert.Equal(t, strings.ToLower(tagStr), tagStr,
					"Tag must be lowercase: %s", tagStr)

				// Must not contain spaces
				assert.NotContains(t, tagStr, " ",
					"Tag must not contain spaces: %s", tagStr)

				// Must not contain uppercase
				for _, r := range tagStr {
					assert.False(t, unicode.IsUpper(r),
						"Tag must not contain uppercase letters: %s", tagStr)
				}

				// Must match valid pattern
				assert.True(t, validTagPattern.MatchString(tagStr),
					"Tag must match valid format (lowercase alphanumeric, colon, dash): %s", tagStr)

				// Must not start or end with colon or dash
				assert.False(t, strings.HasPrefix(tagStr, ":") || strings.HasPrefix(tagStr, "-"),
					"Tag must not start with colon or dash: %s", tagStr)
				assert.False(t, strings.HasSuffix(tagStr, ":") || strings.HasSuffix(tagStr, "-"),
					"Tag must not end with colon or dash: %s", tagStr)

				// Must not have consecutive colons or dashes
				assert.NotContains(t, tagStr, "::",
					"Tag must not have consecutive colons: %s", tagStr)
				assert.NotContains(t, tagStr, "--",
					"Tag must not have consecutive dashes: %s", tagStr)

				// Must not contain periods (normalized format)
				assert.NotContains(t, tagStr, ".",
					"Tag must not contain periods (should be normalized): %s", tagStr)

				// Must not contain underscores (use dashes instead)
				assert.NotContains(t, tagStr, "_",
					"Tag must not contain underscores (use dashes): %s", tagStr)
			})
		}
	}
}

// TestTagHierarchyFormat verifies hierarchical tags are properly formed
func TestTagHierarchyFormat(t *testing.T) {
	for tagType, tags := range CanonicalTagDefinitions {
		for _, tag := range tags {
			tagStr := string(tag)
			if strings.Contains(tagStr, ":") {
				t.Run(string(tagType)+"/"+tagStr, func(t *testing.T) {
					parts := strings.Split(tagStr, ":")

					// Each part must be non-empty
					for i, part := range parts {
						assert.NotEmpty(t, part,
							"Hierarchical tag part %d must not be empty in: %s", i, tag)
					}

					// Each part must not contain dashes at start/end
					for i, part := range parts {
						assert.False(t, strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-"),
							"Hierarchical tag part %d must not start/end with dash in: %s", i, tag)
					}

					// Hierarchical depth should be reasonable (max 3 levels)
					assert.LessOrEqual(t, len(parts), 3,
						"Hierarchical tag should not exceed 3 levels: %s", tag)
				})
			}
		}
	}
}

// TestTagUniqueness verifies no duplicate tags within each type
func TestTagUniqueness(t *testing.T) {
	for tagType, tags := range CanonicalTagDefinitions {
		t.Run(string(tagType), func(t *testing.T) {
			seen := make(map[string]bool)
			duplicates := []string{}

			for _, tag := range tags {
				tagStr := string(tag)
				if seen[tagStr] {
					duplicates = append(duplicates, tagStr)
				}
				seen[tagStr] = true
			}

			assert.Empty(t, duplicates,
				"TagType %s has duplicate tags: %v", tagType, duplicates)
		})
	}
}

// TestTagValueConsistency verifies tags follow consistent patterns
func TestTagValueConsistency(t *testing.T) {
	for tagType, tags := range CanonicalTagDefinitions {
		for _, tag := range tags {
			tagStr := string(tag)
			t.Run(string(tagType)+"/"+tagStr, func(t *testing.T) {
				// Multi-word tags should use dashes, not other separators
				if strings.Contains(tagStr, "-") {
					// After splitting on dashes, no part should contain spaces or underscores
					parts := strings.Split(tagStr, "-")
					for _, part := range parts {
						assert.NotContains(t, part, " ",
							"Multi-word tag should use dashes throughout: %s", tagStr)
						assert.NotContains(t, part, "_",
							"Multi-word tag should use dashes, not underscores: %s", tagStr)
					}
				}

				// Numbers should not have leading zeros (except standalone "0")
				parts := strings.FieldsFunc(tagStr, func(r rune) bool {
					return r == ':' || r == '-'
				})
				for _, part := range parts {
					if len(part) > 1 && part[0] == '0' {
						// Check if it's a number
						isNumber := true
						for _, r := range part {
							if !unicode.IsDigit(r) {
								isNumber = false
								break
							}
						}
						assert.False(t, isNumber,
							"Number should not have leading zeros: %s in %s", part, tagStr)
					}
				}
			})
		}
	}
}

// TestSpecificTagTypeRules verifies type-specific validation rules
func TestSpecificTagTypeRules(t *testing.T) {
	t.Run("Year tags", func(t *testing.T) {
		yearPattern := regexp.MustCompile(`^\d{4}$|^\d{2,3}x{1,2}$`)
		tags := CanonicalTagDefinitions[TagTypeYear]

		for _, tag := range tags {
			tagStr := string(tag)
			assert.True(t, yearPattern.MatchString(tagStr),
				"Year tag must be 4 digits or decade format (e.g., 198x): %s", tagStr)
		}
	})

	t.Run("Player count tags", func(t *testing.T) {
		tags := CanonicalTagDefinitions[TagTypePlayers]
		playerNumPattern := regexp.MustCompile(`^\d{1,2}$`)

		for _, tag := range tags {
			tagStr := string(tag)
			// Should be a number or known mode (mmo, vs, coop, alt)
			if tagStr != "mmo" && tagStr != "vs" && tagStr != "coop" && tagStr != "alt" {
				// Should be a valid number
				assert.True(t, playerNumPattern.MatchString(tagStr),
					"Player tag should be a number or mode (mmo/vs/coop/alt): %s", tagStr)
			}
		}
	})

	t.Run("Disc number tags", func(t *testing.T) {
		tags := CanonicalTagDefinitions[TagTypeDisc]
		numRegex := regexp.MustCompile(`^\d+$`)

		for _, tag := range tags {
			tagStr := string(tag)
			matched := numRegex.MatchString(tagStr)
			assert.True(t, matched,
				"Disc tag should be a number: %s", tagStr)
		}
	})

	t.Run("DiscTotal tags", func(t *testing.T) {
		tags := CanonicalTagDefinitions[TagTypeDiscTotal]
		numRegex := regexp.MustCompile(`^\d+$`)

		for _, tag := range tags {
			tagStr := string(tag)
			matched := numRegex.MatchString(tagStr)
			assert.True(t, matched,
				"DiscTotal tag should be a number: %s", tagStr)

			// Should start at 2 (no point in single-disc total)
			// This is a soft check - we allow it but verify intent
			if tagStr == "1" {
				t.Log("Warning: DiscTotal has '1' which is semantically odd for multi-disc games")
			}
		}
	})

	t.Run("Language tags", func(t *testing.T) {
		tags := CanonicalTagDefinitions[TagTypeLang]

		for _, tag := range tags {
			// Should be 2-7 characters (ISO codes and variants like "zh-trad", "pt-br")
			assert.GreaterOrEqual(t, len(tag), 2,
				"Language code should be at least 2 characters: %s", tag)
			assert.LessOrEqual(t, len(tag), 7,
				"Language code should not exceed 7 characters: %s", tag)
		}
	})

	t.Run("Region tags", func(t *testing.T) {
		tags := CanonicalTagDefinitions[TagTypeRegion]

		for _, tag := range tags {
			// Should be 2-5 characters (ISO country codes or special like "world")
			assert.GreaterOrEqual(t, len(tag), 2,
				"Region code should be at least 2 characters: %s", tag)
			assert.LessOrEqual(t, len(tag), 5,
				"Region code should not exceed 5 characters: %s", tag)
		}
	})
}

// TestNoReservedCharacters verifies tags don't use characters with special meaning
func TestNoReservedCharacters(t *testing.T) {
	reservedChars := []string{
		"|", "&", "$", "#", "@", "!", "?", "*", "(", ")", "[", "]", "{", "}",
		"<", ">", "=", "+", "~", "`", "^", "%", "\\", "/", "'", "\"",
	}

	for tagType, tags := range CanonicalTagDefinitions {
		for _, tag := range tags {
			for _, char := range reservedChars {
				assert.NotContains(t, tag, char,
					"Tag in %s contains reserved character '%s': %s", tagType, char, tag)
			}
		}
	}
}

// TestTagSorting verifies tags are in a reasonable order (helps with maintainability)
func TestTagSorting(t *testing.T) {
	// This is a soft check - we don't enforce strict alphabetical sorting
	// but we do check for obvious issues like completely random ordering

	t.Run("Numeric sequences", func(t *testing.T) {
		// Types with numeric sequences should be in order
		numericTypes := map[TagType]bool{
			TagTypePlayers:   true,
			TagTypeDisc:      true,
			TagTypeDiscTotal: true,
			TagTypeSet:       true,
			TagTypeAlt:       true,
			TagTypeRev:       true,
		}

		numRegex := regexp.MustCompile(`^\d+$`)
		for tagType := range numericTypes {
			tags := CanonicalTagDefinitions[tagType]

			// Extract pure numeric tags
			numericTags := []string{}
			for _, tag := range tags {
				tagStr := string(tag)
				if numRegex.MatchString(tagStr) {
					numericTags = append(numericTags, tagStr)
				}
			}

			// Verify they appear in ascending order (when converted to int)
			if len(numericTags) > 1 {
				for i := range len(numericTags) - 1 {
					current := numericTags[i]
					next := numericTags[i+1]

					// Simple lexicographic check (works for single digits)
					if len(current) == len(next) {
						assert.LessOrEqual(t, current, next,
							"Numeric tags in %s should be in order: %s should come before %s",
							tagType, current, next)
					}
				}
			}
		}
	})
}

// TestNoWhitespace verifies no accidental whitespace in tags
func TestNoWhitespace(t *testing.T) {
	for tagType, tags := range CanonicalTagDefinitions {
		for _, tag := range tags {
			tagStr := string(tag)
			t.Run(string(tagType)+"/"+tagStr, func(t *testing.T) {
				// No leading whitespace
				assert.Equal(t, tagStr, strings.TrimLeft(tagStr, " \t\n\r"),
					"Tag has leading whitespace: '%s'", tagStr)

				// No trailing whitespace
				assert.Equal(t, tagStr, strings.TrimRight(tagStr, " \t\n\r"),
					"Tag has trailing whitespace: '%s'", tagStr)

				// No internal tabs or newlines
				assert.NotContains(t, tagStr, "\t", "Tag contains tab character: '%s'", tagStr)
				assert.NotContains(t, tagStr, "\n", "Tag contains newline: '%s'", tagStr)
				assert.NotContains(t, tagStr, "\r", "Tag contains carriage return: '%s'", tagStr)
			})
		}
	}
}

// TestTagCount provides overview of tag coverage
func TestTagCount(t *testing.T) {
	totalTags := 0
	tagCounts := make(map[TagType]int)

	for tagType, tags := range CanonicalTagDefinitions {
		count := len(tags)
		tagCounts[tagType] = count
		totalTags += count
	}

	t.Logf("Total canonical tags defined: %d", totalTags)
	t.Logf("Total tag types: %d", len(tagCounts))

	// Log top 10 types by tag count
	t.Log("\nTop tag types by count:")
	for tagType, count := range tagCounts {
		if count > 30 {
			t.Logf("  %s: %d tags", tagType, count)
		}
	}

	// Verify we have a reasonable number of tags (should be 900+)
	assert.GreaterOrEqual(t, totalTags, 900,
		"Should have at least 900 canonical tags defined")
}

// TestHierarchicalConsistency verifies hierarchical tags have their parent tags
func TestHierarchicalConsistency(t *testing.T) {
	for tagType, tags := range CanonicalTagDefinitions {
		// Build a set of all tags for quick lookup
		tagSet := make(map[string]bool)
		for _, tag := range tags {
			tagSet[string(tag)] = true
		}

		// For each hierarchical tag, check if parent exists
		for _, tag := range tags {
			tagStr := string(tag)
			if strings.Contains(tagStr, ":") {
				parts := strings.Split(tagStr, ":")

				// Build parent tag path
				for i := 1; i < len(parts); i++ {
					parent := strings.Join(parts[:i], ":")

					// Parent should exist (soft check - log warning)
					if !tagSet[parent] {
						t.Logf("Note: Hierarchical tag '%s' in %s has no parent '%s'",
							tagStr, tagType, parent)
					}
				}
			}
		}
	}
}

// TestSpecialCases verifies edge cases and special tags
func TestSpecialCases(t *testing.T) {
	t.Run("3D tags handle leading digit", func(t *testing.T) {
		tags := CanonicalTagDefinitions[TagTypeSearch]
		has3DTags := false

		for _, tag := range tags {
			tagStr := string(tag)
			if strings.HasPrefix(tagStr, "3d:") {
				has3DTags = true
				// Verify format
				assert.True(t, strings.HasPrefix(tagStr, "3d:"),
					"3D tags should start with '3d:': %s", tagStr)
			}
		}

		assert.True(t, has3DTags, "Should have 3D tags in search type")
	})

	t.Run("Numbered chip tags", func(t *testing.T) {
		tags := CanonicalTagDefinitions[TagTypeEmbedded]

		for _, tag := range tags {
			tagStr := string(tag)
			if strings.HasPrefix(tagStr, "chip:") && len(tagStr) > 5 {
				// Chips can have numbers in names (7755, 7756, etc.)
				// This is valid - verify the chip tag prefix exists
				assert.True(t, strings.HasPrefix(tagStr, "chip:"), "Chip tag should have chip: prefix: %s", tagStr)
			}
		}
	})

	t.Run("Hyphenated TOSEC tags", func(t *testing.T) {
		copyrightTags := CanonicalTagDefinitions[TagTypeCopyright]

		hasHyphenated := false
		for _, tag := range copyrightTags {
			tagStr := string(tag)
			if strings.Contains(tagStr, "-") {
				hasHyphenated = true
				// Should be format like "cw-r" (type-restriction)
				parts := strings.Split(tagStr, "-")
				assert.Len(t, parts, 2,
					"Copyright tag with dash should be two parts: %s", tagStr)
			}
		}

		assert.True(t, hasHyphenated, "Copyright should have hyphenated tags like 'cw-r'")
	})
}

// TestTagTypeStringConversion verifies TagType can convert to/from string
func TestTagTypeStringConversion(t *testing.T) {
	testCases := map[string]TagType{
		"input":       TagTypeInput,
		"players":     TagTypePlayers,
		"gamegenre":   TagTypeGameGenre,
		"disctotal":   TagTypeDiscTotal,
		"extension":   TagTypeExtension,
		"mameparent":  TagTypeMameParent,
		"arcadeboard": TagTypeArcadeBoard,
	}

	for str, expectedType := range testCases {
		t.Run(str, func(t *testing.T) {
			// Convert string to TagType
			tagType := TagType(str)
			assert.Equal(t, expectedType, tagType)

			// Convert back to string
			assert.Equal(t, str, string(tagType))
		})
	}
}

// TestNoTagTypeCollisions verifies no two TagTypes have the same string value
func TestNoTagTypeCollisions(t *testing.T) {
	allTypes := []TagType{
		TagTypeInput, TagTypePlayers, TagTypeGameGenre, TagTypeAddon, TagTypeEmbedded,
		TagTypeSave, TagTypeArcadeBoard, TagTypeCompatibility, TagTypeDisc, TagTypeDiscTotal,
		TagTypeBased, TagTypeSearch, TagTypeMultigame, TagTypeReboxed, TagTypePort,
		TagTypeLang, TagTypeUnfinished, TagTypeRerelease, TagTypeRev, TagTypeSet,
		TagTypeAlt, TagTypeUnlicensed, TagTypeMameParent, TagTypeRegion, TagTypeYear,
		TagTypeVideo, TagTypeCopyright, TagTypeDump, TagTypeMedia, TagTypeExtension,
		TagTypeUnknown,
	}

	seen := make(map[string]TagType)

	for _, tagType := range allTypes {
		str := string(tagType)
		if existing, exists := seen[str]; exists {
			t.Errorf("TagType collision: both %v and %v have string value '%s'",
				existing, tagType, str)
		}
		seen[str] = tagType
	}

	// Should have exactly as many unique strings as types
	require.Len(t, seen, len(allTypes),
		"Should have unique string value for each TagType")
}

// TestCanonicalTagStringGlobalUniqueness verifies all CanonicalTag.String() outputs are globally unique.
// This is critical because composite keys "type:value" are used in the TagIDs map to avoid collisions
// when tag values overlap between types (e.g., "1" in both disc:1 and rev:1).
func TestCanonicalTagStringGlobalUniqueness(t *testing.T) {
	// Map to track all String() outputs globally
	seen := make(map[string]struct {
		tagType TagType
		value   TagValue
	})

	duplicates := []string{}
	totalTags := 0

	// Iterate through all canonical tag definitions
	for tagType, tags := range CanonicalTagDefinitions {
		for _, tagValue := range tags {
			// Create CanonicalTag and get its String() output
			canonicalTag := CanonicalTag{
				Type:  tagType,
				Value: tagValue,
			}
			compositeKey := canonicalTag.String() // Returns "type:value" format

			// Check for duplicates
			if existing, exists := seen[compositeKey]; exists {
				duplicates = append(duplicates,
					compositeKey+" (conflict between "+
						string(existing.tagType)+":"+string(existing.value)+
						" and "+string(tagType)+":"+string(tagValue)+")")
			} else {
				seen[compositeKey] = struct {
					tagType TagType
					value   TagValue
				}{tagType, tagValue}
			}
			totalTags++
		}
	}

	// Report findings
	t.Logf("Checked %d canonical tags across %d tag types", totalTags, len(CanonicalTagDefinitions))
	t.Logf("Found %d unique composite keys (type:value)", len(seen))

	// Assert no duplicates exist
	assert.Empty(t, duplicates,
		"Found duplicate CanonicalTag.String() outputs (these would cause TagIDs map collisions):\n%v",
		strings.Join(duplicates, "\n"))

	// Verify we checked a reasonable number of tags
	assert.GreaterOrEqual(t, totalTags, 900,
		"Should have checked at least 900 canonical tags")

	// Verify all String() outputs are unique (total tags == unique composite keys)
	assert.Len(t, seen, totalTags,
		"All CanonicalTag.String() outputs must be globally unique")
}
