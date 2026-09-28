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

// Command scummvmnames builds pkg/database/scummvmnames/names.tsv.gz from
// the game list a ScummVM release prints with `scummvm --list-games`. Each
// line keeps the engine, the game ID and ScummVM's full title for it.
//
// Usage:
//
//	SDL_VIDEODRIVER=dummy scummvm --list-games > games.txt
//	go run ./scripts/scummvmnames 2.9.1 games.txt pkg/database/scummvmnames/names.tsv.gz
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

type game struct {
	engine, id, title string
}

// parse reads the two-column table: a "Game ID" header, a dashed rule whose
// first run sets the ID column's width, then "engine:gameid   Title" rows.
func parse(data []byte) ([]game, error) {
	var games []game
	width := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \r")
		if width == 0 {
			if strings.HasPrefix(line, "---") {
				width = strings.Index(line, " ")
			}
			continue
		}
		if len(line) <= width {
			continue
		}
		engine, id, ok := strings.Cut(strings.TrimSpace(line[:width]), ":")
		title := strings.TrimSpace(line[width:])
		if !ok || engine == "" || id == "" || title == "" || strings.ContainsAny(title, "\t\n") {
			continue
		}
		games = append(games, game{engine: engine, id: strings.ToLower(id), title: title})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read game list: %w", err)
	}
	if len(games) == 0 {
		return nil, errors.New("no games found; expected `scummvm --list-games` output")
	}
	return games, nil
}

func run(version, input, output string) error {
	data, err := os.ReadFile(input) //nolint:gosec // developer-supplied input
	if err != nil {
		return fmt.Errorf("read %s: %w", input, err)
	}
	games, err := parse(data)
	if err != nil {
		return err
	}
	slices.SortFunc(games, func(a, b game) int {
		if c := strings.Compare(a.id, b.id); c != 0 {
			return c
		}
		return strings.Compare(a.engine, b.engine)
	})
	games = slices.CompactFunc(games, func(a, b game) bool { return a.id == b.id && a.engine == b.engine })
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	if _, err := fmt.Fprintf(zw, "# ScummVM %s\n", version); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	for _, g := range games {
		if _, err := fmt.Fprintf(zw, "%s\t%s\t%s\n", g.id, g.engine, g.title); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	if err := os.WriteFile(output, buf.Bytes(), 0o644); err != nil { //nolint:gosec // checked-in data file
		return fmt.Errorf("write %s: %w", output, err)
	}
	_, _ = fmt.Printf("%d games from ScummVM %s\n", len(games), version)
	return nil
}

func main() {
	if len(os.Args) != 4 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: scummvmnames VERSION GAMES.txt OUTPUT.tsv.gz")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
