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
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/afero"
)

// ESDateFormat is the strftime-style format EmulationStation uses for
// releasedate and lastplayed: "%Y%m%dT%H%M%S". In Go's time package this
// translates to the layout below. Some scrapers omit the time component and
// write only the date portion ("19950311T000000").
const (
	ESDateFormat = "20060102T150405"
	// MaxGameListXMLSize bounds the bytes read from one gamelist.xml. The file
	// is decoded as a stream, so this guards against a single oversized element
	// rather than sizing a buffer; MaxGameListEntries is the limit a large but
	// ordinary library reaches first.
	MaxGameListXMLSize = 128 << 20
	// MaxGameListXMLSizeConstrained is the limit for platforms that report
	// ResourceConstrained. Decoded entries stay in memory for the whole scrape,
	// and a list near MaxGameListXMLSize costs hundreds of MB — more than a
	// MiSTer has, on top of the indexes a scrape already builds. Such a device
	// is told its file is too large rather than being pushed into swapless
	// thrashing; its artwork can still be imported from media folders.
	MaxGameListXMLSizeConstrained = 16 << 20
	MaxGameListEntries            = 100_000
	MaxGameListXMLDepth           = 64
)

var (
	ErrGameListTooLarge     = errors.New("gamelist.xml exceeds size limit")
	ErrGameListTooManyItems = errors.New("gamelist.xml exceeds entry limit")
	ErrGameListTooDeep      = errors.New("gamelist.xml exceeds XML depth limit")
	// ErrGameListInvalidRoot covers every way a file fails to be a gameList
	// document at all: no root element, a different root, or content outside
	// it. Each wraps this sentinel with the detail, so a caller reporting the
	// failure to a user has one condition to name.
	ErrGameListInvalidRoot = errors.New("file is not an EmulationStation game list")
)

// GameListTooLargeError reports the limit that a gamelist.xml exceeded, which
// differs by platform. It satisfies errors.Is(err, ErrGameListTooLarge).
type GameListTooLargeError struct {
	Limit int64
}

func (e *GameListTooLargeError) Error() string {
	if e.Limit < 1<<20 {
		return fmt.Sprintf("file is larger than the %d byte limit", e.Limit)
	}
	return fmt.Sprintf("file is larger than the %d MB limit", e.Limit>>20)
}

func (*GameListTooLargeError) Unwrap() error {
	return ErrGameListTooLarge
}

// GameList is the root element of an EmulationStation gamelist.xml file.
// It may contain any mix of <game> and <folder> children.
type GameList struct {
	XMLName xml.Name `xml:"gameList"`
	Games   []Game   `xml:"game"`
	Folders []Folder `xml:"folder"`
	// Skipped counts <game> and <folder> entries dropped because one of their
	// typed fields held a value that does not parse, such as a non-numeric
	// <playcount>. The rest of the document is still decoded.
	Skipped int `xml:"-"`
}

// GameReference is the lightweight gamelist subset needed during media discovery.
type GameReference struct {
	Name string `xml:"name"`
	Path string `xml:"path"`
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
	Players             string   `xml:"players,omitempty"`
	Logo                string   `xml:"logo,omitempty"`
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
	ArcadeSystemName    string   `xml:"arcadesystemname,omitempty"`
	Path                string   `xml:"path"`
	Publisher           string   `xml:"publisher,omitempty"`
	LastPlayed          string   `xml:"lastplayed,omitempty"`
	Developer           string   `xml:"developer,omitempty"`
	Bezel               string   `xml:"bezel,omitempty"`
	Tags                string   `xml:"tags,omitempty"`
	ScreenScraperIDAttr string   `xml:"id,attr,omitempty"`
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
	SourceAttr          string   `xml:"source,attr,omitempty"` //nolint:revive // attr and el
	ParentIDAttr        string   `xml:"parentid,attr,omitempty"`
	Screenshot          string   `xml:"screenshot,omitempty"`
	TitleScreen         string   `xml:"titlescreen,omitempty"`
	Boxart2D            string   `xml:"boxart2d,omitempty"`
	Family              string   `xml:"family,omitempty"`
	Boxart3D            string   `xml:"boxart3d,omitempty"`
	PlayCount           int      `xml:"playcount,omitempty"`
	ScreenScraperID     int      `xml:"id,omitempty"` //nolint:revive // attr and el
	CheevosID           int      `xml:"cheevosId,omitempty"`
	GameTime            int      `xml:"gametime,omitempty"`
	Favorite            bool     `xml:"favorite,omitempty"`
	Hidden              bool     `xml:"hidden,omitempty"`
	KidGame             bool     `xml:"kidgame,omitempty"`
}

