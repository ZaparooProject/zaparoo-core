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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// Keep this harness independent of TitleCandidates so the identical source can
// measure the feature's base revision. Captures contain public names, never paths
// or UserDB content. Every database/cache mutation is a disposable fixture write.
type slugLibraryFixture struct {
	Systems map[string][]string `json:"systems"`
	Rows    int                 `json:"rows"`
}

type slugLibrarySystem struct {
	ID string `json:"id"`
}

type slugLibraryMedia struct {
	Name   string            `json:"name"`
	System slugLibrarySystem `json:"system"`
}

type slugLibraryPagination struct {
	NextCursor  string `json:"nextCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}

type slugLibraryPage struct {
	Results    []slugLibraryMedia    `json:"results"`
	Pagination slugLibraryPagination `json:"pagination"`
}

type slugLibraryResponse struct {
	Error  json.RawMessage `json:"error"`
	Result slugLibraryPage `json:"result"`
	ID     int             `json:"id"`
}

func TestCaptureSlugLibrary(t *testing.T) {
	path := os.Getenv("ZAPAROO_SLUG_LIBRARY_CAPTURE")
	if path == "" {
		t.Skip("explicit name-only device capture not requested")
	}
	//nolint:bodyclose // websocket owns the handshake response.
	conn, _, err := websocket.DefaultDialer.DialContext(t.Context(), "ws://10.0.0.107:7497/api/v0", nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(16 << 20)
	names := make(map[string]map[string]struct{})
	fixture := slugLibraryFixture{Systems: make(map[string][]string)}
	cursor := ""
	for page := 1; ; page++ {
		require.Less(t, page, 1000)
		require.NoError(t, conn.SetWriteDeadline(time.Now().Add(30*time.Second)))
		require.NoError(t, conn.WriteJSON(map[string]any{
			"jsonrpc": "2.0", "id": page, "method": "media.search",
			"params": map[string]any{"maxResults": 1000, "cursor": cursor},
		}))
		var response slugLibraryResponse
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(30*time.Second)))
		for {
			require.NoError(t, conn.ReadJSON(&response))
			if response.ID == page {
				break
			}
		}
		require.True(t, len(response.Error) == 0 || string(response.Error) == "null", "%s", response.Error)
		for _, media := range response.Result.Results {
			require.NotEmpty(t, media.System.ID)
			if names[media.System.ID] == nil {
				names[media.System.ID] = make(map[string]struct{})
			}
			names[media.System.ID][media.Name] = struct{}{}
			fixture.Rows++
		}
		if page%25 == 0 {
			t.Logf("captured pages=%d rows=%d systems=%d", page, fixture.Rows, len(names))
		}
		if !response.Result.Pagination.HasNextPage {
			break
		}
		require.NotEmpty(t, response.Result.Results)
		require.NotEmpty(t, response.Result.Pagination.NextCursor)
		require.NotEqual(t, cursor, response.Result.Pagination.NextCursor)
		cursor = response.Result.Pagination.NextCursor
	}
	for system, entries := range names {
		for name := range entries {
			fixture.Systems[system] = append(fixture.Systems[system], name)
		}
		slices.Sort(fixture.Systems[system])
	}
	//nolint:gosec // Explicit operator-selected, exclusive fixture export.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	require.NoError(t, json.NewEncoder(file).Encode(fixture))
	t.Logf("captured rows=%d systems=%d", fixture.Rows, len(fixture.Systems))
}

func TestExportSlugLibrary(t *testing.T) {
	input, output := os.Getenv("ZAPAROO_SLUG_LIBRARY_INPUT"), os.Getenv("ZAPAROO_SLUG_LIBRARY_EXPORT")
	if input == "" || output == "" {
		t.Skip("explicit synthetic library export not requested")
	}
	//nolint:gosec // Explicit name-only fixture input.
	file, err := os.Open(input)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	var fixture slugLibraryFixture
	require.NoError(t, json.NewDecoder(io.LimitReader(file, 64<<20)).Decode(&fixture))
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	require.NoError(t, db.CreateSecondaryIndexes())
	systemIDs := make([]string, 0, len(fixture.Systems))
	for id := range fixture.Systems {
		systemIDs = append(systemIDs, id)
	}
	slices.Sort(systemIDs)
	count := 0
	for _, id := range systemIDs {
		system, systemErr := systemdefs.GetSystem(id)
		require.NoError(t, systemErr)
		row, insertErr := db.InsertSystem(database.System{SystemID: id, Name: id})
		require.NoError(t, insertErr)
		tx, txErr := db.sql.Load().BeginTx(t.Context(), nil)
		require.NoError(t, txErr)
		for _, name := range fixture.Systems[id] {
			metadata := GenerateSlugWithMetadata(system.GetMediaType(), name)
			result, titleErr := tx.ExecContext(t.Context(), `INSERT INTO MediaTitles
				(SystemDBID, Name, Slug, SecondarySlug, SlugLength, SlugWordCount) VALUES (?, ?, ?, ?, ?, ?)`,
				row.DBID, name, metadata.Slug, metadata.SecondarySlug, metadata.SlugLength, metadata.SlugWordCount)
			require.NoError(t, titleErr)
			titleID, idErr := result.LastInsertId()
			require.NoError(t, idErr)
			path := filepath.Join(string(filepath.Separator), "media", "fat", "games", id,
				fmt.Sprintf("fixture-%d.rom", titleID))
			_, mediaErr := tx.ExecContext(t.Context(),
				"INSERT INTO Media (SystemDBID, MediaTitleDBID, Path, ParentDir) VALUES (?, ?, ?, ?)",
				row.DBID, titleID, path, filepath.Dir(path))
			require.NoError(t, mediaErr)
			count++
		}
		require.NoError(t, tx.Commit())
	}
	require.NoError(t, db.SetIndexingStatus(IndexingStatusCompleted))
	require.NoError(t, db.UpdateLastGenerated())
	// Model a completed index, not a legacy database awaiting one-time repairs.
	// Run the actual maintenance path rather than inventing completion markers.
	db.RunBackgroundOptimization(nil, nil)
	pending, pendingErr := db.TemporaryRepairJobsPending(t.Context())
	require.NoError(t, pendingErr)
	require.False(t, pending)
	_, err = db.sql.Load().ExecContext(t.Context(), "ANALYZE")
	require.NoError(t, err)
	_, err = db.sql.Load().ExecContext(t.Context(), "VACUUM INTO ?", output)
	require.NoError(t, err)
	t.Logf("exported systems=%d titles=%d from_public_media_rows=%d", len(systemIDs), count, fixture.Rows)
}

func copySlugValidationFile(t *testing.T, source, target string) {
	t.Helper()
	//nolint:gosec // Operator-selected fixture; target belongs to disposable test directory.
	in, err := os.Open(source)
	require.NoError(t, err)
	defer func() { _ = in.Close() }()
	//nolint:gosec // Explicit operator-selected test fixture output directory.
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	//nolint:gosec // Operator-selected fixture output; never overwrite an existing file.
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = io.Copy(out, in)
	require.NoError(t, err)
	require.NoError(t, out.Close())
}

func slugValidationRSS(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		return "unavailable"
	}
	status, err := os.ReadFile("/proc/self/status")
	require.NoError(t, err)
	var fields []string
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "VmRSS:") || strings.HasPrefix(line, "VmHWM:") {
			fields = append(fields, strings.TrimSpace(line))
		}
	}
	return strings.Join(fields, ";")
}

func TestSlugLibraryCacheMeasurement(t *testing.T) {
	input, mode := os.Getenv("ZAPAROO_SLUG_LIBRARY_DB"), os.Getenv("ZAPAROO_SLUG_LIBRARY_MODE")
	if input == "" || mode == "" {
		t.Skip("explicit fresh-process cache measurement not requested")
	}
	require.Contains(t, []string{"build", "load", "repeat", "contended"}, mode)
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	require.NoError(t, db.Close())
	database.RemoveSidecars(db.dbPath)
	require.NoError(t, os.Remove(db.dbPath))
	copySlugValidationFile(t, input, db.dbPath)
	require.NoError(t, db.Open())
	var titles int
	require.NoError(t, db.sql.Load().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM MediaTitles").Scan(&titles))
	if mode == "load" {
		copySlugValidationFile(t, os.Getenv("ZAPAROO_SLUG_LIBRARY_CACHE"), db.slugSearchCachePath())
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	rssBefore := slugValidationRSS(t)
	start := time.Now()
	switch mode {
	case "load":
		loaded, err := db.LoadCachedSlugSearchCache()
		require.NoError(t, err)
		require.True(t, loaded)
	case "contended":
		built := make(chan error, 1)
		go func() { built <- db.RebuildSlugSearchCache() }()
		for sample := range 6 {
			readStarted := time.Now()
			results, readErr := db.SearchMediaBySlug(t.Context(), "NES", "metroid", nil)
			duration := time.Since(readStarted)
			require.NoError(t, readErr)
			require.NotEmpty(t, results)
			t.Logf("FOREGROUND sample=%d elapsed_ns=%d", sample, duration)
		}
		require.NoError(t, <-built)
	default:
		require.NoError(t, db.RebuildSlugSearchCache())
	}
	elapsed := time.Since(start)
	runtime.GC()
	runtime.ReadMemStats(&after)
	cache := db.slugSearchCache.Load()
	require.NotNil(t, cache)
	require.Equal(t, titles, cache.entryCount)
	require.True(t, cache.complete)
	t.Logf("MEASURE mode=%s elapsed_ns=%d titles=%d systems=%d heap_before=%d heap_after=%d "+
		"cache_bytes=%d rss_before=%q rss_after=%q",
		mode, elapsed, titles, len(cache.systemRanges), before.HeapAlloc, after.HeapAlloc, cache.Size(),
		rssBefore, slugValidationRSS(t))
	digest := slugLibrarySearchDigest(t, cache)
	broken := *cache
	broken.entryCount = 0
	require.NotEqual(t, digest, slugLibrarySearchDigest(t, &broken), "comparison must notice missing entries")
	t.Logf("SEARCH_DIGEST %s", digest)
	if mode == "repeat" {
		for cycle := range 12 {
			require.NoError(t, db.RebuildSlugSearchCache())
			runtime.GC()
			var current runtime.MemStats
			runtime.ReadMemStats(&current)
			t.Logf("REPEAT cycle=%d heap=%d rss=%q", cycle, current.HeapAlloc, slugValidationRSS(t))
		}
	}
	if output := os.Getenv("ZAPAROO_SLUG_LIBRARY_CACHE_EXPORT"); output != "" {
		require.NoError(t, db.PersistSlugSearchCache())
		copySlugValidationFile(t, db.slugSearchCachePath(), output)
	}
	runtime.KeepAlive(db)
}

func slugLibrarySearchDigest(t *testing.T, cache *SlugSearchCache) string {
	t.Helper()
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	systems := make([]int64, 0, len(cache.systemRanges))
	for id := range cache.systemRanges {
		systems = append(systems, id)
	}
	slices.Sort(systems)
	for _, id := range systems {
		for _, query := range []string{"", "a", "mario", "quest", "zzzzqqqqvvvv"} {
			ids := cache.Search([]int64{id}, [][][]byte{{[]byte(query)}})
			slices.Sort(ids)
			require.NoError(t, encoder.Encode(ids))
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}
