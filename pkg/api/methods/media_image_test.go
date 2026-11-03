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
	byteorder "encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/wuffspng"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

func captureHandlerLogEvent(t *testing.T, message string, run func()) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	originalLogger := log.Logger
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	defer func() { log.Logger = originalLogger }()

	run()
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		var event map[string]any
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&event); err != nil {
			continue
		}
		if event["message"] == message {
			return event
		}
	}
	t.Fatalf("log event %q not found in %q", message, buf.String())
	return nil
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

// fillMediaImageSem takes every lookup slot so a handler blocks at the
// semaphore, and returns a function that frees them all.
func fillMediaImageSem() (release func()) {
	for range cap(mediaImageSem) {
		mediaImageSem <- struct{}{}
	}
	return func() {
		for range cap(mediaImageSem) {
			<-mediaImageSem
		}
	}
}

func setMediaThumbCacheForTest(
	t testing.TB,
	cache *mediaThumbCache,
	sourceKey string,
	maxSize int,
	data []byte,
	contentType string,
) string {
	t.Helper()
	path, err := cache.set(sourceKey, maxSize, data, contentType)
	require.NoError(t, err)
	return path
}

func newTestThumbCache(fs afero.Fs, dir string) *mediaThumbCache {
	return &mediaThumbCache{fs: fs, dir: dir, resolved: make(map[string]resolvedThumb)}
}

func TestMediaThumbCache_GetSet(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cache := newTestThumbCache(fs, filepath.Join("cache", "thumbs", mediaThumbCacheVersionDir()))
	key := "f:10:20:/media/boxart/mario.png"

	_, _, found := cache.get(key, 100)
	assert.False(t, found)

	path := setMediaThumbCacheForTest(t, cache, key, 100, []byte("png-data"), "image/png")
	data, contentType, found := cache.get(key, 100)
	require.True(t, found)
	assert.Equal(t, []byte("png-data"), data)
	assert.Equal(t, "image/png", contentType)

	entries, err := afero.ReadDir(fs, filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "atomic write must not leave a temporary file")

	_, _, found = cache.get(key, 128)
	assert.False(t, found, "each size tier is its own file")
	_, _, found = cache.get("f:10:21:/media/boxart/mario.png", 100)
	assert.False(t, found, "a changed source is a different file")
	_, _, found = cache.get("", 100)
	assert.False(t, found)
	_, err = cache.set("", 100, []byte("png-data"), "image/png")
	require.Error(t, err)
}

func TestMediaThumbCache_SkipsUnsupportedContentType(t *testing.T) {
	t.Parallel()

	cache := newTestThumbCache(afero.NewMemMapFs(), filepath.Join("cache", "thumbs", mediaThumbCacheVersionDir()))
	_, err := cache.set("d:abc", 100, []byte("not an image"), "text/plain")
	require.Error(t, err)
	_, _, found := cache.get("d:abc", 100)
	assert.False(t, found)
}

func TestMediaThumbCache_IsSafeLocalPath(t *testing.T) {
	t.Parallel()

	fs := afero.NewOsFs()
	cache := newTestThumbCache(fs, filepath.Join(t.TempDir(), mediaThumbCacheVersionDir()))
	require.NoError(t, fs.MkdirAll(cache.dir, 0o750))
	inside := filepath.Join(cache.dir, "inside.webp")
	require.NoError(t, afero.WriteFile(fs, inside, []byte("inside"), 0o600))
	assert.True(t, cache.isSafeLocalPath(inside))

	outside := filepath.Join(t.TempDir(), "outside.webp")
	require.NoError(t, afero.WriteFile(fs, outside, []byte("outside"), 0o600))
	assert.False(t, cache.isSafeLocalPath(outside))

	symlink := filepath.Join(cache.dir, "link.webp")
	if err := os.Symlink(outside, symlink); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ENOSYS) ||
			errors.Is(err, syscall.EOPNOTSUPP) {
			t.Skipf("symlinks unavailable: %v", err)
		}
		require.NoError(t, err)
	}
	assert.False(t, cache.isSafeLocalPath(symlink))

	relativeCache := &mediaThumbCache{fs: afero.NewMemMapFs(), dir: "relative"}
	assert.False(t, relativeCache.isSafeLocalPath(filepath.Join("relative", "image.webp")))
}

func TestMediaThumbCache_MemoIsBounded(t *testing.T) {
	t.Parallel()

	cache := newTestThumbCache(afero.NewMemMapFs(), "cache")
	for i := range mediaThumbMemoMax + 10 {
		id := int64(i)
		cache.setResolvedThumb(mediaRefParam{MediaID: &id}, nil, 256, resolvedThumb{system: "SNES"})
	}
	assert.Len(t, cache.resolved, mediaThumbMemoMax)
}

// seedThumbMemo stores one cached file and memo entry per system.
func seedThumbMemo(t *testing.T, cache *mediaThumbCache, systems ...string) map[string]mediaRefParam {
	t.Helper()
	refs := make(map[string]mediaRefParam, len(systems))
	for i, system := range systems {
		id := int64(i + 1)
		ref := mediaRefParam{MediaID: &id}
		key := "f:1:1:/" + system + ".png"
		path := setMediaThumbCacheForTest(t, cache, key, 256, []byte(system), "image/webp")
		cache.setResolvedThumb(ref, nil, 256, resolvedThumb{
			system: system, typeTag: "property:image-boxart", path: path, contentType: "image/webp",
		})
		refs[system] = ref
	}
	return refs
}

func TestInvalidateIndexedThumbnails_SelectiveSystems(t *testing.T) {
	cache := newTestThumbCache(afero.NewMemMapFs(), filepath.Join("cache", "thumbs", mediaThumbCacheVersionDir()))
	mediaThumbCachePointer.Store(cache)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })
	refs := seedThumbMemo(t, cache, "SNES", "Genesis")

	invalidateIndexedThumbnails([]systemdefs.System{{ID: "SNES"}}, false)

	_, memoOK := cache.getResolvedThumb(refs["SNES"], nil, 256)
	assert.False(t, memoOK, "the indexed system is resolved again")
	_, memoOK = cache.getResolvedThumb(refs["Genesis"], nil, 256)
	assert.True(t, memoOK, "other systems stay memoized")
	for _, system := range []string{"SNES", "Genesis"} {
		_, _, found := cache.get("f:1:1:/"+system+".png", 256)
		assert.True(t, found, "cached files are kept for %s", system)
	}
}

