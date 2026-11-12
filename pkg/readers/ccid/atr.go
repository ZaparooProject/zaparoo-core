/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package ccid

import (
	"bytes"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
)

// PC/SC Part 3 card names for the tags this driver reads.
const (
	cardNameClassic1K   = 0x0001
	cardNameClassic4K   = 0x0002
	cardNameUltralight  = 0x0003
	cardNameUltralightC = 0x003A

	// standardISO14443A3 is the Part 3 "SS" byte for a Type A tag that is not
	// an ISO 14443-4 card.
	standardISO14443A3 = 0x03

	atrStorageLen = 15
)

// storageCardHeader is the historical-byte prefix of a PC/SC storage card ATR,
// up to and including the PC/SC workgroup's RID.
var storageCardHeader = []byte{0x80, 0x4F, 0x0C, 0xA0, 0x00, 0x00, 0x03, 0x06}

// isContactlessATR reports whether an answer to reset has the form PC/SC
// requires a contactless reader to synthesise for a tag: 3B 8N 80 01.
//
// Contact cards and security keys do not have it. The driver never talks to a
// card without it, so those are left alone.
func isContactlessATR(atr []byte) bool {
	return len(atr) >= 4 && atr[0] == 0x3B && atr[1]&0xF0 == 0x80 && atr[2] == 0x80 && atr[3] == 0x01
}

// tagKind returns the tag family an ATR declares. Anything that is not a
// storage card this driver reads is KindUnknown and gets a UID-only token.
func tagKind(atr []byte) nfctag.Kind {
	if !isContactlessATR(atr) || len(atr) < atrStorageLen {
		return nfctag.KindUnknown
	}
	if !bytes.Equal(atr[4:12], storageCardHeader) || atr[12] != standardISO14443A3 {
		return nfctag.KindUnknown
	}
	switch uint16(atr[13])<<8 | uint16(atr[14]) {
	case cardNameClassic1K:
		return nfctag.KindClassic1K
	case cardNameClassic4K:
		return nfctag.KindClassic4K
	case cardNameUltralight, cardNameUltralightC:
		return nfctag.KindType2
	default:
		return nfctag.KindUnknown
	}
}
