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

package steam

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/vdfbinary"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/andygrunwald/vdf"
	"github.com/rs/zerolog/log"
)

// ScanApps scans Steam library for installed official apps.
// steamDir should point to the steamapps directory (e.g., ~/.steam/steam/steamapps).
func (*Client) ScanApps(steamDir string) ([]platforms.ScanResult, error) {
	return ScanSteamApps(steamDir)
}

// ScanShortcuts scans Steam for non-Steam games (user-added shortcuts).
// steamDir should point to the Steam root directory.
func (c *Client) ScanShortcuts(steamDir string) ([]platforms.ScanResult, error) {
	return scanSteamShortcutsFiltered(steamDir, c.opts.ExcludedShortcutExecutables)
}

// ScanSteamApps scans official Steam games from the libraryfolders.vdf file.
// steamDir should point to the steamapps directory (e.g., ~/.steam/steam/steamapps).
func ScanSteamApps(steamDir string) ([]platforms.ScanResult, error) {
	var results []platforms.ScanResult
	var librariesScanned int
	var librariesSkipped int
	var manifestsFound int
	var manifestsSkipped int

	//nolint:gosec // Safe: reads Steam config files for game library scanning
	f, err := os.Open(filepath.Join(steamDir, "libraryfolders.vdf"))
	if err != nil {
		// Steam not installed at this path (no library file) is expected on many
		// devices; log at Debug to keep it out of Sentry. Mirror ScanSteamShortcuts.
		if os.IsNotExist(err) {
			log.Debug().Err(err).Msg("libraryfolders.vdf not found, skipping Steam app scan")
		} else {
			log.Warn().Err(err).Msg("error opening libraryfolders.vdf")
		}
		return results, nil
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing libraryfolders.vdf")
		}
	}()

	p := vdf.NewParser(f)
	m, err := p.Parse()
	if err != nil {
		log.Error().Err(err).Msg("error parsing libraryfolders.vdf")
		return results, nil
	}
	m = normalizeVDFKeys(m)

	lfs, ok := m["libraryfolders"].(map[string]any)
	if !ok {
		log.Error().Msg("libraryfolders is not a map")
		return results, nil
	}
	for libraryID, value := range lfs {
		log.Debug().Str("libraryID", libraryID).Msg("scanning Steam library")
		library, ok := value.(map[string]any)
		if !ok {
			librariesSkipped++
			log.Warn().Str("libraryID", libraryID).Msg("skipping invalid Steam library entry")
			continue
		}

		libraryPath, ok := library["path"].(string)
		if !ok {
			librariesSkipped++
			log.Warn().Str("libraryID", libraryID).Msg("skipping Steam library without a valid path")
			continue
		}
		steamAppsPath := filepath.Join(libraryPath, "steamapps")
		steamApps, err := os.ReadDir(steamAppsPath)
		if err != nil {
			librariesSkipped++
			log.Warn().
				Err(err).
				Str("libraryID", libraryID).
				Str("libraryPath", libraryPath).
				Msg("skipping unavailable Steam library")
			continue
		}
		librariesScanned++

		var manifestFiles []string
		for _, manifest := range steamApps {
			if strings.HasPrefix(manifest.Name(), "appmanifest_") {
				manifestFiles = append(manifestFiles, filepath.Join(steamAppsPath, manifest.Name()))
			}
		}
		manifestsFound += len(manifestFiles)

		for _, manifestPath := range manifestFiles {
			manifestAppID := strings.TrimSuffix(
				strings.TrimPrefix(filepath.Base(manifestPath), "appmanifest_"), filepath.Ext(manifestPath),
			)
			log.Debug().Str("manifestPath", manifestPath).Str("appID", manifestAppID).
				Msg("reading Steam app manifest")

			//nolint:gosec // Safe: reads Steam manifest files for game library scanning
			manifestFile, err := os.Open(manifestPath)
			if err != nil {
				manifestsSkipped++
				log.Warn().
					Err(err).
					Str("manifestPath", manifestPath).
					Str("appID", manifestAppID).
					Msg("skipping unreadable Steam app manifest")
				continue
			}

			parser := vdf.NewParser(manifestFile)
			manifest, err := parser.Parse()
			if err != nil {
				if closeErr := manifestFile.Close(); closeErr != nil {
					log.Warn().Err(closeErr).Str("manifestPath", manifestPath).
						Msg("error closing Steam app manifest")
				}
				manifestsSkipped++
				log.Warn().
					Err(err).
					Str("manifestPath", manifestPath).
					Str("appID", manifestAppID).
					Msg("skipping invalid Steam app manifest")
				continue
			}
			if closeErr := manifestFile.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Str("manifestPath", manifestPath).
					Msg("error closing Steam app manifest")
			}
			manifest = normalizeVDFKeys(manifest)

			appState, ok := manifest["appstate"].(map[string]any)
			if !ok {
				manifestsSkipped++
				log.Warn().Str("manifestPath", manifestPath).Str("appID", manifestAppID).
					Msg("skipping Steam app manifest without valid appstate")
				continue
			}

			appID, ok := appState["appid"].(string)
			if !ok {
				manifestsSkipped++
				log.Warn().Str("manifestPath", manifestPath).Str("appID", manifestAppID).
					Msg("skipping Steam app manifest without valid appid")
				continue
			}

			appName, ok := appState["name"].(string)
			if !ok {
				manifestsSkipped++
				log.Warn().Str("manifestPath", manifestPath).Str("appID", manifestAppID).
					Msg("skipping Steam app manifest without valid name")
				continue
			}

			results = append(results, platforms.ScanResult{
				Path:  virtualpath.CreateVirtualPath("steam", appID, appName),
				Name:  appName,
				NoExt: true,
			})
		}
	}

	summary := log.Debug()
	if librariesSkipped > 0 || manifestsSkipped > 0 {
		summary = log.Warn()
	}
	summary.
		Str("steamAppsDir", steamDir).
		Int("librariesScanned", librariesScanned).
		Int("librariesSkipped", librariesSkipped).
		Int("manifestsFound", manifestsFound).
		Int("manifestsSkipped", manifestsSkipped).
		Int("results", len(results)).
		Msg("Steam app scan complete")

	return results, nil
}

