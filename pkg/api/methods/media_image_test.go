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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/webp"
)

func makeMediaImageEnv(
	t *testing.T, mockMediaDB *testhelpers.MockMediaDBI, params json.RawMessage,
) requests.RequestEnv {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)
	drainNotifications(t, ns)

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	return requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
		Config:   cfg,
		Params:   params,
	}
}

func makeMediaFullRow(mediaDBID, titleDBID int64) *database.MediaFullRow {
	return &database.MediaFullRow{
		Media:  database.Media{DBID: mediaDBID, Path: filepath.Join("games", fmt.Sprintf("test-%d.rom", mediaDBID))},
		Title:  database.MediaTitle{DBID: titleDBID, Slug: "test-game", Name: "Test Game"},
		System: database.System{DBID: 100, SystemID: "NES", Name: "NES"},
	}
}

func expectMediaImageResolve(mockDB *testhelpers.MockMediaDBI, row *database.MediaFullRow) {
	mockDB.On("FindSystemBySystemID", row.System.SystemID).Return(row.System, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, row.System.DBID, row.Path).
		Return(&row.Media, nil)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, row.DBID).Return(row, nil)
}

func mediaImageParams(row *database.MediaFullRow, extra string) json.RawMessage {
	if extra != "" {
		extra = ", " + extra
	}
	return json.RawMessage(fmt.Sprintf(`{"system": %q, "path": %q%s}`, row.System.SystemID, row.Path, extra))
}

func TestMediaThumbCache_GetSetAndWipe(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cache := &mediaThumbCache{
		fs:            fs,
		dir:           filepath.Join("cache", "thumbs", "current"),
		resolvedTypes: make(map[string]string),
	}
	mediaID := int64(1)
	ref := mediaRefParam{MediaID: &mediaID}

	_, _, found := cache.get(ref, "property:image-boxart", 100)
	assert.False(t, found)

	cache.set(ref, "property:image-boxart", 100, []byte("png-data"), "image/png")
	data, contentType, found := cache.get(ref, "property:image-boxart", 100)
	require.True(t, found)
	assert.Equal(t, []byte("png-data"), data)
	assert.Equal(t, "image/png", contentType)

	cache.wipe()
	_, _, found = cache.get(ref, "property:image-boxart", 100)
	assert.False(t, found)
}

func TestMediaThumbCache_SkipsUnsupportedContentType(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cache := &mediaThumbCache{
		fs:            fs,
		dir:           filepath.Join("cache", "thumbs", "current"),
		resolvedTypes: make(map[string]string),
	}
	mediaID := int64(1)
	ref := mediaRefParam{MediaID: &mediaID}

	cache.set(ref, "property:image-boxart", 100, []byte("not an image"), "text/plain")
	_, _, found := cache.get(ref, "property:image-boxart", 100)
	assert.False(t, found)
}

func TestWipeMediaThumbCache_EmptiesLiveDirInPlace(t *testing.T) {
	fs := afero.NewMemMapFs()
	thumbs := filepath.Join("cache", "thumbs")
	dir := filepath.Join(thumbs, mediaThumbCacheVersionDir())
	cache := &mediaThumbCache{fs: fs, dir: dir, resolvedTypes: make(map[string]string)}
	mediaThumbCachePointer.Store(cache)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })

	mediaID := int64(1)
	ref := mediaRefParam{MediaID: &mediaID}
	cache.set(ref, "property:image-boxart", 512, []byte("webp-bytes"), "image/webp")
	cache.setResolvedTypeTag(ref, nil, 512, "property:image-boxart")
	_, _, found := cache.get(ref, "property:image-boxart", 512)
	require.True(t, found)

	WipeMediaThumbCache()

	// The live dir keeps its deterministic name (stable across restarts) rather
	// than rolling to a new generation directory.
	live := mediaThumbCachePointer.Load()
	require.NotNil(t, live)
	assert.Equal(t, dir, live.dir)

	// Cached contents and the resolved-type memo are cleared.
	_, _, found = cache.get(ref, "property:image-boxart", 512)
	assert.False(t, found)
	_, memoOK := cache.getResolvedTypeTag(ref, nil, 512)
	assert.False(t, memoOK)

	// No moved-aside stale directory is left behind (removal is synchronous).
	entries, err := afero.ReadDir(fs, thumbs)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{mediaThumbCacheVersionDir()}, names)
}

