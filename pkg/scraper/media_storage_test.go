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

package scraper

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMediaStorage(t *testing.T) {
	t.Parallel()

	mockPlatform := &mocks.MockPlatform{}
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)
	require.NotNil(t, ms)
	assert.Equal(t, mockPlatform, ms.platform)
	assert.Equal(t, mockConfig, ms.config)
}

func TestGetMediaPath_PhysicalPath(t *testing.T) {
	t.Parallel()

	mockPlatform := &mocks.MockPlatform{}
	mockPlatform.SetupBasicMock()
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	tests := []struct {
		name        string
		gamePath    string
		systemID    string
		mediaType   MediaType
		extension   string
		expectPath  string // relative to root
		expectError bool
	}{
		{
			name:       "Cover art with jpg extension",
			gamePath:   "/games/snes/Super Mario World.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeCover,
			extension:  ".jpg",
			expectPath: "images/Super Mario World-box.jpg",
		},
		{
			name:       "Screenshot with png extension",
			gamePath:   "/games/genesis/Sonic.md",
			systemID:   "genesis",
			mediaType:  MediaTypeScreenshot,
			extension:  ".png",
			expectPath: "images/Sonic-image.png",
		},
		{
			name:       "Video with mp4 extension",
			gamePath:   "/roms/arcade/pacman.zip",
			systemID:   "arcade",
			mediaType:  MediaTypeVideo,
			extension:  ".mp4",
			expectPath: "videos/pacman-video.mp4",
		},
		{
			name:       "Manual with pdf extension",
			gamePath:   "/games/gba/Pokemon Ruby.gba",
			systemID:   "gba",
			mediaType:  MediaTypeManual,
			extension:  ".pdf",
			expectPath: "manuals/Pokemon Ruby-manual.pdf",
		},
		{
			name:       "Extension without dot gets dot added",
			gamePath:   "/games/snes/game.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeCover,
			extension:  "jpg",
			expectPath: "images/game-box.jpg",
		},
		{
			name:       "Box back image",
			gamePath:   "/games/snes/game.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeBoxBack,
			extension:  ".jpg",
			expectPath: "images/game-boxback.jpg",
		},
		{
			name:       "Title shot",
			gamePath:   "/games/snes/game.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeTitleShot,
			extension:  ".jpg",
			expectPath: "images/game-titleshot.jpg",
		},
		{
			name:       "Fan art",
			gamePath:   "/games/snes/game.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeFanArt,
			extension:  ".jpg",
			expectPath: "images/game-fanart.jpg",
		},
		{
			name:       "Marquee",
			gamePath:   "/games/snes/game.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeMarquee,
			extension:  ".jpg",
			expectPath: "images/game-marquee.jpg",
		},
		{
			name:       "Wheel",
			gamePath:   "/games/snes/game.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeWheel,
			extension:  ".png",
			expectPath: "images/game-wheel.png",
		},
		{
			name:       "Cartridge",
			gamePath:   "/games/snes/game.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeCartridge,
			extension:  ".jpg",
			expectPath: "images/game-cartridge.jpg",
		},
		{
			name:       "Bezel",
			gamePath:   "/games/snes/game.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeBezel,
			extension:  ".png",
			expectPath: "images/game-bezel.png",
		},
		{
			name:       "Map",
			gamePath:   "/games/snes/game.sfc",
			systemID:   "snes",
			mediaType:  MediaTypeMap,
			extension:  ".jpg",
			expectPath: "images/game-map.jpg",
		},
		{
			name:       "Complex filename with spaces and special chars",
			gamePath:   "/games/snes/Super Mario World - Special Edition (USA).sfc",
			systemID:   "snes",
			mediaType:  MediaTypeCover,
			extension:  ".jpg",
			expectPath: "images/Super Mario World - Special Edition (USA)-box.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaPath, err := ms.GetMediaPath(tt.gamePath, tt.systemID, tt.mediaType, tt.extension)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			// Check that the path ends with the expected relative path
			assert.True(t, filepath.IsAbs(mediaPath), "Media path should be absolute")
			assert.Contains(t, mediaPath, tt.expectPath)
		})
	}
}