func TestInvalidateIndexedThumbnails_FullCache(t *testing.T) {
	tests := []struct {
		name    string
		systems []systemdefs.System
		rebuild bool
	}{
		{name: "rebuild", systems: []systemdefs.System{{ID: "SNES"}}, rebuild: true},
		{name: "empty systems"},
		{name: "all systems", systems: systemdefs.AllSystems()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newTestThumbCache(
				afero.NewMemMapFs(), filepath.Join("cache", "thumbs", mediaThumbCacheVersionDir()),
			)
			mediaThumbCachePointer.Store(cache)
			t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })
			refs := seedThumbMemo(t, cache, "SNES", "Genesis")

			invalidateIndexedThumbnails(tt.systems, tt.rebuild)

			for system, ref := range refs {
				_, memoOK := cache.getResolvedThumb(ref, nil, 256)
				assert.False(t, memoOK, "%s is resolved again", system)
				_, _, found := cache.get("f:1:1:/"+system+".png", 256)
				assert.True(t, found, "cached files are kept for %s", system)
			}
		})
	}
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
	require.NoError(t, fs.MkdirAll(filepath.Join(thumbs, "v1"), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Join(thumbs, "v2"), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Join(thumbs, "v2.stale7"), 0o750))

	// The current-version dir already has content that must be preserved — a
	// version bump invalidates other versions, but restarting on the same
	// version must keep the warm cache, not cold-wipe it every boot.
	live := filepath.Join(thumbs, mediaThumbCacheVersionDir())
	require.NoError(t, fs.MkdirAll(live, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(live, "keep.webp"), []byte("y"), 0o600))
	shardDir := filepath.Join(live, "ab")
	require.NoError(t, fs.MkdirAll(shardDir, 0o750))
	tmpPath := filepath.Join(shardDir, ".thumb-abandoned.tmp")
	require.NoError(t, afero.WriteFile(fs, tmpPath, []byte("partial"), 0o600))

	cache := newTestThumbCache(fs, live)
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
	tmpExists, err := afero.Exists(fs, tmpPath)
	require.NoError(t, err)
	assert.False(t, tmpExists, "abandoned atomic-write temporary files must be reaped")
}

