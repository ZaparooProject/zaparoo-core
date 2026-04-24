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

package mediadb

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTagFilterSQL_Empty(t *testing.T) {
	clauses, args := BuildTagFilterSQL(nil)
	assert.Empty(t, clauses)
	assert.Empty(t, args)

	clauses, args = BuildTagFilterSQL([]zapscript.TagFilter{})
	assert.Empty(t, clauses)
	assert.Empty(t, args)
}

func TestBuildTagFilterSQL_ANDOnly(t *testing.T) {
	t.Run("Single AND filter", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)
		require.Len(t, args, 4) // 1 filter * 4 args (MediaTags + MediaTitleTags)

		// Should use INTERSECT pattern with both tag sources
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		assert.Contains(t, clauses[0], "SELECT MediaDBID FROM MediaTags")
		assert.Contains(t, clauses[0], "MediaTitleTags")
		assert.NotContains(t, clauses[0], "INTERSECT") // Single tag doesn't need INTERSECT
		assert.Equal(t, "region", args[0])
		assert.Equal(t, "usa", args[1])
		assert.Equal(t, "region", args[2])
		assert.Equal(t, "usa", args[3])
	})

	t.Run("Multiple AND filters", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)
		require.Len(t, args, 8) // 2 filters * 4 args each

		// Should use INTERSECT pattern for optimal performance
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		assert.Contains(t, clauses[0], "INTERSECT")
		assert.Equal(t, 2, strings.Count(clauses[0], "SELECT MediaDBID FROM MediaTags"))
		assert.Equal(t, 2, strings.Count(clauses[0], "MediaTitleTags"))
	})

	t.Run("Three AND filters", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
			{Type: "genre", Value: "action", Operator: zapscript.TagOperatorAND},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)
		require.Len(t, args, 12) // 3 filters * 4 args each

		// Should have 2 INTERSECT operators for 3 filters
		assert.Equal(t, 2, strings.Count(clauses[0], "INTERSECT"))
		assert.Equal(t, 3, strings.Count(clauses[0], "SELECT MediaDBID FROM MediaTags"))
	})
}

func TestBuildTagFilterSQL_NOTOnly(t *testing.T) {
	t.Run("Single NOT filter", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)
		require.Len(t, args, 4) // 1 filter * 4 args (MediaTags + MediaTitleTags)

		// Should use NOT EXISTS pattern for both tag sources
		assert.Contains(t, clauses[0], "NOT EXISTS (")
		assert.Contains(t, clauses[0], "MediaTags.MediaDBID = Media.DBID")
		assert.Contains(t, clauses[0], "MediaTitleTags.MediaTitleDBID = Media.MediaTitleDBID")
		assert.Equal(t, "unfinished", args[0])
		assert.Equal(t, "demo", args[1])
		assert.Equal(t, "unfinished", args[2])
		assert.Equal(t, "demo", args[3])
	})

	t.Run("Multiple NOT filters", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
			{Type: "unfinished", Value: "beta", Operator: zapscript.TagOperatorNOT},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 2) // Each NOT filter gets its own clause
		require.Len(t, args, 8)    // 2 filters * 4 args each

		// Both should use NOT EXISTS for both tag sources
		assert.Contains(t, clauses[0], "NOT EXISTS")
		assert.Contains(t, clauses[1], "NOT EXISTS")
	})
}

func TestBuildTagFilterSQL_OROnly(t *testing.T) {
	t.Run("Single OR filter", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)
		require.Len(t, args, 4) // 1 filter * 4 args (MediaTags + MediaTitleTags)

		// Should use EXISTS with single condition for both tag sources
		assert.Contains(t, clauses[0], "EXISTS (")
		assert.Contains(t, clauses[0], "MediaTags.MediaDBID = Media.DBID")
		assert.Contains(t, clauses[0], "MediaTitleTags.MediaTitleDBID = Media.MediaTitleDBID")
	})

	t.Run("Multiple OR filters", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "es", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "fr", Operator: zapscript.TagOperatorOR},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1) // All OR filters grouped into one clause
		require.Len(t, args, 12)   // 3 filters * 2 args * 2 sources

		// Should use EXISTS with OR conditions for both tag sources
		assert.Contains(t, clauses[0], "EXISTS (")
		assert.Equal(t, 6, strings.Count(clauses[0], "TagTypes.Type = ?")) // 3 per source
	})
}

