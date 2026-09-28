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

// Package scummvmnames names ScummVM games. A ScummVM launch file is named
// after the game's ScummVM ID ("sky.scummvm"), which is neither a readable
// title nor what libretro and ES-style scrapers key their artwork by. The
// embedded catalog maps IDs to ScummVM's own full titles. It is generated
// from a ScummVM release's game list by scripts/scummvmnames; see LICENSE in
// this directory for its terms.
package scummvmnames

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strings"
	"sync"
	"unicode"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

//go:embed names.tsv.gz
var embedded []byte

type game struct {
	engine string
	title  string
}

var (
	loadOnce sync.Once
	games    map[string][]game
	version  string
)

func load() {
	games = make(map[string][]game)
	zr, err := gzip.NewReader(bytes.NewReader(embedded))
	if err != nil {
		log.Error().Err(err).Msg("ScummVM names catalog unreadable")
		return
	}
	defer func() { _ = zr.Close() }()
	scanner := bufio.NewScanner(zr)
	for scanner.Scan() {
		line := scanner.Text()
		if rest, ok := strings.CutPrefix(line, "# ScummVM "); ok {
			version = rest
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[0] == "" || fields[2] == "" {
			continue
		}
		games[fields[0]] = append(games[fields[0]], game{engine: fields[1], title: fields[2]})
	}
	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("ScummVM names catalog truncated")
	}
}

// Title finds ScummVM's full title for a game ID, ignoring case. An
// "engine:gameid" target names one game exactly. A bare ID that several
// engines use goes to the title sharing the most words with hint, such as
// the name of the folder the game sits in; ties keep the catalog's order.
// The first call loads the catalog (about 11,700 games).
func Title(target, hint string) (string, bool) {
	loadOnce.Do(load)
	target = strings.ToLower(strings.TrimSpace(target))
	engine, id, qualified := strings.Cut(target, ":")
	if !qualified {
		id, engine = engine, ""
	}
	candidates := games[id]
	if engine != "" {
		for _, g := range candidates {
			if g.engine == engine {
				return g.title, true
			}
		}
		return "", false
	}
	switch len(candidates) {
	case 0:
		return "", false
	case 1:
		return candidates[0].title, true
	}
	wanted := words(hint)
	best, bestScore := candidates[0].title, -1
	for _, g := range candidates {
		score := 0
		for word := range words(g.title) {
			if _, ok := wanted[word]; ok {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = g.title, score
		}
	}
	return best, true
}

// Version is the ScummVM release the catalog was generated from.
func Version() string {
	loadOnce.Do(load)
	return version
}

// IsTargetFile reports whether a file is a ScummVM launch file: a
// ".scummvm" file on the ScummVM system, named after the game it starts.
func IsTargetFile(systemID, ext string) bool {
	return systemID == systemdefs.SystemScummVM && strings.EqualFold(ext, ".scummvm")
}

// LooksLikeID reports whether a file name has the shape of a ScummVM game
// ID (lowercase letters, digits, "-" and "_") rather than a readable title.
func LooksLikeID(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func words(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for word := range strings.FieldsFuncSeq(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		out[word] = struct{}{}
	}
	return out
}
