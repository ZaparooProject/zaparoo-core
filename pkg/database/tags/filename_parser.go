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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
)

// Package-level compiled regexes for filename parsing.
// These are compiled once at initialization for optimal performance.
var (
	// Special pattern extraction
	reDisc               = regexp.MustCompile(`(?i)\(Disc\s+(\d+)\s+of\s+(\d+)\)`)
	reRev                = regexp.MustCompile(`(?i)\(Rev[\s-]([A-Z0-9]+)\)`)
	reVersion            = regexp.MustCompile(`(?i)\(v(\d+(?:\.\d+)*)\)`)
	reYear               = regexp.MustCompile(`\((19[789]\d|20\d{2})\)`)
	reTrans              = regexp.MustCompile(`(^|\s)(T)([+-]?)([A-Za-z]{2,3})(?:\s+v(\d+(?:\.\d+)*))?(?:\s|[.]|$)`)
	reBracketlessVersion = regexp.MustCompile(`\bv(\d+(?:\.\d+)*)`)
	reEditionWord        = regexp.MustCompile(
		`(?i)\s+(version|edition|ausgabe|versione|edizione|versao|edicao|バージョン|エディション|ヴァージョン)(\s*[\(\[{<]|\s*$)`,
	)

	// Title parsing
	reMultiSpace = regexp.MustCompile(`\s+`)
	reLeadingNum = regexp.MustCompile(`^\d+[.\s\-]+`)
)

// langMap maps 3-letter ROM language codes to 2-letter ISO 639-1 codes.
var langMap = map[string]string{
	"eng": "en", "ger": "de", "fre": "fr", "spa": "es", "ita": "it",
	"rus": "ru", "por": "pt", "dut": "nl", "swe": "sv", "nor": "no",
	"fin": "fi", "dan": "da", "pol": "pl", "cze": "cs", "gre": "el",
	"hun": "hu", "tur": "tr", "ara": "ar", "heb": "he", "jpn": "ja",
	"kor": "ko", "chi": "zh", "bra": "pt",
}

// BracketType represents the type of bracket enclosing a tag.
// Different bracket types follow different conventions (No-Intro/TOSEC).
type BracketType uint8

const (
	// BracketTypeParen represents tags in parentheses (), braces {}, or angle brackets <>.
	// These typically contain region, language, version, and dev status information.
	BracketTypeParen BracketType = iota

	// BracketTypeSquare represents tags in square brackets [].
	// These always contain dump-related information (verified, bad, hacked, etc.).
	BracketTypeSquare
)

// ParseContext holds context information for disambiguating tags during parsing.
// This follows the No-Intro/TOSEC convention of using tag position and bracket type
// to determine meaning.
type ParseContext struct {
	Filename           string
	CurrentTag         string
	ParenTags          []string
	BracketTags        []string
	ProcessedTags      []CanonicalTag
	CurrentIndex       int
	CurrentBracketType BracketType
}