func TestBuildTagFilterSQL_MixedOperators(t *testing.T) {
	t.Run("AND + NOT", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 2) // One for AND, one for NOT
		require.Len(t, args, 8)    // 2 filters * 4 args each

		// First clause should be INTERSECT (or IN for single)
		assert.Contains(t, clauses[0], "Media.DBID IN (")

		// Second clause should be NOT EXISTS
		assert.Contains(t, clauses[1], "NOT EXISTS")
	})

	t.Run("AND + OR", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "es", Operator: zapscript.TagOperatorOR},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 2) // One for AND, one for OR group
		require.Len(t, args, 12)   // 1 AND*4 + 2 OR*4

		// First clause should be INTERSECT
		assert.Contains(t, clauses[0], "Media.DBID IN (")

		// Second clause should be EXISTS with OR
		assert.Contains(t, clauses[1], "EXISTS (")
	})

	t.Run("NOT + OR", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "es", Operator: zapscript.TagOperatorOR},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 2) // One for NOT, one for OR group
		require.Len(t, args, 12)   // 1 NOT*4 + 2 OR*4

		// First clause should be NOT EXISTS
		assert.Contains(t, clauses[0], "NOT EXISTS")

		// Second clause should be EXISTS with OR
		assert.Contains(t, clauses[1], "EXISTS (")
	})

	t.Run("AND + NOT + OR", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "genre", Value: "action", Operator: zapscript.TagOperatorAND},
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
			{Type: "unfinished", Value: "beta", Operator: zapscript.TagOperatorNOT},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "es", Operator: zapscript.TagOperatorOR},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 4) // 1 AND group, 2 NOT, 1 OR group
		require.Len(t, args, 24)   // 2 AND*4 + 2 NOT*4 + 2 OR*4

		// First clause: INTERSECT for AND
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		assert.Contains(t, clauses[0], "INTERSECT")

		// Next two clauses: NOT EXISTS
		assert.Contains(t, clauses[1], "NOT EXISTS")
		assert.Contains(t, clauses[2], "NOT EXISTS")

		// Last clause: EXISTS with OR
		assert.Contains(t, clauses[3], "EXISTS (")
	})
}

func TestBuildTagFilterSQL_ArgumentOrder(t *testing.T) {
	t.Run("Arguments match clause order", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
			{Type: "players", Value: "2", Operator: zapscript.TagOperatorOR},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 3) // AND group, NOT, OR
		require.Len(t, args, 16)   // 4 filters * 4 args each

		// Verify argument order: AND filters (doubled for UNION), then NOT (doubled), then OR (doubled)
		// AND: region/usa for MediaTags, region/usa for MediaTitleTags
		assert.Equal(t, "region", args[0])
		assert.Equal(t, "usa", args[1])
		assert.Equal(t, "region", args[2])
		assert.Equal(t, "usa", args[3])
		// AND: lang/en for MediaTags, lang/en for MediaTitleTags
		assert.Equal(t, "lang", args[4])
		assert.Equal(t, "en", args[5])
		assert.Equal(t, "lang", args[6])
		assert.Equal(t, "en", args[7])
		// NOT: unfinished/demo for MediaTags, unfinished/demo for MediaTitleTags
		assert.Equal(t, "unfinished", args[8])
		assert.Equal(t, "demo", args[9])
		assert.Equal(t, "unfinished", args[10])
		assert.Equal(t, "demo", args[11])
		// OR: players/2 padded to 0002 for MediaTags, players/0002 for MediaTitleTags
		assert.Equal(t, "players", args[12])
		assert.Equal(t, "0002", args[13])
		assert.Equal(t, "players", args[14])
		assert.Equal(t, "0002", args[15])
	})
}

