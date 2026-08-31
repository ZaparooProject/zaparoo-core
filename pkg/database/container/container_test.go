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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func media(dbid int64, path string) database.Media {
	return database.Media{DBID: dbid, Path: path, ParentDir: container.ParentDir(path)}
}

// A row whose Path came from filepath.Join carries the host separator. Before
// this, ParentDir searched only for "/", returned empty, and Resolve then found
// no container at all — folder artwork was dropped on Windows.
func TestParentDirAcceptsHostSeparators(t *testing.T) {
	t.Parallel()

	joined := filepath.Join(string(filepath.Separator), "games", "psx", "Cool Game", "Disc 1.cue")
	parent := container.ParentDir(joined)

	require.NotEmpty(t, parent, "a host-separated path must still yield a parent")
	assert.Equal(t, filepath.ToSlash(filepath.Dir(joined))+"/", parent)
	assert.NotContains(t, parent, "\\", "the parent must be reported forward-slashed")
}

func TestSelectLaunchMedia(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rows  []database.Media
		want  int64
		found bool
	}{
		{
			name: "empty directory",
			rows: nil,
		},
		{
			name:  "lone file is its own target",
			rows:  []database.Media{media(1, "/roms/PSX/Game/Game.chd")},
			want:  1,
			found: true,
		},
		{
			name: "cue sheet with bin tracks",
			rows: []database.Media{
				media(1, "/roms/PSX/Game/Game.cue"),
				media(2, "/roms/PSX/Game/Game (Track 1).bin"),
				media(3, "/roms/PSX/Game/Game (Track 2).bin"),
			},
			want:  1,
			found: true,
		},
		{
			name: "m3u playlist with discs",
			rows: []database.Media{
				media(1, "/roms/PSX/Game/Game.m3u"),
				media(2, "/roms/PSX/Game/Game (Disc 1).chd"),
				media(3, "/roms/PSX/Game/Game (Disc 2).chd"),
			},
			want:  1,
			found: true,
		},
		{
			name: "m3u wins over cue",
			rows: []database.Media{
				media(1, "/roms/PSX/Game/Game.m3u"),
				media(2, "/roms/PSX/Game/Game (Disc 1).cue"),
				media(3, "/roms/PSX/Game/Game (Disc 1).bin"),
			},
			want:  1,
			found: true,
		},
		{
			name: "two cue sheets are ambiguous",
			rows: []database.Media{
				media(1, "/roms/PSX/Game/Disc 1.cue"),
				media(2, "/roms/PSX/Game/Disc 2.cue"),
				media(3, "/roms/PSX/Game/Disc 1.bin"),
			},
		},
		{
			name: "discs without a playlist are ambiguous",
			rows: []database.Media{
				media(1, "/roms/PSX/Game/Game (Disc 1).chd"),
				media(2, "/roms/PSX/Game/Game (Disc 2).chd"),
			},
		},
		{
			name: "unrelated file beside the cue is ambiguous",
			rows: []database.Media{
				media(1, "/roms/PSX/Game/Game.cue"),
				media(2, "/roms/PSX/Game/Bonus.chd"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := container.SelectLaunchMedia(tt.rows)
			if !tt.found {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.DBID)
		})
	}
}

func TestIndexResolvesDiscFolder(t *testing.T) {
	t.Parallel()

	idx := container.NewIndex([]database.Media{
		media(1, "/roms/PSX/Cool Game/Cool Game.cue"),
		media(2, "/roms/PSX/Cool Game/Cool Game.bin"),
		media(3, "/roms/PSX/Other.chd"),
	})

	got := idx.Resolve("/roms/PSX/Cool Game")
	require.NotNil(t, got)
	assert.Equal(t, int64(1), got.DBID)

	assert.Equal(t, got.DBID, idx.Resolve("/roms/PSX/Cool Game/").DBID,
		"a trailing slash must not change the result")
}

func TestIndexRejectsDirectoriesWithNestedMedia(t *testing.T) {
	t.Parallel()

	idx := container.NewIndex([]database.Media{
		media(1, "/roms/PSX/Collection/Cool Game/Cool Game.cue"),
		media(2, "/roms/PSX/Collection/Cool Game/Cool Game.bin"),
	})

	assert.Nil(t, idx.Resolve("/roms/PSX/Collection"),
		"a directory whose media lives in subdirectories is not a container")
	assert.True(t, idx.HasMedia("/roms/PSX/Collection"))

	got := idx.Resolve("/roms/PSX/Collection/Cool Game")
	require.NotNil(t, got)
	assert.Equal(t, int64(1), got.DBID)
}

func TestIndexSkipsMissingMedia(t *testing.T) {
	t.Parallel()

	missing := media(2, "/roms/PSX/Cool Game/Cool Game.bin")
	missing.IsMissing = true
	idx := container.NewIndex([]database.Media{
		media(1, "/roms/PSX/Cool Game/Cool Game.cue"),
		missing,
	})

	got := idx.Resolve("/roms/PSX/Cool Game")
	require.NotNil(t, got)
	assert.Equal(t, int64(1), got.DBID)
}

func TestIndexUnknownDirectory(t *testing.T) {
	t.Parallel()

	idx := container.NewIndex([]database.Media{media(1, "/roms/PSX/Other.chd")})

	assert.Nil(t, idx.Resolve("/roms/PSX/Nothing Here"))
	assert.False(t, idx.HasMedia("/roms/PSX/Nothing Here"))
	assert.Nil(t, idx.Resolve(""))
}

func TestIndexIgnoresVirtualSchemeRoots(t *testing.T) {
	t.Parallel()

	idx := container.NewIndex([]database.Media{
		media(1, "steam://run/12345"),
		media(2, "steam://run/67890"),
	})

	assert.Nil(t, idx.Resolve("steam://"), "a scheme root holding two entries is ambiguous")
	assert.True(t, idx.HasMedia("steam://"))
}

func TestParentDir(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/roms/PSX/", container.ParentDir("/roms/PSX/Game.chd"))
	assert.Equal(t, "steam://", container.ParentDir("steam://run/12345"))
	assert.Empty(t, container.ParentDir("Game.chd"))
}

func TestMediaExt(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ".cue", container.MediaExt("/roms/PSX/Game.CUE"))
	assert.Empty(t, container.MediaExt("/roms/PSX/Game"))
	assert.Empty(t, container.MediaExt("/roms/my.dir/Game"))
}
