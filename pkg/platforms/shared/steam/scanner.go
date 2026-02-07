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
func (*Client) ScanShortcuts(steamDir string) ([]platforms.ScanResult, error) {
	return ScanSteamShortcuts(steamDir)
}

// ScanSteamApps scans official Steam games from the libraryfolders.vdf file.
// steamDir should point to the steamapps directory (e.g., ~/.steam/steam/steamapps).
func ScanSteamApps(steamDir string) ([]platforms.ScanResult, error) {
	var results []platforms.ScanResult

	//nolint:gosec // Safe: reads Steam config files for game library scanning
	f, err := os.Open(filepath.Join(steamDir, "libraryfolders.vdf"))
	if err != nil {
		log.Error().Err(err).Msg("error opening libraryfolders.vdf")
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
	for l, v := range lfs {
		log.Debug().Msgf("library id: %s", l)
		ls, ok := v.(map[string]any)
		if !ok {
			log.Error().Msgf("library %s is not a map", l)
			continue
		}

		libraryPath, ok := ls["path"].(string)
		if !ok {
			log.Error().Msgf("library %s path is not a string", l)
			continue
		}
		steamApps, err := os.ReadDir(filepath.Join(libraryPath, "steamapps"))
		if err != nil {
			log.Error().Err(err).Msg("error listing steamapps folder")
			continue
		}

		var manifestFiles []string
		for _, mf := range steamApps {
			if strings.HasPrefix(mf.Name(), "appmanifest_") {
				manifestFiles = append(manifestFiles, filepath.Join(libraryPath, "steamapps", mf.Name()))
			}
		}

		for _, mf := range manifestFiles {
			log.Debug().Msgf("manifest file: %s", mf)

			//nolint:gosec // Safe: reads Steam manifest files for game library scanning
			af, err := os.Open(mf)
			if err != nil {
				log.Error().Err(err).Msgf("error opening manifest: %s", mf)
				return results, nil
			}

			ap := vdf.NewParser(af)
			am, err := ap.Parse()
			if err != nil {
				if closeErr := af.Close(); closeErr != nil {
					log.Warn().Err(closeErr).Msg("error closing manifest file")
				}
				log.Error().Err(err).Msgf("error parsing manifest: %s", mf)
				return results, nil
			}
			if closeErr := af.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("error closing manifest file")
			}
			am = normalizeVDFKeys(am)

			appState, ok := am["appstate"].(map[string]any)
			if !ok {
				log.Error().Msgf("appstate is not a map in manifest: %s", mf)
				continue
			}

			appID, ok := appState["appid"].(string)
			if !ok {
				log.Error().Msgf("appid is not a string in manifest: %s", mf)
				continue
			}

			appName, ok := appState["name"].(string)
			if !ok {
				log.Error().Msgf("name is not a string in manifest: %s", mf)
				continue
			}

			results = append(results, platforms.ScanResult{
				Path:  virtualpath.CreateVirtualPath("steam", appID, appName),
				Name:  appName,
				NoExt: true,
			})
		}
	}

	return results, nil
}

// ScanSteamShortcuts scans Steam shortcuts (non-Steam games) from the shortcuts.vdf file.
// steamDir should point to the Steam root directory.
func ScanSteamShortcuts(steamDir string) ([]platforms.ScanResult, error) {
	var results []platforms.ScanResult

	log.Debug().Str("steamDir", steamDir).Msg("scanning Steam shortcuts")

	userdataDir := filepath.Join(steamDir, "userdata")
	if _, err := os.Stat(userdataDir); err != nil {
		if os.IsNotExist(err) {
			log.Debug().Str("path", userdataDir).Msg("Steam userdata directory not found")
		} else {
			log.Warn().Err(err).Str("path", userdataDir).Msg("error accessing Steam userdata directory")
		}
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

		shortcutsPath := filepath.Join(userdataDir, userDir.Name(), "config", "shortcuts.vdf")
		if _, err := os.Stat(shortcutsPath); err != nil {
			if os.IsNotExist(err) {
				log.Debug().Str("userId", userDir.Name()).Msg("no shortcuts.vdf for user")
			} else {
				log.Warn().Err(err).Str("path", shortcutsPath).Msg("error accessing shortcuts.vdf")
			}
			continue
		}

		log.Debug().Str("path", shortcutsPath).Msg("reading shortcuts")

		//nolint:gosec // Safe: reads Steam config files for game library scanning
		shortcutsData, err := os.ReadFile(shortcutsPath)
		if err != nil {
			log.Error().Err(err).Msgf("error reading shortcuts.vdf: %s", shortcutsPath)
			continue
		}

		shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(shortcutsData))
		if err != nil {
			log.Error().Err(err).Msgf("error parsing shortcuts.vdf: %s", shortcutsPath)
			continue
		}

		log.Debug().
			Str("userId", userDir.Name()).
			Int("count", len(shortcuts)).
			Msg("parsed shortcuts for user")

		for _, shortcut := range shortcuts {
			if shortcut.AppName == "" {
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

	log.Debug().Int("total", len(results)).Msg("Steam shortcuts scan complete")

	return results, nil
}
