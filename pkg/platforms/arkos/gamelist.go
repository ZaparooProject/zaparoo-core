//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"encoding/xml"
	"errors"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// esExeDir is where ArkOS installs the ES-fcamod binary; ES looks for
	// its resources and event scripts beside it.
	esExeDir = "/usr/bin/emulationstation"
	// systemGamelistDir holds the gamelists ES falls back to when neither the
	// ROM folder nor the user has one.
	systemGamelistDir = "/etc/emulationstation/gamelists"
	// maxGamelistSize bounds the gamelist and MAME name files read for a
	// launch. Scraped gamelists with descriptions run to a few MiB.
	maxGamelistSize = 64 << 20
)

// gameMeta is the part of a game's ES metadata that changes how it launches.
type gameMeta struct {
	name     string
	emulator string
	core     string
	governor string
}

// gamelistEntry is a <game> element. ES-fcamod reads each value as the
// element's raw text.
type gamelistEntry struct {
	Path     string `xml:"path"`
	Name     string `xml:"name"`
	Emulator string `xml:"emulator"`
	Core     string `xml:"core"`
	Governor string `xml:"governor"`
}

// esStartPath is the system's ROM folder as ES-fcamod resolves it, with a
// leading "~" expanded to the frontend home.
func esStartPath(sys *esSystem, home string) string {
	p := strings.TrimSpace(sys.Path)
	if strings.HasPrefix(p, "~") {
		p = home + p[1:]
	}
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// gamelistPath mirrors SystemData::getGamelistPath for reading: the ROM
// folder's gamelist.xml, then the user's, then the system-wide one.
func gamelistPath(fs afero.Fs, sys *esSystem, startPath, home string) string {
	candidates := []string{
		filepath.Join(startPath, "gamelist.xml"),
		filepath.Join(home, ".emulationstation", "gamelists", sys.Name, "gamelist.xml"),
	}
	for _, c := range candidates {
		if _, err := fs.Stat(c); err == nil {
			return c
		}
	}
	return filepath.Join(systemGamelistDir, sys.Name, "gamelist.xml")
}

// loadGameMeta reads a game's metadata the way parseGamelist does: the
// system's gamelist, then the game's file in the recovery folder, which ES
// writes when a game's options are edited and which replaces the gamelist
// entry while the gamelist is unchanged since. ES only knows games under the
// system's own folder, so a ROM elsewhere has no metadata.
func loadGameMeta(fs afero.Fs, sys *esSystem, romPath, home string) gameMeta {
	startPath := esStartPath(sys, home)
	if startPath == "" || !helpers.PathHasPrefix(romPath, startPath) || romPath == startPath {
		return gameMeta{}
	}

	var meta gameMeta
	listPath := gamelistPath(fs, sys, startPath, home)
	var listSize int64
	if info, err := fs.Stat(listPath); err == nil {
		listSize = info.Size()
	}
	if listSize > 0 {
		if entry, ok := findGamelistEntry(fs, listPath, startPath, romPath, nil); ok {
			meta = entry
		}
	}

	rel, err := filepath.Rel(startPath, romPath)
	if err != nil {
		return meta
	}
	recovery := filepath.Join(home, ".emulationstation", "recovery", sys.Name,
		strings.TrimSuffix(rel, filepath.Ext(rel))+".xml")
	if entry, ok := findGamelistEntry(fs, recovery, startPath, romPath, &listSize); ok {
		meta = entry
	}
	return meta
}

// findGamelistEntry returns the metadata of the last <game> in a gamelist
// whose path resolves to romPath. With parentHash set, the file only counts
// when its gameList element carries that parentHash, as recovery files must.
func findGamelistEntry(
	fs afero.Fs, listPath, startPath, romPath string, parentHash *int64,
) (gameMeta, bool) {
	f, err := fs.Open(listPath)
	if err != nil {
		return gameMeta{}, false
	}
	defer func() { _ = f.Close() }()

	dec := xml.NewDecoder(io.LimitReader(f, maxGamelistSize))
	dec.Strict = false
	var (
		meta   gameMeta
		found  bool
		inList bool
	)
	for {
		tok, err := dec.Token()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Debug().Err(err).Str("path", listPath).Msg("cannot read ES gamelist")
				return gameMeta{}, false
			}
			return meta, found
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !inList {
			if start.Name.Local != "gameList" {
				if err := dec.Skip(); err != nil {
					return gameMeta{}, false
				}
				continue
			}
			if parentHash != nil && attrUint(start.Attr, "parentHash") != *parentHash {
				return gameMeta{}, false
			}
			inList = true
			continue
		}
		var entry gamelistEntry
		if err := dec.DecodeElement(&entry, &start); err != nil {
			log.Debug().Err(err).Str("path", listPath).Msg("cannot read ES gamelist")
			return gameMeta{}, false
		}
		if start.Name.Local != "game" {
			continue
		}
		if resolveGamelistPath(entry.Path, startPath) != romPath {
			continue
		}
		meta = gameMeta{
			name:     entry.Name,
			emulator: entry.Emulator,
			core:     entry.Core,
			governor: entry.Governor,
		}
		found = true
	}
}

