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
// companion files stands in for the set. A flat set of disc images also
// collapses when every row belongs to the same known media title.
func SelectLaunchMedia(rows []database.Media) *database.Media {
	var sel LaunchSelector
	for i := range rows {
		sel.addAt(&rows[i], i)
	}
	if _, idx := sel.choice(); idx >= 0 {
		return &rows[idx]
	}
	return nil
}

// LaunchSelector applies SelectLaunchMedia's rule to rows added one at a time
// with a fixed amount of state, so a caller streaming a large system can answer
// for a few directories without holding every row. The zero value is an empty
// directory.
type LaunchSelector struct {
	first, m3u, cue, lowest database.Media

	firstIdx, m3uIdx, cueIdx, lowestIdx int
	count, m3uCount, cueCount           int
	// badForM3U and badForCue count rows that are neither the descriptor nor
	// one of its companions; any one of them stops that descriptor standing in.
	badForM3U, badForCue int
	// discSet holds while every row so far is a disc image of first's title.
	discSet bool
}

// Add records one direct media row of the directory.
func (s *LaunchSelector) Add(row *database.Media) {
	s.addAt(row, s.count)
}

// Result returns what SelectLaunchMedia would return for the rows added so
// far, or nil. The returned row is a copy owned by the selector.
func (s *LaunchSelector) Result() *database.Media {
	row, _ := s.choice()
	return row
}

// Count returns how many rows were added.
func (s *LaunchSelector) Count() int {
	return s.count
}

func (s *LaunchSelector) addAt(row *database.Media, idx int) {
	ext := MediaExt(row.Path)
	if s.count == 0 {
		s.first, s.firstIdx = *row, idx
		s.lowest, s.lowestIdx = *row, idx
		s.discSet = row.MediaTitleDBID > 0
	}
	s.count++

	if ext == ".m3u" {
		s.m3uCount++
		s.m3u, s.m3uIdx = *row, idx
	} else if !isM3UCompanionExt(ext) {
		s.badForM3U++
	}
	if ext == ".cue" {
		s.cueCount++
		s.cue, s.cueIdx = *row, idx
	} else if !isCueCompanionExt(ext) {
		s.badForCue++
	}

	if row.MediaTitleDBID != s.first.MediaTitleDBID || !isDiscSetExt(ext) {
		s.discSet = false
	}
	if row.Path < s.lowest.Path || (row.Path == s.lowest.Path && row.DBID < s.lowest.DBID) {
		s.lowest, s.lowestIdx = *row, idx
	}
}

// choice returns the chosen row and its position in add order, or nil and -1.
func (s *LaunchSelector) choice() (row *database.Media, idx int) {
	switch {
	case s.count == 0:
		return nil, -1
	case s.count == 1:
		return &s.first, s.firstIdx
	case s.m3uCount == 1 && s.badForM3U == 0:
		return &s.m3u, s.m3uIdx
	case s.cueCount == 1 && s.badForCue == 0:
		return &s.cue, s.cueIdx
	case s.discSet:
		return &s.lowest, s.lowestIdx
	default:
		return nil, -1
	}
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

// MayHaveContainerTarget reports whether a media path could sit in a directory
// whose launch target is a different file. Only extensions accepted by the cue,
// playlist, or shared-title disc-set rules qualify, so a caller holding an
// ordinary ROM can skip a container lookup entirely. An .m3u itself is excluded
// because promoting one could only return itself.
func MayHaveContainerTarget(mediaPath string) bool {
	ext := MediaExt(mediaPath)
	return isM3UCompanionExt(ext) || isDiscSetExt(ext)
}

func isDiscSetExt(ext string) bool {
	switch ext {
	case ".cue", ".chd", ".iso", ".bin", ".img", ".pbp":
		return true
	default:
		return false
	}
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