// TestBuildTagFilterSQL_AliasCanonicalisation verifies that deprecated tag forms are rewritten
// to their canonical equivalents in the SQL arguments produced by BuildTagFilterSQL.
func TestBuildTagFilterSQL_AliasCanonicalisation(t *testing.T) {
	filters := []zapscript.TagFilter{
		{Type: "addon", Value: "barcodeboy", Operator: zapscript.TagOperatorAND},
		{Type: "addon", Value: "controller:jcart", Operator: zapscript.TagOperatorNOT},
		{Type: "addon", Value: "controller:rumble", Operator: zapscript.TagOperatorOR},
	}

	_, args := BuildTagFilterSQL(filters)

	require.Len(t, args, 12)
	// AND: addon:barcodeboy → addon / barcode:barcodeboy (doubled for UNION)
	assert.Equal(t, "addon", args[0])
	assert.Equal(t, "barcode:barcodeboy", args[1])
	assert.Equal(t, "addon", args[2])
	assert.Equal(t, "barcode:barcodeboy", args[3])

	// NOT: addon:controller:jcart → embedded / slot:jcart (doubled)
	assert.Equal(t, "embedded", args[4])
	assert.Equal(t, "slot:jcart", args[5])
	assert.Equal(t, "embedded", args[6])
	assert.Equal(t, "slot:jcart", args[7])

	// OR: addon:controller:rumble → embedded / vibration:rumble (doubled)
	assert.Equal(t, "embedded", args[8])
	assert.Equal(t, "vibration:rumble", args[9])
	assert.Equal(t, "embedded", args[10])
	assert.Equal(t, "vibration:rumble", args[11])
}

// TestBuildTagFilterSQL_SQLInjectionSafety tests that generated SQL is safe from injection
func TestBuildTagFilterSQL_SQLInjectionSafety(t *testing.T) {
	tests := []struct {
		name        string
		description string
		filters     []zapscript.TagFilter
	}{
		{
			name: "SQL injection in type",
			filters: []zapscript.TagFilter{
				{Type: "region'; DROP TABLE Media; --", Value: "usa", Operator: zapscript.TagOperatorAND},
			},
			description: "SQL injection attempts in type should be parameterized",
		},
		{
			name: "SQL injection in value",
			filters: []zapscript.TagFilter{
				{Type: "region", Value: "usa' OR '1'='1", Operator: zapscript.TagOperatorAND},
			},
			description: "SQL injection attempts in value should be parameterized",
		},
		{
			name: "SQL comment injection",
			filters: []zapscript.TagFilter{
				{Type: "region", Value: "usa--", Operator: zapscript.TagOperatorNOT},
			},
			description: "SQL comments should be parameterized",
		},
		{
			name: "UNION injection attempt",
			filters: []zapscript.TagFilter{
				{Type: "region", Value: "usa' UNION SELECT * FROM tags", Operator: zapscript.TagOperatorOR},
			},
			description: "UNION injection should be parameterized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clauses, args := BuildTagFilterSQL(tt.filters)

			// Should generate clauses
			assert.NotEmpty(t, clauses, tt.description)
			assert.NotEmpty(t, args, tt.description)

			// All user input should be in args, not in SQL string
			for _, clause := range clauses {
				for _, filter := range tt.filters {
					// SQL should not contain literal filter values
					assert.NotContains(t, clause, filter.Type+" =", tt.description)
					assert.NotContains(t, clause, filter.Value+" =", tt.description)
				}

				// SQL should use placeholders (?)
				assert.Contains(t, clause, "?", tt.description)
			}

			// Args should contain the actual values
			assert.Contains(t, args, tt.filters[0].Type)
			assert.Contains(t, args, tt.filters[0].Value)
		})
	}
}

// TestBuildTagFilterSQL_SpecialCharacters tests handling of special characters
func TestBuildTagFilterSQL_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name    string
		filters []zapscript.TagFilter
	}{
		{
			name: "Unicode characters",
			//nolint:gosmopolitan // Test requires non-ASCII to verify SQL parameterization
			filters: []zapscript.TagFilter{
				{Type: "lang", Value: "日本語", Operator: zapscript.TagOperatorAND},
			},
		},
		{
			name: "Emoji",
			filters: []zapscript.TagFilter{
				{Type: "mood", Value: "😀", Operator: zapscript.TagOperatorOR},
			},
		},
		{
			name: "Special SQL characters",
			filters: []zapscript.TagFilter{
				{Type: "name", Value: "it's a game (beta) [usa]", Operator: zapscript.TagOperatorAND},
			},
		},
		{
			name: "Newlines and tabs",
			filters: []zapscript.TagFilter{
				{Type: "text", Value: "line1\nline2\ttab", Operator: zapscript.TagOperatorNOT},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clauses, args := BuildTagFilterSQL(tt.filters)

			// Should generate valid SQL
			assert.NotEmpty(t, clauses)
			assert.NotEmpty(t, args)

			// All special characters should be in args
			assert.Contains(t, args, tt.filters[0].Value)
		})
	}
}

