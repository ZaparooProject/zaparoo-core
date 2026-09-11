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

package filters

import (
	"slices"

	zapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// IncludesHidden identifies an explicit favorites/hidden view. An OR term does
// not qualify: other branches of that query are still ordinary discovery.
func IncludesHidden(filters []zapscript.TagFilter, includeHidden bool) bool {
	if includeHidden {
		return true
	}
	for _, filter := range filters {
		if filter.Type == string(tags.TagTypeUser) && filter.Operator == zapscript.TagOperatorAND &&
			(filter.Value == string(tags.TagUserFavorite) || filter.Value == string(tags.TagUserHidden)) {
			return true
		}
	}
	return false
}

// ExcludeHidden adds discovery visibility to the query, before pagination or
// counts. It never mutates the caller's slice and is safe to apply repeatedly.
func ExcludeHidden(filters []zapscript.TagFilter) []zapscript.TagFilter {
	hidden := zapscript.TagFilter{
		Type: string(tags.TagTypeUser), Value: string(tags.TagUserHidden), Operator: zapscript.TagOperatorNOT,
	}
	if slices.Contains(filters, hidden) {
		return filters
	}
	return append(slices.Clone(filters), hidden)
}