func TestReapStaleVersions_RemovesOtherVersionsAndLegacyDirs(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	thumbs := filepath.Join("cache", "thumbs")
	// Leftovers from older builds and prior versions, plus a crash-abandoned
	// moved-aside wipe directory.
	require.NoError(t, fs.MkdirAll(filepath.Join(thumbs, "current"), 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(thumbs, "current", "old.png"), []byte("x"), 0o600))
	require.NoError(t, fs.MkdirAll(filepath.Join(thumbs, "gen-3"), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Join(thumbs, "v0"), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Join(thumbs, mediaThumbCacheVersionDir()+".stale7"), 0o750))

	// The current-version dir already has content that must be preserved — a
	// version bump invalidates other versions, but restarting on the same
	// version must keep the warm cache, not cold-wipe it every boot.
	live := filepath.Join(thumbs, mediaThumbCacheVersionDir())
	require.NoError(t, fs.MkdirAll(live, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(live, "keep.webp"), []byte("y"), 0o600))

	cache := &mediaThumbCache{fs: fs, dir: live, resolvedTypes: make(map[string]string)}
	cache.reapStaleVersions()

	entries, err := afero.ReadDir(fs, thumbs)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{mediaThumbCacheVersionDir()}, names, "only the current version dir should remain")

	kept, err := afero.Exists(fs, filepath.Join(live, "keep.webp"))
	require.NoError(t, err)
	assert.True(t, kept, "current-version contents must be preserved across restart")
}

func TestSnapThumbMaxSize(t *testing.T) {
	t.Parallel()

	// Non-positive means "full size" — returned unchanged so no resize happens.
	assert.Equal(t, int32(0), snapThumbMaxSize(0))
	assert.Equal(t, int32(-5), snapThumbMaxSize(-5))
	// A positive request snaps up to the smallest tier that is >= the request.
	assert.Equal(t, int32(128), snapThumbMaxSize(1))
	assert.Equal(t, int32(128), snapThumbMaxSize(128))
	assert.Equal(t, int32(256), snapThumbMaxSize(129))
	assert.Equal(t, int32(512), snapThumbMaxSize(257))
	assert.Equal(t, int32(512), snapThumbMaxSize(512))
	assert.Equal(t, int32(768), snapThumbMaxSize(513))
	// Oversized requests are capped to the largest tier.
	assert.Equal(t, int32(768), snapThumbMaxSize(9999))
}

func TestResizeImageIfNeeded_TransparentImageOutputsWebPWithAlpha(t *testing.T) {
	t.Parallel()

	// A larger transparent source so the lossy encoder has real content to work
	// with and the output is unambiguously WebP after the downscale.
	const dim = 64
	src := image.NewRGBA(image.Rect(0, 0, dim, dim))
	for y := range dim {
		for x := range dim {
			// Opaque red on the right half, transparent green on the left half.
			if x < dim/2 {
				src.Set(x, y, color.RGBA{G: 255, A: 32})
			} else {
				src.Set(x, y, color.RGBA{R: 255, A: 255})
			}
		}
	}

	var in bytes.Buffer
	require.NoError(t, png.Encode(&in, src))

	resized, contentType := resizeImageIfNeeded(in.Bytes(), "image/png", 16)
	require.Equal(t, "image/webp", contentType)
	assert.Less(t, len(resized), in.Len(), "resized webp should be smaller than the source png")

	decoded, err := webp.Decode(bytes.NewReader(resized))
	require.NoError(t, err)
	assert.Equal(t, 16, decoded.Bounds().Dx())
	assert.Equal(t, 16, decoded.Bounds().Dy())
	assert.True(t, imageHasTransparency(decoded), "alpha must survive the lossy webp encode")
}

func TestResizeImageIfNeeded_OpaqueImageOutputsWebP(t *testing.T) {
	t.Parallel()

	const dim = 64
	src := image.NewRGBA(image.Rect(0, 0, dim, dim))
	for y := range dim {
		for x := range dim {
			src.Set(x, y, color.RGBA{R: 255, G: 128, B: 64, A: 255})
		}
	}

	var in bytes.Buffer
	require.NoError(t, png.Encode(&in, src))

	resized, contentType := resizeImageIfNeeded(in.Bytes(), "image/png", 16)
	require.Equal(t, "image/webp", contentType)

	decoded, err := webp.Decode(bytes.NewReader(resized))
	require.NoError(t, err)
	assert.Equal(t, 16, decoded.Bounds().Dx())
	assert.Equal(t, 16, decoded.Bounds().Dy())
}