// newReapTestCache returns a cache whose files are written with the fake
// clock's current time as their mtime.
func newReapTestCache(t *testing.T, maxBytes int64) (*mediaThumbCache, *clockwork.FakeClock) {
	t.Helper()
	clock := clockwork.NewFakeClockAt(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	cache := newTestThumbCache(afero.NewMemMapFs(), filepath.Join("cache", "thumbs", mediaThumbCacheVersionDir()))
	cache.clock = clock
	cache.maxBytes = maxBytes
	return cache, clock
}

func writeReapTestFile(t *testing.T, cache *mediaThumbCache, key string, size int, modTime time.Time) string {
	t.Helper()
	path := setMediaThumbCacheForTest(t, cache, key, 256, bytes.Repeat([]byte{1}, size), "image/webp")
	require.NoError(t, cache.fs.Chtimes(path, modTime, modTime))
	return path
}

func thumbFileExists(t *testing.T, cache *mediaThumbCache, path string) bool {
	t.Helper()
	exists, err := afero.Exists(cache.fs, path)
	require.NoError(t, err)
	return exists
}

func TestReapOrphans_KeepsEverythingUnderTheCap(t *testing.T) {
	t.Parallel()

	cache, clock := newReapTestCache(t, 1000)
	old := writeReapTestFile(t, cache, "d:old", 400, clock.Now().Add(-30*24*time.Hour))
	newer := writeReapTestFile(t, cache, "d:new", 400, clock.Now().Add(-48*time.Hour))

	cache.reapOrphans(context.Background())

	assert.True(t, thumbFileExists(t, cache, old))
	assert.True(t, thumbFileExists(t, cache, newer))
}

func TestReapOrphans_RemovesLeastRecentlyUsedPastTheCap(t *testing.T) {
	t.Parallel()

	cache, clock := newReapTestCache(t, 1000)
	now := clock.Now()
	oldest := writeReapTestFile(t, cache, "d:oldest", 300, now.Add(-10*24*time.Hour))
	older := writeReapTestFile(t, cache, "d:older", 300, now.Add(-5*24*time.Hour))
	old := writeReapTestFile(t, cache, "d:old", 300, now.Add(-2*24*time.Hour))
	recent := writeReapTestFile(t, cache, "d:recent", 300, now.Add(-time.Hour))

	cache.reapOrphans(context.Background())

	// 1200 bytes against a 1000-byte cap trims to at most 800: the two least
	// recently used files go, and nothing else is touched.
	assert.False(t, thumbFileExists(t, cache, oldest))
	assert.False(t, thumbFileExists(t, cache, older))
	assert.True(t, thumbFileExists(t, cache, old))
	assert.True(t, thumbFileExists(t, cache, recent))
}

func TestReapOrphans_NeverRemovesRecentlyUsedFiles(t *testing.T) {
	t.Parallel()

	cache, clock := newReapTestCache(t, 100)
	now := clock.Now()
	old := writeReapTestFile(t, cache, "d:old", 300, now.Add(-2*24*time.Hour))
	recent := writeReapTestFile(t, cache, "d:recent", 300, now.Add(-time.Hour))

	cache.reapOrphans(context.Background())

	assert.False(t, thumbFileExists(t, cache, old))
	assert.True(t, thumbFileExists(t, cache, recent), "a file used within a day may still be read by a client")
}

func TestReapOrphans_RemovesAbandonedTemporaryFiles(t *testing.T) {
	t.Parallel()

	cache, clock := newReapTestCache(t, 1<<20)
	now := clock.Now()
	shard := filepath.Join(cache.dir, "ab")
	require.NoError(t, cache.fs.MkdirAll(shard, 0o750))
	abandoned := filepath.Join(shard, ".thumb-abandoned.tmp")
	live := filepath.Join(shard, ".thumb-live.tmp")
	for path, modTime := range map[string]time.Time{
		abandoned: now.Add(-2 * time.Hour),
		live:      now.Add(-time.Minute),
	} {
		require.NoError(t, afero.WriteFile(cache.fs, path, []byte("partial"), 0o600))
		require.NoError(t, cache.fs.Chtimes(path, modTime, modTime))
	}

	cache.reapOrphans(context.Background())

	assert.False(t, thumbFileExists(t, cache, abandoned))
	assert.True(t, thumbFileExists(t, cache, live), "a write in progress keeps its temporary file")
}

func TestReapOrphans_StopsWhenCancelled(t *testing.T) {
	t.Parallel()

	cache, clock := newReapTestCache(t, 100)
	old := writeReapTestFile(t, cache, "d:old", 300, clock.Now().Add(-10*24*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cache.reapOrphans(ctx)

	assert.True(t, thumbFileExists(t, cache, old))
}

// A hit marks a file as used so the reaper keeps it, writing at most once a day.
func TestMediaThumbCache_HitRefreshesStaleModTime(t *testing.T) {
	t.Parallel()

	cache, clock := newReapTestCache(t, 1<<20)
	now := clock.Now()
	stale := writeReapTestFile(t, cache, "d:stale", 10, now.Add(-2*24*time.Hour))
	fresh := writeReapTestFile(t, cache, "d:fresh", 10, now.Add(-time.Hour))

	_, _, found := cache.lookupPath("d:stale", 256)
	require.True(t, found)
	_, _, found = cache.lookupPath("d:fresh", 256)
	require.True(t, found)

	info, err := cache.fs.Stat(stale)
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(now))
	info, err = cache.fs.Stat(fresh)
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(now.Add(-time.Hour)), "a recently used file is not rewritten")
}

func TestRequestReap_RunsAtMostOncePerInterval(t *testing.T) {
	t.Parallel()

	cache, clock := newReapTestCache(t, 100)
	first := writeReapTestFile(t, cache, "d:first", 300, clock.Now().Add(-10*24*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	cache.reapCtx = ctx
	defer cache.stopReaps(cancel)

	cache.requestReap(mediaThumbReapInterval)
	cache.reapWG.Wait()
	assert.False(t, thumbFileExists(t, cache, first))

	second := writeReapTestFile(t, cache, "d:second", 300, clock.Now().Add(-10*24*time.Hour))
	cache.requestReap(mediaThumbReapInterval)
	cache.reapWG.Wait()
	assert.True(t, thumbFileExists(t, cache, second), "a reap within the interval is skipped")

	clock.Advance(mediaThumbReapInterval)
	cache.requestReap(mediaThumbReapInterval)
	cache.reapWG.Wait()
	assert.False(t, thumbFileExists(t, cache, second))
}

func TestRequestReap_NoopWithoutStartedCache(t *testing.T) {
	t.Parallel()

	cache, clock := newReapTestCache(t, 100)
	old := writeReapTestFile(t, cache, "d:old", 300, clock.Now().Add(-10*24*time.Hour))
	cache.requestReap(mediaThumbReapInterval)
	cache.reapWG.Wait()
	assert.True(t, thumbFileExists(t, cache, old))

	ctx, cancel := context.WithCancel(context.Background())
	cache.reapCtx = ctx
	cache.stopReaps(cancel)
	cache.requestReap(mediaThumbReapInterval)
	cache.reapWG.Wait()
	assert.True(t, thumbFileExists(t, cache, old), "a stopped cache starts no reaps")
}

func TestSnapThumbMaxSize(t *testing.T) {
	t.Parallel()

	// Non-positive means "full size" — returned unchanged so no resize happens.
	assert.Equal(t, int32(0), snapThumbMaxSize(0))
	assert.Equal(t, int32(-5), snapThumbMaxSize(-5))
	// A positive request snaps up to the smallest tier that is >= the request.
	assert.Equal(t, int32(32), snapThumbMaxSize(1))
	assert.Equal(t, int32(32), snapThumbMaxSize(32))
	assert.Equal(t, int32(64), snapThumbMaxSize(33))
	assert.Equal(t, int32(64), snapThumbMaxSize(64))
	assert.Equal(t, int32(128), snapThumbMaxSize(65))
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

func TestResizeImageIfNeeded_OpaqueImageOutputsJPEG(t *testing.T) {
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
	require.Equal(t, "image/jpeg", contentType)

	decoded, err := jpeg.Decode(bytes.NewReader(resized))
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
	webpBytes, _, err := encodeWebPImage(src)
	require.NoError(t, err)

	resized, contentType := resizeImageIfNeeded(webpBytes, "image/webp", 16)
	require.Equal(t, "image/jpeg", contentType)
	decoded, err := jpeg.Decode(bytes.NewReader(resized))
	require.NoError(t, err)
	assert.Equal(t, 16, decoded.Bounds().Dx())
}

func TestResizeImageIfNeeded_FitsTierReencodes(t *testing.T) {
	t.Parallel()

	// Source already fits the requested tier (no downscale) but is too large to
	// pass through, so it must still be re-encoded at native dimensions: a
	// request snapped to a tier at or above native size gets the format win.
	// High-frequency content that PNG cannot compress but a lossy encode can
	// guarantees the re-encode comes out smaller.
	const dim = 250
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

	require.Greater(t, in.Len(), thumbPassthroughMaxBytes, "source must be past the passthrough limit")

	resized, contentType, avgColor := resizeImageWithColor(in.Bytes(), "image/png", 256)
	require.Equal(t, "image/jpeg", contentType)
	assert.NotNil(t, avgColor)
	assert.Less(t, len(resized), in.Len(), "a lossy encode should beat the high-entropy png")
	decoded, err := jpeg.Decode(bytes.NewReader(resized))
	require.NoError(t, err)
	assert.Equal(t, dim, decoded.Bounds().Dx(), "native dimensions preserved when no downscale")
	assert.Equal(t, dim, decoded.Bounds().Dy())
}

// noisyTestImage is high-frequency content, which PNG cannot compress and
// lossy WebP can, so an encode of it is always smaller than its PNG.
func noisyTestImage(dim int) *image.RGBA {
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
	return src
}

// A small source that already fits is handed back as it is: on ARM32 the
// WebP encode was nearly the whole request and saved a few kilobytes.
func TestResizeImageWithColor_SmallSourceThatFitsSkipsEncode(t *testing.T) {
	t.Parallel()

	// The PNG here is one WebP would shrink, so it is the size rule and not
	// the "WebP no smaller" rule that keeps the original.
	src := noisyTestImage(64)
	var pngBytes, jpegBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, src))
	require.NoError(t, jpeg.Encode(&jpegBytes, src, &jpeg.Options{Quality: 90}))
	webpBytes, webpType, err := encodeWebPImage(src)
	require.NoError(t, err)
	require.Less(t, len(webpBytes), pngBytes.Len())

	tests := []struct {
		name        string
		contentType string
		binary      []byte
		maxSize     int
	}{
		{name: "jpeg below the box", contentType: "image/jpeg", binary: jpegBytes.Bytes(), maxSize: 128},
		{name: "jpeg exactly the box", contentType: "image/jpeg", binary: jpegBytes.Bytes(), maxSize: 64},
		{name: "png", contentType: "image/png", binary: pngBytes.Bytes(), maxSize: 128},
		{name: "webp", contentType: webpType, binary: webpBytes, maxSize: 128},
		{name: "jpeg with no content type", contentType: "", binary: jpegBytes.Bytes(), maxSize: 128},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.LessOrEqual(t, len(tt.binary), thumbPassthroughMaxBytes)

			resized, contentType, avgColor := resizeImageWithColor(tt.binary, tt.contentType, tt.maxSize)
			assert.Equal(t, tt.binary, resized, "original bytes returned unchanged")
			assert.Equal(t, tt.contentType, contentType)
			require.NotNil(t, avgColor, "the colour still comes from the decoded frame")
			assert.Equal(t, *averageImageColor(mustDecodeResizable(t, tt.binary, tt.contentType)), *avgColor)
		})
	}
}

func mustDecodeResizable(t *testing.T, binary []byte, contentType string) image.Image {
	t.Helper()
	img, err := decodeResizableImage(binary, contentType)
	require.NoError(t, err)
	return img
}

func TestResizeImageWithColor_SmallSourceNeedingDownscaleStillEncodes(t *testing.T) {
	t.Parallel()

	var in bytes.Buffer
	require.NoError(t, jpeg.Encode(&in, noisyTestImage(64), &jpeg.Options{Quality: 90}))
	require.LessOrEqual(t, in.Len(), thumbPassthroughMaxBytes)

	resized, contentType, avgColor := resizeImageWithColor(in.Bytes(), "image/jpeg", 32)
	require.Equal(t, "image/jpeg", contentType)
	assert.NotNil(t, avgColor)
	decoded, err := jpeg.Decode(bytes.NewReader(resized))
	require.NoError(t, err)
	assert.Equal(t, 32, decoded.Bounds().Dx())
	assert.Equal(t, 32, decoded.Bounds().Dy())
}

