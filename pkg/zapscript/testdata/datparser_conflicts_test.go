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
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
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

			if strings.TrimSpace(originalName) == "" {
				continue
			}

			// SlugifyString handles all normalization internally including metadata stripping
			slug := slugs.SlugifyString(originalName)
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

	// Advanced analysis
	reportFalseCollisions(t, conflicts)
	reportIncompleteStripping(t, conflicts, indexMap)
}

// lightNormalizeToWords performs a minimal normalization for analysis.
// It lowercases and extracts alphanumeric words, but crucially, it does NOT
// strip brackets or other metadata that SlugifyString removes. This allows
// us to see the words that were lost during slugification.
func lightNormalizeToWords(s string) []string {
	// A simple regex to find sequences of letters and numbers.
	// This is more reliable than replacing non-alphanum with spaces.
	re := regexp.MustCompile(`[a-z0-9]+`)
	return re.FindAllString(strings.ToLower(s), -1)
}

// TitleParts represents the parsed components of a game title.
// BaseWords are from the main title, MetadataWords are from bracketed content.
type TitleParts struct {
	BaseWords     []string
	MetadataWords []string
}

// metadataRegex extracts content within () or [] brackets
var metadataRegex = regexp.MustCompile(`\s*(\([^)]*\)|\[[^\]]*\])`)

// extractTitleParts separates words in the base title from words in metadata brackets.
// This allows us to distinguish between title words and metadata, solving the ambiguity
// where "USA" could be part of a title or a region tag.
func extractTitleParts(s string) TitleParts {
	// Extract all bracketed metadata content first
	metadataMatches := metadataRegex.FindAllString(s, -1)

	// The base title is what's left after removing the metadata
	baseTitle := metadataRegex.ReplaceAllString(s, "")
	baseTitle = strings.TrimSpace(baseTitle)

	// Tokenize the base title and metadata content separately
	baseWords := lightNormalizeToWords(baseTitle)

	var metadataWords []string
	for _, match := range metadataMatches {
		words := lightNormalizeToWords(match)
		metadataWords = append(metadataWords, words...)
	}

	return TitleParts{
		BaseWords:     baseWords,
		MetadataWords: metadataWords,
	}
}

// sliceToSet converts a slice of strings to a set (map) for efficient lookups
func sliceToSet(s []string) map[string]struct{} {
	set := make(map[string]struct{}, len(s))
	for _, item := range s {
		set[item] = struct{}{}
	}
	return set
}

// areSetsEqual checks if two sets contain exactly the same elements
func areSetsEqual(setA, setB map[string]struct{}) bool {
	if len(setA) != len(setB) {
		return false
	}
	for k := range setA {
		if _, ok := setB[k]; !ok {
			return false
		}
	}
	return true
}

// symmetricDifference returns elements that are in either setA or setB but not in both
func symmetricDifference(setA, setB map[string]struct{}) map[string]struct{} {
	diff := make(map[string]struct{})
	for k := range setA {
		if _, ok := setB[k]; !ok {
			diff[k] = struct{}{}
		}
	}
	for k := range setB {
		if _, ok := setA[k]; !ok {
			diff[k] = struct{}{}
		}
	}
	return diff
}

