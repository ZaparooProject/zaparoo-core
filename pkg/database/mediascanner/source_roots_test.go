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

package mediascanner

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// sourceTestPlatform serves source roots from an in-memory tree keyed by the
// directory's canonical path.
type sourceTestPlatform struct {
	*mocks.MockPlatform
	rootsErr error
	readErr  map[string]error
	tree     map[string][]platforms.SourceEntry
	roots    []string
	reads    []string
}

func (p *sourceTestPlatform) SourceRoots(context.Context) ([]string, error) {
	return p.roots, p.rootsErr
}

func (p *sourceTestPlatform) ReadSourceDir(_ context.Context, path string) ([]platforms.SourceEntry, error) {
	p.reads = append(p.reads, path)
	if err := p.readErr[path]; err != nil {
		return nil, err
	}
	return p.tree[path], nil
}

func file(name string) platforms.SourceEntry { return platforms.SourceEntry{Name: name, Size: 1} }
func folder(name string) platforms.SourceEntry {
	return platforms.SourceEntry{Name: name, Size: -1, Dir: true}
}

func mustSourcePath(t *testing.T, root string, segments ...string) string {
	t.Helper()
	id, _, err := platforms.SourceLocation(root)
	require.NoError(t, err)
	path, err := sourcePath(id, segments)
	require.NoError(t, err)
	return path
}

type sourceIndex struct {
	pl      *sourceTestPlatform
	index   func() (int, error)
	present func() []string
	db      *database.Database
}

func sourceIndexFixture(t *testing.T) sourceIndex {
	t.Helper()
	cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	launchers := []platforms.Launcher{{
		ID: "NESCore", SystemID: systemdefs.SystemNES,
		Folders: []string{"NES"}, Extensions: []string{".nes"},
	}}
	base := mocks.NewMockPlatform()
	base.On("ID").Return("source-test")
	base.On("Settings").Return(platforms.Settings{})
	base.On("RootDirs", mock.Anything).Return([]string{})
	base.On("Launchers", mock.Anything).Return(launchers)

	root := platforms.SourceRootPath("granted-tree")
	pl := &sourceTestPlatform{MockPlatform: base, roots: []string{root}, readErr: map[string]error{}}
	nes := mustSourcePath(t, root, "nes")
	pl.tree = map[string][]platforms.SourceEntry{
		root: {folder("nes"), folder("Other"), file("readme.txt")},
		nes: {
			file("Fixture + 50% (USA).nes"), file(".hidden.nes"), file("._Apple.nes"), file("notes.txt"),
			folder("Sub"), folder(".git"), folder("__MACOSX"), folder("Ignored"),
		},
		mustSourcePath(t, root, "nes", "Sub"):      {file("Deep #2.nes")},
		mustSourcePath(t, root, "nes", ".git"):     {file("hidden-dir.nes")},
		mustSourcePath(t, root, "nes", "__MACOSX"): {file("resource.nes")},
		mustSourcePath(t, root, "nes", "Ignored"):  {file(".zaparooignore"), file("skipped.nes")},
	}

	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	testLauncherCacheMutex.Lock()
	previous := helpers.GlobalLauncherCache
	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice(launchers)
	helpers.GlobalLauncherCache = cache
	t.Cleanup(func() {
		helpers.GlobalLauncherCache = previous
		testLauncherCacheMutex.Unlock()
	})

	index := func() (int, error) {
		return NewNamesIndex(t.Context(), pl, cfg,
			[]systemdefs.System{{ID: systemdefs.SystemNES}}, db, func(IndexStatus) {}, nil)
	}
	present := func() []string {
		rows, rowsErr := db.MediaDB.GetMediaBySystemID(systemdefs.SystemNES)
		require.NoError(t, rowsErr)
		var paths []string
		for _, row := range rows {
			if !row.IsMissing {
				paths = append(paths, row.Path)
			}
		}
		slices.Sort(paths)
		return paths
	}
	return sourceIndex{pl: pl, index: index, present: present, db: db}
}

