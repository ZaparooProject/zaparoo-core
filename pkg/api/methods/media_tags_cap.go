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

package methods

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"

// isCappedTagType reports whether a tag type is long-tail and should be capped
// when returning tag lists. It follows the tag rules (tags.RuleFor): a closed
// type has a finite canonical list and is returned in full, apart from search,
// whose franchise and feature values run long. Free-text types (company names)
// and format types without a small fixed range (extensions, track numbers,
// deck membership, build dates) are capped. Year and rating are bounded by
// their rules and returned in full.
func isCappedTagType(tagType string) bool {
	t := tags.TagType(tagType)
	if t == tags.TagTypeSearch {
		return true
	}
	if t == tags.TagTypeYear || t == tags.TagTypeRating {
		return false
	}
	rule, ok := tags.RuleFor(tags.TagType(tagType))
	return !ok || rule.Kind != tags.KindClosed
}