// reportFalseCollisions analyzes conflicts by comparing intermediate slugs.
// Collisions are classified as: Genuine (different titles before version/edition stripping),
// or Expected (same title, version/edition differences correctly normalized).
func reportFalseCollisions(t *testing.T, conflicts map[string][]DATEntry) {
	t.Logf("\n=== COLLISION ANALYSIS (Comparing Intermediate Slugs) ===\n")

	// Collision categories
	type collision struct {
		key              string
		systemID         string
		titleA           string
		titleB           string
		intermediateSlugA string
		intermediateSlugB string
	}

	var genuineCollisions []collision      // Different intermediate slugs -> TRUE BUG
	var expectedCollisions int             // Same intermediate slug -> version/edition stripping working

	for key, entries := range conflicts {
		// Extract system ID from key
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		systemID := parts[0]

		// Compare every pair in the conflict group
		for i := 0; i < len(entries); i++ {
			for j := i + 1; j < len(entries); j++ {
				// Generate "intermediate slugs" - these show what the slug would be
				// AFTER bracket removal (Stage 7) but BEFORE edition/version stripping (Stage 8).
				// This lets us see if version/edition stripping is working correctly.
				intermediateA := generateIntermediateSlug(entries[i].OriginalName)
				intermediateB := generateIntermediateSlug(entries[j].OriginalName)

				if intermediateA != intermediateB {
					// GENUINE COLLISION: Different titles (after bracket removal)
					// are mapping to the same final slug. This is a bug!
					genuineCollisions = append(genuineCollisions, collision{
						key:              key,
						systemID:         systemID,
						titleA:           entries[i].OriginalName,
						titleB:           entries[j].OriginalName,
						intermediateSlugA: intermediateA,
						intermediateSlugB: intermediateB,
					})
					goto nextConflict
				} else {
					// EXPECTED COLLISION: Same intermediate slug means these titles
					// differ only in version/edition markers (which are correctly stripped).
					expectedCollisions++
					goto nextConflict
				}
			}
		}
	nextConflict:
	}

	// Report results
	t.Logf("📊 Collision Summary:")
	t.Logf("  🚨 Genuine Collisions: %d (different titles → same final slug = BUG)", len(genuineCollisions))
	t.Logf("  ✅ Expected Collisions: %d (same title, version/edition differences = WORKING CORRECTLY)\n", expectedCollisions)

	// Report genuine collisions (HIGH PRIORITY)
	if len(genuineCollisions) > 0 {
		t.Logf("\n=== 🚨 GENUINE COLLISIONS (BUGS TO FIX) ===\n")
		t.Logf("These titles produce different intermediate slugs but the same final slug.\n")
		t.Logf("This indicates a bug in the normalization pipeline.\n")
		limit := 100
		if len(genuineCollisions) < limit {
			limit = len(genuineCollisions)
		}
		for i := 0; i < limit; i++ {
			c := genuineCollisions[i]
			t.Logf("\nCollision #%d in key %q:", i+1, c.key)
			t.Logf("  Title A: %q", c.titleA)
			t.Logf("  Title B: %q", c.titleB)
			t.Logf("  Intermediate Slug A: %q", c.intermediateSlugA)
			t.Logf("  Intermediate Slug B: %q", c.intermediateSlugB)
			t.Logf("  Final Slug (same): %q", strings.SplitN(c.key, "/", 2)[1])
		}
		if len(genuineCollisions) > limit {
			t.Logf("\n... and %d more genuine collisions (limit %d shown)\n", len(genuineCollisions)-limit, limit)
		}
	} else {
		t.Logf("\n✅ No genuine collisions found! All conflicts are expected (version/edition variants).\n")
	}
}

// generateIntermediateSlug creates a normalized version of the title with brackets removed
// but WITHOUT edition/version stripping. This simulates what the title looks like after
// Stage 7 but before Stage 8, allowing us to see if two titles differ only in version/edition markers.
func generateIntermediateSlug(title string) string {
	// Remove brackets first
	withoutBrackets := slugs.StripMetadataBrackets(title)
	s := strings.TrimSpace(withoutBrackets)
	if s == "" {
		return ""
	}

	// Now normalize but DON'T strip edition/version suffixes
	// We'll do basic normalization: lowercase, remove non-alphanum
	// This is a simplified version of slugification without Stage 8

	// Basic text cleanup (similar to slug pipeline but simplified)
	s = strings.ToLower(s)

	// Remove common separators
	s = strings.ReplaceAll(s, " - ", " ")
	s = strings.ReplaceAll(s, ":", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")

	// Convert to words and rejoin (removes extra spaces)
	words := strings.Fields(s)
	s = strings.Join(words, "")

	// Remove non-alphanumeric
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "")

	return s
}

