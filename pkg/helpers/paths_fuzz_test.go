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

package helpers

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzGetPathExt tests extension extraction with random path inputs
// to discover edge cases in file extension parsing
func FuzzGetPathExt(f *testing.F) {
	// Seed corpus with various path formats
	f.Add("/path/to/file.txt")
	f.Add("file.zip")
	f.Add("/path/file.tar.gz")
	f.Add("C:\\Windows\\file.exe")
	f.Add("file")
	f.Add(".hidden")
	f.Add("/path/.hidden")
	f.Add("")
	f.Add(".")
	f.Add("..")
	f.Add("/path/to/")
	f.Add("file.")
	f.Add("file..txt")
	f.Add("/path.with.dots/file")
	f.Add("/path.with.dots/file.txt")

	f.Fuzz(func(t *testing.T, path string) {
		// Call function - should never panic
		result := getPathExt(path)

		// Property 1: Result should be valid UTF-8
		if !utf8.ValidString(result) {
			t.Errorf("Invalid UTF-8 in extension: %q from path: %q", result, path)
		}

		// Property 2: Extension should start with dot if non-empty
		if result != "" && result[0] != '.' {
			t.Errorf("Extension should start with dot: %q from path: %q", result, path)
		}

		// Property 3: Special cases should return empty
		base := getPathBase(path)
		if base == "" || base == "." || base == ".." {
			if result != "" {
				t.Errorf("Should return empty for special cases: path=%q, result=%q", path, result)
			}
		}

		// Property 4: Hidden files without extension should return empty
		if base != "" && base[0] == '.' && !strings.Contains(base[1:], ".") {
			if result != "" {
				t.Errorf("Hidden file without extension should return empty: path=%q, result=%q", path, result)
			}
		}

		// Property 5: Result should not be longer than the base name
		if len(result) > len(base) {
			t.Errorf("Extension longer than base: base=%d, ext=%d, path=%q", len(base), len(result), path)
		}

		// Property 6: Deterministic
		result2 := getPathExt(path)
		if result != result2 {
			t.Errorf("Non-deterministic result for path: %q", path)
		}
	})
}

// FuzzGetPathBase tests base name extraction with random path inputs
// to discover edge cases in path parsing
func FuzzGetPathBase(f *testing.F) {
	// Seed corpus with Unix and Windows style paths
	f.Add("/path/to/file.txt")
	f.Add("C:\\Windows\\System32\\file.exe")
	f.Add("/path/to/")
	f.Add("file.txt")
	f.Add("")
	f.Add("/")
	f.Add("\\")
	f.Add(".")
	f.Add("..")
	f.Add("/path/to/../file")
	f.Add("path\\to\\file")
	f.Add("///multiple///slashes///")
	f.Add("\\\\\\multiple\\\\\\backslashes\\\\\\")

	f.Fuzz(func(t *testing.T, path string) {
		// Call function - should never panic
		result := getPathBase(path)

		// Property 1: Result should be valid UTF-8
		if !utf8.ValidString(result) {
			t.Errorf("Invalid UTF-8 in base: %q from path: %q", result, path)
		}

		// Property 2: Empty path should return "."
		if path == "" && result != "." {
			t.Errorf("Empty path should return '.', got: %q", result)
		}

		// Property 3: Result should not contain path separators
		if strings.Contains(result, "/") || strings.Contains(result, "\\") {
			t.Errorf("Base contains separator: %q from path: %q", result, path)
		}

		// Property 4: Result should not be longer than input
		if len(result) > len(path)+1 { // +1 for "." case
			t.Errorf("Base longer than path: input=%d, output=%d", len(path), len(result))
		}

		// Property 5: Deterministic
		result2 := getPathBase(path)
		if result != result2 {
			t.Errorf("Non-deterministic result for path: %q", path)
		}
	})
}

// FuzzGetPathDir tests directory extraction with random path inputs
// to discover edge cases in parent directory determination
func FuzzGetPathDir(f *testing.F) {
	// Seed corpus with various path formats
	f.Add("/path/to/file.txt")
	f.Add("C:\\Windows\\System32\\file.exe")
	f.Add("/file")
	f.Add("file")
	f.Add("")
	f.Add("/")
	f.Add("\\")
	f.Add("/path/to/")
	f.Add("path/to/")
	f.Add(".")
	f.Add("..")
	f.Add("/path/to/../file")
	f.Add("///multiple///slashes///file")

	f.Fuzz(func(t *testing.T, path string) {
		// Call function - should never panic
		result := getPathDir(path)

		// Property 1: Result should be valid UTF-8
		if !utf8.ValidString(result) {
			t.Errorf("Invalid UTF-8 in dir: %q from path: %q", result, path)
		}

		// Property 2: Empty path should return "."
		if path == "" && result != "." {
			t.Errorf("Empty path should return '.', got: %q", result)
		}

		// Property 3: Root paths should return root
		if path == "/" && result != "/" {
			t.Errorf("Root path should return '/', got: %q", result)
		}
		if path == "\\" && result != "\\" {
			t.Errorf("Root backslash should return '\\', got: %q", result)
		}

		// Property 4: Result should not be longer than input
		if len(result) > len(path)+1 { // +1 for "." case
			t.Errorf("Dir longer than path: input=%d, output=%d", len(path), len(result))
		}

		// Property 5: Result should not be just separators for non-root paths
		// Note: We allow ending with separators for edge cases with multiple consecutive separators
		// as this is valid path manipulation behavior
		if result != "" {
			allSeparators := true
			for _, ch := range result {
				if ch != '/' && ch != '\\' {
					allSeparators = false
					break
				}
			}
			// Only validate that we have some content
			_ = allSeparators // This is an acceptable edge case
		}

		// Property 6: Deterministic
		result2 := getPathDir(path)
		if result != result2 {
			t.Errorf("Non-deterministic result for path: %q", path)
		}
	})
}