func TestResizeImageIfNeeded_DecodesWebPSource(t *testing.T) {
	t.Parallel()

	// A webp source (an accepted artwork format) must be resizable, not passed
	// through untouched.
	const dim = 64
	src := image.NewRGBA(image.Rect(0, 0, dim, dim))
	for y := range dim {
		for x := range dim {
			src.Set(x, y, color.RGBA{R: 10, G: 200, B: 90, A: 255})
		}
	}
	webpBytes, _, err := encodeResizedImage(src)
	require.NoError(t, err)

	resized, contentType := resizeImageIfNeeded(webpBytes, "image/webp", 16)
	require.Equal(t, "image/webp", contentType)
	decoded, err := webp.Decode(bytes.NewReader(resized))
	require.NoError(t, err)
	assert.Equal(t, 16, decoded.Bounds().Dx())
}

func TestResizeImageIfNeeded_FitsTierReencodesToWebP(t *testing.T) {
	t.Parallel()

	// Source already fits the requested tier (no downscale), but must still be
	// re-encoded to WebP at native dimensions so a request snapped to a tier at
	// or above native size gets the format win. High-frequency content that PNG
	// cannot compress but lossy WebP can guarantees the WebP comes out smaller.
	const dim = 64
	src := image.NewRGBA(image.Rect(0, 0, dim, dim))
	for y := range dim {
		for x := range dim {
			src.Set(x, y, color.RGBA{
				R: uint8((x*53 ^ y*97) & 0xff),
				G: uint8((x*131 + y*29) & 0xff),
				B: uint8((x*17 ^ y*191) & 0xff),
				A: 255,
			})
		}
	}
	var in bytes.Buffer
	require.NoError(t, png.Encode(&in, src))

	resized, contentType := resizeImageIfNeeded(in.Bytes(), "image/png", 128)
	require.Equal(t, "image/webp", contentType)
	assert.Less(t, len(resized), in.Len(), "lossy webp should beat the high-entropy png")
	decoded, err := webp.Decode(bytes.NewReader(resized))
	require.NoError(t, err)
	assert.Equal(t, dim, decoded.Bounds().Dx(), "native dimensions preserved when no downscale")
	assert.Equal(t, dim, decoded.Bounds().Dy())
}

func TestResizeImageIfNeeded_KeepsOriginalWhenWebPNotSmaller(t *testing.T) {
	t.Parallel()

	// A smooth gradient compresses to a smaller PNG than lossy WebP can manage.
	// With no downscale needed, the original bytes must be kept rather than
	// inflated, and the no-inflation invariant must hold either way.
	const dim = 64
	src := image.NewRGBA(image.Rect(0, 0, dim, dim))
	for y := range dim {
		for x := range dim {
			src.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: uint8(x + y), A: 255})
		}
	}
	var in bytes.Buffer
	require.NoError(t, png.Encode(&in, src))

	resized, contentType := resizeImageIfNeeded(in.Bytes(), "image/png", 128)
	assert.LessOrEqual(t, len(resized), in.Len(), "result must never inflate when no downscale happens")
	assert.Equal(t, "image/png", contentType, "original kept when webp would not shrink it")
	assert.Equal(t, in.Bytes(), resized)
}

func TestResizeImageIfNeeded_PassesThroughUnchanged(t *testing.T) {
	t.Parallel()

	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 8, 8))))

	tests := []struct {
		name        string
		contentType string
		binary      []byte
		maxSize     int
	}{
		// Full size requested: nothing is resized or re-encoded.
		{name: "non-positive maxSize", contentType: "image/png", binary: pngBytes.Bytes(), maxSize: 0},
		{name: "negative maxSize", contentType: "image/png", binary: pngBytes.Bytes(), maxSize: -10},
		// Empty payload short-circuits.
		{name: "empty binary", contentType: "image/png", binary: nil, maxSize: 128},
		// Undecodable source is served as-is rather than dropped.
		{name: "undecodable source", contentType: "image/png", binary: []byte("not an image"), maxSize: 128},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resized, contentType := resizeImageIfNeeded(tt.binary, tt.contentType, tt.maxSize)
			assert.Equal(t, tt.contentType, contentType)
			assert.Equal(t, tt.binary, resized)
		})
	}
}