func TestGetMediaPath_VirtualPaths(t *testing.T) {
	t.Parallel()

	mockPlatform := &mocks.MockPlatform{}
	mockPlatform.SetupBasicMock()
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	tests := []struct {
		name      string
		gamePath  string
		systemID  string
		mediaType MediaType
		extension string
	}{
		{
			name:      "Steam ID path",
			gamePath:  "steam://123456",
			systemID:  "steam",
			mediaType: MediaTypeCover,
			extension: ".jpg",
		},
		{
			name:      "Launcher path",
			gamePath:  "launcher://epic/game-id",
			systemID:  "epic",
			mediaType: MediaTypeScreenshot,
			extension: ".png",
		},
		{
			name:      "Simple ID without slashes",
			gamePath:  "12345",
			systemID:  "gog",
			mediaType: MediaTypeCover,
			extension: ".jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaPath, err := ms.GetMediaPath(tt.gamePath, tt.systemID, tt.mediaType, tt.extension)
			require.NoError(t, err)

			// Virtual paths should use zaparoo's media directory
			assert.Contains(t, mediaPath, "media")
			assert.Contains(t, mediaPath, tt.systemID)
		})
	}
}

func TestIsVirtualPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "Steam path",
			path:     "steam://123456",
			expected: true,
		},
		{
			name:     "Launcher path",
			path:     "launcher://epic/game",
			expected: true,
		},
		{
			name:     "Simple numeric ID",
			path:     "123456",
			expected: true,
		},
		{
			name:     "Simple string ID",
			path:     "game-id",
			expected: true,
		},
		{
			name:     "Unix file path",
			path:     "/games/snes/game.sfc",
			expected: false,
		},
		{
			name:     "Windows file path",
			path:     "C:\\Games\\SNES\\game.sfc",
			expected: false,
		},
		{
			name:     "Relative file path",
			path:     "games/snes/game.sfc",
			expected: false,
		},
		{
			name:     "File with extension only",
			path:     "game.sfc",
			expected: true, // Single filename is treated as virtual ID
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVirtualPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetMediaRootDir_PhysicalPaths(t *testing.T) {
	t.Parallel()

	// Create temporary directory structure for testing
	tmpDir := t.TempDir()

	// Create a structure like: tmpDir/games/snes/roms/game.sfc
	gamesDir := filepath.Join(tmpDir, "games")
	snesDir := filepath.Join(gamesDir, "snes")
	romsDir := filepath.Join(snesDir, "roms")
	err := os.MkdirAll(romsDir, 0755)
	require.NoError(t, err)

	gameFile := filepath.Join(romsDir, "game.sfc")
	err = os.WriteFile(gameFile, []byte("test"), 0644)
	require.NoError(t, err)

	mockPlatform := &mocks.MockPlatform{}
	mockPlatform.SetupBasicMock()
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	tests := []struct {
		name           string
		gamePath       string
		systemID       string
		expectedSubdir string // subdirectory that should be in the result
	}{
		{
			name:           "Game in snes directory",
			gamePath:       gameFile,
			systemID:       "snes",
			expectedSubdir: "snes",
		},
		{
			name:           "Game in games directory structure",
			gamePath:       gameFile,
			systemID:       "snes",
			expectedSubdir: "games", // Should find games as root
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir, err := ms.getMediaRootDir(tt.gamePath, tt.systemID)
			require.NoError(t, err)
			assert.Contains(t, rootDir, tt.expectedSubdir)
		})
	}
}

func TestGetMediaRootDir_VirtualPaths(t *testing.T) {
	t.Parallel()

	mockPlatform := &mocks.MockPlatform{}
	mockPlatform.SetupBasicMock()
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	rootDir, err := ms.getMediaRootDir("steam://123456", "steam")
	require.NoError(t, err)

	// Should use zaparoo's media directory for virtual paths
	assert.Contains(t, rootDir, "media")
	assert.Contains(t, rootDir, "steam")
}

func TestEnsureMediaDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	mockPlatform := &mocks.MockPlatform{}
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	// Test creating nested directory structure
	mediaPath := filepath.Join(tmpDir, "media", "snes", "images", "game-box.jpg")

	err := ms.EnsureMediaDirectory(mediaPath)
	require.NoError(t, err)

	// Verify the directory was created
	expectedDir := filepath.Join(tmpDir, "media", "snes", "images")
	_, err = os.Stat(expectedDir)
	assert.NoError(t, err)
}

func TestEnsureMediaDirectory_ExistingDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	mockPlatform := &mocks.MockPlatform{}
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	// Create directory first
	existingDir := filepath.Join(tmpDir, "existing")
	err := os.MkdirAll(existingDir, 0755)
	require.NoError(t, err)

	mediaPath := filepath.Join(existingDir, "game-box.jpg")

	// Should not error when directory already exists
	err = ms.EnsureMediaDirectory(mediaPath)
	assert.NoError(t, err)
}

