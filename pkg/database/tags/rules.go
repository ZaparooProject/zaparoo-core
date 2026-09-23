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
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Tags are meant to be as stable as practical, so every tag type states which
// values it accepts and every write is checked against it. There are three
// kinds, and a type belongs to exactly one:
//
//   - KindClosed: the value must be one of the type's CanonicalTagDefinitions.
//     This is almost every type. A source whose words differ (a scraper's
//     "Shoot'em Up", a catalog's board name) maps onto a listed value or is
//     dropped; a genuinely new value is added to the list in code.
//   - KindFormat: there is no list, but the value must match a strict rule
//     (a year, a date, a disc number, a file extension, a versioned label).
//   - KindFreeText: any value, in normalised form. Only company names are
//     free text, because they cannot be listed. Adding a type to this kind is
//     a deliberate decision; see docs/scraper.md "Tag rules".

// TagKind says how a tag type's values are controlled.
type TagKind int

const (
	KindClosed TagKind = iota
	KindFormat
	KindFreeText
)

func (k TagKind) String() string {
	switch k {
	case KindClosed:
		return "closed"
	case KindFormat:
		return "format"
	case KindFreeText:
		return "free-text"
	default:
		return "kind(" + strconv.Itoa(int(k)) + ")"
	}
}

// TagRule is the value rule for one tag type.
type TagRule struct {
	// Format checks an unpadded value for KindFormat types. A closed or
	// free-text type leaves it nil.
	Format func(value string) bool
	Kind   TagKind
	// AlsoListed accepts a KindFormat value that is in the type's canonical
	// list even when the rule would not (year wildcards such as "198x", rev
	// letters, set labels such as "f1").
	AlsoListed bool
}

var (
	// ErrUnknownTagType reports a tag type Core does not define.
	ErrUnknownTagType = errors.New("unknown tag type")
	// ErrTagNotInVocabulary reports a value its type does not accept.
	ErrTagNotInVocabulary = errors.New("tag value not in vocabulary")
)

// RulesVersion changes whenever a rule in this file changes what it accepts.
// MediaDB folds it into its vocabulary stamp, so an upgrade that narrows a rule
// prunes stored values the new rule refuses. Bump it with any such change.
const RulesVersion = 1

// maxFreeTextTagLen bounds a free-text value in bytes.
const maxFreeTextTagLen = 128

// maxSequenceNumber bounds the numeric sequence types. Values are stored
// zero-padded to four digits, so this is also the largest that sorts right.
const maxSequenceNumber = 9999

var (
	reExtension  = regexp.MustCompile(`^[a-z0-9][a-z0-9_+-]{0,15}$`)
	reSetname    = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	reVersion    = regexp.MustCompile(`^[0-9a-z]{1,8}(-[0-9a-z]{1,8}){0,4}$`)
	reScraperID  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	reScrapeRun  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	reDeckTagID  = regexp.MustCompile(`^[0-9a-z]{8}$|^[0-9a-z]{12}$`)
	reYearDigits = regexp.MustCompile(`^\d{4}$`)
)

