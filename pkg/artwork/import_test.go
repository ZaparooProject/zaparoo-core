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
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type libraryStub struct {
	database.MediaDBI
	findErr   error
	rowErr    error
	groupErr  error
	titlesErr error
	groups    map[int64][]int64
	rows      []database.MediaFullRow
	lookups   int
}

func (db *libraryStub) FindMediaIDsByPaths(_ context.Context, paths []string) ([]database.MediaPathID, error) {
	db.lookups++
	if db.findErr != nil {
		return nil, db.findErr
	}
	var matches []database.MediaPathID
	for i := range db.rows {
		row := &db.rows[i]
		if row.Path == paths[0] {
			matches = append(matches, database.MediaPathID{DBID: row.DBID})
		}
	}
	return matches, nil
}

func (db *libraryStub) GetMediaWithTitleAndSystem(_ context.Context, id int64) (*database.MediaFullRow, error) {
	if db.rowErr != nil {
		return nil, db.rowErr
	}
	for i := range db.rows {
		row := &db.rows[i]
		if row.DBID == id {
			return row, nil
		}
	}
	return nil, nil //nolint:nilnil // matches MediaDB, which returns no row for an unknown ID
}

func (db *libraryStub) GetVersionGroupMembers(_ context.Context, _ []int64) (map[int64][]int64, error) {
	if db.groupErr != nil {
		return nil, db.groupErr
	}
	return db.groups, nil
}

func (db *libraryStub) GetTitlesByDBIDs(_ context.Context, ids []int64) ([]database.TitleWithSystem, error) {
	if db.titlesErr != nil {
		return nil, db.titlesErr
	}
	var titles []database.TitleWithSystem
	for _, id := range ids {
		for i := range db.rows {
			row := &db.rows[i]
			if row.Title.DBID == id {
				// Production leaves SystemID empty and fills SystemDBID.
				titles = append(titles, database.TitleWithSystem{
					DBID: id, Slug: row.Title.Slug, SystemDBID: row.Title.SystemDBID,
				})
				break
			}
		}
	}
	return titles, nil
}

func arcadeRow(path, slug string, mediaID, titleID int64) database.MediaFullRow {
	return systemRow("Arcade", 1, path, slug, mediaID, titleID)
}

func systemRow(system string, systemDBID int64, path, slug string, mediaID, titleID int64) database.MediaFullRow {
	return database.MediaFullRow{
		System: database.System{SystemID: system, DBID: systemDBID},
		Media:  database.Media{Path: path, DBID: mediaID},
		Title:  database.MediaTitle{Slug: slug, DBID: titleID, SystemDBID: systemDBID},
	}
}

func makePNG(t *testing.T, pixel color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, pixel)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func stage(t *testing.T, dataDir, name, path, kind, scope string, pngData []byte) string {
	t.Helper()
	dir := filepath.Join(dataDir, "artwork-inbox", name)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	// This is the manifest written by Main_MiSTer's OSD.
	request := fmt.Sprintf("{\"version\":1,\"gamePath\":%q,\"corePath\":\"core.rbf\",\"imageType\":%q,\"scope\":%q}\n",
		path, kind, scope)
	require.NoError(t, os.WriteFile(filepath.Join(dir, requestName), []byte(request), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, captureName), pngData, 0o600))
	return dir
}

