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

package helpers

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"
)

// FSHelper provides utilities for filesystem mocking in tests
type FSHelper struct {
	Fs afero.Fs
}

// NewMemoryFS creates a new in-memory filesystem for testing
func NewMemoryFS() *FSHelper {
	return &FSHelper{
		Fs: afero.NewMemMapFs(),
	}
}

// NewOSFS creates a filesystem helper using the real filesystem (for integration tests)
func NewOSFS() *FSHelper {
	return &FSHelper{
		Fs: afero.NewOsFs(),
	}
}

// CreateConfigFile creates a config file with the provided configuration map
func (h *FSHelper) CreateConfigFile(path string, cfg map[string]any) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config to JSON: %w", err)
	}

	// Ensure directory exists
	if err := h.Fs.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create directory for config file: %w", err)
	}

	if err := afero.WriteFile(h.Fs, path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// CreateAuthFile creates an auth file with the provided content
func (h *FSHelper) CreateAuthFile(path string, authData []byte) error {
	// Ensure directory exists
	if err := h.Fs.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create auth directory: %w", err)
	}

	if err := afero.WriteFile(h.Fs, path, authData, 0o600); err != nil {
		return fmt.Errorf("failed to write auth file: %w", err)
	}
	return nil
}

// CreateMediaDirectory creates a media directory structure with sample files
func (h *FSHelper) CreateMediaDirectory(basePath string) error {
	// Create base directory
	if err := h.Fs.MkdirAll(basePath, 0o755); err != nil {
		return fmt.Errorf("failed to create media base directory: %w", err)
	}

	// Create sample system directories
	systems := []string{
		"Atari - 2600",
		"Nintendo - Game Boy",
		"Nintendo - Nintendo Entertainment System",
		"Sega - Master System - Mark III",
		"Sony - PlayStation",
	}

	for _, system := range systems {
		systemPath := filepath.Join(basePath, system)
		if err := h.Fs.MkdirAll(systemPath, 0o755); err != nil {
			return fmt.Errorf("failed to create system directory %s: %w", systemPath, err)
		}

		// Create sample game files for each system
		sampleGames := []string{
			"Game 1.zip",
			"Game 2.zip",
			"Game 3.zip",
		}

		for _, game := range sampleGames {
			gamePath := filepath.Join(systemPath, game)
			// Create empty files
			if err := afero.WriteFile(h.Fs, gamePath, []byte{}, 0o644); err != nil {
				return fmt.Errorf("failed to create game file %s: %w", gamePath, err)
			}
		}
	}

	return nil
}

// CreateDirectoryStructure creates a complex directory structure for testing
func (h *FSHelper) CreateDirectoryStructure(structure map[string]any) error {
	return h.createStructureRecursive("", structure)
}

// createStructureRecursive recursively creates directory structures
func (h *FSHelper) createStructureRecursive(basePath string, structure map[string]any) error {
	for name, content := range structure {
		fullPath := filepath.Join(basePath, name)

		switch v := content.(type) {
		case string:
			// It's a file with content
			if err := h.Fs.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return fmt.Errorf("failed to create directory for file %s: %w", fullPath, err)
			}
			if err := afero.WriteFile(h.Fs, fullPath, []byte(v), 0o644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", fullPath, err)
			}
		case []byte:
			// It's a file with binary content
			if err := h.Fs.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return fmt.Errorf("failed to create directory for binary file %s: %w", fullPath, err)
			}
			if err := afero.WriteFile(h.Fs, fullPath, v, 0o644); err != nil {
				return fmt.Errorf("failed to write binary file %s: %w", fullPath, err)
			}
		case map[string]any:
			// It's a directory with subdirectories/files
			if err := h.Fs.MkdirAll(fullPath, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", fullPath, err)
			}
			if err := h.createStructureRecursive(fullPath, v); err != nil {
				return err
			}
		case nil:
			// It's an empty directory
			if err := h.Fs.MkdirAll(fullPath, 0o755); err != nil {
				return fmt.Errorf("failed to create empty directory %s: %w", fullPath, err)
			}
		}
	}
	return nil
}

// FileExists checks if a file exists
func (h *FSHelper) FileExists(path string) bool {
	exists, err := afero.Exists(h.Fs, path)
	if err != nil {
		return false
	}
	return exists
}

// ReadFile reads a file and returns its content
func (h *FSHelper) ReadFile(path string) ([]byte, error) {
	data, err := afero.ReadFile(h.Fs, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return data, nil
}

// WriteFile writes content to a file
func (h *FSHelper) WriteFile(path string, content []byte, _ int) error {
	// Ensure directory exists
	if err := h.Fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for file %s: %w", path, err)
	}
	if err := afero.WriteFile(h.Fs, path, content, 0o644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}
	return nil
}

// ListFiles lists all files in a directory
func (h *FSHelper) ListFiles(path string) ([]string, error) {
	files, err := afero.ReadDir(h.Fs, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", path, err)
	}

	fileNames := make([]string, len(files))
	for i, file := range files {
		fileNames[i] = file.Name()
	}

	return fileNames, nil
}

