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

package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

type candidateWireResponse struct {
	Error  *models.ErrorObject `json:"error"`
	Result json.RawMessage     `json:"result"`
	ID     int                 `json:"id"`
}

// Exercise production route registration, dispatch and JSON encoding with real
// SQLite over both transports. Only hardware and UserDB boundaries are mocked;
// the listener and every writable path belong to this test, including on MiSTer.
func TestMediaLookupCandidatesTransports(t *testing.T) {
	t.Parallel()
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()})
	platform.SetupBasicMock()
	mediaDB, err := mediadb.OpenMediaDB(t.Context(), platform)
	require.NoError(t, err)
	defer func() { require.NoError(t, mediaDB.Close()) }()
	require.NoError(t, mediaDB.CreateSecondaryIndexes())
	seedCandidateTransportDB(t, mediaDB)
	cfg, err := helpers.NewTestConfigWithListenAndPort(helpers.NewMemoryFS(), t.TempDir(), "127.0.0.1", 0)
	require.NoError(t, err)
	st, notifications := state.NewState(platform, "candidate-transport-test")
	userDB := helpers.NewMockUserDBI()
	db := &database.Database{UserDB: userDB, MediaDB: mediaDB}
	queue := make(chan tokens.Token, 1)
	ready, stopped := make(chan error, 1), make(chan error, 1)
	go func() {
		stopped <- StartWithReady(platform, cfg, st, queue, nil, db,
			nil, nil, newTestBroker(st.GetContext(), notifications), nil, nil, nil, nil, nil, nil, ready)
	}()
	defer func() {
		st.StopService()
		require.NoError(t, <-stopped)
		require.Empty(t, queue, "candidate discovery must not enqueue a launch")
	}()
	select {
	case bindErr := <-ready:
		require.NoError(t, bindErr)
	case <-time.After(10 * time.Second):
		t.Fatal("isolated candidate API did not bind")
	}
	address := fmt.Sprintf("127.0.0.1:%d/api/v0", cfg.APIPort())
	ws := dialWS(t, "ws://"+address)
	defer func() { _ = ws.Close() }()
	client := &http.Client{Timeout: 30 * time.Second}
	defer client.CloseIdleConnections()
	call := func(transport, method string, params any) candidateWireResponse {
		t.Helper()
		payload, marshalErr := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
		})
		require.NoError(t, marshalErr)
		var response candidateWireResponse
		if transport == "ws" {
			require.NoError(t, ws.SetWriteDeadline(time.Now().Add(30*time.Second)))
			require.NoError(t, ws.WriteMessage(1, payload))
			require.NoError(t, ws.SetReadDeadline(time.Now().Add(30*time.Second)))
			require.NoError(t, ws.ReadJSON(&response))
		} else {
			req, requestErr := http.NewRequestWithContext(
				t.Context(), http.MethodPost, "http://"+address, bytes.NewReader(payload))
			require.NoError(t, requestErr)
			req.Header.Set("Content-Type", "application/json")
			httpResponse, httpErr := client.Do(req)
			require.NoError(t, httpErr)
			defer func() { _ = httpResponse.Body.Close() }()
			require.Equal(t, http.StatusOK, httpResponse.StatusCode)
			require.NoError(t, json.NewDecoder(httpResponse.Body).Decode(&response))
		}
		require.Equal(t, 1, response.ID)
		return response
	}
	for _, phase := range []string{"cold", "warm", "recovered"} {
		switch phase {
		case "warm":
			require.NoError(t, mediaDB.RebuildSlugSearchCache())
		case "recovered":
			response := call("http", "media.clean.orphans", map[string]any{})
			require.Nil(t, response.Error)
			require.JSONEq(t, `{"deleted":1}`, string(response.Result))
			mediaDB.WaitForBackgroundOperations()
			require.True(t, mediaDB.CanServeSystemsFromSlugCacheForTesting([]string{"NES"}))
		}
		for _, transport := range []string{"http", "ws"} {
			for _, probe := range []struct{ query, name, kind string }{
				{"Metroid", "Metroid", "exact"},
				{"Metriod", "Metroid", "fuzzy"},
				{"Ocarina of Time", "Legend of Zelda: Ocarina of Time", "secondary"},
				{"Super Mario Bros", "", ""},
				{"Chrono Trigger", "", ""},
				{"zzzzqqqqvvvv", "", ""},
			} {
				started := time.Now()
				response := call(transport, "media.lookup.candidates",
					map[string]any{"system": "NES", "name": probe.query})
				elapsed := time.Since(started)
				require.Nil(t, response.Error)
				var result models.MediaLookupCandidatesResponse
				require.NoError(t, json.Unmarshal(response.Result, &result))
				if probe.name == "" {
					require.JSONEq(t, `{"candidates":[]}`, string(response.Result))
				} else {
					require.Len(t, result.Candidates, 1)
					candidate := result.Candidates[0]
					require.Equal(t, probe.name, candidate.Name)
					require.Equal(t, probe.kind, candidate.MatchType)
					require.Equal(t, "NES", candidate.SystemID)
					require.Equal(t, 1, candidate.Rank)
				}
				t.Logf("phase=%s transport=%s query=%q elapsed_ns=%d", phase, transport, probe.query, elapsed)
			}
			invalid := call(transport, "media.lookup.candidates",
				map[string]any{"system": "NES", "name": "Metroid", "maxResults": 6})
			require.NotNil(t, invalid.Error, "wire harness must detect rejected input")
		}
	}
}

func seedCandidateTransportDB(t *testing.T, db *mediadb.MediaDB) {
	t.Helper()
	system, err := db.InsertSystem(database.System{SystemID: "NES", Name: "NES"})
	require.NoError(t, err)
	for _, name := range []string{"Metroid", "Legend of Zelda: Ocarina of Time", "Super Mario Bros", "Chrono Trigger"} {
		metadata := mediadb.GenerateSlugWithMetadata(slugs.MediaTypeGame, name)
		title, titleErr := db.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: system.DBID, Name: name, Slug: metadata.Slug,
			SecondarySlug: sql.NullString{String: metadata.SecondarySlug, Valid: metadata.SecondarySlug != ""},
			SlugLength:    metadata.SlugLength, SlugWordCount: metadata.SlugWordCount,
		})
		require.NoError(t, titleErr)
		media, mediaErr := db.InsertMedia(database.Media{
			SystemDBID: system.DBID, MediaTitleDBID: title.DBID,
			Path: filepath.Join("roms", "NES", name+".nes"),
		})
		require.NoError(t, mediaErr)
		if name == "Super Mario Bros" {
			require.NoError(t, db.UpdateMediaTags(t.Context(), media.DBID, nil,
				[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
		}
		if name == "Chrono Trigger" {
			_, err = db.UnsafeGetSQLDb().ExecContext(
				t.Context(), "UPDATE Media SET IsMissing = 1 WHERE DBID = ?", media.DBID)
			require.NoError(t, err)
		}
	}
}
