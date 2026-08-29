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
	"strconv"
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
	EmulationPath string
	RomsPath      string
	ToolsPath     string
	BiosPath      string
	SavesPath     string
	StoragePath   string
	GamelistPath  string
}

// RetroDECKPaths holds discovered RetroDECK library paths.
type RetroDECKPaths struct {
	HomePath     string
	RomsPath     string
	SavesPath    string
	StatesPath   string
	BiosPath     string
	StoragePath  string
	GamelistPath string
}

type retroDECKConfig struct {
	Paths map[string]string `json:"paths"`
}

// DefaultEmuDeckPaths discovers EmuDeck's independently configurable data
// roots without executing its shell settings file.
func DefaultEmuDeckPaths(homeDir string) EmuDeckPaths {
	if homeDir == "" {
		return EmuDeckPaths{}
	}
	paths := EmuDeckPaths{EmulationPath: filepath.Join(homeDir, "Emulation")}
	settingsPath := filepath.Join(homeDir, filepath.FromSlash(emuDeckSettingsDir), "settings.sh")
	contents := ""
	if data, err := readLimitedFile(settingsPath); err == nil {
		contents = string(data)
		if configured := parseShellPathAssignment(contents, "emulationPath", homeDir); configured != "" {
			paths.EmulationPath = configured
		}
	}
	paths.RomsPath = configuredShellPath(contents, "romsPath", homeDir, filepath.Join(paths.EmulationPath, "roms"))
	paths.ToolsPath = configuredShellPath(contents, "toolsPath", homeDir, filepath.Join(paths.EmulationPath, "tools"))
	paths.BiosPath = configuredShellPath(contents, "biosPath", homeDir, filepath.Join(paths.EmulationPath, "bios"))
	paths.SavesPath = configuredShellPath(contents, "savesPath", homeDir, filepath.Join(paths.EmulationPath, "saves"))
	paths.StoragePath = configuredShellPath(
		contents, "storagePath", homeDir, filepath.Join(paths.EmulationPath, "storage"),
	)
	paths.GamelistPath = filepath.Join(homeDir, "ES-DE", "gamelists")
	return paths
}

func configuredShellPath(contents, key, homeDir, fallback string) string {
	if configured := parseShellPathAssignment(contents, key, homeDir); configured != "" {
		return configured
	}
	return fallback
}

// DefaultRetroDECKPaths discovers RetroDECK's independently configurable
// userdata paths from its Flatpak JSON config, falling back to documented defaults.
func DefaultRetroDECKPaths(homeDir string) RetroDECKPaths {
	if homeDir == "" {
		return RetroDECKPaths{}
	}
	configured := make(map[string]string)
	configPath := filepath.Join(homeDir, filepath.FromSlash(retroDECKConfigDir), "retrodeck.json")
	if data, err := readLimitedFile(configPath); err == nil {
		var parsed retroDECKConfig
		if json.Unmarshal(data, &parsed) == nil {
			configured = parsed.Paths
		}
	}
	homePath := configuredJSONPath(configured, "rd_home_path", homeDir, filepath.Join(homeDir, "retrodeck"))
	return RetroDECKPaths{
		HomePath:     homePath,
		RomsPath:     configuredJSONPath(configured, "roms_path", homeDir, filepath.Join(homePath, "roms")),
		SavesPath:    configuredJSONPath(configured, "saves_path", homeDir, filepath.Join(homePath, "saves")),
		StatesPath:   configuredJSONPath(configured, "states_path", homeDir, filepath.Join(homePath, "states")),
		BiosPath:     configuredJSONPath(configured, "bios_path", homeDir, filepath.Join(homePath, "bios")),
		StoragePath:  configuredJSONPath(configured, "storage_path", homeDir, filepath.Join(homePath, "storage")),
		GamelistPath: filepath.Join(homePath, "ES-DE", "gamelists"),
	}
}

func configuredJSONPath(paths map[string]string, key, homeDir, fallback string) string {
	if validConfiguredPath(paths[key], homeDir) {
		return filepath.Clean(paths[key])
	}
	return fallback
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
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return ""
			}
			value = unquoted
		} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
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
	if clean == string(filepath.Separator) || clean == filepath.Clean(homeDir) {
		return false
	}
	resolved, existingRoot, ok := resolvedPathFromExistingAncestor(clean)
	if !ok || existingRoot == string(filepath.Separator) {
		return false
	}
	return resolved != string(filepath.Separator) && resolved != filepath.Clean(homeDir)
}

func hasAncestorResolvingSymlink(candidate string) bool {
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return true
	}
	volume := filepath.VolumeName(absolute)
	current := volume + string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(absolute, current), string(filepath.Separator))
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return false
		}
		if statErr != nil {
			return true
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr != nil {
			return true
		}
		rel, relErr := filepath.Rel(filepath.Clean(resolved), current)
		if relErr != nil || rel == "." || rel == ".." ||
			(!filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

func resolvedPathFromExistingAncestor(candidate string) (resolved, existingRoot string, ok bool) {
	if hasAncestorResolvingSymlink(candidate) {
		return "", "", false
	}
	current := filepath.Clean(candidate)
	var suffix []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			root, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", "", false
			}
			root = filepath.Clean(root)
			return filepath.Clean(filepath.Join(append([]string{root}, suffix...)...)), root, true
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", "", false
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}