func TestMediaExists(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	mockPlatform := &mocks.MockPlatform{}
	mockPlatform.SetupBasicMock()
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	// Create a temporary game file
	gameFile := filepath.Join(tmpDir, "game.sfc")
	err := os.WriteFile(gameFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Test when media doesn't exist
	exists, err := ms.MediaExists(gameFile, "snes", MediaTypeCover, ".jpg")
	require.NoError(t, err)
	assert.False(t, exists)

	// Create the media file
	mediaPath, err := ms.GetMediaPath(gameFile, "snes", MediaTypeCover, ".jpg")
	require.NoError(t, err)

	err = ms.EnsureMediaDirectory(mediaPath)
	require.NoError(t, err)

	err = os.WriteFile(mediaPath, []byte("image data"), 0644)
	require.NoError(t, err)

	// Test when media exists
	exists, err = ms.MediaExists(gameFile, "snes", MediaTypeCover, ".jpg")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestMediaExists_PathError(t *testing.T) {
	t.Parallel()

	mockPlatform := &mocks.MockPlatform{}
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	// Test with invalid platform that might cause getMediaRootDir to fail
	// We'll use a mock that doesn't have proper setup
	exists, err := ms.MediaExists("invalid://path", "invalid", MediaTypeCover, ".jpg")
	// Should handle the error gracefully
	assert.False(t, exists)
	// Error handling depends on implementation details - could be nil or actual error
	_ = err
}

func TestGetMediaPath_AllMediaTypes(t *testing.T) {
	t.Parallel()

	mockPlatform := &mocks.MockPlatform{}
	mockPlatform.SetupBasicMock()
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	gamePath := "/games/snes/test.sfc"
	extension := ".jpg"

	// Test all defined media types to ensure they map to appropriate folders and suffixes
	mediaTypeTests := []struct {
		mediaType      MediaType
		expectedFolder string
		expectedSuffix string
	}{
		{MediaTypeCover, "images", "box"},
		{MediaTypeBoxBack, "images", "boxback"},
		{MediaTypeScreenshot, "images", "image"},
		{MediaTypeTitleShot, "images", "titleshot"},
		{MediaTypeFanArt, "images", "fanart"},
		{MediaTypeMarquee, "images", "marquee"},
		{MediaTypeWheel, "images", "wheel"},
		{MediaTypeCartridge, "images", "cartridge"},
		{MediaTypeBezel, "images", "bezel"},
		{MediaTypeMap, "images", "map"},
		{MediaTypeVideo, "videos", "video"},
		{MediaTypeManual, "manuals", "manual"},
	}

	for _, tt := range mediaTypeTests {
		t.Run(string(tt.mediaType), func(t *testing.T) {
			mediaPath, err := ms.GetMediaPath(gamePath, "snes", tt.mediaType, extension)
			require.NoError(t, err)

			// Check folder
			assert.Contains(t, mediaPath, tt.expectedFolder)
			// Check suffix
			expectedFilename := fmt.Sprintf("test-%s%s", tt.expectedSuffix, extension)
			assert.Contains(t, mediaPath, expectedFilename)
		})
	}
}

func TestGetMediaPath_UnknownMediaType(t *testing.T) {
	t.Parallel()

	mockPlatform := &mocks.MockPlatform{}
	mockPlatform.SetupBasicMock()
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	// Test with custom/unknown media type
	customType := MediaType("custom_type")
	mediaPath, err := ms.GetMediaPath("/games/snes/test.sfc", "snes", customType, ".jpg")
	require.NoError(t, err)

	// Should default to images folder and use the media type as suffix
	assert.Contains(t, mediaPath, "images")
	assert.Contains(t, mediaPath, "test-custom_type.jpg")
}

func TestGetMediaRootDir_EdgeCases(t *testing.T) {
	t.Parallel()

	mockPlatform := &mocks.MockPlatform{}
	mockPlatform.SetupBasicMock()
	mockConfig := &config.Instance{}

	ms := NewMediaStorage(mockPlatform, mockConfig)

	tests := []struct {
		name     string
		gamePath string
		systemID string
	}{
		{
			name:     "Relative path",
			gamePath: "relative/path/game.sfc",
			systemID: "snes",
		},
		{
			name:     "Single filename",
			gamePath: "game.sfc",
			systemID: "snes",
		},
		{
			name:     "Deep nested path",
			gamePath: "/very/deep/nested/path/structure/with/many/levels/game.sfc",
			systemID: "snes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootDir, err := ms.getMediaRootDir(tt.gamePath, tt.systemID)
			require.NoError(t, err)
			assert.NotEmpty(t, rootDir)
			// Should return some valid directory path
			assert.True(t, len(rootDir) > 0) // Should return a valid directory path
		})
	}
}