func TestDecodeResizableImage(t *testing.T) {
	t.Parallel()

	const dim = 8
	src := image.NewRGBA(image.Rect(0, 0, dim, dim))
	for y := range dim {
		for x := range dim {
			src.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, src))
	var jpegBytes bytes.Buffer
	require.NoError(t, jpeg.Encode(&jpegBytes, src, nil))
	webpBytes, _, err := encodeResizedImage(src)
	require.NoError(t, err)

	tests := []struct {
		name        string
		contentType string
		binary      []byte
		wantErr     bool
	}{
		// Declared content type drives the decoder directly.
		{name: "png by content type", contentType: "image/png", binary: pngBytes.Bytes()},
		{name: "jpeg by content type", contentType: "image/jpeg", binary: jpegBytes.Bytes()},
		{name: "webp by content type", contentType: "image/webp", binary: webpBytes},
		// Content type missing or wrong: the magic-byte sniff must still decode,
		// since stored sources do not always carry an accurate type.
		{name: "png sniffed when type empty", contentType: "", binary: pngBytes.Bytes()},
		{name: "jpeg sniffed when type wrong", contentType: "application/octet-stream", binary: jpegBytes.Bytes()},
		{name: "webp sniffed when type empty", contentType: "", binary: webpBytes},
		// Undecodable input must error, not panic — it is untrusted.
		{name: "unsupported bytes error", contentType: "text/plain", binary: []byte("not an image"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			img, decErr := decodeResizableImage(tt.binary, tt.contentType)
			if tt.wantErr {
				require.Error(t, decErr)
				assert.Nil(t, img)
				return
			}
			require.NoError(t, decErr)
			require.NotNil(t, img)
			assert.Equal(t, dim, img.Bounds().Dx())
			assert.Equal(t, dim, img.Bounds().Dy())
		})
	}
}

// noOpaqueImage is a minimal image.Image that deliberately implements neither a
// special-cased opaque type nor an Opaque() helper, forcing imageHasTransparency
// down its per-pixel fallback scan.
type noOpaqueImage struct{ inner *image.RGBA }

func (n noOpaqueImage) ColorModel() color.Model { return n.inner.ColorModel() }
func (n noOpaqueImage) Bounds() image.Rectangle { return n.inner.Bounds() }
func (n noOpaqueImage) At(x, y int) color.Color { return n.inner.At(x, y) }

func TestImageHasTransparency(t *testing.T) {
	t.Parallel()

	const dim = 4
	opaqueRGBA := image.NewRGBA(image.Rect(0, 0, dim, dim))
	transparentRGBA := image.NewRGBA(image.Rect(0, 0, dim, dim))
	for y := range dim {
		for x := range dim {
			opaqueRGBA.Set(x, y, color.RGBA{R: 255, A: 255})
			transparentRGBA.Set(x, y, color.RGBA{R: 255, A: 128})
		}
	}

	tests := []struct {
		img  image.Image
		name string
		want bool
	}{
		// JPEG decodes to YCbCr; the short-circuit must report opaque without a
		// pixel scan (the ARM hot path for every JPEG cover).
		{name: "ycbcr is opaque", img: image.NewYCbCr(image.Rect(0, 0, dim, dim), image.YCbCrSubsampleRatio444)},
		{name: "gray is opaque", img: image.NewGray(image.Rect(0, 0, dim, dim))},
		// Opaque() helper branch, both outcomes.
		{name: "opaque rgba", img: opaqueRGBA},
		{name: "transparent rgba", img: transparentRGBA, want: true},
		// Per-pixel fallback for a type without an Opaque() helper.
		{name: "fallback scan opaque", img: noOpaqueImage{inner: opaqueRGBA}},
		{name: "fallback scan transparent", img: noOpaqueImage{inner: transparentRGBA}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, imageHasTransparency(tt.img))
		})
	}
}

