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

package misterdocs

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/ssgenre"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// packNotAGenre lists the pack genre names that are dropped on purpose.
var packNotAGenre = map[string]struct{}{
	"Adults":      {}, // a content rating
	"Casual Game": {}, // the vocabulary has no casual genre
	"Compilation": {}, // a release format
	"Demo":        {}, // tech and scene demos
	"Various":     {}, // ScreenScraper's catch-all
}

// splitPackGenre splits the pack's genre field into its names. The field
// joins names with a bare "/"; a name's own subgenre separator is " / ".
func splitPackGenre(field string) []string {
	var names []string
	start := 0
	for i := range len(field) {
		if field[i] != '/' {
			continue
		}
		if i > 0 && i+1 < len(field) && field[i-1] == ' ' && field[i+1] == ' ' {
			continue
		}
		names = append(names, strings.TrimSpace(field[start:i]))
		start = i + 1
	}
	return append(names, strings.TrimSpace(field[start:]))
}

func TestSplitPackGenre(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"Shoot'em Up / Vertical", "Shoot'em Up"},
		splitPackGenre("Shoot'em Up / Vertical/Shoot'em Up"))
	assert.Equal(t, []string{"Various"}, splitPackGenre("Various"))
}

// TestPackGenresAllMap checks every distinct genre field in the published
// MiSTer artwork pack gameinfo.tsv files (testdata, collected from all 39
// system catalogues) maps onto the vocabulary, or is a name dropped on
// purpose.
func TestPackGenresAllMap(t *testing.T) {
	t.Parallel()

	f, err := os.Open(filepath.Join("testdata", "artwork_pack_genres.txt"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		field := scanner.Text()
		if field == "" {
			continue
		}
		lines++
		values, unmapped := ssgenre.Lookup(field)
		assert.Emptyf(t, unmapped, "pack genre %q has unmapped parts", field)
		if len(values) > 0 {
			continue
		}
		for _, name := range splitPackGenre(field) {
			_, dropped := packNotAGenre[name]
			assert.Truef(t, dropped, "pack genre %q maps to nothing and %q is not listed as dropped", field, name)
		}
	}
	require.NoError(t, scanner.Err())
	assert.Greater(t, lines, 1000, "fixture looks truncated")

	for name := range packNotAGenre {
		values, unmapped := ssgenre.Lookup(name)
		assert.Emptyf(t, values, "%q is listed as dropped but maps to %v", name, values)
		assert.Empty(t, unmapped)
	}
}