// resolveGamelistPath mirrors Utils::FileSystem::resolveRelativePath for
// gamelist entries: "./" is relative to the system folder.
func resolveGamelistPath(p, startPath string) string {
	switch {
	case p == "":
		return ""
	case p == ".":
		return startPath
	case strings.HasPrefix(p, "./"):
		return filepath.Clean(startPath + p[1:])
	default:
		return filepath.Clean(p)
	}
}

// attrUint reads an unsigned attribute as pugixml's as_uint does, with 0 for
// a missing or malformed value.
func attrUint(attrs []xml.Attr, name string) int64 {
	for _, a := range attrs {
		if a.Name.Local != name {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSpace(a.Value), 10, 32)
		if err != nil {
			return 0
		}
		return int64(v)
	}
	return 0
}

// esPlatforms splits a system's <platform> list as ES's readList does.
func esPlatforms(sys *esSystem) []string {
	return strings.FieldsFunc(sys.Platform, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == ','
	})
}

// isArcadeSystem reports whether ES treats the system's games as MAME sets,
// naming them from mamenames.xml.
func isArcadeSystem(sys *esSystem) bool {
	platforms := esPlatforms(sys)
	return slices.Contains(platforms, "arcade") || slices.Contains(platforms, "neogeo")
}

// esGameName mirrors FileData::getName, which keys the "<name>.governor"
// setting: the file stem when ShowFilenames is on (the MAME title on arcade
// systems), otherwise the gamelist name, falling back to the display name.
func esGameName(
	fs afero.Fs, sys *esSystem, settings map[string]string, meta gameMeta, romPath, home string,
) string {
	stem := strings.TrimSuffix(path.Base(romPath), path.Ext(romPath))
	arcade := isArcadeSystem(sys)
	displayName := func() string {
		if arcade {
			return mameRealName(fs, home, stem)
		}
		return stem
	}
	if settings["ShowFilenames"] == "true" {
		return displayName()
	}
	if meta.name != "" {
		return meta.name
	}
	return displayName()
}

// mameRealName looks a MAME set name up in ES's mamenames.xml, found the way
// ResourceManager::getResourcePath does, and returns the set name when there
// is no entry.
func mameRealName(fs afero.Fs, home, mameName string) string {
	for _, p := range []string{
		filepath.Join(home, ".emulationstation", "resources", "mamenames.xml"),
		filepath.Join(esExeDir, "resources", "mamenames.xml"),
	} {
		if _, err := fs.Stat(p); err != nil {
			continue
		}
		if name, ok := findMameName(fs, p, mameName); ok {
			return name
		}
		return mameName
	}
	return mameName
}

func findMameName(fs afero.Fs, p, mameName string) (string, bool) {
	f, err := fs.Open(p)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()

	dec := xml.NewDecoder(io.LimitReader(f, maxGamelistSize))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", false
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "game" {
			continue
		}
		var game struct {
			MameName string `xml:"mamename"`
			RealName string `xml:"realname"`
		}
		if err := dec.DecodeElement(&game, &start); err != nil {
			return "", false
		}
		if game.MameName == mameName {
			return game.RealName, true
		}
	}
}
