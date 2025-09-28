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

package mister

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/stretchr/testify/assert"
)

func TestBuildPathPrefixCache(t *testing.T) {
	platform := NewPlatform()
	cfg := &config.Instance{}

	// Initialize launcher cache first
	helpers.GlobalLauncherCache.Initialize(platform, cfg)

	// Build the path prefix cache
	platform.BuildPathPrefixCache(cfg)

	// Verify cache was built
	platform.pathPrefixMu.RLock()
	defer platform.pathPrefixMu.RUnlock()

	assert.NotNil(t, platform.pathPrefixCache)
	assert.NotEmpty(t, platform.pathPrefixCache, "Cache should contain prefix mappings")

	// Verify cache contains expected entries
	found := false
	for prefix := range platform.pathPrefixCache {
		if prefix != "" {
			found = true
			break
		}
	}
	assert.True(t, found, "Cache should contain at least one non-empty prefix")
}

func TestFastNormalizePath(t *testing.T) {
	platform := NewPlatform()
	cfg := &config.Instance{}

	// Initialize and build cache
	helpers.GlobalLauncherCache.Initialize(platform, cfg)
	platform.BuildPathPrefixCache(cfg)

	tests := []struct {
		name     string
		input    string
		expected string // empty means should return input unchanged
	}{
		{
			name:     "empty path",
			input:    "",
			expected: "",
		},
		{
			name:     "unrecognized path",
			input:    "/random/unknown/path.txt",
			expected: "/random/unknown/path.txt",
		},
		{
			name:     "path without filename",
			input:    "/media/fat/games/SNES/",
			expected: "/media/fat/games/SNES/", // Should return unchanged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := platform.fastNormalizePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizePathWithCache(t *testing.T) {
	platform := NewPlatform()
	cfg := &config.Instance{}

	// Initialize and build cache
	helpers.GlobalLauncherCache.Initialize(platform, cfg)
	platform.BuildPathPrefixCache(cfg)

	// Test that the main NormalizePath function works with our optimization
	testPath := "/some/test/path.rom"
	result := platform.NormalizePath(cfg, testPath)

	// Should get some result (either normalized or original)
	assert.NotEmpty(t, result)
}

func TestNormalizePathBehavior(t *testing.T) {
	platform := NewPlatform()
	cfg := &config.Instance{}

	// Initialize and build cache
	helpers.GlobalLauncherCache.Initialize(platform, cfg)
	platform.BuildPathPrefixCache(cfg)

	tests := []struct {
		name     string
		input    string
		wantType string // "normalized", "unchanged", or "fallback"
	}{
		{
			name:     "recognizable game path",
			input:    "/media/fat/games/SNES/test.sfc",
			wantType: "normalized", // Should be handled by fast path
		},
		{
			name:     "unrecognized path",
			input:    "/random/unknown/path.txt",
			wantType: "unchanged", // Should return unchanged
		},
		{
			name:     "empty path",
			input:    "",
			wantType: "unchanged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := platform.NormalizePath(cfg, tt.input)

			switch tt.wantType {
			case "normalized":
				// Should return something different from input
				assert.NotEqual(t, tt.input, result, "Path should be normalized")
				assert.NotEmpty(t, result, "Normalized path should not be empty")
			case "unchanged":
				// Should return the same as input
				assert.Equal(t, tt.input, result, "Path should remain unchanged")
			}
		})
	}
}

func TestFastNormalizePathWithNilCache(t *testing.T) {
	platform := NewPlatform()

	// Don't build cache - should return original path
	result := platform.fastNormalizePath("/test/path.rom")
	assert.Equal(t, "/test/path.rom", result)
}

func TestConcurrentNormalization(t *testing.T) {
	platform := NewPlatform()
	cfg := &config.Instance{}

	// Initialize and build cache
	helpers.GlobalLauncherCache.Initialize(platform, cfg)
	platform.BuildPathPrefixCache(cfg)

	// Test concurrent access to verify thread safety
	testPath := "/media/fat/games/test.rom"
	numGoroutines := 10
	numIterations := 100

	done := make(chan bool, numGoroutines)

	for range numGoroutines {
		go func() {
			defer func() { done <- true }()
			for range numIterations {
				result := platform.NormalizePath(cfg, testPath)
				assert.NotEmpty(t, result)
			}
		}()
	}

	// Wait for all goroutines to complete
	for range numGoroutines {
		<-done
	}
}
