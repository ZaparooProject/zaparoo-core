package helpers

import (
	"regexp"
	"sync"
)

// RegexCache provides thread-safe caching of compiled regular expressions
// to avoid repeated compilation overhead during media scanning operations.
type RegexCache struct {
	mu    sync.RWMutex
	cache map[string]*regexp.Regexp
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
		return nil, err
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

// Convenience functions that use the global cache
func CachedMustCompile(pattern string) *regexp.Regexp {
	return GlobalRegexCache.MustCompile(pattern)
}

func CachedCompile(pattern string) (*regexp.Regexp, error) {
	return GlobalRegexCache.Compile(pattern)
}