func TestImportPendingScopes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := t.Context()
	original := arcadeRow("/games/example.mra", "example", 1, 10)
	alternate := arcadeRow("/games/example-alt.mra", "example-alt", 2, 11)
	// A same-group title on another system must not join the Arcade group.
	foreign := systemRow("NES", 2, "/games/example.nes", "a-example", 3, 12)
	db := &libraryStub{
		rows: []database.MediaFullRow{original, alternate, foreign},
		groups: map[int64][]int64{
			10: {10, 11, 12},
			11: {10, 11, 12},
		},
	}
	gamePNG := makePNG(t, color.NRGBA{R: 255, A: 255})
	titlePNG := makePNG(t, color.NRGBA{B: 255, A: 255})
	gameBundle := stage(t, dir, "capture-game01", original.Path, "screenshot", scopeVersion, gamePNG)
	titleBundle := stage(t, dir, "capture-title1", alternate.Path, "screenshot", scopeTitle, titlePNG)
	partial := stage(t, dir, ".capture-temp01", original.Path, "wallpaper", scopeTitle, gamePNG)

	var changed []string
	onImport := func(system string) { changed = append(changed, system) }
	require.NoError(t, ImportPending(ctx, db, filepath.Join(dir, InboxDir), dir, 1<<20, onImport))
	assert.Equal(t, []string{"Arcade", "Arcade"}, changed)
	assert.NoDirExists(t, gameBundle)
	assert.NoDirExists(t, titleBundle)
	assert.DirExists(t, partial)

	version, title, err := Properties(ctx, db, dir, &original)
	require.NoError(t, err)
	require.Len(t, version, 1)
	require.Len(t, title, 1)
	assert.Equal(t, "property:image-screenshot", version[0].TypeTag)
	got, err := os.ReadFile(version[0].Text)
	require.NoError(t, err)
	assert.Equal(t, gamePNG, got)
	got, err = os.ReadFile(title[0].Text)
	require.NoError(t, err)
	assert.Equal(t, titlePNG, got)

	// Keep the assignments after an index rebuild renumbers the media and titles.
	original.DBID, original.Title.DBID = 100, 110
	alternate.DBID, alternate.Title.DBID = 200, 210
	db.rows = []database.MediaFullRow{original, alternate}
	db.groups = map[int64][]int64{110: {110, 210}, 210: {110, 210}}
	version, title, err = Properties(ctx, db, dir, &alternate)
	require.NoError(t, err)
	assert.Empty(t, version)
	require.Len(t, title, 1)
	got, err = os.ReadFile(title[0].Text)
	require.NoError(t, err)
	assert.Equal(t, titlePNG, got)
}

func TestImportPendingRetriesAndRejects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	game := arcadeRow("/games/example.mra", "example", 1, 10)
	db := &libraryStub{rows: []database.MediaFullRow{game}}
	invalid := stage(t, dir, "capture-bad001", game.Path, "screenshot", scopeVersion, []byte("broken PNG"))
	pending := stage(t, dir, "capture-new001", "/games/new.mra", "boxart", scopeVersion, makePNG(t, color.NRGBA{}))
	wrongKind := stage(t, dir, "capture-bad002", game.Path, "../boxart", scopeVersion, makePNG(t, color.NRGBA{}))

	calls := 0
	changed := func(_ string) { calls++ }
	require.NoError(t, ImportPending(t.Context(), db, filepath.Join(dir, InboxDir), dir, 1<<20, changed))
	assert.NoDirExists(t, invalid)
	assert.NoDirExists(t, wrongKind)
	assert.DirExists(t, filepath.Join(dir, "artwork-inbox", ".rejected-capture-bad001"))
	assert.DirExists(t, filepath.Join(dir, "artwork-inbox", ".rejected-capture-bad002"))
	assert.DirExists(t, pending)
	assert.Zero(t, calls)

	db.rows = append(db.rows, arcadeRow("/games/new.mra", "new", 2, 20))
	require.NoError(t, ImportPending(t.Context(), db, filepath.Join(dir, InboxDir), dir, 1<<20, changed))
	assert.NoDirExists(t, pending)
	assert.Equal(t, 1, calls)
}

