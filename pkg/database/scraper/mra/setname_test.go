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

package mra_test

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/mra"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadSetName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "a plain descriptor",
			body: `<misterromdescription><name>Pac-Man</name><setname>puckman</setname>` +
				`</misterromdescription>`,
			want: "puckman",
		},
		{
			name: "the set name is lower-cased",
			body: `<misterromdescription><setname>PuckMan</setname></misterromdescription>`,
			want: "puckman",
		},
		{
			name: "the embedded rom payload is never read",
			body: `<misterromdescription><setname>puckman</setname><rom index="0"><part>` +
				strings.Repeat("A", mra.MaxHeaderBytes*2) + `</part></rom></misterromdescription>`,
			want: "puckman",
		},
		{
			name: "a repeated set name identifies nothing",
			body: `<misterromdescription><setname>puckman</setname><setname>pacman</setname>` +
				`</misterromdescription>`,
		},
		{
			name: "a descriptor with no set name",
			body: `<misterromdescription><name>Pac-Man</name></misterromdescription>`,
		},
		{
			name: "another document's root",
			body: `<mistergamedescription><setname>puckman</setname></mistergamedescription>`,
		},
		{
			name: "a header that outruns the read bound",
			body: `<misterromdescription><about>` + strings.Repeat("x", mra.MaxHeaderBytes) +
				`</about><setname>puckman</setname></misterromdescription>`,
		},
		{name: "not xml at all", body: "puckman"},
		{
			name: "a set name that is not an identity",
			body: `<misterromdescription><setname>puck man!</setname></misterromdescription>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/game.mra", []byte(tc.body), 0o600))
			assert.Equal(t, tc.want, mra.ReadSetName(fs, "/game.mra"))
		})
	}
}

func TestReadSetNameRejectsWhatIsNotAFile(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/dir.mra", 0o750))
	assert.Empty(t, mra.ReadSetName(fs, "/dir.mra"))
	assert.Empty(t, mra.ReadSetName(fs, "/missing.mra"))
}

func TestSetStem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{in: "./pacman.zip", want: "pacman"},
		{in: `C:\roms\mame\pacman.zip`, want: "pacman"},
		{in: "pacman.7z", want: "pacman"},
		{in: "pacman", want: "pacman"},
		{in: "sf2ce-rev_1", want: "sf2ce-rev_1"},
		// A source bundle may carry paths from another machine; they are read
		// as identities, never opened, so anything that is not a bare set name
		// resolves to nothing.
		{in: "./pacman.nes", want: ""},
		{in: "http://example.com/pacman.zip", want: ""},
		{in: "pac man.zip", want: ""},
		{in: strings.Repeat("a", 129), want: ""},
		{in: "", want: ""},
	} {
		t.Run("stem "+tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, mra.SetStem(tc.in))
		})
	}
}
