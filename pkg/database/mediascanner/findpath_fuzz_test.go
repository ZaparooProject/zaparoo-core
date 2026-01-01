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

package mediascanner

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

// FuzzFindPath uses Go's native fuzzing to test stability and invariants.
// It creates a complex directory structure and attempts to resolve paths against it.
//
// Run with: go test -fuzz=FuzzFindPath -fuzztime=30s
//
// Invariants verified:
//  1. No panics (implicit - test framework catches these)
//  2. If success, result must exist on filesystem
//  3. If success, result must be absolute
//  4. Idempotency: FindPath(FindPath(x)) == FindPath(x)
func FuzzFindPath(f *testing.F) {
	// Seed corpus with interesting path patterns
	f.Add("NormalDir/NormalFile.txt")
	f.Add("Spaces Dir/Spaces File.txt")
	f.Add("UpperDir/UPPERFILE.TXT")
	f.Add("Mixed Case/Mixed.Txt")
	f.Add("Unicode_🚀/File_🎮.txt")
	f.Add("..")
	f.Add(".")
	f.Add("///")
	f.Add("....//.//..//")

	f.Fuzz(func(t *testing.T, pathFragment string) {
		// Limit path length and ensure valid UTF-8 to prevent OS errors
		// from dominating fuzz results
		if len(pathFragment) > 260 || !utf8.ValidString(pathFragment) {
			return
		}

		// Setup test environment for this iteration
		tmpDir := t.TempDir()
		setupFuzzEnvironment(t, tmpDir)

		// Construct full input path
		fullPath := filepath.Join(tmpDir, pathFragment)

		// EXECUTE: Call FindPath
		result, err := FindPath(fullPath)

		// INVARIANT CHECKS
		if err == nil {
			// INVARIANT 2: If success, result MUST exist
			if _, statErr := os.Stat(result); statErr != nil {
				t.Errorf("FindPath succeeded but returned non-existent path: %s (error: %v)", result, statErr)
			}

			// INVARIANT 3: Result MUST be absolute
			if !filepath.IsAbs(result) {
				t.Errorf("FindPath returned non-absolute path: %s", result)
			}

			// INVARIANT 4: Idempotency - calling FindPath on result should return same result
			result2, err2 := FindPath(result)
			if err2 != nil {
				t.Errorf("FindPath failed on its own output: %s (error: %v)", result, err2)
			} else if result != result2 {
				t.Errorf("FindPath is not idempotent. First: %s, Second: %s", result, result2)
			}
		}
		// If err != nil, that's acceptable - just means path doesn't exist
		// The important thing is it didn't panic
	})
}

// setupFuzzEnvironment creates a complex directory structure for fuzzing
func setupFuzzEnvironment(t *testing.T, root string) {
	t.Helper()

	dirs := []string{
		"NormalDir",
		"Spaces Dir",
		"UpperDir",
		"Mixed Case",
		"Unicode_🚀",
	}

	files := []string{
		"NormalDir/NormalFile.txt",
		"Spaces Dir/Spaces File.txt",
		"UpperDir/UPPERFILE.TXT",
		"Mixed Case/Mixed.Txt",
		"Unicode_🚀/File_🎮.txt",
	}

	for _, d := range dirs {
		_ = os.Mkdir(filepath.Join(root, d), 0o750)
	}
	for _, f := range files {
		_ = os.WriteFile(filepath.Join(root, f), []byte("content"), 0o600)
	}
}
