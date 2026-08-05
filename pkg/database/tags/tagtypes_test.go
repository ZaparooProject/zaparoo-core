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
	"testing"

	"github.com/stretchr/testify/assert"
)

// IsScannerOwnedType gates which tags enter a media identity observation, and
// those tags are hashed into the cross-service observation fingerprint: a type
// silently changing sides here re-fingerprints every affected media on every
// device. Pin both sides of the boundary.
func TestIsScannerOwnedType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tagType TagType
		owned   bool
	}{
		{tagType: TagTypeRegion, owned: true},
		{tagType: TagTypeLang, owned: true},
		{tagType: TagTypeRev, owned: true},
		{tagType: TagTypeUnfinished, owned: true},
		{tagType: TagTypeExtension, owned: true},
		{tagType: TagTypeInput, owned: true},
		{tagType: TagTypePlayers, owned: true},
		{tagType: TagTypeGameGenre, owned: true},
		// User intent, scraped metadata and bookkeeping are all excluded.
		{tagType: TagTypeUser, owned: false},
		{tagType: TagTypeProperty, owned: false},
		{tagType: TagTypeRating, owned: false},
		{tagType: TagTypeGenre, owned: false},
		{tagType: TagTypeGameFamily, owned: false},
		{tagType: ScraperType("igdb"), owned: false},
		{tagType: ScraperRunType("igdb"), owned: false},
		// An unknown type is scanner-owned by default: new scanner tag types
		// must not silently vanish from identity snapshots.
		{tagType: TagType("brand-new-scanner-type"), owned: true},
	}
	for _, tt := range tests {
		t.Run(string(tt.tagType), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.owned, IsScannerOwnedType(tt.tagType))
		})
	}
}

func TestIsExclusiveType(t *testing.T) {
	t.Parallel()

	assert.True(t, IsExclusiveType(TagTypeYear), "one authoritative release year")
	assert.True(t, IsExclusiveType(TagTypeGameFamily))
	assert.False(t, IsExclusiveType(TagTypeRegion), "media can carry several regions")
	assert.False(t, IsExclusiveType(TagTypeLang))
	assert.False(t, IsExclusiveType(TagType("unknown-type")), "unknown types default to additive")
}