// TagRules holds the value rule for every tag type Core defines. A type
// missing from this map is unknown and refused. Tests assert that every
// TagType constant has an entry and that the free-text set stays exactly
// developer, publisher and credit.
var TagRules = map[TagType]TagRule{
	TagTypeInput:         {Kind: KindClosed},
	TagTypePlayers:       {Kind: KindClosed},
	TagTypeGenre:         {Kind: KindClosed},
	TagTypeAddon:         {Kind: KindClosed},
	TagTypeEmbedded:      {Kind: KindClosed},
	TagTypeSave:          {Kind: KindClosed},
	TagTypeArcadeBoard:   {Kind: KindClosed},
	TagTypeCabinet:       {Kind: KindClosed},
	TagTypeProtection:    {Kind: KindClosed},
	TagTypeCompatibility: {Kind: KindClosed},
	TagTypeSupplement:    {Kind: KindClosed},
	TagTypeDistribution:  {Kind: KindClosed},
	TagTypeBased:         {Kind: KindClosed},
	TagTypeSearch:        {Kind: KindClosed},
	TagTypeMultigame:     {Kind: KindClosed},
	TagTypeReboxed:       {Kind: KindClosed},
	TagTypePort:          {Kind: KindClosed},
	TagTypeLang:          {Kind: KindClosed},
	TagTypeUnfinished:    {Kind: KindClosed},
	TagTypeRerelease:     {Kind: KindClosed},
	TagTypeUnlicensed:    {Kind: KindClosed},
	TagTypeRegion:        {Kind: KindClosed},
	TagTypeVideo:         {Kind: KindClosed},
	TagTypeCopyright:     {Kind: KindClosed},
	TagTypeDump:          {Kind: KindClosed},
	TagTypeMedia:         {Kind: KindClosed},
	TagTypeEdition:       {Kind: KindClosed},
	TagTypePerspective:   {Kind: KindClosed},
	TagTypeArt:           {Kind: KindClosed},
	TagTypeAccessibility: {Kind: KindClosed},
	TagTypeUnknown:       {Kind: KindClosed},
	TagTypeRelease:       {Kind: KindClosed},
	TagTypeProperty:      {Kind: KindClosed},

	TagTypeYear:      {Kind: KindFormat, Format: isYear, AlsoListed: true},
	TagTypeBuildDate: {Kind: KindFormat, Format: isBuildDate},
	TagTypeRating:    {Kind: KindFormat, Format: isRating},
	TagTypeDisc:      {Kind: KindFormat, Format: isSequenceNumber},
	TagTypeDiscTotal: {Kind: KindFormat, Format: isSequenceNumber},
	TagTypeTrack:     {Kind: KindFormat, Format: isSequenceNumber},
	TagTypeSeason:    {Kind: KindFormat, Format: isSequenceNumber},
	TagTypeEpisode:   {Kind: KindFormat, Format: isSequenceNumber},
	TagTypeIssue:     {Kind: KindFormat, Format: isSequenceNumber},
	TagTypeVolume:    {Kind: KindFormat, Format: isSequenceNumber},
	TagTypeSet:       {Kind: KindFormat, Format: isSequenceNumber, AlsoListed: true},
	TagTypeAlt:       {Kind: KindFormat, Format: isSequenceNumber, AlsoListed: true},
	TagTypeRev:       {Kind: KindFormat, Format: reVersion.MatchString, AlsoListed: true},
	TagTypePatch:     {Kind: KindFormat, Format: isPatch},
	TagTypeExtension: {Kind: KindFormat, Format: reExtension.MatchString},
	TagTypeMameParent: {
		Kind: KindFormat, Format: reSetname.MatchString,
	},
	TagTypeUser: {Kind: KindFormat, Format: isUserTag},

	TagTypeDeveloper: {Kind: KindFreeText},
	TagTypePublisher: {Kind: KindFreeText},
	TagTypeCredit:    {Kind: KindFreeText},
}

// canonicalValueSets indexes CanonicalTagDefinitions for membership checks.
var canonicalValueSets = func() map[TagType]map[TagValue]struct{} {
	sets := make(map[TagType]map[TagValue]struct{}, len(CanonicalTagDefinitions))
	for tagType, values := range CanonicalTagDefinitions {
		set := make(map[TagValue]struct{}, len(values))
		for _, v := range values {
			set[v] = struct{}{}
		}
		sets[tagType] = set
	}
	return sets
}()

// RuleFor returns the rule for a tag type. Scraper bookkeeping types, which
// are named per scraper, get their own format rules. ok is false for a type
// Core does not define.
func RuleFor(tagType TagType) (TagRule, bool) {
	if rule, ok := TagRules[tagType]; ok {
		return rule, true
	}
	s := string(tagType)
	if id, found := strings.CutPrefix(s, ScraperTypePrefix); found && reScraperID.MatchString(id) {
		return TagRule{Kind: KindFormat, Format: isScraperSentinel}, true
	}
	if id, found := strings.CutPrefix(s, ScraperRunTypePrefix); found && reScraperID.MatchString(id) {
		return TagRule{Kind: KindFormat, Format: reScrapeRun.MatchString}, true
	}
	return TagRule{}, false
}