// TestHandleMediaImage_DefaultPrefs_TitleBlobFound verifies that when no imageTypes
// param is given, the handler uses the default preference order and returns the
// first matching title-level property with inline binary data.
func TestHandleMediaImage_DefaultPrefs_TitleBlobFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobData := []byte("fake-png-bytes")

	row := makeMediaFullRow(1, 10)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(1)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(10)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: blobData},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, ""))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.NotNil(t, resp.Extension)
	assert.Equal(t, "png", *resp.Extension)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString(blobData), resp.Data)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_DefaultPrefs_PathBackedCompanionArtwork(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string][]byte{
		"boxart.png":     []byte("boxart-2d"),
		"boxart3d.png":   []byte("boxart-3d"),
		"screenshot.png": []byte("screenshot"),
		"wheel.png":      []byte("wheel"),
		"titleshot.png":  []byte("titleshot"),
	}
	for name, data := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(11, 110)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(11)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(110)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-screenshot", Text: filepath.Join(dir, "screenshot.png")},
			{TypeTag: "property:image-titleshot", Text: filepath.Join(dir, "titleshot.png")},
			{TypeTag: "property:image-boxart", Text: filepath.Join(dir, "boxart.png")},
			{TypeTag: "property:image-boxart3d", Text: filepath.Join(dir, "boxart3d.png")},
			{TypeTag: "property:image-wheel", Text: filepath.Join(dir, "wheel.png")},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, ""))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.NotNil(t, resp.Extension)
	assert.Equal(t, "png", *resp.Extension)
	assert.Equal(t, base64.StdEncoding.EncodeToString(files["boxart.png"]), resp.Data)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_DefaultPrefs_TitleThumbnailFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	thumbnailData := []byte("thumbnail-png-bytes")

	row := makeMediaFullRow(13, 130)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(13)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(130)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-thumbnail", ContentType: "image/png", Binary: thumbnailData},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, ""))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-thumbnail", resp.TypeTag)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.Equal(t, base64.StdEncoding.EncodeToString(thumbnailData), resp.Data)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_ExplicitThumbnailType(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	thumbnailData := []byte("thumbnail-png-bytes")

	row := makeMediaFullRow(14, 140)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(14)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(140)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-thumbnail", ContentType: "image/png", Binary: thumbnailData},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["thumbnail"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-thumbnail", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString(thumbnailData), resp.Data)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_DefaultPrefs_FallsBackToNextCompanionArtwork(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	boxart3D := []byte("boxart-3d")
	screenshot := []byte("screenshot")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "boxart3d.png"), boxart3D, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "screenshot.png"), screenshot, 0o600))

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(12, 120)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(12)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(120)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-screenshot", Text: filepath.Join(dir, "screenshot.png")},
			{TypeTag: "property:image-boxart3d", Text: filepath.Join(dir, "boxart3d.png")},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, ""))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-boxart3d", resp.TypeTag)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.NotNil(t, resp.Extension)
	assert.Equal(t, "png", *resp.Extension)
	assert.Equal(t, base64.StdEncoding.EncodeToString(boxart3D), resp.Data)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_ExplicitPrefs_MediaLevelPriority verifies that media-level
// properties take priority over title-level properties for the same TypeTag.
func TestHandleMediaImage_ExplicitPrefs_MediaLevelPriority(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mediaBlob := []byte("media-level-screenshot")
	titleBlob := []byte("title-level-screenshot")

	row := makeMediaFullRow(2, 20)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(2)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-screenshot", ContentType: "image/jpeg", Binary: mediaBlob},
		}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(20)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-screenshot", ContentType: "image/jpeg", Binary: titleBlob},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["screenshot"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t,
		base64.StdEncoding.EncodeToString(mediaBlob), resp.Data, "expected media-level blob, not title-level")
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_BinaryNil_LoadsFromFile verifies that when a matching
// property has no inline binary, the handler reads the file at the Text path.
func TestHandleMediaImage_BinaryNil_LoadsFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "boxart.png")
	fileContents := []byte("real-png-data")
	require.NoError(t, os.WriteFile(imgPath, fileContents, 0o600))

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(3, 30)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(3)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(30)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Text: imgPath, Binary: nil},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	decoded, decErr := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, decErr)
	assert.Equal(t, fileContents, decoded)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_NoMatchFound verifies that an error is returned when no
// image property matches the preference list.
func TestHandleMediaImage_FilePathInfersContentType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "boxart.png")
	fileContents := []byte("real-png-data")
	require.NoError(t, os.WriteFile(imgPath, fileContents, 0o600))

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(33, 330)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(33)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(330)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", Text: imgPath, Binary: nil},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.NotNil(t, resp.Extension)
	assert.Equal(t, "png", *resp.Extension)
	decoded, decErr := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, decErr)
	assert.Equal(t, fileContents, decoded)
	mockDB.AssertExpectations(t)
}

func TestMediaImagePropSources_IncludesSingletonAliasSources(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Once()

	row := &database.MediaFullRow{
		Media: database.Media{
			DBID:      20,
			Path:      filepath.ToSlash(filepath.Join("roms", "Game.zip", "Game.nes")),
			ParentDir: filepath.ToSlash(filepath.Join("roms", "Game.zip")) + "/",
		},
		Title:  database.MediaTitle{DBID: 200},
		System: database.System{DBID: 1, SystemID: "NES"},
	}
	parentPath := filepath.ToSlash(filepath.Join("roms", "Game.zip"))
	parent := &database.Media{DBID: 10, Path: parentPath}

	mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, row.System.DBID, parentPath).
		Return(&row.Media, nil).Once()
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, row.System.DBID, parentPath).
		Return(parent, nil).Once()
	mockDB.On("GetMediaPropertiesByMediaDBIDs", mock.Anything, []int64{20, 10}).
		Return(map[int64][]database.MediaProperty{
			20: {{TypeTag: "property:image-boxart", Text: "child.png"}},
			10: {{TypeTag: "property:image-boxart", Text: "parent.png"}},
		}, nil).Once()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
		Platform: platform,
	}
	sources, err := mediaImagePropSources(env, row)
	require.NoError(t, err)

	require.Len(t, sources, 2)
	assert.Equal(t, "child.png", sources[0][0].Text)
	assert.Equal(t, "parent.png", sources[1][0].Text)
	mockDB.AssertExpectations(t)
	platform.AssertExpectations(t)
}

