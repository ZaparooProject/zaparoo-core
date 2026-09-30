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

package methods

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const coverTestBoxart = "property:image-boxart"

func newMemThumbCache() *mediaThumbCache {
	return &mediaThumbCache{
		fs: afero.NewMemMapFs(),
		dir: filepath.Join(
			string(filepath.Separator), "cache", mediaThumbCacheDirName, mediaThumbCacheVersionDir(),
		),
		resolvedTypes: make(map[string]resolvedThumb),
	}
}

func installThumbCache(t *testing.T, cache *mediaThumbCache) {
	t.Helper()
	mediaThumbCachePointer.Store(cache)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })
}

func solidPNG(t *testing.T, dim int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, dim, dim))
	for y := range dim {
		for x := range dim {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// A restarted Core has an empty in-memory memo, but the recorded cover type
// leads straight to the disk thumbnail: the original is never loaded and the
// lookup semaphore is never taken.
func TestHandleMediaImage_ColdRestartServesRecordedThumbWithoutOriginal(t *testing.T) {
	// Not parallel: installs the process-wide thumb cache pointer and hook.
	mediaImageNoImages.clear()
	row := makeMediaFullRow(501, 5010)
	cache := newMemThumbCache()
	installThumbCache(t, cache)
	ref := mediaRefParam{System: row.System.SystemID, Path: row.Path}
	setMediaThumbCacheForTest(t, cache, ref, row.System.SystemID, coverTestBoxart, 256, []byte("thumb"), "image/webp")

	semTouched := false
	mediaImageBeforeSemAcquire = func() { semTouched = true }
	t.Cleanup(func() { mediaImageBeforeSemAcquire = nil })

	// No property, blob or row expectations: any original-image load panics.
	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("FindSystemBySystemID", row.System.SystemID).Return(row.System, nil).Once()
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, row.System.DBID, row.Path).Return(&row.Media, nil).Once()
	mockDB.On("GetMediaCoverThumb", mock.Anything, row.DBID).Return(database.MediaCoverThumb{
		MediaDBID:         row.DBID,
		SystemID:          row.System.SystemID,
		Path:              row.Path,
		TypeTag:           coverTestBoxart,
		AvailableTypeTags: []string{coverTestBoxart, "property:image-screenshot"},
	}, true, nil).Once()

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"maxSize": 200, "imageTypes": ["boxart", "screenshot"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, coverTestBoxart, resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("thumb")), resp.Data)
	assert.False(t, semTouched, "the recorded path must not queue for the lookup semaphore")
	mockDB.AssertExpectations(t)

	// The hit also warms the memo, so the next request needs no database.
	strictDB := testhelpers.NewMockMediaDBI()
	params := mediaImageParams(row, `"maxSize": 200, "imageTypes": ["boxart", "screenshot"]`)
	env2 := makeMediaImageEnv(t, strictDB, params)
	_, err = HandleMediaImage(env2)
	require.NoError(t, err)
	strictDB.AssertExpectations(t)
}

func TestHandleMediaImage_ColdRestartByMediaID(t *testing.T) {
	mediaImageNoImages.clear()
	row := makeMediaFullRow(502, 5020)
	cache := newMemThumbCache()
	installThumbCache(t, cache)
	id := row.DBID
	ref := mediaRefParam{MediaID: &id}
	setMediaThumbCacheForTest(t, cache, ref, row.System.SystemID, coverTestBoxart, 128, []byte("by-id"), "image/webp")

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaCoverThumb", mock.Anything, row.DBID).Return(database.MediaCoverThumb{
		MediaDBID: row.DBID, SystemID: row.System.SystemID, Path: row.Path,
		TypeTag: coverTestBoxart, AvailableTypeTags: []string{coverTestBoxart},
	}, true, nil).Once()

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId":502,"maxSize":100}`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("by-id")), resp.Data)
	mockDB.AssertExpectations(t)
}

// A record resolved under other preferences is not trusted when a type the
// request prefers is available: the full lookup decides.
func TestHandleMediaImage_RecordedTypeConflictingWithPrefsTakesFullPath(t *testing.T) {
	mediaImageNoImages.clear()
	row := makeMediaFullRow(503, 5030)
	cache := newMemThumbCache()
	installThumbCache(t, cache)
	ref := mediaRefParam{System: row.System.SystemID, Path: row.Path}
	setMediaThumbCacheForTest(
		t, cache, ref, row.System.SystemID, coverTestBoxart, 256, []byte("boxart-thumb"), "image/webp",
	)

	mockDB := testhelpers.NewMockMediaDBI()
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaCoverThumb", mock.Anything, row.DBID).Return(database.MediaCoverThumb{
		MediaDBID: row.DBID, SystemID: row.System.SystemID, Path: row.Path, TypeTag: coverTestBoxart,
		AvailableTypeTags: []string{coverTestBoxart, "property:image-screenshot"},
	}, true, nil).Once()
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).Return([]database.MediaProperty{
		{TypeTag: "property:image-screenshot", ContentType: "image/png", Binary: []byte("shot")},
	}, nil).Once()
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).Return([]database.MediaProperty{}, nil).Once()

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"maxSize": 256, "imageTypes": ["screenshot", "boxart"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-screenshot", resp.TypeTag)
	mockDB.AssertExpectations(t)
}

