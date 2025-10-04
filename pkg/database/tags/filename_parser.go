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

package tags

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
)

// ParseContext holds context information for disambiguating tags during parsing.
// This follows the No-Intro/TOSEC convention of using tag position and bracket type
// to determine meaning.
type ParseContext struct {
	Filename           string
	CurrentTag         string
	CurrentBracketType string
	ParenTags          []string
	BracketTags        []string
	ProcessedTags      []CanonicalTag
	CurrentIndex       int
}

// RawTag represents an extracted tag before canonical mapping
type RawTag struct {
	Value       string
	BracketType string // "paren" or "bracket"
	Position    int    // Position in the tag sequence
}

// extractTags uses a manual state machine to extract tags from parentheses and brackets.
// Returns separate slices for parentheses tags and bracket tags to aid disambiguation.
func extractTags(filename string) (parenTags, bracketTags []string) {
	const (
		stateOutside = iota
		stateInParen
		stateInBracket
	)

	state := stateOutside
	tagStart := 0
	parenTags = make([]string, 0, 8) // Pre-allocate for typical case
	bracketTags = make([]string, 0, 4)

	for i := range len(filename) {
		char := filename[i]

		switch state {
		case stateOutside:
			switch char {
			case '(':
				state = stateInParen
				tagStart = i + 1
			case '[':
				state = stateInBracket
				tagStart = i + 1
			}

		case stateInParen:
			if char == ')' {
				tag := filename[tagStart:i]
				if tag != "" {
					parenTags = append(parenTags, tag)
				}
				state = stateOutside
			}

		case stateInBracket:
			if char == ']' {
				tag := filename[tagStart:i]
				if tag != "" {
					bracketTags = append(bracketTags, tag)
				}
				state = stateOutside
			}
		}
	}

	return parenTags, bracketTags
}

// SpecialPattern represents a pre-extracted special pattern
type SpecialPattern struct {
	Pattern string
	Tags    []CanonicalTag
}

// extractSpecialPatterns handles special formats like "Disc X of Y", "Rev X", "v1.2"
// These are extracted before general tag parsing to avoid ambiguity and improve performance.
// Returns the canonical tags and the filename with special patterns removed.
func extractSpecialPatterns(filename string) (tags []CanonicalTag, remaining string) {
	tags = make([]CanonicalTag, 0) // Initialize to empty slice, not nil
	remaining = filename

	// Pattern 1: "Disc X of Y" - most common multi-disc format
	discRe := helpers.CachedMustCompile(`(?i)\(Disc\s+(\d+)\s+of\s+(\d+)\)`)
	if matches := discRe.FindStringSubmatch(remaining); len(matches) > 2 {
		tags = append(tags,
			CanonicalTag{TagTypeMedia, TagValue("disc")},
			CanonicalTag{TagTypeDisc, TagValue(matches[1])},
			CanonicalTag{TagTypeDiscTotal, TagValue(matches[2])},
		)
		remaining = discRe.ReplaceAllString(remaining, "")
	}

	// Pattern 2: Revision tags - "Rev 1", "Rev A", "Rev-1"
	revRe := helpers.CachedMustCompile(`(?i)\(Rev[\s-]([A-Z0-9]+)\)`)
	if matches := revRe.FindStringSubmatch(remaining); len(matches) > 1 {
		tags = append(tags, CanonicalTag{TagTypeRev, TagValue(strings.ToLower(matches[1]))})
		remaining = revRe.ReplaceAllString(remaining, "")
	}

	// Pattern 3: Version tags - "v1.2", "v1.2.3"
	versionRe := helpers.CachedMustCompile(`(?i)\(v(\d+(?:\.\d+)*)\)`)
	if matches := versionRe.FindStringSubmatch(remaining); len(matches) > 1 {
		tags = append(tags, CanonicalTag{TagTypeRev, TagValue(matches[1])})
		remaining = versionRe.ReplaceAllString(remaining, "")
	}

	return tags, remaining
}

// parseMultiLanguageTag handles comma-separated language tags like "(En,Fr,De)"
// Returns individual language tags or nil if not a multi-language tag
func parseMultiLanguageTag(tag string) []CanonicalTag {
	// Multi-language tags contain commas
	if !strings.Contains(tag, ",") {
		return nil
	}

	parts := strings.Split(tag, ",")
	var langs []CanonicalTag

	for _, part := range parts {
		normalized := normalizeTagValue(part)
		// Check if it's a known language code (2 chars typically)
		if len(normalized) == 2 || len(normalized) == 3 {
			// Try to map as language
			mapped := mapFilenameTagToCanonical(normalized)
			for _, ct := range mapped {
				if ct.Type == TagTypeLang {
					langs = append(langs, ct)
				}
			}
		}
	}

	// Only return if we found at least 2 languages
	if len(langs) >= 2 {
		return langs
	}
	return nil
}

