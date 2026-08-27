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

// These goldens are the Zaparoo Online remote-operations contract: since
// deleting wire.go, an allowlisted operation's result is the exact camelCase
// shape pkg/api/models already documents publicly, with no second mirror to
// keep in sync by hand. Changing one of these shapes is a coordinated,
// deliberate contract change with the Online server, not a side effect of
// an unrelated local model edit — that is what a failing test here should
// prompt: update this golden AND the server together.

package remote

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustMarshal(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
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
			"releaseDate": "1990-11-21", "manufacturer": "Nintendo", "mediaCount": 842,
			"zapScript": "**launch.system:SNES"
		}]
	}`, mustMarshal(t, response))
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
		}},
	}

	assert.JSONEq(t, `{
		"launchers": [{
			"id": "DualRAM3DO", "systemId": "3DO", "systemName": "3DO",
			"groups": ["libretro"], "available": true, "default": true,
			"backend": "mister_core",
			"misterCore": {
				"name": "3DO", "file": "3DO_20250101.rbf", "mglPath": "_Console (Dual SDRAM)/3DO"
			}
		}]
	}`, mustMarshal(t, response))
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
		"pagination": {"nextCursor": "cursor-1", "hasNextPage": true, "pageSize": 1},
		"results": [{
			"name": "Chrono Trigger", "path": "/roms/SNES/Chrono Trigger.sfc",
			"zapScript": "**launch:/roms/SNES/Chrono Trigger.sfc",
			"relativePath": "SNES/Chrono Trigger.sfc",
			"mediaId": 42, "hasCover": true,
			"system": {"id": "SNES", "name": "Super Nintendo", "category": "Console"},
			"tags": [{"tag": "rpg", "type": "genre", "label": "RPG", "count": 3}],
			"disambiguatingTags": [{"tag": "rev:1", "type": "revision"}]
		}]
	}`, mustMarshal(t, response))
}

func TestContract_BrowseResults(t *testing.T) {
	t.Parallel()

	systemID := "SNES"
	nextCursor := "cursor-2"
	response := models.BrowseResults{
		Path: "/roms/SNES/", TotalFiles: 1, TotalDirs: 0,
		Pagination: &models.PaginationInfo{NextCursor: &nextCursor, HasNextPage: true, PageSize: 1},
		Entries: []models.BrowseEntry{{
			Type: "file", Name: "Chrono Trigger.sfc", Path: "/roms/SNES/Chrono Trigger.sfc",
			SystemID: &systemID, MediaID: 42, HasCover: true,
			Tags: []database.TagInfo{{Tag: "rpg", Type: "genre"}},
		}},
	}

	assert.JSONEq(t, `{
		"path": "/roms/SNES/", "totalFiles": 1, "totalDirs": 0,
		"pagination": {"nextCursor": "cursor-2", "hasNextPage": true, "pageSize": 1},
		"entries": [{
			"type": "file", "name": "Chrono Trigger.sfc", "path": "/roms/SNES/Chrono Trigger.sfc",
			"systemId": "SNES", "mediaId": 42, "hasCover": true,
			"tags": [{"tag": "rpg", "type": "genre"}]
		}]
	}`, mustMarshal(t, response))
}

func TestContract_VersionResponse(t *testing.T) {
	t.Parallel()

	response := models.VersionResponse{Version: "2.0.0", Platform: "mister"}
	assert.JSONEq(t, `{"version": "2.0.0", "platform": "mister"}`, mustMarshal(t, response))
}
