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

package decks_test

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
)

// FuzzParseServedPlaylist feeds arbitrary bodies to the parser that decides
// whether a fetched ZapLink serves a playlist to keep as a deck. The body
// comes off the network, so the parser must answer for any bytes at all
// rather than panicking, and must only ever accept a lone playlist command
// that has something to run.
func FuzzParseServedPlaylist(f *testing.F) {
	f.Add(`**playlist.open:{"id":"ZON-LHM6N9T8","name":"D","items":[{"name":"A","zapscript":"@Genesis/A"}]}`)
	f.Add(`**playlist.play:{"id":"party-list","items":[{"zapscript":"**launch.system:NES"}]}?mode=shuffle`)
	f.Add(`**playlist.load:{"items":[{"zapscript":"**stop"}]}`)
	f.Add(`**playlist.open:{"id":"p","name":"D","items":[]}`)
	f.Add(`**playlist.open:{"id":"p","items":[{"zapscript":"  "}]}`)
	f.Add(`**playlist.open:/roms/list.pls`)
	f.Add(`**playlist.open:deck://0123456789ab`)
	f.Add(`**launch.system:SNES`)
	f.Add(`**playlist.open:{"id":`)
	f.Add("")
	f.Add(`**playlist.open:{"id":"p","items":[{"zapscript":"**stop"}]}||**stop`)

	f.Fuzz(func(t *testing.T, body string) {
		cmd, arg, ok := decks.ParseServedPlaylist(body)
		if !ok {
			return
		}
		switch cmd.Name {
		case zapscript.ZapScriptCmdPlaylistOpen, zapscript.ZapScriptCmdPlaylistPlay,
			zapscript.ZapScriptCmdPlaylistLoad:
		default:
			t.Fatalf("accepted command %q", cmd.Name)
		}
		if len(cmd.Args) != 1 {
			t.Fatalf("accepted a playlist command with %d arguments", len(cmd.Args))
		}
		runnable := false
		for _, item := range arg.Items {
			runnable = runnable || strings.TrimSpace(item.ZapScript) != ""
		}
		if !runnable {
			t.Fatalf("accepted a playlist with nothing to run: %q", body)
		}
	})
}
