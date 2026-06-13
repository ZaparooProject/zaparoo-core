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

package profiles

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWordlist_Loaded(t *testing.T) {
	t.Parallel()

	assert.Len(t, wordlist, 1295)
	wordRe := regexp.MustCompile(`^[a-z]+$`)
	for _, w := range wordlist {
		assert.True(t, wordRe.MatchString(w), "word %q must be plain lowercase", w)
	}
}

func TestGenerateSwitchID_Format(t *testing.T) {
	t.Parallel()

	for range 50 {
		id, err := GenerateSwitchID()
		require.NoError(t, err)
		parts := strings.Split(id, "-")
		require.Len(t, parts, switchIDWords, "switch ID %q", id)
		for _, part := range parts {
			assert.Contains(t, wordlist, part)
		}
	}
}