// IsKnownType reports whether Core defines a tag type.
func IsKnownType(tagType TagType) bool {
	_, ok := RuleFor(tagType)
	return ok
}

// IsCanonicalValue reports whether a value is in a type's canonical list.
func IsCanonicalValue(tagType TagType, value TagValue) bool {
	_, ok := canonicalValueSets[tagType][value]
	return ok
}

// ValidateTagValue checks a value against its type's rule. The value may be
// stored (zero-padded) or unpadded; the rule sees it unpadded. The returned
// error wraps ErrUnknownTagType or ErrTagNotInVocabulary.
func ValidateTagValue(tagType TagType, value string) error {
	rule, ok := RuleFor(tagType)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownTagType, tagType)
	}
	if !ruleAccepts(tagType, rule, UnpadTagValue(value)) {
		return fmt.Errorf("%w: %s:%s", ErrTagNotInVocabulary, tagType, UnpadTagValue(value))
	}
	return nil
}

// IsValidTagValue reports what ValidateTagValue checks, without building an
// error, for hot paths that only filter.
func IsValidTagValue(tagType TagType, value string) bool {
	rule, ok := RuleFor(tagType)
	return ok && ruleAccepts(tagType, rule, UnpadTagValue(value))
}

func ruleAccepts(tagType TagType, rule TagRule, v string) bool {
	if v == "" {
		return false
	}
	switch rule.Kind {
	case KindClosed:
		return IsCanonicalValue(tagType, TagValue(v))
	case KindFormat:
		if rule.Format != nil && rule.Format(v) {
			return true
		}
		return rule.AlsoListed && IsCanonicalValue(tagType, TagValue(v))
	case KindFreeText:
		return isFreeTextValue(v)
	default:
		return false
	}
}

// isFreeTextValue accepts the shape NormalizeCompanyName produces: words of
// lower-case letters and digits joined by single dashes, bounded in length.
func isFreeTextValue(v string) bool {
	if len(v) > maxFreeTextTagLen || !utf8.ValidString(v) {
		return false
	}
	for _, word := range strings.Split(v, "-") {
		if word == "" {
			return false
		}
		for _, r := range word {
			if unicode.IsUpper(r) || (!unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)) {
				return false
			}
		}
	}
	return true
}

func isYear(v string) bool {
	if !reYearDigits.MatchString(v) {
		return false
	}
	n, err := strconv.Atoi(v)
	return err == nil && n >= 1950 && n <= 2099
}

// isBuildDate accepts a full date or, where the source gives only a month, a
// year and month: "1991-04-02" or "1988-01".
func isBuildDate(v string) bool {
	if _, err := time.Parse(time.DateOnly, v); err == nil {
		return true
	}
	_, err := time.Parse("2006-01", v)
	return err == nil
}

func isRating(v string) bool {
	n, ok := parseDecimal(v)
	return ok && n <= 100
}

func isSequenceNumber(v string) bool {
	n, ok := parseDecimal(v)
	return ok && n >= 1 && n <= maxSequenceNumber
}

// parseDecimal accepts plain decimal digits only: no sign, no spaces.
func parseDecimal(v string) (int, bool) {
	if v == "" || len(v) > 5 {
		return 0, false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// isPatch accepts a listed patch label with an optional version, such as
// "fastrom" or "fastrom:1-1", or a font patch naming a listed language or
// region, "font:en" or "font:us".
func isPatch(v string) bool {
	label, rest, hasRest := strings.Cut(v, ":")
	if label == string(TagPatchFont) {
		return hasRest && (IsCanonicalValue(TagTypeLang, TagValue(rest)) ||
			IsCanonicalValue(TagTypeRegion, TagValue(rest)))
	}
	if !IsCanonicalValue(TagTypePatch, TagValue(label)) {
		return false
	}
	return !hasRest || reVersion.MatchString(rest)
}

// isUserTag accepts the fixed user flags and deck membership.
func isUserTag(v string) bool {
	if IsMutableUserTag(TagValue(v)) {
		return true
	}
	id, ok := ParseDeckTag(TagValue(v))
	return ok && reDeckTagID.MatchString(id)
}

func isScraperSentinel(v string) bool {
	return v == string(TagScraperScraped)
}
