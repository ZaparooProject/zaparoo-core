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

package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/andygrunwald/vdf"
	"github.com/rs/zerolog/log"
)

// AppInfo contains metadata for a Steam app from its manifest.
type AppInfo struct {
	Name       string
	InstallDir string
	AppID      int
}

// LookupAppName finds the name of a Steam app by its AppID.
// Returns the app name and true if found, or empty string and false if not.
// steamAppsDir should point to the steamapps directory (e.g., ~/.steam/steam/steamapps).
func LookupAppName(steamAppsDir string, appID int) (string, bool) {
	info, ok := ReadAppManifest(steamAppsDir, appID)
	if !ok {
		return "", false
	}
	return info.Name, true
}

// ReadAppManifest reads a Steam app manifest and returns its info.
// steamAppsDir should point to the steamapps directory.
func ReadAppManifest(steamAppsDir string, appID int) (AppInfo, bool) {
	manifestPath := filepath.Join(steamAppsDir, fmt.Sprintf("appmanifest_%d.acf", appID))

	//nolint:gosec // Safe: reads Steam manifest files
	f, err := os.Open(manifestPath)
	if err != nil {
		log.Debug().Err(err).Int("appID", appID).Msg("failed to open app manifest")
		return AppInfo{}, false
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing app manifest")
		}
	}()

	p := vdf.NewParser(f)
	m, err := p.Parse()
	if err != nil {
		log.Warn().Err(err).Int("appID", appID).Msg("failed to parse app manifest")
		return AppInfo{}, false
	}

	appState, ok := m["AppState"].(map[string]any)
	if !ok {
		log.Warn().Int("appID", appID).Msg("AppState not found in manifest")
		return AppInfo{}, false
	}

	name, ok := appState["name"].(string)
	if !ok {
		log.Warn().Int("appID", appID).Msg("name not found in manifest")
		return AppInfo{}, false
	}

	installDir, _ := appState["installdir"].(string) //nolint:revive // installdir is optional

	return AppInfo{
		AppID:      appID,
		Name:       name,
		InstallDir: installDir,
	}, true
}

// FindSteamAppsDir finds the steamapps directory from a Steam root directory.
// It checks for both lowercase and mixed-case "steamapps" directories.
func FindSteamAppsDir(steamDir string) string {
	// Common variations of the steamapps directory name
	candidates := []string{
		"steamapps",
		"SteamApps",
		"steam/steamapps",
	}

	for _, candidate := range candidates {
		path := filepath.Join(steamDir, candidate)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}

	// Default fallback
	return filepath.Join(steamDir, "steamapps")
}

// forEachSteamLibrary iterates through all Steam libraries that contain an app.
// It calls the callback with each library's steamapps directory, starting with
// the main steamapps directory. Iteration stops when the callback returns true.
func forEachSteamLibrary(mainSteamAppsDir string, appID int, callback func(steamAppsDir string) bool) {
	// Try main library first
	if callback(mainSteamAppsDir) {
		return
	}

	// Parse libraryfolders.vdf for additional libraries
	libraryFoldersPath := filepath.Join(mainSteamAppsDir, "libraryfolders.vdf")

	//nolint:gosec // Safe: reads Steam config files
	f, err := os.Open(libraryFoldersPath)
	if err != nil {
		log.Debug().Err(err).Msg("failed to open libraryfolders.vdf")
		return
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing libraryfolders.vdf")
		}
	}()

	p := vdf.NewParser(f)
	m, err := p.Parse()
	if err != nil {
		log.Warn().Err(err).Msg("failed to parse libraryfolders.vdf")
		return
	}

	lfs, ok := m["libraryfolders"].(map[string]any)
	if !ok {
		return
	}

	appIDStr := strconv.Itoa(appID)
	for _, v := range lfs {
		ls, ok := v.(map[string]any)
		if !ok {
			continue
		}

		// Check if this library has our app
		apps, ok := ls["apps"].(map[string]any)
		if ok {
			if _, hasApp := apps[appIDStr]; !hasApp {
				continue
			}
		}

		libraryPath, ok := ls["path"].(string)
		if !ok {
			continue
		}

		librarySteamApps := filepath.Join(libraryPath, "steamapps")
		if callback(librarySteamApps) {
			return
		}
	}
}

// LookupAppNameInLibraries searches all Steam library folders for an app.
// steamAppsDir should point to the main steamapps directory.
func LookupAppNameInLibraries(steamAppsDir string, appID int) (string, bool) {
	var result string
	forEachSteamLibrary(steamAppsDir, appID, func(dir string) bool {
		if name, ok := LookupAppName(dir, appID); ok {
			result = name
			return true
		}
		return false
	})
	return result, result != ""
}

// DefaultSteamAppsDirs returns default locations for Steam's steamapps directory.
// These are platform-specific paths where Steam is commonly installed.
func DefaultSteamAppsDirs() []string {
	// Get home directory
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	return []string{
		// Standard Linux locations
		filepath.Join(home, ".steam", "steam", "steamapps"),
		filepath.Join(home, ".local", "share", "Steam", "steamapps"),
		// Steam Deck
		filepath.Join(home, ".steam", "steamapps"),
		// Flatpak
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".steam", "steam", "steamapps"),
	}
}

// FindAppNameByAppID searches common Steam locations for an app's name.
// This is a convenience function that tries multiple locations.
func FindAppNameByAppID(appID int) (string, bool) {
	for _, steamAppsDir := range DefaultSteamAppsDirs() {
		if name, ok := LookupAppNameInLibraries(steamAppsDir, appID); ok {
			return name, true
		}
	}
	return "", false
}

// FindInstallDirByAppID searches common Steam locations for an app's install directory.
// Returns the full path to the game's install directory (e.g., /path/to/steamapps/common/GameName).
func FindInstallDirByAppID(appID int) (string, bool) {
	for _, steamAppsDir := range DefaultSteamAppsDirs() {
		if path, ok := lookupInstallDirInLibraries(steamAppsDir, appID); ok {
			return path, true
		}
	}
	return "", false
}

// lookupInstallDirInLibraries searches all Steam library folders for an app's install directory.
func lookupInstallDirInLibraries(steamAppsDir string, appID int) (string, bool) {
	var result string
	forEachSteamLibrary(steamAppsDir, appID, func(dir string) bool {
		if info, ok := ReadAppManifest(dir, appID); ok && info.InstallDir != "" {
			result = filepath.Join(dir, "common", info.InstallDir)
			return true
		}
		return false
	})
	return result, result != ""
}

// FormatGameName returns a formatted game name for display.
// If the name is found, returns it; otherwise returns "Steam Game {AppID}".
func FormatGameName(appID int, name string) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("Steam Game %d", appID)
}

// ExtractAppIDFromPath extracts an AppID from a Steam virtual path.
// Path format: "steam://[id]/[name]" or "steam://rungameid/[id]"
func ExtractAppIDFromPath(path string) (int, bool) {
	// Remove "steam://" prefix
	path = strings.TrimPrefix(path, "steam://")

	// Handle "rungameid/123" format
	if strings.HasPrefix(path, "rungameid/") {
		idStr := strings.TrimPrefix(path, "rungameid/")
		appID, err := strconv.Atoi(idStr)
		if err != nil {
			return 0, false
		}
		return appID, true
	}

	// Handle "[id]/[name]" format
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 {
		return 0, false
	}

	appID, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return appID, true
}
