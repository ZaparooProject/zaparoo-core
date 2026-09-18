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
	"net/url"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
)

// FuzzParseDeckPlaylist feeds arbitrary bodies to the parser that reads a
// fetched ZapLink as a served deck. The body comes off the network, so the
// parser must answer for any bytes at all rather than panicking, and must
// never claim a body is a deck whose ID it does not carry.
func FuzzParseDeckPlaylist(f *testing.F) {
	f.Add(`**playlist.open:{"id":"ZON-0123456789ab","name":"D","items":[]}`, "0123456789ab")
	f.Add(`**playlist.open:{"id":"ZON-0123456789AB","name":"D",`+
		`"items":[{"name":"A","zapscript":"**launch.system:NES"}]}`, "0123456789ab")
	f.Add(`**playlist.open:{"id":"ZON-zzzzzzzzzzzz"}`, "0123456789ab")
	f.Add(`**playlist.open:/roms/list.pls`, "0123456789ab")
	f.Add(`**launch.system:SNES`, "0123456789ab")
	f.Add(`**playlist.open:{"id":`, "0123456789ab")
	f.Add("", "")
	f.Add(`**playlist.open:{"id":"ZON-0123456789ab"}||**stop`, "0123456789ab")

	f.Fuzz(func(t *testing.T, body, deckID string) {
		arg, ok := decks.ParseDeckPlaylist(body, deckID)
		if !ok {
			return
		}
		// A body only parses as this deck when the ID is a real one and the
		// served playlist names it, so a caller can trust what it stores.
		normalized, err := database.NormalizeDeckID(deckID)
		if err != nil {
			t.Fatalf("accepted playlist for unusable deck id %q", deckID)
		}
		if want := decks.PlaylistID(normalized); !strings.EqualFold(arg.ID, want) {
			t.Fatalf("accepted playlist %q for deck %q", arg.ID, want)
		}
	})
}

// FuzzDeckIDFromZapLinkURL feeds arbitrary URLs to the check that decides
// whether a tapped link is a deck this device may cache. Anything it accepts
// must be an official host and a usable deck ID.
func FuzzDeckIDFromZapLinkURL(f *testing.F) {
	f.Add("https://zpr.au/d0123456789ab")
	f.Add("https://zpr.au/DABCDEFGH")
	f.Add("https://edge.zaparoo.com/d0123456789ab")
	f.Add("https://zpr.au/c0123456789ab")
	f.Add("http://zpr.au/d0123456789ab")
	f.Add("https://example.com/d0123456789ab")
	f.Add("https://zpr.au/d0123456789ab/extra")
	f.Add("https://zpr.au@evil.test/d0123456789ab")
	f.Add("not a url %%")
	f.Add("")

	f.Fuzz(func(t *testing.T, raw string) {
		deckID, ok := decks.DeckIDFromZapLinkURL(raw)
		if !ok {
			return
		}
		if _, err := database.NormalizeDeckID(deckID); err != nil {
			t.Fatalf("accepted %q as deck id %q", raw, deckID)
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("accepted unparseable url %q", raw)
		}
		if !strings.EqualFold(u.Scheme, "https") {
			t.Fatalf("accepted non-https url %q", raw)
		}
		official := false
		for _, host := range config.OfficialAuthHosts {
			if strings.EqualFold(u.Hostname(), host) {
				official = true
				break
			}
		}
		if !official {
			t.Fatalf("accepted unofficial host in %q", raw)
		}
	})
}