// RawTag represents an extracted tag before canonical mapping
type RawTag struct {
	Value       string
	BracketType BracketType // BracketTypeParen or BracketTypeSquare
	Position    int         // Position in the tag sequence
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
//
// Optimization: Uses FindStringSubmatchIndex instead of FindStringSubmatch + ReplaceAllString
// to eliminate intermediate string allocations (~5MB savings per 400K files).
func extractSpecialPatterns(filename string) (tags []CanonicalTag, remaining string) {
	tags = make([]CanonicalTag, 0) // Initialize to empty slice, not nil
	remaining = filename

	// Pattern 1: "Disc X of Y" - most common multi-disc format
	if indices := reDisc.FindStringSubmatchIndex(remaining); len(indices) > 0 {
		// indices[0:2] = full match, indices[2:4] = first capture, indices[4:6] = second capture
		tags = append(tags,
			CanonicalTag{
				Type:   TagTypeMedia,
				Value:  TagValue("disc"),
				Source: TagSourceBracketed,
			},
			CanonicalTag{
				Type:   TagTypeDisc,
				Value:  TagValue(remaining[indices[2]:indices[3]]),
				Source: TagSourceBracketed,
			},
			CanonicalTag{
				Type:   TagTypeDiscTotal,
				Value:  TagValue(remaining[indices[4]:indices[5]]),
				Source: TagSourceBracketed,
			},
		)
		// Remove matched pattern
		remaining = remaining[:indices[0]] + remaining[indices[1]:]
	}

	// Pattern 2: Revision tags - "Rev 1", "Rev A", "Rev-1"
	if indices := reRev.FindStringSubmatchIndex(remaining); len(indices) > 0 {
		revValue := strings.ToLower(remaining[indices[2]:indices[3]])
		// Normalize periods to dashes (e.g., "1.2" → "1-2")
		revValue = strings.ReplaceAll(revValue, ".", "-")
		tags = append(tags, CanonicalTag{
			Type:   TagTypeRev,
			Value:  TagValue(revValue),
			Source: TagSourceBracketed,
		})
		remaining = remaining[:indices[0]] + remaining[indices[1]:]
	}

	// Pattern 3: Version tags - "v1.2", "v1.2.3"
	if indices := reVersion.FindStringSubmatchIndex(remaining); len(indices) > 0 {
		versionValue := remaining[indices[2]:indices[3]]
		// Normalize periods to dashes (e.g., "1.2.3" → "1-2-3")
		versionValue = strings.ReplaceAll(versionValue, ".", "-")
		tags = append(tags, CanonicalTag{
			Type:   TagTypeRev,
			Value:  TagValue(versionValue),
			Source: TagSourceBracketed,
		})
		remaining = remaining[:indices[0]] + remaining[indices[1]:]
	}

	// Pattern 4: Year tags - "(1995)", "(2004)"
	// Matches years 1970-2099 in parentheses
	if indices := reYear.FindStringSubmatchIndex(remaining); len(indices) > 0 {
		yearValue := remaining[indices[2]:indices[3]]
		tags = append(tags, CanonicalTag{
			Type:   TagTypeYear,
			Value:  TagValue(yearValue),
			Source: TagSourceBracketed,
		})
		remaining = remaining[:indices[0]] + remaining[indices[1]:]
	}

	// Pattern 6: Bracketless translation tags - "T+Eng", "T-Ger", "T+Spa v1.2"
	// Format: T[+-]?<lang_code>( v<version>)?
	// Examples: "T+Eng", "T-Ger", "TFre", "T+Eng v1.0", "T+Spa v2.1.3"
	// Must be standalone: preceded by space (captured) OR at start, followed by space/dot/end
	if indices := reTrans.FindStringSubmatchIndex(remaining); len(indices) >= 10 {
		// indices[0:2] = full match
		// indices[2:4] = prefix (^ or space)
		// indices[4:6] = "T"
		// indices[6:8] = +/- or empty
		// indices[8:10] = language code
		// indices[10:12] = version number or empty (if present)
		plusMinus := ""
		if indices[6] != -1 {
			plusMinus = remaining[indices[6]:indices[7]]
		}
		langCode := strings.ToLower(remaining[indices[8]:indices[9]])
		versionNum := ""
		if len(indices) > 11 && indices[10] != -1 {
			versionNum = remaining[indices[10]:indices[11]]
		}

		// Only process if it's a valid translation tag pattern:
		// - Has +/- prefix (T+Eng, T-Ger), OR
		// - Language code is exactly 3 letters (TFre, TEng)
		isValid := plusMinus != "" || len(langCode) == 3

		if isValid {
			// Convert 3-letter to 2-letter if needed
			if mappedLang, ok := langMap[langCode]; ok {
				langCode = mappedLang
			}

			// Add translation tag based on +/- prefix
			// T+ or T (no prefix) = current/generic translation (use base tag)
			// T- = older/outdated translation (use :old hierarchical tag)
			// Inferred from plain text (bracketless)
			if plusMinus == "-" {
				tags = append(tags, CanonicalTag{
					Type:   TagTypeUnlicensed,
					Value:  TagUnlicensedTranslationOld,
					Source: TagSourceInferred,
				})
			} else {
				// T+ and T both use the base translation tag
				tags = append(tags, CanonicalTag{
					Type:   TagTypeUnlicensed,
					Value:  TagUnlicensedTranslation,
					Source: TagSourceInferred,
				})
			}

			// Add language tag (map to canonical language codes)
			// Inferred from bracketless translation
			langTags := mapFilenameTagToCanonical(langCode)
			for _, lt := range langTags {
				if lt.Type == TagTypeLang {
					lt.Source = TagSourceInferred
					tags = append(tags, lt)
					break
				}
			}

			// If version number present, add as revision tag
			// Inferred from bracketless translation
			if versionNum != "" {
				// Normalize periods to dashes (e.g., "1.2" → "1-2")
				versionNum = strings.ReplaceAll(versionNum, ".", "-")
				tags = append(tags, CanonicalTag{
					Type:   TagTypeRev,
					Value:  TagValue(versionNum),
					Source: TagSourceInferred,
				})
			}

			// Replace the matched pattern with a space to preserve word boundaries
			remaining = remaining[:indices[0]] + " " + remaining[indices[1]:]
		}
	}

	// Pattern 7: Bracketless version tags (if not part of translation) - "v1.0", "v1.2.3"
	// Only extract if not already processed as part of translation pattern
	if indices := reBracketlessVersion.FindStringSubmatchIndex(remaining); len(indices) > 0 {
		// Check if we already extracted a version from translation pattern
		hasVersion := false
		for _, tag := range tags {
			if tag.Type == TagTypeRev {
				hasVersion = true
				break
			}
		}
		if !hasVersion {
			versionValue := remaining[indices[2]:indices[3]]
			// Normalize periods to dashes (e.g., "1.2.3" → "1-2-3")
			versionValue = strings.ReplaceAll(versionValue, ".", "-")
			// Inferred from plain text (bracketless)
			tags = append(tags, CanonicalTag{
				Type:   TagTypeRev,
				Value:  TagValue(versionValue),
				Source: TagSourceInferred,
			})
			remaining = remaining[:indices[0]] + remaining[indices[1]:]
		}
	}

	// Pattern 8: Edition/Version word detection - "Version", "Edition", and multi-language equivalents
	// Detects standalone edition words that will be stripped by slugification
	if indices := reEditionWord.FindStringSubmatchIndex(remaining); len(indices) > 0 {
		editionWord := strings.ToLower(remaining[indices[2]:indices[3]])

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

		// Inferred from plain text (not bracketed)
		if versionWords[editionWord] {
			tags = append(tags, CanonicalTag{
				Type:   TagTypeEdition,
				Value:  TagEditionVersion,
				Source: TagSourceInferred,
			})
		} else {
			tags = append(tags, CanonicalTag{
				Type:   TagTypeEdition,
				Value:  TagEditionEdition,
				Source: TagSourceInferred,
			})
		}

		// Don't remove the word from remaining - it's part of the title and will be
		// stripped later by slugification. We just want to tag its presence.
	}

	return tags, remaining
}

// parseMultiLanguageTag handles multi-language tags in both formats:
//   - Comma-separated (No-Intro): "(En,Fr,De)" → lang:en, lang:fr, lang:de
//   - Plus-separated (TOSEC): "(En+Fr)" → lang:en, lang:fr
//
// Returns individual language tags or nil if not a multi-language tag.
func parseMultiLanguageTag(tag string) []CanonicalTag {
	// Multi-language tags contain commas or plus signs
	var parts []string
	switch {
	case strings.Contains(tag, ","):
		parts = strings.Split(tag, ",")
	case strings.Contains(tag, "+"):
		parts = strings.Split(tag, "+")
	default:
		return nil // Single language
	}

	var langs []CanonicalTag

	for _, part := range parts {
		normalized := NormalizeTag(part)
		// Check if it's a known language code (2-3 chars typically)
		if len(normalized) >= 2 && len(normalized) <= 3 {
			// Try to map as language
			mapped := mapFilenameTagToCanonical(normalized)
			for _, ct := range mapped {
				if ct.Type == TagTypeLang {
					ct.Source = TagSourceBracketed
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

// parseMultiRegionTag handles multi-region tags in various formats:
//   - Comma-separated: "(USA, Europe)" → region:us + lang:en, region:eu
//   - Dash-separated: "(EU-US)" → region:eu, region:us + lang:en
//   - Comma-dash: "(USA,-Europe)" → region:us + lang:en, region:eu
//
// Returns individual region (and associated language) tags or nil if not a multi-region tag.
func parseMultiRegionTag(tag string) []CanonicalTag {
	// Multi-region tags can use commas, dashes, or combinations
	var parts []string

	// First, try splitting by comma (handles "USA, Europe" and "USA,-Europe")
	switch {
	case strings.Contains(tag, ","):
		parts = strings.Split(tag, ",")
	case strings.Contains(tag, "-"):
		// Split by dash (handles "EU-US")
		parts = strings.Split(tag, "-")
	default:
		return nil // Single region
	}

	var regions []CanonicalTag

	for _, part := range parts {
		// Clean up the part (remove leading/trailing whitespace and dashes)
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "-")
		if part == "" {
			continue
		}

		normalized := NormalizeTag(part)
		// Try to map as region
		mapped := mapFilenameTagToCanonical(normalized)
		for _, ct := range mapped {
			if ct.Type == TagTypeRegion || ct.Type == TagTypeLang {
				ct.Source = TagSourceBracketed
				regions = append(regions, ct)
			}
		}
	}

	// Only return if we found at least 2 region-related tags
	// (regions may also include their associated languages, so we check for any region/lang tags)
	regionCount := 0
	for _, ct := range regions {
		if ct.Type == TagTypeRegion {
			regionCount++
		}
	}
	if regionCount >= 2 {
		return regions
	}
	return nil
}

// disambiguateTag uses context to determine the correct canonical tag(s) for an ambiguous raw tag.
// This implements No-Intro/TOSEC conventions:
// - Parentheses tags appear in order: region → language → version → dev status
// - Bracket tags are always dump info
// - Tag position and previously seen tag types provide context
func disambiguateTag(ctx *ParseContext) []CanonicalTag {
	// Bracket tags need special handling BEFORE normalization
	// because some dump markers (!, !p) contain special characters
	if ctx.CurrentBracketType == BracketTypeSquare {
		return mapBracketTag(ctx.CurrentTag)
	}

	// For parentheses tags, normalize and process
	normalized := NormalizeTag(ctx.CurrentTag)

	// First check if it's a multi-language tag (En,Fr,De)
	if multiLang := parseMultiLanguageTag(normalized); multiLang != nil {
		return multiLang
	}

	// Check if it's a multi-region tag (USA, Europe or EU-US)
	if multiRegion := parseMultiRegionTag(normalized); multiRegion != nil {
		return multiRegion
	}

	// For parentheses tags, use position and context
	return mapParenthesisTag(normalized, ctx)
}

// mapBracketTag maps tags from square brackets [].
// These are always dump-related: verified, bad, hacked, cracked, fixed, translated, etc.
// NOTE: This function receives the raw tag (not normalized) to preserve special characters like !
func mapBracketTag(tag string) []CanonicalTag {
	// Special handling for dump markers with special characters (must come BEFORE normalization)
	switch tag {
	case "!":
		return []CanonicalTag{{Type: TagTypeDump, Value: TagDumpVerified, Source: TagSourceBracketed}}
	case "!p":
		return []CanonicalTag{{Type: TagTypeDump, Value: TagDumpPending, Source: TagSourceBracketed}}
	}

	// Normalize the tag for regular processing
	normalized := NormalizeTag(tag)

	// Handle standard dump info tags
	switch normalized {
	case "tr":
		// In brackets, "tr" is always "translated" (dump info)
		return []CanonicalTag{{Type: TagTypeDump, Value: TagDumpTranslated, Source: TagSourceBracketed}}
	case "b":
		return []CanonicalTag{{Type: TagTypeDump, Value: TagDumpBad, Source: TagSourceBracketed}}
	case "h":
		return []CanonicalTag{{Type: TagTypeDump, Value: TagDumpHacked, Source: TagSourceBracketed}}
	case "f":
		return []CanonicalTag{{Type: TagTypeDump, Value: TagDumpFixed, Source: TagSourceBracketed}}
	case "cr":
		return []CanonicalTag{{Type: TagTypeDump, Value: TagDumpCracked, Source: TagSourceBracketed}}
	case "t":
		return []CanonicalTag{{Type: TagTypeDump, Value: TagDumpTrained, Source: TagSourceBracketed}}
	default:
		// Try default mapping, filtering for dump-related tags only
		mapped := mapFilenameTagToCanonical(normalized)
		var dumpTags []CanonicalTag
		for _, ct := range mapped {
			if ct.Type == TagTypeDump || ct.Type == TagTypeUnlicensed {
				ct.Source = TagSourceBracketed
				dumpTags = append(dumpTags, ct)
			}
		}
		if len(dumpTags) > 0 {
			return dumpTags
		}
		return []CanonicalTag{{Type: TagTypeDump, Value: TagValue(normalized), Source: TagSourceBracketed}}
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
			if pt.Type == TagTypeLang && pt.Value == TagLangDE {
				return []CanonicalTag{{Type: TagTypeRegion, Value: TagRegionCH, Source: TagSourceBracketed}}
			}
		}
		// If no region yet and early in sequence, prefer region
		if !hasRegion && ctx.CurrentIndex < 2 {
			return []CanonicalTag{{Type: TagTypeRegion, Value: TagRegionCH, Source: TagSourceBracketed}}
		}
		// Otherwise, Chinese language
		return []CanonicalTag{{Type: TagTypeLang, Value: TagLangZH, Source: TagSourceBracketed}}

	case "tr":
		// In parentheses, "tr" is Turkey (region) or Turkish (language)
		// Never "translated" (that's only in brackets)
		if !hasRegion && ctx.CurrentIndex < 2 {
			return []CanonicalTag{{Type: TagTypeRegion, Value: TagRegionTR, Source: TagSourceBracketed}}
		}
		if !hasLanguage {
			return []CanonicalTag{{Type: TagTypeLang, Value: TagLangTR, Source: TagSourceBracketed}}
		}
		// Default to region if ambiguous
		return []CanonicalTag{{Type: TagTypeRegion, Value: TagRegionTR, Source: TagSourceBracketed}}

	case "bs":
		// "bs" in parentheses is almost always Bosnian language
		// Broadcast Satellite would be "satellaview" or in special hardware context
		return []CanonicalTag{{Type: TagTypeLang, Value: TagLangBS, Source: TagSourceBracketed}}

	case "hi":
		// In brackets, "hi" is "hacked intro"
		if ctx.CurrentBracketType == BracketTypeSquare {
			// Note: "hacked:intro" doesn't have a constant yet, keeping as raw string
			return []CanonicalTag{
				{Type: TagTypeDump, Value: TagDumpHacked, Source: TagSourceBracketed},
				{Type: TagTypeDump, Value: "hacked:intro", Source: TagSourceBracketed},
			}
		}
		// In parentheses, "hi" is Hindi language
		return []CanonicalTag{{Type: TagTypeLang, Value: TagLangHI, Source: TagSourceBracketed}}

	case "st":
		// "st" could be Sufami Turbo (SNES peripheral cartridge adapter) but that's rare
		// Context: if SNES-related or hardware tags present
		for _, pt := range ctx.ProcessedTags {
			if pt.Type == TagTypeAddon || pt.Type == TagTypeCompatibility {
				return []CanonicalTag{{Type: TagTypeAddon, Value: TagAddonPeripheralSufami, Source: TagSourceBracketed}}
			}
		}
		// Otherwise, fallback to map (might be unknown)
		return withSource(mapFilenameTagToCanonical(tag), TagSourceBracketed)

	case "np":
		// "np" could be Nintendo Power (SNES kiosk service) but uncommon
		// Context: if SNES-related or hardware tags present
		for _, pt := range ctx.ProcessedTags {
			if pt.Type == TagTypeAddon || pt.Type == TagTypeCompatibility {
				return []CanonicalTag{{
					Type:   TagTypeAddon,
					Value:  TagAddonOnlineNintendopower,
					Source: TagSourceBracketed,
				}}
			}
		}
		// Otherwise, fallback to map
		return withSource(mapFilenameTagToCanonical(tag), TagSourceBracketed)
	}

	// Try default mapping
	mapped := mapFilenameTagToCanonical(tag)
	if len(mapped) == 0 {
		return []CanonicalTag{{Type: TagTypeUnknown, Value: TagValue(tag), Source: TagSourceBracketed}}
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

	return withSource(mapped, TagSourceBracketed)
}

// withSource sets the Source field on all tags in a slice and returns the modified slice
func withSource(tags []CanonicalTag, source TagSource) []CanonicalTag {
	for i := range tags {
		tags[i].Source = source
	}
	return tags
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
		ctx.CurrentBracketType = BracketTypeParen

		resolved := disambiguateTag(ctx)
		allTags = append(allTags, resolved...)
		ctx.ProcessedTags = allTags // Update context with newly processed tags
	}

	// Step 4: Process bracket tags (dump info, hacks, etc.)
	for i, tag := range bracketTags {
		ctx.CurrentTag = tag
		ctx.CurrentIndex = i
		ctx.CurrentBracketType = BracketTypeSquare

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
//   - Converts underscores to spaces (common filename artifact)
//   - Removes all bracket content: (), [], {}, <>
//   - Normalizes multiple spaces to single space
//
// Examples:
//   - "Super Mario Bros (USA) [!]" → "Super Mario Bros"
//   - "Super_Mario_Bros (USA)" → "Super Mario Bros"
//   - "Sonic & Knuckles (USA)" → "Sonic & Knuckles"
//   - "Street Fighter II: The World Warrior" → "Street Fighter II: The World Warrior"
//   - "1. Game Title (USA)" → "Game Title" (if stripLeadingNumbers is true)
//   - "1942 (USA)" → "1942" (if stripLeadingNumbers is false)
//
// Note: This uses shared normalization functions from the slugs package to eliminate
// code duplication. However, it only applies transformations appropriate for display
// titles (no Roman numeral conversion, edition stripping, etc.).
func ParseTitleFromFilename(filename string, stripLeadingNumbers bool) string {
	// Import the slugs package for shared normalization functions
	// This eliminates code duplication while keeping display-appropriate behavior
	title := filename

	// Step 1: Normalize filename separators (underscores and dashes used as space substitutes)
	// Heuristic: If filename has no spaces AND contains 2+ separators, treat them as space substitutes
	// This handles: "super-mario-bros.sfc", "legend_of_zelda.sfc", "mega-man-x.sfc"
	// Preserves: "Spider-Man.sfc" (only 1 dash), "F-Zero.sfc" (only 1 dash)
	// IMPORTANT: Do this BEFORE leading number stripping so "01_super_mario" → "01 super mario" → "super mario"
	sepCount := strings.Count(title, "_") + strings.Count(title, "-")
	if !strings.Contains(title, " ") && sepCount >= 2 {
		title = strings.ReplaceAll(title, "_", " ")
		title = strings.ReplaceAll(title, "-", " ")
	}

	// Step 2: Optionally strip leading number prefixes (e.g., "1. ", "01 - ")
	// Only done when contextual detection confirms list-based numbering
	if stripLeadingNumbers {
		title = reLeadingNum.ReplaceAllString(title, "")
		title = strings.TrimSpace(title)
	}

	// Step 3: Remove all bracket content using shared function from slugs package
	// This replaces the previous regex-based extraction with a more robust implementation
	// that handles nested brackets and all bracket types uniformly
	title = slugs.StripMetadataBrackets(title)
	title = strings.TrimSpace(title)

	// Step 4: Normalize multiple spaces to single space
	// This handles cases where bracket removal creates gaps like "Game [USA] [!]" → "Game  "
	title = reMultiSpace.ReplaceAllString(title, " ")

	return strings.TrimSpace(title)
}