// The thumbnail cache names files by format. A source whose declared type it
// cannot store would miss the cache on every request if passed through, so it
// keeps going through the encode.
func TestResizeImageWithColor_SmallSourceTheCacheCannotStoreStillEncodes(t *testing.T) {
	t.Parallel()

	var in bytes.Buffer
	require.NoError(t, png.Encode(&in, noisyTestImage(64)))
	require.LessOrEqual(t, in.Len(), thumbPassthroughMaxBytes)
	require.Empty(t, thumbCacheExtension("application/octet-stream", in.Bytes()))

	resized, contentType, avgColor := resizeImageWithColor(in.Bytes(), "application/octet-stream", 128)
	assert.Equal(t, "image/jpeg", contentType)
	assert.NotNil(t, avgColor)
	assert.Less(t, len(resized), in.Len())
}

// A passed-through original lands in the thumbnail cache under its own
// format, and both deliveries serve it back as that format, cold and warm.
func TestHandleMediaImage_SmallSourceThatFitsIsCachedInItsOwnFormat(t *testing.T) {
	// Not parallel: installs process-wide thumbnail cache pointer.
	cache := &mediaThumbCache{
		fs: afero.NewOsFs(), dir: filepath.Join(t.TempDir(), mediaThumbCacheVersionDir()),
		resolved: make(map[string]resolvedThumb),
	}
	mediaThumbCachePointer.Store(cache)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })

	var blob bytes.Buffer
	require.NoError(t, jpeg.Encode(&blob, noisyTestImage(64), &jpeg.Options{Quality: 90}))
	require.LessOrEqual(t, blob.Len(), thumbPassthroughMaxBytes)

	row := makeMediaFullRow(9101, 9110)
	mockDB := testhelpers.NewMockMediaDBI()
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/jpeg", Binary: blob.Bytes()},
		}, nil)

	result, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, mediaImageParams(
		row, `"maxSize": 256, "delivery": "localPath"`,
	)))
	require.NoError(t, err)
	cold, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, mediaImageDeliveryPath, cold.Delivery)
	assert.Equal(t, "image/jpeg", cold.ContentType)
	assert.Equal(t, ".jpg", filepath.Ext(cold.LocalPath))
	assert.True(t, cache.isSafeLocalPath(cold.LocalPath))
	stored, err := afero.ReadFile(cache.fs, cold.LocalPath)
	require.NoError(t, err)
	assert.Equal(t, blob.Bytes(), stored, "the original is stored, not a re-encode")

	// Warm, from the cache alone: a strict mock fails on any database call.
	strictDB := testhelpers.NewMockMediaDBI()
	result, err = HandleMediaImage(makeMediaImageEnv(t, strictDB, mediaImageParams(
		row, `"maxSize": 256, "delivery": "localPath"`,
	)))
	require.NoError(t, err)
	warm, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, cold.LocalPath, warm.LocalPath)
	assert.Equal(t, "image/jpeg", warm.ContentType)

	result, err = HandleMediaImage(makeMediaImageEnv(t, strictDB, mediaImageParams(row, `"maxSize": 256`)))
	require.NoError(t, err)
	inline, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/jpeg", inline.ContentType)
	data, err := base64.StdEncoding.DecodeString(inline.Data)
	require.NoError(t, err)
	assert.Equal(t, blob.Bytes(), data)
	strictDB.AssertExpectations(t)
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
	webpBytes, _, err := encodeWebPImage(src)
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

func TestHandleMediaImage_TimingLog(t *testing.T) {
	// Not parallel: swaps process-wide logger.
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	t.Run("success", func(t *testing.T) {
		var imageData bytes.Buffer
		require.NoError(t, png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))))

		mockDB := testhelpers.NewMockMediaDBI()
		row := makeMediaFullRow(9200, 9210)
		mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{row.DBID}).
			Return(map[int64]database.MediaFullRow{row.DBID: *row}, nil)
		mockDB.On("GetMediaProperties", mock.Anything, row.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).
			Return([]database.MediaProperty{
				{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: imageData.Bytes()},
			}, nil)

		env := makeMediaImageEnv(t, mockDB, json.RawMessage(
			`{"mediaId":9200,"delivery":"inline","maxSize":300}`))
		event := captureHandlerLogEvent(t, "media.image handler timing", func() {
			_, err := HandleMediaImage(env)
			require.NoError(t, err)
		})

		assert.Equal(t, true, event["ok"])
		assert.Equal(t, mediaImageDeliveryInline, event["delivery"])
		assert.Equal(t, true, event["mediaId"])
		assert.Equal(t, json.Number("512"), event["maxSize"])
		assert.Contains(t, event, "duration")
		mockDB.AssertExpectations(t)
	})

	t.Run("validation error", func(t *testing.T) {
		env := makeMediaImageEnv(t, testhelpers.NewMockMediaDBI(), json.RawMessage(
			`{"mediaId":9201,"delivery":"localPath"}`))
		event := captureHandlerLogEvent(t, "media.image handler timing", func() {
			_, err := HandleMediaImage(env)
			require.Error(t, err)
		})

		assert.Equal(t, false, event["ok"])
		assert.Equal(t, mediaImageDeliveryPath, event["delivery"])
		assert.Equal(t, true, event["mediaId"])
		assert.Contains(t, event, "duration")
	})
}

func TestHandleMediaImage_DirectoryPathUsesDirectoryArtwork(t *testing.T) {
	// Not parallel: resets process-wide no-image cache.
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	var imageData bytes.Buffer
	require.NoError(t, png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	imagePath := filepath.Join(t.TempDir(), "Collection.png")
	require.NoError(t, os.WriteFile(imagePath, imageData.Bytes(), 0o600))
	directoryPath := filepath.Join("games", "Collection")
	system := database.System{DBID: 100, SystemID: "NES", Name: "NES"}

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("FindSystemBySystemID", system.SystemID).Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, directoryPath).
		Return((*database.Media)(nil), nil)
	mockDB.On("GetDirectoryProperties", mock.Anything, system.DBID, directoryPath).
		Return([]database.MediaProperty{{
			TypeTag: "property:image-boxart", Text: imagePath,
		}}, nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(fmt.Sprintf(
		`{"system":%q,"path":%q,"imageTypes":["boxart"]}`, system.SystemID, directoryPath,
	)))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	decoded, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	assert.Equal(t, imageData.Bytes(), decoded)
	mockDB.AssertExpectations(t)
}