// disambiguateTag uses context to determine the correct canonical tag(s) for an ambiguous raw tag.
// This implements No-Intro/TOSEC conventions:
// - Parentheses tags appear in order: region → language → version → dev status
// - Bracket tags are always dump info
// - Tag position and previously seen tag types provide context
func disambiguateTag(ctx *ParseContext) []CanonicalTag {
	normalized := normalizeTagValue(ctx.CurrentTag)

	// First check if it's a multi-language tag (En,Fr,De)
	if multiLang := parseMultiLanguageTag(normalized); multiLang != nil {
		return multiLang
	}

	// Bracket tags are always dump info or hacks
	if ctx.CurrentBracketType == "bracket" {
		return mapBracketTag(normalized)
	}

	// For parentheses tags, use position and context
	return mapParenthesisTag(normalized, ctx)
}

// mapBracketTag maps tags from square brackets [].
// These are always dump-related: verified, bad, hacked, cracked, fixed, translated, etc.
func mapBracketTag(tag string) []CanonicalTag {
	// Special handling for ambiguous tags in bracket context
	switch tag {
	case "tr":
		// In brackets, "tr" is always "translated" (dump info)
		return []CanonicalTag{{TagTypeDump, "translated"}}
	case "b":
		return []CanonicalTag{{TagTypeDump, "bad"}}
	case "!":
		return []CanonicalTag{{TagTypeDump, "verified"}}
	case "h":
		return []CanonicalTag{{TagTypeDump, "hacked"}}
	case "f":
		return []CanonicalTag{{TagTypeDump, "fixed"}}
	case "cr":
		return []CanonicalTag{{TagTypeDump, "cracked"}}
	case "t":
		return []CanonicalTag{{TagTypeDump, "trained"}}
	default:
		// Try default mapping, filtering for dump-related tags only
		mapped := mapFilenameTagToCanonical(tag)
		var dumpTags []CanonicalTag
		for _, ct := range mapped {
			if ct.Type == TagTypeDump || ct.Type == TagTypeUnlicensed {
				dumpTags = append(dumpTags, ct)
			}
		}
		if len(dumpTags) > 0 {
			return dumpTags
		}
		return []CanonicalTag{{TagTypeDump, TagValue(tag)}}
	}
}

// mapParenthesisTag maps tags from parentheses ().
// Uses context to disambiguate: region vs language vs version vs dev status.
func mapParenthesisTag(tag string, ctx *ParseContext) []CanonicalTag {
	// Check if we've already seen certain tag types to provide context
	hasRegion := false
	hasLanguage := false
	hasVersion := false

	for _, pt := range ctx.ProcessedTags {
		switch pt.Type {
		case TagTypeRegion:
			hasRegion = true
		case TagTypeLang:
			hasLanguage = true
		case TagTypeRev:
			hasVersion = true
		default:
			// Ignore other tag types for context
		}
	}

	// Special disambiguation rules for ambiguous tags
	switch tag {
	case "ch":
		// If we have German language, "Ch" is Switzerland (region)
		for _, pt := range ctx.ProcessedTags {
			if pt.Type == TagTypeLang && pt.Value == "de" {
				return []CanonicalTag{{TagTypeRegion, "ch"}}
			}
		}
		// If no region yet and early in sequence, prefer region
		if !hasRegion && ctx.CurrentIndex < 2 {
			return []CanonicalTag{{TagTypeRegion, "ch"}}
		}
		// Otherwise, Chinese language
		return []CanonicalTag{{TagTypeLang, "zh"}}

	case "tr":
		// In parentheses, "tr" is Turkey (region) or Turkish (language)
		// Never "translated" (that's only in brackets)
		if !hasRegion && ctx.CurrentIndex < 2 {
			return []CanonicalTag{{TagTypeRegion, "tr"}}
		}
		if !hasLanguage {
			return []CanonicalTag{{TagTypeLang, "tr"}}
		}
		// Default to region if ambiguous
		return []CanonicalTag{{TagTypeRegion, "tr"}}

	case "bs":
		// "bs" in parentheses is almost always Bosnian language
		// Broadcast Satellite would be "satellaview" or in special hardware context
		return []CanonicalTag{{TagTypeLang, "bs"}}

	case "hi":
		// In brackets, "hi" is "hacked intro"
		if ctx.CurrentBracketType == "bracket" {
			return []CanonicalTag{{TagTypeDump, "hacked"}, {TagTypeDump, "hacked:intro"}}
		}
		// In parentheses, "hi" is Hindi language
		return []CanonicalTag{{TagTypeLang, "hi"}}

	case "st":
		// "st" could be Sufami Turbo (SNES addon) but that's rare
		// Context: if SNES-related or hardware tags present
		for _, pt := range ctx.ProcessedTags {
			if pt.Type == TagTypeAddon || pt.Type == TagTypeCompatibility {
				return []CanonicalTag{{TagTypeAddon, "snes:sufami"}}
			}
		}
		// Otherwise, fallback to map (might be unknown)
		return mapFilenameTagToCanonical(tag)

	case "np":
		// "np" could be Nintendo Power (SNES cartridge) but uncommon
		// Context: if SNES-related or hardware tags present
		for _, pt := range ctx.ProcessedTags {
			if pt.Type == TagTypeAddon || pt.Type == TagTypeCompatibility {
				return []CanonicalTag{{TagTypeAddon, "snes:nintendopower"}}
			}
		}
		// Otherwise, fallback to map
		return mapFilenameTagToCanonical(tag)
	}

	// Try default mapping
	mapped := mapFilenameTagToCanonical(tag)
	if len(mapped) == 0 {
		return []CanonicalTag{{TagTypeUnknown, TagValue(tag)}}
	}

	// If multiple mappings, check if they're complementary (like region+language)
	// or conflicting (same type, different values)
	if len(mapped) > 1 {
		// Check if all tags have different types (complementary, like usa→region+language)
		types := make(map[TagType]bool)
		hasConflict := false
		for _, tag := range mapped {
			if types[tag.Type] {
				hasConflict = true // Same type appears twice = conflicting
				break
			}
			types[tag.Type] = true
		}

		// If complementary (different types), return all
		// If conflicting (same type), use context to pick best one
		if hasConflict {
			return selectBestMapping(mapped, hasRegion, hasLanguage, hasVersion, ctx.CurrentIndex)
		}
	}

	return mapped
}

