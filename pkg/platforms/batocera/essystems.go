//go:build linux

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

package batocera

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

const (
	// ESConfigDir is the EmulationStation config directory on Batocera.
	ESConfigDir = "/userdata/system/configs/emulationstation"
	// ESSystemsConfigFile is the main ES systems config file.
	ESSystemsConfigFile = "es_systems.cfg"
	// ESSystemsOverlayPattern is the glob pattern for overlay config files.
	ESSystemsOverlayPattern = "es_systems_*.cfg"
	// defaultROMPath is the default ROM directory used for relative path resolution.
	defaultROMPath = "/userdata/roms"
)

// ESSystem represents a single system entry from es_systems.cfg.
type ESSystem struct {
	Name string `xml:"name"`
	Path string `xml:"path"`
}

// ESSystemList is the root element of es_systems.cfg.
type ESSystemList struct {
	XMLName xml.Name   `xml:"systemList"`
	Systems []ESSystem `xml:"system"`
}

// ESSystemConfig holds parsed ES system configuration.
type ESSystemConfig struct {
	// Systems maps system name to its configuration.
	Systems map[string]ESSystem
}

// ParseESSystemsConfig parses EmulationStation config files and returns
// discovered system configurations. It reads the main config file and
// any overlay files (es_systems_*.cfg).
func ParseESSystemsConfig(configDir string) (*ESSystemConfig, error) {
	config := &ESSystemConfig{
		Systems: make(map[string]ESSystem),
	}

	// Parse main config file if it exists
	mainConfigPath := filepath.Join(configDir, ESSystemsConfigFile)
	if err := parseESSystemFile(mainConfigPath, config); err != nil {
		log.Debug().Err(err).Str("path", mainConfigPath).Msg("skipping main ES config")
	}

	// Parse overlay files (es_systems_*.cfg)
	overlayPattern := filepath.Join(configDir, ESSystemsOverlayPattern)
	overlayFiles, err := filepath.Glob(overlayPattern)
	if err != nil {
		log.Debug().Err(err).Str("pattern", overlayPattern).Msg("failed to glob overlay files")
	}

	for _, overlayPath := range overlayFiles {
		if err := parseESSystemFile(overlayPath, config); err != nil {
			log.Debug().Err(err).Str("path", overlayPath).Msg("skipping overlay config")
		}
	}

	return config, nil
}

// parseESSystemFile parses a single ES systems config file and merges
// systems into the provided config. Overlay files extend/override the base.
func parseESSystemFile(path string, cfg *ESSystemConfig) error {
	file, err := os.Open(path) // #nosec G304 - path comes from controlled config directory
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("path", path).Msg("error closing ES config file")
		}
	}()

	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var systemList ESSystemList
	if err := xml.Unmarshal(data, &systemList); err != nil {
		return fmt.Errorf("parse XML: %w", err)
	}

	// Merge systems into cfg (overlay systems override existing)
	for _, sys := range systemList.Systems {
		if sys.Name != "" && sys.Path != "" {
			cfg.Systems[sys.Name] = sys
		}
	}

	log.Debug().Str("path", path).Int("systems", len(systemList.Systems)).Msg("parsed ES systems config")
	return nil
}

// GetROMPaths returns deduplicated ROM paths from parsed ES config.
// Paths are expanded (environment variables resolved) and normalized.
func (c *ESSystemConfig) GetROMPaths() []string {
	if c == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var paths []string

	for _, sys := range c.Systems {
		expandedPath := expandPath(sys.Path)
		if expandedPath == "" {
			continue
		}

		// Normalize to directory (ROM path is the parent of system-specific folders)
		// ES paths are typically like "/userdata/roms" or "/media/SHARE/roms"
		// We want the root paths, not the system-specific subdirectories
		rootPath := extractROMRoot(expandedPath)
		if rootPath == "" {
			continue
		}

		if _, exists := seen[rootPath]; !exists {
			seen[rootPath] = struct{}{}
			paths = append(paths, rootPath)
		}
	}

	return paths
}

// expandPath expands environment variables and cleans the path.
func expandPath(path string) string {
	if path == "" {
		return ""
	}

	// Expand environment variables like $HOME
	expanded := os.ExpandEnv(path)

	// Handle ~ for home directory
	if strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			expanded = filepath.Join(home, expanded[2:])
		}
	}

	// Clean and normalize
	expanded = filepath.Clean(expanded)

	// Ensure absolute path
	if !filepath.IsAbs(expanded) {
		expanded = defaultROMPath + string(filepath.Separator) + expanded
	}

	return expanded
}

// extractROMRoot extracts the ROM root directory from an ES system path.
// ES paths are typically in format: /path/to/roms or /path/to/roms/systemname
// We want just the parent roms directory.
func extractROMRoot(path string) string {
	if path == "" {
		return ""
	}

	// ES paths can be:
	// 1. Direct ROM folder: /userdata/roms (contains system subdirs)
	// 2. System-specific: /userdata/roms/nes (we want /userdata/roms)
	// 3. External drive: /media/SHARE/roms/nes (we want /media/SHARE/roms)

	// Look for "roms" in the path and return up to and including it
	parts := strings.Split(path, string(filepath.Separator))
	for i, part := range parts {
		if strings.EqualFold(part, "roms") {
			// Return path up to and including "roms"
			// Preserve leading separator for absolute paths
			result := filepath.Join(parts[:i+1]...)
			if filepath.IsAbs(path) && !filepath.IsAbs(result) {
				result = string(filepath.Separator) + result
			}
			return result
		}
	}

	// If no "roms" directory found, return parent directory
	// (assumes path is something like /media/SHARE/games/nes)
	return filepath.Dir(path)
}