// A multi-disc folder collapses to its first disc, which may carry artwork of
// its own. Asked for by path, the folder's artwork wins even when it is of a
// less preferred type; the disc's artwork is only the fallback.
func TestHandleMediaImage_CollapsedDirectoryPrefersFolderArtwork(t *testing.T) {
	// Not parallel: resets process-wide no-image cache.
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	writeImage := func(name string, shade uint8) (string, []byte) {
		var data bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{R: shade, A: 255})
		require.NoError(t, png.Encode(&data, img))
		imagePath := filepath.Join(t.TempDir(), name)
		require.NoError(t, os.WriteFile(imagePath, data.Bytes(), 0o600))
		return imagePath, data.Bytes()
	}
	folderPath, folderData := writeImage("Chrono Cross (USA).png", 10)
	discPath, discData := writeImage("Chrono Cross (USA) (Disc 1).png", 200)

	directoryPath := filepath.Join("games", "Chrono Cross (USA)")
	system := database.System{DBID: 100, SystemID: "PSX", Name: "PSX"}
	disc := database.Media{DBID: 20, Path: filepath.Join(directoryPath, "Chrono Cross (USA) (Disc 1).cue")}
	row := &database.MediaFullRow{
		Media:  disc,
		Title:  database.MediaTitle{DBID: 30, Name: "Chrono Cross"},
		System: system,
	}
	request := json.RawMessage(fmt.Sprintf(
		`{"system":%q,"path":%q,"imageTypes":["boxart","image"]}`, system.SystemID, directoryPath,
	))

	t.Run("folder artwork wins", func(t *testing.T) {
		mockDB := testhelpers.NewMockMediaDBI()
		mockDB.On("FindSystemBySystemID", system.SystemID).Return(system, nil)
		mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, directoryPath).
			Return((*database.Media)(nil), nil)
		mockDB.On("GetDirectoryProperties", mock.Anything, system.DBID, directoryPath).
			Return([]database.MediaProperty{{TypeTag: "property:image-image", Text: folderPath}}, nil)

		result, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		require.NoError(t, err)
		resp, ok := result.(models.MediaImageResponse)
		require.True(t, ok)
		assert.Equal(t, "property:image-image", resp.TypeTag)
		decoded, err := base64.StdEncoding.DecodeString(resp.Data)
		require.NoError(t, err)
		assert.Equal(t, folderData, decoded)
		mockDB.AssertNotCalled(t, "FindSingleContainerLaunchMedia", mock.Anything, mock.Anything, mock.Anything)
		mockDB.AssertExpectations(t)
	})

	t.Run("first disc artwork is the fallback", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		mockDB.On("FindSystemBySystemID", system.SystemID).Return(system, nil)
		mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, directoryPath).
			Return((*database.Media)(nil), nil)
		mockDB.On("GetDirectoryProperties", mock.Anything, system.DBID, directoryPath).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, system.DBID, directoryPath).
			Return(&disc, nil)
		mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, disc.DBID).Return(row, nil)
		mockDB.On("GetMediaProperties", mock.Anything, disc.DBID).
			Return([]database.MediaProperty{{TypeTag: "property:image-boxart", Text: discPath}}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).
			Return([]database.MediaProperty{}, nil)

		result, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		require.NoError(t, err)
		resp, ok := result.(models.MediaImageResponse)
		require.True(t, ok)
		assert.Equal(t, "property:image-boxart", resp.TypeTag)
		decoded, err := base64.StdEncoding.DecodeString(resp.Data)
		require.NoError(t, err)
		assert.Equal(t, discData, decoded)
		mockDB.AssertExpectations(t)
	})
}

// directoryMediaRefFixture is a multi-disc folder whose browse entry launches
// its second disc.
type directoryMediaRefFixture struct {
	disc1, disc2  *database.MediaFullRow
	directoryPath string
	system        database.System
}

func newDirectoryMediaRefFixture() directoryMediaRefFixture {
	directoryPath := filepath.Join("games", "Chrono Cross (USA)")
	system := database.System{DBID: 100, SystemID: "PSX", Name: "PSX"}
	title := database.MediaTitle{DBID: 30, Name: "Chrono Cross"}
	disc := func(id int64, n int) *database.MediaFullRow {
		return &database.MediaFullRow{
			Media: database.Media{
				DBID: id, Path: filepath.Join(directoryPath, fmt.Sprintf("Chrono Cross (USA) (Disc %d).cue", n)),
			},
			Title:  title,
			System: system,
		}
	}
	return directoryMediaRefFixture{
		system: system, directoryPath: directoryPath, disc1: disc(20, 1), disc2: disc(21, 2),
	}
}