// An unmatched capture is looked up once per completed index, not on every
// pass, and is dropped once it outlives MaxCaptureAge.
func TestImporterWaitsForIndexAndExpires(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inbox := filepath.Join(dir, InboxDir)
	db := &libraryStub{}
	calls := 0
	now := time.Now()
	importer := &Importer{
		DB: db, Inbox: inbox, DataDir: dir, MaxBytes: 1 << 20,
		Changed: func(string) { calls++ },
		Now:     func() time.Time { return now },
	}
	pngData := makePNG(t, color.NRGBA{A: 255})
	waiting := stage(t, dir, "capture-wait01", "/games/later.mra", "screenshot", scopeVersion, pngData)

	require.NoError(t, importer.Run(t.Context(), 0))
	require.NoError(t, importer.Run(t.Context(), 0))
	assert.Equal(t, 1, db.lookups, "no lookup again until an index completes")
	assert.DirExists(t, waiting)

	db.rows = []database.MediaFullRow{arcadeRow("/games/later.mra", "later", 1, 10)}
	require.NoError(t, importer.Run(t.Context(), 1))
	assert.Equal(t, 2, db.lookups)
	assert.Equal(t, 1, calls)
	assert.NoDirExists(t, waiting)

	never := stage(t, dir, "capture-never1", "/games/gone.mra", "screenshot", scopeVersion, pngData)
	rejected := stage(t, dir, ".rejected-capture-bad001", "/games/gone.mra", "screenshot", scopeVersion, pngData)
	leftover := stage(t, dir, ".imported-capture-old001", "/games/gone.mra", "screenshot", scopeVersion, pngData)
	require.NoError(t, importer.Run(t.Context(), 1))
	assert.DirExists(t, never)
	assert.DirExists(t, rejected, "a recent rejection is kept for diagnosis")
	assert.NoDirExists(t, leftover, "a failed cleanup is retried")

	now = now.Add(MaxCaptureAge + time.Hour)
	require.NoError(t, importer.Run(t.Context(), 1))
	assert.NoDirExists(t, never, "an expired capture is dropped without waiting for an index")
	assert.NoDirExists(t, rejected)
	assert.Empty(t, importer.waiting)
}

func FuzzParseRequest(f *testing.F) {
	f.Add([]byte("{\"version\":1,\"gamePath\":\"example.mra\",\"imageType\":\"screenshot\",\"scope\":\"title\"}"))
	f.Add([]byte("{\"version\":1,\"imageType\":\"../../test\",\"scope\":\"../\"}"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxRequestBytes {
			return
		}
		request, err := parseRequest(data)
		if err == nil {
			assert.Equal(t, 1, request.Version)
			assert.NotEmpty(t, request.GamePath)
			assert.NotEmpty(t, typeTag(request.ImageType))
			assert.Contains(t, []string{scopeVersion, scopeTitle}, request.Scope)
		}
	})
}

func TestHasOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := t.Context()
	versioned := arcadeRow("/games/versioned.mra", "versioned", 1, 10)
	original := arcadeRow("/games/example.mra", "example", 2, 20)
	alternate := arcadeRow("/games/example-alt.mra", "example-alt", 3, 21)
	plain := arcadeRow("/games/plain.mra", "plain", 4, 30)
	db := &libraryStub{
		rows:   []database.MediaFullRow{versioned, original, alternate, plain},
		groups: map[int64][]int64{20: {20, 21}, 21: {20, 21}},
	}

	has, err := HasOverride(ctx, db, dir, "Arcade", plain.Path, plain.DBID)
	require.NoError(t, err)
	assert.False(t, has, "a system without imports has no overrides")

	pngData := makePNG(t, color.NRGBA{G: 255, A: 255})
	stage(t, dir, "capture-vers01", versioned.Path, "screenshot", scopeVersion, pngData)
	require.NoError(t, ImportPending(ctx, db, filepath.Join(dir, InboxDir), dir, 1<<20, func(string) {}))
	has, err = HasOverride(ctx, db, dir, "Arcade", versioned.Path, versioned.DBID)
	require.NoError(t, err)
	assert.True(t, has, "the captured version")
	has, err = HasOverride(ctx, db, dir, "Arcade", plain.Path, plain.DBID)
	require.NoError(t, err)
	assert.False(t, has, "another game in the same system")

	stage(t, dir, "capture-titl01", alternate.Path, "screenshot", scopeTitle, pngData)
	require.NoError(t, ImportPending(ctx, db, filepath.Join(dir, InboxDir), dir, 1<<20, func(string) {}))
	for _, row := range []database.MediaFullRow{original, alternate} {
		has, err = HasOverride(ctx, db, dir, "Arcade", row.Path, row.DBID)
		require.NoError(t, err)
		assert.True(t, has, "every member of the captured title group: %s", row.Path)
	}
	has, err = HasOverride(ctx, db, dir, "Arcade", plain.Path, plain.DBID)
	require.NoError(t, err)
	assert.False(t, has, "a game outside the captured title group")
	has, err = HasOverride(ctx, db, dir, "NES", versioned.Path, versioned.DBID)
	require.NoError(t, err)
	assert.False(t, has, "the same path in another system")
}
