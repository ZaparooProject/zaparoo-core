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

package tags

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLookupFranchiseResolvesSourceSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want TagValue
	}{
		{raw: "Street Fighter", want: TagSearchFranchiseStreetFighter},
		{raw: "Street Fighter II", want: TagSearchFranchiseStreetFighter},
		{raw: "street fighter alpha", want: TagSearchFranchiseStreetFighter},
		{raw: "Pac-Man", want: TagSearchFranchisePacMan},
		{raw: "Ms. Pac-man", want: TagSearchFranchisePacMan},
		{raw: "Castlevania", want: TagSearchFranchiseCastlevania},
		{raw: "Akumajo Dracula", want: TagSearchFranchiseCastlevania},
		{raw: "Wonder Boy", want: TagSearchFranchiseWonderboy},
		{raw: "19XX", want: TagSearchFranchise19XX},
		{raw: "Ghosts 'n", want: TagSearchFranchiseGhostsNGoblins},
		{raw: "Ghouls 'n Ghosts", want: TagSearchFranchiseGhostsNGoblins},
		{raw: "Muscle Bomber", want: TagSearchFranchiseSlamMasters},
		{raw: "Truxton - Tatsujin", want: TagSearchFranchiseTruxton},
		{raw: "Underconver Cops", want: TagSearchFranchiseUndercoverCops},
		{raw: "X-Men - Marvel", want: TagSearchFranchiseXMen},
		{raw: "Teenage Mutant Ninja Turtles", want: TagSearchFranchiseTeenageMutantNinjaTurtles},
		{raw: "Mega Man", want: TagSearchFranchiseMegaMan},
		{raw: "Rockman", want: TagSearchFranchiseMegaMan},
	} {
		got, ok := LookupFranchise(tc.raw)
		if assert.True(t, ok, "%q should resolve", tc.raw) {
			assert.Equal(t, tc.want, got, "%q", tc.raw)
		}
	}
}

func TestLookupFranchiseRefusesWhatIsNotAFranchise(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"", "Marvel - Capcom", "Moero!!", "Othello",
		// Other search values are not franchises.
		"tate:cw", "feature:mario", "keyword:flip",
	} {
		_, ok := LookupFranchise(raw)
		assert.False(t, ok, "%q must not resolve", raw)
	}
}

func TestEveryFranchiseIsFoundByItsOwnName(t *testing.T) {
	t.Parallel()
	count := 0
	for _, value := range CanonicalTagDefinitions[TagTypeSearch] {
		name, ok := strings.CutPrefix(string(value), "franchise:")
		if !ok {
			continue
		}
		count++
		got, found := LookupFranchise(name)
		if assert.True(t, found, "%s", value) {
			assert.Equal(t, value, got, "an explicit alias shadows the canonical value %s", value)
		}
	}
	assert.Greater(t, count, 3)
}

func TestFranchiseSpellingsDoNotConflict(t *testing.T) {
	t.Parallel()
	seen := make(map[string]TagValue)
	for spelling, value := range franchiseSpellings {
		assert.True(t, IsCanonicalValue(TagTypeSearch, value), "%q maps to %s", spelling, value)
		assert.True(t, strings.HasPrefix(string(value), "franchise:"), "%q maps to %s", spelling, value)
		key := lookupKey(spelling)
		if prior, dup := seen[key]; dup {
			assert.Equal(t, prior, value, "key %q from %q", key, spelling)
		}
		seen[key] = value
	}
}

// TestLookupFranchiseResolvesARealLibrarysFamilies pins every family field an
// ES-DE library on a real desktop carried: each is a game series and must
// resolve, and "Disney", a licensor rather than a series, must not.
func TestLookupFranchiseResolvesARealLibrarysFamilies(t *testing.T) {
	t.Parallel()
	families := []string{
		"Ace Combat", "After Burner", "Animal Crossing", "Ape Escape", "Army Men", "Asphalt", "Astro Boy",
		"Baldur's Gate", "Batman", "Ben 10", "Breath of Fire", "Burnout", "Buzz!", "Cabela's", "Call Of Duty",
		"Capcom VS SNK", "Crash Bandicoot", "Daisenryaku", "Dave Mirra", "Def Jam", "Digimon",
		"Dungeon Explorer", "Ecco the Dolphin", "Everybody's Golf", "F-Zero", "FIFA", "Fatal Fury",
		"Final Fantasy", "GTA", "Get Bass - Sega Bass Fishing", "Ghost'n Goblins", "God Of War", "Hakuoki",
		"Harvest Moon", "Heroes of Might and Magic", "House of the Dead", "Jak And Daxter", "James Bond 007",
		"Kingdom Hearts", "Lemmings", "LittleBigPlanet", "Looney Tunes", "Lord of the Rings", "Macross",
		"Madden NFL", "Mario Kart", "Mario Party", "Marvel Vs. Capcom", "Metal Gear", "Metal Slug",
		"MicroMachines", "Mobile Suit Gundam", "Monaco GP", "Monkey Island", "NASCAR (EA)", "NCAA Football",
		"NFL Blitz", "NFL Quarterback Club", "NHL", "Naruto", "Need for Speed", "PGA Tour Golf",
		"Parasite Eve", "Pikmin", "Pokémon", "Power Stone", "Prince of Persia", "Project DIVA", "Quake",
		"Rayman", "Ready 2 Rumble", "Resident Evil", "SSX", "Saints Row", "Samba de Amigo", "Scooby-doo",
		"Sega Rally", "Sega Worldwide Soccer", "Sonic the Hedgehog", "South Park", "Spectral Souls",
		"Spider-Man", "Star Wars", "Super Smash Bros", "Tekken", "Tenchu", "Test Drive",
		"The King of Fighters", "The Legend of Zelda", "The Simpsons", "The Sims", "Tomb Raider",
		"Tony Hawk", "Valhalla Knights", "Valkyria Chronicles", "Viewtiful Joe", "Virtua Tennis",
		"Warhammer", "Wipeout", "Worms", "Xyanide", "Ys", "Yu-Gi-Oh! Tag Force",
	}
	for _, family := range families {
		value, ok := LookupFranchise(family)
		if assert.True(t, ok, "%q should resolve", family) {
			assert.True(t, IsCanonicalValue(TagTypeSearch, value), "%q resolved to %q", family, value)
		}
	}
	_, ok := LookupFranchise("Disney")
	assert.False(t, ok, "a licensor is not a game series")
}
