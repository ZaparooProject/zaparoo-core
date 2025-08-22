/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package tty2oled

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	pictureRepoURL  = "https://raw.githubusercontent.com/venice1200/MiSTer_tty2oled_Pictures/main/Pictures"
	pictureSubDir   = "tty2oled_pics"
	downloadTimeout = 30 * time.Second
)

type PictureManager struct {
	cfg        *config.Instance
	httpClient *http.Client
	cacheDir   string
}

func NewPictureManager(cfg *config.Instance, pl platforms.Platform) *PictureManager {
	cacheDir := filepath.Join(helpers.DataDir(pl), config.AssetsDir, pictureSubDir)
	return &PictureManager{
		cfg:      cfg,
		cacheDir: cacheDir,
		httpClient: &http.Client{
			Timeout: downloadTimeout,
		},
	}
}

func (pm *PictureManager) GetPictureForSystem(systemID string) (string, error) {
	if systemID == "" {
		return "", errors.New("empty system ID")
	}

	// Map system ID to picture name
	pictureName := mapSystemToPicture(systemID)
	if pictureName == "" {
		return "", fmt.Errorf("no picture mapping available for system: %s", systemID)
	}

	// Select variant (base or alternative)
	selectedVariant := selectPictureVariant(pictureName)

	log.Debug().
		Str("system", systemID).
		Str("mapped_picture", pictureName).
		Str("selected_variant", selectedVariant).
		Msg("mapped system to picture")

	// Ensure cache directory exists
	if err := pm.ensureCacheDir(); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Try to find picture in order of preference
	for _, format := range PictureFormats {
		picturePath, err := pm.findPicture(selectedVariant, format)
		if err == nil && picturePath != "" {
			// Verify file exists and is readable
			if _, err := os.Stat(picturePath); err == nil {
				log.Debug().
					Str("system", systemID).
					Str("variant", selectedVariant).
					Str("format", format).
					Str("path", picturePath).
					Msg("found picture for system")
				return picturePath, nil
			}
		}
	}

	// No picture found, try to download
	return pm.downloadPictureForSystem(selectedVariant)
}

// FindPictureOnDisk checks if a picture is immediately available on disk without downloading
func (pm *PictureManager) FindPictureOnDisk(systemID string) (string, bool) {
	log.Debug().Str("system", systemID).Msg("FindPictureOnDisk: starting")

	if systemID == "" {
		return "", false
	}

	// Map system ID to picture name
	log.Debug().Str("system", systemID).Msg("FindPictureOnDisk: mapping system to picture")
	pictureName := mapSystemToPicture(systemID)
	log.Debug().Str("system", systemID).Str("picture", pictureName).Msg("FindPictureOnDisk: mapped to picture")
	if pictureName == "" {
		log.Debug().Str("system", systemID).Msg("no picture mapping available for system")
		return "", false
	}

	// Select variant (base or alternative) - same selection logic as GetPictureForSystem
	log.Debug().Str("system", systemID).Str("picture", pictureName).Msg("FindPictureOnDisk: selecting variant")
	selectedVariant := selectPictureVariant(pictureName)
	log.Debug().
		Str("system", systemID).
		Str("selected_variant", selectedVariant).
		Msg("FindPictureOnDisk: variant selected")

	// Ensure cache directory exists
	if err := pm.ensureCacheDir(); err != nil {
		return "", false
	}

	// Try to find picture in order of preference
	for _, format := range PictureFormats {
		picturePath, err := pm.findPicture(selectedVariant, format)
		if err == nil && picturePath != "" {
			// Verify file exists and is readable
			if _, err := os.Stat(picturePath); err == nil {
				log.Debug().
					Str("system", systemID).
					Str("variant", selectedVariant).
					Str("format", format).
					Str("path", picturePath).
					Msg("found picture on disk for system")
				return picturePath, true
			}
		}
	}

	return "", false
}

// findPicture looks for a picture file in the cache directory
// Note: pictureName is now the exact picture name to find (e.g., "Genesis", "Genesis_alt1", "AO486", etc.)
func (pm *PictureManager) findPicture(pictureName, format string) (string, error) {
	formatDir := filepath.Join(pm.cacheDir, format)

	// Look for the exact picture file
	exactPath := filepath.Join(formatDir, pictureName+pm.getFileExtension(format))
	if _, err := os.Stat(exactPath); err == nil {
		return exactPath, nil
	}

	return "", fmt.Errorf("picture not found: %s in format %s", pictureName, format)
}