// A source root is indexed like a root directory: the launcher's folder is
// found in it case-insensitively and walked with the same rules, and its
// files are stored as source:// paths.
func TestSourceRootIndexesLikeARootDirectory(t *testing.T) {
	fx := sourceIndexFixture(t)
	pl, index, present := fx.pl, fx.index, fx.present
	root := pl.roots[0]

	count, err := index()
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, []string{
		mustSourcePath(t, root, "nes", "Fixture + 50% (USA).nes"),
		mustSourcePath(t, root, "nes", "Sub", "Deep #2.nes"),
	}, present())
	for _, read := range pl.reads {
		assert.NotContains(t, read, "Other", "only launcher folders are walked")
	}
}

// A source file's name is decoded, never shown escaped.
func TestSourceRootMediaNamesAreDecoded(t *testing.T) {
	fx := sourceIndexFixture(t)
	index, db := fx.index, fx.db
	_, err := index()
	require.NoError(t, err)
	rows, err := db.MediaDB.GetMediaBySystemID(systemdefs.SystemNES)
	require.NoError(t, err)
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.SortName)
	}
	slices.Sort(names)
	assert.Equal(t, []string{"Deep #2", "Fixture + 50%"}, names)
}

// A root the host no longer grants is not listed, and its media goes missing
// as it would from an unmounted card.
func TestSourceRootNoLongerGrantedMarksMediaMissing(t *testing.T) {
	fx := sourceIndexFixture(t)
	pl, index, present := fx.pl, fx.index, fx.present
	_, err := index()
	require.NoError(t, err)
	require.Len(t, present(), 2)

	pl.roots = nil
	_, err = index()
	require.NoError(t, err)
	assert.Empty(t, present())
}

// A host that cannot list its roots at all fails the run: the host itself is
// unreachable, not just one root of it. A root that is listed but fails to
// read, whether at a system folder or at the root itself, does not fail the
// run: it marks the affected systems incomplete instead. Either way nothing
// is marked missing because the host did not answer.
func TestSourceRootFailuresNeverMarkMediaMissing(t *testing.T) {
	fx := sourceIndexFixture(t)
	pl, index, present := fx.pl, fx.index, fx.present
	_, err := index()
	require.NoError(t, err)
	indexed := present()
	require.Len(t, indexed, 2)

	unavailable := errors.New("host unavailable")
	pl.rootsErr = unavailable
	_, err = index()
	require.ErrorIs(t, err, unavailable)
	assert.Equal(t, indexed, present())

	pl.rootsErr = nil
	pl.readErr[mustSourcePath(t, pl.roots[0], "nes", "Sub")] = unavailable
	_, err = index()
	require.NoError(t, err)
	assert.Equal(t, indexed, present(), "an unreadable folder keeps the system's media")

	pl.readErr = map[string]error{pl.roots[0]: unavailable}
	_, err = index()
	require.NoError(t, err, "a root that fails to read is skipped, not an aborted run")
	assert.Equal(t, indexed, present(), "the unreadable root's system keeps its media")
}

