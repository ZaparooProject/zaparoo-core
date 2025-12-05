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
	"fmt"
	"regexp"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// RegexCache provides thread-safe caching of compiled regular expressions
// to avoid repeated compilation overhead for dynamic/runtime patterns.
//
// # When to Use
//
// Use CachedCompile for DYNAMIC patterns only:
//   - Patterns from user configuration (config files, databases)
//   - Patterns from external sources (CSV files, APIs)
//   - Patterns that vary at runtime
//
// For STATIC patterns (constant strings), use package-level variables instead:
//
//	var myPattern = regexp.MustCompile(`pattern`)  // ✅ Static pattern
//	re := helpers.CachedMustCompile(`pattern`)     // ❌ Don't use for static
//
// Package-level vars provide:
//   - Zero runtime overhead (no map lookups)
//   - Compile-time validation (errors caught at startup)
//   - Better code organization (patterns visible at top of file)
type RegexCache struct {
	cache map[string]*regexp.Regexp
	mu    syncutil.RWMutex
}

// GlobalRegexCache is the singleton instance used throughout the application
var GlobalRegexCache = NewRegexCache()

// NewRegexCache creates a new RegexCache instance
func NewRegexCache() *RegexCache {
	return &RegexCache{
		cache: make(map[string]*regexp.Regexp),
	}
}

// MustCompile compiles a regex pattern and caches it for future use.
// If the pattern is already cached, returns the cached version.
// Panics if the pattern cannot be compiled (same behavior as regexp.MustCompile).
func (rc *RegexCache) MustCompile(pattern string) *regexp.Regexp {
	// Fast path: try read lock first
	rc.mu.RLock()
	if re, exists := rc.cache[pattern]; exists {
		rc.mu.RUnlock()
		return re
	}
	rc.mu.RUnlock()

	// Slow path: compile and cache with write lock
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Double-check pattern wasn't added while waiting for lock
	if re, exists := rc.cache[pattern]; exists {
		return re
	}

	// Compile and cache the pattern
	re := regexp.MustCompile(pattern)
	rc.cache[pattern] = re
	return re
}

// Compile compiles a regex pattern and caches it for future use.
// If the pattern is already cached, returns the cached version.
// Returns an error if the pattern cannot be compiled.
func (rc *RegexCache) Compile(pattern string) (*regexp.Regexp, error) {
	// Fast path: try read lock first
	rc.mu.RLock()
	if re, exists := rc.cache[pattern]; exists {
		rc.mu.RUnlock()
		return re, nil
	}
	rc.mu.RUnlock()

	// Slow path: compile and cache with write lock
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Double-check pattern wasn't added while waiting for lock
	if re, exists := rc.cache[pattern]; exists {
		return re, nil
	}

	// Compile and cache the pattern
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile regex pattern %q: %w", pattern, err)
	}

	rc.cache[pattern] = re
	return re, nil
}

// Clear removes all cached patterns (useful for testing or memory management)
func (rc *RegexCache) Clear() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.cache = make(map[string]*regexp.Regexp)
}

// Size returns the number of cached patterns
func (rc *RegexCache) Size() int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return len(rc.cache)
}

// Convenience functions that use the global cache for DYNAMIC patterns.
//
// IMPORTANT: Only use these for runtime/dynamic patterns (from config, database, user input).
// For static constant patterns, use package-level vars instead:
//
//	var myPattern = regexp.MustCompile(`pattern`)  // ✅ For static patterns
//
// See package documentation for detailed guidance.

// CachedMustCompile compiles and caches a dynamic regex pattern.
// Panics if pattern is invalid. Only use for runtime patterns - for static patterns,
// use package-level vars instead.
func CachedMustCompile(pattern string) *regexp.Regexp {
	return GlobalRegexCache.MustCompile(pattern)
}

// CachedCompile compiles and caches a dynamic regex pattern.
// Returns error if pattern is invalid. Only use for runtime patterns - for static patterns,
// use package-level vars instead.
func CachedCompile(pattern string) (*regexp.Regexp, error) {
	return GlobalRegexCache.Compile(pattern)
}
