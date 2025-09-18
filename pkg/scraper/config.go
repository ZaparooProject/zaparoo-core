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

	"github.com/BurntSushi/toml"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// LoadConfig loads scraper configuration from the config directory
func LoadConfig(pl platforms.Platform) (*ScraperConfig, error) {
	configDir := helpers.ConfigDir(pl)
	configPath := filepath.Join(configDir, "scraper.toml")

	// Start with defaults
	cfg := DefaultScraperConfig()

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config file
		if err := SaveConfig(pl, cfg); err != nil {
			return nil, fmt.Errorf("failed to create default scraper config: %w", err)
		}
		return cfg, nil
	}

	// Load existing config
	if _, err := toml.DecodeFile(configPath, cfg); err != nil {
		return nil, fmt.Errorf("failed to decode scraper config: %w", err)
	}

	// Update default media types based on boolean flags
	cfg.UpdateDefaultMediaTypes()

	return cfg, nil
}

// SaveConfig saves scraper configuration to the config directory
func SaveConfig(pl platforms.Platform, cfg *ScraperConfig) error {
	configDir := helpers.ConfigDir(pl)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "scraper.toml")
	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	encoder := toml.NewEncoder(file)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to encode scraper config: %w", err)
	}

	return nil
}

// GetScraperConfig gets the scraper configuration using the platform
func GetScraperConfig(pl platforms.Platform) *ScraperConfig {
	// Try to load scraper config from the separate scraper.toml file
	if scraperCfg, err := LoadConfig(pl); err == nil {
		return scraperCfg
	}

	// Fall back to defaults if loading fails
	scraperCfg := DefaultScraperConfig()
	scraperCfg.UpdateDefaultMediaTypes()
	return scraperCfg
}