// downloadPictureForSystem attempts to download a picture for the given picture name
// Note: systemID is now the already-mapped picture name (e.g., "AO486", "Genesis", etc.)
func (pm *PictureManager) downloadPictureForSystem(pictureName string) (string, error) {
	log.Info().Str("picture", pictureName).Msg("attempting to download picture")

	var lastErr error

	// Try each format in order of preference
	for _, format := range PictureFormats {
		picturePath, err := pm.downloadPicture(pictureName, format)
		if err == nil {
			log.Info().
				Str("picture", pictureName).
				Str("format", format).
				Str("path", picturePath).
				Msg("successfully downloaded picture")
			return picturePath, nil
		}
		lastErr = err
	}

	return "", fmt.Errorf("failed to download picture %s: %w", pictureName, lastErr)
}

// downloadPicture downloads a specific picture file from the GitHub repository
func (pm *PictureManager) downloadPicture(fileName, format string) (string, error) {
	fileExt := pm.getFileExtension(format)
	fullFileName := fileName + fileExt

	url := fmt.Sprintf("%s/%s/%s", pictureRepoURL, format, fullFileName)

	// Create format directory
	formatDir := filepath.Join(pm.cacheDir, format)
	if err := os.MkdirAll(formatDir, 0o750); err != nil {
		return "", fmt.Errorf("failed to create format directory: %w", err)
	}

	// Download file
	localPath := filepath.Join(formatDir, fullFileName)
	if err := pm.downloadFile(url, localPath); err != nil {
		return "", fmt.Errorf("failed to download %s: %w", url, err)
	}

	return localPath, nil
}

// downloadFile downloads a file from URL to local path
func (pm *PictureManager) downloadFile(url, localPath string) error {
	const maxBytes = 1 << 20 // 1 MiB limit for picture files

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := pm.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	if resp == nil {
		return errors.New("received nil response")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
	}

	// Check Content-Length header if available
	if resp.ContentLength > 0 && resp.ContentLength > maxBytes {
		return fmt.Errorf("file too large: %d bytes exceeds limit of %d bytes", resp.ContentLength, maxBytes)
	}

	// Validate and create the file
	cleanPath := filepath.Clean(localPath)
	if !strings.HasPrefix(cleanPath, filepath.Clean(pm.cacheDir)) {
		return errors.New("invalid file path")
	}

	file, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Use LimitedReader to enforce max size during copy
	limited := &io.LimitedReader{R: resp.Body, N: maxBytes}
	n, err := io.Copy(file, limited)
	if err != nil {
		// Clean up partial file on error
		_ = os.Remove(cleanPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Check if download was truncated due to size limit
	if n == maxBytes && limited.N == 0 {
		_ = os.Remove(cleanPath)
		return errors.New("download truncated: file exceeded maximum size limit")
	}

	// Basic validation: ensure we got some data
	if n == 0 {
		_ = os.Remove(cleanPath)
		return errors.New("downloaded file is empty")
	}

	log.Debug().Str("url", url).Str("path", localPath).Int64("bytes", n).Msg("downloaded picture file")
	return nil
}

// getFileExtension returns the appropriate file extension for a picture format
func (*PictureManager) getFileExtension(format string) string {
	switch strings.ToUpper(format) {
	case "GSC", "GSC_US":
		return ".gsc"
	case "XBM", "XBM_TEXT", "XBM_US":
		return ".xbm"
	default:
		return ".bin" // fallback
	}
}

// ensureCacheDir creates the cache directory if it doesn't exist
func (pm *PictureManager) ensureCacheDir() error {
	if _, err := os.Stat(pm.cacheDir); os.IsNotExist(err) {
		log.Info().Str("dir", pm.cacheDir).Msg("creating tty2oled pictures cache directory")
		if err := os.MkdirAll(pm.cacheDir, 0o750); err != nil {
			return fmt.Errorf("failed to create cache directory: %w", err)
		}
		return nil
	}
	return nil
}

// ClearCache removes all cached pictures
func (pm *PictureManager) ClearCache() error {
	log.Info().Str("dir", pm.cacheDir).Msg("clearing tty2oled pictures cache")
	if err := os.RemoveAll(pm.cacheDir); err != nil {
		return fmt.Errorf("failed to remove cache directory: %w", err)
	}
	return nil
}

// GetCacheInfo returns information about the picture cache
func (pm *PictureManager) GetCacheInfo() (map[string]int, error) {
	info := make(map[string]int)

	for _, format := range PictureFormats {
		formatDir := filepath.Join(pm.cacheDir, format)
		count := 0

		if files, err := os.ReadDir(formatDir); err == nil {
			for _, file := range files {
				if !file.IsDir() {
					count++
				}
			}
		}

		info[format] = count
	}

	return info, nil
}
