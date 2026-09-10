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
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaceDirectoryProperties_ReplacesSnapshotAndTracksChanges(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	dir := filepath.ToSlash(filepath.Join("roms", "NES", "Collection"))
	boxart := filepath.ToSlash(filepath.Join("roms", "NES", "media", "boxart", "Collection.png"))

	changed, err := mediaDB.ReplaceDirectoryProperties(ctx, 1, []database.DirectoryProperty{{
		Path: dir + "/", TypeTag: "property:image-boxart", Text: boxart,
	}})
	require.NoError(t, err)
	assert.True(t, changed)

	props, err := mediaDB.GetDirectoryProperties(ctx, 1, dir)
	require.NoError(t, err)
	require.Len(t, props, 1)
	assert.Equal(t, "property:image-boxart", props[0].TypeTag)
	assert.Equal(t, boxart, props[0].Text)

	systems, all := mediaDB.ConsumeScrapeImageChanges()
	assert.False(t, all)
	assert.Equal(t, []string{"NES"}, systems)

	changed, err = mediaDB.ReplaceDirectoryProperties(ctx, 1, []database.DirectoryProperty{{
		Path: dir, TypeTag: "property:image-boxart", Text: boxart,
	}})
	require.NoError(t, err)
	assert.False(t, changed, "identical normalized snapshot must not rewrite")
	systems, all = mediaDB.ConsumeScrapeImageChanges()
	assert.False(t, all)
	assert.Empty(t, systems)

	changed, err = mediaDB.ReplaceDirectoryProperties(ctx, 1, nil)
	require.NoError(t, err)
	assert.True(t, changed)
	props, err = mediaDB.GetDirectoryProperties(ctx, 1, dir)
	require.NoError(t, err)
	assert.Empty(t, props)
}

func TestDirectoryCoverQueryPlan_UsesPathIndex(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	dir := browseTestPath("roms", "Collection")
	_, err := mediaDB.ReplaceDirectoryProperties(ctx, 1, []database.DirectoryProperty{{
		Path: dir, TypeTag: "property:image-boxart", Text: "collection.png",
	}})
	require.NoError(t, err)

	rows, err := mediaDB.sql.Load().QueryContext(ctx, `
		EXPLAIN QUERY PLAN
		SELECT DISTINCT dp.Path, s.SystemID
		FROM DirectoryProperties dp INDEXED BY directoryproperties_path_system_idx
		JOIN Systems s ON s.DBID = dp.SystemDBID
		WHERE dp.Path IN (?) AND dp.TypeTagDBID IN (?)
	`, dir, int64(2))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		details = append(details, detail)
	}
	require.NoError(t, rows.Err())
	plan := strings.Join(details, "\n")
	assert.Contains(t, plan, "directoryproperties_path_system_idx", plan)
}

func TestBrowseDirectories_DirectoryCoverRespectsSystems(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	root := browseTestDir("roms")
	dir := browseTestPath("roms", "Shared")
	parentDir := dir + "/"

	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (2, 'SNES', 'Super Nintendo');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (2, 2, 'zelda', 'Zelda');
		UPDATE Media SET Path = ?, ParentDir = ? WHERE DBID = 1;
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir)
			VALUES (2, 2, 2, ?, ?);
	`, filepath.ToSlash(filepath.Join(dir, "Mario.nes")), parentDir,
		filepath.ToSlash(filepath.Join(dir, "Zelda.sfc")), parentDir)
	require.NoError(t, err)
	_, err = mediaDB.ReplaceDirectoryProperties(ctx, 1, []database.DirectoryProperty{{
		Path: dir, TypeTag: "property:image-boxart", Text: "shared.png",
	}})
	require.NoError(t, err)
	require.NoError(t, sqlPopulateBrowseCache(ctx, mediaDB.sql.Load()))

	nes, err := sqlBrowseDirectories(ctx, mediaDB.sql.Load(), database.BrowseDirectoriesOptions{
		PathPrefix: root, Systems: []systemdefs.System{{ID: "NES"}},
	})
	require.NoError(t, err)
	require.Len(t, nes, 1)
	assert.True(t, nes[0].HasCover)

	snes, err := sqlBrowseDirectories(ctx, mediaDB.sql.Load(), database.BrowseDirectoriesOptions{
		PathPrefix: root, Systems: []systemdefs.System{{ID: "SNES"}},
	})
	require.NoError(t, err)
	require.Len(t, snes, 1)
	assert.False(t, snes[0].HasCover)
}

func TestDirectoryProperties_SurviveBrowseCacheRebuild(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	dir := filepath.ToSlash(filepath.Join("roms", "NES"))

	_, err := mediaDB.ReplaceDirectoryProperties(ctx, 1, []database.DirectoryProperty{{
		Path: dir, TypeTag: "property:image-boxart", Text: "folder.png",
	}})
	require.NoError(t, err)
	require.NoError(t, sqlPopulateBrowseCache(ctx, mediaDB.sql.Load()))

	props, err := mediaDB.GetDirectoryProperties(ctx, 1, dir)
	require.NoError(t, err)
	require.Len(t, props, 1)
	assert.Equal(t, "folder.png", props[0].Text)
}

func TestReplaceDirectoryProperties_IsolatesSamePathBySystem(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (2, 'SNES', 'Super Nintendo')
	`)
	require.NoError(t, err)

	dir := filepath.ToSlash(filepath.Join("roms", "Shared"))
	_, err = mediaDB.ReplaceDirectoryProperties(ctx, 1, []database.DirectoryProperty{{
		Path: dir, TypeTag: "property:image-boxart", Text: "nes.png",
	}})
	require.NoError(t, err)
	_, err = mediaDB.ReplaceDirectoryProperties(ctx, 2, []database.DirectoryProperty{{
		Path: dir, TypeTag: "property:image-boxart", Text: "snes.png",
	}})
	require.NoError(t, err)

	nes, err := mediaDB.GetDirectoryProperties(ctx, 1, dir)
	require.NoError(t, err)
	snes, err := mediaDB.GetDirectoryProperties(ctx, 2, dir)
	require.NoError(t, err)
	require.Len(t, nes, 1)
	require.Len(t, snes, 1)
	assert.Equal(t, "nes.png", nes[0].Text)
	assert.Equal(t, "snes.png", snes[0].Text)

	_, err = mediaDB.ReplaceDirectoryProperties(ctx, 1, nil)
	require.NoError(t, err)
	snes, err = mediaDB.GetDirectoryProperties(ctx, 2, dir)
	require.NoError(t, err)
	require.Len(t, snes, 1)
	assert.Equal(t, "snes.png", snes[0].Text)

	require.NoError(t, mediaDB.TruncateSystems([]string{"SNES"}))
	snes, err = mediaDB.GetDirectoryProperties(ctx, 2, dir)
	require.NoError(t, err)
	assert.Empty(t, snes, "system truncation must explicitly remove directory properties")
}

func TestReplaceDirectoryProperties_RejectsInvalidRows(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	_, err := mediaDB.ReplaceDirectoryProperties(ctx, 1, []database.DirectoryProperty{{
		Path: "roms/NES/Games", TypeTag: "property:description", Text: "not image",
	}})
	require.ErrorContains(t, err, "not an image property")

	_, err = mediaDB.ReplaceDirectoryProperties(ctx, 1, []database.DirectoryProperty{{
		Path: "roms/NES/Games", TypeTag: "property:image-boxart", Text: "",
	}})
	require.ErrorContains(t, err, "empty text")

	_, err = mediaDB.ReplaceDirectoryProperties(ctx, 999, nil)
	require.ErrorContains(t, err, "not found")
}
