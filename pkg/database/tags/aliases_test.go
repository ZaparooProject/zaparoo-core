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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanonicalizeTagAlias(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// deprecated aliases rewrite to canonical form
		{"addon:barcodeboy", "addon:barcode:barcodeboy"},
		{"addon:controller:jcart", "embedded:slot:jcart"},
		{"addon:controller:rumble", "embedded:vibration:rumble"},
		// pass-through: canonical or unknown tags are unchanged
		{"addon:barcode:barcodeboy", "addon:barcode:barcodeboy"},
		{"embedded:slot:jcart", "embedded:slot:jcart"},
		{"disc:1", "disc:1"},
		{"unknown:tag", "unknown:tag"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, CanonicalizeTagAlias(tt.in))
		})
	}
}

// TestAliasMapNoCycles verifies that no alias target is itself a key in
// deprecatedTagAliases, preventing infinite-loop canonicalization.
func TestAliasMapNoCycles(t *testing.T) {
	for _, target := range deprecatedTagAliases {
		_, cycle := deprecatedTagAliases[target]
		assert.False(t, cycle, "alias target %q is also a key — would cause a cycle", target)
	}
}

// TestAliasTargetsExistInCanonicalDefs validates that every alias target in
// deprecatedTagAliases exists in CanonicalTagDefinitions.
func TestAliasTargetsExistInCanonicalDefs(t *testing.T) {
	validTags := make(map[string]bool)
	for tagType, values := range CanonicalTagDefinitions {
		for _, value := range values {
			validTags[string(tagType)+":"+string(value)] = true
		}
	}

	for oldTag, canonical := range deprecatedTagAliases {
		idx := strings.Index(canonical, ":")
		if idx < 0 {
			t.Errorf("alias target %q (from %q) has no colon — expected type:value", canonical, oldTag)
			continue
		}
		assert.True(t, validTags[canonical],
			"alias target %q (from %q) not found in CanonicalTagDefinitions", canonical, oldTag)
	}
}