// isTrivialMetadata checks if a set of difference tokens contains ONLY trivial metadata.
// This function can be very aggressive because it only analyzes words from within brackets.
// Returns true if all tokens are trivial (safe to ignore), false if any meaningful token exists.
func isTrivialMetadata(diff map[string]struct{}) bool {
	// Aggressive filter list - can safely include anything that appears in brackets
	// as metadata rather than being part of the actual game title
	trivialTokens := map[string]bool{
		// Regions (most common metadata)
		"usa": true, "eur": true, "europe": true, "jpn": true, "japan": true, "world": true,
		"aus": true, "australia": true, "uk": true, "fra": true, "france": true, "ger": true, "germany": true,
		"spa": true, "spain": true, "ita": true, "italy": true, "kor": true, "korea": true,
		"chn": true, "china": true, "asia": true, "brasil": true, "brazil": true,
		// Video standards
		"ntsc": true, "pal": true, "secam": true,
		// Version markers
		"v": true, "rev": true, "revision": true, "v0": true, "v1": true, "v2": true, "v3": true, "v4": true, "v5": true,
		"v6": true, "v7": true, "v8": true, "v9": true, "v10": true,
		// Numeric versions (common in multi-disc/update releases)
		"00": true, "01": true, "02": true, "03": true, "04": true, "05": true, "06": true, "07": true, "08": true, "09": true,
		"10": true, "11": true, "12": true, "13": true, "14": true, "15": true, "16": true, "17": true, "18": true, "19": true,
		"20": true, "21": true, "22": true, "23": true, "24": true, "25": true, "26": true, "27": true, "28": true, "29": true,
		"30": true,
		// Dump flags (single letters and common combinations)
		"a": true, "b": true, "f": true, "h": true, "m": true, "o": true, "p": true, "t": true, "x": true, "u": true,
		"a2": true, "a3": true, "b2": true, "b3": true, "h2": true, "h3": true, "f1": true, "f2": true,
		// Crack groups and release groups
		"cr": true, "xor": true, "nps": true, "lxt": true, "blz": true, "ass": true, "aod": true,
		"hf": true, "ex": true, "mc": true, "tb": true, "efx": true, "tpu": true, "mindrape": true,
		"ffe": true, "sekret": true, "gamesx": true, "swj": true, "tiboh": true, "amonik": true,
		"brokimsoft": true, "slider": true, "cdac": true, "gpa": true, "spe": true, "cach": true,
		"coconuts": true, "phantom": true,
		// Common metadata keywords
		"alt": true, "proto": true, "prototype": true, "beta": true, "demo": true,
		"unl": true, "unlicensed": true, "licensed": true, "sample": true, "promo": true,
		"update": true, "patch": true, "dlc": true, "theme": true, "trial": true, "remaster": true,
		"merged": true, "shortcut": true, "enhanced": true,
		// Format/Technical
		"ines": true, "rom": true, "mapper": true, "enlarged": true, "vimm": true, "bits": true,
		"swapped": true, "gfx": true,
		// Dump quality
		"dump": true, "baddump": true, "corrupt": true, "bamcopy": true, "doscopy": true,
		"rebuilt": true, "errdms": true, "fixed": true, "bug": true,
		// Memory/Hardware specs
		"eeprom": true, "sram": true, "bootblock": true, "ffs": true, "16k": true, "32k": true,
		"48k": true, "64k": true, "128k": true, "256k": true, "512k": true, "1mb": true,
		"ks2": true, "cdrm": true, "r1c": true, "r1d": true, "r1j": true, "r1m": true,
		"ccj001": true, "ccj002": true, "ste": true, "pentag on": true,
		// Disc/Tape/Side markers
		"disc": true, "disk": true, "tape": true, "side": true, "cd": true,
		// Emulation tools
		"tzxtools": true,
		// Language codes
		"en": true, "es": true, "fr": true, "de": true, "it": true, "pt": true, "nl": true,
		"ja": true, "zh": true, "ko": true, "ru": true, "pl": true, "cs": true, "sv": true,
		"da": true, "no": true, "fi": true, "tr": true, "ar": true, "he": true,
		// Budget/Edition markers
		"budget": true, "kixx": true, "platinum": true, "classics": true, "essentials": true,
		// Platform distribution
		"psn": true, "wii": true, "eshop": true, "xbla": true, "gog": true, "steam": true,
		// Single digits
		"0": true, "1": true, "2": true, "3": true, "4": true, "5": true, "6": true, "7": true, "8": true, "9": true,
		// Common filler words
		"the": true, "of": true, "and": true, "with": true,
	}

	// Check if ALL tokens are trivial
	for token := range diff {
		if _, isTrivial := trivialTokens[token]; !isTrivial {
			// Found a non-trivial token, so this difference is meaningful
			return false
		}
	}

	// All tokens are trivial
	return true
}

