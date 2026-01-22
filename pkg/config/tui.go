// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog/log"
)

// TUIConfig holds TUI-specific configuration.
type TUIConfig struct {
	Theme            string `toml:"theme"`
	WriteFormat      string `toml:"write_format"`
	Mouse            bool   `toml:"mouse"`
	CRTMode          bool   `toml:"crt_mode"`
	OnScreenKeyboard bool   `toml:"on_screen_keyboard"`
}

// tuiConfigRaw is used for TOML unmarshalling with pointer fields
// to distinguish between missing values and explicit false/empty.
type tuiConfigRaw struct {
	Theme            *string `toml:"theme"`
	WriteFormat      *string `toml:"write_format"`
	Mouse            *bool   `toml:"mouse"`
	CRTMode          *bool   `toml:"crt_mode"`
	OnScreenKeyboard *bool   `toml:"on_screen_keyboard"`
}

const (
	defaultTUITheme            = "default"
	defaultTUIWriteFormat      = "zapscript"
	defaultTUIMouse            = true
	defaultTUICRTMode          = false
	defaultTUIOnScreenKeyboard = false
)

var tuiCfg atomic.Value

// DefaultTUIConfig returns the default TUI configuration.
func DefaultTUIConfig() TUIConfig {
	return TUIConfig{
		Theme:            defaultTUITheme,
		WriteFormat:      defaultTUIWriteFormat,
		Mouse:            defaultTUIMouse,
		CRTMode:          defaultTUICRTMode,
		OnScreenKeyboard: defaultTUIOnScreenKeyboard,
	}
}

// isCRTPlatform returns true for platforms that default to CRT mode.
func isCRTPlatform(platformID string) bool {
	return platformID == platformids.Mister || platformID == platformids.Mistex
}

// isOSKPlatform returns true for platforms that default to on-screen keyboard.
// These are typically controller-based devices without easy keyboard access.
func isOSKPlatform(platformID string) bool {
	return platformID == platformids.Mister || platformID == platformids.Mistex
}

// applyTUIDefaults merges raw config with defaults for any missing values.
// Platform ID is used to apply platform-specific defaults (e.g., CRT mode on MiSTer).
func applyTUIDefaults(raw tuiConfigRaw, platformID string) TUIConfig {
	cfg := DefaultTUIConfig()

	// Apply platform-specific defaults before user overrides
	if isCRTPlatform(platformID) {
		cfg.CRTMode = true
	}
	if isOSKPlatform(platformID) {
		cfg.OnScreenKeyboard = true
	}

	if raw.Theme != nil {
		cfg.Theme = *raw.Theme
	}
	if raw.WriteFormat != nil {
		cfg.WriteFormat = *raw.WriteFormat
	}
	if raw.Mouse != nil {
		cfg.Mouse = *raw.Mouse
	}
	if raw.CRTMode != nil {
		cfg.CRTMode = *raw.CRTMode
	}
	if raw.OnScreenKeyboard != nil {
		cfg.OnScreenKeyboard = *raw.OnScreenKeyboard
	}
	return cfg
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
// Missing values in the file are filled with defaults.
// Platform ID is used to apply platform-specific defaults (e.g., CRT mode on MiSTer).
func LoadTUIConfig(configDir, platformID string) error {
	tuiPath := filepath.Clean(filepath.Join(configDir, TUIFile))

	if _, err := os.Stat(tuiPath); os.IsNotExist(err) {
		log.Info().Str("path", tuiPath).Msg("creating default TUI config")
		cfg := applyTUIDefaults(tuiConfigRaw{}, platformID)
		tuiCfg.Store(cfg)
		if err := SaveTUIConfig(configDir); err != nil {
			return fmt.Errorf("failed to create TUI config: %w", err)
		}
		return nil
	}

	log.Info().Str("path", tuiPath).Msg("loading TUI config")
	data, err := os.ReadFile(tuiPath) //nolint:gosec // path is constructed from trusted config dir
	if err != nil {
		return fmt.Errorf("failed to read TUI config: %w", err)
	}

	var raw tuiConfigRaw
	if err := toml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to unmarshal TUI config: %w", err)
	}

	cfg := applyTUIDefaults(raw, platformID)
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
