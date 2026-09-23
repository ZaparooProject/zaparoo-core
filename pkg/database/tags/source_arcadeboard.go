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
	"slices"
	"strings"
)

// arcadeBoardSpellings maps how sources write an arcade board to its canonical
// arcadeboard value: the MiSTer arcade catalog's own platform names first,
// then the short and alternative names other sources use. Keys are folded by
// lookupKey, so case and punctuation do not matter. Catalogs append "hardware"
// or "based" to a board family's name; entries are written without it and
// withBoardSuffixes makes each reachable with either word or none.
//
// A name that covers several unrelated boards (a CPU, a vendor's catch-all,
// "Capcom CPS-0") is deliberately absent: it has no single canonical value.
var arcadeBoardSpellings = map[string]TagValue{
	"Atari Battle Zone":                  TagArcadeBoardAtariBattleZone,
	"Atari Centipede":                    TagArcadeBoardAtariCentipede,
	"Atari Gauntlet":                     TagArcadeBoardAtariGauntlet,
	"Atari Missile Command":              TagArcadeBoardAtariMissileCommand,
	"Atari System-1":                     TagArcadeBoardAtariSystem1,
	"CAVE 68000":                         TagArcadeBoardCave68000,
	"Capcom CPS-1":                       TagArcadeBoardCapcomCPS,
	"Capcom CPS-1.5":                     TagArcadeBoardCapcomCPSDash,
	"Capcom CPS-2":                       TagArcadeBoardCapcomCPS2,
	"Capcom Mitchell":                    TagArcadeBoardCapcomMitchell,
	"Cinematronics Jack The Giantkiller": TagArcadeBoardCinematronicsJackTheGiantkiller,
	"Data East MEC-M1":                   TagArcadeBoardDataEastMECM1,
	"Data East Burger Time":              TagArcadeBoardDataEastBurgerTime,
	"Data East Karnov":                   TagArcadeBoardDataEastKarnov,
	"Donkey Kong":                        TagArcadeBoardNintendoDonkeyKong,
	"Dottori-Kun hardware":               TagArcadeBoardSegaDottoriKun,
	"Exidy Universal Game Board Ver. 2":  TagArcadeBoardExidyUniversalGameBoard2,
	"Game Plan":                          TagArcadeBoardGamePlan,
	"Irem M107":                          TagArcadeBoardIremM107,
	"Jaleco Exerion":                     TagArcadeBoardJalecoExerion,
	"Jaleco Ginga Ninkyouden":            TagArcadeBoardJalecoGingaNinkyouden,
	"Jaleco Mega System 1":               TagArcadeBoardJalecoMegaSystem1,
	"Jaleco Naughty Boy":                 TagArcadeBoardJalecoNaughtyBoy,
	"Jaleco Psychic 5":                   TagArcadeBoardJalecoPsychic5,
	"Kiwako Mr. Jong":                    TagArcadeBoardKiwakoMrJong,
	"Konami Ajax":                        TagArcadeBoardKonamiAjax,
	"Konami Aliens":                      TagArcadeBoardKonamiAliens,
	"Konami Asterix":                     TagArcadeBoardKonamiAsterix,
	"Konami Contra":                      TagArcadeBoardKonamiContra,
	"Konami Double Dribble":              TagArcadeBoardKonamiDoubleDribble,
	"Konami GX400":                       TagArcadeBoardKonamiGX400,
	"Konami Green Beret":                 TagArcadeBoardKonamiGreenBeret,
	"Konami Juno First":                  TagArcadeBoardKonamiJunoFirst,
	"Konami Run and Gun":                 TagArcadeBoardKonamiRunAndGun,
	"Konami Scramble":                    TagArcadeBoardKonamiScramble,
	"Konami Surprise Attack":             TagArcadeBoardKonamiSurpriseAttack,
	"Konami TMNT 2":                      TagArcadeBoardKonamiTMNT2,
	"Konami TMNT":                        TagArcadeBoardKonamiTMNT,
	"Konami The Simpsons":                TagArcadeBoardKonamiSimpsons,
	"Konami Thunder X":                   TagArcadeBoardKonamiThunderX,
	"Konami Twin16":                      TagArcadeBoardKonamiTwin16,
	"Konami Vendetta":                    TagArcadeBoardKonamiVendetta,
	"Konami X-Men":                       TagArcadeBoardKonamiXMen,
	"Mario Bros.":                        TagArcadeBoardNintendoMarioBros,
	"Midway 8080":                        TagArcadeBoardMidway8080,
	"Midway Astrocade":                   TagArcadeBoardMidwayAstrocade,
	"Midway MCR":                         TagArcadeBoardMidwayMCR,
	"Midway MCR1":                        TagArcadeBoardMidwayMCR1,
	"Midway MCR2":                        TagArcadeBoardMidwayMCR2,
	"Midway MCR3":                        TagArcadeBoardMidwayMCR3,
	"Midway Y-Unit":                      TagArcadeBoardMidwayYUnit,
	"Mr. Do":                             TagArcadeBoardUniversalMrDo,
	"Namco Baraduke":                     TagArcadeBoardNamcoBaraduke,
	"Namco Galaga":                       TagArcadeBoardNamcoGalaga,
	"Namco Galaxian":                     TagArcadeBoardNamcoGalaxian,
	"Namco Pac-Man":                      TagArcadeBoardNamcoPacMan,
	"Namco Super Pac-Man":                TagArcadeBoardNamcoSuperPacMan,
	"Namco System 86":                    TagArcadeBoardNamcoSystem86,
	"Namco System-1":                     TagArcadeBoardNamcoSystem1,
	"Nichibutsu M68000 (Armed F)":        TagArcadeBoardNichibutsuArmedF,
	"Nichibutsu M68000 (Terra Cresta)":   TagArcadeBoardNichibutsuTerraCresta,
	"Nichibutsu Z80 (Galivan)":           TagArcadeBoardNichibutsuGalivan,
	"Poly Play":                          TagArcadeBoardPolyPlay,
	"SNK - Alpha Denshi":                 TagArcadeBoardSNKAlpha68K,
	"SNK 68000":                          TagArcadeBoardSNK68000,
	"SNK Triple Z80":                     TagArcadeBoardSNKTripleZ80,
	"Sega Blockade":                      TagArcadeBoardSegaBlockade,
	"Sega Out Run":                       TagArcadeBoardSegaOutRun,
	"Sega ST-V":                          TagArcadeBoardSegaSTV,
	"Sega System 1":                      TagArcadeBoardSegaSystem1,
	"Sega System 16":                     TagArcadeBoardSegaSystem16,
	"Sega System 18":                     TagArcadeBoardSegaSystem18,
	"Sega System E":                      TagArcadeBoardSegaSystemE,
	"Sega VIC Dual":                      TagArcadeBoardSegaVicDual,
	"Sega Zaxxon":                        TagArcadeBoardSegaZaxxon,
	"Seibu Toki":                         TagArcadeBoardSeibuToki,
	"Solomon's Key":                      TagArcadeBoardTecmoSolomonsKey,
	"Stern based":                        TagArcadeBoardSternBerzerk,
	"TIAMC1":                             TagArcadeBoardTIAMC1,
	"Taito 8080":                         TagArcadeBoardTaito8080,
	"Taito Arkanoid":                     TagArcadeBoardTaitoArkanoid,
	"Taito Bubble Booble":                TagArcadeBoardTaitoBubbleBobble,
	"Taito F2 System":                    TagArcadeBoardTaitoF2System,
	"Taito Kick and Run":                 TagArcadeBoardTaitoKickAndRun,
	"Taito N.Y. Captor":                  TagArcadeBoardTaitoNYCaptor,
	"Taito System SJ":                    TagArcadeBoardTaitoSJSystem,
	"Taito The FairyLand Story":          TagArcadeBoardTaitoFairylandStory,
	"Taito The NewZealand Story":         TagArcadeBoardTaitoNewZealandStory,
	"Technos Xain'd Sleena":              TagArcadeBoardTechnosXaindSleena,
	"Tecmo Ninja Gaiden":                 TagArcadeBoardTecmoNinjaGaiden,
	"Tecmo Senjyo":                       TagArcadeBoardTecmoSenjyo,
	"Tecmo Tehkan":                       TagArcadeBoardTecmoTehkanWorldCup,
	"Tecmo hardware":                     TagArcadeBoardTecmoRygar,
	"Tehkan hardware":                    TagArcadeBoardTehkanBombJack,
	"Toaplan 1":                          TagArcadeBoardToaplanVersion1,
	"Toaplan 2":                          TagArcadeBoardToaplanVersion2,
	"Toaplan Slap Fight":                 TagArcadeBoardToaplanSlapFight,
	"Toaplan Twin Cobra":                 TagArcadeBoardToaplanTwinCobra,
	"Universal Cosmic Guerilla":          TagArcadeBoardUniversalCosmicGuerilla,
	"Universal Cosmic":                   TagArcadeBoardUniversalCosmic,
	"Universal Lady Bug":                 TagArcadeBoardUniversalLadyBug,
	"Williams 1st Generation":            TagArcadeBoardWilliamsGen1,
	"Williams 2nd Generation":            TagArcadeBoardWilliamsGen2,
	"Alpha Denshi Champion Base Ball":    TagArcadeBoardAlphaDenshiChampionBaseBall,
	"Atari Klax":                         TagArcadeBoardAtariKlax,
	"Atari System-2":                     TagArcadeBoardAtariSystem2,
	"Blue Print hardware":                TagArcadeBoardBluePrint,
	"Cinematronics Laserdisc":            TagArcadeBoardCinematronicsLaserdisc,
	"Data East Act-Fancer":               TagArcadeBoardDataEastActFancer,
	"Data East Caveman Ninja":            TagArcadeBoardDataEastCavemanNinja,
	"Data East Crude Buster":             TagArcadeBoardDataEastCrudeBuster,
	"Data East Dark Seal":                TagArcadeBoardDataEastDarkSeal,
	"Data East DE-0359":                  TagArcadeBoardDataEastDE0359,
	"Data East DE-0379":                  TagArcadeBoardDataEastDE0379,
	"Data East DE-0397":                  TagArcadeBoardDataEastDE0397,
	"Data East DEC8":                     TagArcadeBoardDataEastDEC8,
	"Data East DECO Cassette":            TagArcadeBoardDataEastDECOCassette,
	"Data East Diet Go Go":               TagArcadeBoardDataEastDietGoGo,
	"Data East Double Wings":             TagArcadeBoardDataEastDoubleWings,
	"Data East Pocket Gal":               TagArcadeBoardDataEastPocketGal,
	"Data East Rohga":                    TagArcadeBoardDataEastRohga,
	"Data East Super Burger Time":        TagArcadeBoardDataEastSuperBurgerTime,
	"Data East Thunder Zone":             TagArcadeBoardDataEastThunderZone,
	"Data East Tumble Pop":               TagArcadeBoardDataEastTumblePop,
	"Data East Vapor Trail":              TagArcadeBoardDataEastVaporTrail,
	"Gaelco hardware":                    TagArcadeBoardGaelcoBigKarnak,
	"IGS PGM":                            TagArcadeBoardIGSPGM,
	"Kaneko Gals Panic":                  TagArcadeBoardKanekoGalsPanic,
	"Kangaroo hardware":                  TagArcadeBoardSunElectronicsKangaroo,
	"Konami G.I. Joe":                    TagArcadeBoardKonamiGIJoe,
	"Konami Gradius III":                 TagArcadeBoardKonamiGradius3,
	"Konami Tutankham":                   TagArcadeBoardKonamiTutankham,
	"Kyugo hardware":                     TagArcadeBoardKyugo,
	"Mat Mania hardware":                 TagArcadeBoardTechnosMatMania,
	"Nintendo Aleck64":                   TagArcadeBoardNintendoAleck64,
	"Psikyo SH-2":                        TagArcadeBoardPsikyoSH2,
	"Seibu Blood Bros":                   TagArcadeBoardSeibuBloodBros,
	"Seibu Cabal":                        TagArcadeBoardSeibuCabal,
	"Seibu D-Con":                        TagArcadeBoardSeibuDCon,
	"Seibu Legionnaire":                  TagArcadeBoardSeibuLegionnaire,
	"Seibu Raiden":                       TagArcadeBoardSeibuRaiden,
	"Seibu Raiden 2":                     TagArcadeBoardSeibuRaiden2,
	"SNK6502 hardware":                   TagArcadeBoardSNK6502,
	"Sun Electronics Arabian":            TagArcadeBoardSunElectronicsArabian,
	"Taito Darius":                       TagArcadeBoardTaitoDarius,
	"Taito Ninja Warriors":               TagArcadeBoardTaitoNinjaWarriors,
	"Taito Qix":                          TagArcadeBoardTaitoQix,
	"Taito Volfied":                      TagArcadeBoardTaitoVolfied,
	"Taito Warrior Blade":                TagArcadeBoardTaitoWarriorBlade,
	"Technos Block Out":                  TagArcadeBoardTechnosBlockOut,
	"Toaplan Snow Bros":                  TagArcadeBoardToaplanSnowBros,
	"Vastar hardware":                    TagArcadeBoardOrcaVastar,
	"Sega G80 Vector":                    TagArcadeBoardSegaG80,
	"CPS":                                TagArcadeBoardCapcomCPS,
	"CPS1":                               TagArcadeBoardCapcomCPS,
	"CP System":                          TagArcadeBoardCapcomCPS,
	"Capcom Play System":                 TagArcadeBoardCapcomCPS,
	"CPS-1.5":                            TagArcadeBoardCapcomCPSDash,
	"CPS Dash":                           TagArcadeBoardCapcomCPSDash,
	"CPS Changer":                        TagArcadeBoardCapcomCPSChanger,
	"CPS2":                               TagArcadeBoardCapcomCPS2,
	"CP System II":                       TagArcadeBoardCapcomCPS2,
	"Capcom Play System 2":               TagArcadeBoardCapcomCPS2,
	"CPS3":                               TagArcadeBoardCapcomCPS3,
	"CP System III":                      TagArcadeBoardCapcomCPS3,
	"Capcom Play System 3":               TagArcadeBoardCapcomCPS3,
	"Neo-Geo":                            TagArcadeBoardSNKMVS,
	"Neo Geo MVS":                        TagArcadeBoardSNKMVS,
	"MVS":                                TagArcadeBoardSNKMVS,
	"SNK Neo-Geo":                        TagArcadeBoardSNKMVS,
	"SNK Neo-Geo MVS":                    TagArcadeBoardSNKMVS,
	"Naomi":                              TagArcadeBoardSegaNaomi,
	"ST-V":                               TagArcadeBoardSegaSTV,
	"Sega Titan":                         TagArcadeBoardSegaSTV,
	"System 16":                          TagArcadeBoardSegaSystem16,
	"System 16A":                         TagArcadeBoardSegaSystem16A,
	"System 16B":                         TagArcadeBoardSegaSystem16B,
	"System 16C":                         TagArcadeBoardSegaSystem16C,
	"System 18":                          TagArcadeBoardSegaSystem18,
	"System 24":                          TagArcadeBoardSegaSystem24,
	"System 32":                          TagArcadeBoardSegaSystem32,
	"System C2":                          TagArcadeBoardSegaSystemC2,
	"System E":                           TagArcadeBoardSegaSystemE,
	"Sega X Board":                       TagArcadeBoardSegaXBoard,
	"X Board":                            TagArcadeBoardSegaXBoard,
	"Sega Y Board":                       TagArcadeBoardSegaYBoard,
	"Y Board":                            TagArcadeBoardSegaYBoard,
	"Model 1":                            TagArcadeBoardSegaModel1,
	"Model 3":                            TagArcadeBoardSegaModel3,
	"Mega Play":                          TagArcadeBoardSegaMegaplay,
	"Mega System 1":                      TagArcadeBoardJalecoMegaSystem1,
	"Taito F1":                           TagArcadeBoardTaitoF1System,
	"Taito F2":                           TagArcadeBoardTaitoF2System,
	"F2 System":                          TagArcadeBoardTaitoF2System,
	"Taito F3":                           TagArcadeBoardTaitoF3System,
	"F3 System":                          TagArcadeBoardTaitoF3System,
	"Taito B System":                     TagArcadeBoardTaitoBSystem,
	"Taito H System":                     TagArcadeBoardTaitoHSystem,
	"Taito L System":                     TagArcadeBoardTaitoLSystem,
	"Taito X System":                     TagArcadeBoardTaitoXSystem,
	"Taito Z System":                     TagArcadeBoardTaitoZSystem,
	"Taito O System":                     TagArcadeBoardTaitoOSystem,
	"Taito SJ System":                    TagArcadeBoardTaitoSJSystem,
	"Toaplan Version 1":                  TagArcadeBoardToaplanVersion1,
	"Toaplan Version 2":                  TagArcadeBoardToaplanVersion2,
	"Nintendo VS. System":                TagArcadeBoardNintendoVS,
	"VS. System":                         TagArcadeBoardNintendoVS,
	"VS. UniSystem":                      TagArcadeBoardNintendoVS,
	"VS. DualSystem":                     TagArcadeBoardNintendoVS,
	"Nintendo Super System":              TagArcadeBoardNintendoNSS,
	"Konami Nemesis":                     TagArcadeBoardKonamiGX400,
	"Williams 1st Gen":                   TagArcadeBoardWilliamsGen1,
	"Williams 2nd Gen":                   TagArcadeBoardWilliamsGen2,
	"SNK Alpha 68K":                      TagArcadeBoardSNKAlpha68K,
	"Alpha Denshi 68000":                 TagArcadeBoardSNKAlpha68K,
	"Cave 68K":                           TagArcadeBoardCave68000,
}

