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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/andygrunwald/vdf"
	"github.com/rs/zerolog/log"
)

// PathIsLauncher returns true if a given path matches against any of the
// criteria defined in a launcher.
func PathIsLauncher(
	cfg *config.Instance,
	pl platforms.Platform,
	l *platforms.Launcher,
	path string,
) bool {
	if path == "" {
		return false
	}

	lp := strings.ToLower(path)

	// ignore dot files
	if strings.HasPrefix(filepath.Base(lp), ".") {
		return false
	}

	// check uri scheme
	for _, scheme := range l.Schemes {
		if strings.HasPrefix(lp, scheme+":") {
			return true
		}
	}

	// check for data dir media folder
	inDataDir := false
	if l.SystemID != "" {
		zaparooMedia := filepath.Join(DataDir(pl), config.MediaDir, l.SystemID)
		zaparooMedia = strings.ToLower(zaparooMedia)
		if strings.HasPrefix(lp, zaparooMedia) {
			inDataDir = true
		}
	}

	// check root folder if it's not a generic launcher
	if !inDataDir && len(l.Folders) > 0 {
		inRoot := false
		isAbs := false

		for _, root := range pl.RootDirs(cfg) {
			for _, folder := range l.Folders {
				if strings.HasPrefix(lp, strings.ToLower(filepath.Join(root, folder))) {
					inRoot = true
					break
				}
			}
		}

		if !inRoot {
			for _, folder := range l.Folders {
				if filepath.IsAbs(folder) && strings.HasPrefix(lp, strings.ToLower(folder)) {
					isAbs = true
					break
				}
			}
		}

		if !inRoot && !isAbs {
			return false
		}
	}

	// check file extension
	for _, ext := range l.Extensions {
		if strings.HasSuffix(lp, strings.ToLower(ext)) {
			return true
		}
	}

	// finally, launcher's test func
	if l.Test != nil {
		return l.Test(cfg, lp)
	}
	return false
}

// MatchSystemFile returns true if a given path is for a given system.
// This function now uses the launcher cache for O(1) system lookup instead of O(n*m).
func MatchSystemFile(
	cfg *config.Instance,
	pl platforms.Platform,
	systemID string,
	path string,
) bool {
	launchers := GlobalLauncherCache.GetLaunchersBySystem(systemID)
	for i := range launchers {
		if PathIsLauncher(cfg, pl, &launchers[i], path) {
			return true
		}
	}
	return false
}

// PathToLaunchers is a reverse lookup to match a given path against all
// possible launchers in a platform. Returns all matched launchers.
func PathToLaunchers(
	cfg *config.Instance,
	pl platforms.Platform,
	path string,
) []platforms.Launcher {
	var launchers []platforms.Launcher
	allLaunchers := GlobalLauncherCache.GetAllLaunchers()
	for i := range allLaunchers {
		if PathIsLauncher(cfg, pl, &allLaunchers[i], path) {
			launchers = append(launchers, allLaunchers[i])
		}
	}
	return launchers
}

func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	return filepath.Dir(exe)
}

func ScanSteamApps(steamDir string) ([]platforms.ScanResult, error) {
	var results []platforms.ScanResult

	//nolint:gosec // Safe: reads Steam config files for game library scanning
	f, err := os.Open(filepath.Join(steamDir, "libraryfolders.vdf"))
	if err != nil {
		log.Error().Err(err).Msg("error opening libraryfolders.vdf")
		return results, nil
	}

	p := vdf.NewParser(f)
	m, err := p.Parse()
	if err != nil {
		log.Error().Err(err).Msg("error parsing libraryfolders.vdf")
		return results, nil
	}

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
				log.Error().Err(err).Msgf("error parsing manifest: %s", mf)
				return results, nil
			}

			appState, ok := am["AppState"].(map[string]any)
			if !ok {
				log.Error().Msgf("AppState is not a map in manifest: %s", mf)
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
				Path: CreateVirtualPath("steam", appID, appName),
				Name: appName,
			})
		}
	}

	return results, nil
}

type PathInfo struct {
	Path      string
	Base      string
	Filename  string
	Extension string
	Name      string
}

func GetPathInfo(path string) PathInfo {
	var info PathInfo
	info.Path = path
	info.Base = filepath.Dir(path)
	info.Filename = filepath.Base(path)
	info.Extension = filepath.Ext(path)
	info.Name = strings.TrimSuffix(info.Filename, info.Extension)
	return info
}

// FindLauncher takes a path and tries to find the best possible match for a
// launcher, taking into account any allowlist restrictions. Returns the
// launcher to be used.
func FindLauncher(
	cfg *config.Instance,
	pl platforms.Platform,
	path string,
) (platforms.Launcher, error) {
	launchers := PathToLaunchers(cfg, pl, path)
	if len(launchers) == 0 {
		return platforms.Launcher{}, errors.New("no launcher found for: " + path)
	}

	// TODO: must be some better logic to picking this!
	launcher := launchers[0]

	if launcher.AllowListOnly && !cfg.IsLauncherFileAllowed(path) {
		return platforms.Launcher{}, errors.New("file not allowed: " + path)
	}

	return launcher, nil
}

// DoLaunch launches the given path and updates the active media with it if
// it was successful.
func DoLaunch(
	cfg *config.Instance,
	pl platforms.Platform,
	setActiveMedia func(*models.ActiveMedia),
	launcher *platforms.Launcher,
	path string,
) error {
	log.Debug().Msgf("launching with: %v", launcher)

	err := launcher.Launch(cfg, path)
	if err != nil {
		return fmt.Errorf("failed to launch: %w", err)
	}

	systemMeta, err := assets.GetSystemMetadata(launcher.SystemID)
	if err != nil {
		log.Warn().Err(err).Msgf("no system metadata for: %s", launcher.SystemID)
	}

	setActiveMedia(&models.ActiveMedia{
		LauncherID: launcher.ID,
		SystemID:   launcher.SystemID,
		SystemName: systemMeta.Name,
		Name:       GetPathInfo(path).Name,
		Path:       pl.NormalizePath(cfg, path),
	})

	return nil
}

// HasUserDir checks if a "user" directory exists next to the Zaparoo binary
// and returns true and the absolute path to it. This directory is used as a
// parent for all platform directories if it exists, for a portable install.
func HasUserDir() (string, bool) {
	exeDir := ""
	envExe := os.Getenv(config.AppEnv)
	var err error

	if envExe != "" {
		exeDir = envExe
	} else {
		exeDir, err = os.Executable()
		if err != nil {
			return "", false
		}
	}

	parent := filepath.Dir(exeDir)
	userDir := filepath.Join(parent, config.UserDir)

	info, err := os.Stat(userDir)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		return "", false
	}
	return userDir, true
}

func ConfigDir(pl platforms.Platform) string {
	if v, ok := HasUserDir(); ok {
		return v
	}
	return pl.Settings().ConfigDir
}

func DataDir(pl platforms.Platform) string {
	if v, ok := HasUserDir(); ok {
		return v
	}
	return pl.Settings().DataDir
}

var ReURI = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*)://(.+)$`)
