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

package android

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/retroarch_precedence_baseline.json
var retroArchPrecedenceBaselineJSON []byte

// TestRetroArchCatalogMatchesPreMigrationPrecedence guards the migration of
// Android's system→core assignment onto pkg/platforms/shared/retroarch
// (2026-09-30): every one of the 92 systems this catalog covers must offer
// the exact same cores, in the exact same order, as it did before. The
// fixture was captured directly from this package's own pre-migration
// loadRetroArchCatalog output - see the migration's PR description for how.
//
// This checks per-system order only, not each system's absolute position in
// the wider catalog: Android's old JSON happened to list every system's
// primary pick before circling back for every system's alternates, an
// artifact of how the file was authored over time, not a stated policy
// (catalog/README.md's "Order is precedence" is scoped to "within a
// system"). Reproducing that historical global interleaving would need
// alternates sorted by core name across every system rather than grouped by
// system, which no other data in this package does and would only make
// androidCoreAlternates harder to read, for no behavioral benefit: nothing
// downstream reads absolute position, only per-system order.
func TestRetroArchCatalogMatchesPreMigrationPrecedence(t *testing.T) {
	t.Parallel()

	var baseline map[string][]string
	require.NoError(t, json.Unmarshal(retroArchPrecedenceBaselineJSON, &baseline))
	require.Len(t, baseline, 92)

	entries, err := loadRetroArchCatalog(retroArchCatalogJSON)
	require.NoError(t, err)

	bySystem := make(map[string][]string, len(baseline))
	for i := range entries {
		system := entries[i].definition.System
		bySystem[system] = append(bySystem[system], bareCoreName(entries[i].coreFile))
	}

	assert.Len(t, bySystem, len(baseline), "no system was added or dropped by the migration")
	for system, want := range baseline {
		assert.Equal(t, want, bySystem[system], "system %s", system)
	}
}

func bareCoreName(coreFile string) string {
	name := coreFile
	for _, suffix := range []string{"_libretro_android.so", "_libretro.so"} {
		if len(name) > len(suffix) && name[len(name)-len(suffix):] == suffix {
			return name[:len(name)-len(suffix)]
		}
	}
	return name
}