// TestBuildTagFilterSQL_LargeScale tests handling of large numbers of filters
func TestBuildTagFilterSQL_LargeScale(t *testing.T) {
	t.Run("Many AND filters", func(t *testing.T) {
		filters := make([]zapscript.TagFilter, 20)
		for i := range filters {
			filters[i] = zapscript.TagFilter{
				Type:     "tag",
				Value:    "value",
				Operator: zapscript.TagOperatorAND,
			}
		}

		clauses, args := BuildTagFilterSQL(filters)
		assert.NotEmpty(t, clauses)
		assert.Len(t, args, 80) // 20 filters * 4 args each (MediaTags + MediaTitleTags)
	})

	t.Run("Many OR filters", func(t *testing.T) {
		filters := make([]zapscript.TagFilter, 15)
		for i := range filters {
			filters[i] = zapscript.TagFilter{
				Type:     "lang",
				Value:    "en",
				Operator: zapscript.TagOperatorOR,
			}
		}

		clauses, args := BuildTagFilterSQL(filters)
		assert.Len(t, clauses, 1) // All ORs grouped together
		assert.Len(t, args, 60)   // 15 filters * 2 args * 2 sources
	})

	t.Run("Many NOT filters", func(t *testing.T) {
		filters := make([]zapscript.TagFilter, 10)
		for i := range filters {
			filters[i] = zapscript.TagFilter{
				Type:     "unfinished",
				Value:    "demo",
				Operator: zapscript.TagOperatorNOT,
			}
		}

		clauses, args := BuildTagFilterSQL(filters)
		assert.Len(t, clauses, 10) // Each NOT gets its own clause
		assert.Len(t, args, 40)    // 10 filters * 4 args each (MediaTags + MediaTitleTags)
	})
}

// TestBuildTagFilterSQL_EdgeCases tests edge cases in SQL generation
func TestBuildTagFilterSQL_EdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		description     string
		filters         []zapscript.TagFilter
		expectedClauses int
		expectedArgsMin int
	}{
		{
			name: "Single filter of each operator type",
			filters: []zapscript.TagFilter{
				{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
				{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
				{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			},
			expectedClauses: 3,
			expectedArgsMin: 12, // 3 filters * 4 args each (MediaTags + MediaTitleTags)
			description:     "Should generate 3 separate clauses",
		},
		{
			name: "Very long tag values",
			filters: []zapscript.TagFilter{
				{Type: "description", Value: string(make([]byte, 500)), Operator: zapscript.TagOperatorAND},
			},
			expectedClauses: 1,
			expectedArgsMin: 4, // 1 filter * 4 args (MediaTags + MediaTitleTags)
			description:     "Should handle very long values",
		},
		{
			name: "Empty string values (after normalization)",
			filters: []zapscript.TagFilter{
				{Type: "tag", Value: "", Operator: zapscript.TagOperatorAND},
			},
			expectedClauses: 1,
			expectedArgsMin: 4, // 1 filter * 4 args (MediaTags + MediaTitleTags)
			description:     "Should handle empty values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clauses, args := BuildTagFilterSQL(tt.filters)
			assert.Len(t, clauses, tt.expectedClauses, tt.description)
			assert.GreaterOrEqual(t, len(args), tt.expectedArgsMin, tt.description)
		})
	}
}

// TestBuildTagFilterSQL_SQLStructure tests the structure of generated SQL
func TestBuildTagFilterSQL_SQLStructure(t *testing.T) {
	t.Run("AND filters use INTERSECT pattern with both tag sources", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
		}

		clauses, _ := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)

		// Should contain INTERSECT with both MediaTags and MediaTitleTags
		assert.Contains(t, clauses[0], "INTERSECT")
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		assert.Contains(t, clauses[0], "SELECT MediaDBID FROM MediaTags")
		assert.Contains(t, clauses[0], "MediaTitleTags")
	})

	t.Run("NOT filters use NOT EXISTS pattern with both tag sources", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
		}

		clauses, _ := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)

		// Should contain NOT EXISTS for both MediaTags and MediaTitleTags
		assert.Contains(t, clauses[0], "NOT EXISTS")
		assert.Contains(t, clauses[0], "MediaTags.MediaDBID = Media.DBID")
		assert.Contains(t, clauses[0], "MediaTitleTags.MediaTitleDBID = Media.MediaTitleDBID")
	})

	t.Run("OR filters use EXISTS with OR pattern for both tag sources", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "es", Operator: zapscript.TagOperatorOR},
		}

		clauses, _ := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)

		// Should contain EXISTS with OR for both MediaTags and MediaTitleTags
		assert.Contains(t, clauses[0], "EXISTS (")
		assert.Contains(t, clauses[0], " OR ")
		assert.Contains(t, clauses[0], "MediaTags.MediaDBID = Media.DBID")
		assert.Contains(t, clauses[0], "MediaTitleTags.MediaTitleDBID = Media.MediaTitleDBID")
	})
}

