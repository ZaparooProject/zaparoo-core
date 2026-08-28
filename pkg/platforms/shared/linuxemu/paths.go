//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linuxemu

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	emuDeckSettingsDir       = ".config/EmuDeck"
	retroDECKConfigDir       = ".var/app/net.retrodeck.retrodeck/config/retrodeck"
	maxIntegrationConfigSize = 1 << 20
	maxProviderSystemDirs    = 1024
)

// EmuDeckPaths holds discovered EmuDeck library paths.
type EmuDeckPaths struct {
	RomsPath     string
	GamelistPath string
}

// RetroDECKPaths holds discovered RetroDECK library paths.
type RetroDECKPaths struct {
	RomsPath     string
	GamelistPath string
}

type retroDECKConfig struct {
	Paths map[string]string `json:"paths"`
}

// DefaultEmuDeckPaths discovers EmuDeck's configured ROM root and standard
// ES-DE gamelist directory without executing its shell settings file.
func DefaultEmuDeckPaths(homeDir string) EmuDeckPaths {
	if homeDir == "" {
		return EmuDeckPaths{}
	}
	romsPath := filepath.Join(homeDir, "Emulation", "roms")
	settingsPath := filepath.Join(homeDir, filepath.FromSlash(emuDeckSettingsDir), "settings.sh")
	if data, err := readLimitedFile(settingsPath); err == nil {
		if configured := parseShellPathAssignment(string(data), "romsPath", homeDir); configured != "" {
			romsPath = configured
		}
	}
	return EmuDeckPaths{
		RomsPath:     romsPath,
		GamelistPath: filepath.Join(homeDir, "ES-DE", "gamelists"),
	}
}

// DefaultRetroDECKPaths discovers RetroDECK's configurable userdata paths from
// its Flatpak JSON config, falling back to documented defaults.
func DefaultRetroDECKPaths(homeDir string) RetroDECKPaths {
	if homeDir == "" {
		return RetroDECKPaths{}
	}
	homePath := filepath.Join(homeDir, "retrodeck")
	romsPath := filepath.Join(homePath, "roms")
	configPath := filepath.Join(homeDir, filepath.FromSlash(retroDECKConfigDir), "retrodeck.json")
	if data, err := readLimitedFile(configPath); err == nil {
		var parsed retroDECKConfig
		if json.Unmarshal(data, &parsed) == nil {
			paths := parsed.Paths
			if validConfiguredPath(paths["rd_home_path"], homeDir) {
				homePath = filepath.Clean(paths["rd_home_path"])
			}
			if validConfiguredPath(paths["roms_path"], homeDir) {
				romsPath = filepath.Clean(paths["roms_path"])
			} else {
				romsPath = filepath.Join(homePath, "roms")
			}
		}
	}
	return RetroDECKPaths{
		RomsPath:     romsPath,
		GamelistPath: filepath.Join(homePath, "ES-DE", "gamelists"),
	}
}

func readProviderSystemFolders(path string) ([]string, error) {
	//nolint:gosec // Path is a discovered per-user provider ROM root.
	directory, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open provider ROM directory: %w", err)
	}
	defer directory.Close() //nolint:errcheck // Read-only directory handle.

	result := make([]string, 0, 64)
	for {
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil, fmt.Errorf("stat provider ROM entry %q: %w", entry.Name(), infoErr)
			}
			if !info.Mode().IsDir() {
				continue
			}
			if len(result) >= maxProviderSystemDirs {
				return nil, errors.New("provider ROM directory exceeds system limit")
			}
			result = append(result, entry.Name())
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read provider ROM directory: %w", readErr)
		}
	}
	sort.Strings(result)
	return result, nil
}

func readLimitedFile(path string) ([]byte, error) {
	//nolint:gosec // Callers provide fixed per-user integration config paths.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open integration config: %w", err)
	}
	defer file.Close() //nolint:errcheck // Read-only file; close errors do not affect parsed data.
	data, err := io.ReadAll(io.LimitReader(file, maxIntegrationConfigSize+1))
	if err != nil {
		return nil, fmt.Errorf("read integration config: %w", err)
	}
	if len(data) > maxIntegrationConfigSize {
		return nil, errors.New("integration config exceeds size limit")
	}
	return data, nil
}

func parseShellPathAssignment(contents, key, homeDir string) string {
	for line := range strings.SplitSeq(contents, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "export ")
		name, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		value = strings.ReplaceAll(value, "${HOME}", homeDir)
		value = strings.ReplaceAll(value, "$HOME", homeDir)
		if strings.HasPrefix(value, "~/") {
			value = filepath.Join(homeDir, value[2:])
		}
		if strings.ContainsAny(value, "$`\n\r") || !validConfiguredPath(value, homeDir) {
			return ""
		}
		return filepath.Clean(value)
	}
	return ""
}

func validConfiguredPath(path, homeDir string) bool {
	if path == "" || strings.ContainsAny(path, "\x00\n\r") {
		return false
	}
	if !filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	return clean != string(filepath.Separator) && clean != filepath.Clean(homeDir)
}
