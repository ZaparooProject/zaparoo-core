//go:build slugs_integration

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

func TestPrefixFallback_EditionOverSequel(t *testing.T) {
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
		name           string
		input          string
		preferredMatch string
		avoidMatch     string
	}{
		{
			name:           "Alien Breed prefers SE over 2",
			input:          "Amiga/Alien Breed",
			preferredMatch: "SE",
			avoidMatch:     "Alien Breed 2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			captured, slug, err := testSlugResolutionFlow(ctx, db, cfg, mockPlatform, tc.input)
			require.NoError(t, err, "Expected to find match for %s (slug: %s)", tc.input, slug)
			require.NotNil(t, captured)
			require.NotEmpty(t, captured.Path)

			if tc.preferredMatch != "" {
				require.Contains(t, captured.Path, tc.preferredMatch,
					"Expected path to contain '%s', got: %s", tc.preferredMatch, captured.Path)
			}

			if tc.avoidMatch != "" {
				require.NotContains(t, captured.Path, tc.avoidMatch,
					"Should not match '%s', got: %s", tc.avoidMatch, captured.Path)
			}

			t.Logf("✓ Matched: %s → %s", tc.input, captured.Path)
		})
	}
}
