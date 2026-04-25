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
	XMLName xml.Name `xml:"game"`

	// ScreenScraperIDAttr is the Screenscraper.fr numeric game ID written as
	// an XML attribute on the <game> element by some scrapers.
	//
	// Differences:
	//   [Sky]     writes it as attribute: <game id="12345">
	//   [Batocera] writes it as child element <id>12345</id> (see ScreenScraperID)
	//   [Aloshi]  not in spec; most forks ignore it
	ScreenScraperIDAttr string `xml:"id,attr,omitempty"`

	// --- Core identity / ROM path ---

	// Path is the file path to the ROM. Required.
	// Typically system-relative (./Contra.nes) or absolute.
	// Folder entries also use <path> for the subfolder path.
	Path string `xml:"path"`

	// Name is the displayed title for the game.
	Name string `xml:"name,omitempty"`

	// SortName is an alternative name used only for sort order.
	// [RPI] added this field; [Aloshi] does not define it.
	// [Batocera] supports it as <sortname>.
	// [ES-DE] also supports <sortname>.
	SortName string `xml:"sortname,omitempty"`

	// Desc is a (potentially multi-line) game description.
	// Long descriptions auto-scroll in ES themes.
	Desc string `xml:"desc,omitempty"`

	// --- PATH-BASED MEDIA FIELDS ---
	//
	// IMPORTANT: Tag names and filesystem conventions differ across forks.
	// See each field's comment for the 20+ documented differences.

	// Image is the primary artwork shown for the game (box art, screenshot,
	// or a composited "mix image"). This is the most widely supported media tag.
	//
	// Tag: <image>   Supported by: all forks
	//
	// Path differences (difference 1–6):
	//   [Aloshi]    no defined subfolder convention; absolute path expected
	//   [RPI]       ~/.emulationstation/downloaded_images/<system>/<rom>-image.png
	//   [Sky→RPI]   ./media/images/<rom>.png  (Skyscraper targeting RetroPie)
	//   [Batocera]  /userdata/roms/<system>/media/images/<rom>.png
	//   [ES-DE]     ~/ES-DE/downloaded_media/<system>/miximages/<rom>.png
	//               (ES-DE writes the mix image here by default, not a plain screenshot)
	//   [Recalbox]  ~/.recalbox/share/roms/<system>/<rom>.png
	Image string `xml:"image,omitempty"`

	// Thumbnail is a smaller image for grid/list view modes.
	// In [Aloshi] it was defined but explicitly marked "currently unused."
	// Forks repurposed it inconsistently — some use it for cover art while
	// <image> holds a screenshot; others invert that convention.
	//
	// Tag: <thumbnail>   Supported by: all forks (usage varies)
	//
	// Differences (7–11):
	//   [Aloshi]    defined but not rendered ("currently unused")
	//   [RPI]       ~/.emulationstation/downloaded_images/<system>/<rom>-thumb.png
	//               typically the cover art; <image> is a screenshot
	//   [Sky]       changed from <cover> to <thumbnail> in v3.5.4, breaking scrapers
	//               that expected <cover> — see Skyscraper issue #229
	//               stores cover art here: ./media/covers/<rom>.png
	//   [Batocera]  semantically the "box2dfront" image:
	//               /userdata/roms/<system>/media/box2dfront/<rom>.png
	//   [ES-DE]     ~/ES-DE/downloaded_media/<system>/covers/<rom>.png
	//               ES-DE uses <image> for miximages, <thumbnail> for box covers
	Thumbnail string `xml:"thumbnail,omitempty"`

	// Video is the path to a short video clip (attract mode / preview).
	// Only rendered by themes that enable the video viewstyle.
	//
	// Tag: <video>   Not in [Aloshi] original spec; added by later forks.
	//
	// Differences (12–15):
	//   [Aloshi]    not defined; field silently ignored by original ES
	//   [RPI]       ~/.emulationstation/downloaded_videos/<system>/<rom>.mp4
	//   [Sky]       ./media/videos/<rom>.mp4
	//   [Batocera]  /userdata/roms/<system>/media/videos/<rom>.mp4
	//   [ES-DE]     ~/ES-DE/downloaded_media/<system>/videos/<rom>.mp4
	//   [AmberELEC] same tag <video>, path under /storage/roms/<system>/media/videos/
	Video string `xml:"video,omitempty"`

	// Marquee is the cabinet marquee strip image. Forks disagree on whether
	// this tag should hold the traditional horizontal marquee graphic or the
	// stylised wheel/logo artwork — the two meanings are swapped between major
	// ecosystems, causing silent mismatches when migrating gamelists.
	//
	// Tag: <marquee>   Not in [Aloshi] original spec.
	//
	// Differences (16–19):
	//   [Batocera]  marquee = horizontal cabinet marquee strip
	//               /userdata/roms/<system>/media/marquee/<rom>.png
	//   [Sky→RPI]   marquee = wheel/logo art (Skyscraper internally calls
	//               this "wheel" but emits it under the <marquee> XML tag)
	//               ./media/marquees/<rom>.png  (contains logo, not marquee strip)
	//   [Recalbox]  <marquee> holds the logo/wheel art (same as Skyscraper)
	//   [ES-DE]     <marquee> holds the traditional marquee strip;
	//               a separate <wheel> tag holds the logo (see Wheel field)
	Marquee string `xml:"marquee,omitempty"`

	// Wheel is the stylised round/die-cut game logo ("wheel art") used in
	// spinning-wheel theme views. [Batocera] and [ES-DE] use a dedicated
	// <wheel> tag; [Sky] and [Recalbox] fold this into <marquee> instead.
	//
	// Tag: <wheel>   Not in [Aloshi] or [RPI] original specs.
	//
	// Differences (20–22):
	//   [Batocera]  /userdata/roms/<system>/media/wheel/<rom>.png
	//   [ES-DE]     ~/ES-DE/downloaded_media/<system>/logos/<rom>.png
	//               (stored in a "logos" folder but written to <wheel> in gamelist)
	//   [Sky]       does NOT emit <wheel>; logo art goes into <marquee> instead
	//   [RPI]       not officially supported; wheel images not scraped by default
	Wheel string `xml:"wheel,omitempty"`

	// FanArt is a full-screen background/fan art image.
	//
	// Tag: <fanart>   Not in [Aloshi] or original [RPI] specs.
	//
	// Differences (23–25):
	//   [Batocera]  /userdata/roms/<system>/media/fanart/<rom>.png
	//   [ES-DE]     ~/ES-DE/downloaded_media/<system>/fanart/<rom>.png
	//   [Sky]       can scrape fanart; emits <fanart> tag; not enabled by default
	//   [Aloshi/RPI] not in spec; field silently ignored
	FanArt string `xml:"fanart,omitempty"`

	// TitleShot is a screenshot of the game's title/attract screen.
	// Distinct from a gameplay screenshot which typically goes in <image>.
	//
	// IMPORTANT TAG NAME DIFFERENCE (26):
	//   [Batocera]  tag is <titleshot>
	//               /userdata/roms/<system>/media/titleshots/<rom>.png
	//   [ES-DE]     tag is <titlescreen>  ← different XML tag name entirely
	//               ~/ES-DE/downloaded_media/<system>/titlescreens/<rom>.png
	//   [Aloshi/RPI/Sky] not in spec
	TitleShot string `xml:"titleshot,omitempty"`

	// Manual is the path to a PDF or scanned-image game manual.
	//
	// Tag: <manual>   Not in [Aloshi] or [RPI] original specs.
	//
	// Differences (27–29):
	//   [Batocera]  /userdata/roms/<system>/media/manuals/<rom>.pdf
	//   [ES-DE]     ~/ES-DE/downloaded_media/<system>/manuals/<rom>.pdf
	//               same tag name, different base path
	//   [Sky]       scrapes PDF manuals; outputs to correct folder for Batocera
	//               and ES-DE backends; not enabled by default (--flags manuals)
	//   [RPI]       not in spec; no scraper support
	Manual string `xml:"manual,omitempty"`

	// Magazine is the path to a scanned gaming magazine scan/PDF.
	// Batocera-specific; no other fork defines or renders this field.
	//
	// Tag: <magazine>   [Batocera] only (difference 30).
	//   [Batocera]  /userdata/roms/<system>/media/magazines/<rom>.pdf
	//   [all others] not defined; silently ignored
	Magazine string `xml:"magazine,omitempty"`

	// Map is the path to an in-game world/level map image.
	//
	// Tag: <map>   Not in [Aloshi] or [RPI] original specs.
	//
	// Differences (31–32):
	//   [Batocera]  /userdata/roms/<system>/media/maps/<rom>.png
	//   [ES-DE]     ~/ES-DE/downloaded_media/<system>/maps/<rom>.png
	//               both use the same <map> tag; paths differ
	//   [Aloshi/RPI/Sky] not in spec
	Map string `xml:"map,omitempty"`

	// Bezel is the path to a bezel/overlay decoration image (letterbox frame).
	// [Batocera]'s tag targets 16:9 aspect ratio bezels.
	// Other forks configure bezels outside gamelist.xml entirely.
	//
	// Tag: <bezel>   [Batocera] only (difference 33).
	//   [Batocera]  /userdata/roms/<system>/media/bezel16-9/<rom>.png
	//   [RPI]       bezels configured via runcommand-onstart scripts, not gamelist
	//   [ES-DE]     uses a separate retroarch/bezel project; not a gamelist field
	Bezel string `xml:"bezel,omitempty"`

	// Cartridge is an image of the physical cartridge, disc, or cassette.
	//
	// IMPORTANT TAG NAME DIFFERENCE (34):
	//   [Batocera]  tag is <cartridge>
	//               /userdata/roms/<system>/media/cartridges/<rom>.png
	//   [ES-DE]     tag is <physicalmedia>  ← different XML tag name
	//               ~/ES-DE/downloaded_media/<system>/physicalmedia/<rom>.png
	//   [Aloshi/RPI/Sky] not in spec
	Cartridge string `xml:"cartridge,omitempty"`

	// BoxBack is an image of the back face of the game box.
	//
	// IMPORTANT TAG NAME DIFFERENCE (35):
	//   [Batocera]  tag is <boxback>
	//               /userdata/roms/<system>/media/backcovers/<rom>.png
	//   [ES-DE]     tag is <backcover>  ← different XML tag name
	//               ~/ES-DE/downloaded_media/<system>/backcovers/<rom>.png
	//   [Sky→Batocera] scrapes back covers; emits <boxback> for Batocera output
	//   [RPI/Aloshi] not in spec
	BoxBack string `xml:"boxback,omitempty"`

	// Mix is a pre-composited "mix image" combining box art, screenshot, and
	// logo into a single image (sometimes called "miximage").
	//
	// Tag: <mix>   Not in [Aloshi] or [RPI] original specs.
	//
	// Differences (36–38):
	//   [Batocera]  /userdata/roms/<system>/media/mixes/<rom>.png  tag: <mix>
	//   [Sky→RPI]   generates miximages but writes result to <image> not <mix>
	//               ./media/miximages/<rom>.png written into <image> tag
	//   [ES-DE]     generates miximages internally; does not expose via gamelist field
	//   [ARRM]      can generate mixes; writes to <mix> or to <image> depending on config
	Mix string `xml:"mix,omitempty"`

	// --- SCALAR METADATA FIELDS ---

	// Rating is the game's rating expressed as a float string between "0" and "1".
	// Example: "0.75" for a 3-star-out-of-4 rating.
	// Use ParseRating() to convert to float64.
	Rating string `xml:"rating,omitempty"`

	// ReleaseDate is the game's release date encoded as an ISO-style string:
	// "YYYYMMDDTHHMMSS" (e.g. "19950311T000000" for March 11, 1995).
	// Use ParseESDate() to convert to time.Time.
	// The time component is typically "000000" for dates without time info.
	ReleaseDate string `xml:"releasedate,omitempty"`

	Developer string `xml:"developer,omitempty"`
	Publisher  string `xml:"publisher,omitempty"`

	// Genre is the primary genre string.
	// [Aloshi/RPI/ES-DE/Sky] use a single <genre> element.
	// [Batocera] also supports a separate <genres> element (see Genres).
	Genre string `xml:"genre,omitempty"`

	// Genres is a multi-value genre string (e.g. "Action, Platform").
	// [Batocera] only. Distinct from the singular <genre> used by all other forks.
	Genres string `xml:"genres,omitempty"`

	// Players is stored as a string to accommodate single values ("1"),
	// ranges ("1-4"), or lists ("1, 2") used by different scrapers.
	Players string `xml:"players,omitempty"`

	// Tags is a freeform comma-separated tag string for additional categorization.
	// [Batocera] only.
	Tags string `xml:"tags,omitempty"`

	// Family is a "game family" grouping (e.g. all Zelda games).
	// [Batocera] only; XML tag is <family>.
	Family string `xml:"family,omitempty"`

	// ArcadeSystemName identifies the arcade PCB/hardware (e.g. "CPS-2").
	// [Batocera] only; XML tag is <arcadesystemname>.
	ArcadeSystemName string `xml:"arcadesystemname,omitempty"`

	// Emulator is the preferred emulator name for this specific game.
	// [Batocera] / [EmuELEC] only; overrides the system-level default.
	Emulator string `xml:"emulator,omitempty"`

	// Core is the preferred libretro core name for this game.
	// [Batocera] / [EmuELEC] only; overrides the system-level default.
	Core string `xml:"core,omitempty"`

	// Lang is the game's language code(s), e.g. "en" or "en,fr".
	// [Batocera] only; XML tag is <lang>.
	Lang string `xml:"lang,omitempty"`

	// Region is the game's region code, e.g. "us", "eu", "jp".
	// [Batocera] only; XML tag is <region>.
	Region string `xml:"region,omitempty"`

	// Source records which scraping database the metadata came from
	// (e.g. "screenscraper", "thegamesdb"). Written by some scrapers; not
	// defined in any official ES fork spec.
	Source string `xml:"source,omitempty"`

	// --- CHECKSUM / DATABASE ID FIELDS ---

	// CRC32 is the ROM file's CRC32 checksum used for Screenscraper matching.
	// [Batocera] only; XML tag is <crc32>.
	CRC32 string `xml:"crc32,omitempty"`

	// MD5 is the ROM file's MD5 checksum used for Screenscraper matching.
	// [Batocera] only; XML tag is <md5>.
	MD5 string `xml:"md5,omitempty"`

	// MultiDisk holds multi-disc grouping metadata (e.g. disc number info).
	// [Batocera] only; XML tag is <multidisk>.
	MultiDisk string `xml:"multidisk,omitempty"`

	// CheevosHash is the RetroAchievements ROM hash for achievement linking.
	// [Batocera] only; XML tag is <cheevosHash>.
	CheevosHash string `xml:"cheevosHash,omitempty"`

	// CheevosID is the RetroAchievements numeric game ID.
	// [Batocera] only; XML tag is <cheevosId>.
	CheevosID int `xml:"cheevosId,omitempty"`

	// ScreenScraperID is the Screenscraper.fr numeric game ID stored as a
	// child element. [Batocera] MetaData.cpp defines this as tag <id>.
	// Some scrapers also write it as an XML attribute (see ScreenScraperIDAttr).
	// If both are present, this element value takes precedence for ES.
	ScreenScraperID int `xml:"id,omitempty"`

	// --- BOOLEAN STATUS / FILTER FIELDS ---

	// Favorite marks the game as a user favourite for filtered browsing.
	// Stored in XML as "true" or "false".
	// Supported by [RPI], [Batocera], [ES-DE], [Recalbox], [AmberELEC].
	Favorite bool `xml:"favorite,omitempty"`

	// Hidden excludes the game from normal browsing (record still exists in XML).
	// ES shows hidden games only when "Show Hidden" is enabled.
	// Supported by [RPI], [Batocera], [ES-DE], [Recalbox].
	Hidden bool `xml:"hidden,omitempty"`

	// KidGame marks the game as appropriate for kid-safe/parental filter mode.
	// Supported by [RPI], [Batocera], [ES-DE].
	KidGame bool `xml:"kidgame,omitempty"`

	// --- STATISTICS (written by ES at runtime) ---

	// PlayCount is the number of times ES has launched this game.
	PlayCount int `xml:"playcount,omitempty"`

	// LastPlayed is the timestamp of the most recent launch.
	// Same datetime format as ReleaseDate: "YYYYMMDDTHHMMSS".
	// Use ParseESDate() to convert.
	LastPlayed string `xml:"lastplayed,omitempty"`

	// GameTime is the total number of seconds the game has been played.
	// [Batocera] only; XML tag is <gametime>.
	GameTime int `xml:"gametime,omitempty"`
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
		return time.Time{}, fmt.Errorf("empty datetime string")
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
		return 0, fmt.Errorf("empty rating string")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing rating %q: %w", s, err)
	}
	return f, nil
}
