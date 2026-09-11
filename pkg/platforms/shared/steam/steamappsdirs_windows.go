//go:build windows

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
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// registrySteamPaths returns Steam install roots recorded in the registry.
// HKCU holds the path for the logged-in user, which is where a per-user
// install lands; the HKLM keys cover machine-wide installs.
func registrySteamPaths() []string {
	lookups := []struct {
		path  string
		value string
		key   registry.Key
	}{
		{`SOFTWARE\Valve\Steam`, "SteamPath", registry.CURRENT_USER},
		{`SOFTWARE\Wow6432Node\Valve\Steam`, "InstallPath", registry.LOCAL_MACHINE},
		{`SOFTWARE\Valve\Steam`, "InstallPath", registry.LOCAL_MACHINE},
	}

	paths := make([]string, 0, len(lookups))
	for _, l := range lookups {
		key, err := registry.OpenKey(l.key, l.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, _, valErr := key.GetStringValue(l.value)
		_ = key.Close()
		if valErr != nil || value == "" {
			continue
		}
		// Steam writes SteamPath with forward slashes and a lowercase drive
		// letter, so normalize before it reaches filepath.Join.
		paths = append(paths, filepath.Clean(filepath.FromSlash(value)))
	}
	return paths
}

// platformSteamAppsDirs returns default steamapps locations for Windows. The
// registry is consulted first because Steam is routinely installed outside
// Program Files, commonly on a second drive.
func platformSteamAppsDirs(_ string) []string {
	roots := registrySteamPaths()
	roots = append(roots,
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Steam"),
		filepath.Join(os.Getenv("ProgramFiles"), "Steam"),
	)

	seen := make(map[string]struct{}, len(roots))
	dirs := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" || root == "." || !filepath.IsAbs(root) {
			continue
		}
		dir := filepath.Join(root, "steamapps")
		key := strings.ToLower(dir)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		dirs = append(dirs, dir)
	}
	return dirs
}