// A cancelled context aborts discovery entirely: it is the caller giving up,
// not a flaky root, so it must not be swallowed as a per-root failure.
func TestGetSourceSystemPathsAbortsEntirelyOnContextCancellation(t *testing.T) {
	t.Parallel()
	root := platforms.SourceRootPath("granted-tree")
	launchers := []platforms.Launcher{{
		ID: "NESCore", SystemID: systemdefs.SystemNES,
		Folders: []string{"NES"}, Extensions: []string{".nes"},
	}}
	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice(launchers)
	pl := &sourceTestPlatform{roots: []string{root}, readErr: map[string]error{}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := getSourceSystemPaths(ctx, pl, pl.roots, []systemdefs.System{{ID: systemdefs.SystemNES}}, cache)
	require.ErrorIs(t, err, context.Canceled,
		"a cancelled context must abort discovery entirely, not just mark one root failed")
}

// Multiple source roots are independent: one failing to read does not stop
// discovery in the others, and does not mark its own previously-indexed
// media missing.
func TestSourceRootFailureDoesNotAffectOtherRoots(t *testing.T) {
	fx := sourceIndexFixture(t)
	pl, index, present := fx.pl, fx.index, fx.present
	firstRoot := pl.roots[0]
	_, err := index()
	require.NoError(t, err)
	firstIndexed := present()
	require.Len(t, firstIndexed, 2)

	secondRoot := platforms.SourceRootPath("second-granted-tree")
	secondNes := mustSourcePath(t, secondRoot, "nes")
	secondFile := mustSourcePath(t, secondRoot, "nes", "Second Fixture (USA).nes")
	pl.tree[secondRoot] = []platforms.SourceEntry{folder("nes")}
	pl.tree[secondNes] = []platforms.SourceEntry{file("Second Fixture (USA).nes")}
	pl.roots = []string{firstRoot, secondRoot}
	pl.readErr = map[string]error{firstRoot: errors.New("host unavailable")}

	_, err = index()
	require.NoError(t, err)
	combined := present()
	assert.Contains(t, combined, secondFile, "the second root still indexes while the first fails")
	for _, path := range firstIndexed {
		assert.Contains(t, combined, path, "the failed root's previously-indexed media is not marked missing")
	}
}

// A source root has real nested folders, unlike every other virtual scheme,
// and browsing must see them: the root's top level lists "nes" as a
// directory, not a flat dump of every file under it; browsing into "nes"
// lists its one real subdirectory ("Sub") plus the file sitting directly in
// it, not the file inside Sub; browsing into Sub lists only its own file.
// Checked against both the uncached (live media scan) and cache-populated
// paths, since a rebuilt cache must agree with the fallback it replaces.
func TestSourceRootDirectoriesBrowseHierarchically(t *testing.T) {
	fx := sourceIndexFixture(t)
	pl, index := fx.pl, fx.index
	root := pl.roots[0]
	_, err := index()
	require.NoError(t, err)

	rootPrefix := mustSourcePath(t, root) + "/"
	nesPrefix := mustSourcePath(t, root, "nes") + "/"
	subPrefix := mustSourcePath(t, root, "nes", "Sub") + "/"

	check := func(t *testing.T, label string) {
		t.Helper()
		ctx := t.Context()

		rootDirs, err := fx.db.MediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{PathPrefix: rootPrefix})
		require.NoError(t, err, label)
		rootNames := make([]string, 0, len(rootDirs))
		for _, d := range rootDirs {
			rootNames = append(rootNames, d.Name)
		}
		assert.Equal(t, []string{"nes"}, rootNames, "%s: the root's only real child is the nes folder", label)

		nesDirs, err := fx.db.MediaDB.BrowseDirectories(ctx, database.BrowseDirectoriesOptions{PathPrefix: nesPrefix})
		require.NoError(t, err, label)
		nesDirNames := make([]string, 0, len(nesDirs))
		for _, d := range nesDirs {
			nesDirNames = append(nesDirNames, d.Name)
		}
		assert.Equal(t, []string{"Sub"}, nesDirNames, "%s: nes's only real subdirectory is Sub", label)

		nesFiles, err := fx.db.MediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{PathPrefix: nesPrefix, Limit: 50})
		require.NoError(t, err, label)
		nesFileNames := make([]string, 0, len(nesFiles))
		for _, f := range nesFiles {
			nesFileNames = append(nesFileNames, f.Path)
		}
		assert.Equal(t, []string{mustSourcePath(t, root, "nes", "Fixture + 50% (USA).nes")}, nesFileNames,
			"%s: nes's direct files exclude Sub's content", label)

		subFiles, err := fx.db.MediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{PathPrefix: subPrefix, Limit: 50})
		require.NoError(t, err, label)
		subFileNames := make([]string, 0, len(subFiles))
		for _, f := range subFiles {
			subFileNames = append(subFileNames, f.Path)
		}
		assert.Equal(t, []string{mustSourcePath(t, root, "nes", "Sub", "Deep #2.nes")}, subFileNames, "%s", label)
	}

	check(t, "uncached fallback")
	require.NoError(t, fx.db.MediaDB.PopulateBrowseCache(t.Context()))
	check(t, "rebuilt cache")
}
