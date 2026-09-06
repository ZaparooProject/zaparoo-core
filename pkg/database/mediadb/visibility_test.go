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
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHiddenOverlayDoesNotShadowVisibleEntries(t *testing.T) {
	t.Parallel()
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%t", cached), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f, cleanup := setupMergeFixture(t, 2)
			t.Cleanup(cleanup)
			hidden := []string{
				filepath.Join(f.roots[0], "Duplicate.nes"),
				filepath.Join(f.roots[0], "Shadow.nes", "Child.nes"),
				filepath.Join(f.roots[0], "Blocking.nes"),
			}
			visible := []string{
				filepath.Join(f.roots[1], "Duplicate.nes"),
				filepath.Join(f.roots[1], "Shadow.nes"),
			}
			for _, path := range append(append([]string{}, hidden...), visible...) {
				f.insert(filepath.Base(path), filepath.ToSlash(path))
			}
			f.insert("Child", filepath.ToSlash(filepath.Join(f.roots[1], "Blocking.nes", "Child.nes")))
			f.commit(t, cached)
			system, err := f.mediaDB.FindSystemBySystemID("NES")
			require.NoError(t, err)
			for _, path := range hidden {
				media, lookupErr := f.mediaDB.FindMediaBySystemAndPath(ctx, system.DBID, filepath.ToSlash(path))
				require.NoError(t, lookupErr)
				require.NoError(t, f.mediaDB.UpdateMediaTags(ctx, media.DBID, nil,
					[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
			}
			systems := []systemdefs.System{f.system}
			files, err := f.mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
				Overlay: f.overlay(), Systems: systems, ExcludeHidden: true, Limit: 10,
			})
			require.NoError(t, err)
			paths := make([]string, 0, len(files))
			for _, file := range files {
				paths = append(paths, file.Path)
			}
			assert.ElementsMatch(t, []string{filepath.ToSlash(visible[0]), filepath.ToSlash(visible[1])}, paths)
			count, err := f.mediaDB.BrowseFileCount(ctx, database.BrowseFileCountOptions{
				Overlay: f.overlay(), Systems: systems, ExcludeHidden: true,
			})
			require.NoError(t, err)
			assert.Equal(t, 2, count)
			for _, sort := range []string{"name-asc", "filename-asc"} {
				index, indexErr := f.mediaDB.BrowseIndex(ctx, database.BrowseIndexOptions{
					Overlay: f.overlay(), Systems: systems, ExcludeHidden: true, Sort: sort,
				})
				require.NoError(t, indexErr)
				assert.Equal(t, 2, index.TotalFiles)
			}
			dirs, err := f.mediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{
				Overlay: f.overlay(), Systems: systems, ExcludeHidden: true,
			})
			require.NoError(t, err)
			require.Len(t, dirs, 1)
			assert.Equal(t, "Blocking.nes", dirs[0].Name)
			assert.Equal(t, 1, dirs[0].FileCount)
			dirCount, err := f.mediaDB.BrowseDirCount(ctx, database.BrowseDirCountOptions{
				Overlay: f.overlay(), Systems: systems, ExcludeHidden: true,
			})
			require.NoError(t, err)
			assert.Equal(t, 1, dirCount)
		})
	}
}
