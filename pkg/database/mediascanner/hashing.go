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
	"fmt"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper/hasher"
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
	fileHashes, err := hasher.ComputeFileHashes(mediaPath)
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