// setDifference returns elements in setA that are not in setB
func setDifference(setA, setB []string) []string {
	mapB := make(map[string]struct{}, len(setB))
	for _, item := range setB {
		mapB[item] = struct{}{}
	}

	var diff []string
	for _, item := range setA {
		if _, ok := mapB[item]; !ok {
			diff = append(diff, item)
		}
	}
	return diff
}

// trackTags adds tags to the system's tag count map
func trackTags(systemTags map[string]map[string]int, systemID string, tags []string) {
	if systemTags[systemID] == nil {
		systemTags[systemID] = make(map[string]int)
	}
	for _, tag := range tags {
		systemTags[systemID][tag]++
	}
}

// reportTagStatistics shows the most common differentiating tags per system
func reportTagStatistics(t *testing.T, systemTags map[string]map[string]int) {
	t.Logf("\n=== TAG STATISTICS (Differentiating Tokens) ===\n")

	// Sort systems alphabetically
	systems := make([]string, 0, len(systemTags))
	for system := range systemTags {
		systems = append(systems, system)
	}
	sort.Strings(systems)

	for _, system := range systems {
		tags := systemTags[system]

		// Convert to sorted slice
		type tagCount struct {
			tag   string
			count int
		}
		tagCounts := make([]tagCount, 0, len(tags))
		for tag, count := range tags {
			tagCounts = append(tagCounts, tagCount{tag, count})
		}

		// Sort by count descending
		sort.Slice(tagCounts, func(i, j int) bool {
			return tagCounts[i].count > tagCounts[j].count
		})

		// Show top 30 tags for this system
		t.Logf("\n%s - Top differentiating tags:", system)
		limit := 30
		if len(tagCounts) < limit {
			limit = len(tagCounts)
		}
		for i := 0; i < limit; i++ {
			t.Logf("  %-20s: %d", tagCounts[i].tag, tagCounts[i].count)
		}
	}
}

// reportIncompleteStripping analyzes slugs to find cases where metadata
// was likely not stripped correctly, e.g., "gametitleusa" instead of "gametitle".
func reportIncompleteStripping(t *testing.T, conflicts map[string][]DATEntry, indexMap map[string][]DATEntry) {
	t.Logf("\n=== ANALYSIS: Potential Incomplete Metadata Stripping ===\n")

	// A dictionary of common metadata tokens found in filenames.
	// Must be lowercase and ordered from longest to shortest to handle overlaps (e.g., "prototype" before "proto").
	// Expanded to include edition/collection markers that should be stripped as metadata.
	metadataTokens := []string{
		// Regions (longest first)
		"prototype", "unlicensed", "europe", "japan", "korea", "china",
		// Edition markers (longest first)
		"anniversary", "championship", "tournament", "collectors", "collection",
		"compilation", "anthology", "definitive", "remastered", "ultimate",
		"directors", "extended", "complete", "trilogy", "special", "deluxe",
		"limited", "enhanced", "updated", "edition", "remix", "remaster",
		// Other metadata
		"demo", "beta", "proto", "rev", "revision",
		"usa", "eur", "jpn", "aus", "pal", "ntsc", "unl",
		"goty", "full",
	}

	found := 0
	reportedSlugs := make(map[string]bool)

	for key := range conflicts {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		systemID, slug := parts[0], parts[1]

		if reportedSlugs[slug] {
			continue
		}

		for _, token := range metadataTokens {
			if strings.HasSuffix(slug, token) {
				baseSlug := strings.TrimSuffix(slug, token)
				if baseSlug == "" {
					continue
				}

				// High-confidence check: does the base slug also exist for this system?
				baseKey := fmt.Sprintf("%s/%s", systemID, baseSlug)
				if _, ok := indexMap[baseKey]; ok {
					found++
					t.Logf("Potential Leaked Metadata in key %q:", key)
					t.Logf("  - Detected token: %q", token)
					t.Logf("  - Base key %q also exists.", baseKey)
					reportedSlugs[slug] = true
					break // Move to the next slug
				}
			}
		}
	}

	if found == 0 {
		t.Log("✅ No obvious cases of incomplete metadata stripping found.")
	} else {
		t.Logf("⚠️  Found %d potential cases of incomplete metadata stripping.\n", found)
	}
}

