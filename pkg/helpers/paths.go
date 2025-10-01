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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	valvevdfbinary "github.com/TimDeve/valve-vdf-binary"
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

	// Get base once for dot file check
	base := filepath.Base(lp)

	// ignore dot files
	if base != "" && base[0] == '.' {
		return false
	}

	// check uri scheme
	for _, scheme := range l.Schemes {
		// scheme is already lowercase in launcher definitions
		if strings.HasPrefix(lp, scheme+":") {
			return true
		}
	}

	// check for data dir media folder
	inDataDir := false
	if l.SystemID != "" {
		// Cache DataDir result (now cached internally)
		dataDir := DataDir(pl)
		// Build zaparooMedia path once and lowercase it
		zaparooMedia := strings.ToLower(filepath.Join(dataDir, config.MediaDir, l.SystemID))
		if strings.HasPrefix(lp, zaparooMedia) {
			inDataDir = true
		}
	}

	// check root folder if it's not a generic launcher
	if !inDataDir && len(l.Folders) > 0 {
		inRoot := false
		isAbs := false

		// Get root dirs once and pre-lowercase
		rootDirs := pl.RootDirs(cfg)

		for _, root := range rootDirs {
			lowerRoot := strings.ToLower(root)
			if inRoot {
				break
			}
			for _, folder := range l.Folders {
				// Build full path from pre-lowered parts to avoid repeated ToLower on the whole string
				lowerFolder := strings.ToLower(folder)
				fullPath := filepath.Join(lowerRoot, lowerFolder)
				if strings.HasPrefix(lp, fullPath) {
					inRoot = true
					break
				}
			}
		}

		if !inRoot {
			for _, folder := range l.Folders {
				if filepath.IsAbs(folder) {
					// Lowercase once per absolute folder
					lowerFolder := strings.ToLower(folder)
					if strings.HasPrefix(lp, lowerFolder) {
						isAbs = true
						break
					}
				}
			}
		}

		if !inRoot && !isAbs {
			return false
		}
	}

	// check file extension (if declared)
	if len(l.Extensions) > 0 {
		for _, e := range l.Extensions {
			if strings.HasSuffix(lp, strings.ToLower(e)) {
				return true
			}
		}
		// Extension didn't match - if there's a Test function, let it decide
		if l.Test != nil {
			return l.Test(cfg, lp)
		}
		return false
	}

	// finally, launcher's test func (if no extensions were specified)
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

func ScanSteamShortcuts(steamDir string) ([]platforms.ScanResult, error) {
	var results []platforms.ScanResult

	userdataDir := filepath.Join(steamDir, "userdata")
	if _, err := os.Stat(userdataDir); os.IsNotExist(err) {
		log.Debug().Msg("Steam userdata directory not found")
		return results, nil
	}

	userDirs, err := os.ReadDir(userdataDir)
	if err != nil {
		log.Error().Err(err).Msg("error reading Steam userdata directory")
		return results, nil
	}

	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}

		shortcutsPath := filepath.Join(userdataDir, userDir.Name(), "config", "shortcuts.vdf")
		if _, err := os.Stat(shortcutsPath); os.IsNotExist(err) {
			continue
		}

		log.Debug().Msgf("reading shortcuts from: %s", shortcutsPath)

		//nolint:gosec // Safe: reads Steam config files for game library scanning
		shortcutsData, err := os.ReadFile(shortcutsPath)
		if err != nil {
			log.Error().Err(err).Msgf("error reading shortcuts.vdf: %s", shortcutsPath)
			continue
		}

		shortcuts, err := valvevdfbinary.ParseShortcuts(bytes.NewReader(shortcutsData))
		if err != nil {
			log.Error().Err(err).Msgf("error parsing shortcuts.vdf: %s", shortcutsPath)
			continue
		}

		for _, shortcut := range shortcuts {
			if shortcut.AppName == "" {
				continue
			}

			results = append(results, platforms.ScanResult{
				Path: CreateVirtualPath("steam", fmt.Sprintf("%d", shortcut.AppId), shortcut.AppName),
				Name: shortcut.AppName,
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

	// Use custom path parsing to preserve original path format
	// instead of filepath functions which are OS-specific
	info.Base = getPathDir(path)
	info.Filename = getPathBase(path)
	info.Extension = getPathExt(path)
	info.Name = strings.TrimSuffix(info.Filename, info.Extension)
	return info
}

// getPathDir returns the directory portion of a path, preserving the original separator style
func getPathDir(path string) string {
	if path == "" {
		return "."
	}

	// Remove trailing separators first
	cleanPath := path
	for len(cleanPath) > 1 && (cleanPath[len(cleanPath)-1] == '/' || cleanPath[len(cleanPath)-1] == '\\') {
		cleanPath = cleanPath[:len(cleanPath)-1]
	}

	// Find the last separator (either / or \)
	lastSlash := -1
	for i := len(cleanPath) - 1; i >= 0; i-- {
		if cleanPath[i] == '/' || cleanPath[i] == '\\' {
			lastSlash = i
			break
		}
	}

	if lastSlash == -1 {
		return "."
	}

	if lastSlash == 0 {
		return cleanPath[:1] // Return "/" or "\"
	}

	return cleanPath[:lastSlash]
}

// getPathBase returns the last element of a path
func getPathBase(path string) string {
	if path == "" {
		return "."
	}

	// Find the last separator (either / or \)
	lastSlash := -1
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			lastSlash = i
			break
		}
	}

	if lastSlash == -1 {
		return path
	}

	return path[lastSlash+1:]
}

