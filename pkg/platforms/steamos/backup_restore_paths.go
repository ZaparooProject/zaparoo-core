//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxemu"
)

// PrepareBackupRestore preserves destination storage mappings while restoring
// provider settings. Data follows the destination's active roots rather than
// stale absolute paths captured on another device.
func (*Platform) PrepareBackupRestore() (func(bool) error, error) {
	home := steamOSHomeDir()
	if !validBackupHome(home) {
		return nil, errors.New("home directory could not be resolved for restore")
	}
	emuDeckPaths := linuxemu.DefaultEmuDeckPaths(home)
	retroPaths := destinationRetroDECKPaths(home)
	return func(success bool) error {
		if !success {
			return nil
		}
		return errors.Join(
			rewriteEmuDeckPaths(home, &emuDeckPaths),
			rewriteRetroDECKPaths(home, retroPaths),
		)
	}, nil
}

// shellQuote wraps a value in POSIX single quotes so a sourced settings.sh
// stores the path verbatim. Double quotes would leave $, ` and \ active.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func rewriteEmuDeckPaths(home string, paths *linuxemu.EmuDeckPaths) error {
	path := filepath.Join(home, ".config", "EmuDeck", "settings.sh")
	assignments := map[string]string{
		"emulationPath": paths.EmulationPath,
		"romsPath":      paths.RomsPath,
		"toolsPath":     paths.ToolsPath,
		"biosPath":      paths.BiosPath,
		"savesPath":     paths.SavesPath,
		"storagePath":   paths.StoragePath,
		"ESDEscrapData": filepath.Join(paths.ToolsPath, "downloaded_media"),
	}
	data, mode, err := readRestoreConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading restored EmuDeck paths: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	seen := make(map[string]struct{}, len(assignments))
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		key, _, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		value, ok := assignments[strings.TrimSpace(key)]
		if !ok {
			continue
		}
		prefix := ""
		if strings.HasPrefix(strings.TrimSpace(line), "export ") {
			prefix = "export "
		}
		key = strings.TrimSpace(key)
		lines[i] = prefix + key + "=" + shellQuote(value)
		seen[key] = struct{}{}
	}
	missing := make([]string, 0, len(assignments))
	for key := range assignments {
		if _, ok := seen[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		lines = append(lines, key+"="+shellQuote(assignments[key]))
	}
	return writeRestoreConfig(path, []byte(strings.Join(lines, "\n")), mode)
}

func destinationRetroDECKPaths(home string) map[string]string {
	paths := linuxemu.DefaultRetroDECKPaths(home)
	esdeRoot := filepath.Dir(paths.GamelistPath)
	result := map[string]string{
		"rd_home_path":          paths.HomePath,
		"roms_path":             paths.RomsPath,
		"saves_path":            paths.SavesPath,
		"states_path":           paths.StatesPath,
		"bios_path":             paths.BiosPath,
		"storage_path":          paths.StoragePath,
		"shaders_path":          filepath.Join(paths.HomePath, "shaders"),
		"backups_path":          filepath.Join(paths.HomePath, "backups"),
		"downloaded_media_path": filepath.Join(esdeRoot, "downloaded_media"),
		"themes_path":           filepath.Join(esdeRoot, "themes"),
		"logs_path":             filepath.Join(paths.HomePath, "logs"),
		"screenshots_path":      filepath.Join(paths.HomePath, "screenshots"),
		"mods_path":             filepath.Join(paths.HomePath, "mods"),
		"texture_packs_path":    filepath.Join(paths.HomePath, "texture_packs"),
		"borders_path":          filepath.Join(paths.HomePath, "borders"),
		"cheats_path":           filepath.Join(paths.HomePath, "cheats"),
		"portmaster_path":       filepath.Join(paths.HomePath, "PortMaster"),
		"videos_path":           filepath.Join(paths.HomePath, "videos"),
	}
	configPath := filepath.Join(
		home, ".var", "app", "net.retrodeck.retrodeck", "config", "retrodeck", "retrodeck.json",
	)
	data, _, err := readRestoreConfig(configPath)
	if err != nil {
		return result
	}
	var parsed struct {
		Paths map[string]string `json:"paths"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return result
	}
	for key, value := range parsed.Paths {
		if safeExternalRoot(value, home) {
			result[key] = filepath.Clean(value)
		}
	}
	return result
}

func rewriteRetroDECKPaths(home string, destinationPaths map[string]string) error {
	path := filepath.Join(
		home, ".var", "app", "net.retrodeck.retrodeck", "config", "retrodeck", "retrodeck.json",
	)
	data, mode, err := readRestoreConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading restored RetroDECK paths: %w", err)
	}
	var parsed map[string]any
	if err = json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("parsing restored RetroDECK paths: %w", err)
	}
	rawPaths, ok := parsed["paths"].(map[string]any)
	if !ok {
		return errors.New("restored RetroDECK configuration has no paths object")
	}
	for key, value := range destinationPaths {
		if _, exists := rawPaths[key]; exists {
			rawPaths[key] = value
		}
	}
	updated, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding restored RetroDECK paths: %w", err)
	}
	updated = append(updated, '\n')
	return writeRestoreConfig(path, updated, mode)
}

func readRestoreConfig(path string) (data []byte, mode os.FileMode, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("checking provider configuration: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, errors.New("provider configuration is not a regular file")
	}
	data, err = readLimitedBackupFile(path, 1<<20)
	return data, info.Mode().Perm(), err
}

func writeRestoreConfig(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".zaparoo-provider-paths-*")
	if err != nil {
		return fmt.Errorf("creating provider path temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("writing provider path temporary file: %w", err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing restored provider paths: %w", err)
	}
	dirHandle, err := os.Open(dir) // #nosec G304 -- path is fixed provider configuration parent.
	if err != nil {
		return fmt.Errorf("opening provider path directory: %w", err)
	}
	if syncErr := dirHandle.Sync(); syncErr != nil {
		_ = dirHandle.Close()
		return fmt.Errorf("syncing provider path directory: %w", syncErr)
	}
	if err = dirHandle.Close(); err != nil {
		return fmt.Errorf("closing provider path directory: %w", err)
	}
	return nil
}
