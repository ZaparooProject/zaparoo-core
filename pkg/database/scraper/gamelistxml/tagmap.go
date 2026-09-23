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

package gamelistxml

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"

// unmappedScraperID names this scraper in the unmapped-value log lines.
const unmappedScraperID = "gamelist.xml"

// screenScraperRegions maps the ScreenScraper region codes that are not also
// filename region words. "ss" is ScreenScraper's own default, not a region.
var screenScraperRegions = map[string]tags.TagValue{
	"wor": tags.TagRegionWorld,
	"eur": tags.TagRegionEU,
	"jpn": tags.TagRegionJP,
	"asi": tags.TagRegionAsia,
	"sp":  tags.TagRegionES,
}

// recalboxGenreIDs maps the numeric <genreid> Recalbox writes (its GameGenres
// enum in recalbox-emulationstation es-app/src/games/classifications/Genres.h:
// a category in the high byte, a subgenre in the low byte) onto
// canonical genre values, parents included. Every id the enum defines is
// listed; an empty list drops an id with no honest canonical value. An id not
// listed at all is reported as unmapped.
var recalboxGenreIDs = map[string][]tags.TagValue{
	"256":  {tags.TagGameGenreAction},
	"257":  {tags.TagGameGenreActionPlatformer, tags.TagGameGenreAction},
	"258":  {tags.TagGameGenreActionRunAndGun, tags.TagGameGenreAction},
	"259":  {tags.TagGameGenreShootingFPS, tags.TagGameGenreShooting},
	"260":  {tags.TagGameGenreShmup},
	"261":  {tags.TagGameGenreShootingGallery, tags.TagGameGenreShooting},
	"262":  {tags.TagGameGenreFighting},
	"263":  {tags.TagGameGenreBrawler},
	"266":  {tags.TagGameGenreRhythm},
	"512":  {tags.TagGameGenreAdventure},
	"513":  {tags.TagGameGenreAdventureText, tags.TagGameGenreAdventure},
	"514":  {tags.TagGameGenreAdventurePointClick, tags.TagGameGenreAdventure},
	"515":  {tags.TagGameGenreAdventureVisualNovel, tags.TagGameGenreAdventure},
	"516":  {tags.TagGameGenreAdventure},
	"517":  {tags.TagGameGenreAdventure},
	"518":  {tags.TagGameGenreAdventureSurvivalHorror, tags.TagGameGenreAdventure},
	"768":  {tags.TagGameGenreRPG},
	"769":  {tags.TagGameGenreRPGAction, tags.TagGameGenreRPG},
	"770":  {tags.TagGameGenreRPGMMO, tags.TagGameGenreRPG},
	"771":  {tags.TagGameGenreRPGDungeonCrawler, tags.TagGameGenreRPG},
	"772":  {tags.TagGameGenreRPGStrategy, tags.TagGameGenreRPG},
	"773":  {tags.TagGameGenreRPGJapanese, tags.TagGameGenreRPG},
	"774":  {tags.TagGameGenreRPG},
	"1024": {tags.TagGameGenreSim},
	"1025": {tags.TagGameGenreSimBuilding, tags.TagGameGenreSim},
	"1026": {tags.TagGameGenreSimLife, tags.TagGameGenreSim},
	"1027": {tags.TagGameGenreSim},
	"1028": {tags.TagGameGenreSim},
	"1029": {tags.TagGameGenreSim},
	"1280": {tags.TagGameGenreSimStrategy, tags.TagGameGenreSim},
	"1281": {tags.TagGameGenreSimStrategy, tags.TagGameGenreSim},
	"1282": {tags.TagGameGenreSimStrategy, tags.TagGameGenreSim},
	"1283": {tags.TagGameGenreSimStrategy, tags.TagGameGenreSim},
	"1284": {tags.TagGameGenreSimStrategy, tags.TagGameGenreSim},
	"1285": {tags.TagGameGenreSimStrategy, tags.TagGameGenreSim},
	"1286": {tags.TagGameGenreSimStrategy, tags.TagGameGenreSim},
	"1287": {tags.TagGameGenreSimStrategy, tags.TagGameGenreSim},
	"1288": {tags.TagGameGenreSimStrategy, tags.TagGameGenreSim},
	"1536": {tags.TagGameGenreSports},
	"1537": {tags.TagGameGenreRacing},
	"1538": {tags.TagGameGenreSports},
	"1539": {tags.TagGameGenreSports},
	"1540": {tags.TagGameGenreSports},
	"1792": {tags.TagGameGenreParlorPinball, tags.TagGameGenreParlor},
	"2048": {tags.TagGameGenreBoard},
	"2560": {tags.TagGameGenreSimCardgame, tags.TagGameGenreSim},
	"2816": {tags.TagGameGenrePuzzle},
	"3072": {tags.TagGameGenreBoardParty, tags.TagGameGenreBoard},
	"3328": {tags.TagGameGenreQuiz},
	"3584": {tags.TagGameGenreParlor},
	"4352": {tags.TagGameGenreNotAGameEducational, tags.TagGameGenreNotAGame},

	// Ids with no honest canonical value, dropped on purpose: stealth, battle
	// royale, casual, compilation, demoscene.
	"264":  {},
	"265":  {},
	"2304": {},
	"3840": {},
	"4096": {},
}