// TestBuildTagFilterSQL_Regression tests for previously found bugs
func TestBuildTagFilterSQL_Regression(t *testing.T) {
	tests := []struct {
		validate    func(t *testing.T, clauses []string, args []any)
		name        string
		description string
		filters     []zapscript.TagFilter
	}{
		{
			name: "Single AND filter should not have INTERSECT",
			filters: []zapscript.TagFilter{
				{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			},
			description: "Single AND filter doesn't need INTERSECT operator",
			validate: func(t *testing.T, clauses []string, _ []any) {
				assert.Len(t, clauses, 1)
				assert.NotContains(t, clauses[0], "INTERSECT")
				assert.Contains(t, clauses[0], "Media.DBID IN (")
			},
		},
		{
			name: "Single OR filter uses EXISTS for both tag sources",
			filters: []zapscript.TagFilter{
				{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			},
			description: "Single OR filter uses EXISTS with both MediaTags and MediaTitleTags",
			validate: func(t *testing.T, clauses []string, _ []any) {
				assert.Len(t, clauses, 1)
				assert.Contains(t, clauses[0], "EXISTS (")
				assert.Contains(t, clauses[0], "MediaTags")
				assert.Contains(t, clauses[0], "MediaTitleTags")
				// Single OR condition appears in both EXISTS clauses (one per tag source)
				assert.Equal(t, 2, strings.Count(clauses[0], "TagTypes.Type = ?"))
			},
		},
		{
			name: "All args must be used in order with doubled args for both tag sources",
			filters: []zapscript.TagFilter{
				{Type: "a", Value: "1", Operator: zapscript.TagOperatorAND},
				{Type: "b", Value: "2", Operator: zapscript.TagOperatorNOT},
				{Type: "c", Value: "3", Operator: zapscript.TagOperatorOR},
			},
			description: "Arguments must match SQL clause order with MediaTags + MediaTitleTags",
			validate: func(t *testing.T, _ []string, args []any) {
				require.Len(t, args, 12) // 3 filters * 4 args each
				// AND: a/1 padded to 0001 for MediaTags, a/0001 for MediaTitleTags
				assert.Equal(t, "a", args[0])
				assert.Equal(t, "0001", args[1])
				assert.Equal(t, "a", args[2])
				assert.Equal(t, "0001", args[3])
				// NOT: b/2 padded to 0002 for MediaTags, b/0002 for MediaTitleTags
				assert.Equal(t, "b", args[4])
				assert.Equal(t, "0002", args[5])
				assert.Equal(t, "b", args[6])
				assert.Equal(t, "0002", args[7])
				// OR: c/3 padded to 0003 for MediaTags, c/0003 for MediaTitleTags
				assert.Equal(t, "c", args[8])
				assert.Equal(t, "0003", args[9])
				assert.Equal(t, "c", args[10])
				assert.Equal(t, "0003", args[11])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clauses, args := BuildTagFilterSQL(tt.filters)
			tt.validate(t, clauses, args)
		})
	}
}

func TestExpandCreditFilters(t *testing.T) {
	t.Parallel()

	t.Run("AND credit passes through unchanged", func(t *testing.T) {
		input := []zapscript.TagFilter{
			{Type: "credit", Value: "konami", Operator: zapscript.TagOperatorAND},
		}
		got := expandCreditFilters(input)
		require.Len(t, got, 1)
		assert.Equal(t, zapscript.TagOperatorAND, got[0].Operator)
		assert.Equal(t, "credit", got[0].Type)
		assert.Equal(t, "konami", got[0].Value)
	})

	t.Run("NOT credit expands to three NOT filters", func(t *testing.T) {
		input := []zapscript.TagFilter{
			{Type: "credit", Value: "sega", Operator: zapscript.TagOperatorNOT},
		}
		got := expandCreditFilters(input)
		require.Len(t, got, 3)
		for _, f := range got {
			assert.Equal(t, zapscript.TagOperatorNOT, f.Operator)
			assert.Equal(t, "sega", f.Value)
		}
	})

	t.Run("OR credit expands to three OR filters", func(t *testing.T) {
		input := []zapscript.TagFilter{
			{Type: "credit", Value: "nintendo", Operator: zapscript.TagOperatorOR},
		}
		got := expandCreditFilters(input)
		require.Len(t, got, 3)
		for _, f := range got {
			assert.Equal(t, zapscript.TagOperatorOR, f.Operator)
		}
	})

	t.Run("Non-credit filters pass through unchanged", func(t *testing.T) {
		input := []zapscript.TagFilter{
			{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
			{Type: "credit", Value: "capcom", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
		}
		got := expandCreditFilters(input)
		// region, AND credit, and lang all pass through unchanged (AND credit is not expanded here)
		require.Len(t, got, 3)
		assert.Equal(t, "region", got[0].Type)
		assert.Equal(t, "credit", got[1].Type)
		assert.Equal(t, "lang", got[2].Type)
	})

	t.Run("Empty input returns empty", func(t *testing.T) {
		assert.Empty(t, expandCreditFilters(nil))
		assert.Empty(t, expandCreditFilters([]zapscript.TagFilter{}))
	})
}

func TestBuildTagFilterSQL_CreditUnion(t *testing.T) {
	t.Parallel()

	t.Run("AND credit generates IN clause covering all credit roles", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "credit", Value: "nintendo", Operator: zapscript.TagOperatorAND},
		}
		clauses, args := BuildTagFilterSQL(filters)
		// Single AND credit → one per-filter IN clause (not via OR expansion)
		require.Len(t, clauses, 1)
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		// Types are in args (placeholders), not embedded in SQL
		argStrs := make([]string, 0, len(args))
		for _, a := range args {
			if s, ok := a.(string); ok {
				argStrs = append(argStrs, s)
			}
		}
		assert.Contains(t, argStrs, "developer")
		assert.Contains(t, argStrs, "publisher")
		assert.Contains(t, argStrs, "credit")
		assert.Contains(t, argStrs, "nintendo")
	})

	t.Run("two AND credit filters intersect, not union", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "credit", Value: "konami", Operator: zapscript.TagOperatorAND},
			{Type: "credit", Value: "capcom", Operator: zapscript.TagOperatorAND},
		}
		clauses, args := BuildTagFilterSQL(filters)
		// Two AND credit filters → two separate IN clauses, joined with AND by caller
		require.Len(t, clauses, 2)
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		assert.Contains(t, clauses[1], "Media.DBID IN (")
		argStrs := make([]string, 0, len(args))
		for _, a := range args {
			if s, ok := a.(string); ok {
				argStrs = append(argStrs, s)
			}
		}
		assert.Contains(t, argStrs, "konami")
		assert.Contains(t, argStrs, "capcom")
	})

	t.Run("credit AND combined with region AND", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
			{Type: "credit", Value: "konami", Operator: zapscript.TagOperatorAND},
		}
		clauses, _ := BuildTagFilterSQL(filters)
		// One INTERSECT clause for region (AND), one IN clause for credit
		require.Len(t, clauses, 2)
	})
}

