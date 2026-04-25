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

// Package esapi provides types and helpers for reading EmulationStation
// gamelist.xml files.
//
// EmulationStation (ES) has been forked many times; each fork and scraper
// tool differs in which fields it writes and how it formats media paths.
// The Game and Folder structs here cover the superset of all known fields.
// Unknown elements are silently ignored by encoding/xml.
//
// # Path format conventions
//
// All path-based media fields (image, thumbnail, video, etc.) accept three
// path formats, which ES resolves at runtime:
//
//   - Absolute:            /home/pi/.emulationstation/downloaded_images/snes/game.png
//   - System-relative:     ./media/images/game.png   (relative to system ROM folder)
//   - Home-relative:       ~/.emulationstation/downloaded_images/snes/game.png
//
// ES will try to write paths as system-relative or home-relative when saving
// so that installations remain portable across machines.
//
// # Fork / version landscape
//
// This file comments each media field with observed differences across:
//   - Aloshi (original ES, ~2014)
//   - RetroPie fork (RetroPie/EmulationStation)
//   - Batocera fork (batocera-linux/batocera-emulationstation, MetaData.cpp)
//   - ES-DE (EmulationStation Desktop Edition)
//   - AmberELEC / EmuELEC forks
//   - Recalbox fork
//   - Skyscraper scraper (muldjord/skyscraper) output
//   - ARRM scraper output
//   - Pegasus frontend (compatible reader)
package esapi

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

// ESDateFormat is the strftime-style format EmulationStation uses for
// releasedate and lastplayed: "%Y%m%dT%H%M%S". In Go's time package this
// translates to the layout below. Some scrapers omit the time component and
// write only the date portion ("19950311T000000").
const ESDateFormat = "20060102T150405"

// GameList is the root element of an EmulationStation gamelist.xml file.
// It may contain any mix of <game> and <folder> children.
type GameList struct {
	XMLName xml.Name `xml:"gameList"`
	Games   []Game   `xml:"game"`
	Folders []Folder `xml:"folder"`
}

// Game represents a single <game> entry in the gamelist.xml.
//
// Fields are marked omitempty so that re-marshalling preserves sparseness;
// ES itself omits fields whose value matches the type default.
//
// # Media path field differences (20+ documented cases)
//
// Each path-type field below carries a comment block describing known
// fork/scraper differences. The short codes used are:
//
//	[Aloshi]   — original Aloshi/EmulationStation (master branch)
//	[RPI]      — RetroPie fork
//	[Batocera] — Batocera fork (MetaData.cpp defines the canonical tag names)
//	[ES-DE]    — EmulationStation Desktop Edition
//	[AmberELEC]— AmberELEC / EmuELEC forks
//	[Recalbox] — Recalbox fork
//	[Sky]      — Skyscraper scraper output
//	[ARRM]     — ARRM scraper output
type Game struct {
	XMLName             xml.Name `xml:"game"`
	Developer           string   `xml:"developer,omitempty"`
	Bezel               string   `xml:"bezel,omitempty"`
	Name                string   `xml:"name,omitempty"`
	SortName            string   `xml:"sortname,omitempty"`
	Desc                string   `xml:"desc,omitempty"`
	Image               string   `xml:"image,omitempty"`
	Thumbnail           string   `xml:"thumbnail,omitempty"`
	Video               string   `xml:"video,omitempty"`
	Marquee             string   `xml:"marquee,omitempty"`
	Wheel               string   `xml:"wheel,omitempty"`
	FanArt              string   `xml:"fanart,omitempty"`
	TitleShot           string   `xml:"titleshot,omitempty"`
	Manual              string   `xml:"manual,omitempty"`
	Magazine            string   `xml:"magazine,omitempty"`
	Map                 string   `xml:"map,omitempty"`
	Genre               string   `xml:"genre,omitempty"`
	Cartridge           string   `xml:"cartridge,omitempty"`
	BoxBack             string   `xml:"boxback,omitempty"`
	Mix                 string   `xml:"mix,omitempty"`
	Rating              string   `xml:"rating,omitempty"`
	ReleaseDate         string   `xml:"releasedate,omitempty"`
	ScreenScraperIDAttr string   `xml:"id,attr,omitempty"`
	Path                string   `xml:"path"`
	Publisher           string   `xml:"publisher,omitempty"`
	LastPlayed          string   `xml:"lastplayed,omitempty"`
	Players             string   `xml:"players,omitempty"`
	Tags                string   `xml:"tags,omitempty"`
	Family              string   `xml:"family,omitempty"`
	ArcadeSystemName    string   `xml:"arcadesystemname,omitempty"`
	Emulator            string   `xml:"emulator,omitempty"`
	Core                string   `xml:"core,omitempty"`
	Lang                string   `xml:"lang,omitempty"`
	Region              string   `xml:"region,omitempty"`
	Source              string   `xml:"source,omitempty"`
	CRC32               string   `xml:"crc32,omitempty"`
	MD5                 string   `xml:"md5,omitempty"`
	MultiDisk           string   `xml:"multidisk,omitempty"`
	CheevosHash         string   `xml:"cheevosHash,omitempty"`
	Genres              string   `xml:"genres,omitempty"`
	ScreenScraperID     int      `xml:"id,omitempty"`
	PlayCount           int      `xml:"playcount,omitempty"`
	CheevosID           int      `xml:"cheevosId,omitempty"`
	GameTime            int      `xml:"gametime,omitempty"`
	Favorite            bool     `xml:"favorite,omitempty"`
	Hidden              bool     `xml:"hidden,omitempty"`
	KidGame             bool     `xml:"kidgame,omitempty"`
}

