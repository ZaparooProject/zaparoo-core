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
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type libraryStub struct {
	database.MediaDBI
	groups map[int64][]int64
	rows   []database.MediaFullRow
}

func (db *libraryStub) FindMediaIDsByPaths(_ context.Context, paths []string) ([]database.MediaPathID, error) {
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
	for i := range db.rows {
		row := &db.rows[i]
		if row.DBID == id {
			return row, nil
		}
	}
	return nil, errors.New("media row not in fixture")
}

func (db *libraryStub) GetVersionGroupMembers(_ context.Context, _ []int64) (map[int64][]int64, error) {
	return db.groups, nil
}

func (db *libraryStub) GetTitlesByDBIDs(_ context.Context, ids []int64) ([]database.TitleWithSystem, error) {
	var titles []database.TitleWithSystem
	for _, id := range ids {
		for i := range db.rows {
			row := &db.rows[i]
			if row.Title.DBID == id {
				titles = append(titles, database.TitleWithSystem{
					DBID: id, Slug: row.Title.Slug, SystemID: row.System.SystemID,
				})
				break
			}
		}
	}
	return titles, nil
}

func arcadeRow(path, slug string, mediaID, titleID int64) database.MediaFullRow {
	return database.MediaFullRow{
		System: database.System{SystemID: "Arcade"},
		Media:  database.Media{Path: path, DBID: mediaID},
		Title:  database.MediaTitle{Slug: slug, DBID: titleID},
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
	db := &libraryStub{
		rows: []database.MediaFullRow{original, alternate},
		groups: map[int64][]int64{
			10: {10, 11},
			11: {10, 11},
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
