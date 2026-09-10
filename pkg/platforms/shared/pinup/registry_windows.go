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
package pinup

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// progIDCLSID reads the ProgID's CLSID, checking the 32-bit view as well as the
// default one. A 64-bit process sees only the 64-bit view of HKEY_CLASSES_ROOT,
// so a PinUP Player that registered its COM server 32-bit is invisible here and
// the LocalServer32 lookup below never gets a CLSID to try.
func progIDCLSID() string {
	const path = `PinUpPlayer.PinDisplay\CLSID`
	for _, access := range []uint32{
		registry.QUERY_VALUE,
		registry.QUERY_VALUE | registry.WOW64_32KEY,
	} {
		key, err := registry.OpenKey(registry.CLASSES_ROOT, path, access)
		if err != nil {
			continue
		}
		clsid, _, valErr := key.GetStringValue("")
		_ = key.Close()
		if valErr == nil && clsid != "" {
			return clsid
		}
	}
	return ""
}

// LocateFromRegistry finds the PinUP System folder through the COM server
// PinUP Player registers: the PinUpPlayer.PinDisplay ProgID names a CLSID
// whose LocalServer32 value is the path of PinUpPlayer.exe inside the
// install. Both the native and the 32-bit (WOW6432Node) views are checked
// because older PinUP Player builds were 32-bit.
func LocateFromRegistry() (string, error) {
	clsid := progIDCLSID()
	if clsid == "" {
		return "", errors.New("PinUpPlayer.PinDisplay has no CLSID")
	}

	for _, path := range []string{
		`CLSID\` + clsid + `\LocalServer32`,
		`WOW6432Node\CLSID\` + clsid + `\LocalServer32`,
	} {
		key, openErr := registry.OpenKey(registry.CLASSES_ROOT, path, registry.QUERY_VALUE)
		if openErr != nil {
			continue
		}
		value, _, valErr := key.GetStringValue("")
		_ = key.Close()
		if valErr != nil || value == "" {
			continue
		}
		if dir := InstallDirFromServerPath(value); dir != "" {
			return filepath.Clean(dir), nil
		}
	}
	return "", errors.New("PinUpPlayer COM server path not registered")
}
