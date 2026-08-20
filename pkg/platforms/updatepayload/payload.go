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
	"path"
	"strings"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
)

// Root maps a repository source directory to its top-level archive and install
// directory. ArchiveRoot is always slash-separated.
type Root struct {
	SourceDir   string
	ArchiveRoot string
}

var rootsByPlatform = map[string][]Root{
	platformids.Batocera: {
		{SourceDir: "cmd/batocera/scripts", ArchiveRoot: "scripts"},
	},
}

// Roots returns a copy of the payload roots configured for platformID.
func Roots(platformID string) []Root {
	roots := rootsByPlatform[platformID]
	return append([]Root(nil), roots...)
}

// RelativeMember returns a clean path below root for an archive member. Archive
// names are slash paths on every host; native separators and path traversal are
// rejected before callers convert the result to a filesystem path.
func RelativeMember(root Root, member string) (string, bool) {
	if root.ArchiveRoot == "" || member == "" || strings.Contains(member, `\`) || path.IsAbs(member) {
		return "", false
	}
	if path.Clean(root.ArchiveRoot) != root.ArchiveRoot || strings.Contains(root.ArchiveRoot, "/") {
		return "", false
	}
	if path.Clean(member) != member {
		return "", false
	}
	prefix := root.ArchiveRoot + "/"
	if !strings.HasPrefix(member, prefix) {
		return "", false
	}
	rel := strings.TrimPrefix(member, prefix)
	if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	for component := range strings.SplitSeq(rel, "/") {
		if component == "" || component == "." || component == ".." {
			return "", false
		}
	}
	return rel, true
}

// IncludeSource reports whether a source-tree entry belongs in a release
// payload. Finder metadata is never part of the runtime payload.
func IncludeSource(relative string) bool {
	if relative == "" {
		return false
	}
	for component := range strings.SplitSeq(strings.ReplaceAll(relative, `\`, "/"), "/") {
		if component == ".DS_Store" {
			return false
		}
	}
	return true
}
