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

package artwork

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestImportRejectsInvalidBundles(t *testing.T) {
	t.Parallel()
	valid := makePNG(t, color.NRGBA{A: 255})
	// The header still decodes, so only the full decode notices the cut.
	truncated := valid[:len(valid)-20]
	oversized := encodePNG(t, image.NewGray(image.Rect(0, 0, 2000, 1000)))

	tests := []struct {
		stage func(t *testing.T, dir string)
		name  string
	}{
		{name: "missing manifest", stage: func(t *testing.T, dir string) {
			require.NoError(t, os.Remove(filepath.Join(dir, requestName)))
		}},
		{name: "oversized manifest", stage: func(t *testing.T, dir string) {
			big := strings.Repeat(" ", maxRequestBytes+1)
			require.NoError(t, os.WriteFile(filepath.Join(dir, requestName), []byte(big), 0o600))
		}},
		{name: "manifest is a directory", stage: func(t *testing.T, dir string) {
			require.NoError(t, os.Remove(filepath.Join(dir, requestName)))
			require.NoError(t, os.Mkdir(filepath.Join(dir, requestName), 0o750))
		}},
		{name: "malformed manifest", stage: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, requestName), []byte("{"), 0o600))
		}},
		{name: "missing capture", stage: func(t *testing.T, dir string) {
			require.NoError(t, os.Remove(filepath.Join(dir, captureName)))
		}},
		{name: "capture over the byte limit", stage: func(t *testing.T, dir string) {
			big := make([]byte, 64<<10+1)
			require.NoError(t, os.WriteFile(filepath.Join(dir, captureName), big, 0o600))
		}},
		{name: "truncated PNG", stage: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, captureName), truncated, 0o600))
		}},
		{name: "PNG larger than the scaler", stage: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, captureName), oversized, 0o600))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			game := arcadeRow("/games/example.mra", "example", 1, 10)
			db := &libraryStub{rows: []database.MediaFullRow{game}}
			bundle := stage(t, dir, "capture-bad001", game.Path, "screenshot", scopeVersion, valid)
			tt.stage(t, bundle)

			importer := &Importer{
				DB: db, Inbox: filepath.Join(dir, InboxDir), DataDir: dir, MaxBytes: 64 << 10,
				Changed: func(string) { t.Error("an invalid capture must not publish") },
			}
			require.NoError(t, importer.Run(t.Context(), 0))
			assert.NoDirExists(t, bundle)
			assert.DirExists(t, filepath.Join(dir, InboxDir, rejectedPrefix+"capture-bad001"))
		})
	}
}

func TestImportRejectsPathInTwoSystems(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db := &libraryStub{rows: []database.MediaFullRow{
		arcadeRow("/games/shared.zip", "shared", 1, 10),
		systemRow("NeoGeo", 2, "/games/shared.zip", "shared", 2, 20),
	}}
	bundle := stage(t, dir, "capture-dup001", "/games/shared.zip", "screenshot", scopeVersion,
		makePNG(t, color.NRGBA{A: 255}))
	require.NoError(t, ImportPending(t.Context(), db, filepath.Join(dir, InboxDir), dir, 1<<20, nil))
	assert.NoDirExists(t, bundle)
	assert.DirExists(t, filepath.Join(dir, InboxDir, rejectedPrefix+"capture-dup001"))
}