// UnmarshalXML accepts the ZapScraper box2d alias while keeping boxart2d as
// the canonical field and serialized tag. An explicit canonical value wins.
func (g *Game) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type GameXML Game
	var decoded struct {
		Box2D string `xml:"box2d"`
		GameXML
	}
	if err := d.DecodeElement(&decoded, &start); err != nil {
		return fmt.Errorf("decode gamelist game: %w", err)
	}
	if decoded.Boxart2D == "" {
		decoded.Boxart2D = decoded.Box2D
	}
	*g = Game(decoded.GameXML)
	return nil
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

// ReadGameListXML opens and decodes a full EmulationStation gamelist.xml file.
func ReadGameListXML(path string) (GameList, error) {
	return ReadGameListXMLFS(afero.NewOsFs(), path)
}

// ReadGameListXMLFS decodes a full gamelist.xml through the supplied filesystem.
func ReadGameListXMLFS(fs afero.Fs, path string) (GameList, error) {
	return ReadGameListXMLLimitFS(fs, path, MaxGameListXMLSize)
}

// ReadGameListXMLLimitFS decodes a full gamelist.xml, reading at most maxBytes.
// Callers on memory-constrained platforms pass MaxGameListXMLSizeConstrained so
// an oversized list is refused with a clear reason instead of being decoded into
// memory the device does not have.
func ReadGameListXMLLimitFS(fs afero.Fs, path string, maxBytes int64) (GameList, error) {
	file, cleanPath, err := openGameListFS(fs, path, maxBytes)
	if err != nil {
		return GameList{}, err
	}
	defer file.Close() //nolint:errcheck // Read-only file; close errors do not affect parsed data.
	gameList, err := decodeGameList(file, maxBytes)
	if err != nil {
		return GameList{}, fmt.Errorf("failed to unmarshal gamelist XML file %s: %w", cleanPath, err)
	}
	return gameList, nil
}

// ReadGameReferencesXML decodes only names and paths needed during discovery.
func ReadGameReferencesXML(path string) ([]GameReference, error) {
	return ReadGameReferencesXMLFS(afero.NewOsFs(), path)
}

// ReadGameReferencesXMLFS decodes lightweight game references through the supplied filesystem.
func ReadGameReferencesXMLFS(fs afero.Fs, path string) ([]GameReference, error) {
	file, cleanPath, err := openGameListFS(fs, path, MaxGameListXMLSize)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // Read-only file; close errors do not affect parsed data.
	references, err := decodeGameReferences(file, MaxGameListXMLSize)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal gamelist XML file %s: %w", cleanPath, err)
	}
	return references, nil
}