func (f directoryMediaRefFixture) request(disc *database.MediaFullRow, extra string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"mediaId":%d,"system":%q,"path":%q%s}`,
		disc.DBID, f.system.SystemID, f.directoryPath, extra))
}

func expectMediaByID(mockDB *testhelpers.MockMediaDBI, disc *database.MediaFullRow) {
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{disc.DBID}).
		Return(map[int64]database.MediaFullRow{disc.DBID: *disc}, nil)
}

func writeShadedPNG(t *testing.T, name string, shade uint8) (imagePath string, data []byte) {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: shade, A: 255})
	require.NoError(t, png.Encode(&buf, img))
	imagePath = filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(imagePath, buf.Bytes(), 0o600))
	return imagePath, buf.Bytes()
}

func decodeMediaImageData(t *testing.T, result any) (typeTag string, data []byte) {
	t.Helper()
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	data, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	return resp.TypeTag, data
}

// A multi-disc folder's entry launches the disc last played from it. Asked for
// with that disc's mediaId, the folder's image follows the disc instead of
// resolving the path to the first disc.
func TestHandleMediaImage_DirectoryMediaRef(t *testing.T) {
	// Not parallel: resets process-wide no-image cache.
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	f := newDirectoryMediaRefFixture()
	folderPath, folderData := writeShadedPNG(t, "folder.png", 10)
	disc1Path, disc1Data := writeShadedPNG(t, "disc1.png", 100)
	disc2Path, disc2Data := writeShadedPNG(t, "disc2.png", 200)
	titlePath, titleData := writeShadedPNG(t, "title.png", 250)
	request := f.request(f.disc2, `,"imageTypes":["boxart","image"]`)

	t.Run("folder artwork wins", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, f.disc2)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty{{TypeTag: "property:image-image", Text: folderPath}}, nil)

		result, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		require.NoError(t, err)
		typeTag, data := decodeMediaImageData(t, result)
		assert.Equal(t, "property:image-image", typeTag)
		assert.Equal(t, folderData, data)
		mockDB.AssertNotCalled(t, "GetMediaProperties", mock.Anything, mock.Anything)
		mockDB.AssertExpectations(t)
	})

	t.Run("given disc artwork, not the first disc's", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, f.disc2)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaProperties", mock.Anything, f.disc2.DBID).
			Return([]database.MediaProperty{{TypeTag: "property:image-boxart", Text: disc2Path}}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, f.disc2.Title.DBID).
			Return([]database.MediaProperty{}, nil)

		result, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		require.NoError(t, err)
		typeTag, data := decodeMediaImageData(t, result)
		assert.Equal(t, "property:image-boxart", typeTag)
		assert.Equal(t, disc2Data, data)
		mockDB.AssertNotCalled(t, "FindSingleContainerLaunchMedia", mock.Anything, mock.Anything, mock.Anything)
		mockDB.AssertExpectations(t)
	})

	t.Run("given disc's title artwork", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, f.disc2)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaProperties", mock.Anything, f.disc2.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, f.disc2.Title.DBID).
			Return([]database.MediaProperty{{TypeTag: "property:image-boxart", Text: titlePath}}, nil)

		result, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		require.NoError(t, err)
		_, data := decodeMediaImageData(t, result)
		assert.Equal(t, titleData, data)
		mockDB.AssertNotCalled(t, "FindSingleContainerLaunchMedia", mock.Anything, mock.Anything, mock.Anything)
		mockDB.AssertExpectations(t)
	})

	t.Run("first disc artwork is the last fallback", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, f.disc2)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaProperties", mock.Anything, f.disc2.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, f.disc2.Title.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("FindSystemBySystemID", f.system.SystemID).Return(f.system, nil)
		mockDB.On("FindMediaBySystemAndPath", mock.Anything, f.system.DBID, f.directoryPath).
			Return((*database.Media)(nil), nil)
		mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, f.system.DBID, f.directoryPath).
			Return(&f.disc1.Media, nil)
		mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, f.disc1.DBID).Return(f.disc1, nil)
		mockDB.On("GetMediaProperties", mock.Anything, f.disc1.DBID).
			Return([]database.MediaProperty{{TypeTag: "property:image-boxart", Text: disc1Path}}, nil)

		result, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		require.NoError(t, err)
		_, data := decodeMediaImageData(t, result)
		assert.Equal(t, disc1Data, data)
		mockDB.AssertExpectations(t)
	})

	t.Run("no image anywhere is per disc", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, f.disc2)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaProperties", mock.Anything, f.disc2.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, f.disc2.Title.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("FindSystemBySystemID", f.system.SystemID).Return(f.system, nil)
		mockDB.On("FindMediaBySystemAndPath", mock.Anything, f.system.DBID, f.directoryPath).
			Return((*database.Media)(nil), nil)
		mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, f.system.DBID, f.directoryPath).
			Return((*database.Media)(nil), nil)

		_, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		var noImage *mediaImageNotFoundError
		require.ErrorAs(t, err, &noImage)
		mockDB.AssertExpectations(t)

		// The cached miss for disc 2 does not answer for disc 1.
		mockDB = testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, f.disc1)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaProperties", mock.Anything, f.disc1.DBID).
			Return([]database.MediaProperty{{TypeTag: "property:image-boxart", Text: disc1Path}}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, f.disc1.Title.DBID).
			Return([]database.MediaProperty{}, nil)
		result, err := HandleMediaImage(makeMediaImageEnv(t, mockDB,
			f.request(f.disc1, `,"imageTypes":["boxart","image"]`)))
		require.NoError(t, err)
		_, data := decodeMediaImageData(t, result)
		assert.Equal(t, disc1Data, data)
		mockDB.AssertExpectations(t)
	})

	t.Run("missing folder artwork file is not cached as no image", func(t *testing.T) {
		mediaImageNoImages.clear()
		missing := filepath.Join(t.TempDir(), "gone.png")
		mockDB := testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, f.disc2)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty{{TypeTag: "property:image-image", Text: missing}}, nil)
		mockDB.On("GetMediaProperties", mock.Anything, f.disc2.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, f.disc2.Title.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("FindSystemBySystemID", f.system.SystemID).Return(f.system, nil)
		mockDB.On("FindMediaBySystemAndPath", mock.Anything, f.system.DBID, f.directoryPath).
			Return((*database.Media)(nil), nil)
		mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, f.system.DBID, f.directoryPath).
			Return((*database.Media)(nil), nil)

		_, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		var noImage *mediaImageNotFoundError
		require.ErrorAs(t, err, &noImage)
		ref, _, err := parseMediaImageRequest(request)
		require.NoError(t, err)
		cached, _ := mediaImageNoImages.get(mediaImageNoImageRequestKey(ref, imagePrefs(nil, ref.ImageTypes)))
		assert.False(t, cached, "a restored folder artwork file must be found on the next request")
		mockDB.AssertExpectations(t)
	})

	// expectDisc2Miss sets up a request for disc 2 that finds no folder, disc
	// or title artwork, so the path fallback runs next.
	expectDisc2Miss := func(mockDB *testhelpers.MockMediaDBI) {
		expectMediaByID(mockDB, f.disc2)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaProperties", mock.Anything, f.disc2.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, f.disc2.Title.DBID).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("FindSystemBySystemID", f.system.SystemID).Return(f.system, nil)
		mockDB.On("FindMediaBySystemAndPath", mock.Anything, f.system.DBID, f.directoryPath).
			Return((*database.Media)(nil), nil)
	}

	t.Run("path resolving to the given disc is not loaded twice", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		expectDisc2Miss(mockDB)
		mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, f.system.DBID, f.directoryPath).
			Return(&f.disc2.Media, nil)
		mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, f.disc2.DBID).Return(f.disc2, nil)

		_, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		var noImage *mediaImageNotFoundError
		require.ErrorAs(t, err, &noImage)
		mockDB.AssertNumberOfCalls(t, "GetMediaProperties", 1)
		mockDB.AssertExpectations(t)
	})

	t.Run("no artwork on either disc names the given disc", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		expectDisc2Miss(mockDB)
		mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, f.system.DBID, f.directoryPath).
			Return(&f.disc1.Media, nil)
		mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, f.disc1.DBID).Return(f.disc1, nil)
		mockDB.On("GetMediaProperties", mock.Anything, f.disc1.DBID).
			Return([]database.MediaProperty{}, nil)

		_, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		var noImage *mediaImageNotFoundError
		require.ErrorAs(t, err, &noImage)
		assert.Equal(t, f.disc2.Path, noImage.path)
		mockDB.AssertExpectations(t)
	})

	t.Run("database error in the path fallback is returned, not cached", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		expectDisc2Miss(mockDB)
		mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, f.system.DBID, f.directoryPath).
			Return((*database.Media)(nil), assert.AnError)

		_, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		require.ErrorIs(t, err, assert.AnError)
		var noImage *mediaImageNotFoundError
		require.NotErrorAs(t, err, &noImage)
		mockDB.AssertExpectations(t)
	})

	t.Run("database errors are returned", func(t *testing.T) {
		mediaImageNoImages.clear()
		mockDB := testhelpers.NewMockMediaDBI()
		mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{f.disc2.DBID}).
			Return(map[int64]database.MediaFullRow(nil), assert.AnError)
		_, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		require.ErrorIs(t, err, assert.AnError)

		mockDB = testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, f.disc2)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty(nil), assert.AnError)
		_, err = HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
		require.ErrorIs(t, err, assert.AnError)
		mockDB.AssertNotCalled(t, "GetMediaProperties", mock.Anything, mock.Anything)
	})

	t.Run("media outside the path is rejected", func(t *testing.T) {
		mediaImageNoImages.clear()
		other := makeMediaFullRow(40, 41)
		other.System = f.system
		mockDB := testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, other)

		_, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, f.request(other, "")))
		require.ErrorContains(t, err, "media not in path")
		mockDB.AssertNotCalled(t, "GetDirectoryProperties", mock.Anything, mock.Anything, mock.Anything)
		mockDB.AssertExpectations(t)
	})
}

// A path request whose folder artwork file is missing keeps its final
// no-image result out of the no-image cache, so the restored file is found.
func TestHandleMediaImage_DirectoryPathMissingFolderArtworkNotCached(t *testing.T) {
	// Not parallel: resets process-wide no-image cache.
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)

	f := newDirectoryMediaRefFixture()
	missing := filepath.Join(t.TempDir(), "gone.png")
	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("FindSystemBySystemID", f.system.SystemID).Return(f.system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, f.system.DBID, f.directoryPath).
		Return((*database.Media)(nil), nil)
	mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
		Return([]database.MediaProperty{{TypeTag: "property:image-image", Text: missing}}, nil)
	mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, f.system.DBID, f.directoryPath).
		Return(&f.disc1.Media, nil)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, f.disc1.DBID).Return(f.disc1, nil)
	mockDB.On("GetMediaProperties", mock.Anything, f.disc1.DBID).Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, f.disc1.Title.DBID).
		Return([]database.MediaProperty{}, nil)

	request := json.RawMessage(fmt.Sprintf(`{"system":%q,"path":%q}`, f.system.SystemID, f.directoryPath))
	_, err := HandleMediaImage(makeMediaImageEnv(t, mockDB, request))
	var noImage *mediaImageNotFoundError
	require.ErrorAs(t, err, &noImage)
	cached, _ := mediaImageNoImages.get(mediaImageNoImagePathKey(f.system.SystemID, f.directoryPath, defaultImageTypes))
	assert.False(t, cached)
	mockDB.AssertExpectations(t)
}

// The resolved-thumbnail memo keys a directory media ref by its disc, so
// switching the disc a folder launches switches the thumbnail served.
func TestHandleMediaImage_DirectoryMediaRefThumbMemoFollowsDisc(t *testing.T) {
	// Not parallel: installs the process-wide thumb cache pointer.
	mediaImageNoImages.clear()
	t.Cleanup(mediaImageNoImages.clear)
	cache := &mediaThumbCache{
		fs:       afero.NewMemMapFs(),
		dir:      filepath.Join("cache", mediaThumbCacheDirName, mediaThumbCacheVersionDir()),
		resolved: make(map[string]resolvedThumb),
	}
	mediaThumbCachePointer.Store(cache)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })

	f := newDirectoryMediaRefFixture()
	disc1Path, disc1Data := writeShadedPNG(t, "disc1.png", 100)
	disc2Path, disc2Data := writeShadedPNG(t, "disc2.png", 200)

	for _, tc := range []struct {
		disc     *database.MediaFullRow
		artPath  string
		expected []byte
	}{
		{disc: f.disc1, artPath: disc1Path, expected: disc1Data},
		{disc: f.disc2, artPath: disc2Path, expected: disc2Data},
	} {
		mockDB := testhelpers.NewMockMediaDBI()
		expectMediaByID(mockDB, tc.disc)
		mockDB.On("GetDirectoryProperties", mock.Anything, f.system.DBID, f.directoryPath).
			Return([]database.MediaProperty{}, nil)
		mockDB.On("GetMediaProperties", mock.Anything, tc.disc.DBID).
			Return([]database.MediaProperty{{TypeTag: "property:image-boxart", Text: tc.artPath}}, nil)
		mockDB.On("GetMediaTitleProperties", mock.Anything, tc.disc.Title.DBID).
			Return([]database.MediaProperty{}, nil)

		result, err := HandleMediaImage(makeMediaImageEnv(t, mockDB,
			f.request(tc.disc, `,"imageTypes":["boxart"],"maxSize":256`)))
		require.NoError(t, err)
		_, data := decodeMediaImageData(t, result)
		assert.Equal(t, tc.expected, data)
		mockDB.AssertExpectations(t)
	}
	assert.Len(t, cache.resolved, 2)
}

func TestHandleMediaImage_MaxSizeResizesAndCachesThumbnail(t *testing.T) {
	// Not parallel: installs the process-wide thumb cache pointer.
	fs := afero.NewMemMapFs()
	cache := &mediaThumbCache{
		fs:       fs,
		dir:      filepath.Join("cache", mediaThumbCacheDirName, mediaThumbCacheVersionDir()),
		resolved: make(map[string]resolvedThumb),
	}
	mediaThumbCachePointer.Store(cache)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })

	// A high-entropy source larger than the snapped tier (256), so the handler
	// both downscales it and re-encodes to a smaller JPEG than the source PNG.
	const dim = 300
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
	var blob bytes.Buffer
	require.NoError(t, png.Encode(&blob, src))

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(1)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(10)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: blob.Bytes()},
		}, nil)

	// First request: loads the original, resizes to a 256px JPEG, caches it.
	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"maxSize": 256`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/jpeg", resp.ContentType)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	firstData, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	decoded, err := jpeg.Decode(bytes.NewReader(firstData))
	require.NoError(t, err)
	assert.Equal(t, 256, decoded.Bounds().Dx(), "downscaled to the snapped tier")
	assert.Less(t, len(firstData), blob.Len(), "resized jpeg is smaller than the source png")

	// Second request: the pre-semaphore fast path must serve it from the disk
	// thumb cache and the resolved-type memo without touching the database. A
	// strict mock with no expectations fails on any unexpected call.
	strictDB := testhelpers.NewMockMediaDBI()
	env2 := makeMediaImageEnv(t, strictDB, mediaImageParams(row, `"maxSize": 256`))
	result2, err := HandleMediaImage(env2)
	require.NoError(t, err)
	resp2, ok := result2.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, resp.Data, resp2.Data, "cache hit returns the identical thumbnail")
	strictDB.AssertExpectations(t)
}

