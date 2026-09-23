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

import "strings"

// franchiseSpellings maps a series name a source uses, where it differs from
// the canonical name, to its search:franchise value: regional titles
// ("Akumajo Dracula"), later entries sources file as the series ("Street
// Fighter II"), and the MiSTer arcade catalog's own spellings, typos included
// ("Underconver Cops"). Keys are folded by lookupKey.
var franchiseSpellings = map[string]TagValue{
	// Console-era spellings from gamelist family fields.
	"Mario Kart":                   TagSearchFranchiseMario,
	"Mario Party":                  TagSearchFranchiseMario,
	"Ghost'n Goblins":              TagSearchFranchiseGhostsNGoblins,
	"Ecco":                         TagSearchFranchiseEccoTheDolphin,
	"Hot Shots Golf":               TagSearchFranchiseEverybodysGolf,
	"Minna no Golf":                TagSearchFranchiseEverybodysGolf,
	"Garou Densetsu":               TagSearchFranchiseFatalFury,
	"GTA":                          TagSearchFranchiseGrandTheftAuto,
	"Get Bass - Sega Bass Fishing": TagSearchFranchiseSegaBassFishing,
	"Get Bass":                     TagSearchFranchiseSegaBassFishing,
	"Bokujou Monogatari":           TagSearchFranchiseHarvestMoon,
	"Story of Seasons":             TagSearchFranchiseHarvestMoon,
	"The House of the Dead":        TagSearchFranchiseHouseOfTheDead,
	"James Bond 007":               TagSearchFranchiseJamesBond,
	"007":                          TagSearchFranchiseJamesBond,
	"The Lord of the Rings":        TagSearchFranchiseLordOfTheRings,
	"Madden":                       TagSearchFranchiseMaddenNFL,
	"Metal Gear Solid":             TagSearchFranchiseMetalGear,
	"Mobile Suit Gundam":           TagSearchFranchiseGundam,
	"Kidou Senshi Gundam":          TagSearchFranchiseGundam,
	"NASCAR (EA)":                  TagSearchFranchiseNASCAR,
	"PGA Tour Golf":                TagSearchFranchisePGATour,
	"Pokémon":                      TagSearchFranchisePokemon,
	"Pocket Monsters":              TagSearchFranchisePokemon,
	"Hatsune Miku: Project DIVA":   TagSearchFranchiseProjectDiva,
	"Ready 2 Rumble Boxing":        TagSearchFranchiseReady2Rumble,
	"Biohazard":                    TagSearchFranchiseResidentEvil,
	"Sonic the Hedgehog":           TagSearchFranchiseSonic,
	"Super Smash Bros.":            TagSearchFranchiseSuperSmashBros,
	"Dairantou Smash Brothers":     TagSearchFranchiseSuperSmashBros,
	"The King of Fighters":         TagSearchFranchiseKingOfFighters,
	"KOF":                          TagSearchFranchiseKingOfFighters,
	"The Legend of Zelda":          TagSearchFranchiseZelda,
	"Zelda no Densetsu":            TagSearchFranchiseZelda,
	"The Simpsons":                 TagSearchFranchiseSimpsons,
	"The Sims":                     TagSearchFranchiseSims,
	"Tony Hawk's Pro Skater":       TagSearchFranchiseTonyHawk,
	"Senjou no Valkyria":           TagSearchFranchiseValkyriaChronicles,
	"Power Smash":                  TagSearchFranchiseVirtuaTennis,
	"Yu-Gi-Oh!":                    TagSearchFranchiseYuGiOh,
	"Yu-Gi-Oh! Tag Force":          TagSearchFranchiseYuGiOh,
	"Aliens":                       TagSearchFranchiseAlien,
	"Battle Zone":                  TagSearchFranchiseBattlezone,
	"Bradley Trainer":              TagSearchFranchiseBattlezone,
	"Bomber Man":                   TagSearchFranchiseBomberman,
	"Dynablaster":                  TagSearchFranchiseBomberman,
	"Mercs":                        TagSearchFranchiseCommando,
	"Senjou no Ookami":             TagSearchFranchiseCommando,
	"Gryzor":                       TagSearchFranchiseContra,
	"Probotector":                  TagSearchFranchiseContra,
	"Quiz Crayon Shinchan":         TagSearchFranchiseCrayonShinChan,
	"Crayon Shinchan":              TagSearchFranchiseCrayonShinChan,
	"Vampire Savior":               TagSearchFranchiseDarkstalkers,
	"Horror Story":                 TagSearchFranchiseDemonsWorld,
	"Dr. Toppel's Adventure":       TagSearchFranchiseDrToppel,
	"Dungeons and Dragons":         TagSearchFranchiseDungeonsDragons,
	"Dungeons & Dragons":           TagSearchFranchiseDungeonsDragons,
	"Die Hard Arcade":              TagSearchFranchiseDynamiteDeka,
	"Tenchi wo Kurau":              TagSearchFranchiseDynastyWars,
	"Enigma II":                    TagSearchFranchiseEnigma,
	"Engima II":                    TagSearchFranchiseEnigma,
	"Taito Football Champ":         TagSearchFranchiseFootballChamp,
	"Cosmo Police Galivan":         TagSearchFranchiseGalivan,
	"Ghosts 'n":                    TagSearchFranchiseGhostsNGoblins,
	"Ghouls 'n Ghosts":             TagSearchFranchiseGhostsNGoblins,
	"Makaimura":                    TagSearchFranchiseGhostsNGoblins,
	"Nemesis":                      TagSearchFranchiseGradius,
	"Rush'n Attack":                TagSearchFranchiseGreenBeret,
	"Gun & Frontier":               TagSearchFranchiseGunFrontier,
	"High Impact":                  TagSearchFranchiseHighImpactFootball,
	"Jungle Hunt":                  TagSearchFranchiseJungleKing,
	"Pocky & Rocky":                TagSearchFranchiseKiKiKaiKai,
	"Thunder Blaster":              TagSearchFranchiseLethalThunder,
	"Super Mario":                  TagSearchFranchiseMario,
	"Super Mario Bros.":            TagSearchFranchiseMario,
	"Rockman":                      TagSearchFranchiseMegaMan,
	"Chiki Chiki Boys":             TagSearchFranchiseMegaTwins,
	"Ninja-kun":                    TagSearchFranchiseNinjaKid,
	"P.O.W. Prisoners of War":      TagSearchFranchisePOW,
	"Prisoners of War":             TagSearchFranchisePOW,
	"Ms. Pac-Man":                  TagSearchFranchisePacMan,
	"Pang!":                        TagSearchFranchisePang,
	"Buster Bros.":                 TagSearchFranchisePang,
	"Pomping World":                TagSearchFranchisePang,
	"Pipi and Bibis - Whoopee!!":   TagSearchFranchisePipiBibis,
	"Pipi & Bibi's":                TagSearchFranchisePipiBibis,
	"Gouketsuji Ichizoku":          TagSearchFranchisePowerInstinct,
	"Puzzle & Action":              TagSearchFranchisePuzzleAction,
	"Q'bert":                       TagSearchFranchiseQBert,
	"Quatret":                      TagSearchFranchiseQuartet,
	"Robotron 2084":                TagSearchFranchiseRobotron,
	"SAR - Search And Rescue":      TagSearchFranchiseSAR,
	"Search and Rescue":            TagSearchFranchiseSAR,
	"Muscle Bomber":                TagSearchFranchiseSlamMasters,
	"Saturday Night Slam Masters":  TagSearchFranchiseSlamMasters,
	"Street Fighter II":            TagSearchFranchiseStreetFighter,
	"Street Fighter Alpha":         TagSearchFranchiseStreetFighter,
	"Street Fighter Zero":          TagSearchFranchiseStreetFighter,
	"Street Fighter III":           TagSearchFranchiseStreetFighter,
	"Street Fighter EX":            TagSearchFranchiseStreetFighter,
	"Strider Hiryu":                TagSearchFranchiseStrider,
	"Super Champion Basebal":       TagSearchFranchiseSuperChampionBaseball,
	"TMNT":                         TagSearchFranchiseTeenageMutantNinjaTurtles,
	"Teenage Mutant Hero Turtles":  TagSearchFranchiseTeenageMutantNinjaTurtles,
	"Teenage Mutant Ninja Turtles": TagSearchFranchiseTeenageMutantNinjaTurtles,
	"The NewZealand Story":         TagSearchFranchiseTheNewZealandStory,
	"The Next Space":               TagSearchFranchiseTheNextSpace,
	"Tower of Druaga":              TagSearchFranchiseTheTowerOfDruaga,
	"Babylonian Castle Saga":       TagSearchFranchiseTheTowerOfDruaga,
	"The Tower of Druaga":          TagSearchFranchiseTheTowerOfDruaga,
	"Truxton - Tatsujin":           TagSearchFranchiseTruxton,
	"Tatsujin":                     TagSearchFranchiseTruxton,
	"Kyukyoku Tiger":               TagSearchFranchiseTwinCobra,
	"Area 88":                      TagSearchFranchiseUNSquadron,
	"Underconver Cops":             TagSearchFranchiseUndercoverCops,
	"Zhong Guo Long":               TagSearchFranchiseDragonWorld,
	"Knights of Valor":             TagSearchFranchiseKnightsOfValour,
	"Sangoku Senki":                TagSearchFranchiseKnightsOfValour,
	"Xi You Shi E Zhuan":           TagSearchFranchiseOrientalLegend,
	"The Tin Star":                 TagSearchFranchiseTheTinStar,
	"WWF Superstars":               TagSearchFranchiseWWF,
	"WWF WrestleFest":              TagSearchFranchiseWWF,
	"X-Men - Marvel":               TagSearchFranchiseXMen,
	"Akumajo Dracula":              TagSearchFranchiseCastlevania,
	"Akumajou Dracula":             TagSearchFranchiseCastlevania,
	"Monster World":                TagSearchFranchiseWonderboy,
}

// franchiseAliases maps lookupKey(source series name) to a canonical
// search:franchise value. Every canonical franchise is reachable by its own
// name; the explicit entries cover other spellings sources use.
var franchiseAliases = withCanonicalKeys(TagTypeSearch, foldKeys(franchiseSpellings))

// foldKeys rekeys a spelling table by lookupKey.
func foldKeys(spellings map[string]TagValue) map[string]TagValue {
	out := make(map[string]TagValue, len(spellings))
	for spelling, value := range spellings {
		out[lookupKey(spelling)] = value
	}
	return out
}

// withCanonicalKeys adds each canonical value of a type under the lookup key
// of its own spelling, without overriding an explicit alias. For search, only
// franchise values are added, keyed by the name after "franchise:".
func withCanonicalKeys(tagType TagType, aliases map[string]TagValue) map[string]TagValue {
	for _, value := range CanonicalTagDefinitions[tagType] {
		name := string(value)
		if tagType == TagTypeSearch {
			rest, ok := strings.CutPrefix(name, "franchise:")
			if !ok {
				continue
			}
			name = rest
		}
		key := lookupKey(name)
		if _, exists := aliases[key]; !exists {
			aliases[key] = value
		}
	}
	return aliases
}
