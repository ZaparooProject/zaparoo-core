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

// Package updatepayload describes platform files shipped beside the Core binary
// that must stay in sync when an unmanaged install updates itself.
package updatepayload

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// File is one exact file in a platform update payload. SourcePath uses native
// separators; archive and install paths are slash-separated descendants.
type File struct {
	SourcePath  string
	ArchivePath string
	InstallPath string
	Mode        os.FileMode
}

// RelativeArchivePath validates member and returns its path below archiveRoot.
func RelativeArchivePath(archiveRoot, member string) (string, bool) {
	if !validDescendant(archiveRoot) || member == "" ||
		strings.Contains(member, `\`) || path.IsAbs(member) || path.Clean(member) != member {
		return "", false
	}
	prefix := archiveRoot + "/"
	if !strings.HasPrefix(member, prefix) {
		return "", false
	}
	relative := strings.TrimPrefix(member, prefix)
	if !validDescendant(relative) {
		return "", false
	}
	return relative, true
}

// MatchArchiveFile returns the exact configured file represented by member.
func MatchArchiveFile(files []File, member string) (File, bool) {
	for _, file := range files {
		archiveRoot, _, ok := strings.Cut(file.ArchivePath, "/")
		if !ok || file.ArchivePath != member {
			continue
		}
		if _, ok := RelativeArchivePath(archiveRoot, member); ok && validInstallPath(file.InstallPath) &&
			validMode(file.Mode) {
			return file, true
		}
	}
	return File{}, false
}

// InvalidArchiveMember reports malformed names that claim a configured payload root.
func InvalidArchiveMember(files []File, member string) bool {
	for _, file := range files {
		archiveRoot, _, ok := strings.Cut(file.ArchivePath, "/")
		if !ok || (!strings.HasPrefix(member, archiveRoot+"/") &&
			!strings.HasPrefix(member, archiveRoot+`\`)) {
			continue
		}
		_, valid := RelativeArchivePath(archiveRoot, member)
		return !valid
	}
	return false
}

// ResolveInstallPath resolves a configured path relative to the binary directory.
func ResolveInstallPath(binaryPath, installPath string) (string, bool) {
	if binaryPath == "" || !validInstallPath(installPath) {
		return "", false
	}
	return filepath.Clean(filepath.Join(filepath.Dir(binaryPath), filepath.FromSlash(installPath))), true
}

func validMode(mode os.FileMode) bool {
	return mode.Perm() == 0o644 || mode.Perm() == 0o755
}

func validInstallPath(value string) bool {
	return value != "" && !strings.Contains(value, `\`) && !path.IsAbs(value) && path.Clean(value) == value
}

func validDescendant(value string) bool {
	if value == "" || strings.Contains(value, `\`) || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for component := range strings.SplitSeq(value, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}
