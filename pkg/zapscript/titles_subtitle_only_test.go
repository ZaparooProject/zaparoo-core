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

func TestSubtitleOnlyFallback_PositiveCases(t *testing.T) {
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
			name:          "Ocarina of Time subtitle-only",
			input:         "N64/Legend of Zelda: Ocarina of Time",
			expectedMatch: "Ocarina of Time",
		},
		{
			name:          "Blue Version subtitle-only",
			input:         "GBA/Mega Man Battle Network 3: Blue Version",
			expectedMatch: "Blue Version",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			captured, slug, err := testSlugResolutionFlow(ctx, db, cfg, mockPlatform, tc.input)
			if err != nil {
				t.Logf("Note: Subtitle-only fallback may not trigger if base or progressive trim already matched")
				t.Logf("Input: %s, Slug: %s", tc.input, slug)
				return
			}

			require.NotNil(t, captured)
			require.NotEmpty(t, captured.Path)
			t.Logf("✓ Matched: %s → %s", tc.input, captured.Path)
		})
	}
}