func expectInlineImageProperty(mockDB *testhelpers.MockMediaDBI, row *database.MediaFullRow, data []byte) {
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: data},
		}, nil)
}

func TestHandleMediaImage_LocalPathColdAndWarm(t *testing.T) {
	// Not parallel: installs process-wide thumbnail cache pointer.
	cache := &mediaThumbCache{
		fs: afero.NewOsFs(), dir: filepath.Join(t.TempDir(), mediaThumbCacheVersionDir()),
		resolved: make(map[string]resolvedThumb),
	}
	mediaThumbCachePointer.Store(cache)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })

	row := makeMediaFullRow(9001, 9010)
	mockDB := testhelpers.NewMockMediaDBI()
	expectInlineImageProperty(mockDB, row, []byte("cached-image"))
	env := makeMediaImageEnv(t, mockDB, mediaImageParams(
		row, `"maxSize": 256, "delivery": "localPath"`,
	))
	// Path delivery is explicit and independent of peer locality or platform.
	require.False(t, env.IsLocal)
	require.Nil(t, env.Platform)

	result, err := HandleMediaImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, mediaImageDeliveryPath, resp.Delivery)
	assert.Empty(t, resp.Data)
	assert.True(t, filepath.IsAbs(resp.LocalPath))
	assert.True(t, cache.isSafeLocalPath(resp.LocalPath))
	cached, err := afero.ReadFile(cache.fs, resp.LocalPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("cached-image"), cached)

	strictDB := testhelpers.NewMockMediaDBI()
	warmEnv := makeMediaImageEnv(t, strictDB, mediaImageParams(
		row, `"maxSize": 256, "delivery": "localPath"`,
	))
	warmResult, err := HandleMediaImage(warmEnv)
	require.NoError(t, err)
	warmResp, ok := warmResult.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, resp.LocalPath, warmResp.LocalPath)
	assert.Equal(t, mediaImageDeliveryPath, warmResp.Delivery)
	strictDB.AssertExpectations(t)
}

