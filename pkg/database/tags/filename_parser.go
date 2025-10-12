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
	"regexp"
	"strings"
)

// Package-level compiled regexes for filename parsing.
// These are compiled once at initialization for optimal performance.
var (
	// Special pattern extraction
	reDisc               = regexp.MustCompile(`(?i)\(Disc\s+(\d+)\s+of\s+(\d+)\)`)
	reRev                = regexp.MustCompile(`(?i)\(Rev[\s-]([A-Z0-9]+)\)`)
	reVersion            = regexp.MustCompile(`(?i)\(v(\d+(?:\.\d+)*)\)`)
	reTrans              = regexp.MustCompile(`(^|\s)(T)([+-]?)([A-Za-z]{2,3})(?:\s+v(\d+(?:\.\d+)*))?(?:\s|[.]|$)`)
	reBracketlessVersion = regexp.MustCompile(`\bv(\d+(?:\.\d+)*)`)
	reEditionWord        = regexp.MustCompile(
		`(?i)\s+(version|edition|ausgabe|versione|edizione|versao|edicao|バージョン|エディション|ヴァージョン)(\s*[\(\[{<]|\s*$)`,
	)

	// Title parsing
	reTitleExtract = regexp.MustCompile(`^([^(\[{<]*)`)
	reMultiSpace   = regexp.MustCompile(`\s+`)
	reLeadingNum   = regexp.MustCompile(`^\d+[.\s\-]+`)
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

// extractTags uses a manual state machine to extract tags from parentheses, brackets, braces, and angle brackets.
// Returns separate slices for parentheses tags and bracket tags to aid disambiguation.
// Braces {} and angle brackets <> are treated like parentheses for tag extraction.
func extractTags(filename string) (parenTags, bracketTags []string) {
	const (
		stateOutside = iota
		stateInParen
		stateInBracket
		stateInBrace
		stateInAngle
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
			case '{':
				state = stateInBrace
				tagStart = i + 1
			case '<':
				state = stateInAngle
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

		case stateInBrace:
			if char == '}' {
				tag := filename[tagStart:i]
				if tag != "" {
					parenTags = append(parenTags, tag) // Treat like parentheses
				}
				state = stateOutside
			}

		case stateInAngle:
			if char == '>' {
				tag := filename[tagStart:i]
				if tag != "" {
					parenTags = append(parenTags, tag) // Treat like parentheses
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
	if matches := reDisc.FindStringSubmatch(remaining); len(matches) > 2 {
		tags = append(tags,
			CanonicalTag{TagTypeMedia, TagValue("disc")},
			CanonicalTag{TagTypeDisc, TagValue(matches[1])},
			CanonicalTag{TagTypeDiscTotal, TagValue(matches[2])},
		)
		remaining = reDisc.ReplaceAllString(remaining, "")
	}

	// Pattern 2: Revision tags - "Rev 1", "Rev A", "Rev-1"
	if matches := reRev.FindStringSubmatch(remaining); len(matches) > 1 {
		tags = append(tags, CanonicalTag{TagTypeRev, TagValue(strings.ToLower(matches[1]))})
		remaining = reRev.ReplaceAllString(remaining, "")
	}

	// Pattern 3: Version tags - "v1.2", "v1.2.3"
	if matches := reVersion.FindStringSubmatch(remaining); len(matches) > 1 {
		tags = append(tags, CanonicalTag{TagTypeRev, TagValue(matches[1])})
		remaining = reVersion.ReplaceAllString(remaining, "")
	}

	// Pattern 4: Bracketless translation tags - "T+Eng", "T-Ger", "T+Spa v1.2"
	// Format: T[+-]?<lang_code>( v<version>)?
	// Examples: "T+Eng", "T-Ger", "TFre", "T+Eng v1.0", "T+Spa v2.1.3"
	// Must be standalone: preceded by space (captured) OR at start, followed by space/dot/end
	if matches := reTrans.FindStringSubmatch(remaining); len(matches) >= 5 {
		// matches[1] = prefix (^ or space)
		// matches[2] = "T"
		// matches[3] = +/- or empty
		// matches[4] = language code
		// matches[5] = version number or empty
		plusMinus := matches[3]
		langCode := strings.ToLower(matches[4])
		versionNum := ""
		if len(matches) > 5 && matches[5] != "" {
			versionNum = matches[5]
		}

		// Only process if it's a valid translation tag pattern:
		// - Has +/- prefix (T+Eng, T-Ger), OR
		// - Language code is exactly 3 letters (TFre, TEng)
		isValid := plusMinus != "" || len(langCode) == 3

		if isValid {
			// Map 3-letter ROM codes to 2-letter ISO 639-1 codes
			langMap := map[string]string{
				"eng": "en", "ger": "de", "fre": "fr", "spa": "es", "ita": "it",
				"rus": "ru", "por": "pt", "dut": "nl", "swe": "sv", "nor": "no",
				"fin": "fi", "dan": "da", "pol": "pl", "cze": "cs", "gre": "el",
				"hun": "hu", "tur": "tr", "ara": "ar", "heb": "he", "jpn": "ja",
				"kor": "ko", "chi": "zh", "bra": "pt",
			}

			// Convert 3-letter to 2-letter if needed
			if mappedLang, ok := langMap[langCode]; ok {
				langCode = mappedLang
			}

			// Add translation tag based on +/- prefix
			// T+ or T (no prefix) = current/generic translation (use base tag)
			// T- = older/outdated translation (use :old hierarchical tag)
			if plusMinus == "-" {
				tags = append(tags, CanonicalTag{TagTypeUnlicensed, TagUnlicensedTranslationOld})
			} else {
				// T+ and T both use the base translation tag
				tags = append(tags, CanonicalTag{TagTypeUnlicensed, TagUnlicensedTranslation})
			}

			// Add language tag (map to canonical language codes)
			langTags := mapFilenameTagToCanonical(langCode)
			for _, lt := range langTags {
				if lt.Type == TagTypeLang {
					tags = append(tags, lt)
					break
				}
			}

			// If version number present, add as revision tag
			if versionNum != "" {
				tags = append(tags, CanonicalTag{TagTypeRev, TagValue(versionNum)})
			}

			// Replace the matched pattern, preserving leading space if present
			remaining = reTrans.ReplaceAllString(remaining, " ")
		}
	}

	// Pattern 5: Bracketless version tags (if not part of translation) - "v1.0", "v1.2.3"
	// Only extract if not already processed as part of translation pattern
	if matches := reBracketlessVersion.FindStringSubmatch(remaining); len(matches) > 1 {
		// Check if we already extracted a version from translation pattern
		hasVersion := false
		for _, tag := range tags {
			if tag.Type == TagTypeRev {
				hasVersion = true
				break
			}
		}
		if !hasVersion {
			tags = append(tags, CanonicalTag{TagTypeRev, TagValue(matches[1])})
			remaining = reBracketlessVersion.ReplaceAllString(remaining, "")
		}
	}

	// Pattern 6: Edition/Version word detection - "Version", "Edition", and multi-language equivalents
	// Detects standalone edition words that will be stripped by slugification
	if matches := reEditionWord.FindStringSubmatch(remaining); len(matches) > 1 {
		editionWord := strings.ToLower(matches[1])

		// Determine if this is a "version" word or "edition" word
		// Version words: version, versione, versao, バージョン, ヴァージョン
		// Edition words: edition, ausgabe, edizione, edicao, エディション
		versionWords := map[string]bool{
			"version":  true,
			"versione": true,
			"versao":   true,
			"バージョン":    true,
			"ヴァージョン":   true,
		}

		if versionWords[editionWord] {
			tags = append(tags, CanonicalTag{TagTypeEdition, TagEditionVersion})
		} else {
			tags = append(tags, CanonicalTag{TagTypeEdition, TagEditionEdition})
		}

		// Don't remove the word from remaining - it's part of the title and will be
		// stripped later by slugification. We just want to tag its presence.
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

// ParseTitleFromFilename extracts a clean, human-readable display title from a filename.
// It removes metadata brackets and normalizes common filename artifacts for better presentation.
//
// The stripLeadingNumbers parameter controls whether leading number prefixes like "1. ", "01 - ", etc.
// should be removed. This should only be true when contextual detection confirms list-based numbering
// is present in the directory.
//
// Transformations applied:
//   - Optionally removes leading number prefixes (e.g., "1. ", "01 - ") if stripLeadingNumbers is true
//   - Removes everything after first bracket of any type: (), [], {}, <>
//   - Normalizes underscores and multiple separators to spaces
//   - Converts "&" to "and"
//   - Normalizes multiple spaces to single space
//
// Examples:
//   - "Super Mario Bros (USA) [!]" → "Super Mario Bros"
//   - "Super_Mario_Bros (USA)" → "Super Mario Bros"
//   - "Sonic & Knuckles" → "Sonic and Knuckles"
//   - "1. Game Title (USA)" → "Game Title" (if stripLeadingNumbers is true)
//   - "1942 (USA)" → "1942" (if stripLeadingNumbers is false)
func ParseTitleFromFilename(filename string, stripLeadingNumbers bool) string {
	// Step 1: Optionally strip leading number prefixes (e.g., "1. ", "01 - ")
	// Only done when contextual detection confirms list-based numbering
	title := filename
	if stripLeadingNumbers {
		title = reLeadingNum.ReplaceAllString(title, "")
		title = strings.TrimSpace(title)
	}

	// Step 2: Extract title before first bracket
	title = reTitleExtract.FindString(title)
	title = strings.TrimSpace(title)

	// Step 3: Normalize underscores to spaces
	title = strings.ReplaceAll(title, "_", " ")

	// Step 4: Convert "&" to "and"
	title = strings.ReplaceAll(title, "&", "and")

	// Step 5: Normalize multiple spaces to single space
	title = reMultiSpace.ReplaceAllString(title, " ")

	return strings.TrimSpace(title)
}
