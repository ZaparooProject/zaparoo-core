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

// These goldens are the Zaparoo Online remote-operations result contract:
// the snake_case shape each allowlisted query verb reports, produced by the
// explicit per-response encoders in wire.go from Core's camelCase models.
// Changing one of these shapes is a coordinated, deliberate contract change
// with the Online server, not a side effect of an unrelated local model
// edit — that is what a failing test here should prompt: update this golden
// AND the server (protocol doc §13.1, devicesim) together.

package remote

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustEncode(t *testing.T, encode func(any) (any, error), value any) string {
	t.Helper()
	wire, err := encode(value)
	require.NoError(t, err)
	encoded, err := json.Marshal(wire)
	require.NoError(t, err)
	return string(encoded)
}

func TestContract_SystemsResponse(t *testing.T) {
	t.Parallel()

	releaseDate := "1990-11-21"
	manufacturer := "Nintendo"
	mediaCount := 842
	response := models.SystemsResponse{
		Systems: []models.System{{
			ID: "SNES", Name: "Super Nintendo", Category: "Console",
			ReleaseDate: &releaseDate, Manufacturer: &manufacturer, MediaCount: &mediaCount,
			ZapScript: "**launch.system:SNES",
		}},
	}

	assert.JSONEq(t, `{
		"systems": [{
			"id": "SNES", "name": "Super Nintendo", "category": "Console",
			"release_date": "1990-11-21", "manufacturer": "Nintendo", "media_count": 842,
			"zap_script": "**launch.system:SNES"
		}]
	}`, mustEncode(t, encodeSystemsResponse, response))
}

func TestContract_LaunchersResponse(t *testing.T) {
	t.Parallel()

	response := models.LaunchersResponse{
		Launchers: []models.Launcher{{
			ID: "DualRAM3DO", SystemID: "3DO", SystemName: "3DO",
			Groups: []string{"libretro"}, Available: true, Default: true,
			LauncherRuntime: models.LauncherRuntime{
				Backend: models.LauncherBackendMisterCore,
				MisterCore: &models.MisterCoreInfo{
					Name: "3DO", File: "3DO_20250101.rbf", MGLPath: "_Console (Dual SDRAM)/3DO",
				},
			},
		}, {
			ID: "Missing", SystemID: "3DO", Available: false, AvailabilityReason: "core not installed: 3DO",
		}},
	}

	assert.JSONEq(t, `{
		"launchers": [{
			"id": "DualRAM3DO", "system_id": "3DO", "system_name": "3DO",
			"groups": ["libretro"], "available": true, "default": true,
			"backend": "mister_core",
			"mister_core": {
				"name": "3DO", "file": "3DO_20250101.rbf", "mgl_path": "_Console (Dual SDRAM)/3DO"
			}
		}, {
			"id": "Missing", "system_id": "3DO", "available": false,
			"availability_reason": "core not installed: 3DO"
		}]
	}`, mustEncode(t, encodeLaunchersResponse, &response))
}

func TestContract_SearchResults(t *testing.T) {
	t.Parallel()

	relPath := "SNES/Chrono Trigger.sfc"
	nextCursor := "cursor-1"
	response := models.SearchResults{
		Total: 1,
		Pagination: &models.PaginationInfo{
			NextCursor: &nextCursor, HasNextPage: true, PageSize: 1,
		},
		Results: []models.SearchResultMedia{{
			Name: "Chrono Trigger", Path: "/roms/SNES/Chrono Trigger.sfc",
			ZapScript: "**launch:/roms/SNES/Chrono Trigger.sfc", RelPath: &relPath,
			MediaID: 42, HasCover: true,
			System: models.System{ID: "SNES", Name: "Super Nintendo", Category: "Console"},
			Tags:   []database.TagInfo{{Tag: "rpg", Type: "genre", Label: "RPG", Count: 3}},
			DisambiguatingTags: []database.TagInfo{
				{Tag: "rev:1", Type: "revision"},
			},
		}},
	}

	assert.JSONEq(t, `{
		"total": 1,
		"pagination": {"next_cursor": "cursor-1", "has_next_page": true, "page_size": 1},
		"results": [{
			"name": "Chrono Trigger", "path": "/roms/SNES/Chrono Trigger.sfc",
			"zap_script": "**launch:/roms/SNES/Chrono Trigger.sfc",
			"relative_path": "SNES/Chrono Trigger.sfc",
			"media_id": 42, "has_cover": true,
			"system": {"id": "SNES", "name": "Super Nintendo", "category": "Console"},
			"tags": [{"tag": "rpg", "type": "genre", "label": "RPG", "count": 3}],
			"disambiguating_tags": [{"tag": "rev:1", "type": "revision"}]
		}]
	}`, mustEncode(t, encodeSearchResults, response))
}

