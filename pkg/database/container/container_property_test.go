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

package container_test

import (
	"path"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// referenceSelectLaunchMedia is the rule as written over a whole slice. The
// streaming selector must agree with it on every input.
func referenceSelectLaunchMedia(rows []database.Media) *database.Media {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) == 1 {
		return &rows[0]
	}
	single := func(ext string) *database.Media {
		var match *database.Media
		for i := range rows {
			if container.MediaExt(rows[i].Path) != ext {
				continue
			}
			if match != nil {
				return nil
			}
			match = &rows[i]
		}
		return match
	}
	othersAllowed := func(dbid int64, allowed map[string]bool) bool {
		for i := range rows {
			if rows[i].DBID != dbid && !allowed[container.MediaExt(rows[i].Path)] {
				return false
			}
		}
		return true
	}
	cueCompanions := map[string]bool{
		".bin": true, ".wav": true, ".mp3": true, ".ogg": true, ".flac": true, ".ape": true,
	}
	m3uCompanions := map[string]bool{".cue": true, ".chd": true, ".iso": true}
	for ext := range cueCompanions {
		m3uCompanions[ext] = true
	}
	discSet := map[string]bool{".cue": true, ".chd": true, ".iso": true, ".bin": true, ".img": true, ".pbp": true}

	if m3u := single(".m3u"); m3u != nil && othersAllowed(m3u.DBID, m3uCompanions) {
		return m3u
	}
	if cue := single(".cue"); cue != nil && othersAllowed(cue.DBID, cueCompanions) {
		return cue
	}
	title := rows[0].MediaTitleDBID
	if title <= 0 {
		return nil
	}
	lowest := &rows[0]
	for i := range rows {
		if rows[i].MediaTitleDBID != title || !discSet[container.MediaExt(rows[i].Path)] {
			return nil
		}
		if rows[i].Path < lowest.Path || (rows[i].Path == lowest.Path && rows[i].DBID < lowest.DBID) {
			lowest = &rows[i]
		}
	}
	return lowest
}

var propertyExts = []string{".m3u", ".cue", ".bin", ".chd", ".iso", ".img", ".pbp", ".wav", ".sav", ".txt", ""}

func genDirRows(t *rapid.T, dir string, firstDBID int64) []database.Media {
	n := rapid.IntRange(0, 6).Draw(t, "n")
	rows := make([]database.Media, 0, n)
	for i := range n {
		name := rapid.SampledFrom([]string{"a", "b", "Disc 1", "Disc 2", "game"}).Draw(t, "name")
		ext := rapid.SampledFrom(propertyExts).Draw(t, "ext")
		rows = append(rows, database.Media{
			DBID:           firstDBID + int64(i),
			MediaTitleDBID: rapid.Int64Range(0, 2).Draw(t, "title"),
			Path:           dir + name + ext,
		})
	}
	return rows
}

func TestPropertyLaunchSelectorMatchesSliceRule(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		rows := genDirRows(t, "/roms/PSX/Game/", 1)

		var sel container.LaunchSelector
		for i := range rows {
			sel.Add(&rows[i])
		}
		want := referenceSelectLaunchMedia(rows)
		got := sel.Result()
		viaSlice := container.SelectLaunchMedia(rows)
		if want == nil {
			require.Nil(t, got)
			require.Nil(t, viaSlice)
			return
		}
		require.NotNil(t, got)
		require.NotNil(t, viaSlice)
		assert.Equal(t, *want, *got)
		assert.Same(t, want, viaSlice, "SelectLaunchMedia must point into the caller's slice")
	})
}

// A WatchedIndex fed every row answers exactly as a full Index does for the
// directories it watches, and says nothing about the rest.
func TestPropertyWatchedIndexMatchesIndex(t *testing.T) {
	t.Parallel()
	dirs := []string{"/roms/", "/roms/PSX/", "/roms/PSX/Game/", "/roms/PSX/Game/Extra/", "/roms/PSX/Other/"}
	rapid.Check(t, func(t *rapid.T) {
		var all []database.Media
		for _, dir := range dirs {
			rows := genDirRows(t, dir, int64(len(all)+1))
			for i := range rows {
				rows[i].IsMissing = rapid.Bool().Draw(t, "missing")
				if rapid.Bool().Draw(t, "explicitParent") {
					rows[i].ParentDir = container.ParentDir(rows[i].Path)
				}
			}
			all = append(all, rows...)
		}
		order := rapid.Permutation(all).Draw(t, "order")
		watched := rapid.SliceOfDistinct(rapid.SampledFrom(dirs), func(d string) string { return d }).
			Draw(t, "watched")

		full := container.NewIndex(order)
		streamed := container.NewWatchedIndex(watched)
		for i := range order {
			streamed.Add(&order[i])
		}
		watchedSet := make(map[string]bool, len(watched))
		for _, dir := range watched {
			watchedSet[dir] = true
		}
		for _, dir := range dirs {
			// Callers pass directories without the trailing slash.
			query := path.Clean(dir)
			if !watchedSet[dir] {
				assert.False(t, streamed.HasMedia(query))
				assert.Nil(t, streamed.Resolve(query))
				continue
			}
			assert.Equal(t, full.HasMedia(query), streamed.HasMedia(query), "HasMedia(%s)", dir)
			want, got := full.Resolve(query), streamed.Resolve(query)
			if want == nil {
				assert.Nil(t, got, "Resolve(%s)", dir)
				continue
			}
			require.NotNil(t, got, "Resolve(%s)", dir)
			assert.Equal(t, *want, *got, "Resolve(%s)", dir)
		}
	})
}

// LaunchTargets fed every row resolves every directory exactly as a full Index.
func TestPropertyLaunchTargetsMatchesIndex(t *testing.T) {
	t.Parallel()
	dirs := []string{"/roms/", "/roms/PSX/", "/roms/PSX/Game/", "/roms/PSX/Game/Extra/", "/roms/PSX/Other/"}
	rapid.Check(t, func(t *rapid.T) {
		var all []database.Media
		for _, dir := range dirs {
			rows := genDirRows(t, dir, int64(len(all)+1))
			for i := range rows {
				rows[i].IsMissing = rapid.Bool().Draw(t, "missing")
				if rapid.Bool().Draw(t, "explicitParent") {
					rows[i].ParentDir = container.ParentDir(rows[i].Path)
				}
			}
			all = append(all, rows...)
		}
		order := rapid.Permutation(all).Draw(t, "order")

		full := container.NewIndex(order)
		streamed := container.NewLaunchTargets()
		for i := range order {
			streamed.Add(&order[i])
		}
		targets := streamed.Map()
		for _, dir := range dirs {
			query := path.Clean(dir)
			want, got := full.Resolve(query), targets.Resolve(query)
			if want == nil {
				assert.Nil(t, got, "Resolve(%s)", dir)
				continue
			}
			require.NotNil(t, got, "Resolve(%s)", dir)
			assert.Equal(t, *want, *got, "Resolve(%s)", dir)
		}
	})
}