func TestRecordedTypeMatchesPrefs(t *testing.T) {
	t.Parallel()
	shot := "property:image-screenshot"
	imageTag := "property:image-image"
	tests := []struct {
		name      string
		typeTag   string
		available []string
		prefs     []string
		want      bool
	}{
		{"first preference", coverTestBoxart, []string{coverTestBoxart, shot}, []string{"boxart", "screenshot"}, true},
		{"earlier preference absent", shot, []string{shot}, []string{"boxart", "screenshot"}, true},
		{
			"earlier preference available", coverTestBoxart,
			[]string{coverTestBoxart, imageTag},
			defaultImageTypes, false,
		},
		{"not requested", coverTestBoxart, []string{coverTestBoxart}, []string{"screenshot"}, false},
		{"property gone", coverTestBoxart, []string{shot}, []string{"boxart", "screenshot"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, recordedTypeMatchesPrefs(tt.typeTag, tt.available, tt.prefs))
		})
	}
}

// Building a thumbnail records its resolved type and the average colour of
// the downscaled frame.
func TestHandleMediaImage_RecordsCoverTypeAndColor(t *testing.T) {
	mediaImageNoImages.clear()
	row := makeMediaFullRow(504, 5040)
	installThumbCache(t, newMemThumbCache())
	blob := solidPNG(t, 300, color.RGBA{R: 0x20, G: 0x80, B: 0xc0, A: 0xff})

	mockDB := testhelpers.NewMockMediaDBI()
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaCoverThumb", mock.Anything, row.DBID).Return(database.MediaCoverThumb{}, false, nil).Once()
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).Return([]database.MediaProperty{}, nil).Once()
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).Return([]database.MediaProperty{
		{TypeTag: coverTestBoxart, ContentType: "image/png", Binary: blob},
	}, nil).Once()
	want := uint32(0x2080c0)
	mockDB.On("PutMediaCoverThumb", mock.Anything, row.DBID, coverTestBoxart, &want).Return(nil).Once()

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"maxSize": 256`))
	_, err := HandleMediaImage(env)
	require.NoError(t, err)
	mockDB.AssertExpectations(t)
}

// A single explicit type is a detail request, not the cover: it must not
// replace the cover record.
func TestHandleMediaImage_SingleTypeRequestDoesNotRecordCover(t *testing.T) {
	mediaImageNoImages.clear()
	row := makeMediaFullRow(505, 5050)
	installThumbCache(t, newMemThumbCache())

	mockDB := testhelpers.NewMockMediaDBI()
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).Return([]database.MediaProperty{
		{TypeTag: "property:image-screenshot", ContentType: "image/png", Binary: solidPNG(t, 8, color.RGBA{A: 0xff})},
	}, nil).Once()
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).Return([]database.MediaProperty{}, nil).Once()
	mockDB.On("PutMediaCoverThumb", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"maxSize": 64, "imageTypes": ["screenshot"]`))
	_, err := HandleMediaImage(env)
	require.NoError(t, err)
	mockDB.AssertNotCalled(t, "PutMediaCoverThumb", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// Up to the lookup semaphore's capacity, lookups run at the same time.
func TestHandleMediaImage_ConcurrentLookupsUpToCapacity(t *testing.T) {
	mediaImageNoImages.clear()
	require.Equal(t, 3, cap(mediaImageSem))
	n := cap(mediaImageSem)

	var arrived sync.WaitGroup
	arrived.Add(n)
	allInside := make(chan struct{})
	go func() {
		arrived.Wait()
		close(allInside)
	}()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, mock.Anything).
		Run(func(mock.Arguments) {
			arrived.Done()
			select {
			case <-allInside:
			case <-time.After(5 * time.Second):
			}
		}).
		Return(map[int64]database.MediaFullRow{}, nil)

	errs := make(chan error, n)
	start := time.Now()
	for i := range n {
		params := json.RawMessage(`{"mediaId":` + string(rune('1'+i)) + `}`)
		env := makeMediaImageEnv(t, mockDB, params)
		go func() {
			_, err := HandleMediaImage(env)
			errs <- err
		}()
	}
	select {
	case <-allInside:
	case <-time.After(3 * time.Second):
		t.Fatalf("only some of %d lookups entered the semaphore at once", n)
	}
	for range n {
		<-errs
	}
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestMediaThumbCache_ResizeConcurrencyFollowsPlatform(t *testing.T) {
	t.Parallel()
	for _, constrained := range []bool{false, true} {
		pl := mocks.NewMockPlatform()
		pl.On("Settings").Return(platforms.Settings{
			DataDir: t.TempDir(), HostManagedPaths: true, ResourceConstrained: constrained,
		})
		cache := newMediaThumbCacheWithFS(pl, afero.NewMemMapFs())
		want := mediaImageResizeConcurrency
		if constrained {
			want = mediaImageResizeConcurrencyConstrained
		}
		assert.Equal(t, want, cap(cache.resizeSemaphore()), "constrained=%v", constrained)
	}
	assert.Equal(t, mediaImageResizeConcurrency, cap((*mediaThumbCache)(nil).resizeSemaphore()))
}

// The background reap never deletes a temporary file a live request may still
// be renaming into place.
func TestReapStaleVersions_KeepsTemporaryFilesNewerThanCache(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	live := filepath.Join("cache", "thumbs", mediaThumbCacheVersionDir())
	systemDir := filepath.Join(live, thumbSystemDirName("SNES"))
	require.NoError(t, fs.MkdirAll(systemDir, 0o750))
	createdAt := time.Now()
	oldTmp := filepath.Join(systemDir, ".thumb-old.tmp")
	newTmp := filepath.Join(systemDir, ".thumb-new.tmp")
	require.NoError(t, afero.WriteFile(fs, oldTmp, []byte("old"), 0o600))
	require.NoError(t, fs.Chtimes(oldTmp, createdAt.Add(-time.Hour), createdAt.Add(-time.Hour)))
	require.NoError(t, afero.WriteFile(fs, newTmp, []byte("new"), 0o600))
	require.NoError(t, fs.Chtimes(newTmp, createdAt.Add(time.Second), createdAt.Add(time.Second)))

	cache := &mediaThumbCache{fs: fs, dir: live, resolvedTypes: make(map[string]resolvedThumb), createdAt: createdAt}
	cache.reapStaleVersions()

	oldExists, err := afero.Exists(fs, oldTmp)
	require.NoError(t, err)
	assert.False(t, oldExists)
	newExists, err := afero.Exists(fs, newTmp)
	require.NoError(t, err)
	assert.True(t, newExists)
}

func TestInitMediaThumbCache_UsableBeforeReapFinishes(t *testing.T) {
	// Not parallel: installs the process-wide thumb cache pointer.
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir(), HostManagedPaths: true})
	InitMediaThumbCache(pl)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })
	cache := mediaThumbCachePointer.Load()
	require.NotNil(t, cache)
	exists, err := afero.DirExists(cache.fs, cache.dir)
	require.NoError(t, err)
	assert.True(t, exists, "the live directory exists as soon as Init returns")
	select {
	case <-cache.reapDone:
	case <-time.After(5 * time.Second):
		t.Fatal("background reap did not finish")
	}
}

