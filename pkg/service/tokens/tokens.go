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

package tokens

import (
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
)

const (
	TypeNTAG           = "NTAG"
	TypeMifare         = "MIFARE"
	TypeAmiibo         = "Amiibo"
	TypeLegoDimensions = "LegoDimensions"
	TypeFeliCa         = "FeliCa"
	TypeBarcode        = "Barcode"
	TypeUnknown        = "Unknown"
)

// Source constants for token origins.
const (
	SourceReader   = "Reader"   // Hardware reader (NFC, barcode, etc.)
	SourceAPI      = "API"      // JSON-RPC or REST run API
	SourcePlaylist = "Playlist" // Playlist-triggered launch
	SourceHook     = "Hook"     // Hook-generated token
	SourceGMC      = "GMC"      // Groovy Media Center proxy
	SourceControl  = "Control"  // Config-defined control script
	SourceRemote   = "Remote"   // Allowlisted Zaparoo Online remote operation
)

//nolint:govet // Field order groups token identity and structural command source.
type Token struct {
	ScanTime time.Time
	// Commands bypass text parsing and mappings for trusted structural callers.
	// Remote operations use this only after deny-by-default verb validation.
	Commands []gozapscript.Command
	Type     string
	UID      string
	Text     string
	Data     string
	Source   string
	ReaderID string // Deterministic ID of the source reader
	// PathRoot is an optional filesystem root used to resolve relative paths
	// originating from source-backed tokens such as external drives.
	PathRoot string
	// Traits is the set of traits this token's ZapScript declared, resolved
	// once where the token entered the system. Tokens derived from this one,
	// such as playlist tracks and hook scripts, inherit it rather than
	// resolving their own.
	Traits Traits
	Unsafe bool
}