func TestHandleMediaImage_NoMatchFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(4, 40)
	row.Path = string(filepath.Separator) + filepath.Join("games", "missing.rom")
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(4)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(40)).
		Return([]database.MediaProperty{}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, ""))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	assert.Contains(t, err.Error(), `system="NES"`)
	assert.Contains(t, err.Error(), `path="/games/missing.rom"`)
	assert.NotContains(t, err.Error(), "NES//games")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_NoImageCacheSkipsPropertyFetch(t *testing.T) {
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(404, 4040)

	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{row.DBID}).
		Return(map[int64]database.MediaFullRow{row.DBID: *row}, nil).Once()
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).
		Return([]database.MediaProperty{}, nil).Once()
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).
		Return([]database.MediaProperty{}, nil).Once()

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId":404,"imageTypes":["boxart"]}`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)
	var firstNoImage *mediaImageNotFoundError
	require.ErrorAs(t, err, &firstNoImage)

	_, err = HandleMediaImage(env)
	require.Error(t, err)
	var secondNoImage *mediaImageNotFoundError
	require.ErrorAs(t, err, &secondNoImage)

	mediaImageNoImages.clear()
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).
		Return([]database.MediaProperty{}, nil).Once()
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).
		Return([]database.MediaProperty{}, nil).Once()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{row.DBID}).
		Return(map[int64]database.MediaFullRow{row.DBID: *row}, nil).Once()
	_, err = HandleMediaImage(env)
	require.Error(t, err)
	var thirdNoImage *mediaImageNotFoundError
	require.ErrorAs(t, err, &thirdNoImage)

	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_MissingFileBackedImageIsNotCached(t *testing.T) {
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(408, 4080)
	imagePath := filepath.Join(t.TempDir(), "boxart.png")
	params := json.RawMessage(`{"mediaId":408,"imageTypes":["boxart"]}`)

	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{row.DBID}).
		Return(map[int64]database.MediaFullRow{row.DBID: *row}, nil).Once()
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).
		Return([]database.MediaProperty{{TypeTag: "property:image-boxart", Text: imagePath}}, nil).Once()
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).
		Return([]database.MediaProperty{}, nil).Once()

	env := makeMediaImageEnv(t, mockDB, params)
	_, err := HandleMediaImage(env)
	require.Error(t, err)
	var noImage *mediaImageNotFoundError
	require.ErrorAs(t, err, &noImage)

	require.NoError(t, os.WriteFile(imagePath, []byte("boxart"), 0o600))
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{row.DBID}).
		Return(map[int64]database.MediaFullRow{row.DBID: *row}, nil).Once()
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).
		Return([]database.MediaProperty{{TypeTag: "property:image-boxart", Text: imagePath}}, nil).Once()
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).
		Return([]database.MediaProperty{}, nil).Once()

	result, err := HandleMediaImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("boxart")), resp.Data)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_NoImagePathCacheSkipsMediaDB(t *testing.T) {
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	row := makeMediaFullRow(405, 4050)
	params := mediaImageParams(row, `"imageTypes": ["boxart"]`)
	ref, err := parseMediaImageRequest(params)
	require.NoError(t, err)
	prefs := imagePrefs(nil, ref.ImageTypes)
	mediaImageNoImages.add(
		mediaImageNoImageRequestKey(ref, prefs),
		&mediaImageNotFoundError{system: row.System.SystemID, path: row.Path},
	)

	env := makeMediaImageEnv(t, testhelpers.NewMockMediaDBI(), params)
	_, err = HandleMediaImage(env)
	require.Error(t, err)

	var quietErr *models.QuietClientError
	require.ErrorAs(t, err, &quietErr)
	var noImage *mediaImageNotFoundError
	require.ErrorAs(t, err, &noImage)
}