// NormalizeShortcutExecutable extracts and cleans a shortcut's executable
// without interpreting its display name or remaining arguments.
func NormalizeShortcutExecutable(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) {
		if end := strings.Index(value[1:], `"`); end >= 0 {
			return filepath.Clean(value[1 : end+1])
		}
	}
	if index := strings.IndexByte(value, ' '); index > 0 {
		value = value[:index]
	}
	return filepath.Clean(strings.Trim(value, `"`))
}

// ScanSteamShortcuts scans Steam shortcuts (non-Steam games) from the shortcuts.vdf file.
// steamDir should point to the Steam root directory.
func ScanSteamShortcuts(steamDir string) ([]platforms.ScanResult, error) {
	return scanSteamShortcutsFiltered(steamDir, nil)
}

func scanSteamShortcutsFiltered(steamDir string, excludedExecutables []string) ([]platforms.ScanResult, error) {
	var results []platforms.ScanResult
	excluded := make(map[string]struct{}, len(excludedExecutables))
	for _, executable := range excludedExecutables {
		if executable != "" {
			excluded[filepath.Clean(executable)] = struct{}{}
		}
	}
	var userDirsScanned int
	var shortcutsFilesFound int
	var shortcutFileAccessFailures int
	var shortcutFileReadFailures int
	var shortcutFileParseFailures int
	var parsedShortcuts int
	var skippedBlankNames int
	var skippedExecutables int

	log.Debug().Str("steamDir", steamDir).Msg("scanning Steam shortcuts")

	userdataDir := filepath.Join(steamDir, "userdata")
	if _, err := os.Stat(userdataDir); err != nil {
		if os.IsNotExist(err) {
			log.Debug().Str("path", userdataDir).Msg("Steam userdata directory not found")
		} else {
			log.Warn().Err(err).Str("path", userdataDir).Msg("error accessing Steam userdata directory")
		}
		log.Debug().
			Str("steamDir", steamDir).
			Str("userdataDir", userdataDir).
			Int("userdataEntries", 0).
			Int("userDirsScanned", userDirsScanned).
			Int("shortcutFilesFound", shortcutsFilesFound).
			Int("shortcutFileAccessFailures", shortcutFileAccessFailures).
			Int("shortcutFileReadFailures", shortcutFileReadFailures).
			Int("shortcutFileParseFailures", shortcutFileParseFailures).
			Int("parsedShortcuts", parsedShortcuts).
			Int("skippedBlankNames", skippedBlankNames).
			Int("skippedExecutables", skippedExecutables).
			Int("results", len(results)).
			Msg("Steam shortcuts scan complete")
		return results, nil
	}

	userDirs, err := os.ReadDir(userdataDir)
	if err != nil {
		log.Error().Err(err).Str("path", userdataDir).Msg("error reading Steam userdata directory")
		return results, nil
	}

	log.Debug().Int("count", len(userDirs)).Msg("found Steam user directories")

	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			log.Debug().Str("name", userDir.Name()).Msg("skipping non-directory entry in userdata")
			continue
		}
		userDirsScanned++

		shortcutsPath := filepath.Join(userdataDir, userDir.Name(), "config", "shortcuts.vdf")
		if _, err := os.Stat(shortcutsPath); err != nil {
			if os.IsNotExist(err) {
				log.Debug().Str("userId", userDir.Name()).Msg("no shortcuts.vdf for user")
			} else {
				shortcutFileAccessFailures++
				log.Warn().Err(err).Str("path", shortcutsPath).Msg("error accessing shortcuts.vdf")
			}
			continue
		}
		shortcutsFilesFound++

		log.Debug().Str("path", shortcutsPath).Msg("reading shortcuts")

		//nolint:gosec // Safe: reads Steam config files for game library scanning
		shortcutsData, err := os.ReadFile(shortcutsPath)
		if err != nil {
			shortcutFileReadFailures++
			log.Error().Err(err).Msgf("error reading shortcuts.vdf: %s", shortcutsPath)
			continue
		}

		shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(shortcutsData))
		if err != nil {
			shortcutFileParseFailures++
			log.Error().Err(err).Msgf("error parsing shortcuts.vdf: %s", shortcutsPath)
			continue
		}
		parsedShortcuts += len(shortcuts)

		log.Debug().
			Str("userId", userDir.Name()).
			Int("count", len(shortcuts)).
			Msg("parsed shortcuts for user")

		for _, shortcut := range shortcuts {
			if shortcut.AppName == "" {
				skippedBlankNames++
				continue
			}
			if _, skip := excluded[NormalizeShortcutExecutable(shortcut.Exe)]; skip {
				skippedExecutables++
				continue
			}

			// Non-Steam games require a "Big Picture ID" (BPID) for launching.
			// BPID = (AppID << 32) | 0x02000000
			// This converts the 32-bit shortcut AppID to the 64-bit ID Steam uses for shortcuts.
			bpid := (uint64(shortcut.AppID) << 32) | 0x02000000

			results = append(results, platforms.ScanResult{
				Path:  virtualpath.CreateVirtualPath("steam", strconv.FormatUint(bpid, 10), shortcut.AppName),
				Name:  shortcut.AppName,
				NoExt: true,
			})
		}
	}

	log.Debug().
		Str("steamDir", steamDir).
		Str("userdataDir", userdataDir).
		Int("userdataEntries", len(userDirs)).
		Int("userDirsScanned", userDirsScanned).
		Int("shortcutFilesFound", shortcutsFilesFound).
		Int("shortcutFileAccessFailures", shortcutFileAccessFailures).
		Int("shortcutFileReadFailures", shortcutFileReadFailures).
		Int("shortcutFileParseFailures", shortcutFileParseFailures).
		Int("parsedShortcuts", parsedShortcuts).
		Int("skippedBlankNames", skippedBlankNames).
		Int("skippedExecutables", skippedExecutables).
		Int("results", len(results)).
		Msg("Steam shortcuts scan complete")

	return results, nil
}