func TestBuildTagFilterSQL_EditionAndRelease(t *testing.T) {
	t.Parallel()

	t.Run("edition filter is strict (no expansion)", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "edition", Value: "special", Operator: zapscript.TagOperatorAND},
		}
		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)
		// AND path: Media.DBID IN (...), not an OR EXISTS
		assert.Contains(t, clauses[0], "Media.DBID IN")
		assert.NotContains(t, clauses[0], "EXISTS")
		argStrs := make([]string, 0, len(args))
		for _, a := range args {
			if s, ok := a.(string); ok {
				argStrs = append(argStrs, s)
			}
		}
		assert.Contains(t, argStrs, "edition")
		assert.Contains(t, argStrs, "special")
		// Must not expand to developer/publisher/credit
		assert.NotContains(t, argStrs, "developer")
		assert.NotContains(t, argStrs, "publisher")
		assert.NotContains(t, argStrs, "credit")
	})

	t.Run("release filter is strict (no expansion)", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "release", Value: "homebrew", Operator: zapscript.TagOperatorAND},
		}
		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)
		assert.Contains(t, clauses[0], "Media.DBID IN")
		assert.NotContains(t, clauses[0], "EXISTS")
		argStrs := make([]string, 0, len(args))
		for _, a := range args {
			if s, ok := a.(string); ok {
				argStrs = append(argStrs, s)
			}
		}
		assert.Contains(t, argStrs, "release")
		assert.Contains(t, argStrs, "homebrew")
	})
}
