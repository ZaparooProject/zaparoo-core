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
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type candidateCatalogFixture struct {
	System string   `json:"system"`
	Names  []string `json:"names"`
	Rows   int      `json:"rows"`
}

type candidateCatalogSystem struct {
	ID string `json:"id"`
}

type candidateCatalogMedia struct {
	Name   string                 `json:"name"`
	System candidateCatalogSystem `json:"system"`
}

type candidateCatalogPagination struct {
	NextCursor  string `json:"nextCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}

type candidateCatalogPage struct {
	Results    []candidateCatalogMedia    `json:"results"`
	Pagination candidateCatalogPagination `json:"pagination"`
}

type candidateCatalogResponse struct {
	Error  json.RawMessage      `json:"error"`
	Result candidateCatalogPage `json:"result"`
	ID     int                  `json:"id"`
}

// captureCandidateCatalog is an explicitly requested, read-only fixture generator.
// It uses the public search API on the authorized test device, not live DB files.
// Only names are saved: no paths, tokens, history, configuration or credentials.
func captureCandidateCatalog(b *testing.B, path string) {
	b.Helper()
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	//nolint:bodyclose // gorilla/websocket owns the handshake response body.
	conn, _, err := dialer.DialContext(b.Context(), "ws://10.0.0.107:7497/api/v0", nil)
	require.NoError(b, err)
	defer func() { require.NoError(b, conn.Close()) }()
	conn.SetReadLimit(8 << 20)
	systemID := os.Getenv("ZAPAROO_CANDIDATE_CATALOG_SYSTEM")
	if systemID == "" {
		systemID = "C64"
	}
	system, err := systemdefs.GetSystem(systemID)
	require.NoError(b, err)
	require.Equal(b, slugs.MediaTypeGame, system.GetMediaType(), "fixture seeding currently models games only")
	fixture := candidateCatalogFixture{System: system.ID}
	names := make(map[string]struct{})
	cursor := ""
	for page := 1; ; page++ {
		require.LessOrEqual(b, page, 1000, "capture pagination did not terminate")
		require.NoError(b, conn.SetWriteDeadline(time.Now().Add(30*time.Second)))
		require.NoError(b, conn.WriteJSON(map[string]any{
			"jsonrpc": "2.0", "id": page, "method": "media.search",
			"params": map[string]any{"systems": []string{fixture.System}, "maxResults": 1000, "cursor": cursor},
		}))
		require.NoError(b, conn.SetReadDeadline(time.Now().Add(30*time.Second)))
		var response candidateCatalogResponse
		for {
			require.NoError(b, conn.ReadJSON(&response))
			if response.ID == page {
				break
			}
		}
		require.True(b, len(response.Error) == 0 || string(response.Error) == "null",
			"capture API error: %s", response.Error)
		for _, media := range response.Result.Results {
			require.Equal(b, fixture.System, media.System.ID)
			names[media.Name] = struct{}{}
			fixture.Rows++
		}
		if page%10 == 0 {
			b.Logf("captured %d pages / %d public media rows", page, fixture.Rows)
		}
		if !response.Result.Pagination.HasNextPage {
			break
		}
		require.NotEmpty(b, response.Result.Results)
		require.NotEmpty(b, response.Result.Pagination.NextCursor)
		require.NotEqual(b, cursor, response.Result.Pagination.NextCursor)
		cursor = response.Result.Pagination.NextCursor
	}
	for name := range names {
		fixture.Names = append(fixture.Names, name)
	}
	slices.Sort(fixture.Names)
	require.NotEmpty(b, fixture.Names)
	//nolint:gosec // Explicit operator-supplied fixture output; never overwrite an existing capture.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(b, err)
	defer func() { require.NoError(b, file.Close()) }()
	require.NoError(b, json.NewEncoder(file).Encode(fixture))
	b.Skipf("captured %d public media rows / %d distinct names; timing skipped", fixture.Rows, len(fixture.Names))
}

func loadCandidateCatalog(t testing.TB) candidateCatalogFixture {
	t.Helper()
	path := os.Getenv("ZAPAROO_CANDIDATE_CATALOG_INPUT")
	if path == "" {
		t.Skip("set ZAPAROO_CANDIDATE_CATALOG_INPUT to a name-only fixture")
	}
	//nolint:gosec // Explicit operator-supplied, local validation fixture.
	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	var fixture candidateCatalogFixture
	require.NoError(t, json.NewDecoder(io.LimitReader(file, 20<<20)).Decode(&fixture))
	require.NotEmpty(t, fixture.Names)
	system, err := systemdefs.GetSystem(fixture.System)
	require.NoError(t, err)
	require.Equal(t, slugs.MediaTypeGame, system.GetMediaType(), "fixture seeding currently models games only")
	return fixture
}

// prepareCandidateCatalog can reuse a host-generated fixture so native runs
// measure queries/recovery rather than repeatedly inserting thousands of rows.
func prepareCandidateCatalog(t testing.TB, db *MediaDB, fixture candidateCatalogFixture) {
	t.Helper()
	if root := os.Getenv("ZAPAROO_CANDIDATE_CATALOG_DB_INPUT"); root != "" {
		loadCandidateFixtureDB(t, db, filepath.Join(root, fixture.System+".db"))
	} else {
		require.NoError(t, db.CreateSecondaryIndexes())
		seedCandidateTitles(t, db, fixture.System, fixture.Names...)
		_, err := db.sql.Load().ExecContext(t.Context(), "ANALYZE")
		require.NoError(t, err)
	}
	var titles, media int
	require.NoError(t, db.sql.Load().QueryRowContext(t.Context(),
		"SELECT (SELECT COUNT(*) FROM MediaTitles), (SELECT COUNT(*) FROM Media)").Scan(&titles, &media))
	require.Equal(t, len(fixture.Names), titles)
	require.Equal(t, len(fixture.Names), media)
	if root := os.Getenv("ZAPAROO_CANDIDATE_CATALOG_DB_EXPORT"); root != "" {
		//nolint:gosec // Explicit operator-selected test fixture export directory.
		require.NoError(t, os.MkdirAll(root, 0o700))
		path, err := filepath.Abs(filepath.Join(root, fixture.System+".db"))
		require.NoError(t, err)
		_, err = db.sql.Load().ExecContext(t.Context(), "VACUUM INTO ?", path)
		require.NoError(t, err)
		t.Skipf("exported name-only fixture %s; validation/timing skipped", path)
	}
}

// BenchmarkTitleCandidatesCatalog is offline unless CAPTURE is explicitly set.
// INPUT accepts the name-only JSON capture; normal test/benchmark runs skip it.
// This measures representative title distributions, not the live DB's tag or
// variant costs. Synthetic eligibility regressions remain separate.
func BenchmarkTitleCandidatesCatalog(b *testing.B) {
	if path := os.Getenv("ZAPAROO_CANDIDATE_CATALOG_CAPTURE"); path != "" {
		captureCandidateCatalog(b, path)
	}
	fixture := loadCandidateCatalog(b)
	db, cleanup := setupBrowseBenchMediaDB(b)
	defer cleanup()
	prepareCandidateCatalog(b, db, fixture)
	require.NoError(b, db.RebuildSlugSearchCache())
	b.Logf("%s: %d distinct API names from %d visible media rows", fixture.System, len(fixture.Names), fixture.Rows)
	type probe struct{ label, query string }
	probes := []probe{{"no-match", "zzzzzzzzqqqqqqqqvvvvvvvv"}}
	for _, position := range []struct {
		label string
		index int
	}{
		{"head", len(fixture.Names) / 10}, {"middle", len(fixture.Names) / 2}, {"tail", len(fixture.Names) * 9 / 10},
	} {
		name := fixture.Names[position.index]
		if !database.ValidTitleCandidateName(name) {
			continue
		}
		probes = append(probes, probe{"exact-" + position.label, name})
		// A spelling change can retain a valid secondary exact alias. Choose
		// a nearby genuine fuzzy query instead of timing that cheaper path
		// under a misleading typo label. Keep the fuzzy assertion below.
		fuzzyQuery := ""
		for offset := range min(32, len(fixture.Names)) {
			candidateName := fixture.Names[(position.index+offset)%len(fixture.Names)]
			query := candidateCatalogTypo(candidateName)
			if !database.ValidTitleCandidateName(query) {
				continue
			}
			matches, lookupErr := db.TitleCandidates(b.Context(), fixture.System, query, 5)
			require.NoError(b, lookupErr)
			if len(matches) > 0 && matches[0].MatchType == "fuzzy" {
				fuzzyQuery = query
				break
			}
		}
		require.NotEmpty(b, fuzzyQuery, "catalog region must provide a genuine fuzzy benchmark")
		b.Logf("%s fuzzy query: %q", position.label, fuzzyQuery)
		probes = append(probes, probe{"typo-" + position.label, fuzzyQuery})
	}
	for _, probe := range probes {
		b.Run(probe.label, func(b *testing.B) {
			initial, lookupErr := db.TitleCandidates(b.Context(), fixture.System, probe.query, 5)
			require.NoError(b, lookupErr)
			if strings.HasPrefix(probe.label, "typo-") {
				require.NotEmpty(b, initial)
				for _, result := range initial {
					require.Equal(b, "fuzzy", result.MatchType, "typo probe must exercise fuzzy traversal")
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				results, lookupErr := db.TitleCandidates(b.Context(), fixture.System, probe.query, 5)
				if lookupErr != nil {
					b.Fatal(lookupErr)
				}
				if len(results) > 5 || (len(results) == 0) != (probe.label == "no-match") {
					b.Fatalf("unexpected result count for %q: %d", probe.query, len(results))
				}
			}
			b.ReportMetric(float64(db.slugSearchCache.Load().Size()), "cache-B")
		})
	}
}
