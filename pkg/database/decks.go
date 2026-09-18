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

package database

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A deck is the user's persistent, ordered list of games and cards, and the
// one list type that syncs with a linked online account. Everything here
// works with no account: the ID is minted on the device.

const (
	DeckItemKindCard   = "card"
	DeckItemKindScript = "script"

	// DeckIDLength is the length of a deck ID this device mints, in
	// characters of DeckIDAlphabet. DeckIDLegacyLength is the length of IDs
	// allocated before minting moved to devices, which are still valid.
	DeckIDLength       = 12
	DeckIDLegacyLength = 8
	// DeckIDAlphabet is Crockford base32: digits and upper-case letters
	// without I, L, O and U. Core stores and shows IDs lower-case; matching
	// is case-insensitive everywhere.
	DeckIDAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	// DeckIDLegacyAlphabet is the alphabet of legacy IDs, which encoded a row
	// number over every digit and letter. I, L, O and U are distinct
	// characters in them, not misreadings of 1 and 0.
	DeckIDLegacyAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	// DeckMaxItems bounds one deck. How many decks a device holds is not
	// bounded: a deck costs well under ten kilobytes, and the background work
	// a deck creates is per item, so this is the limit that matters.
	DeckMaxItems          = 120
	DeckNameMaxLen        = 100
	DeckDescriptionMaxLen = 1000
	DeckZapScriptMaxLen   = 5000
)

var (
	ErrDeckNotFound  = errors.New("deck not found")
	ErrDeckLimit     = errors.New("deck limit reached")
	ErrDeckItemLimit = errors.New("deck item limit reached")
	// ErrDeckItemNotFound and ErrDeckItemRepeated reject an edit that keeps
	// an existing item the deck does not hold, or keeps one twice.
	ErrDeckItemNotFound = errors.New("deck item not found")
	ErrDeckItemRepeated = errors.New("deck item listed more than once")
	ErrDeckOwned        = errors.New("deck is owned by this device")
	ErrDeckReadOnly     = errors.New("deck is read-only")
	ErrInvalidDeckID    = errors.New("invalid deck id")
)

// DeckCardScript is one script a card item runs.
type DeckCardScript struct {
	Name      string `json:"name,omitempty"`
	ZapScript string `json:"zapscript"`
}

// DeckItemAnchor is the per-device link from a game item to the indexed
// file it was added from, with the scanner identity snapshot that lets the
// row be re-linked after the media database is rebuilt.
type DeckItemAnchor struct {
	SystemID  string
	Path      string
	MediaName string
	Tags      []string
}

// DeckItem is one member of a deck.
type DeckItem struct {
	Kind      string
	Name      string
	ZapScript string
	CardID    string
	Anchor    DeckItemAnchor
	Scripts   []DeckCardScript
	Metadata  json.RawMessage
	DBID      int64
	DeckDBID  int64
	CreatedAt int64
	UpdatedAt int64
	Position  int
}

// HasAnchor reports whether the item is linked to a local file.
func (i *DeckItem) HasAnchor() bool {
	return i.Anchor.Path != ""
}

// Deck is a user's list. Items is populated by GetDeck and left nil by
// ListDecks, which fills ItemCount instead.
type Deck struct {
	DeckID      string
	Name        string
	Description string
	SourceURL   string
	Metadata    json.RawMessage
	Items       []DeckItem
	DBID        int64
	CreatedAt   int64
	UpdatedAt   int64
	FetchedAt   int64
	ItemCount   int
	Owned       bool
}

// NewDeckID mints a deck ID from the operating system's random source:
// DeckIDLength characters of DeckIDAlphabet, lower-cased. One byte per
// character modulo 32 is unbiased because 256 is a multiple of 32.
func NewDeckID() (string, error) {
	raw := make([]byte, DeckIDLength)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("mint deck id: %w", err)
	}
	out := make([]byte, DeckIDLength)
	for i, b := range raw {
		out[i] = DeckIDAlphabet[int(b)%len(DeckIDAlphabet)]
	}
	return strings.ToLower(string(out)), nil
}

// NormalizeDeckID trims and lower-cases a deck ID and checks it is a minted
// ID drawn from DeckIDAlphabet or a legacy ID drawn from
// DeckIDLegacyAlphabet. Characters an alphabet omits are refused rather than
// folded, so the stored ID is always the one that was issued.
func NormalizeDeckID(raw string) (string, error) {
	id := strings.ToUpper(strings.TrimSpace(raw))
	var alphabet string
	switch len(id) {
	case DeckIDLength:
		alphabet = DeckIDAlphabet
	case DeckIDLegacyLength:
		alphabet = DeckIDLegacyAlphabet
	default:
		return "", fmt.Errorf("%w: %q must be %d or %d characters",
			ErrInvalidDeckID, raw, DeckIDLength, DeckIDLegacyLength)
	}
	for _, r := range id {
		if !strings.ContainsRune(alphabet, r) {
			return "", fmt.Errorf("%w: %q holds a character outside the deck alphabet", ErrInvalidDeckID, raw)
		}
	}
	return strings.ToLower(id), nil
}

// IsMintedDeckID reports whether an ID is one a device minted, rather than a
// legacy ID issued before minting moved to devices. Only a minted ID can be
// used to create a deck on an account, so a legacy one is never offered as a
// create. The ID must already be normalized.
func IsMintedDeckID(deckID string) bool {
	if len(deckID) != DeckIDLength {
		return false
	}
	for _, r := range strings.ToUpper(deckID) {
		if !strings.ContainsRune(DeckIDAlphabet, r) {
			return false
		}
	}
	return true
}

// EncodeDeckCardScripts serializes a card item's scripts for a UserDB TEXT
// column. Nil or empty input encodes to the empty string.
func EncodeDeckCardScripts(scripts []DeckCardScript) string {
	if len(scripts) == 0 {
		return ""
	}
	encoded, err := json.Marshal(scripts)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// DecodeDeckCardScripts parses a stored scripts column. Empty or malformed
// input decodes to nil.
func DecodeDeckCardScripts(raw string) []DeckCardScript {
	if raw == "" {
		return nil
	}
	var scripts []DeckCardScript
	if err := json.Unmarshal([]byte(raw), &scripts); err != nil {
		return nil
	}
	return scripts
}
