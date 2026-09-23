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

package ssgenre

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
)

func TestSourceGenresAreCanonical(t *testing.T) {
	t.Parallel()
	for name, values := range sourceGenres {
		for _, v := range values {
			assert.Truef(t, tags.IsCanonicalValue(tags.TagTypeGenre, v),
				"%q maps to %q, which is not a canonical genre", name, v)
		}
	}
}

func TestSourceGenreKeysDoNotCollide(t *testing.T) {
	t.Parallel()
	seen := make(map[string]string, len(sourceGenres))
	for name, values := range sourceGenres {
		key := lookupKey(name)
		if other, ok := seen[key]; ok {
			assert.Equalf(t, withParents(sourceGenres[other]), withParents(values),
				"%q and %q share a key but map differently", name, other)
		}
		seen[key] = name
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		want     []tags.TagValue
		unmapped []string
	}{
		{name: "empty", raw: "  "},
		{name: "plain", raw: "Sports", want: []tags.TagValue{"sports"}},
		{name: "case and punctuation", raw: "shoot em up", want: []tags.TagValue{"shmup"}},
		{name: "subgenre adds parent", raw: "Platform", want: []tags.TagValue{"action:platformer", "action"}},
		{
			name: "pack hierarchy",
			raw:  "Shoot'em Up / Vertical/Shoot'em Up",
			want: []tags.TagValue{"shmup:v", "shmup"},
		},
		{
			name: "gamelist hyphen join",
			raw:  "Sports / Football (American)-Sports",
			want: []tags.TagValue{"sports:football", "sports"},
		},
		{
			name: "comma inside a name",
			raw:  "Racing, Driving/Racing TPV",
			want: []tags.TagValue{"racing"},
		},
		{
			name: "comma separated list",
			raw:  "Platform, Puzzle",
			want: []tags.TagValue{"action:platformer", "action", "puzzle"},
		},
		{
			name: "several names in one field",
			raw:  "Casino/Casino / Cards",
			want: []tags.TagValue{"parlor", "board:cards", "board"},
		},
		{
			name: "longest names win over a greedy first match",
			raw:  "Action/Platform / Run & Jump/Platform",
			want: []tags.TagValue{"action", "action:platformer"},
		},
		{
			name: "repeated parent before its child",
			raw:  "Racing, Driving/Racing, Driving / Boat",
			want: []tags.TagValue{"racing"},
		},
		{name: "hyphen inside a name", raw: "Puzzle-Game", want: []tags.TagValue{"puzzle"}},
		{name: "role playing", raw: "Role Playing Game", want: []tags.TagValue{"rpg"}},
		{
			name: "action adventure",
			raw:  "Action / Adventure",
			want: []tags.TagValue{"action", "adventure"},
		},
		{name: "lightgun", raw: "Lightgun Shooter", want: []tags.TagValue{"shooting:gallery", "shooting"}},
		{name: "pinball", raw: "Pinball", want: []tags.TagValue{"parlor:pinball", "parlor"}},
		{name: "rhythm", raw: "Rhythm / Music", want: []tags.TagValue{"rhythm"}},
		{name: "strategy", raw: "Strategy", want: []tags.TagValue{"sim:strategy", "sim"}},
		{name: "dropped on purpose", raw: "Compilation"},
		{name: "dropped part", raw: "Various/Compilation"},
		{name: "unknown", raw: "Gardening", unmapped: []string{"Gardening"}},
		{
			name:     "unknown part keeps the known part",
			raw:      "Platform/Gardening",
			want:     []tags.TagValue{"action:platformer", "action"},
			unmapped: []string{"Gardening"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, unmapped := Lookup(tt.raw)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.unmapped, unmapped)
			for _, v := range got {
				assert.NoError(t, tags.ValidateTagValue(tags.TagTypeGenre, string(v)))
			}
		})
	}
}

func FuzzLookup(f *testing.F) {
	for _, seed := range []string{
		"Shoot'em Up / Vertical/Shoot'em Up", "Racing, Driving", "a/b,c-d;e|f", "", "////", "-",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		values, _ := Lookup(raw)
		for _, v := range values {
			if err := tags.ValidateTagValue(tags.TagTypeGenre, string(v)); err != nil {
				t.Fatalf("Lookup(%q) returned %v", raw, err)
			}
		}
	})
}

// TestLookupRealGamelistGenres pins genre strings an ES-DE library on a real
// desktop produced that the first version of the table dropped. They are
// written as "Parent-Child", so each also exercises the regrouping.
func TestLookupRealGamelistGenres(t *testing.T) {
	t.Parallel()
	tests := map[string][]tags.TagValue{
		"Shooter-1st person": {tags.TagGameGenreShootingFPS, tags.TagGameGenreShooting},
		"Shooter-3rd person": {tags.TagGameGenreShootingTPS, tags.TagGameGenreShooting},
		"Sports-Soccer":      {tags.TagGameGenreSportsSoccer, tags.TagGameGenreSports},
		"Music and Dance":    {tags.TagGameGenreRhythmDance, tags.TagGameGenreRhythm},
		"Role playing games": {tags.TagGameGenreRPG},
		"Action-Fight":       {tags.TagGameGenreAction, tags.TagGameGenreFighting},
		"Fighting-2D":        {tags.TagGameGenreFighting},
		"Race-Motorcycle":    {tags.TagGameGenreRacing},
	}
	for raw, want := range tests {
		got, unmapped := Lookup(raw)
		assert.ElementsMatch(t, want, got, raw)
		assert.Empty(t, unmapped, raw)
	}
}
