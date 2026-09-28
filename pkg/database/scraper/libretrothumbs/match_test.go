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

package libretrothumbs

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/require"
)

func TestEveryMappedSystemExists(t *testing.T) {
	t.Parallel()
	for _, id := range SupportedSystems() {
		_, err := systemdefs.GetSystem(id)
		require.NoError(t, err, id)
		require.NotEmpty(t, playlists[id], id)
	}
}

func TestMediaBaseName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "3-D WorldRunner (USA)",
		mediaBaseName("source://abc/NES/3-D%20WorldRunner%20%28USA%29.nes"))
	require.Equal(t, "Super Mario 64 (USA)", mediaBaseName("/media/fat/games/N64/Super Mario 64 (USA).z64"))
	require.Equal(t, "Game 100%", mediaBaseName("/roms/Game 100%.zip"))
	require.Equal(t, "Game (Disc 1)", mediaBaseName(`C:\roms\PSX\Game (Disc 1).chd`))
}

func TestThumbnailNameReplacesForbiddenCharacters(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Tom _ Jerry_ The Movie (USA)", thumbnailName("Tom & Jerry: The Movie (USA)"))
}

func TestIndexMatch(t *testing.T) {
	t.Parallel()
	ix := newIndex([]string{
		"3-D WorldRunner (1987-09)(Acclaim)(US)",
		"3-D WorldRunner (1987-09)(Acclaim)(US)[b]",
		"3-D WorldRunner (USA)",
		"Legend of Zelda, The (Europe) (Rev 1)",
		"Legend of Zelda, The (USA)",
		"Legend of Zelda, The (USA) (Beta)",
		"Tom _ Jerry (USA)",
		"Blaster Master (Japan)",
		"Blaster Master (Europe)",
		"1942 (Revision B)",
	})
	cases := []struct {
		file, slug, want string
	}{
		{"3-D WorldRunner (USA)", "", "3-D WorldRunner (USA)"},
		{"3-d worldrunner (usa)", "", "3-D WorldRunner (USA)"},
		{"3-D WorldRunner (U) [!]", "3dworldrunner", "3-D WorldRunner (USA)"},
		{"Zelda", "legendofzelda", "Legend of Zelda, The (USA)"},
		{"Legend of Zelda, The (E)", "legendofzelda", "Legend of Zelda, The (Europe) (Rev 1)"},
		{"Tom & Jerry (USA)", "", "Tom _ Jerry (USA)"},
		{"Blaster Master (Japan)", "", "Blaster Master (Japan)"},
		{"Blaster Master", "blastermaster", "Blaster Master (Europe)"},
		{"1942", "1942", "1942 (Revision B)"},
	}
	for _, c := range cases {
		got, ok := ix.match(c.file, c.slug)
		require.True(t, ok, c.file)
		require.Equal(t, c.want, got, c.file)
	}
	_, ok := ix.match("Unknown Game (USA)", "unknowngame")
	require.False(t, ok)
}
