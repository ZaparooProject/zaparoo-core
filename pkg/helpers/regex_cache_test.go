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
	"regexp"
	"testing"
)

func TestRegexCache(t *testing.T) {
	t.Parallel()
	cache := NewRegexCache()

	pattern := `test\d+`

	// First compilation
	re1 := cache.MustCompile(pattern)
	if re1 == nil {
		t.Fatal("expected compiled regex, got nil")
	}

	// Second compilation should return cached version
	re2 := cache.MustCompile(pattern)
	if re1 != re2 {
		t.Fatal("expected cached regex instance, got different instance")
	}

	// Verify pattern works
	if !re1.MatchString("test123") {
		t.Error("regex should match test123")
	}

	// Test cache size
	if cache.Size() != 1 {
		t.Errorf("expected cache size 1, got %d", cache.Size())
	}
}

func TestRegexCacheCompile(t *testing.T) {
	t.Parallel()
	cache := NewRegexCache()

	validPattern := `test\d+`
	invalidPattern := `[`

	// Valid pattern
	re, err := cache.Compile(validPattern)
	if err != nil {
		t.Fatalf("expected no error for valid pattern, got: %v", err)
	}
	if re == nil {
		t.Fatal("expected compiled regex, got nil")
	}

	// Invalid pattern
	_, err = cache.Compile(invalidPattern)
	if err == nil {
		t.Fatal("expected error for invalid pattern")
	}

	// Valid pattern should be cached
	re2, err := cache.Compile(validPattern)
	if err != nil {
		t.Fatalf("expected no error for cached pattern, got: %v", err)
	}
	if re != re2 {
		t.Fatal("expected cached regex instance, got different instance")
	}
}

func TestGlobalRegexCache(t *testing.T) {
	t.Parallel()
	pattern := `global\d+`

	// Test global convenience functions
	re1 := CachedMustCompile(pattern)
	re2 := CachedMustCompile(pattern)

	if re1 != re2 {
		t.Fatal("expected cached regex instance from global cache")
	}

	re3, err := CachedCompile(pattern)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if re1 != re3 {
		t.Fatal("expected same instance between MustCompile and Compile")
	}
}

func BenchmarkRegexCacheVsStandard(b *testing.B) {
	pattern := `benchmark\d+`
	testString := "benchmark123"

	b.Run("Standard", func(b *testing.B) {
		for range b.N {
			re := regexp.MustCompile(pattern)
			re.MatchString(testString)
		}
	})

	b.Run("Cached", func(b *testing.B) {
		cache := NewRegexCache()
		for range b.N {
			re := cache.MustCompile(pattern)
			re.MatchString(testString)
		}
	})

	b.Run("GlobalCached", func(b *testing.B) {
		for range b.N {
			re := CachedMustCompile(pattern)
			re.MatchString(testString)
		}
	})
}
