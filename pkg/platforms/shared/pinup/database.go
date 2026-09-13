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

package pinup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	_ "github.com/mattn/go-sqlite3" // SQLite driver for Popper's database.
	"github.com/rs/zerolog/log"
)

const (
	maxTables      = 100_000
	maxFieldLength = 4096
	queryTimeout   = 5 * time.Second
)

// ErrTooManyTables is returned when the database holds more visible tables
// than Core is willing to index.
var ErrTooManyTables = errors.New("PinUP Popper database exceeds the table limit")

// ReadLibrary loads the launchable tables from a Popper database. The file is
// opened read-only with a short busy timeout because Popper and its setup tool
// write to it while they run.
func ReadLibrary(ctx context.Context, dbPath string) (Library, error) {
	info, err := os.Stat(dbPath)
	if err != nil {
		return Library{}, fmt.Errorf("stat PinUP Popper database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Library{}, errors.New("PinUP Popper database path is not a regular file")
	}

	db, err := sql.Open("sqlite3", readOnlyDSN(dbPath))
	if err != nil {
		return Library{}, fmt.Errorf("open PinUP Popper database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close PinUP Popper database")
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	emulators, err := readEmulators(ctx, db)
	if err != nil {
		return Library{}, err
	}
	tables, err := readTables(ctx, db, emulators)
	if err != nil {
		return Library{}, err
	}
	log.Debug().Int("emulators", len(emulators)).Int("tables", len(tables)).Msg("read PinUP Popper library")
	return Library{Emulators: emulators, Tables: tables}, nil
}

// readOnlyDSN builds a SQLite URI that opens the file read-only. SQLite's URI
// form needs a rooted, forward-slash path even on Windows, so a drive path
// becomes file:///C:/... rather than a URL whose authority is the drive. The
// backslashes are replaced explicitly rather than through filepath so the same
// code, and its test, behave the same on every platform.
func readOnlyDSN(dbPath string) string {
	path := strings.ReplaceAll(dbPath, `\`, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_busy_timeout=1000"}).String()
}

// readEmulators returns the visible emulators that run pinball tables, keyed
// by EMUID. Others are logged so a user can see why their games are absent.
func readEmulators(ctx context.Context, db *sql.DB) (map[int]Emulator, error) {
	gamesDirColumn := "''"
	var gamesDirColumns int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('Emulators') WHERE name = 'DirGames'",
	).Scan(&gamesDirColumns); err == nil && gamesDirColumns > 0 {
		gamesDirColumn = "DirGames"
	}
	//nolint:gosec // Column expression is selected from fixed local constants.
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT EMUID, COALESCE(Visible, 1),
			substr(COALESCE(EmuName, ''), 1, ?), substr(COALESCE(EmuDisplay, ''), 1, ?),
			substr(COALESCE(DirMedia, ''), 1, ?), substr(COALESCE(%s, ''), 1, ?),
			substr(COALESCE(GamesExt, ''), 1, ?), substr(COALESCE(LaunchScript, ''), 1, ?),
			substr(COALESCE(ProcessName, ''), 1, ?),
			substr(COALESCE(WinTitle, ''), 1, ?)
		FROM Emulators`, gamesDirColumn),
		maxFieldLength, maxFieldLength, maxFieldLength, maxFieldLength,
		maxFieldLength, maxFieldLength, maxFieldLength, maxFieldLength,
	)
	if err != nil {
		return nil, fmt.Errorf("query PinUP Popper emulators: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close PinUP Popper emulator rows")
		}
	}()

	emulators := make(map[int]Emulator)
	for rows.Next() {
		var (
			id      sql.NullInt64
			visible sql.NullInt64
			e       Emulator
		)
		if err := rows.Scan(
			&id, &visible, &e.Name, &e.Display, &e.MediaDir, &e.GamesDir, &e.GamesExt,
			&e.LaunchScript, &e.ProcessName, &e.WindowTitle,
		); err != nil {
			return nil, fmt.Errorf("scan PinUP Popper emulator row: %w", err)
		}
		if !id.Valid || id.Int64 <= 0 || id.Int64 > maxGameID {
			continue
		}
		e.ID = int(id.Int64)
		e.Visible = visible.Int64 == 1
		e.Name = cleanField(e.Name)
		e.Display = cleanField(e.Display)
		e.ProcessName = cleanField(e.ProcessName)
		e.WindowTitle = cleanField(e.WindowTitle)
		e.MediaDir = strings.TrimSpace(e.MediaDir)
		e.GamesDir = strings.TrimSpace(e.GamesDir)
		e.GamesExt = strings.TrimSpace(e.GamesExt)
		switch {
		case !e.Visible:
			log.Debug().Int("emuID", e.ID).Str("name", e.Name).Msg("skipping hidden PinUP Popper emulator")
		case !IsPinball(&e):
			log.Debug().Int("emuID", e.ID).Str("name", e.Name).Msg("skipping non-pinball PinUP Popper emulator")
		default:
			emulators[e.ID] = e
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PinUP Popper emulator rows: %w", err)
	}
	return emulators, nil
}

// readTables returns the visible tables that belong to one of the given
// emulators, in GameID order.
func readTables(ctx context.Context, db *sql.DB, emulators map[int]Emulator) ([]Table, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT GameID, EMUID,
			substr(COALESCE(GameName, ''), 1, ?), substr(COALESCE(GameDisplay, ''), 1, ?),
			substr(COALESCE(GameFileName, ''), 1, ?), substr(COALESCE(Manufact, ''), 1, ?),
			substr(COALESCE(GameType, ''), 1, ?), substr(COALESCE(Category, ''), 1, ?),
			substr(COALESCE(GameTheme, ''), 1, ?), substr(COALESCE(Notes, ''), 1, ?),
			substr(COALESCE(Author, ''), 1, ?), substr(COALESCE(ALTEXE, ''), 1, ?),
			GameYear, NumPlayers, GameRating
		FROM Games
		WHERE COALESCE(Visible, 1) = 1
		ORDER BY GameID
		LIMIT ?`,
		maxFieldLength, maxFieldLength, maxFieldLength, maxFieldLength, maxFieldLength,
		maxFieldLength, maxFieldLength, maxFieldLength, maxFieldLength, maxFieldLength,
		maxTables+1,
	)
	if err != nil {
		return nil, fmt.Errorf("query PinUP Popper games: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close PinUP Popper game rows")
		}
	}()

	tables := make([]Table, 0)
	for rows.Next() {
		var (
			id, emuID             sql.NullInt64
			year, players, rating sql.NullInt64
			t                     Table
		)
		if err := rows.Scan(
			&id, &emuID, &t.Name, &t.Display, &t.FileName, &t.Manufacturer,
			&t.GameType, &t.Category, &t.Theme, &t.Notes, &t.Author, &t.AltExe,
			&year, &players, &rating,
		); err != nil {
			return nil, fmt.Errorf("scan PinUP Popper game row: %w", err)
		}
		if len(tables) >= maxTables {
			return nil, ErrTooManyTables
		}
		if !id.Valid || id.Int64 <= 0 || id.Int64 > maxGameID || !emuID.Valid {
			continue
		}
		if _, ok := emulators[int(emuID.Int64)]; !ok {
			continue
		}
		t.ID = int(id.Int64)
		t.EmulatorID = int(emuID.Int64)
		t.Name = cleanField(t.Name)
		t.Display = cleanField(t.Display)
		t.FileName = cleanField(t.FileName)
		t.Manufacturer = cleanField(t.Manufacturer)
		t.GameType = cleanField(t.GameType)
		t.Category = cleanField(t.Category)
		t.Theme = cleanField(t.Theme)
		t.Author = cleanField(t.Author)
		t.AltExe = cleanField(t.AltExe)
		t.Notes = strings.TrimSpace(t.Notes)
		if t.Name == "" && t.Display == "" {
			log.Debug().Int("gameID", t.ID).Msg("skipping PinUP Popper game with no name")
			continue
		}
		t.Year = int(year.Int64)
		t.Players = int(players.Int64)
		t.Rating = int(rating.Int64)
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PinUP Popper game rows: %w", err)
	}
	return tables, nil
}

// cleanField trims a single-line text field and drops it entirely when it
// carries control characters, which never appear in legitimate names.
func cleanField(value string) string {
	value = strings.TrimSpace(value)
	if virtualpath.ContainsControlChar(value) {
		return ""
	}
	return value
}
