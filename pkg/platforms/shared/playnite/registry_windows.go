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

package playnite

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// uninstallKey is the key Playnite's Inno Setup installer registers itself
// under. The installer is per-user, but a machine-wide entry is checked too.
const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Playnite_is1`

// LocateFromRegistry finds the Playnite folder through the installer's
// uninstall entry. A portable copy registers nothing and is found through
// install_dir instead.
func LocateFromRegistry() (string, error) {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		key, err := registry.OpenKey(root, uninstallKey, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, _, valErr := key.GetStringValue("InstallLocation")
		_ = key.Close()
		if valErr == nil && value != "" {
			return filepath.Clean(value), nil
		}
	}
	return "", errors.New("no Playnite uninstall entry")
}
