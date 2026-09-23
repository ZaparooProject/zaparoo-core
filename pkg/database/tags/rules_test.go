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
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredTagTypes reads every TagType constant from tags.go, so a type added
// there without a rule fails the tests below rather than slipping through.
func declaredTagTypes(t *testing.T) map[string]TagType {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "tags.go", nil, 0)
	require.NoError(t, err)
	declared := make(map[string]TagType)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != "TagType" {
				continue
			}
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				require.True(t, ok, "TagType constant %s must be a string literal", name.Name)
				value, unquoteErr := strconv.Unquote(lit.Value)
				require.NoError(t, unquoteErr)
				declared[name.Name] = TagType(value)
			}
		}
	}
	require.NotEmpty(t, declared)
	return declared
}

// TestEveryTagTypeHasARuleAndADefinition is the guard against a new tag type
// that nothing controls. Every declared type needs a rule saying which values
// it accepts, and a CanonicalTagDefinitions entry so seeding creates it.
func TestEveryTagTypeHasARuleAndADefinition(t *testing.T) {
	t.Parallel()
	declared := declaredTagTypes(t)
	for name, tagType := range declared {
		_, hasRule := TagRules[tagType]
		assert.True(t, hasRule, "%s (%q) has no entry in TagRules", name, tagType)
		_, hasDefinition := CanonicalTagDefinitions[tagType]
		assert.True(t, hasDefinition, "%s (%q) has no entry in CanonicalTagDefinitions", name, tagType)
	}

	values := make(map[TagType]struct{}, len(declared))
	for _, tagType := range declared {
		values[tagType] = struct{}{}
	}
	for tagType := range TagRules {
		_, ok := values[tagType]
		assert.True(t, ok, "TagRules names %q, which is not a declared TagType", tagType)
	}
	for tagType := range CanonicalTagDefinitions {
		_, ok := values[tagType]
		assert.True(t, ok, "CanonicalTagDefinitions names %q, which is not a declared TagType", tagType)
	}
	for tagType := range CanonicalIsExclusive {
		_, ok := values[tagType]
		assert.True(t, ok, "CanonicalIsExclusive names %q, which is not a declared TagType", tagType)
	}
}

// TestFreeTextTypesAreOnlyCompanyNames is a tripwire. Tags are meant to be
// stable, so free text is limited to company names, which cannot be listed.
// If this fails because a type was added, that is a design change: agree it,
// then update this test and the "Tag rules" section of docs/scraper.md.
func TestFreeTextTypesAreOnlyCompanyNames(t *testing.T) {
	t.Parallel()
	var free []string
	for tagType, rule := range TagRules {
		if rule.Kind == KindFreeText {
			free = append(free, string(tagType))
		}
	}
	sort.Strings(free)
	assert.Equal(t, []string{"credit", "developer", "publisher"}, free)
}

// TestClosedTypesListTheirValues keeps a closed type from being declared with
// an empty list, which would refuse every value it is given.
func TestClosedTypesListTheirValues(t *testing.T) {
	t.Parallel()
	for tagType, rule := range TagRules {
		switch rule.Kind {
		case KindClosed:
			assert.NotEmpty(t, CanonicalTagDefinitions[tagType], "closed type %q lists no values", tagType)
		case KindFormat:
			assert.NotNil(t, rule.Format, "format type %q has no format rule", tagType)
		case KindFreeText:
			assert.Nil(t, rule.Format, "free-text type %q must not carry a format rule", tagType)
		}
	}
}

// TestEveryCanonicalValueIsAccepted keeps the lists and the rules agreeing: a
// listed value its own rule refuses could never be stored.
func TestEveryCanonicalValueIsAccepted(t *testing.T) {
	t.Parallel()
	for tagType, values := range CanonicalTagDefinitions {
		for _, value := range values {
			require.NoError(t, ValidateTagValue(tagType, string(value)))
			require.NoError(t, ValidateTagValue(tagType, PadTagValue(string(value))),
				"the stored, padded form must be accepted too")
		}
	}
}

func TestValidateTagValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tagType TagType
		value   string
		valid   bool
	}{
		// Closed: listed values only.
		{TagTypeGenre, "shmup:v", true},
		{TagTypeGenre, "action", true},
		{TagTypeGenre, "shootem-up", false},
		{TagTypeRegion, "us", true},
		{TagTypeRegion, "usa", false},
		{TagTypeArcadeBoard, "capcom:cps2", true},
		{TagTypeArcadeBoard, "cps2", false},
		{TagTypePlayers, "2", true},
		{TagTypePlayers, "0002", true},
		{TagTypePlayers, "11", false},
		{TagTypeUnknown, "unknown", true},
		{TagTypeUnknown, "blah", false},
		{TagTypeSearch, "franchise:castlevania", true},
		{TagTypeSearch, "franchise:not-a-listed-series", false},
		// Format rules.
		{TagTypeYear, "1987", true},
		{TagTypeYear, "198x", true},
		{TagTypeYear, "1949", false},
		{TagTypeYear, "87", false},
		{TagTypeBuildDate, "1991-04-02", true},
		{TagTypeBuildDate, "1991-02-30", false},
		{TagTypeBuildDate, "910402", false},
		{TagTypeBuildDate, "1988-01", true},
		{TagTypeBuildDate, "1988-13", false},
		{TagTypeRating, "0", true},
		{TagTypeRating, "0085", true},
		{TagTypeRating, "100", true},
		{TagTypeRating, "101", false},
		{TagTypeRating, "-1", false},
		{TagTypeDisc, "1", true},
		{TagTypeDisc, "0", false},
		{TagTypeSeason, "12", true},
		{TagTypeEpisode, "10000", false},
		{TagTypeSet, "f1", true},
		{TagTypeSet, "3", true},
		{TagTypeRev, "a", true},
		{TagTypeRev, "1-2", true},
		{TagTypeRev, "1 2", false},
		{TagTypePatch, "fastrom", true},
		{TagTypePatch, "fastrom:1-1", true},
		{TagTypePatch, "font:en", true},
		{TagTypePatch, "font:us", true},
		{TagTypePatch, "font:xx", false},
		{TagTypePatch, "made-up-patch", false},
		{TagTypeExtension, "sfc", true},
		{TagTypeExtension, "7z", true},
		{TagTypeExtension, ".sfc", false},
		{TagTypeMameParent, "sf2", true},
		{TagTypeMameParent, "SF2", false},
		{TagTypeUser, "favorite", true},
		{TagTypeUser, "deck:0123456789ab", true},
		{TagTypeUser, "deck:", false},
		{TagTypeUser, "anything", false},
		{ScraperType("gamelist.xml"), "scraped", true},
		{ScraperType("gamelist.xml"), "other", false},
		{ScraperRunType("mister-arcade"), "3f2b9c1e-7a1d-4c55-9a55-1b6e2d7c0f11", true},
		// Free text: normalised company names.
		{TagTypeDeveloper, "t-and-e-soft", true},
		{TagTypeDeveloper, "T&E Soft", false},
		{TagTypePublisher, "nintendo", true},
		{TagTypePublisher, "nin:tendo", false},
		{TagTypeCredit, "", false},
		// Types Core does not define.
		{"gamegenre", "action", false},
		{"gamefamily", "mario", false},
		{"made-up", "x", false},
	}
	for _, tt := range tests {
		err := ValidateTagValue(tt.tagType, tt.value)
		if tt.valid {
			assert.NoError(t, err, "%s:%s", tt.tagType, tt.value)
		} else {
			assert.Error(t, err, "%s:%s", tt.tagType, tt.value)
		}
	}
}

func TestValidateTagValueErrors(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, ValidateTagValue("gamegenre", "action"), ErrUnknownTagType)
	require.ErrorIs(t, ValidateTagValue(TagTypeGenre, "shootem-up"), ErrTagNotInVocabulary)
}

// FuzzNormalizeCompanyNameIsAccepted checks that the normaliser for the
// free-text types only ever produces values those types accept.
func FuzzNormalizeCompanyNameIsAccepted(f *testing.F) {
	for _, seed := range []string{
		"T&E Soft", "Nintendo", "SNK Playmore", "Hudson Soft / Konami",
		"コナミ", "Sega Enterprises, Ltd.", "Data East:USA", "  ", "id Software",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		value := string(NormalizeCompanyName(raw))
		if value == "" || len(value) > maxFreeTextTagLen {
			return
		}
		if err := ValidateTagValue(TagTypeDeveloper, value); err != nil {
			t.Fatalf("NormalizeCompanyName(%q) = %q, refused: %v", raw, value, err)
		}
	})
}
