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

package mediadb

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/require"
)

type libraryCandidateProbe struct {
	system    string
	query     string
	want      []database.TitleCandidate
	canonical bool
}

// Exercise every captured system, including non-game media, against the full
// SQL fallback. The shared fixture is never opened for writes.
func TestTitleCandidatesFullLibraryParity(t *testing.T) {
	input, names := os.Getenv("ZAPAROO_SLUG_LIBRARY_DB"), os.Getenv("ZAPAROO_SLUG_LIBRARY_INPUT")
	if input == "" || names == "" {
		t.Skip("explicit generated full-library fixture not supplied")
	}
	//nolint:gosec // Operator-selected name-only fixture.
	file, err := os.Open(names)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	var fixture slugLibraryFixture
	require.NoError(t, json.NewDecoder(io.LimitReader(file, 64<<20)).Decode(&fixture))
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	require.NoError(t, db.Close())
	_ = database.RemoveSidecars(db.dbPath)
	require.NoError(t, os.Remove(db.dbPath))
	copySlugValidationFile(t, input, db.dbPath)
	require.NoError(t, db.Open())
	require.Nil(t, db.slugSearchCache.Load())
	var probes []libraryCandidateProbe
	for _, system := range slices.Sorted(maps.Keys(fixture.Systems)) {
		entries := fixture.Systems[system]
		chosen := ""
		for offset := range entries {
			name := entries[(len(entries)/2+offset)%len(entries)]
			if database.ValidTitleCandidateName(name) {
				chosen = name
				break
			}
		}
		require.NotEmpty(t, chosen, "system %s has no valid canonical probe", system)
		probes = append(probes, libraryCandidateProbe{system: system, query: chosen, canonical: true})
		if typo := candidateCatalogTypo(chosen); typo != "" {
			probes = append(probes, libraryCandidateProbe{system: system, query: typo})
		}
	}
	measure := func(probe libraryCandidateProbe) []database.TitleCandidate {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		got, lookupErr := db.TitleCandidates(ctx, probe.system, probe.query, 5)
		require.NoError(t, lookupErr, "%s/%s", probe.system, probe.query)
		return got
	}
	fuzzy := 0
	for i := range probes {
		probes[i].want = measure(probes[i])
		if probes[i].canonical {
			require.NotEmpty(t, probes[i].want, "canonical title in %s", probes[i].system)
		}
		if len(probes[i].want) > 0 && probes[i].want[0].MatchType == "fuzzy" {
			fuzzy++
		}
	}
	require.Positive(t, fuzzy, "fixture must exercise successful fuzzy traversal")
	require.NoError(t, db.RebuildSlugSearchCache())
	check := func(phase string) {
		t.Helper()
		for _, probe := range probes {
			require.Equal(t, probe.want, measure(probe), "%s: %s/%s", phase, probe.system, probe.query)
		}
	}
	check("warm")
	require.NoError(t, db.PersistSlugSearchCache())
	db.slugSearchCache.Store(nil)
	loaded, err := db.LoadCachedSlugSearchCache()
	require.NoError(t, err)
	require.True(t, loaded)
	check("persisted")
	previous := db.slugSearchCache.Load()
	_, err = db.sql.Load().ExecContext(t.Context(),
		"UPDATE Media SET IsMissing = 1 WHERE DBID = (SELECT MAX(DBID) FROM Media)")
	require.NoError(t, err)
	func() {
		db.slugCacheState.buildMu.Lock()
		defer db.slugCacheState.buildMu.Unlock()
		deleted, cleanErr := db.CleanMediaOrphans(t.Context())
		require.NoError(t, cleanErr)
		require.Equal(t, int64(1), deleted)
		require.Nil(t, db.slugSearchCache.Load())
		// Hold publication while capturing post-cleanup SQL truth. The worker
		// must later recover every system, not just the system that lost a row.
		for i := range probes {
			probes[i].want = measure(probes[i])
		}
	}()
	db.WaitForBackgroundOperations()
	require.NotNil(t, db.slugSearchCache.Load())
	require.NotSame(t, previous, db.slugSearchCache.Load())
	require.True(t, db.slugSearchCache.Load().complete)
	check("recovered")
	t.Logf("FULL_LIBRARY_PARITY systems=%d probes=%d fuzzy=%d warm=true persisted=true recovered=true",
		len(fixture.Systems), len(probes), fuzzy)
}
