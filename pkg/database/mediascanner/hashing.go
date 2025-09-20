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

package mediascanner

import (
	"archive/zip"
	"crypto/md5"  //nolint:gosec // MD5 required for ROM identification
	"crypto/sha1" //nolint:gosec // SHA1 required for ROM identification
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// GetHashConfig determines which hash types to compute based on user config and platform defaults
func GetHashConfig(cfg *config.Instance, platform platforms.Platform) database.HashConfig {
	// Check user configuration first
	if cfg != nil {
		if userHashes := cfg.SystemHashes(); userHashes != nil {
			// User has explicitly configured hashes
			hashTypes := database.ParseHashTypes(*userHashes)
			return database.ToHashConfig(hashTypes)
		}
	}

	// Fall back to platform defaults (or empty if platform is nil - for tests)
	if platform != nil {
		platformHashes := platform.Settings().DefaultHashes
		return database.ToHashConfig(platformHashes)
	}

	// Default: no hashing
	return database.HashConfig{}
}

// ComputeAndStoreHashes computes and stores hashes for a media file if hashing is enabled
func ComputeAndStoreHashes(
	cfg *config.Instance,
	platform platforms.Platform,
	db database.MediaDBI,
	systemID string,
	mediaPath string,
) error {
	hashConfig := GetHashConfig(cfg, platform)

	// Skip if no hashing is configured
	if !hashConfig.CRC32 && !hashConfig.MD5 && !hashConfig.SHA1 {
		return nil
	}

	// Check if hashes already exist for this file
	existingHashes, err := db.GetGameHashes(systemID, mediaPath)
	if err == nil && existingHashes != nil {
		// Hashes already exist, skip computation
		log.Debug().Str("system", systemID).Str("path", mediaPath).Msg("hashes already exist, skipping")
		return nil
	}

	// Compute file hashes
	log.Debug().Str("system", systemID).Str("path", mediaPath).Msg("computing file hashes")
	fileHashes, err := ComputeFileHashes(mediaPath)
	if err != nil {
		log.Warn().Err(err).Str("system", systemID).Str("path", mediaPath).Msg("failed to compute file hashes")
		return fmt.Errorf("failed to compute file hashes for %s: %w", mediaPath, err)
	}

	// Create GameHashes struct with only requested hash types
	gameHashes := &database.GameHashes{
		SystemID:   systemID,
		MediaPath:  mediaPath,
		ComputedAt: time.Now(),
		FileSize:   fileHashes.FileSize,
	}

	// Only include hash types that are enabled
	if hashConfig.CRC32 {
		gameHashes.CRC32 = fileHashes.CRC32
	}
	if hashConfig.MD5 {
		gameHashes.MD5 = fileHashes.MD5
	}
	if hashConfig.SHA1 {
		gameHashes.SHA1 = fileHashes.SHA1
	}

	// Save hashes to database
	if err := db.SaveGameHashes(gameHashes); err != nil {
		log.Error().Err(err).Str("system", systemID).Str("path", mediaPath).Msg("failed to save file hashes")
		return fmt.Errorf("failed to save game hashes for %s: %w", mediaPath, err)
	}

	log.Debug().Str("system", systemID).Str("path", mediaPath).Msg("successfully computed and stored file hashes")
	return nil
}

// GetHashConfigDescription returns a human-readable description of the hash configuration
func GetHashConfigDescription(cfg *config.Instance, platform platforms.Platform) string {
	hashConfig := GetHashConfig(cfg, platform)
	if !hashConfig.CRC32 && !hashConfig.MD5 && !hashConfig.SHA1 {
		return "disabled"
	}

	var enabled []string
	if hashConfig.CRC32 {
		enabled = append(enabled, "CRC32")
	}
	if hashConfig.MD5 {
		enabled = append(enabled, "MD5")
	}
	if hashConfig.SHA1 {
		enabled = append(enabled, "SHA1")
	}

	return filepath.Join(enabled...)
}

// FileHash contains all hash information for a file
type FileHash struct {
	CRC32    string
	MD5      string
	SHA1     string
	FileSize int64
}

// ComputeFileHashes calculates all hashes for a file path
// Handles regular files and paths that include ZIP archives (MiSTer-style paths)
func ComputeFileHashes(filePath string) (*FileHash, error) {
	// Check if path contains a ZIP file (MiSTer-style path)
	// Example: /media/games/snes/roms.zip/Super Mario World.sfc
	for i := len(filePath) - 1; i >= 0; i-- {
		if i > 4 && strings.HasSuffix(filePath[:i+1], ".zip") {
			zipPath := filePath[:i+1]
			fileInZip := filePath[i+2:] // Skip the '/' after .zip

			if _, err := os.Stat(zipPath); err == nil {
				return computeFileInZip(zipPath, fileInZip)
			}
		}
	}

	// Regular file hashing
	return computeRegularFileHash(filePath)
}

// computeFileInZip extracts and hashes a specific file from a ZIP
func computeFileInZip(zipPath, fileInZip string) (*FileHash, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close zip reader")
		}
	}()

	// Find the specific file in the ZIP
	for _, f := range r.File {
		if f.Name != fileInZip {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open file in zip: %w", err)
		}

		size := f.UncompressedSize64
		if size > 9223372036854775807 { // max int64
			_ = rc.Close()
			return nil, fmt.Errorf("file too large: %d bytes", size)
		}

		result, err := hashReader(rc, int64(size))
		_ = rc.Close()
		return result, err
	}

	return nil, fmt.Errorf("file %s not found in archive %s", fileInZip, zipPath)
}

// computeRegularFileHash computes hashes for a regular file
func computeRegularFileHash(filePath string) (*FileHash, error) {
	file, err := os.Open(filePath) //nolint:gosec // filePath is validated by caller
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close file")
		}
	}()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file stats: %w", err)
	}

	return hashReader(file, stat.Size())
}

// hashReader computes all hashes from an io.Reader
func hashReader(r io.Reader, size int64) (*FileHash, error) {
	crc32Hash := crc32.NewIEEE()
	md5Hash := md5.New()   //nolint:gosec // MD5 required for ROM identification
	sha1Hash := sha1.New() //nolint:gosec // SHA1 required for ROM identification

	// Use io.MultiWriter to compute all hashes in one pass
	w := io.MultiWriter(crc32Hash, md5Hash, sha1Hash)

	if _, err := io.Copy(w, r); err != nil {
		return nil, fmt.Errorf("failed to read file for hashing: %w", err)
	}

	return &FileHash{
		CRC32:    fmt.Sprintf("%08x", crc32Hash.Sum32()),
		MD5:      fmt.Sprintf("%x", md5Hash.Sum(nil)),
		SHA1:     fmt.Sprintf("%x", sha1Hash.Sum(nil)),
		FileSize: size,
	}, nil
}

// ValidateHashes checks if the provided hashes match the file
func ValidateHashes(filePath string, expectedHash *FileHash) (bool, error) {
	computedHash, err := ComputeFileHashes(filePath)
	if err != nil {
		return false, err
	}

	// Check each hash type if provided
	if expectedHash.CRC32 != "" && computedHash.CRC32 != expectedHash.CRC32 {
		return false, nil
	}
	if expectedHash.MD5 != "" && computedHash.MD5 != expectedHash.MD5 {
		return false, nil
	}
	if expectedHash.SHA1 != "" && computedHash.SHA1 != expectedHash.SHA1 {
		return false, nil
	}
	if expectedHash.FileSize > 0 && computedHash.FileSize != expectedHash.FileSize {
		return false, nil
	}

	return true, nil
}

