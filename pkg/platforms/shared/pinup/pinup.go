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

// Package pinup integrates the PinUP Popper virtual pinball frontend. Popper
// keeps its library in PUPDatabase.db, a SQLite file in the PinUPSystem folder,
// and takes remote commands through its web remote (PuPServer.exe) while
// PinUpMenu.exe is running. Everything except the Win32 bindings is free of
// build tags so the behaviour can be tested on any platform.
package pinup

import (
	"fmt"
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// LauncherID identifies the Popper launcher to users and in ActiveMedia.
const LauncherID = "PinUPPopper"

// maxGameID bounds GameID, an INTEGER primary key in Popper's schema.
const maxGameID = 1<<31 - 1

// Install is a located PinUP System folder. ServerExe is empty when the
// install does not ship the web remote.
type Install struct {
	Dir       string
	DBPath    string
	MenuExe   string
	ServerExe string
	MediaDir  string
}

// Emulator is a Popper emulator definition: how tables of one kind are
// launched and closed, and where their media lives.
type Emulator struct {
	Name         string
	Display      string
	MediaDir     string
	GamesDir     string
	GamesExt     string
	LaunchScript string
	ProcessName  string
	WindowTitle  string
	ID           int
	Visible      bool
}

// Table is one Popper game row. Name is the table file's basename without
// extension, which is also the stem every media file for the table is named
// after. ID is Popper's GameID and survives renames and metadata edits.
// AltExe is the alternate launcher executable Popper runs this table with
// instead of the emulator's default, when one is set.
type Table struct {
	Name         string
	Display      string
	FileName     string
	Manufacturer string
	GameType     string
	Category     string
	Theme        string
	Notes        string
	Author       string
	AltExe       string
	ID           int
	EmulatorID   int
	Year         int
	Players      int
	Rating       int
}

// Library is the launchable part of a Popper database: visible tables that
// belong to visible emulators classified as pinball.
type Library struct {
	Emulators map[int]Emulator
	Tables    []Table
}

// Table returns the table with the given GameID and its emulator.
func (l Library) Table(gameID int) (Table, Emulator, bool) {
	for i := range l.Tables {
		if l.Tables[i].ID != gameID {
			continue
		}
		emu, ok := l.Emulators[l.Tables[i].EmulatorID]
		if !ok {
			return Table{}, Emulator{}, false
		}
		return l.Tables[i], emu, true
	}
	return Table{}, Emulator{}, false
}

// DisplayName returns the name shown for the table, falling back to the file
// stem when Popper has no display title.
func (t *Table) DisplayName() string {
	if t.Display != "" {
		return t.Display
	}
	return t.Name
}

// TablePath builds the virtual path indexed for a table and written to tokens.
// The GameID is the identity; the display name is cosmetic.
func TablePath(gameID int, display string) string {
	return virtualpath.CreateVirtualPath(shared.SchemePopper, strconv.Itoa(gameID), display)
}

// ParseTablePath extracts the Popper GameID from a popper:// virtual path.
func ParseTablePath(path string) (int, error) {
	id, err := virtualpath.ExtractSchemeID(path, shared.SchemePopper)
	if err != nil {
		return 0, fmt.Errorf("parse Popper table path: %w", err)
	}
	gameID, err := strconv.Atoi(id)
	if err != nil || gameID <= 0 || gameID > maxGameID {
		return 0, fmt.Errorf("invalid Popper game ID %q", id)
	}
	return gameID, nil
}
