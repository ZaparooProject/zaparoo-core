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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// companyNameCache and its mutex protect cached NormalizeCompanyName results for
// the duration of an indexing run. Company names repeat heavily across a library
// and the slug word pipeline behind them is expensive. nil map means caching is
// disabled.
var (
	companyNameCacheMu syncutil.RWMutex
	companyNameCache   map[string]TagValue
)

// SetCompanyNameCache replaces the NormalizeCompanyName cache. Pass a freshly
// allocated map before each indexing run; pass nil to disable caching.
func SetCompanyNameCache(m map[string]TagValue) {
	companyNameCacheMu.Lock()
	companyNameCache = m
	companyNameCacheMu.Unlock()
}

// NormalizeCompanyName converts a raw company/person name to a dash-joined slug
// suitable for open-valued company tags (publisher, developer, credit). Uses the
// full slug word pipeline so ampersands, accents, and punctuation are handled
// consistently ("T&E Soft" → "t-and-e-soft").
func NormalizeCompanyName(raw string) TagValue {
	companyNameCacheMu.RLock()
	m := companyNameCache
	if m != nil {
		if v, ok := m[raw]; ok {
			companyNameCacheMu.RUnlock()
			return v
		}
	}
	companyNameCacheMu.RUnlock()

	v := computeCompanyName(raw)

	if m != nil {
		companyNameCacheMu.Lock()
		if companyNameCache != nil {
			companyNameCache[raw] = v
		}
		companyNameCacheMu.Unlock()
	}
	return v
}

func computeCompanyName(raw string) TagValue {
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
