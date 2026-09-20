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

package tags_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
)

func TestLookupRegionWord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want tags.TagValue
	}{
		{in: "World", want: tags.TagRegionWorld},
		{in: "Japan", want: tags.TagRegionJP},
		{in: "USA", want: tags.TagRegionUS},
		{in: "Europe", want: tags.TagRegionEU},
		// External sources spell multi-word regions with spaces where filenames
		// use dashes.
		{in: "Hong Kong", want: tags.TagRegionHK},
		{in: " korea ", want: tags.TagRegionKR},
		{in: "jp", want: tags.TagRegionJP},
		// Not a region the vocabulary knows, and a word that maps to something
		// other than a region.
		{in: "Hispanic"},
		{in: "Etc."},
		{in: ""},
	} {
		t.Run("region "+tc.in, func(t *testing.T) {
			t.Parallel()
			value, ok := tags.LookupRegionWord(tc.in)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, tc.want, value)
		})
	}
}

func TestLookupRegionWordReturnsNoImpliedLanguage(t *testing.T) {
	t.Parallel()
	// The filename table pairs Japan with Japanese, which is an inference about
	// a release rather than something a metadata source stated. Callers asking
	// for a region get only the region.
	value, ok := tags.LookupRegionWord("Japan")
	assert.True(t, ok)
	assert.Equal(t, tags.TagRegionJP, value)
	assert.NotEqual(t, tags.TagLangJA, value)
}

// TestExportedParseBuildDate pins the exported wrapper external metadata
// sources use; parseBuildDate's own behaviour is covered in string_parsers_test.
func TestExportedParseBuildDate(t *testing.T) {
	t.Parallel()
	// Arcade catalogs and MiSTer descriptor filenames both write YYMMDD, and
	// both must land on the same stored spelling.
	value, ok := tags.ParseBuildDate("900227")
	assert.True(t, ok)
	assert.Equal(t, "1990-02-27", value)

	_, ok = tags.ParseBuildDate("Phoenix Edition")
	assert.False(t, ok)
}