// selectBestMapping chooses the most appropriate canonical tag when multiple options exist.
// Priority based on No-Intro/TOSEC conventions:
// Early tags: Region > Language > Other
// Late tags: Version > Dev Status > Other
func selectBestMapping(options []CanonicalTag, hasRegion, hasLanguage, hasVersion bool, position int) []CanonicalTag {
	// Early position (0-1): prefer region, then language
	if position < 2 {
		if !hasRegion {
			for _, opt := range options {
				if opt.Type == TagTypeRegion {
					return []CanonicalTag{opt}
				}
			}
		}
		if !hasLanguage {
			for _, opt := range options {
				if opt.Type == TagTypeLang {
					return []CanonicalTag{opt}
				}
			}
		}
	}

	// Mid position (2-4): prefer version, dev status
	if position >= 2 && position < 5 {
		if !hasVersion {
			for _, opt := range options {
				if opt.Type == TagTypeRev {
					return []CanonicalTag{opt}
				}
			}
		}
		for _, opt := range options {
			if opt.Type == TagTypeUnfinished {
				return []CanonicalTag{opt}
			}
		}
	}

	// Fallback: return first option
	return []CanonicalTag{options[0]}
}

// ParseFilenameToCanonicalTags is the main entry point for parsing ROM filenames.
// It extracts and disambiguates tags following No-Intro/TOSEC conventions.
// Returns a slice of canonical tags ready for database insertion.
func ParseFilenameToCanonicalTags(filename string) []CanonicalTag {
	var allTags []CanonicalTag

	// Step 1: Extract special patterns first
	specialTags, remaining := extractSpecialPatterns(filename)
	allTags = append(allTags, specialTags...)

	// Step 2: Extract parentheses and bracket tags
	parenTags, bracketTags := extractTags(remaining)

	// Step 3: Process parentheses tags (region, language, version, dev status)
	ctx := &ParseContext{
		Filename:      filename,
		ParenTags:     parenTags,
		BracketTags:   bracketTags,
		ProcessedTags: allTags,
	}

	for i, tag := range parenTags {
		ctx.CurrentTag = tag
		ctx.CurrentIndex = i
		ctx.CurrentBracketType = "paren"

		resolved := disambiguateTag(ctx)
		allTags = append(allTags, resolved...)
		ctx.ProcessedTags = allTags // Update context with newly processed tags
	}

	// Step 4: Process bracket tags (dump info, hacks, etc.)
	for i, tag := range bracketTags {
		ctx.CurrentTag = tag
		ctx.CurrentIndex = i
		ctx.CurrentBracketType = "bracket"

		resolved := disambiguateTag(ctx)
		allTags = append(allTags, resolved...)
		ctx.ProcessedTags = allTags
	}

	return allTags
}