// arcadeBoardAliases maps lookupKey(source name) to a canonical arcadeboard
// value. Every canonical value is reachable by its own spelling as well.
var arcadeBoardAliases = withCanonicalKeys(TagTypeArcadeBoard, withBoardSuffixes(arcadeBoardSpellings))

// boardSuffixes are the words catalogs append to a board family's name.
var boardSuffixes = []string{"hardware", "based"}

// withBoardSuffixes returns the spellings keyed by lookupKey, each also
// registered with every board suffix and, where a board name is left, with
// none. A one-word name keeps its suffix: "Tecmo hardware" without it is only
// a vendor, and "Tecmo" alone names no one board.
func withBoardSuffixes(spellings map[string]TagValue) map[string]TagValue {
	out := make(map[string]TagValue, len(spellings)*(len(boardSuffixes)+1))
	for spelling, value := range spellings {
		out[lookupKey(spelling)] = value
		words := strings.Fields(spelling)
		if len(words) > 1 && slices.Contains(boardSuffixes, strings.ToLower(words[len(words)-1])) {
			words = words[:len(words)-1]
			if len(words) > 1 {
				out[lookupKey(strings.Join(words, " "))] = value
			}
		}
		base := lookupKey(strings.Join(words, " "))
		for _, suffix := range boardSuffixes {
			out[base+suffix] = value
		}
	}
	return out
}
