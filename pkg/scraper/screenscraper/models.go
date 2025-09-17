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

package screenscraper

// APIResponse represents the top-level ScreenScraper API response
type APIResponse struct {
	Header   Header   `json:"header"`
	Response Response `json:"response"`
}

// Header contains API response metadata
type Header struct {
	APIVersion    string `json:"APIversion"`
	DateTime      string `json:"dateTime"`
	CommandAsked  string `json:"commandAsked"`
	Success       string `json:"success"`
	Error         string `json:"error"`
	MaxRequests   int    `json:"maxrequests"`
	RequestCount  int    `json:"requestcounter"`
	MaxRequestsKO int    `json:"maxrequestsko"`
	RequestsKO    int    `json:"requestsKO"`
}

// Response contains the actual game data
type Response struct {
	Game  *Game `json:"jeu,omitempty"`
	Games []Game `json:"jeux,omitempty"`
}

// Game represents a game in the ScreenScraper database
type Game struct {
	ID            int            `json:"id"`
	RomID         string         `json:"romid,omitempty"`
	NotGame       int            `json:"notgame,omitempty"`
	Names         []Text         `json:"noms,omitempty"`
	Descriptions  []Text         `json:"synopsis,omitempty"`
	Genres        []Text         `json:"genres,omitempty"`
	ReleaseDate   string         `json:"date,omitempty"`
	Developer     string         `json:"developpeur,omitempty"`
	Publisher     string         `json:"editeur,omitempty"`
	Players       string         `json:"joueurs,omitempty"`
	Rating        float64        `json:"note,omitempty"`
	TopStaff      int            `json:"topstaff,omitempty"`
	Rotation      int            `json:"rotation,omitempty"`
	Resolution    string         `json:"resolution,omitempty"`
	Controls      []Text         `json:"controles,omitempty"`
	Colors        []Text         `json:"couleurs,omitempty"`
	Sons          []Text         `json:"sons,omitempty"`
	Languages     []Text         `json:"langues,omitempty"`
	Systems       []System       `json:"systemes,omitempty"`
	Medias        []Media        `json:"medias,omitempty"`
	ROMs          []ROM          `json:"roms,omitempty"`
	Classifications []Classification `json:"classifications,omitempty"`
}

// Text represents localized text with region and language
type Text struct {
	Region   string `json:"region"`
	Language string `json:"langue"`
	Text     string `json:"text"`
}

// System represents a gaming system/platform
type System struct {
	ID           int    `json:"id"`
	Name         string `json:"nom"`
	Text         string `json:"text"`
	ParentID     int    `json:"parentid,omitempty"`
	LoadName     string `json:"loadname,omitempty"`
	Extensions   string `json:"extensions,omitempty"`
	Company      string `json:"compagnie,omitempty"`
	Type         string `json:"type,omitempty"`
	StartDate    string `json:"datesortie,omitempty"`
	EndDate      string `json:"datefin,omitempty"`
}

// Media represents game media (images, videos, etc.)
type Media struct {
	Type        string `json:"type"`
	Parent      string `json:"parent,omitempty"`
	URL         string `json:"url"`
	Region      string `json:"region,omitempty"`
	Format      string `json:"format,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Size        int    `json:"size,omitempty"`
	CRC32       string `json:"crc32,omitempty"`
	MD5         string `json:"md5,omitempty"`
	SHA1        string `json:"sha1,omitempty"`
}

// ROM represents ROM file information
type ROM struct {
	ID         string `json:"id"`
	RomName    string `json:"romnom"`
	RomSize    string `json:"romsize"`
	RomCRC     string `json:"romcrc"`
	RomMD5     string `json:"rommd5"`
	RomSHA1    string `json:"romsha1"`
	Beta       string `json:"beta,omitempty"`
	Demo       string `json:"demo,omitempty"`
	Proto      string `json:"proto,omitempty"`
	Trad       string `json:"trad,omitempty"`
	Hack       string `json:"hack,omitempty"`
	Unl        string `json:"unl,omitempty"`
	Alt        string `json:"alt,omitempty"`
	Best       string `json:"best,omitempty"`
	Netplay    string `json:"netplay,omitempty"`
}

// Classification represents game ratings/classifications
type Classification struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
}

// GameSearch represents search parameters for ScreenScraper
type GameSearch struct {
	SystemID   string
	GameName   string
	CRC32      string
	MD5        string
	SHA1       string
	RomSize    int64
	Region     string
	Language   string
}

// SearchOptions contains options for game searching
type SearchOptions struct {
	MaxResults int
	Region     string
	Language   string
	MediaTypes []string
}

// APIError represents an error from the ScreenScraper API
type APIError struct {
	Code    string
	Message string
}

func (e APIError) Error() string {
	return e.Message
}