// Lookup failures are not the capture's fault, so it stays queued and is
// tried again on the next pass without waiting for another index.
func TestImportRetriesAfterLookupFailures(t *testing.T) {
	t.Parallel()
	failure := errors.New("database busy")
	tests := []struct {
		db    *libraryStub
		name  string
		scope string
	}{
		{name: "path lookup", scope: scopeVersion, db: &libraryStub{findErr: failure}},
		{name: "row lookup", scope: scopeVersion, db: &libraryStub{rowErr: failure}},
		{name: "cancelled lookup", scope: scopeVersion, db: &libraryStub{rowErr: context.Canceled}},
		{name: "version group", scope: scopeTitle, db: &libraryStub{groupErr: failure}},
		{
			name: "group titles", scope: scopeTitle,
			db: &libraryStub{titlesErr: failure, groups: map[int64][]int64{10: {10, 11}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			game := arcadeRow("/games/example.mra", "example", 1, 10)
			tt.db.rows = []database.MediaFullRow{game}
			bundle := stage(t, dir, "capture-try001", game.Path, "screenshot", tt.scope,
				makePNG(t, color.NRGBA{A: 255}))
			importer := &Importer{
				DB: tt.db, Inbox: filepath.Join(dir, InboxDir), DataDir: dir, MaxBytes: 1 << 20,
				Changed: func(string) { t.Error("a failed lookup must not publish") },
			}
			require.NoError(t, importer.Run(t.Context(), 0))
			require.NoError(t, importer.Run(t.Context(), 0))
			assert.Equal(t, 2, tt.db.lookups, "retried on the next pass")
			assert.DirExists(t, bundle)
			assert.Empty(t, importer.waiting)
		})
	}
}

func TestImporterInboxStates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inbox := filepath.Join(dir, InboxDir)
	db := &libraryStub{}
	importer := &Importer{DB: db, Inbox: inbox, DataDir: dir, MaxBytes: 1 << 20}
	require.NoError(t, importer.Run(t.Context(), 0), "no inbox until Main captures something")

	require.NoError(t, os.WriteFile(inbox, nil, 0o600))
	require.Error(t, importer.Run(t.Context(), 0))
	require.NoError(t, os.Remove(inbox))

	waiting := stage(t, dir, "capture-wait01", "/games/later.mra", "screenshot", scopeVersion,
		makePNG(t, color.NRGBA{A: 255}))
	require.NoError(t, os.WriteFile(filepath.Join(inbox, "capture-file01"), nil, 0o600))
	require.NoError(t, importer.Run(t.Context(), 0))
	assert.Contains(t, importer.waiting, "capture-wait01")
	assert.FileExists(t, filepath.Join(inbox, "capture-file01"), "only bundle directories are imported")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, importer.Run(ctx, 1), context.Canceled)
	assert.DirExists(t, waiting)

	// Forget a capture someone deleted by hand.
	require.NoError(t, os.RemoveAll(waiting))
	require.NoError(t, importer.Run(t.Context(), 1))
	assert.Empty(t, importer.waiting)
}

func TestIsOverridePathAndRevision(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	assert.True(t, IsOverridePath(dir, filepath.Join(dir, overridesDir, "system", "version", "key", "boxart.png")))
	assert.False(t, IsOverridePath(dir, filepath.Join(dir, overridesDir)))
	assert.False(t, IsOverridePath(dir, filepath.Join(dir, "media", "boxart.png")))

	before := Revision()
	require.NoError(t, saveOverride(filepath.Join(dir, overridesDir, "system"), "boxart", []byte("png")))
	assert.Greater(t, Revision(), before)
}

func TestOverrideLookupFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := t.Context()
	game := arcadeRow("/games/example.mra", "example", 1, 10)
	other := arcadeRow("/games/other.mra", "other", 2, 20)
	db := &libraryStub{rows: []database.MediaFullRow{game, other}}
	pngData := makePNG(t, color.NRGBA{A: 255})
	stage(t, dir, "capture-vers01", game.Path, "screenshot", scopeVersion, pngData)
	stage(t, dir, "capture-titl01", game.Path, "boxart", scopeTitle, pngData)
	require.NoError(t, ImportPending(ctx, db, filepath.Join(dir, InboxDir), dir, 1<<20, func(string) {}))

	// Files that are not imported artwork are ignored.
	versionDir := assignmentDir(dir, "Arcade", scopeVersion, game.Path)
	require.NoError(t, os.WriteFile(filepath.Join(versionDir, "notes.txt"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(versionDir, "wallpaper.png"), nil, 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(versionDir, "boxart.png"), 0o750))
	version, title, err := Properties(ctx, db, dir, &game)
	require.NoError(t, err)
	require.Len(t, version, 1)
	assert.Equal(t, "property:image-screenshot", version[0].TypeTag)
	require.Len(t, title, 1)
	assert.Equal(t, "property:image-boxart", title[0].TypeTag)

	nes := systemRow("NES", 2, "/games/example.nes", "example", 3, 30)
	version, title, err = Properties(ctx, db, dir, &nes)
	require.NoError(t, err)
	assert.Nil(t, version)
	assert.Nil(t, title)

	has, err := HasOverride(ctx, db, dir, "Arcade", "/games/unknown.mra", 99)
	require.NoError(t, err)
	assert.False(t, has, "a row that left the index")

	failure := errors.New("database busy")
	db.groupErr = failure
	version, title, err = Properties(ctx, db, dir, &game)
	require.ErrorIs(t, err, failure)
	assert.Len(t, version, 1, "exact artwork survives a group lookup failure")
	assert.Nil(t, title)
	_, err = HasOverride(ctx, db, dir, "Arcade", other.Path, other.DBID)
	require.ErrorIs(t, err, failure)

	db.rowErr = failure
	_, err = HasOverride(ctx, db, dir, "Arcade", other.Path, other.DBID)
	require.ErrorIs(t, err, failure)
}

func TestListAssignmentsUnreadable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	system := systemDir(dir, "Arcade")
	require.NoError(t, os.MkdirAll(system, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(system, scopeTitle), nil, 0o600))
	has, err := HasOverride(t.Context(), &libraryStub{}, dir, "Arcade", "/games/example.mra", 1)
	require.NoError(t, err)
	assert.False(t, has)
}
