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

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog/log"
)

// TUIConfig holds TUI-specific configuration.
type TUIConfig struct {
	Theme       string `toml:"theme"`
	WriteFormat string `toml:"write_format"`
	Mouse       bool   `toml:"mouse"`
}

var tuiCfg atomic.Value

// DefaultTUIConfig returns the default TUI configuration.
func DefaultTUIConfig() TUIConfig {
	return TUIConfig{
		Theme:       "default",
		Mouse:       true,
		WriteFormat: "zapscript",
	}
}

// GetTUIConfig returns the current TUI configuration.
func GetTUIConfig() TUIConfig {
	val := tuiCfg.Load()
	if val == nil {
		return DefaultTUIConfig()
	}
	cfg, ok := val.(TUIConfig)
	if !ok {
		return DefaultTUIConfig()
	}
	return cfg
}

// SetTUIConfig updates the TUI configuration in memory.
func SetTUIConfig(cfg TUIConfig) {
	tuiCfg.Store(cfg)
}

// LoadTUIConfig loads the TUI configuration from disk.
// If the file doesn't exist, it creates one with default values.
func LoadTUIConfig(configDir string) error {
	tuiPath := filepath.Clean(filepath.Join(configDir, TUIFile))

	if _, err := os.Stat(tuiPath); os.IsNotExist(err) {
		log.Info().Str("path", tuiPath).Msg("creating default TUI config")
		cfg := DefaultTUIConfig()
		if err := SaveTUIConfig(configDir); err != nil {
			return fmt.Errorf("failed to create TUI config: %w", err)
		}
		tuiCfg.Store(cfg)
		return nil
	}

	log.Info().Str("path", tuiPath).Msg("loading TUI config")
	data, err := os.ReadFile(tuiPath) //nolint:gosec // path is constructed from trusted config dir
	if err != nil {
		return fmt.Errorf("failed to read TUI config: %w", err)
	}

	var cfg TUIConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal TUI config: %w", err)
	}

	tuiCfg.Store(cfg)
	return nil
}

// SaveTUIConfig saves the current TUI configuration to disk.
func SaveTUIConfig(configDir string) error {
	tuiPath := filepath.Join(configDir, TUIFile)
	cfg := GetTUIConfig()

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal TUI config: %w", err)
	}

	if err := os.WriteFile(tuiPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write TUI config: %w", err)
	}

	return nil
}
