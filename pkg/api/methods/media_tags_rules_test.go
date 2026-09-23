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

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
)

// TestIsCappedTagTypeFollowsTagRules keeps media.tags capping in step with
// the tag rules: closed vocabularies are finite and returned in full (search
// excepted, being long), free-text and unbounded format types are capped.
func TestIsCappedTagTypeFollowsTagRules(t *testing.T) {
	t.Parallel()
	for _, capped := range []tags.TagType{
		tags.TagTypeDeveloper, tags.TagTypePublisher, tags.TagTypeCredit, tags.TagTypeMameParent,
		tags.TagTypeExtension, tags.TagTypeTrack, tags.TagTypeBuildDate, tags.TagTypeUser,
		tags.TagTypeSearch, tags.ScraperType("gamelist.xml"), "no-such-type",
	} {
		assert.True(t, isCappedTagType(string(capped)), "%s should be capped", capped)
	}
	for _, full := range []tags.TagType{tags.TagTypeYear, tags.TagTypeRating} {
		assert.False(t, isCappedTagType(string(full)), "%s is bounded and returned in full", full)
	}
	for tagType, rule := range tags.TagRules {
		if rule.Kind == tags.KindClosed && tagType != tags.TagTypeSearch {
			assert.False(t, isCappedTagType(string(tagType)), "closed type %s is returned in full", tagType)
		}
	}
}