func TestHandleMediaImage_NoImageCacheBypassesSemaphore(t *testing.T) {
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	mediaImageSem <- struct{}{}
	defer func() { <-mediaImageSem }()

	params := json.RawMessage(`{"mediaId":406,"imageTypes":["boxart"]}`)
	ref, err := parseMediaImageRequest(params)
	require.NoError(t, err)
	prefs := imagePrefs(nil, ref.ImageTypes)
	mediaImageNoImages.add(
		mediaImageNoImageRequestKey(ref, prefs),
		&mediaImageNotFoundError{system: "NES", path: filepath.Join("games", "test-406.rom")},
	)

	env := makeMediaImageEnv(t, testhelpers.NewMockMediaDBI(), params)
	_, err = HandleMediaImage(env)
	require.Error(t, err)

	var quietErr *models.QuietClientError
	require.ErrorAs(t, err, &quietErr)
}

func TestHandleMediaImage_NoImageCacheRecheckedAfterSemaphore(t *testing.T) {
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	mediaImageSem <- struct{}{}

	params := json.RawMessage(`{"mediaId":407,"imageTypes":["boxart"]}`)
	ref, err := parseMediaImageRequest(params)
	require.NoError(t, err)
	prefs := imagePrefs(nil, ref.ImageTypes)
	noImageKey := mediaImageNoImageRequestKey(ref, prefs)

	reachedSem := make(chan struct{})
	mediaImageBeforeSemAcquire = func() { close(reachedSem) }
	t.Cleanup(func() { mediaImageBeforeSemAcquire = nil })

	done := make(chan error, 1)
	env := makeMediaImageEnv(t, testhelpers.NewMockMediaDBI(), params)
	go func() {
		_, handleErr := HandleMediaImage(env)
		done <- handleErr
	}()

	select {
	case <-reachedSem:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for media image handler to reach semaphore")
	}
	mediaImageNoImages.add(
		noImageKey,
		&mediaImageNotFoundError{system: "NES", path: filepath.Join("games", "test-407.rom")},
	)
	<-mediaImageSem

	select {
	case err = <-done:
		require.Error(t, err)
		var quietErr *models.QuietClientError
		require.ErrorAs(t, err, &quietErr)
		var noImage *mediaImageNotFoundError
		require.ErrorAs(t, err, &noImage)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for media image handler")
	}
}

func TestHandleMediaImage_ContextCanceledWhileWaitingForSemaphore(t *testing.T) {
	mediaImageSem <- struct{}{}
	defer func() {
		<-mediaImageSem
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := makeMediaImageEnv(t, testhelpers.NewMockMediaDBI(), json.RawMessage(`{"mediaId":1}`))
	env.Context = ctx

	_, err := HandleMediaImage(env)
	require.ErrorIs(t, err, context.Canceled)
}

// TestHandleMediaImage_MediaNotFound verifies that an error is returned when no
// media record exists for the given system and path.
func TestHandleMediaImage_MediaNotFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	system := database.System{DBID: 100, SystemID: "NES", Name: "NES"}
	mediaPath := filepath.ToSlash(filepath.Join("games", "missing.rom"))
	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, mediaPath).
		Return((*database.Media)(nil), nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"system":"NES","path":"games/missing.rom"}`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_MediaIDSuccess(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobData := []byte("media-id-image")
	row := makeMediaFullRow(44, 440)

	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{row.DBID}).
		Return(map[int64]database.MediaFullRow{row.DBID: *row}, nil).Once()
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: blobData},
		}, nil).Once()
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).
		Return([]database.MediaProperty{}, nil).Once()

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId":44,"imageTypes":["boxart"]}`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString(blobData), resp.Data)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_ItemsParamReturnsClientError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	env := makeMediaImageEnv(t, mockDB, json.RawMessage(
		`{"items":[{"system":"NES","path":"games/missing.rom"}]}`,
	))
	_, err := HandleMediaImage(env)
	require.Error(t, err)
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	assert.Contains(t, err.Error(), `unknown field "items"`)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_ImageTypeResolvesToImageImage verifies that "image" in the
// imageTypes list resolves to "property:image-image" (no assumed context).
func TestHandleMediaImage_ImageTypeResolvesToImageImage(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobData := []byte("generic-image-bytes")

	row := makeMediaFullRow(5, 50)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(5)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(50)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-image", ContentType: "image/png", Binary: blobData},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["image"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-image", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString(blobData), resp.Data)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_ItemsWithMediaIDReturnsClientError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{
		"imageTypes": ["boxart"],
		"items": [{"mediaId": 5}]
	}`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	assert.Contains(t, err.Error(), `unknown field "items"`)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_FileReadError_IgnoresStaleProperty verifies that when a
// property's Text path is unreadable, the stale property is ignored in-memory
// without mutating the DB. With no remaining image the final result is a ClientError.
func TestHandleMediaImage_FileReadError_IgnoresStaleProperty(t *testing.T) {
	t.Parallel()

	// Use a path inside a real temp dir so the dir exists, but the file does not.
	stalePath := filepath.Join(t.TempDir(), "missing_boxart.png")

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(6, 60)
	expectMediaImageResolve(mockDB, row)

	// Stale title-level property with missing file.
	staleProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Text:        stalePath,
		Binary:      nil,
		TypeTagDBID: 42,
	}
	// Properties are fetched once; the iterative loop removes stale entries from the in-memory map only.
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(60)).
		Return([]database.MediaProperty{staleProp}, nil)

	mockDB.On("GetMediaProperties", mock.Anything, int64(6)).
		Return([]database.MediaProperty{}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart"]`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr, "expected ClientError after exhausting all preferences")
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_StaleMedia_FallsBackToTitle verifies that when a
// media-level property has a stale file path, the handler ignores the stale
// entry in-memory and falls back to the title-level property for the same TypeTag
// before moving on to the next preference in the list.
func TestHandleMediaImage_StaleMedia_FallsBackToTitle(t *testing.T) {
	t.Parallel()

	stalePath := filepath.Join(t.TempDir(), "missing_boxart.png")
	// File is intentionally not created — it must be missing.

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(8, 80)

	expectMediaImageResolve(mockDB, row)

	staleMediaProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Text:        stalePath,
		Binary:      nil,
		TypeTagDBID: 77,
	}
	titleBlob := []byte("title-boxart-bytes")
	titleProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Binary:      titleBlob,
	}

	mockDB.On("GetMediaProperties", mock.Anything, int64(8)).
		Return([]database.MediaProperty{staleMediaProp}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(80)).
		Return([]database.MediaProperty{titleProp}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString(titleBlob), resp.Data)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_FileReadError_FallsBackToNextPref verifies that when a
