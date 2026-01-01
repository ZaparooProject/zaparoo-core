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

package fixtures

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// Common test token fixtures for use in tests

// NewNFCToken creates a sample NFC token with typical values
func NewNFCToken() *tokens.Token {
	return &tokens.Token{
		ScanTime: time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		Type:     tokens.TypeNTAG,
		UID:      "04:12:34:AB:CD:EF:80",
		Text:     "zelda:botw",
		Data:     "",
		Source:   "nfc",
		FromAPI:  false,
		Unsafe:   false,
	}
}

// NewMifareToken creates a sample Mifare token with typical values
func NewMifareToken() *tokens.Token {
	return &tokens.Token{
		ScanTime: time.Date(2025, 1, 15, 12, 5, 0, 0, time.UTC),
		Type:     tokens.TypeMifare,
		UID:      "AB:CD:EF:12:34:56:78",
		Text:     "mario:odyssey",
		Data:     "",
		Source:   "nfc",
		FromAPI:  false,
		Unsafe:   false,
	}
}

// NewAmiiboToken creates a sample Amiibo token with typical values
func NewAmiiboToken() *tokens.Token {
	return &tokens.Token{
		ScanTime: time.Date(2025, 1, 15, 12, 10, 0, 0, time.UTC),
		Type:     tokens.TypeAmiibo,
		UID:      "04:11:22:33:44:55:66",
		Text:     "link:amiibo",
		Data:     "amiibo_data_here",
		Source:   "nfc",
		FromAPI:  false,
		Unsafe:   false,
	}
}

// NewEmptyToken creates a token with no text content (e.g., blank NFC tag)
func NewEmptyToken() *tokens.Token {
	return &tokens.Token{
		ScanTime: time.Date(2025, 1, 15, 12, 15, 0, 0, time.UTC),
		Type:     tokens.TypeNTAG,
		UID:      "04:00:00:00:00:00:00",
		Text:     "",
		Data:     "",
		Source:   "nfc",
		FromAPI:  false,
		Unsafe:   false,
	}
}

// NewAPIToken creates a token that came from the API (not hardware scan)
func NewAPIToken() *tokens.Token {
	return &tokens.Token{
		ScanTime: time.Date(2025, 1, 15, 12, 20, 0, 0, time.UTC),
		Type:     tokens.TypeNTAG,
		UID:      "API:12:34:56:78:90:AB",
		Text:     "pokemon:emerald",
		Data:     "",
		Source:   "api",
		FromAPI:  true,
		Unsafe:   false,
	}
}

// NewUnsafeToken creates a token marked as unsafe (contains potentially harmful content)
func NewUnsafeToken() *tokens.Token {
	return &tokens.Token{
		ScanTime: time.Date(2025, 1, 15, 12, 25, 0, 0, time.UTC),
		Type:     tokens.TypeNTAG,
		UID:      "04:BA:DC:0D:E0:FF:EE",
		Text:     "../../dangerous/path",
		Data:     "",
		Source:   "nfc",
		FromAPI:  false,
		Unsafe:   true,
	}
}

// NewCustomToken creates a token with custom values for specific test scenarios
func NewCustomToken(tokenType, uid, text, source string, fromAPI, unsafe bool) *tokens.Token {
	return &tokens.Token{
		ScanTime: time.Now(),
		Type:     tokenType,
		UID:      uid,
		Text:     text,
		Data:     "",
		Source:   source,
		FromAPI:  fromAPI,
		Unsafe:   unsafe,
	}
}

// TokenCollection represents a set of related tokens for comprehensive testing
type TokenCollection struct {
	NFC    *tokens.Token
	Mifare *tokens.Token
	Amiibo *tokens.Token
	Empty  *tokens.Token
	API    *tokens.Token
	Unsafe *tokens.Token
}

// NewTokenCollection creates a complete set of test tokens
func NewTokenCollection() *TokenCollection {
	return &TokenCollection{
		NFC:    NewNFCToken(),
		Mifare: NewMifareToken(),
		Amiibo: NewAmiiboToken(),
		Empty:  NewEmptyToken(),
		API:    NewAPIToken(),
		Unsafe: NewUnsafeToken(),
	}
}

// AllTokens returns all tokens in the collection as a slice
func (tc *TokenCollection) AllTokens() []*tokens.Token {
	return []*tokens.Token{
		tc.NFC,
		tc.Mifare,
		tc.Amiibo,
		tc.Empty,
		tc.API,
		tc.Unsafe,
	}
}

// SafeTokens returns only tokens that are not marked as unsafe
func (tc *TokenCollection) SafeTokens() []*tokens.Token {
	return []*tokens.Token{
		tc.NFC,
		tc.Mifare,
		tc.Amiibo,
		tc.Empty,
		tc.API,
	}
}

// HardwareTokens returns tokens that came from hardware (not API)
func (tc *TokenCollection) HardwareTokens() []*tokens.Token {
	return []*tokens.Token{
		tc.NFC,
		tc.Mifare,
		tc.Amiibo,
		tc.Empty,
		tc.Unsafe,
	}
}

// SampleTokens returns a collection of sample tokens for testing
func SampleTokens() []*tokens.Token {
	return NewTokenCollection().AllTokens()
}
