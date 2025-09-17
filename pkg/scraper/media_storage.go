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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// MediaStorage handles storing scraped media files following Batocera's naming convention
type MediaStorage struct {
	platform platforms.Platform
	config   *config.Instance
}

// NewMediaStorage creates a new media storage instance
func NewMediaStorage(pl platforms.Platform, cfg *config.Instance) *MediaStorage {
	return &MediaStorage{
		platform: pl,
		config:   cfg,
	}
}

// GetMediaPath returns the full path where a media file should be stored
// Following Batocera's exact file organization structure
func (ms *MediaStorage) GetMediaPath(gamePath string, systemID string, mediaType MediaType, extension string) (string, error) {
	// Get the base filename without extension
	baseFilename := filepath.Base(gamePath)
	stem := strings.TrimSuffix(baseFilename, filepath.Ext(baseFilename))

	// Determine the root directory for media storage
	rootDir, err := ms.getMediaRootDir(gamePath, systemID)
	if err != nil {
		return "", fmt.Errorf("failed to determine media root directory: %w", err)
	}

	// Create media folder structure based on media type
	var mediaFolder string
	switch mediaType {
	case MediaTypeCover, MediaTypeBoxBack, MediaTypeScreenshot, MediaTypeTitleShot,
		 MediaTypeFanArt, MediaTypeMarquee, MediaTypeWheel, MediaTypeCartridge,
		 MediaTypeBezel, MediaTypeMap:
		mediaFolder = "images"
	case MediaTypeVideo:
		mediaFolder = "videos"
	case MediaTypeManual:
		mediaFolder = "manuals"
	default:
		mediaFolder = "images" // Default fallback
	}

	// Create the full directory path
	fullDir := filepath.Join(rootDir, mediaFolder)

	// Generate the filename with Batocera's naming convention
	var suffix string
	switch mediaType {
	case MediaTypeCover:
		suffix = "box" // Batocera uses "box" for cover art
	case MediaTypeBoxBack:
		suffix = "boxback"
	case MediaTypeScreenshot:
		suffix = "image" // Default image type in Batocera
	case MediaTypeTitleShot:
		suffix = "titleshot"
	case MediaTypeFanArt:
		suffix = "fanart"
	case MediaTypeMarquee:
		suffix = "marquee"
	case MediaTypeWheel:
		suffix = "wheel"
	case MediaTypeCartridge:
		suffix = "cartridge"
	case MediaTypeBezel:
		suffix = "bezel"
	case MediaTypeMap:
		suffix = "map"
	case MediaTypeVideo:
		suffix = "video"
	case MediaTypeManual:
		suffix = "manual"
	default:
		suffix = string(mediaType)
	}

	// Ensure extension starts with a dot
	if extension != "" && !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}

	filename := fmt.Sprintf("%s-%s%s", stem, suffix, extension)
	return filepath.Join(fullDir, filename), nil
}

// getMediaRootDir determines where to store media files based on the game's location
func (ms *MediaStorage) getMediaRootDir(gamePath string, systemID string) (string, error) {
	// Check if this is a virtual path (like Steam ID)
	if isVirtualPath(gamePath) {
		// Use zaparoo's media directory as fallback
		dataDir := helpers.DataDir(ms.platform)
		return filepath.Join(dataDir, "media", systemID), nil
	}

	// For physical files, try to determine the system root directory
	// by looking at the game's parent directories
	absGamePath, err := filepath.Abs(gamePath)
	if err != nil {
		// If we can't resolve the path, use zaparoo's media directory
		dataDir := helpers.DataDir(ms.platform)
		return filepath.Join(dataDir, "media", systemID), nil
	}

	// Try to find a reasonable root directory by walking up the directory tree
	// Look for common patterns in game paths
	currentDir := filepath.Dir(absGamePath)
	for i := 0; i < 5; i++ { // Limit to 5 levels up to avoid going too high
		parent := filepath.Dir(currentDir)

		// If we've reached the root or can't go higher, use current directory
		if parent == currentDir || parent == "/" || (len(parent) == 3 && parent[1] == ':') {
			break
		}

		// Check if current directory looks like a system root
		// (contains the system name or is a reasonable stopping point)
		dirName := strings.ToLower(filepath.Base(currentDir))
		if strings.Contains(dirName, systemID) ||
		   dirName == "roms" || dirName == "games" || dirName == "media" {
			return currentDir, nil
		}

		currentDir = parent
	}

	// If we couldn't find a good root, use the directory containing the game
	return filepath.Dir(absGamePath), nil
}

// isVirtualPath checks if a path represents a virtual game (like Steam ID)
func isVirtualPath(path string) bool {
	// Steam IDs are typically numeric
	// Other virtual paths might have special prefixes
	return strings.HasPrefix(path, "steam://") ||
		   strings.HasPrefix(path, "launcher://") ||
		   (!strings.Contains(path, "/") && !strings.Contains(path, "\\"))
}

// EnsureMediaDirectory creates the necessary directory structure for storing media
func (ms *MediaStorage) EnsureMediaDirectory(mediaPath string) error {
	dir := filepath.Dir(mediaPath)
	return os.MkdirAll(dir, 0755)
}

// MediaExists checks if a media file already exists
func (ms *MediaStorage) MediaExists(gamePath string, systemID string, mediaType MediaType, extension string) (bool, error) {
	mediaPath, err := ms.GetMediaPath(gamePath, systemID, mediaType, extension)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(mediaPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// GetExistingMediaPaths returns all existing media files for a game
func (ms *MediaStorage) GetExistingMediaPaths(gamePath string, systemID string) (map[MediaType]string, error) {
	existingMedia := make(map[MediaType]string)

	// Check all common media types
	mediaTypes := []MediaType{
		MediaTypeCover, MediaTypeBoxBack, MediaTypeScreenshot, MediaTypeTitleShot,
		MediaTypeFanArt, MediaTypeMarquee, MediaTypeWheel, MediaTypeCartridge,
		MediaTypeBezel, MediaTypeMap, MediaTypeVideo, MediaTypeManual,
	}

	// Common extensions to check
	extensions := []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".mp4", ".avi", ".pdf"}

	for _, mediaType := range mediaTypes {
		for _, ext := range extensions {
			exists, err := ms.MediaExists(gamePath, systemID, mediaType, ext)
			if err != nil {
				continue // Skip errors and continue checking
			}
			if exists {
				mediaPath, err := ms.GetMediaPath(gamePath, systemID, mediaType, ext)
				if err == nil {
					existingMedia[mediaType] = mediaPath
					break // Found this media type, move to next
				}
			}
		}
	}

	return existingMedia, nil
}

// CleanupEmptyDirectories removes empty media directories
func (ms *MediaStorage) CleanupEmptyDirectories(rootDir string) error {
	// Remove empty images, videos, manuals directories
	subdirs := []string{"images", "videos", "manuals"}

	for _, subdir := range subdirs {
		dirPath := filepath.Join(rootDir, subdir)
		if entries, err := os.ReadDir(dirPath); err == nil && len(entries) == 0 {
			os.Remove(dirPath) // Ignore errors, directory might not exist
		}
	}

	return nil
}