// title-level property's file is unreadable, the stale entry is ignored in-memory,
// and the handler continues to find the next-preference image successfully without
// a second round-trip or DB mutation.
func TestHandleMediaImage_FileReadError_FallsBackToNextPref(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stalePath := filepath.Join(dir, "missing_boxart.png")
	screenshotPath := filepath.Join(dir, "screenshot.png")
	screenshotData := []byte("screenshot-bytes")
	require.NoError(t, os.WriteFile(screenshotPath, screenshotData, 0o600))

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(7, 70)

	expectMediaImageResolve(mockDB, row)

	staleProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Text:        stalePath,
		Binary:      nil,
		TypeTagDBID: 55,
	}
	screenshotProp := database.MediaProperty{
		TypeTag:     "property:image-screenshot",
		ContentType: "image/png",
		Text:        screenshotPath,
		Binary:      nil,
		TypeTagDBID: 56,
	}

	// Properties are fetched once; the iterative loop handles both in a single pass.
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(70)).
		Return([]database.MediaProperty{staleProp, screenshotProp}, nil)

	mockDB.On("GetMediaProperties", mock.Anything, int64(7)).
		Return([]database.MediaProperty{}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart", "screenshot"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-screenshot", resp.TypeTag)
	decoded, decErr := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, decErr)
	assert.Equal(t, screenshotData, decoded)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_OversizedFileReturnsClientError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	largePath := filepath.Join(dir, "large_boxart.png")
	file, err := os.Create(largePath) // #nosec G304 -- test path is created under t.TempDir().
	require.NoError(t, err)
	require.NoError(t, file.Truncate(database.MaxMediaPropertyBinaryBytes+1))
	require.NoError(t, file.Close())

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(9, 90)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(9)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(90)).
		Return([]database.MediaProperty{{
			TypeTag:     "property:image-boxart",
			ContentType: "image/png",
			Text:        largePath,
			TypeTagDBID: 88,
		}}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart"]`))
	_, err = HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	assert.Contains(t, err.Error(), "image file too large")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaImage_OversizedBlobReturnsClientError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(10, 100)
	blobID := int64(123)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(10)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(100)).
		Return([]database.MediaProperty{{
			BlobDBID:    &blobID,
			TypeTag:     "property:image-boxart",
			ContentType: "image/png",
			BlobSize:    database.MaxMediaPropertyBinaryBytes + 1,
		}}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart"]`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	assert.Contains(t, err.Error(), "image blob too large")
	mockDB.AssertExpectations(t)
}
