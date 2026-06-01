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

package tags

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
)

// NormalizeCompanyName converts a raw company/person name to a dash-joined slug
// suitable for open-valued company tags (publisher, developer, credit). Uses the
// full slug word pipeline so ampersands, accents, and punctuation are handled
// consistently ("T&E Soft" → "t-and-e-soft").
func NormalizeCompanyName(raw string) TagValue {
	words := slugs.NormalizeToWords(raw)
	if len(words) == 0 {
		return TagValue(NormalizeTag(raw))
	}
	return TagValue(strings.Join(words, "-"))
}

// NormalizeTagValue normalizes a raw tag value for canonical storage and lookup.
// Numeric padding is storage-only and intentionally not applied here.
func NormalizeTagValue(tagType, raw string) string {
	switch TagType(NormalizeTag(tagType)) {
	case TagTypeDeveloper, TagTypePublisher, TagTypeCredit:
		return string(NormalizeCompanyName(raw))
	default:
		return NormalizeTag(raw)
	}
}