// Folder represents a <folder> entry in the gamelist. Folders support a
// smaller set of metadata than games. Most path-based media fields follow
// the same fork differences as in Game — see Game field comments.
type Folder struct {
	XMLName xml.Name `xml:"folder"`

	// Path is the subfolder path, typically relative to the system ROM folder.
	Path string `xml:"path"`

	Name string `xml:"name,omitempty"`
	Desc string `xml:"desc,omitempty"`

	// Image and Thumbnail follow the same fork/path differences as in Game.
	Image     string `xml:"image,omitempty"`
	Thumbnail string `xml:"thumbnail,omitempty"`

	// Some forks (Batocera, ES-DE) also support video and marquee on folders.
	Video   string `xml:"video,omitempty"`
	Marquee string `xml:"marquee,omitempty"`
}

// ReadGameListXML opens and unmarshals an EmulationStation gamelist.xml file.
// Unknown XML elements are silently ignored, so fork-specific fields not
// present in the target ES version are safe to include.
func ReadGameListXML(path string) (GameList, error) {
	// Clean and validate the path to prevent directory traversal attacks.
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Join(".", cleanPath)
	}

	xmlFile, err := os.Open(cleanPath) // #nosec G304 - path is validated above
	if err != nil {
		return GameList{}, fmt.Errorf("failed to open gamelist XML file %s: %w", cleanPath, err)
	}
	defer func(xmlFile *os.File) {
		closeErr := xmlFile.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing xml file")
		}
	}(xmlFile)

	data, err := io.ReadAll(xmlFile)
	if err != nil {
		return GameList{}, fmt.Errorf("failed to read gamelist XML file %s: %w", path, err)
	}

	var gameList GameList
	err = xml.Unmarshal(data, &gameList)
	if err != nil {
		return GameList{}, fmt.Errorf("failed to unmarshal gamelist XML: %w", err)
	}

	return gameList, nil
}

// ParseESDate parses an EmulationStation datetime string into a time.Time.
// ES stores dates as "YYYYMMDDTHHMMSS" (e.g. "19950311T000000").
// Returns the zero time and an error if the string is empty or malformed.
func ParseESDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty datetime string")
	}
	t, err := time.Parse(ESDateFormat, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing ES datetime %q: %w", s, err)
	}
	return t, nil
}

// FormatESDate formats a time.Time into the EmulationStation datetime string
// format ("YYYYMMDDTHHMMSS") used by releasedate and lastplayed fields.
func FormatESDate(t time.Time) string {
	return t.Format(ESDateFormat)
}

// ParseRating parses an ES rating string (a float between "0" and "1") into
// a float64. Returns 0 and an error if the string is empty or malformed.
func ParseRating(s string) (float64, error) {
	if s == "" {
		return 0, errors.New("empty rating string")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing rating %q: %w", s, err)
	}
	return f, nil
}
