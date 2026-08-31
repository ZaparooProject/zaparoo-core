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

// Package container decides when a directory of indexed media collapses to a
// single logical launch target, such as a disc folder holding one cue sheet
// beside its bin tracks. The rule is shared by the MediaDB queries that resolve
// containers from SQL and by scrapers that resolve them from an in-memory media
// index, so both agree on what counts as one game.
package container

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// SelectLaunchMedia returns the single logical launch target for a directory's
// direct media rows, or nil when the set is ambiguous. A lone file is its own
// target; otherwise one m3u playlist or one cue sheet surrounded only by its
// companion files stands in for the set.
func SelectLaunchMedia(rows []database.Media) *database.Media {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) == 1 {
		return &rows[0]
	}

	m3u := singleMediaWithExt(rows, ".m3u")
	if m3u != nil && allOtherExtsMatch(rows, m3u.DBID, isM3UCompanionExt) {
		return m3u
	}

	cue := singleMediaWithExt(rows, ".cue")
	if cue != nil && allOtherExtsMatch(rows, cue.DBID, isCueCompanionExt) {
		return cue
	}

	return nil
}

func singleMediaWithExt(rows []database.Media, ext string) *database.Media {
	var match *database.Media
	for i := range rows {
		if MediaExt(rows[i].Path) != ext {
			continue
		}
		if match != nil {
			return nil
		}
		match = &rows[i]
	}
	return match
}

func allOtherExtsMatch(rows []database.Media, mediaDBID int64, allowed func(string) bool) bool {
	for i := range rows {
		if rows[i].DBID == mediaDBID {
			continue
		}
		if !allowed(MediaExt(rows[i].Path)) {
			return false
		}
	}
	return true
}

// MediaExt returns the lowercased extension of a slash-separated media path,
// including the leading dot, or an empty string when there is none.
func MediaExt(mediaPath string) string {
	name := mediaPath
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return strings.ToLower(name[idx:])
	}
	return ""
}

func isCueCompanionExt(ext string) bool {
	switch ext {
	case ".bin", ".wav", ".mp3", ".ogg", ".flac", ".ape":
		return true
	default:
		return false
	}
}

func isM3UCompanionExt(ext string) bool {
	if isCueCompanionExt(ext) {
		return true
	}
	switch ext {
	case ".cue", ".chd", ".iso":
		return true
	default:
		return false
	}
}