func TestAverageImageColor(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := range 10 {
		for x := range 10 {
			if x < 5 {
				img.SetRGBA(x, y, color.RGBA{R: 0xff, A: 0xff})
			} else {
				img.SetRGBA(x, y, color.RGBA{B: 0xff, A: 0xff})
			}
		}
	}
	got := averageImageColor(img)
	require.NotNil(t, got)
	assert.Equal(t, uint32(0x7f007f), *got)

	transparent := image.NewRGBA(image.Rect(0, 0, 4, 4))
	assert.Nil(t, averageImageColor(transparent))
	assert.Nil(t, averageImageColor(image.NewRGBA(image.Rect(0, 0, 0, 0))))
}

func TestAttachBrowseCoverColors(t *testing.T) {
	t.Parallel()
	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaCoverColors", mock.Anything, []int64{7, 8}).
		Return(map[int64]uint32{7: 0x0a0b0c}, nil).Once()
	env := makeMediaImageEnv(t, mockDB, nil)
	entries := []models.BrowseEntry{
		{Type: "directory", Name: "plain"},
		{Type: "media", MediaID: 7},
		{Type: "media", MediaID: 8},
	}
	attachBrowseCoverColors(&env, entries)
	assert.Empty(t, entries[0].CoverColor)
	assert.Equal(t, "#0a0b0c", entries[1].CoverColor)
	assert.Empty(t, entries[2].CoverColor, "unknown colours are omitted")
	mockDB.AssertExpectations(t)

	encoded, err := json.Marshal(entries[2])
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "coverColor")
	encoded, err = json.Marshal(entries[1])
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"coverColor":"#0a0b0c"`)
}

func TestMediaCoverColorsIgnoresLookupFailure(t *testing.T) {
	t.Parallel()
	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaCoverColors", mock.Anything, []int64{3}).Return(nil, assert.AnError).Once()
	assert.Nil(t, mediaCoverColors(t.Context(), mockDB, []int64{0, 3}))
	assert.Nil(t, mediaCoverColors(t.Context(), nil, []int64{3}))
	assert.Equal(t, "#000001", formatCoverColor(1))
}
