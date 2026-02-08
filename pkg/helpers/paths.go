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

package helpers

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformsshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

// NormalizePathForComparison normalizes a path for cross-platform case-insensitive comparison.
// Converts to forward slashes and lowercases for consistent matching across all platforms.
// This handles paths from databases (forward slashes) vs filepath.Join (OS-specific slashes),
// and ensures case-insensitive matching works for FAT32/exFAT filesystems on all platforms.
func NormalizePathForComparison(path string) string {
	p := filepath.ToSlash(filepath.Clean(path))
	return strings.ToLower(p)
}

// PathHasPrefix checks if path is within root directory, handling separator boundaries correctly.
// This avoids the prefix bug where "c:/roms2/game.bin" would incorrectly match root "c:/roms".
func PathHasPrefix(path, root string) bool {
	normPath := NormalizePathForComparison(path)
	normRoot := NormalizePathForComparison(root)

	// Handle exact match
	if normPath == normRoot {
		return true
	}

	// Handle empty root - only match if both are empty
	if normRoot == "" {
		return false
	}

	// Ensure root ends with separator to avoid "roms" matching "roms2"
	if !strings.HasSuffix(normRoot, "/") {
		normRoot += "/"
	}

	return strings.HasPrefix(normPath, normRoot)
}

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
		// Cache DataDir result
		dataDir := DataDir(pl)
		zaparooMedia := filepath.Join(dataDir, config.MediaDir, l.SystemID)
		if PathHasPrefix(path, zaparooMedia) {
			inDataDir = true
		}
	}

	// check root folder if it's not a generic launcher
	if !inDataDir && len(l.Folders) > 0 {
		inRoot := false
		isAbs := false

		rootDirs := pl.RootDirs(cfg)

		for _, root := range rootDirs {
			if inRoot {
				break
			}
			for _, folder := range l.Folders {
				fullPath := filepath.Join(root, folder)
				if PathHasPrefix(path, fullPath) {
					inRoot = true
					break
				}
			}
		}

		if !inRoot {
			for _, folder := range l.Folders {
				if filepath.IsAbs(folder) {
					if PathHasPrefix(path, folder) {
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

	log.Debug().
		Str("system", systemID).
		Str("path", path).
		Int("launchersChecked", len(launchers)).
		Msg("no launcher matched file")

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

type PathInfo struct {
	Path      string
	Filename  string
	Extension string
	Name      string
}

func GetPathInfo(path string) PathInfo {
	var info PathInfo
	info.Path = path

	// Use custom path parsing to preserve original path format
	// instead of filepath functions which are OS-specific

	// For URIs (containing ://), check if they need special handling
	if strings.Contains(path, "://") {
		// Extract scheme manually to avoid url.Parse dependency
		schemeEnd := strings.Index(path, "://")
		if schemeEnd >= 0 {
			scheme := strings.ToLower(path[:schemeEnd])

			// For custom Zaparoo schemes and standard web schemes, use FilenameFromPath
			// which properly handles URL decoding via ParseVirtualPathStr
			if platformsshared.ShouldDecodeURIScheme(scheme) {
				decodedFilename := FilenameFromPath(path) // URL-decoded filename

				if platformsshared.IsStandardSchemeForDecoding(scheme) {
					// For http/https, only parse extension if there's a path component
					// (URLs without paths like "https://example.com" shouldn't have .com treated as extension)
					info.Filename = decodedFilename
					rest := path[schemeEnd+3:] // Skip "://"
					if strings.Contains(rest, "/") {
						// Has path component - parse extension from filename
						info.Extension = getPathExt(decodedFilename)
						info.Name = strings.TrimSuffix(decodedFilename, info.Extension)
					} else {
						// No path component (bare domain) - no extension
						info.Extension = ""
						info.Name = decodedFilename
					}
				} else {
					// For custom Zaparoo schemes (steam://, kodi-*://, etc.), no extension
					info.Filename = decodedFilename
					info.Extension = ""
					info.Name = decodedFilename
				}
				return info
			}
		}
	}

	// Regular file paths or URIs that don't need decoding
	info.Filename = getPathBase(path)
	info.Extension = getPathExt(path)
	info.Name = strings.TrimSuffix(info.Filename, info.Extension)
	return info
}

// GetPathName returns just the name portion (filename without extension) from a path.
// This is a convenience wrapper around GetPathInfo for use with platforms.DoLaunch.
func GetPathName(path string) string {
	return GetPathInfo(path).Name
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
		log.Debug().Str("path", path).Int("launchersChecked", len(GlobalLauncherCache.GetAllLaunchers())).
			Msg("no launcher matched path")
		return platforms.Launcher{}, errors.New("no launcher found for: " + path)
	}

	// TODO: must be some better logic to picking this!
	launcher := launchers[0]

	if launcher.AllowListOnly && !cfg.IsLauncherFileAllowed(path) {
		return platforms.Launcher{}, errors.New("file not allowed: " + path)
	}

	return launcher, nil
}

// userDirCache caches the result of HasUserDir to avoid repeated filesystem checks
var (
	userDirCache       string
	userDirCacheExists bool
	userDirOnce        sync.Once
)

// HasUserDir checks if a "user" directory exists next to the Zaparoo binary
// and returns true and the absolute path to it. This directory is used as a
// parent for all platform directories if it exists, for a portable install.
// The result is cached after the first call for better performance.
// This function is safe for concurrent use.
func HasUserDir() (string, bool) {
	userDirOnce.Do(func() {
		exeDir := ""
		envExe := os.Getenv(config.AppEnv)
		var err error

		if envExe != "" {
			exeDir = envExe
		} else {
			exeDir, err = os.Executable()
			if err != nil {
				userDirCacheExists = false
				return
			}
		}

		parent := filepath.Dir(exeDir)
		userDir := filepath.Join(parent, config.UserDir)

		info, err := os.Stat(userDir)
		if err != nil {
			userDirCacheExists = false
			return
		}
		if !info.IsDir() {
			userDirCacheExists = false
			return
		}

		// Cache the result
		userDirCache = userDir
		userDirCacheExists = true
	})

	return userDirCache, userDirCacheExists
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