func TestHandleMediaImage_LocalPathCacheWriteFailureFallsBackInline(t *testing.T) {
	// Not parallel: installs process-wide thumbnail cache pointer.
	cache := &mediaThumbCache{
		fs:       afero.NewReadOnlyFs(afero.NewMemMapFs()),
		dir:      filepath.Join(string(filepath.Separator), "cache", mediaThumbCacheVersionDir()),
		resolved: make(map[string]resolvedThumb),
	}
	mediaThumbCachePointer.Store(cache)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })

	row := makeMediaFullRow(9002, 9020)
	mockDB := testhelpers.NewMockMediaDBI()
	expectInlineImageProperty(mockDB, row, []byte("inline-fallback"))
	env := makeMediaImageEnv(t, mockDB, mediaImageParams(
		row, `"maxSize": 256, "delivery": "localPath"`,
	))

	result, err := HandleMediaImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, mediaImageDeliveryInline, resp.Delivery)
	assert.Empty(t, resp.LocalPath)
	decoded, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	assert.Equal(t, []byte("inline-fallback"), decoded)
}

func TestHandleMediaImage_LocalPathValidation(t *testing.T) {
	tests := []struct {
		name      string
		extra     string
		wantError string
	}{
		{
			name: "missing maxSize", extra: `"delivery": "localPath"`,
			wantError: "requires a positive maxSize",
		},
		{
			name: "unknown delivery", extra: `"delivery": "sharedMemory"`,
			wantError: "unsupported delivery",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := makeMediaFullRow(9100, 9110)
			env := makeMediaImageEnv(t, testhelpers.NewMockMediaDBI(), mediaImageParams(row, tt.extra))

			_, err := HandleMediaImage(env)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
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
	ref, _, err := parseMediaImageRequest(params)
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

	release := fillMediaImageSem()
	defer release()

	params := json.RawMessage(`{"mediaId":406,"imageTypes":["boxart"]}`)
	ref, _, err := parseMediaImageRequest(params)
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

	release := fillMediaImageSem()

	params := json.RawMessage(`{"mediaId":407,"imageTypes":["boxart"]}`)
	ref, _, err := parseMediaImageRequest(params)
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
	release()

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
	release := fillMediaImageSem()
	defer release()

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

// The reaper counts what a file takes on disk, which for a small thumbnail is
// a whole cluster, not its few bytes.
func TestThumbFileDiskBytes_FallsBackToSize(t *testing.T) {
	t.Parallel()

	memInfo, err := afero.NewMemMapFs().Stat(".")
	require.NoError(t, err)
	assert.Equal(t, memInfo.Size(), thumbFileDiskBytes(memInfo, 0), "without an allocation to read the size stands in")
}

// Writing thumbnails past the cap reaps the cache without waiting for an index
// or scrape, and at most once an hour while recently used files hold it over.
func TestNoteWritten_ReapsOncePastTheCap(t *testing.T) {
	t.Parallel()

	cache, clock := newReapTestCache(t, 1000)
	old := writeReapTestFile(t, cache, "d:old", 600, clock.Now().Add(-10*24*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	cache.reapCtx = ctx
	defer cache.stopReaps(cancel)

	setMediaThumbCacheForTest(t, cache, "d:new", 256, bytes.Repeat([]byte{1}, 300), "image/webp")
	cache.reapWG.Wait()
	assert.True(t, thumbFileExists(t, cache, old), "under the cap nothing is reaped")

	setMediaThumbCacheForTest(t, cache, "d:newer", 256, bytes.Repeat([]byte{1}, 300), "image/webp")
	cache.reapWG.Wait()
	assert.False(t, thumbFileExists(t, cache, old), "passing the cap reaps the least recently used file")

	again := writeReapTestFile(t, cache, "d:again", 600, clock.Now().Add(-10*24*time.Hour))
	cache.reapWG.Wait()
	assert.True(t, thumbFileExists(t, cache, again), "a second pass within the hour is not walked")

	clock.Advance(mediaThumbCapReapInterval)
	setMediaThumbCacheForTest(t, cache, "d:last", 256, bytes.Repeat([]byte{1}, 300), "image/webp")
	cache.reapWG.Wait()
	assert.False(t, thumbFileExists(t, cache, again))
}

// PNG covers decode through Wuffs, and a PNG it rejects still gets
// image/png's verdict.
func TestDecodeResizableImage_PNGUsesWuffs(t *testing.T) {
	t.Parallel()

	var valid bytes.Buffer
	require.NoError(t, png.Encode(&valid, noisyTestImage(16)))
	img, err := decodeResizableImage(valid.Bytes(), "image/png")
	require.NoError(t, err)
	if _, wuffsErr := wuffspng.Decode(valid.Bytes()); !errors.Is(wuffsErr, wuffspng.ErrUnavailable) {
		assert.IsType(t, &image.RGBA{}, img, "decoded by Wuffs")
	}

	corrupt := bytes.Clone(valid.Bytes())
	corrupt[len(corrupt)-20] ^= 0xff
	_, err = decodeResizableImage(corrupt, "image/png")
	require.Error(t, err, "both decoders reject a corrupt PNG")
}

// pngWithDimensions is a valid 1x1 PNG whose header declares another size.
func pngWithDimensions(t *testing.T, width, height uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))))
	data := buf.Bytes()
	const ihdr = 8 // after the signature: length, "IHDR", 13 bytes of data, CRC
	byteorder.BigEndian.PutUint32(data[ihdr+8:], width)
	byteorder.BigEndian.PutUint32(data[ihdr+12:], height)
	byteorder.BigEndian.PutUint32(data[ihdr+8+13:], crc32.ChecksumIEEE(data[ihdr+4:ihdr+8+13]))
	return data
}

// A small file declaring a huge frame is refused before any decoder
// allocates it.
func TestDecodeResizableImage_RejectsOversizedFrames(t *testing.T) {
	t.Parallel()

	_, err := decodeResizableImage(pngWithDimensions(t, 4097, 4096), "image/png")
	require.ErrorContains(t, err, "pixel limit")

	_, err = decodeResizableImage(pngWithDimensions(t, 4096, 4096), "image/png")
	require.Error(t, err, "a 1x1 body does not fill the declared frame")
	assert.NotContains(t, err.Error(), "pixel limit", "a frame at the limit is let through to the decoder")
}