// getPathExt returns the file extension
func getPathExt(path string) string {
	base := getPathBase(path)

	// Special cases that should return empty extension
	if base == "" || base == "." || base == ".." {
		return ""
	}

	// Find the last dot
	lastDot := strings.LastIndex(base, ".")
	if lastDot == -1 {
		return ""
	}

	// If the dot is at the beginning (hidden file without extension), return empty
	if lastDot == 0 {
		return ""
	}

	return base[lastDot:]
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

	// Handle different lifecycle modes
	switch launcher.Lifecycle {
	case platforms.LifecycleTracked:
		// Launch and store process handle for future stopping
		proc, err := launcher.Launch(cfg, path)
		if err != nil {
			return fmt.Errorf("failed to launch: %w", err)
		}
		// Store process in platform for tracking and later killing
		if proc != nil {
			pl.SetTrackedProcess(proc)
		}
		log.Debug().Msgf("launched tracked process for: %s", path)
	case platforms.LifecycleBlocking:
		// Launch in goroutine to avoid blocking the service
		go func() {
			log.Debug().Msgf("launching blocking process for: %s", path)
			proc, err := launcher.Launch(cfg, path)
			if err != nil {
				log.Error().Err(err).Msgf("blocking launcher failed for: %s", path)
				setActiveMedia(nil)
				return
			}

			// Store process in platform for tracking (blocking processes can also be killed)
			if proc != nil {
				pl.SetTrackedProcess(proc)

				// Wait for process to finish naturally
				_, waitErr := proc.Wait()
				if waitErr != nil {
					log.Debug().Err(waitErr).Msgf("blocking process wait error for: %s", path)
				} else {
					log.Debug().Msgf("blocking process completed for: %s", path)
				}

				// Clear active media when process ends (naturally or killed)
				setActiveMedia(nil)
				log.Debug().Msgf("cleared active media after blocking process ended: %s", path)
			}
		}()
	case platforms.LifecycleFireAndForget:
		// Default behavior - just launch and forget (ignore process)
		_, err := launcher.Launch(cfg, path)
		if err != nil {
			return fmt.Errorf("failed to launch: %w", err)
		}
	}

	systemMeta, err := assets.GetSystemMetadata(launcher.SystemID)
	if err != nil {
		log.Warn().Err(err).Msgf("no system metadata for: %s", launcher.SystemID)
	}

	// Set active media immediately (non-blocking for all lifecycle modes)
	setActiveMedia(&models.ActiveMedia{
		LauncherID: launcher.ID,
		SystemID:   launcher.SystemID,
		SystemName: systemMeta.Name,
		Name:       GetPathInfo(path).Name,
		Path:       pl.NormalizePath(cfg, path),
	})

	return nil
}

// userDirCache caches the result of HasUserDir to avoid repeated filesystem checks
var (
	userDirCache       string
	userDirCacheExists bool
	userDirCacheInit   bool
)

// HasUserDir checks if a "user" directory exists next to the Zaparoo binary
// and returns true and the absolute path to it. This directory is used as a
// parent for all platform directories if it exists, for a portable install.
// The result is cached after the first call for better performance.
func HasUserDir() (string, bool) {
	// Return cached result if already initialized
	if userDirCacheInit {
		return userDirCache, userDirCacheExists
	}

	exeDir := ""
	envExe := os.Getenv(config.AppEnv)
	var err error

	if envExe != "" {
		exeDir = envExe
	} else {
		exeDir, err = os.Executable()
		if err != nil {
			userDirCacheInit = true
			userDirCacheExists = false
			return "", false
		}
	}

	parent := filepath.Dir(exeDir)
	userDir := filepath.Join(parent, config.UserDir)

	info, err := os.Stat(userDir)
	if err != nil {
		userDirCacheInit = true
		userDirCacheExists = false
		return "", false
	}
	if !info.IsDir() {
		userDirCacheInit = true
		userDirCacheExists = false
		return "", false
	}

	// Cache the result
	userDirCache = userDir
	userDirCacheExists = true
	userDirCacheInit = true
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
