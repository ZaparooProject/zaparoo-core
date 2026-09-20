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

package esapi

import (
	"bytes"
	"testing"
)

func FuzzParseGameListXML(f *testing.F) {
	f.Add([]byte(`<gameList><game><name>Game</name><path>./game.rom</path></game></gameList>`))
	f.Add([]byte(`<gameList><folder><name>Folder</name><path>./folder</path></folder></gameList>`))
	f.Add([]byte(`<gameList><game><path>../GAMEBOY/Game.gbc</path><box2d>./box.png</box2d></game></gameList>`))
	f.Add([]byte(`<gameList><game><boxart2d>./cover.png</boxart2d><box2d>./alias.png</box2d></game></gameList>`))
	f.Add([]byte(`<gameList>`))
	f.Add([]byte(`not xml`))
	f.Add([]byte("\xef\xbb\xbf" + `<?xml version="1.0" encoding="UTF-8"?>` +
		`<gameList><game><path>./game.rom</path></game></gameList>`))
	f.Add([]byte("\xef\xbb\xbf<gameList/>suffix"))
	f.Add([]byte("\xef\xbb\xbf\xef\xbb\xbf<gameList/>"))
	// A typed field that does not parse drops its own entry, so the decoder
	// has to resume the stream mid-document; these seeds cover that resume
	// next to a folder, a nested element and a following good entry.
	f.Add([]byte(`<gameList><game><path>./a</path><playcount>many</playcount></game>` +
		`<game><path>./b</path><playcount>2</playcount></game></gameList>`))
	f.Add([]byte(`<gameList><game><hidden>maybe</hidden><x><y>deep</y></x></game>` +
		`<folder><path>./f</path></folder></gameList>`))
	f.Add([]byte(`<gameList><game><path>./a</path><game><playcount>x</playcount></game></game></gameList>`))
	f.Add([]byte(`<gameList><folder><path>./f</path><playcount>x</playcount></folder></gameList>`))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxGameListXMLSize {
			t.Skip()
		}
		gameList, err := ParseGameListXML(data)
		if err != nil {
			return
		}
		if len(gameList.Games)+len(gameList.Folders) > MaxGameListEntries {
			t.Fatal("decoded entries beyond limit")
		}
		// The visitor does not relax the document limits, so anything the
		// decoder accepted the validator must accept from the same bytes.
		if validateErr := ValidateGameListXML(data); validateErr != nil {
			t.Fatalf("decoded but failed validation: %v", validateErr)
		}
		// The reference walk is the same traversal keeping less, so it must
		// see every <game> the decoder kept plus any it dropped.
		references, refErr := decodeGameReferences(bytes.NewReader(data), MaxGameListXMLSize)
		if refErr != nil {
			t.Fatalf("decoded but references failed: %v", refErr)
		}
		if len(references) < len(gameList.Games) ||
			len(references) > len(gameList.Games)+gameList.Skipped {
			t.Fatalf("reference walk saw %d games, decode saw %d kept and %d skipped",
				len(references), len(gameList.Games), gameList.Skipped)
		}
		// Streaming must not leave the decode dependent on what came before.
		repeat, repeatErr := ParseGameListXML(data)
		if repeatErr != nil {
			t.Fatalf("decoded once but not twice: %v", repeatErr)
		}
		if len(repeat.Games) != len(gameList.Games) ||
			len(repeat.Folders) != len(gameList.Folders) ||
			repeat.Skipped != gameList.Skipped {
			t.Fatal("repeated decode of the same bytes disagreed")
		}
	})
}
