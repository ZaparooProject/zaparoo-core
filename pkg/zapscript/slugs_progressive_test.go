//go:build slugs_integration

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package zapscript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/stretchr/testify/require"
)

func TestProgressiveTrimFallback_PositiveCases(t *testing.T) {
	dbPath := filepath.Join(testDataDir, "media.db")

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skipf("Integration test skipped: %s not found", dbPath)
	}

	ctx := context.Background()
	mockPlatform := setupMockPlatform(t, dbPath)

	db, err := mediadb.OpenMediaDB(ctx, mockPlatform)
	require.NoError(t, err)
	defer db.Close()

	cfg := createTestConfig()

	testCases := []struct {
		name          string
		input         string
		expectedMatch string
	}{
		{
			name:          "Speedball 2 Brutal Deluxe matches Speedball 2",
			input:         "Amiga/Speedball 2 Brutal Deluxe",
			expectedMatch: "Speedball 2",
		},
		{
			name:          "Monkey Island 2 LeChuck's Revenge matches Monkey Island 2",
			input:         "Amiga/Monkey Island 2 LeChuck's Revenge",
			expectedMatch: "Monkey Island 2",
		},
		{
			name:          "Mega Man Battle Network 3 matches Blue Version",
			input:         "GBA/Mega Man Battle Network 3",
			expectedMatch: "Mega Man Battle Network 3 - Blue Version",
		},
		{
			name:          "Speedball 2 Brutal Deluxe exact match on C64",
			input:         "C64/Speedball 2 - Brutal Deluxe",
			expectedMatch: "Speedball 2 - Brutal Deluxe",
		},
		{
			name:          "Zak McKracken and the Alien Mindbenders matches Zak McKracken",
			input:         "Amiga/Zak McKracken and the Alien Mindbenders",
			expectedMatch: "Zak McKracken",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			captured, slug, err := testSlugResolutionFlow(ctx, db, cfg, mockPlatform, tc.input)
			require.NoError(t, err, "Expected to find match for %s (slug: %s)", tc.input, slug)
			require.NotNil(t, captured)
			require.NotEmpty(t, captured.Path)
			require.Contains(t, captured.Path, tc.expectedMatch,
				"Expected path to contain '%s', got: %s", tc.expectedMatch, captured.Path)
			t.Logf("✓ Matched: %s → %s", tc.input, captured.Path)
		})
	}
}

func TestProgressiveTrimFallback_NegativeCases(t *testing.T) {
	dbPath := filepath.Join(testDataDir, "media.db")

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skipf("Integration test skipped: %s not found", dbPath)
	}

	ctx := context.Background()
	mockPlatform := setupMockPlatform(t, dbPath)

	db, err := mediadb.OpenMediaDB(ctx, mockPlatform)
	require.NoError(t, err)
	defer db.Close()

	cfg := createTestConfig()

	testCases := []struct {
		name          string
		input         string
		shouldNotFind string
	}{
		{
			name:          "Generic 'Mario' should not match Mega Man",
			input:         "GBA/Mario",
			shouldNotFind: "Mega Man",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			captured, slug, err := testSlugResolutionFlow(ctx, db, cfg, mockPlatform, tc.input)
			if err != nil {
				t.Logf("✓ Correctly did not match: %s (slug: %s)", tc.input, slug)
				return
			}

			if captured != nil && captured.Path != "" {
				require.NotContains(t, captured.Path, tc.shouldNotFind,
					"Should not have matched '%s', but got: %s", tc.shouldNotFind, captured.Path)
				t.Logf("✓ Matched different game (not %s): %s", tc.shouldNotFind, captured.Path)
			}
		})
	}
}
