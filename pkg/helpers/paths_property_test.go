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

package helpers

import (
	"runtime"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// ============================================================================
// NormalizePathForComparison Property Tests
// ============================================================================

// TestPropertyNormalizePathIdempotent verifies normalizing twice gives same result.
func TestPropertyNormalizePathIdempotent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate path-like strings
		path := rapid.StringMatching(`[a-zA-Z0-9_\-./\\]{0,50}`).Draw(t, "path")

		once := NormalizePathForComparison(path)
		twice := NormalizePathForComparison(once)

		if once != twice {
			t.Fatalf("Not idempotent: first=%q, second=%q", once, twice)
		}
	})
}

// TestPropertyNormalizePathDeterministic verifies same input always gives same output.
func TestPropertyNormalizePathDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		path := rapid.StringMatching(`[a-zA-Z0-9_\-./\\]{0,50}`).Draw(t, "path")

		result1 := NormalizePathForComparison(path)
		result2 := NormalizePathForComparison(path)

		if result1 != result2 {
			t.Fatalf("Non-deterministic: %q vs %q for input %q", result1, result2, path)
		}
	})
}

// TestPropertyNormalizePathLowercase verifies result is always lowercase.
func TestPropertyNormalizePathLowercase(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		path := rapid.StringMatching(`[a-zA-Z0-9_\-./\\]{0,50}`).Draw(t, "path")

		result := NormalizePathForComparison(path)

		if result != strings.ToLower(result) {
			t.Fatalf("Result not lowercase: %q from input %q", result, path)
		}
	})
}

// TestPropertyNormalizePathNoBackslashesWindows verifies result has no backslashes on Windows.
// On Unix, backslashes are valid filename characters and preserved.
func TestPropertyNormalizePathNoBackslashesWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Backslash conversion only applies on Windows")
	}
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		path := rapid.StringMatching(`[a-zA-Z0-9_\-./\\]{0,50}`).Draw(t, "path")

		result := NormalizePathForComparison(path)

		if strings.Contains(result, "\\") {
			t.Fatalf("Result contains backslash: %q from input %q", result, path)
		}
	})
}

// ============================================================================
// PathHasPrefix Property Tests
// ============================================================================

// TestPropertyPathHasPrefixReflexive verifies path is always within itself.
func TestPropertyPathHasPrefixReflexive(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		path := rapid.StringMatching(`[a-zA-Z0-9_\-./]{1,50}`).Draw(t, "path")

		if !PathHasPrefix(path, path) {
			t.Fatalf("PathHasPrefix(%q, %q) should be true (reflexive)", path, path)
		}
	})
}

// TestPropertyPathHasPrefixChildInParent verifies child path is in parent.
func TestPropertyPathHasPrefixChildInParent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		parent := "/" + rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "parent")
		child := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "child")
		fullPath := parent + "/" + child

		if !PathHasPrefix(fullPath, parent) {
			t.Fatalf("Child %q should be in parent %q", fullPath, parent)
		}
	})
}

// TestPropertyPathHasPrefixEmptyRootRejectsPaths verifies empty root rejects non-empty paths.
func TestPropertyPathHasPrefixEmptyRootRejectsPaths(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		path := "/" + rapid.StringMatching(`[a-z]{1,20}`).Draw(t, "path")

		if PathHasPrefix(path, "") {
			t.Fatalf("Empty root should reject non-empty path %q", path)
		}
	})
}

// TestPropertyPathHasPrefixSiblingsDontMatch verifies sibling dirs don't match.
func TestPropertyPathHasPrefixSiblingsDontMatch(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		base := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "base")
		suffix1 := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "suffix1")
		suffix2 := rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "suffix2")

		// Skip if suffixes are the same
		if suffix1 == suffix2 {
			return
		}

		dir1 := "/" + base + suffix1
		dir2 := "/" + base + suffix2
		file := dir2 + "/file.txt"

		// dir2/file should NOT be in dir1 (they're siblings with shared prefix)
		if PathHasPrefix(file, dir1) {
			t.Fatalf("Sibling match bug: %q should not be in %q", file, dir1)
		}
	})
}

// ============================================================================
// GetPathInfo Property Tests
// ============================================================================

// TestPropertyGetPathInfoDeterministic verifies same input gives same output.
func TestPropertyGetPathInfoDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		path := rapid.StringMatching(`[a-zA-Z0-9_\-./]{0,50}`).Draw(t, "path")

		info1 := GetPathInfo(path)
		info2 := GetPathInfo(path)

		if info1.Path != info2.Path || info1.Filename != info2.Filename ||
			info1.Extension != info2.Extension || info1.Name != info2.Name {
			t.Fatalf("Non-deterministic GetPathInfo for %q", path)
		}
	})
}

// TestPropertyGetPathInfoNamePlusExtEqualsFilename verifies name + ext = filename.
func TestPropertyGetPathInfoNamePlusExtEqualsFilename(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate simple file paths
		dir := "/" + rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "dir")
		name := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "name")
		ext := "." + rapid.StringMatching(`[a-z]{1,4}`).Draw(t, "ext")
		path := dir + "/" + name + ext

		info := GetPathInfo(path)

		reconstructed := info.Name + info.Extension
		if reconstructed != info.Filename {
			t.Fatalf("Name+Extension != Filename: %q + %q != %q (path=%q)",
				info.Name, info.Extension, info.Filename, path)
		}
	})
}

// TestPropertyGetPathInfoExtensionStartsWithDot verifies extension starts with dot.
func TestPropertyGetPathInfoExtensionStartsWithDot(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate paths with explicit extensions
		name := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "name")
		ext := rapid.StringMatching(`[a-z]{1,4}`).Draw(t, "ext")
		path := "/" + name + "." + ext

		info := GetPathInfo(path)

		if info.Extension != "" && info.Extension[0] != '.' {
			t.Fatalf("Extension should start with dot: %q (path=%q)", info.Extension, path)
		}
	})
}
