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
		require.Len(t, args, 2)

		// Should use INTERSECT pattern (even for single tag)
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		assert.Contains(t, clauses[0], "SELECT MediaDBID FROM MediaTags")
		assert.NotContains(t, clauses[0], "INTERSECT") // Single tag doesn't need INTERSECT
		assert.Equal(t, "region", args[0])
		assert.Equal(t, "usa", args[1])
	})

	t.Run("Multiple AND filters", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)
		require.Len(t, args, 4) // 2 filters * 2 args each

		// Should use INTERSECT pattern for optimal performance
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		assert.Contains(t, clauses[0], "INTERSECT")
		assert.Equal(t, 2, strings.Count(clauses[0], "SELECT MediaDBID FROM MediaTags"))

		assert.Equal(t, "region", args[0])
		assert.Equal(t, "usa", args[1])
		assert.Equal(t, "lang", args[2])
		assert.Equal(t, "en", args[3])
	})

	t.Run("Three AND filters", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
			{Type: "genre", Value: "action", Operator: zapscript.TagOperatorAND},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)
		require.Len(t, args, 6)

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
		require.Len(t, args, 2)

		// Should use NOT EXISTS pattern
		assert.Contains(t, clauses[0], "NOT EXISTS (")
		assert.Contains(t, clauses[0], "MediaTags.MediaDBID = Media.DBID")
		assert.Equal(t, "unfinished", args[0])
		assert.Equal(t, "demo", args[1])
	})

	t.Run("Multiple NOT filters", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
			{Type: "unfinished", Value: "beta", Operator: zapscript.TagOperatorNOT},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 2) // Each NOT filter gets its own clause
		require.Len(t, args, 4)

		// Both should use NOT EXISTS
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
		require.Len(t, args, 2)

		// Should use EXISTS with single condition
		assert.Contains(t, clauses[0], "EXISTS (")
		assert.Contains(t, clauses[0], "MediaTags.MediaDBID = Media.DBID")
		assert.NotContains(t, clauses[0], " OR ") // Single filter doesn't need OR
	})

	t.Run("Multiple OR filters", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "es", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "fr", Operator: zapscript.TagOperatorOR},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1) // All OR filters grouped into one EXISTS
		require.Len(t, args, 6)

		// Should use EXISTS with OR conditions
		assert.Contains(t, clauses[0], "EXISTS (")
		assert.Equal(t, 2, strings.Count(clauses[0], " OR ")) // 3 conditions = 2 OR operators
		assert.Equal(t, 3, strings.Count(clauses[0], "TagTypes.Type = ?"))
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
		require.Len(t, args, 4)

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
		require.Len(t, args, 6)

		// First clause should be INTERSECT
		assert.Contains(t, clauses[0], "Media.DBID IN (")

		// Second clause should be EXISTS with OR
		assert.Contains(t, clauses[1], "EXISTS (")
		assert.Contains(t, clauses[1], " OR ")
	})

	t.Run("NOT + OR", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "es", Operator: zapscript.TagOperatorOR},
		}

		clauses, args := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 2) // One for NOT, one for OR group
		require.Len(t, args, 6)

		// First clause should be NOT EXISTS
		assert.Contains(t, clauses[0], "NOT EXISTS")

		// Second clause should be EXISTS with OR
		assert.Contains(t, clauses[1], "EXISTS (")
		assert.Contains(t, clauses[1], " OR ")
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
		require.Len(t, args, 12)

		// First clause: INTERSECT for AND
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		assert.Contains(t, clauses[0], "INTERSECT")

		// Next two clauses: NOT EXISTS
		assert.Contains(t, clauses[1], "NOT EXISTS")
		assert.Contains(t, clauses[2], "NOT EXISTS")

		// Last clause: EXISTS with OR
		assert.Contains(t, clauses[3], "EXISTS (")
		assert.Contains(t, clauses[3], " OR ")
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
		require.Len(t, args, 8)

		// Verify argument order: AND filters first, then NOT, then OR
		assert.Equal(t, "region", args[0])
		assert.Equal(t, "usa", args[1])
		assert.Equal(t, "lang", args[2])
		assert.Equal(t, "en", args[3])
		assert.Equal(t, "unfinished", args[4])
		assert.Equal(t, "demo", args[5])
		assert.Equal(t, "players", args[6])
		assert.Equal(t, "2", args[7])
	})
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
		assert.Len(t, args, 40) // 20 filters * 2 args each
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
		assert.Len(t, args, 30)
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
		assert.Len(t, args, 20)
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
			expectedArgsMin: 6,
			description:     "Should generate 3 separate clauses",
		},
		{
			name: "Very long tag values",
			filters: []zapscript.TagFilter{
				{Type: "description", Value: string(make([]byte, 500)), Operator: zapscript.TagOperatorAND},
			},
			expectedClauses: 1,
			expectedArgsMin: 2,
			description:     "Should handle very long values",
		},
		{
			name: "Empty string values (after normalization)",
			filters: []zapscript.TagFilter{
				{Type: "tag", Value: "", Operator: zapscript.TagOperatorAND},
			},
			expectedClauses: 1,
			expectedArgsMin: 2,
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
	t.Run("AND filters use INTERSECT pattern", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "region", Value: "usa", Operator: zapscript.TagOperatorAND},
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
		}

		clauses, _ := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)

		// Should contain INTERSECT
		assert.Contains(t, clauses[0], "INTERSECT")
		assert.Contains(t, clauses[0], "Media.DBID IN (")
		assert.Contains(t, clauses[0], "SELECT MediaDBID FROM MediaTags")
	})

	t.Run("NOT filters use NOT EXISTS pattern", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "unfinished", Value: "demo", Operator: zapscript.TagOperatorNOT},
		}

		clauses, _ := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)

		// Should contain NOT EXISTS
		assert.Contains(t, clauses[0], "NOT EXISTS")
		assert.Contains(t, clauses[0], "MediaTags.MediaDBID = Media.DBID")
	})

	t.Run("OR filters use EXISTS with OR pattern", func(t *testing.T) {
		filters := []zapscript.TagFilter{
			{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			{Type: "lang", Value: "es", Operator: zapscript.TagOperatorOR},
		}

		clauses, _ := BuildTagFilterSQL(filters)
		require.Len(t, clauses, 1)

		// Should contain EXISTS with OR
		assert.Contains(t, clauses[0], "EXISTS (")
		assert.Contains(t, clauses[0], " OR ")
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
			name: "Single OR filter should not have OR operator",
			filters: []zapscript.TagFilter{
				{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			},
			description: "Single OR filter doesn't need OR operator",
			validate: func(t *testing.T, clauses []string, _ []any) {
				assert.Len(t, clauses, 1)
				assert.NotContains(t, clauses[0], " OR ")
				assert.Contains(t, clauses[0], "EXISTS (")
			},
		},
		{
			name: "All args must be used in order",
			filters: []zapscript.TagFilter{
				{Type: "a", Value: "1", Operator: zapscript.TagOperatorAND},
				{Type: "b", Value: "2", Operator: zapscript.TagOperatorNOT},
				{Type: "c", Value: "3", Operator: zapscript.TagOperatorOR},
			},
			description: "Arguments must match SQL clause order",
			validate: func(t *testing.T, _ []string, args []any) {
				// Verify all args are present and in correct order
				assert.Equal(t, "a", args[0])
				assert.Equal(t, "1", args[1])
				assert.Equal(t, "b", args[2])
				assert.Equal(t, "2", args[3])
				assert.Equal(t, "c", args[4])
				assert.Equal(t, "3", args[5])
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
