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

// Package playnite integrates the Playnite game library manager. Playnite
// keeps its library in LiteDB files it holds open exclusively, and only code
// running inside it sees a game start or stop, so everything except launching
// goes through the Zaparoo extension: it connects to a pipe Core serves and
// exchanges newline-delimited JSON. Everything except the Win32 bindings is
// free of build tags so the behaviour can be tested on any platform.
package playnite

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/google/uuid"
)

// LauncherID identifies the Playnite launcher to users and in ActiveMedia.
const LauncherID = "Playnite"

// PipeName is the named pipe Core serves for the extension.
const PipeName = `\\.\pipe\zaparoo-playnite-ipc`

// SteamLibraryPluginID is the library plugin Playnite imports Steam games
// with. Core indexes Steam itself, so those games are left to that launcher.
const SteamLibraryPluginID = "cb91dfc9-b977-43bf-8e70-55f46e410fab"

// Install is a located Playnite folder. FullscreenExe is empty when the
// install does not ship Fullscreen mode.
type Install struct {
	Dir           string
	DesktopExe    string
	FullscreenExe string
}

// GamePath builds the virtual path indexed for a game and written to tokens.
// The ID is Playnite's own database ID and survives renames; the name is
// cosmetic.
func GamePath(gameID, name string) string {
	return virtualpath.CreateVirtualPath(shared.SchemePlaynite, gameID, name)
}

// ParseGamePath extracts the Playnite game ID from a playnite:// virtual
// path, in the lowercase hyphenated form Playnite prints.
func ParseGamePath(path string) (string, error) {
	id, err := virtualpath.ExtractSchemeID(path, shared.SchemePlaynite)
	if err != nil {
		return "", fmt.Errorf("parse Playnite game path: %w", err)
	}
	return NormalizeGameID(id)
}

// NormalizeGameID validates a game ID and returns its canonical form. Only
// the hyphenated form is accepted: the ID goes onto Playnite's command line.
func NormalizeGameID(id string) (string, error) {
	id = strings.TrimSpace(id)
	parsed, err := uuid.Parse(id)
	if err != nil || len(id) != 36 || parsed == uuid.Nil {
		return "", fmt.Errorf("invalid Playnite game ID %q", id)
	}
	return parsed.String(), nil
}