// decodeGameReferences walks the same document as decodeGameList but keeps only
// the name and path of each <game>, which is all media discovery needs.
func decodeGameReferences(r io.Reader, maxBytes int64) ([]GameReference, error) {
	var references []GameReference
	_, err := streamGameList(r, maxBytes, gameListVisitor{
		game: func(d *xml.Decoder, start *xml.StartElement) error {
			var reference GameReference
			if decodeErr := d.DecodeElement(&reference, start); decodeErr != nil {
				return fmt.Errorf("decode gamelist game reference: %w", decodeErr)
			}
			references = append(references, reference)
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return references, nil
}

// ParseGameListXML validates and decodes a full gamelist.xml document.
func ParseGameListXML(data []byte) (GameList, error) {
	if len(data) > MaxGameListXMLSize {
		return GameList{}, &GameListTooLargeError{Limit: MaxGameListXMLSize}
	}
	return decodeGameList(bytes.NewReader(data), MaxGameListXMLSize)
}

// ValidateGameListXML enforces shared size, depth, root, and entry limits.
func ValidateGameListXML(data []byte) error {
	if len(data) > MaxGameListXMLSize {
		return &GameListTooLargeError{Limit: MaxGameListXMLSize}
	}
	_, err := streamGameList(bytes.NewReader(data), MaxGameListXMLSize, gameListVisitor{})
	return err
}

func decodeGameList(r io.Reader, maxBytes int64) (GameList, error) {
	var gameList GameList
	root, err := streamGameList(r, maxBytes, gameListVisitor{
		game: func(d *xml.Decoder, start *xml.StartElement) error {
			var game Game
			if decodeErr := d.DecodeElement(&game, start); decodeErr != nil {
				return decodeErr //nolint:wrapcheck // Game.UnmarshalXML already adds context.
			}
			gameList.Games = append(gameList.Games, game)
			return nil
		},
		folder: func(d *xml.Decoder, start *xml.StartElement) error {
			var folder Folder
			if decodeErr := d.DecodeElement(&folder, start); decodeErr != nil {
				return fmt.Errorf("decode gamelist folder: %w", decodeErr)
			}
			gameList.Folders = append(gameList.Folders, folder)
			return nil
		},
	})
	if err != nil {
		return GameList{}, err
	}
	gameList.XMLName = root.name
	gameList.Skipped = root.skipped
	return gameList, nil
}

// gameListVisitor decodes the direct children of <gameList>. A nil func leaves
// that kind of entry undecoded; it is still counted and validated.
type gameListVisitor struct {
	game   func(d *xml.Decoder, start *xml.StartElement) error
	folder func(d *xml.Decoder, start *xml.StartElement) error
}

type gameListRoot struct {
	name    xml.Name
	skipped int
}

// streamGameList walks one gamelist.xml document without holding it in memory,
// enforcing the size, depth, root, and entry limits as it goes and handing each
// <game> and <folder> to the visitor. An entry whose typed field does not parse
// is skipped and counted; malformed XML and exceeded limits fail the document.
func streamGameList(r io.Reader, maxBytes int64, visitor gameListVisitor) (gameListRoot, error) {
	var root gameListRoot
	buffered := bufio.NewReader(&cappedReader{r: r, limit: maxBytes, remaining: maxBytes})
	// Windows scrapers may emit a UTF-8 BOM. It is an encoding signature only
	// at byte zero, not whitespace we should tolerate elsewhere in the document.
	if bom, err := buffered.Peek(len(utf8BOM)); err == nil && bytes.Equal(bom, utf8BOM) {
		if _, err := buffered.Discard(len(utf8BOM)); err != nil {
			return root, fmt.Errorf("discard gamelist XML byte order mark: %w", err)
		}
	}

	guard := &gameListTokenGuard{decoder: xml.NewDecoder(buffered)}
	for {
		token, err := guard.Token()
		if errors.Is(err, io.EOF) {
			if !guard.sawRoot {
				return root, fmt.Errorf("%w: missing gameList root element", ErrGameListInvalidRoot)
			}
			return root, nil
		}
		if err != nil {
			return root, fmt.Errorf("decode gamelist XML token: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if guard.depth == 1 {
			root.name = start.Name
			continue
		}
		if guard.depth != 2 {
			continue
		}
		visit := visitor.game
		if start.Name.Local == "folder" {
			visit = visitor.folder
		} else if start.Name.Local != "game" {
			continue
		}
		if visit == nil {
			continue
		}
		skipped, err := visitGameListEntry(guard, &start, visit)
		if err != nil {
			return root, err
		}
		if skipped {
			root.skipped++
		}
	}
}

// visitGameListEntry decodes one depth-2 entry on a decoder of its own. A
// failed DecodeElement leaves encoding/xml's element stack holding the marker a
// custom UnmarshalXML pushed, after which that decoder reports EOF at the
// entry's end tag; sharing one decoder across entries would turn a single bad
// value into a silently truncated document.
func visitGameListEntry(
	guard *gameListTokenGuard,
	start *xml.StartElement,
	visit func(d *xml.Decoder, start *xml.StartElement) error,
) (skipped bool, err error) {
	decoder := xml.NewTokenDecoder(&gameListEntryTokens{guard: guard, start: start})
	if _, err := decoder.Token(); err != nil {
		return false, fmt.Errorf("decode gamelist XML token: %w", err)
	}
	if err := visit(decoder, start); err != nil {
		// A value strconv rejects leaves the XML stream itself intact, so the
		// rest of this entry can be drained and the document carried on.
		var numErr *strconv.NumError
		if !errors.As(err, &numErr) {
			return false, err
		}
		skipped = true
	}
	for guard.depth > 1 {
		if _, err := guard.Token(); err != nil {
			return false, fmt.Errorf("decode gamelist XML token: %w", err)
		}
	}
	return skipped, nil
}

// gameListEntryTokens replays an entry's start element and then the guarded
// document tokens up to that entry's end element.
type gameListEntryTokens struct {
	guard   *gameListTokenGuard
	start   *xml.StartElement
	started bool
}

func (e *gameListEntryTokens) Token() (xml.Token, error) {
	if !e.started {
		e.started = true
		return *e.start, nil
	}
	if e.guard.depth <= 1 {
		return nil, io.EOF
	}
	return e.guard.Token()
}

var utf8BOM = []byte("\xef\xbb\xbf")

// gameListTokenGuard applies the document limits to every token, including the
// ones a DecodeElement call consumes on behalf of a visitor. The decoder it
// wraps checks well-formedness, so tag matching is validated here too.
type gameListTokenGuard struct {
	decoder *xml.Decoder
	depth   int
	entries int
	sawRoot bool
}

func (g *gameListTokenGuard) Token() (xml.Token, error) {
	token, err := g.decoder.Token()
	if err != nil {
		return nil, err //nolint:wrapcheck // The outer decoder must see io.EOF and syntax errors unchanged.
	}
	switch value := token.(type) {
	case xml.StartElement:
		g.depth++
		if g.depth > MaxGameListXMLDepth {
			return nil, ErrGameListTooDeep
		}
		if g.depth == 1 {
			if g.sawRoot || value.Name.Local != "gameList" {
				return nil, fmt.Errorf("%w: invalid gameList root element", ErrGameListInvalidRoot)
			}
			g.sawRoot = true
		}
		if g.depth == 2 && (value.Name.Local == "game" || value.Name.Local == "folder") {
			g.entries++
			if g.entries > MaxGameListEntries {
				return nil, ErrGameListTooManyItems
			}
		}
	case xml.EndElement:
		g.depth--
	case xml.CharData:
		if g.depth == 0 && len(bytes.TrimSpace(value)) > 0 {
			return nil, fmt.Errorf("%w: character data outside gameList root element", ErrGameListInvalidRoot)
		}
	}
	return token, nil
}

// cappedReader fails with ErrGameListTooLarge once the source holds more than
// the allowed number of bytes, without reading the remainder.
type cappedReader struct {
	r         io.Reader
	limit     int64
	remaining int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if c.remaining <= 0 {
		var probe [1]byte
		n, err := c.r.Read(probe[:])
		if n > 0 {
			return 0, &GameListTooLargeError{Limit: c.limit}
		}
		return 0, err //nolint:wrapcheck // io.Reader contract: io.EOF must pass through unchanged.
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.r.Read(p)
	c.remaining -= int64(n)
	return n, err //nolint:wrapcheck // io.Reader contract: io.EOF must pass through unchanged.
}

func openGameListFS(fs afero.Fs, path string, maxBytes int64) (afero.File, string, error) {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Join(".", cleanPath)
	}
	file, err := fs.Open(cleanPath)
	if err != nil {
		return nil, cleanPath, fmt.Errorf("failed to open gamelist XML file %s: %w", cleanPath, err)
	}
	// The stream is capped as well; a known size just fails before parsing.
	if info, statErr := file.Stat(); statErr == nil && info.Size() > maxBytes {
		_ = file.Close()
		return nil, cleanPath, fmt.Errorf("failed to read gamelist XML file %s: %w",
			cleanPath, &GameListTooLargeError{Limit: maxBytes})
	}
	return file, cleanPath, nil
}

// ParseESDate parses an EmulationStation datetime string into a time.Time.
// ES stores dates as "YYYYMMDDTHHMMSS" (e.g. "19950311T000000") using ESDateFormat.
// Zone-less ES timestamps are treated as UTC (time.Parse with a layout that has no
// timezone produces a time with UTC location). Callers that need local-time semantics
// must convert the result with time.In or time.ParseInLocation.
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
// a float64. Returns 0 and an error if the string is empty, malformed, or outside [0, 1].
func ParseRating(s string) (float64, error) {
	if s == "" {
		return 0, errors.New("empty rating string")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing rating %q: %w", s, err)
	}
	if f < 0 || f > 1 {
		return 0, fmt.Errorf("rating out of range: %q is not in [0, 1]", s)
	}
	return f, nil
}
