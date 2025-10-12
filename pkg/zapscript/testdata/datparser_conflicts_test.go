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

package testdata

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
)

type DATEntry struct {
	DATFile     string
	SystemID    string
	OriginalName string
	Slug        string
	Key         string // "SystemID/slug"
}

func TestSlugConflicts_AllDATs(t *testing.T) {
	datsDir := filepath.Join("dats")

	if _, err := os.Stat(datsDir); os.IsNotExist(err) {
		t.Skipf("DATs directory not found: %s", datsDir)
	}

	// Index all DAT entries
	indexMap := make(map[string][]DATEntry)
	totalEntries := 0
	skippedDATs := 0
	processedDATs := 0

	err := filepath.Walk(datsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(path, ".dat") {
			return nil
		}

		// Parse DAT file
		dat, err := ParseDATFile(path)
		if err != nil {
			t.Logf("Skipping %s: %v", filepath.Base(path), err)
			skippedDATs++
			return nil
		}

		// Match system ID
		systemID, err := MatchSystemID(dat.Header.Name)
		if err != nil {
			t.Logf("No system match for %s (%s)", filepath.Base(path), dat.Header.Name)
			skippedDATs++
			return nil
		}

		processedDATs++

		// Extract and index all games
		for _, game := range dat.Games {
			originalName := game.Name
			if originalName == "" {
				originalName = game.Description
			}

			// Strip metadata and slugify
			cleanName := slugs.StripMetadataBrackets(originalName)
			cleanName = strings.TrimSpace(cleanName)

			if cleanName == "" {
				continue
			}

			slug := slugs.SlugifyString(cleanName)
			key := fmt.Sprintf("%s/%s", systemID, slug)

			entry := DATEntry{
				DATFile:     filepath.Base(path),
				SystemID:    systemID,
				OriginalName: originalName,
				Slug:        slug,
				Key:         key,
			}

			indexMap[key] = append(indexMap[key], entry)
			totalEntries++
		}

		return nil
	})

	if err != nil {
		t.Fatalf("Error walking DAT files: %v", err)
	}

	// Find conflicts
	conflicts := make(map[string][]DATEntry)
	for key, entries := range indexMap {
		if len(entries) > 1 {
			conflicts[key] = entries
		}
	}

	// Generate report
	t.Logf("\n=== SLUG CONFLICT REPORT ===\n")
	t.Logf("Total DAT files processed: %d", processedDATs)
	t.Logf("Total DAT files skipped: %d", skippedDATs)
	t.Logf("Total entries indexed: %d", totalEntries)
	t.Logf("Total unique keys: %d", len(indexMap))
	t.Logf("Total conflicts: %d\n", len(conflicts))

	if len(conflicts) == 0 {
		t.Log("✅ No slug conflicts found!")
		return
	}

	// Sort conflicts by key for consistent output
	conflictKeys := make([]string, 0, len(conflicts))
	for key := range conflicts {
		conflictKeys = append(conflictKeys, key)
	}
	sort.Strings(conflictKeys)

	// Report conflicts
	t.Logf("\n=== CONFLICT DETAILS ===\n")
	for i, key := range conflictKeys {
		entries := conflicts[key]
		t.Logf("\nConflict #%d: %s (%d entries)", i+1, key, len(entries))

		for j, entry := range entries {
			t.Logf("  [%d] Original: %q", j+1, entry.OriginalName)
			t.Logf("      DAT: %s", entry.DATFile)
		}
	}

	// Summary statistics
	t.Logf("\n=== CONFLICT STATISTICS ===\n")

	// Group conflicts by system
	systemConflicts := make(map[string]int)
	for key := range conflicts {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 {
			systemConflicts[parts[0]]++
		}
	}

	// Sort systems by conflict count
	type systemCount struct {
		system string
		count  int
	}
	systemCounts := make([]systemCount, 0, len(systemConflicts))
	for system, count := range systemConflicts {
		systemCounts = append(systemCounts, systemCount{system, count})
	}
	sort.Slice(systemCounts, func(i, j int) bool {
		return systemCounts[i].count > systemCounts[j].count
	})

	t.Logf("Conflicts by system:")
	for _, sc := range systemCounts {
		t.Logf("  %s: %d", sc.system, sc.count)
	}

	// Find worst offenders (most duplicates for a single key)
	maxDupes := 0
	worstKey := ""
	for key, entries := range conflicts {
		if len(entries) > maxDupes {
			maxDupes = len(entries)
			worstKey = key
		}
	}

	if worstKey != "" {
		t.Logf("\nWorst conflict: %s (%d duplicates)", worstKey, maxDupes)
		t.Logf("Entries:")
		for _, entry := range conflicts[worstKey] {
			t.Logf("  - %q from %s", entry.OriginalName, entry.DATFile)
		}
	}

	// Calculate conflict rate
	conflictRate := float64(len(conflicts)) / float64(len(indexMap)) * 100
	t.Logf("\nConflict rate: %.2f%% of unique keys", conflictRate)
}
