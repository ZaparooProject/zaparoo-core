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

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// PlatformIDs holds the platform IDs for each scraper service
type PlatformIDs struct {
	ScreenScraper string
	IGDB          int
	TheGamesDB    int
}

// PlatformDefinitions maps Zaparoo system IDs to all scraper platform IDs
// This centralizes platform mapping to avoid duplication across scrapers
var PlatformDefinitions = map[string]PlatformIDs{
	// Nintendo Consoles
	systemdefs.SystemNES:        {IGDB: 18, ScreenScraper: "1", TheGamesDB: 7},
	systemdefs.SystemSNES:       {IGDB: 19, ScreenScraper: "2", TheGamesDB: 6},
	systemdefs.SystemNintendo64: {IGDB: 4, ScreenScraper: "14", TheGamesDB: 3},
	systemdefs.SystemGameboy:    {IGDB: 33, ScreenScraper: "9", TheGamesDB: 4},
	systemdefs.SystemGameboyColor: {IGDB: 22, ScreenScraper: "10", TheGamesDB: 5},
	systemdefs.SystemGBA:        {IGDB: 24, ScreenScraper: "12", TheGamesDB: 12},
	systemdefs.SystemNDS:        {IGDB: 20, ScreenScraper: "15", TheGamesDB: 20},
	systemdefs.System3DS:        {IGDB: 37, ScreenScraper: "17", TheGamesDB: 4912},
	systemdefs.SystemGameCube:   {IGDB: 21, ScreenScraper: "13", TheGamesDB: 2},
	systemdefs.SystemWii:        {IGDB: 5, ScreenScraper: "16", TheGamesDB: 9},
	systemdefs.SystemWiiU:       {IGDB: 41, ScreenScraper: "18", TheGamesDB: 38},
	systemdefs.SystemSwitch:     {IGDB: 130, ScreenScraper: "225", TheGamesDB: 4971},

	// Sony Consoles
	systemdefs.SystemPSX: {IGDB: 7, ScreenScraper: "57", TheGamesDB: 10},
	systemdefs.SystemPS2: {IGDB: 8, ScreenScraper: "58", TheGamesDB: 11},
	systemdefs.SystemPS3: {IGDB: 9, ScreenScraper: "59", TheGamesDB: 12},
	systemdefs.SystemPS4: {IGDB: 48, ScreenScraper: "68", TheGamesDB: 4919},
	systemdefs.SystemPS5: {IGDB: 167, ScreenScraper: "68", TheGamesDB: 4920}, // ScreenScraper may not have PS5 yet
	systemdefs.SystemPSP: {IGDB: 38, ScreenScraper: "61", TheGamesDB: 13},
	systemdefs.SystemVita: {IGDB: 46, ScreenScraper: "62", TheGamesDB: 39},

	// Microsoft Consoles
	systemdefs.SystemXbox:    {IGDB: 11, ScreenScraper: "32", TheGamesDB: 14},
	systemdefs.SystemXbox360: {IGDB: 12, ScreenScraper: "33", TheGamesDB: 15},
	systemdefs.SystemXboxOne: {IGDB: 49, ScreenScraper: "29", TheGamesDB: 4920},

	// Sega Consoles
	systemdefs.SystemGenesis:     {IGDB: 29, ScreenScraper: "1", TheGamesDB: 18},
	systemdefs.SystemMasterSystem: {IGDB: 64, ScreenScraper: "2", TheGamesDB: 35},
	systemdefs.SystemGameGear:    {IGDB: 35, ScreenScraper: "8", TheGamesDB: 20},
	systemdefs.SystemSaturn:      {IGDB: 32, ScreenScraper: "22", TheGamesDB: 17},
	systemdefs.SystemDreamcast:   {IGDB: 23, ScreenScraper: "23", TheGamesDB: 16},

	// Atari Systems
	systemdefs.SystemAtari2600: {IGDB: 59, ScreenScraper: "26", TheGamesDB: 22},
	systemdefs.SystemAtari7800: {IGDB: 60, ScreenScraper: "27", TheGamesDB: 28},
	systemdefs.SystemAtariLynx: {IGDB: 61, ScreenScraper: "28", TheGamesDB: 4924},
	systemdefs.SystemJaguar:    {IGDB: 62, ScreenScraper: "31", TheGamesDB: 4925},

	// Arcade and Others
	systemdefs.SystemArcade:  {IGDB: 52, ScreenScraper: "75", TheGamesDB: 23},
	systemdefs.SystemNeoGeo: {IGDB: 80, ScreenScraper: "142", TheGamesDB: 24},

	// Computers
	systemdefs.SystemDOS:   {IGDB: 13, ScreenScraper: "135", TheGamesDB: 1},
	systemdefs.SystemPC:    {IGDB: 6, ScreenScraper: "135", TheGamesDB: 1},
	systemdefs.SystemAmiga: {IGDB: 16, ScreenScraper: "64", TheGamesDB: 4911},
	systemdefs.SystemC64:   {IGDB: 15, ScreenScraper: "66", TheGamesDB: 40},

	// Mobile
	systemdefs.SystemAndroid: {IGDB: 34, ScreenScraper: "224", TheGamesDB: 4916},
	systemdefs.SystemIOS:     {IGDB: 39, ScreenScraper: "224", TheGamesDB: 4917},

	// Additional systems with ScreenScraper mappings
	systemdefs.SystemSG1000:            {ScreenScraper: "109"},
	systemdefs.SystemMegaCD:            {ScreenScraper: "20"},
	systemdefs.SystemSega32X:           {ScreenScraper: "19"},
	systemdefs.SystemCPS1:              {ScreenScraper: "6"},
	systemdefs.SystemCPS2:              {ScreenScraper: "7"},
	systemdefs.SystemCPS3:              {ScreenScraper: "8"},
	systemdefs.SystemAtari5200:         {ScreenScraper: "40"},
	systemdefs.SystemColecoVision:      {ScreenScraper: "48"},
	systemdefs.SystemIntellivision:     {ScreenScraper: "115"},
	systemdefs.SystemVectrex:           {ScreenScraper: "102"},
	systemdefs.SystemOdyssey2:          {ScreenScraper: "104"},
	systemdefs.SystemTurboGrafx16:      {ScreenScraper: "31"},
	systemdefs.SystemPCFX:              {ScreenScraper: "72"},
	systemdefs.SystemWonderSwan:        {ScreenScraper: "45"},
	systemdefs.SystemWonderSwanColor:   {ScreenScraper: "46"},
	systemdefs.SystemNeoGeoPocket:      {ScreenScraper: "25"},
	systemdefs.SystemNeoGeoPocketColor: {ScreenScraper: "82"},
	systemdefs.SystemAmstrad:           {ScreenScraper: "65"},
	systemdefs.SystemAppleII:           {ScreenScraper: "86"},
	systemdefs.SystemMSX:               {ScreenScraper: "113"},
	systemdefs.SystemMSX2:              {ScreenScraper: "116"},
	systemdefs.SystemZXSpectrum:        {ScreenScraper: "76"},
	systemdefs.SystemAtariST:           {ScreenScraper: "42"},
	systemdefs.SystemGameNWatch:        {ScreenScraper: "52"},
	systemdefs.SystemChannelF:          {ScreenScraper: "80"},
	systemdefs.SystemThomson:           {ScreenScraper: "141"},
	systemdefs.SystemSAMCoupe:          {ScreenScraper: "213"},
	systemdefs.SystemX68000:            {ScreenScraper: "79"},
	systemdefs.SystemX1:                {ScreenScraper: "220"},
	systemdefs.SystemFM7:               {ScreenScraper: "97"},
	systemdefs.SystemFMTowns:           {ScreenScraper: "105"},
	systemdefs.SystemPC88:              {ScreenScraper: "221"},
	systemdefs.SystemPC98:              {ScreenScraper: "83"},
	systemdefs.SystemGP32:              {ScreenScraper: "146"},
	systemdefs.SystemPico8:             {ScreenScraper: "234"},
	systemdefs.SystemTIC80:             {ScreenScraper: "232"},
}