// CleanupDir removes all contents from a directory
func (h *FSHelper) CleanupDir(path string) error {
	if err := h.Fs.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to remove directory %s: %w", path, err)
	}
	return nil
}

// SetupTestConfigEnvironment creates a complete test configuration environment
func (h *FSHelper) SetupTestConfigEnvironment(baseDir string) (map[string]any, error) {
	// Create config directory
	configDir := filepath.Join(baseDir, "config")
	if err := h.Fs.MkdirAll(configDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create default config
	cfg := map[string]any{
		"service": map[string]any{
			"api_port": 7497,
		},
		"readers": map[string]any{
			"polling": 500,
		},
		"media_folder": map[string]any{
			"path": filepath.Join(baseDir, "media"),
		},
		"database": map[string]any{
			"path": filepath.Join(baseDir, "database"),
		},
		"platform": map[string]any{
			"name": "test",
		},
	}

	// Create config file
	configPath := filepath.Join(configDir, "config.json")
	if err := h.CreateConfigFile(configPath, cfg); err != nil {
		return nil, err
	}

	// Create media directory structure
	mediaFolder, ok := cfg["media_folder"].(map[string]any)
	if !ok {
		return nil, errors.New("media_folder not found or not a map")
	}
	mediaPath, ok := mediaFolder["path"].(string)
	if !ok {
		return nil, errors.New("media_folder path not found or not a string")
	}
	if err := h.CreateMediaDirectory(mediaPath); err != nil {
		return nil, fmt.Errorf("failed to create media directory: %w", err)
	}

	// Create database directory
	database, ok := cfg["database"].(map[string]any)
	if !ok {
		return nil, errors.New("database not found or not a map")
	}
	dbPath, ok := database["path"].(string)
	if !ok {
		return nil, errors.New("database path not found or not a string")
	}
	if err := h.Fs.MkdirAll(dbPath, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	return cfg, nil
}

// Common test directory structures

// GetBasicTestStructure returns a basic directory structure for testing
func GetBasicTestStructure() map[string]any {
	return map[string]any{
		"config": map[string]any{
			"config.json": `{"service":{"api_bind":"127.0.0.1:7497"}}`,
		},
		"media": map[string]any{
			"Atari - 2600": map[string]any{
				"Pitfall.zip": []byte{0x50, 0x4B}, // ZIP header
				"Combat.zip":  []byte{0x50, 0x4B},
			},
			"Nintendo - Game Boy": map[string]any{
				"Tetris.gb": []byte{0x00, 0xC3}, // Game Boy header
			},
		},
		"database": nil, // Empty directory
		"logs":     nil, // Empty directory
	}
}

// GetComplexTestStructure returns a more complex directory structure for integration testing
func GetComplexTestStructure() map[string]any {
	return map[string]any{
		"config": map[string]any{
			"config.json": `{
				"service": {"api_bind": "127.0.0.1:7497"},
				"readers": {"polling": 500},
				"platform": {"name": "test"}
			}`,
			"auth.json": `{"token": "test-auth-token"}`,
		},
		"media": map[string]any{
			"Atari - 2600": map[string]any{
				"Action": map[string]any{
					"Pitfall! (1982).zip":   []byte{0x50, 0x4B},
					"River Raid (1982).zip": []byte{0x50, 0x4B},
				},
				"Sports": map[string]any{
					"Combat (1977).zip": []byte{0x50, 0x4B},
				},
			},
			"Nintendo - Game Boy": map[string]any{
				"Puzzle": map[string]any{
					"Tetris (1989).gb": []byte{0x00, 0xC3},
				},
				"Action": map[string]any{
					"Metroid II - Return of Samus (1991).gb": []byte{0x00, 0xC3},
				},
			},
			"Sony - PlayStation": map[string]any{
				"RPG": map[string]any{
					"Final Fantasy VII (1997).chd": []byte{0x43, 0x48, 0x44}, // CHD header
				},
			},
		},
		"database": map[string]any{
			"user.db":  nil, // Will be created by tests
			"media.db": nil, // Will be created by tests
		},
		"logs": map[string]any{
			"zaparoo.log": "Test log file content\n",
		},
		"tmp": nil, // Temporary directory
	}
}

// SetupMemoryFilesystem creates a new in-memory filesystem helper with basic directory structure
func SetupMemoryFilesystem() *FSHelper {
	helper := NewMemoryFS()

	// Create basic directory structure
	structure := GetBasicTestStructure()
	if err := helper.CreateDirectoryStructure(structure); err != nil {
		// In testing context, we might want to handle this differently
		// but for now we'll create a minimal structure manually
		_ = helper.Fs.MkdirAll("/config", 0o755)
		_ = helper.Fs.MkdirAll("/media", 0o755)
		_ = helper.Fs.MkdirAll("/database", 0o755)
	}

	return helper
}