func TestContract_BrowseResults(t *testing.T) {
	t.Parallel()

	systemID := "SNES"
	nextCursor := "cursor-2"
	fileCount := 12
	group := "Consoles"
	zapScript := "**launch.system:SNES"
	response := models.BrowseResults{
		Path: "/roms/SNES/", TotalFiles: 1, TotalDirs: 1,
		Pagination: &models.PaginationInfo{NextCursor: &nextCursor, HasNextPage: true, PageSize: 2},
		Entries: []models.BrowseEntry{{
			Type: "dir", Name: "SNES", Path: "/roms/SNES", SystemID: &systemID,
			SystemIDs: []string{"SNES"}, FileCount: &fileCount, Group: &group, ZapScript: &zapScript,
		}, {
			Type: "file", Name: "Chrono Trigger.sfc", Path: "/roms/SNES/Chrono Trigger.sfc",
			SystemID: &systemID, MediaID: 42, HasCover: true,
			Tags: []database.TagInfo{{Tag: "rpg", Type: "genre"}},
		}},
	}

	assert.JSONEq(t, `{
		"path": "/roms/SNES/", "total_files": 1, "total_dirs": 1,
		"pagination": {"next_cursor": "cursor-2", "has_next_page": true, "page_size": 2},
		"entries": [{
			"type": "dir", "name": "SNES", "path": "/roms/SNES", "system_id": "SNES",
			"system_ids": ["SNES"], "file_count": 12, "group": "Consoles",
			"zap_script": "**launch.system:SNES", "has_cover": false
		}, {
			"type": "file", "name": "Chrono Trigger.sfc", "path": "/roms/SNES/Chrono Trigger.sfc",
			"system_id": "SNES", "media_id": 42, "has_cover": true,
			"tags": [{"tag": "rpg", "type": "genre"}]
		}]
	}`, mustEncode(t, encodeBrowseResults, response))
}

func TestContract_VersionResponse(t *testing.T) {
	t.Parallel()

	response := models.VersionResponse{Version: "2.0.0", Platform: "mister"}
	assert.JSONEq(t, `{"version": "2.0.0", "platform": "mister"}`, mustEncode(t, encodeVersionResponse, response))
}

// TestContract_EncodersRejectForeignResponses pins that an encoder never
// guesses: a handler returning something other than its documented model
// is an internal error, not a silently empty or camelCase result.
func TestContract_EncodersRejectForeignResponses(t *testing.T) {
	t.Parallel()

	encoders := map[string]func(any) (any, error){
		"systems":   encodeSystemsResponse,
		"launchers": encodeLaunchersResponse,
		"search":    encodeSearchResults,
		"browse":    encodeBrowseResults,
		"version":   encodeVersionResponse,
	}
	for name, encode := range encoders {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := encode(map[string]string{"unexpected": "shape"})
			require.ErrorIs(t, err, errUnexpectedResponse)
			_, err = encode(nil)
			require.ErrorIs(t, err, errUnexpectedResponse)
		})
	}
}

// TestContract_EmptyResultVerbsReportEmptyObject pins that stop's wire
// result is a bare {} whatever the method returned.
func TestContract_EmptyResultVerbsReportEmptyObject(t *testing.T) {
	t.Parallel()

	assert.JSONEq(t, `{}`, mustEncode(t, encodeEmptyResult, map[string]any{"leaked": true}))
	assert.JSONEq(t, `{}`, mustEncode(t, encodeEmptyResult, nil))
}