// isProperSubset checks if setA is a proper subset of setB.
// It uses slices of strings as sets for simplicity.
func isProperSubset(setA, setB []string) bool {
	if len(setA) >= len(setB) {
		return false
	}
	mapB := make(map[string]struct{}, len(setB))
	for _, item := range setB {
		mapB[item] = struct{}{}
	}
	for _, item := range setA {
		if _, ok := mapB[item]; !ok {
			return false
		}
	}
	return true
}

// UnmappedTagInfo tracks information about an unmapped tag
type UnmappedTagInfo struct {
	Tag        string            // The normalized tag value
	Count      int               // Number of times this tag appears
	Systems    map[string]int    // System ID -> count of occurrences in that system
	Examples   []string          // Example filenames containing this tag (max 5)
	DATFiles   map[string]bool   // DAT files where this tag appears
}

// TestUnmappedTags_AllDATs analyzes all DAT files to identify tags that are not mapped
// in the tag_mappings.go file. This helps prioritize which tags to add mappings for.
func TestUnmappedTags_AllDATs(t *testing.T) {
	datsDir := filepath.Join("dats")

	if _, err := os.Stat(datsDir); os.IsNotExist(err) {
		t.Skipf("DATs directory not found: %s", datsDir)
	}

	// Track unmapped tags
	unmappedTags := make(map[string]*UnmappedTagInfo)
	systemUnmappedCounts := make(map[string]map[string]bool) // systemID -> set of unmapped tags

	// Statistics
	totalGames := 0
	totalTagsExtracted := 0
	totalMappedTags := 0
	totalUnmappedTags := 0
	processedDATs := 0
	skippedDATs := 0

	// Walk through all DAT files
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
		datFileName := filepath.Base(path)

		// Initialize system tracking if needed
		if systemUnmappedCounts[systemID] == nil {
			systemUnmappedCounts[systemID] = make(map[string]bool)
		}

		// Process each game in the DAT
		for _, game := range dat.Games {
			gameName := game.Name
			if gameName == "" {
				gameName = game.Description
			}

			if strings.TrimSpace(gameName) == "" {
				continue
			}

			totalGames++

			// Parse tags from the filename using the canonical tag parser
			canonicalTags := tags.ParseFilenameToCanonicalTags(gameName)
			totalTagsExtracted += len(canonicalTags)

			// Analyze each tag
			for _, canonicalTag := range canonicalTags {
				if canonicalTag.Type == tags.TagTypeUnknown {
					// This is an unmapped tag
					totalUnmappedTags++
					tagValue := string(canonicalTag.Value)

					// Initialize tracking if this is a new unmapped tag
					if unmappedTags[tagValue] == nil {
						unmappedTags[tagValue] = &UnmappedTagInfo{
							Tag:      tagValue,
							Count:    0,
							Systems:  make(map[string]int),
							Examples: make([]string, 0, 5),
							DATFiles: make(map[string]bool),
						}
					}

					info := unmappedTags[tagValue]
					info.Count++
					info.Systems[systemID]++
					info.DATFiles[datFileName] = true
					systemUnmappedCounts[systemID][tagValue] = true

					// Store example filename (max 5)
					if len(info.Examples) < 5 {
						info.Examples = append(info.Examples, gameName)
					}
				} else {
					totalMappedTags++
				}
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("Error walking DAT files: %v", err)
	}

	// Generate report
	t.Logf("\n=== UNMAPPED TAG ANALYSIS ===\n")
	t.Logf("Total DAT files processed: %d", processedDATs)
	t.Logf("Total DAT files skipped: %d", skippedDATs)
	t.Logf("Total game entries analyzed: %d", totalGames)
	t.Logf("Total tags extracted: %d", totalTagsExtracted)
	t.Logf("Mapped tags: %d (%.1f%%)", totalMappedTags,
		float64(totalMappedTags)/float64(totalTagsExtracted)*100)
	t.Logf("Unmapped tags: %d (%.1f%%)\n", totalUnmappedTags,
		float64(totalUnmappedTags)/float64(totalTagsExtracted)*100)

	if len(unmappedTags) == 0 {
		t.Log("✅ No unmapped tags found! All tags are mapped.")
		return
	}

	// Sort unmapped tags by frequency
	type tagFreq struct {
		tag  string
		info *UnmappedTagInfo
	}
	sortedTags := make([]tagFreq, 0, len(unmappedTags))
	for tag, info := range unmappedTags {
		sortedTags = append(sortedTags, tagFreq{tag, info})
	}
	sort.Slice(sortedTags, func(i, j int) bool {
		return sortedTags[i].info.Count > sortedTags[j].info.Count
	})

	// Report top N unmapped tags
	topN := 50
	if len(sortedTags) < topN {
		topN = len(sortedTags)
	}

	t.Logf("\n=== TOP %d UNMAPPED TAGS ===\n", topN)
	for i := 0; i < topN; i++ {
		tf := sortedTags[i]
		info := tf.info

		t.Logf("\n#%d: %q (%d occurrences)", i+1, info.Tag, info.Count)

		// Show top systems for this tag
		type systemFreq struct {
			system string
			count  int
		}
		systemFreqs := make([]systemFreq, 0, len(info.Systems))
		for sys, count := range info.Systems {
			systemFreqs = append(systemFreqs, systemFreq{sys, count})
		}
		sort.Slice(systemFreqs, func(i, j int) bool {
			return systemFreqs[i].count > systemFreqs[j].count
		})

		// Show top 5 systems
		systemStrs := make([]string, 0, 5)
		for j := 0; j < len(systemFreqs) && j < 5; j++ {
			systemStrs = append(systemStrs, fmt.Sprintf("%s (%d)", systemFreqs[j].system, systemFreqs[j].count))
		}
		t.Logf("  Systems: %s", strings.Join(systemStrs, ", "))

		// Show example filenames
		t.Logf("  Examples:")
		for _, example := range info.Examples {
			t.Logf("    - %q", example)
		}
	}

	// Report unmapped tags by system
	t.Logf("\n=== UNMAPPED TAGS BY SYSTEM ===\n")

	type systemStat struct {
		systemID    string
		uniqueTags  int
	}
	systemStats := make([]systemStat, 0, len(systemUnmappedCounts))
	for systemID, tagSet := range systemUnmappedCounts {
		systemStats = append(systemStats, systemStat{systemID, len(tagSet)})
	}
	sort.Slice(systemStats, func(i, j int) bool {
		return systemStats[i].uniqueTags > systemStats[j].uniqueTags
	})

	for _, stat := range systemStats {
		t.Logf("%s: %d unique unmapped tags", stat.systemID, stat.uniqueTags)
	}

	// Summary message
	t.Logf("\n=== SUMMARY ===")
	t.Logf("Found %d unique unmapped tags across all systems.", len(unmappedTags))
	t.Logf("Consider adding mappings for the most common tags to improve tag coverage.")
}
