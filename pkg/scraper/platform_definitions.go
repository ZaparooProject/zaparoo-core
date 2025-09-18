// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package scraper

// PlatformIDs holds the platform IDs for each scraper service
type PlatformIDs struct {
	IGDB          int
	ScreenScraper string
	TheGamesDB    int
}

// PlatformDefinitions maps Zaparoo system IDs to all scraper platform IDs
// This centralizes platform mapping to avoid duplication across scrapers
var PlatformDefinitions = map[string]PlatformIDs{
	// Nintendo Consoles
	"nes":          {IGDB: 18, ScreenScraper: "1", TheGamesDB: 7},
	"famicom":      {IGDB: 18, ScreenScraper: "1", TheGamesDB: 7}, // Same as NES
	"snes":         {IGDB: 19, ScreenScraper: "2", TheGamesDB: 6},
	"superfamicom": {IGDB: 19, ScreenScraper: "2", TheGamesDB: 6}, // Same as SNES
	"n64":          {IGDB: 4, ScreenScraper: "14", TheGamesDB: 3},
	"gb":           {IGDB: 33, ScreenScraper: "9", TheGamesDB: 4},
	"gbc":          {IGDB: 22, ScreenScraper: "10", TheGamesDB: 5},
	"gba":          {IGDB: 24, ScreenScraper: "12", TheGamesDB: 12},
	"nds":          {IGDB: 20, ScreenScraper: "15", TheGamesDB: 20},
	"3ds":          {IGDB: 37, ScreenScraper: "17", TheGamesDB: 4912},
	"gamecube":     {IGDB: 21, ScreenScraper: "13", TheGamesDB: 2},
	"gc":           {IGDB: 21, ScreenScraper: "13", TheGamesDB: 2}, // Same as gamecube
	"wii":          {IGDB: 5, ScreenScraper: "16", TheGamesDB: 9},
	"wiiu":         {IGDB: 41, ScreenScraper: "18", TheGamesDB: 38},
	"switch":       {IGDB: 130, ScreenScraper: "225", TheGamesDB: 4971},

	// Sony Consoles
	"psx":     {IGDB: 7, ScreenScraper: "57", TheGamesDB: 10},
	"ps2":     {IGDB: 8, ScreenScraper: "58", TheGamesDB: 11},
	"ps3":     {IGDB: 9, ScreenScraper: "59", TheGamesDB: 12},
	"ps4":     {IGDB: 48, ScreenScraper: "68", TheGamesDB: 4919},
	"ps5":     {IGDB: 167, ScreenScraper: "68", TheGamesDB: 4920}, // ScreenScraper may not have PS5 yet
	"psp":     {IGDB: 38, ScreenScraper: "61", TheGamesDB: 13},
	"vita":    {IGDB: 46, ScreenScraper: "62", TheGamesDB: 39},

	// Microsoft Consoles
	"xbox":    {IGDB: 11, ScreenScraper: "32", TheGamesDB: 14},
	"xbox360": {IGDB: 12, ScreenScraper: "33", TheGamesDB: 15},
	"xboxone": {IGDB: 49, ScreenScraper: "29", TheGamesDB: 4920},

	// Sega Consoles
	"genesis":   {IGDB: 29, ScreenScraper: "1", TheGamesDB: 18},
	"megadrive": {IGDB: 29, ScreenScraper: "1", TheGamesDB: 18}, // Same as genesis
	"sms":       {IGDB: 64, ScreenScraper: "2", TheGamesDB: 35},
	"gg":        {IGDB: 35, ScreenScraper: "8", TheGamesDB: 20},
	"saturn":    {IGDB: 32, ScreenScraper: "22", TheGamesDB: 17},
	"dreamcast": {IGDB: 23, ScreenScraper: "23", TheGamesDB: 16},

	// Atari Systems
	"atari2600": {IGDB: 59, ScreenScraper: "26", TheGamesDB: 22},
	"atari7800": {IGDB: 60, ScreenScraper: "27", TheGamesDB: 28},
	"lynx":      {IGDB: 61, ScreenScraper: "28", TheGamesDB: 4924},
	"jaguar":    {IGDB: 62, ScreenScraper: "31", TheGamesDB: 4925},

	// Arcade and Others
	"arcade": {IGDB: 52, ScreenScraper: "75", TheGamesDB: 23},
	"mame":   {IGDB: 52, ScreenScraper: "75", TheGamesDB: 23}, // Same as arcade
	"neogeo": {IGDB: 80, ScreenScraper: "142", TheGamesDB: 24},

	// Computers
	"dos":    {IGDB: 13, ScreenScraper: "135", TheGamesDB: 1},
	"pc":     {IGDB: 6, ScreenScraper: "135", TheGamesDB: 1},
	"amiga":  {IGDB: 16, ScreenScraper: "64", TheGamesDB: 4911},
	"c64":    {IGDB: 15, ScreenScraper: "66", TheGamesDB: 40},

	// Mobile
	"android": {IGDB: 34, ScreenScraper: "224", TheGamesDB: 4916},
	"ios":     {IGDB: 39, ScreenScraper: "224", TheGamesDB: 4917},
}