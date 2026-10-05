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

package playnite

import (
	"encoding/json"
	"testing"
)

func FuzzParseEvent(f *testing.F) {
	f.Add([]byte(`{"Event":"Hello","PluginVersion":"1.0.0","ProtocolVersion":1,"Mode":"Desktop"}`))
	f.Add([]byte(`{"Event":"MediaStarted","Id":"67feee56-a90d-4022-9be8-7bec24e4fac0","Pid":4242,"Session":3,` +
		`"Game":{"Id":"67feee56-a90d-4022-9be8-7bec24e4fac0","Name":"Game","IsInstalled":true,` +
		`"Platforms":[{"SpecificationId":"nintendo_nes","Name":"NES"}]}}`))
	f.Add([]byte(`{"Event":"Games","RequestId":"1","Final":true,"Games":[{"Id":"x","Name":null}]}`))
	f.Add([]byte(`{"Event":"MediaStopResult","Id":"x","Status":"unsupported"}`))
	f.Add([]byte(`{"Event":""}`))
	f.Add([]byte(`{"Event":"Games","Games":"wrong"}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, line []byte) {
		event, err := ParseEvent(line)
		if err != nil {
			return
		}
		if event.Event == "" {
			t.Fatalf("accepted an event with no name: %q", line)
		}
		if !json.Valid(line) {
			t.Fatalf("accepted invalid JSON: %q", line)
		}
		// Whatever was accepted must be safe to hand to the indexing rules.
		for i := range event.Games {
			IndexedSystem(&event.Games[i], true)
		}
		if event.Game != nil {
			IndexedSystem(event.Game, false)
		}
	})
}
