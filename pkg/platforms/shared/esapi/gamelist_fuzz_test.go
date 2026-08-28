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

import "testing"

func FuzzParseGameListXML(f *testing.F) {
	f.Add([]byte(`<gameList><game><name>Game</name><path>./game.rom</path></game></gameList>`))
	f.Add([]byte(`<gameList><folder><name>Folder</name><path>./folder</path></folder></gameList>`))
	f.Add([]byte(`<gameList>`))
	f.Add([]byte(`not xml`))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxGameListXMLSize {
			t.Skip()
		}
		gameList, err := ParseGameListXML(data)
		if err == nil && len(gameList.Games)+len(gameList.Folders) > MaxGameListEntries {
			t.Fatal("decoded entries beyond limit")
		}
	})
}
