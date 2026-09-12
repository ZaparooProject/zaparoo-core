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
	"testing"

	zapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryVisibilityFilters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tag     string
		include bool
	}{
		{"user:favorite", true},
		{"+user:favorite", true},
		{"user:hidden", true},
		{"-user:favorite", false},
		{"~user:favorite", false},
		{"genre:action", false},
	} {
		parsed, err := ParseTagFilters([]string{tc.tag})
		require.NoError(t, err)
		assert.Equal(t, tc.include, IncludesHidden(parsed, false), tc.tag)
		assert.True(t, IncludesHidden(parsed, true))
	}
	input := make([]zapscript.TagFilter, 0, 3)
	filtered := ExcludeHidden(input)
	assert.Empty(t, input)
	require.Len(t, filtered, 1)
	assert.Equal(t, "hidden", filtered[0].Value)
	assert.Equal(t, zapscript.TagOperatorNOT, filtered[0].Operator)
	assert.Equal(t, filtered, ExcludeHidden(filtered))
}
