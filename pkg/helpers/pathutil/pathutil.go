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

// Package pathutil provides path resolution utilities with no dependencies on
// other Zaparoo packages. This allows both config and helpers to use these
// functions without circular imports.
package pathutil

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var mediaPathURI = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*)://(.+)$`)

// ExeDir returns the directory containing the currently running executable.
// Returns an empty string if the executable path cannot be determined.
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// CanonicalMediaPath normalizes a persisted media path to media.db's storage
// format. Filesystem paths are cleaned and stored with forward slashes; URI
// paths are kept unchanged.
func CanonicalMediaPath(path string) string {
	if path == "" || mediaPathURI.MatchString(path) {
		return path
	}
	path = strings.ReplaceAll(path, `\`, string(filepath.Separator))
	return filepath.ToSlash(filepath.Clean(path))
}

// ResolveRelativePath resolves a path relative to ExeDir if it is not
// absolute. Absolute and empty paths are returned unchanged.
func ResolveRelativePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	exeDir := ExeDir()
	if exeDir == "" {
		return path
	}
	return filepath.Join(exeDir, path)
}
