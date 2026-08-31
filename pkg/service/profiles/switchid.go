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

package profiles

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"math/big"
	"strings"
)

// wordlistRaw is derived from the EFF short wordlist #1
// (https://www.eff.org/dice, CC-BY 3.0), with hyphenated words removed so
// every word is a plain lowercase token. Switch IDs are selectors, not
// secrets — the list only needs to be large enough to avoid accidental
// collisions and easy to read aloud or print on a card.
//
//go:embed wordlist.txt
var wordlistRaw string

// switchIDWords is the number of words in a generated switch ID.
const switchIDWords = 3

//nolint:gochecknoglobals // immutable parsed copy of the embedded wordlist
var wordlist = strings.Fields(wordlistRaw)

// GenerateSwitchID returns a new random word-phrase switch ID, e.g.
// "corn-arm-truck". Uniqueness is enforced by the database; callers should
// retry on a unique-constraint conflict.
func GenerateSwitchID() (string, error) {
	parts := make([]string, switchIDWords)
	maxIndex := big.NewInt(int64(len(wordlist)))
	for i := range parts {
		n, err := rand.Int(rand.Reader, maxIndex)
		if err != nil {
			return "", fmt.Errorf("failed to generate switch ID word: %w", err)
		}
		parts[i] = wordlist[n.Int64()]
	}
	return strings.Join(parts, "-"), nil
}
