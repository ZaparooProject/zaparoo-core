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

package esapi

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnmarshalGameIDVariants verifies that both the XML attribute form
// (ScreenScraperIDAttr) and the element form (ScreenScraperID) of the "id"
// field parse correctly in isolation and together.
func TestUnmarshalGameIDVariants(t *testing.T) {
	t.Parallel()

	t.Run("both attribute and element", func(t *testing.T) {
		t.Parallel()
		data := []byte(`<game id="attr-val"><id>42</id><path>./rom.nes</path></game>`)
		var g Game
		require.NoError(t, xml.Unmarshal(data, &g))
		assert.Equal(t, "attr-val", g.ScreenScraperIDAttr)
		assert.Equal(t, 42, g.ScreenScraperID)
	})

	t.Run("attribute only", func(t *testing.T) {
		t.Parallel()
		data := []byte(`<game id="only-attr"><path>./rom.nes</path></game>`)
		var g Game
		require.NoError(t, xml.Unmarshal(data, &g))
		assert.Equal(t, "only-attr", g.ScreenScraperIDAttr)
		assert.Equal(t, 0, g.ScreenScraperID, "element form should be zero when absent")
	})

	t.Run("element only", func(t *testing.T) {
		t.Parallel()
		data := []byte(`<game><id>99</id><path>./rom.nes</path></game>`)
		var g Game
		require.NoError(t, xml.Unmarshal(data, &g))
		assert.Empty(t, g.ScreenScraperIDAttr, "attribute form should be empty when absent")
		assert.Equal(t, 99, g.ScreenScraperID)
	})

	t.Run("neither present", func(t *testing.T) {
		t.Parallel()
		data := []byte(`<game><path>./rom.nes</path></game>`)
		var g Game
		require.NoError(t, xml.Unmarshal(data, &g))
		assert.Empty(t, g.ScreenScraperIDAttr)
		assert.Equal(t, 0, g.ScreenScraperID)
	})
}
