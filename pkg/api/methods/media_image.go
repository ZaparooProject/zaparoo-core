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
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KarpelesLab/gowebp"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	misterMediaImageMaxBytes  = int64(2 * 1024 * 1024)
	defaultMediaImageMaxBytes = int64(8 * 1024 * 1024)
	mediaImageNoImageMax      = 4096
	mediaImageDeliveryInline  = "inline"
	mediaImageDeliveryPath    = "localPath"
	// mediaThumbCacheDirName is the sub-directory under the core cache dir where
	// resized thumbnail files are persisted across restarts.
	mediaThumbCacheDirName = "thumbs"
	// mediaThumbCacheVersion identifies the on-disk encode scheme. The live cache
	// directory is thumbs/v<N>; bumping this constant changes the live directory
	// so a new build invalidates the cache automatically (the old v<N-1> dir is
	// reaped on start and thumbnails regenerate lazily) without users reindexing.
	// Bump whenever the resized output changes: format, quality, or resize
	// algorithm. v2 groups entries by system for targeted invalidation; v1
	// introduced lossy WebP output (replacing lossless PNG / JPEG q85).
	mediaThumbCacheVersion = 2
	// mediaThumbWebPQuality and mediaThumbWebPMethod are the lossy VP8 settings
	// used to re-encode resized thumbnails. q75/method2 was selected by
	// benchmarking real covers as the best size/speed point: ~13x smaller than
	// the previous lossless-PNG output while preserving alpha, and faster to
	// encode than the higher methods. Resized output is always WebP regardless
	// of source format.
	mediaThumbWebPQuality = 75
	mediaThumbWebPMethod  = 2
)

// mediaImageSem limits concurrent image lookups so high-volume image misses do
// not saturate SQLite and starve browse/status calls. MediaDB reads use a small
// connection pool, so a few lookups may overlap without blocking browse.
var mediaImageSem = make(chan struct{}, 3)

const (
	// mediaImageResizeConcurrency bounds concurrent decode/resize/encode work,
	// which is CPU and memory heavy and otherwise unbounded once the lookup
	// semaphore is released. Resource-constrained platforms resize one at a time.
	mediaImageResizeConcurrency            = 2
	mediaImageResizeConcurrencyConstrained = 1
)

// defaultMediaImageResizeSem is used when no thumbnail cache was initialised.
var defaultMediaImageResizeSem = make(chan struct{}, mediaImageResizeConcurrency)

var mediaImageBeforeSemAcquire func()

// mediaThumbCachePointer is the process-wide thumbnail disk cache stored as an
// atomic pointer so concurrent StartWithReady calls in tests do not race on it.
// mediaThumbCacheGeneration only supplies a unique suffix for the directories
// moved aside during a runtime wipe; it does not affect the live directory name.
var (
	mediaThumbCachePointer    atomic.Pointer[mediaThumbCache]
	mediaThumbCacheGeneration atomic.Uint64
)

// mediaThumbCache persists resized cover images to disk so that the expensive
// decode+bilinear-resize+encode is only performed once per unique
// (identity, imageType, maxSize) triple. The cache survives core restarts and
// reboots; it is wiped entirely when a scrape completes (scraping replaces
// image sources under a stable cache key) and when a media reindex completes
// (insurance against id:DBID-keyed entries colliding if the MediaDB is reset).
//
// The live directory is thumbs/v<mediaThumbCacheVersion> — deterministic across
// restarts. On start, reapStaleVersions removes any sibling that is not the
// current version, so a version bump invalidates the cache and any directory
// abandoned by a crash mid-wipe (v<N>.stale<gen>) or an older build is reaped.
//
// Each system has its own encoded-name directory for targeted invalidation.
// Keys are SHA-256 hashes of the logical identity string; values are stored as
// <system>/<hash>.<ext> to encode the content-type implicitly.
//
// resolvedTypes maps an early key (identity+prefs+maxSize) to its system and
// resolved typeTag. It enables a pre-semaphore cache hit that skips the entire
// image-load pipeline for warm repeat requests.
type resolvedThumb struct {
	typeTag string
	system  string
}

type mediaThumbCache struct {
	createdAt     time.Time
	fs            afero.Fs
	resolvedTypes map[string]resolvedThumb
	resizeSem     chan struct{}
	reapDone      chan struct{}
	dir           string
	resolvedMu    syncutil.RWMutex
}

// resizeSemaphore returns the channel bounding concurrent resize work.
func (c *mediaThumbCache) resizeSemaphore() chan struct{} {
	if c == nil || c.resizeSem == nil {
		return defaultMediaImageResizeSem
	}
	return c.resizeSem
}

// mediaThumbCacheVersionDir is the basename of the live cache directory for the
// current encode scheme version.
func mediaThumbCacheVersionDir() string {
	return fmt.Sprintf("v%d", mediaThumbCacheVersion)
}

// mediaThumbSizeTiers are the standard thumbnail sizes a requested max_size is
// snapped onto. Each distinct cached size is a separate cold resize per cover
// (read source off slow storage, decode, resize, encode) and a separate entry
// competing for the client's image cache, so the number of variants per cover
// must stay bounded — but a single hardcoded size throws away the resolution
// information clients legitimately have (CRT vs grid vs list vs detail, display
// density, grid-size preferences). The ladder is the middle ground: clients
// request their true ideal size and it snaps up to the nearest tier.
//
// Powers of two are the conventional thumbnail-cache ladder. The 32/64 tiers
// support cheap low-quality placeholders on software-rendered displays. Steps
// are denser at the large end (768 = 512×1.5) because wasted bytes grow with
// size, and the ladder tops out near the ~680px native cover dimension — larger
// requests would only upscale. 128 ≈ list/CRT, 256 ≈ small grid, 512 ≈ grid @2x
// / detail @1x, 768 ≈ detail @2x. Keep ascending.
var mediaThumbSizeTiers = []int32{32, 64, 128, 256, 512, 768}

// snapThumbMaxSize maps a requested max_size to the smallest standard tier that
// is >= the request, capping at the largest tier. This bounds the number of
// cached variants per cover and enforces an upper bound on the resize dimension
// (a huge request is snapped down before any decode/allocation). A non-positive
// request means "full size" and is returned unchanged so no resize is performed.
func snapThumbMaxSize(requested int32) int32 {
	if requested <= 0 {
		return requested
	}
	for _, tier := range mediaThumbSizeTiers {
		if requested <= tier {
			return tier
		}
	}
	return mediaThumbSizeTiers[len(mediaThumbSizeTiers)-1]
}

func newMediaThumbCache(pl platforms.Platform) *mediaThumbCache {
	return newMediaThumbCacheWithFS(pl, nil)
}

func newMediaThumbCacheWithFS(pl platforms.Platform, fs afero.Fs) *mediaThumbCache {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir := filepath.Join(helpers.DataDir(pl), config.CacheDir, mediaThumbCacheDirName, mediaThumbCacheVersionDir())
	concurrency := mediaImageResizeConcurrency
	if pl != nil && pl.Settings().ResourceConstrained {
		concurrency = mediaImageResizeConcurrencyConstrained
	}
	return &mediaThumbCache{
		fs:            fs,
		dir:           dir,
		resolvedTypes: make(map[string]resolvedThumb),
		resizeSem:     make(chan struct{}, concurrency),
		createdAt:     time.Now(),
	}
}

// reapStaleVersions ensures the live versioned directory exists and removes
// every sibling under thumbs/ that is not the current version. This invalidates
// the cache after a mediaThumbCacheVersion bump and reaps directories left by a
// crash mid-wipe or by an older build (the previous current/ and gen-N layout).
//
// It runs in the background while requests are served, so it only removes
// temporary files older than the cache itself: a temporary file written by a
// live request is left to its own rename or cleanup. Temporary files are never
// served either way, because lookups only match final hashed names.
func (c *mediaThumbCache) reapStaleVersions() {
	if err := c.fs.MkdirAll(c.dir, 0o750); err != nil { //nolint:gosec // cache dir, 0o750 is intentional
		log.Debug().Err(err).Str("dir", c.dir).Msg("media.image: thumb cache: failed to create dir")
		return
	}
	parent := filepath.Dir(c.dir)
	live := filepath.Base(c.dir)
	entries, err := afero.ReadDir(c.fs, parent)
	if err != nil {
		log.Debug().Err(err).Str("dir", parent).Msg("media.image: thumb cache: failed to read parent for reap")
		return
	}
	for _, entry := range entries {
		if entry.Name() == live {
			continue
		}
		stale := filepath.Join(parent, entry.Name())
		if err := c.fs.RemoveAll(stale); err != nil {
			log.Warn().Err(err).Str("dir", stale).Msg("media.image: thumb cache: failed to reap stale dir")
		} else {
			log.Debug().Str("dir", stale).Msg("media.image: thumb cache: reaped stale dir")
		}
	}
	if err := afero.Walk(c.fs, c.dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasPrefix(info.Name(), ".thumb-") || !strings.HasSuffix(info.Name(), ".tmp") {
			return nil
		}
		if !c.createdAt.IsZero() && !info.ModTime().Before(c.createdAt) {
			return nil
		}
		if err := c.fs.Remove(path); err != nil {
			log.Warn().Err(err).Str("path", path).
				Msg("media.image: thumb cache: failed to reap temporary file")
		}
		return nil
	}); err != nil {
		log.Warn().Err(err).Str("dir", c.dir).Msg("media.image: thumb cache: failed to reap temporary files")
	}
}

// earlyThumbKey returns a cache key derived only from request-time parameters
// (no typeTag needed). Used for the pre-semaphore resolved-type memo.
func earlyThumbKey(ref mediaRefParam, prefs []string, maxSize int) string {
	var identity string
	if ref.MediaID != nil {
		identity = fmt.Sprintf("id:%d", *ref.MediaID)
	} else {
		identity = fmt.Sprintf("path:%s:%s", ref.System, filepath.ToSlash(ref.Path))
	}
	return fmt.Sprintf("early:%s:[%s]:%d", identity, strings.Join(prefs, ","), maxSize)
}

// getResolvedThumb returns the previously resolved system and typeTag for this
// request, or false when not yet memoized.
func (c *mediaThumbCache) getResolvedThumb(
	ref mediaRefParam, prefs []string, maxSize int,
) (resolvedThumb, bool) {
	c.resolvedMu.RLock()
	defer c.resolvedMu.RUnlock()
	resolved, ok := c.resolvedTypes[earlyThumbKey(ref, prefs, maxSize)]
	return resolved, ok
}

// setResolvedThumb records the resolved system and typeTag for future
// pre-semaphore hits.
func (c *mediaThumbCache) setResolvedThumb(
	ref mediaRefParam, prefs []string, maxSize int, system, typeTag string,
) {
	c.resolvedMu.Lock()
	defer c.resolvedMu.Unlock()
	c.resolvedTypes[earlyThumbKey(ref, prefs, maxSize)] = resolvedThumb{system: system, typeTag: typeTag}
}

func (c *mediaThumbCache) clearResolvedSystems(systems map[string]struct{}) {
	c.resolvedMu.Lock()
	defer c.resolvedMu.Unlock()
	for key, resolved := range c.resolvedTypes {
		if _, ok := systems[resolved.system]; ok {
			delete(c.resolvedTypes, key)
		}
	}
}

// thumbKey returns the cache key string for a resize request. The key encodes
// the stable identity (media-ID or system+path) plus the resolved image-type tag
// and target size. Media IDs are reused across an in-place reindex, but could be
// reassigned if the MediaDB is reset; the reindex wipe covers that case, so they
// are safe to use here.
func thumbKey(ref mediaRefParam, typeTag string, maxSize int) string {
	var identity string
	if ref.MediaID != nil {
		identity = fmt.Sprintf("id:%d", *ref.MediaID)
	} else {
		identity = fmt.Sprintf("path:%s:%s", ref.System, filepath.ToSlash(ref.Path))
	}
	return fmt.Sprintf("%s:%s:%d", identity, typeTag, maxSize)
}

// hashThumbKey converts a key string to the 64-character hex filename prefix
// used on disk.
func hashThumbKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func thumbSystemDirName(system string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(system))
}

func (c *mediaThumbCache) systemDir(system string) string {
	return filepath.Join(c.dir, thumbSystemDirName(system))
}

type thumbCacheFormat struct {
	ext         string
	contentType string
}

var thumbCacheFormats = []thumbCacheFormat{
	{ext: ".jpg", contentType: "image/jpeg"},
	{ext: ".png", contentType: "image/png"},
	{ext: ".gif", contentType: "image/gif"},
	{ext: ".webp", contentType: "image/webp"},
}

func thumbCacheExtension(contentType string, data []byte) string {
	ext := extensionFromContentType(contentType)
	if ext == "" && len(data) > 0 {
		ext = extensionFromContentType(http.DetectContentType(data))
	}
	for _, format := range thumbCacheFormats {
		if strings.TrimPrefix(format.ext, ".") == ext {
			return format.ext
		}
	}
	return ""
}

func thumbCacheFileInfo(fs afero.Fs, path string) (os.FileInfo, error) {
	if lstater, ok := fs.(afero.Lstater); ok {
		info, _, err := lstater.LstatIfPossible(path)
		if err != nil {
			return nil, fmt.Errorf("lstat thumbnail cache file: %w", err)
		}
		return info, nil
	}
	info, err := fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat thumbnail cache file: %w", err)
	}
	return info, nil
}

// lookupPath returns a regular cached thumbnail path without reading its bytes.
func (c *mediaThumbCache) lookupPath(
	ref mediaRefParam, system, typeTag string, maxSize int,
) (path, contentType string, found bool) {
	hash := hashThumbKey(thumbKey(ref, typeTag, maxSize))
	for _, format := range thumbCacheFormats {
		//nolint:gosec // path is constructed from controlled dirs + SHA-256 hash + fixed extension
		candidate := filepath.Join(c.systemDir(system), hash+format.ext)
		info, err := thumbCacheFileInfo(c.fs, candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate, format.contentType, true
		}
	}
	return "", "", false
}

// read returns cached thumbnail bytes. A deletion between lookup and read is a miss.
func (c *mediaThumbCache) read(
	ref mediaRefParam, system, typeTag string, maxSize int,
) (data []byte, contentType string, found bool) {
	path, contentType, found := c.lookupPath(ref, system, typeTag, maxSize)
	if !found {
		return nil, "", false
	}
	//nolint:gosec // lookupPath only returns controlled cache paths.
	data, err := afero.ReadFile(c.fs, path)
	if err != nil {
		return nil, "", false
	}
	return data, contentType, true
}

// get keeps cache-focused tests concise; production response paths use lookupPath or read explicitly.
func (c *mediaThumbCache) get(
	ref mediaRefParam, system, typeTag string, maxSize int,
) (data []byte, contentType string, found bool) {
	return c.read(ref, system, typeTag, maxSize)
}

// isSafeLocalPath proves a returned cache path is absolute, contained by the
// versioned thumbnail cache, and still names a regular non-symlink file.
func (c *mediaThumbCache) isSafeLocalPath(path string) bool {
	cleanDir := filepath.Clean(c.dir)
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanDir) || !filepath.IsAbs(cleanPath) {
		return false
	}
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	info, err := thumbCacheFileInfo(c.fs, cleanPath)
	return err == nil && info.Mode().IsRegular()
}

// set writes resized image bytes through a same-directory temporary file and
// atomic rename. The resulting path is returned only after materialization.
func (c *mediaThumbCache) set(
	ref mediaRefParam, system, typeTag string, maxSize int, data []byte, contentType string,
) (string, error) {
	ext := thumbCacheExtension(contentType, data)
	if ext == "" {
		return "", fmt.Errorf("unsupported thumbnail content type %q", contentType)
	}
	dir := c.systemDir(system)
	if err := c.fs.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // cache dir, 0o750 is intentional
		return "", fmt.Errorf("create thumbnail cache directory: %w", err)
	}
	hash := hashThumbKey(thumbKey(ref, typeTag, maxSize))
	path := filepath.Join(dir, hash+ext)
	tmp, err := afero.TempFile(c.fs, dir, ".thumb-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create thumbnail cache temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = c.fs.Remove(tmpPath)
	}()
	if err := c.fs.Chmod(tmpPath, 0o600); err != nil { //nolint:gosec // cache file, 0o600 is intentional
		return "", fmt.Errorf("set thumbnail cache file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return "", fmt.Errorf("write thumbnail cache temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close thumbnail cache temporary file: %w", err)
	}
	if err := c.fs.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("replace thumbnail cache file: %w", err)
	}
	return path, nil
}

// wipe empties the thumbnail cache and clears the in-memory resolved-type memo.
// The live directory name (v<N>) is kept stable so it stays deterministic across
// restarts: the current directory is renamed aside to v<N>.stale<gen> and a
// fresh empty v<N> is created immediately, then the moved-aside copy is removed.
// In-flight image requests keep reading/writing the stable v<N> path. Called
// after a successful scrape or media reindex so stale entries do not accumulate.
func (c *mediaThumbCache) wipe() {
	generation := mediaThumbCacheGeneration.Add(1)
	staleDir := fmt.Sprintf("%s.stale%d", c.dir, generation)
	if _, err := c.fs.Stat(c.dir); err == nil {
		if renameErr := c.fs.Rename(c.dir, staleDir); renameErr != nil {
			// Rename failed (e.g. memmap edge case) — fall back to a direct,
			// blocking removal of the live directory.
			log.Warn().Err(renameErr).Str("dir", c.dir).
				Msg("media.image: thumb cache: rename for wipe failed, removing in place")
			if rmErr := c.fs.RemoveAll(c.dir); rmErr != nil {
				log.Warn().Err(rmErr).Str("dir", c.dir).Msg("media.image: failed to wipe thumb cache")
			}
			staleDir = ""
		}
	} else {
		staleDir = ""
	}
	if err := c.fs.MkdirAll(c.dir, 0o750); err != nil { //nolint:gosec // cache dir, 0o750 is intentional
		log.Debug().Err(err).Str("dir", c.dir).Msg("media.image: thumb cache: failed to recreate dir after wipe")
	}
	if staleDir != "" {
		if err := c.fs.RemoveAll(staleDir); err != nil {
			log.Warn().Err(err).Str("dir", staleDir).Msg("media.image: failed to remove stale thumb cache")
		}
	}
	log.Info().Str("dir", c.dir).Msg("media.image: thumb cache wiped")
	c.resolvedMu.Lock()
	c.resolvedTypes = make(map[string]resolvedThumb)
	c.resolvedMu.Unlock()
}

func (c *mediaThumbCache) wipeSystems(systemIDs []string) {
	systems := make(map[string]struct{}, len(systemIDs))
	for _, systemID := range systemIDs {
		if systemID == "" {
			continue
		}
		systems[systemID] = struct{}{}
	}
	for systemID := range systems {
		dir := c.systemDir(systemID)
		if err := c.fs.RemoveAll(dir); err != nil {
			log.Warn().Err(err).Str("dir", dir).Str("system", systemID).
				Msg("media.image: failed to wipe system thumb cache")
		} else {
			log.Info().Str("dir", dir).Str("system", systemID).
				Msg("media.image: system thumb cache wiped")
		}
	}
	c.clearResolvedSystems(systems)
}

// InitMediaThumbCache creates or replaces the process-wide thumb cache for the
// given platform. Must be called once during service start, before any
// media.image requests are handled. The cache is usable as soon as this
// returns; stale-version directories and abandoned temporary files are reaped
// in the background so a large cache does not delay startup. Uses atomic store
// so concurrent StartWithReady calls in tests do not race.
func InitMediaThumbCache(pl platforms.Platform) {
	cache := newMediaThumbCache(pl)
	if err := cache.fs.MkdirAll(cache.dir, 0o750); err != nil { //nolint:gosec // cache dir, 0o750 is intentional
		log.Debug().Err(err).Str("dir", cache.dir).Msg("media.image: thumb cache: failed to create dir")
	}
	cache.reapDone = make(chan struct{})
	mediaThumbCachePointer.Store(cache)
	go func() {
		defer close(cache.reapDone)
		cache.reapStaleVersions()
	}()
}

// WipeMediaThumbCache removes all cached thumbnails and, when mediaDB is not
// nil, every recorded cover thumbnail row. Called after a successful scrape
// (image sources may have changed) or media reindex (media IDs may change) so
// neither the cache nor its records serve stale art.
func WipeMediaThumbCache(mediaDB database.MediaDBI) {
	if cache := mediaThumbCachePointer.Load(); cache != nil {
		cache.wipe()
	}
	if mediaDB != nil {
		if err := mediaDB.ClearMediaCoverThumbs(context.Background()); err != nil {
			log.Warn().Err(err).Msg("media.image: failed to clear cover thumbnail records")
		}
	}
}

// WipeMediaThumbCacheSystems removes cached thumbnails and recorded cover
// thumbnail rows only for the supplied systems. Other system directories and
// resolved request memos remain warm.
func WipeMediaThumbCacheSystems(mediaDB database.MediaDBI, systemIDs []string) {
	if cache := mediaThumbCachePointer.Load(); cache != nil {
		cache.wipeSystems(systemIDs)
	}
	if mediaDB != nil && len(systemIDs) > 0 {
		if err := mediaDB.ClearMediaCoverThumbsForSystems(context.Background(), systemIDs); err != nil {
			log.Warn().Err(err).Strs("systems", systemIDs).
				Msg("media.image: failed to clear cover thumbnail records for systems")
		}
	}
}

// defaultImageTypes is the preference order used when no imageTypes param is provided.
var defaultImageTypes = []string{
	"image", "thumbnail", "boxart", "boxart3d", "screenshot", "wheel", "titleshot", "map",
	"marquee", "fanart",
}

var imageTypeTags = map[string]string{
	"image":      tags.PropertyTypeTag(tags.TagPropertyImageImage),
	"thumbnail":  tags.PropertyTypeTag(tags.TagPropertyImageThumbnail),
	"boxart":     tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
	"boxart3d":   tags.PropertyTypeTag(tags.TagPropertyImageBoxart3D),
	"boxartside": tags.PropertyTypeTag(tags.TagPropertyImageBoxartSide),
	"boxartback": tags.PropertyTypeTag(tags.TagPropertyImageBoxartBack),
	"screenshot": tags.PropertyTypeTag(tags.TagPropertyImageScreenshot),
	"wheel":      tags.PropertyTypeTag(tags.TagPropertyImageWheel),
	"titleshot":  tags.PropertyTypeTag(tags.TagPropertyImageTitleshot),
	"map":        tags.PropertyTypeTag(tags.TagPropertyImageMap),
	"marquee":    tags.PropertyTypeTag(tags.TagPropertyImageMarquee),
	"fanart":     tags.PropertyTypeTag(tags.TagPropertyImageFanart),
}

// rawMediaImage carries raw image bytes and metadata before base64-encoding.
// Used to allow the semaphore to be released before CPU-intensive resize/encode.
type rawMediaImage struct {
	contentType string
	text        string // original file path, for extension derivation fallback
	typeTag     string
	system      string
	binary      []byte
	mediaDBID   int64 // 0 when the image belongs to a directory, not a media row
}

// thumbPassthroughMaxBytes is the largest source that is returned as it is
// when it already fits the requested box, instead of being re-encoded as
// WebP. On a MiSTer (ARM32) the encode was 300 to 345 ms of a 0.33 to 0.38 s
// request for a 320x320 JPEG of 10 to 29 KB, to save 4 to 7 KB: at this size
// the format win is a few kilobytes and the encode is nearly the whole cost.
const thumbPassthroughMaxBytes = 64 << 10

// resizeImageIfNeeded decodes binary, scales it down to fit within a
// maxSize×maxSize bounding box when either dimension exceeds maxSize, and
// re-encodes the result as lossy WebP with alpha preserved.
//
// A source that already fits is not scaled. If it is also no larger than
// thumbPassthroughMaxBytes and in a format the thumbnail cache stores (JPEG,
// PNG, WebP), the original bytes and content type are returned without an
// encode. A larger source that fits is still re-encoded as WebP (keeping
// native dimensions) so callers requesting a tier at or above native size get
// the format win, unless the WebP would be no smaller, in which case the
// original bytes are kept.
//
// Returns the original bytes unchanged when maxSize <= 0 (full size requested)
// or when the source cannot be decoded.
func resizeImageIfNeeded(binary []byte, contentType string, maxSize int) (resized []byte, resizedType string) {
	resized, resizedType, _ = resizeImageWithColor(binary, contentType, maxSize)
	return resized, resizedType
}

// resizeImageWithColor is resizeImageIfNeeded that also returns the average
// colour of the decoded frame as 0xRRGGBB, or nil when the image was not
// decoded or is fully transparent. The colour is computed whether or not the
// frame goes on to be encoded.
func resizeImageWithColor(
	binary []byte, contentType string, maxSize int,
) (resized []byte, resizedType string, avgColor *uint32) {
	if maxSize <= 0 || len(binary) == 0 {
		return binary, contentType, nil
	}
	src, err := decodeResizableImage(binary, contentType)
	if err != nil {
		return binary, contentType, nil
	}
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	// Downscale to fit the maxSize box when larger; otherwise keep native
	// dimensions. A frame that was scaled, or a source too large to pass
	// through, is re-encoded as WebP below: a request that snaps to a tier at or
	// above the native size still gets the format win instead of falling back
	// to the original (often large lossless PNG).
	frame := src
	newW, newH := w, h
	if w > maxSize || h > maxSize {
		larger := w
		if h > larger {
			larger = h
		}
		scale := float64(maxSize) / float64(larger)
		newW = int(math.Round(float64(w) * scale))
		newH = int(math.Round(float64(h) * scale))
		if newW < 1 {
			newW = 1
		}
		if newH < 1 {
			newH = 1
		}
		dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
		frame = dst
	}
	avgColor = averageImageColor(frame)
	// A small source that already fits skips the encode; see
	// thumbPassthroughMaxBytes. The colour still comes from the decoded frame.
	// The cache names its files by format, so a source it could not store keeps
	// going through the encode rather than missing the cache on every request.
	if newW == w && newH == h && len(binary) <= thumbPassthroughMaxBytes &&
		thumbCacheExtension(contentType, binary) != "" {
		return binary, contentType, avgColor
	}
	out, outputType, err := encodeResizedImage(frame)
	if err != nil {
		return binary, contentType, avgColor
	}
	// When no downscale happened, only adopt the WebP if it actually shrank the
	// payload; some already-compact sources re-encode larger.
	if newW == w && newH == h && len(out) >= len(binary) {
		return binary, contentType, avgColor
	}
	log.Debug().
		Str("contentType", contentType).
		Str("resizedContentType", outputType).
		Int("maxSize", maxSize).
		Int("width", w).
		Int("height", h).
		Int("resizedWidth", newW).
		Int("resizedHeight", newH).
		Int("inputBytes", len(binary)).
		Int("resizedBytes", len(out)).
		Msg("media.image: resized image")
	return out, outputType, avgColor
}

// averageColorSampleSide bounds the colour average to about this many samples
// per axis, so the cost is independent of the frame size.
const averageColorSampleSide = 64

// averageImageColor returns the alpha-weighted average colour of img as
// 0xRRGGBB, sampling at most averageColorSampleSide² pixels. It returns nil
// for an empty or fully transparent image.
func averageImageColor(img image.Image) *uint32 {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	stepX := max(1, w/averageColorSampleSide)
	stepY := max(1, h/averageColorSampleSide)
	var sumR, sumG, sumB, sumA uint64
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			// RGBA returns alpha-premultiplied 16-bit channels, so the sums are
			// already weighted by coverage.
			r, g, b, a := img.At(x, y).RGBA()
			sumR += uint64(r)
			sumG += uint64(g)
			sumB += uint64(b)
			sumA += uint64(a)
		}
	}
	if sumA == 0 {
		return nil
	}
	channel := func(sum uint64) uint32 {
		v := sum * 0xff / sumA
		if v > 0xff {
			v = 0xff
		}
		return uint32(v) //nolint:gosec // clamped to 8 bits above
	}
	value := channel(sumR)<<16 | channel(sumG)<<8 | channel(sumB)
	return &value
}

// encodeResizedImage encodes a resized frame as lossy WebP (VP8) with alpha
// preserved. Isolated in one function so the encoder can be swapped without
// touching the resize pipeline.
func encodeResizedImage(img image.Image) (data []byte, contentType string, err error) {
	var out bytes.Buffer
	if encErr := gowebp.Encode(&out, img, &gowebp.Options{
		Lossy:   true,
		Quality: mediaThumbWebPQuality,
		Method:  mediaThumbWebPMethod,
	}); encErr != nil {
		return nil, "", fmt.Errorf("encode resized webp: %w", encErr)
	}
	return out.Bytes(), "image/webp", nil
}

func decodeResizableImage(binary []byte, contentType string) (image.Image, error) {
	decodeJPEG := func() (image.Image, error) {
		img, err := jpeg.Decode(bytes.NewReader(binary))
		if err != nil {
			return nil, fmt.Errorf("decode JPEG for resize: %w", err)
		}
		return img, nil
	}
	decodePNG := func() (image.Image, error) {
		img, err := png.Decode(bytes.NewReader(binary))
		if err != nil {
			return nil, fmt.Errorf("decode PNG for resize: %w", err)
		}
		return img, nil
	}
	decodeWebP := func() (image.Image, error) {
		img, err := webp.Decode(bytes.NewReader(binary))
		if err != nil {
			return nil, fmt.Errorf("decode WebP for resize: %w", err)
		}
		return img, nil
	}

	switch extensionFromContentType(contentType) {
	case "jpg":
		return decodeJPEG()
	case "png":
		return decodePNG()
	case "webp":
		return decodeWebP()
	}

	if bytes.HasPrefix(binary, []byte("\x89PNG\r\n\x1a\n")) {
		return decodePNG()
	}
	if len(binary) >= 2 && binary[0] == 0xff && binary[1] == 0xd8 {
		return decodeJPEG()
	}
	if len(binary) >= 12 && bytes.HasPrefix(binary, []byte("RIFF")) && bytes.Equal(binary[8:12], []byte("WEBP")) {
		return decodeWebP()
	}
	return nil, fmt.Errorf("unsupported image type for resize: %s", contentType)
}

// imageHasTransparency reports whether img contains any non-opaque pixel.
// It short-circuits for common opaque types before falling back to a
// pixel-by-pixel scan — avoiding O(w×h) work on ARM for JPEG sources.
func imageHasTransparency(img image.Image) bool {
	// Types that are always fully opaque — no alpha channel possible.
	switch img.(type) {
	case *image.YCbCr, *image.Gray, *image.Gray16, *image.CMYK:
		return false
	}
	// Types that have an Opaque() helper — use it to avoid a pixel scan when
	// the image was decoded with a known-opaque palette or alpha mask.
	type opaque interface{ Opaque() bool }
	if o, ok := img.(opaque); ok {
		return !o.Opaque()
	}
	// Fallback: per-pixel scan for any remaining type (e.g. *image.Paletted).
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha != 0xffff {
				return true
			}
		}
	}
	return false
}

type mediaBinaryTooLargeError struct {
	path string
	size int64
	max  int64
}

type mediaImageSource struct {
	propMap map[string]database.MediaProperty
	isMedia bool
}

type mediaImageNotFoundError struct {
	system                      string
	path                        string
	fileBackedCandidatesChecked bool
}

type mediaImageNoImageCache struct {
	entries map[string]error
	mu      syncutil.Mutex
}

var mediaImageNoImages = &mediaImageNoImageCache{entries: make(map[string]error)}

func (e *mediaBinaryTooLargeError) Error() string {
	return fmt.Sprintf("media binary %q is too large: %d bytes exceeds %d byte limit", e.path, e.size, e.max)
}

func (e *mediaImageNotFoundError) Error() string {
	return fmt.Sprintf("no image found for media: system=%q path=%q", e.system, filepath.ToSlash(e.path))
}

func mediaImageNoImageMediaIDKey(mediaID int64, prefs []string) string {
	return fmt.Sprintf("id:%d\x00%s", mediaID, strings.Join(prefs, "\x00"))
}

func mediaImageNoImagePathKey(system, path string, prefs []string) string {
	return fmt.Sprintf("path:%s\x00%s\x00%s", system, filepath.ToSlash(path), strings.Join(prefs, "\x00"))
}

func mediaImageNoImageRequestKey(ref mediaRefParam, prefs []string) string {
	if ref.MediaID != nil {
		return mediaImageNoImageMediaIDKey(*ref.MediaID, prefs)
	}
	return mediaImageNoImagePathKey(ref.System, ref.Path, prefs)
}

func (c *mediaImageNoImageCache) get(key string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	err, ok := c.entries[key]
	return ok, err
}

func (c *mediaImageNoImageCache) add(key string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= mediaImageNoImageMax {
		for existing := range c.entries {
			delete(c.entries, existing)
			break
		}
	}

	var noImage *mediaImageNotFoundError
	if errors.As(err, &noImage) {
		c.entries[key] = noImage
		return
	}
	c.entries[key] = err
}

func (c *mediaImageNoImageCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]error)
}

func mediaImageNoImageError(row *database.MediaFullRow, fileBackedCandidatesChecked bool) error {
	noImage := &mediaImageNotFoundError{
		system:                      row.System.SystemID,
		path:                        row.Path,
		fileBackedCandidatesChecked: fileBackedCandidatesChecked,
	}
	return fmt.Errorf("%w", models.QuietClientErr(noImage))
}

func cachedMediaImageNoImageError(err error) error {
	return fmt.Errorf("%w", models.QuietClientErr(err))
}

func isCacheableMediaImageNoImageError(err error) bool {
	var noImage *mediaImageNotFoundError
	return errors.As(err, &noImage) && !noImage.fileBackedCandidatesChecked
}

// resolveImageTypeTag converts a short image type name to the full property TypeTag.
func resolveImageTypeTag(t string) (string, bool) {
	typeTag, ok := imageTypeTags[t]
	return typeTag, ok
}

// buildPropsMap converts a []database.MediaProperty slice into a map keyed by TypeTag.
func buildPropsMap(props []database.MediaProperty) map[string]database.MediaProperty {
	m := make(map[string]database.MediaProperty, len(props))
	for _, p := range props {
		m[p.TypeTag] = p
	}
	return m
}

func imagePropertyTypeTags(props []database.MediaProperty) []string {
	known := make(map[string]struct{}, len(imageTypeTags))
	for _, typeTag := range imageTypeTags {
		known[typeTag] = struct{}{}
	}

	seen := make(map[string]struct{})
	for _, p := range props {
		if _, ok := known[p.TypeTag]; ok {
			seen[p.TypeTag] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for typeTag := range seen {
		result = append(result, typeTag)
	}
	sort.Strings(result)
	return result
}

func inlineMediaImageResponse(data []byte, contentType, sourcePath, typeTag string) models.MediaImageResponse {
	return models.MediaImageResponse{
		Extension:   mediaContentExtension(contentType, sourcePath),
		ContentType: contentType,
		Data:        base64.StdEncoding.EncodeToString(data),
		Delivery:    mediaImageDeliveryInline,
		TypeTag:     typeTag,
	}
}

func localMediaImageResponse(
	cache *mediaThumbCache, path, contentType, typeTag string,
) (models.MediaImageResponse, bool) {
	if !cache.isSafeLocalPath(path) {
		return models.MediaImageResponse{}, false
	}
	return models.MediaImageResponse{
		Extension:   mediaContentExtension(contentType, ""),
		ContentType: contentType,
		Delivery:    mediaImageDeliveryPath,
		LocalPath:   path,
		TypeTag:     typeTag,
	}, true
}

func cachedMediaImageResponse(
	cache *mediaThumbCache,
	ref mediaRefParam,
	system string,
	typeTag string,
	maxSize int,
	sourcePath string,
	localPath bool,
) (models.MediaImageResponse, bool) {
	path, contentType, found := cache.lookupPath(ref, system, typeTag, maxSize)
	if !found {
		return models.MediaImageResponse{}, false
	}
	if localPath {
		if response, ok := localMediaImageResponse(cache, path, contentType, typeTag); ok {
			return response, true
		}
	}
	data, err := afero.ReadFile(cache.fs, path)
	if err != nil {
		return models.MediaImageResponse{}, false
	}
	return inlineMediaImageResponse(data, contentType, sourcePath, typeTag), true
}

func validateMediaImageDelivery(delivery string, ref mediaRefParam) error {
	switch delivery {
	case "", mediaImageDeliveryInline:
		return nil
	case mediaImageDeliveryPath:
		if ref.MaxSize == nil || *ref.MaxSize <= 0 {
			return models.ClientErrf("media.image: localPath delivery requires a positive maxSize")
		}
		return nil
	default:
		return models.ClientErrf("media.image: unsupported delivery %q", delivery)
	}
}

// HandleMediaImage returns a single best-match image inline or as a transient,
// Core-owned cached thumbnail path when explicitly requested.
//
//nolint:gocritic // RequestEnv is copied once at the API handler boundary.
func HandleMediaImage(env requests.RequestEnv) (result any, resultErr error) {
	started := time.Now()
	deliveryForLog := ""
	maxSizeForLog := 0
	hasMediaID := false
	defer func() {
		log.Debug().
			Dur("duration", time.Since(started)).
			Str("delivery", deliveryForLog).
			Int("maxSize", maxSizeForLog).
			Bool("mediaId", hasMediaID).
			Bool("ok", resultErr == nil).
			Msg("media.image handler timing")
	}()

	ref, delivery, err := parseMediaImageRequest(env.Params)
	if err != nil {
		return nil, err
	}
	deliveryForLog = delivery
	hasMediaID = ref.MediaID != nil
	if deliveryErr := validateMediaImageDelivery(delivery, ref); deliveryErr != nil {
		return nil, deliveryErr
	}
	localPath := delivery == mediaImageDeliveryPath

	// Snap the requested size onto a standard tier so every view shares one
	// cached image per cover and the resize dimension is bounded. All downstream
	// uses (cache keys, resolved-type memo, resize) see the snapped value.
	if ref.MaxSize != nil {
		snapped := snapThumbMaxSize(*ref.MaxSize)
		ref.MaxSize = &snapped
		maxSizeForLog = int(snapped)
	}
	prefs := imagePrefs(nil, ref.ImageTypes)

	// Pre-semaphore fast path: if this exact request was served before and the
	// disk thumbnail is still present, return it without loading the original.
	if ref.MaxSize != nil && *ref.MaxSize > 0 {
		maxSize := int(*ref.MaxSize)
		if cache := mediaThumbCachePointer.Load(); cache != nil {
			if resolved, ok := cache.getResolvedThumb(ref, prefs, maxSize); ok {
				if response, found := cachedMediaImageResponse(
					cache, ref, resolved.system, resolved.typeTag, maxSize, "", localPath,
				); found {
					return response, nil
				}
			}
		}
	}

	noImageKey := mediaImageNoImageRequestKey(ref, prefs)
	if ok, cachedErr := mediaImageNoImages.get(noImageKey); ok {
		return nil, cachedMediaImageNoImageError(cachedErr)
	}

	// Recorded fast path: after a restart the in-memory memo is empty, but the
	// cover's resolved type is recorded in MediaDB. Confirm it is still the
	// first match for these preferences and serve the disk thumbnail without
	// reading the original artwork or waiting for the lookup semaphore.
	if ref.MaxSize != nil && *ref.MaxSize > 0 {
		if cache := mediaThumbCachePointer.Load(); cache != nil {
			if response, found := recordedThumbResponse(
				&env, cache, ref, prefs, int(*ref.MaxSize), localPath,
			); found {
				return response, nil
			}
		}
	}

	if mediaImageBeforeSemAcquire != nil {
		mediaImageBeforeSemAcquire()
	}

	select {
	case mediaImageSem <- struct{}{}:
	case <-env.Context.Done():
		return nil, env.Context.Err()
	}
	if ok, cachedErr := mediaImageNoImages.get(noImageKey); ok {
		<-mediaImageSem
		return nil, cachedMediaImageNoImageError(cachedErr)
	}

	maxBytes := mediaImageMaxBytes(env.Platform)
	var raw *rawMediaImage
	if ref.MediaID == nil {
		raw, err = loadRawMediaImageSinglePath(&env, ref, prefs, maxBytes)
	} else {
		raw, err = loadRawMediaImageByID(&env, ref, prefs, maxBytes)
	}
	// Cache the no-image result while holding the semaphore so concurrent
	// waiters see it immediately after sem release.
	if isCacheableMediaImageNoImageError(err) {
		mediaImageNoImages.add(noImageKey, err)
	}
	<-mediaImageSem // release before CPU-intensive decode/resize/encode

	if err != nil {
		return nil, err
	}

	binary, contentType := raw.binary, raw.contentType
	if ref.MaxSize != nil && *ref.MaxSize > 0 {
		maxSize := int(*ref.MaxSize)
		cache := mediaThumbCachePointer.Load()
		// Record the resolved typeTag so the pre-semaphore path can find the
		// disk file on the next request without loading the original image.
		if cache != nil {
			cache.setResolvedThumb(ref, prefs, maxSize, raw.system, raw.typeTag)
			if response, found := cachedMediaImageResponse(
				cache, ref, raw.system, raw.typeTag, maxSize, raw.text, localPath,
			); found {
				recordCachedCoverThumb(&env, cache, ref, raw, prefs, maxSize)
				return response, nil
			}
		}

		resizeSem := cache.resizeSemaphore()
		select {
		case resizeSem <- struct{}{}:
		case <-env.Context.Done():
			return nil, env.Context.Err()
		}
		var avgColor *uint32
		binary, contentType, avgColor = resizeImageWithColor(binary, contentType, maxSize)
		<-resizeSem
		if cache != nil {
			path, cacheErr := cache.set(ref, raw.system, raw.typeTag, maxSize, binary, contentType)
			if cacheErr != nil {
				log.Debug().Err(cacheErr).Msg("media.image: failed to materialize thumbnail cache file")
			} else {
				recordCoverThumb(&env, raw, prefs, avgColor)
			}
			if cacheErr == nil && localPath {
				if response, ok := localMediaImageResponse(cache, path, contentType, raw.typeTag); ok {
					return response, nil
				}
				log.Debug().Msg("media.image: thumbnail cache path failed local delivery validation")
			}
		}
	}
	return inlineMediaImageResponse(binary, contentType, raw.text, raw.typeTag), nil
}

// recordedThumbResponse serves a cached thumbnail using the cover type
// recorded in MediaDB, without loading the original image. It reports false
// whenever the record cannot be proven to match what a full lookup would
// resolve, and the caller then takes the full path.
func recordedThumbResponse(
	env *requests.RequestEnv,
	cache *mediaThumbCache,
	ref mediaRefParam,
	prefs []string,
	maxSize int,
	localPath bool,
) (models.MediaImageResponse, bool) {
	if env.Database == nil || env.Database.MediaDB == nil {
		return models.MediaImageResponse{}, false
	}
	db := env.Database.MediaDB
	mediaDBID, ok := mediaImageRequestDBID(env, ref)
	if !ok {
		return models.MediaImageResponse{}, false
	}
	thumb, found, err := db.GetMediaCoverThumb(env.Context, mediaDBID)
	if err != nil {
		log.Debug().Err(err).Int64("mediaDBID", mediaDBID).Msg("media.image: cover thumb record lookup failed")
		return models.MediaImageResponse{}, false
	}
	if !found || (ref.MediaID == nil && thumb.SystemID != ref.System) {
		return models.MediaImageResponse{}, false
	}
	if !recordedTypeMatchesPrefs(thumb.TypeTag, thumb.AvailableTypeTags, prefs) {
		return models.MediaImageResponse{}, false
	}
	// Zip container aliases add artwork from equivalent media rows that the
	// recorded property list does not cover; let the full path decide.
	if zipContainerAliasesEnabled(env) &&
		(helpers.IsZip(thumb.Path) || helpers.IsZip(strings.TrimSuffix(thumb.ParentDir, "/"))) {
		return models.MediaImageResponse{}, false
	}
	response, cached := cachedMediaImageResponse(cache, ref, thumb.SystemID, thumb.TypeTag, maxSize, "", localPath)
	if !cached {
		return models.MediaImageResponse{}, false
	}
	cache.setResolvedThumb(ref, prefs, maxSize, thumb.SystemID, thumb.TypeTag)
	return response, true
}

// mediaImageRequestDBID resolves a request to its media row with cheap
// indexed lookups. Path requests only match an exact indexed path; relative,
// singleton and directory fallbacks are left to the full path.
func mediaImageRequestDBID(env *requests.RequestEnv, ref mediaRefParam) (int64, bool) {
	if ref.MediaID != nil {
		return *ref.MediaID, *ref.MediaID > 0
	}
	db := env.Database.MediaDB
	system, err := db.FindSystemBySystemID(ref.System)
	if err != nil {
		return 0, false
	}
	media, err := db.FindMediaBySystemAndPath(env.Context, system.DBID, ref.Path)
	if err != nil || media == nil {
		return 0, false
	}
	return media.DBID, true
}

// recordedTypeMatchesPrefs reports whether typeTag is what the preference
// walk would pick: it must be one of prefs and no preference ahead of it may
// be available on the media row or its title.
func recordedTypeMatchesPrefs(typeTag string, available, prefs []string) bool {
	availableSet := make(map[string]struct{}, len(available))
	for _, tag := range available {
		availableSet[tag] = struct{}{}
	}
	if _, ok := availableSet[typeTag]; !ok {
		return false
	}
	for _, pref := range prefs {
		prefTag, ok := resolveImageTypeTag(pref)
		if !ok {
			continue
		}
		if prefTag == typeTag {
			return true
		}
		if _, ok := availableSet[prefTag]; ok {
			return false
		}
	}
	return false
}

// coverRecordEligible reports whether a resolved image should be recorded as
// the media row's cover. Requests with a single explicit type (a detail view
// asking for a screenshot, say) do not define the cover, so they never
// replace the record written by cover requests with fallbacks.
func coverRecordEligible(env *requests.RequestEnv, raw *rawMediaImage, prefs []string) bool {
	return raw != nil && raw.mediaDBID > 0 && len(prefs) > 1 &&
		env.Database != nil && env.Database.MediaDB != nil
}

// recordCoverThumb records the cover's resolved type and average colour.
// Failures are logged and ignored: the record is an optimisation.
func recordCoverThumb(env *requests.RequestEnv, raw *rawMediaImage, prefs []string, avgColor *uint32) {
	if !coverRecordEligible(env, raw, prefs) {
		return
	}
	if err := env.Database.MediaDB.PutMediaCoverThumb(
		context.WithoutCancel(env.Context), raw.mediaDBID, raw.typeTag, avgColor,
	); err != nil {
		log.Debug().Err(err).Int64("mediaDBID", raw.mediaDBID).Msg("media.image: failed to record cover thumb")
	}
}

// recordCachedCoverThumb records a cover whose thumbnail was already on disk,
// for example one built before cover records existed. The colour is taken
// from the small cached thumbnail rather than the original.
func recordCachedCoverThumb(
	env *requests.RequestEnv,
	cache *mediaThumbCache,
	ref mediaRefParam,
	raw *rawMediaImage,
	prefs []string,
	maxSize int,
) {
	if !coverRecordEligible(env, raw, prefs) {
		return
	}
	var avgColor *uint32
	if data, contentType, found := cache.read(ref, raw.system, raw.typeTag, maxSize); found {
		if img, err := decodeResizableImage(data, contentType); err == nil {
			avgColor = averageImageColor(img)
		}
	}
	recordCoverThumb(env, raw, prefs, avgColor)
}

func parseMediaImageRequest(raw json.RawMessage) (mediaRefParam, string, error) {
	var params models.MediaImageParams
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		return mediaRefParam{}, "", models.ClientErrf("invalid params: %w", err)
	}
	ref := mediaRefParam{
		MediaID: params.MediaID, MaxSize: params.MaxSize, System: params.System,
		Path: params.Path, ImageTypes: params.ImageTypes,
	}
	if err := validateMediaRef(ref); err != nil {
		return mediaRefParam{}, "", models.ClientErrf("invalid params: %w", err)
	}
	if err := validateImageTypes(ref.ImageTypes); err != nil {
		return mediaRefParam{}, "", models.ClientErrf("invalid params: %w", err)
	}
	if ref.MaxSize != nil && (*ref.MaxSize <= 0 || *ref.MaxSize > 8192) {
		return mediaRefParam{}, "", models.ClientErrf("invalid params: maxSize must be between 1 and 8192")
	}
	return ref, params.Delivery, nil
}

func loadRawMediaImageSinglePath(
	env *requests.RequestEnv,
	ref mediaRefParam,
	prefs []string,
	maxBytes int64,
) (*rawMediaImage, error) {
	db := env.Database.MediaDB
	system, err := db.FindSystemBySystemID(ref.System)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ClientErrf("system not found: %s", ref.System)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve system: %w", err)
	}

	// Preserve exact media behavior ahead of directory lookup. Relative and
	// singleton media fallbacks run only after exact directory properties.
	media, err := db.FindMediaBySystemAndPath(env.Context, system.DBID, ref.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to find media: %w", err)
	}
	if media != nil {
		row, rowErr := getMediaFullRow(env, media.DBID, ref.System, ref.Path)
		if rowErr != nil {
			return nil, rowErr
		}
		return loadRawMediaImageRow(env, row, prefs, maxBytes)
	}

	directoryProps, err := db.GetDirectoryProperties(env.Context, system.DBID, ref.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory properties: %w", err)
	}
	if len(directoryProps) > 0 {
		directoryRow := &database.MediaFullRow{
			Path: ref.Path,
			System: database.System{
				DBID: system.DBID, SystemID: system.SystemID, Name: system.Name,
			},
		}
		raw, directoryErr := selectRawMediaImageFromSources(
			env.Context, afero.NewOsFs(), env.Platform, db, directoryRow, nil, directoryProps, prefs, maxBytes,
		)
		if directoryErr == nil {
			return raw, nil
		}
		var notFoundErr *mediaImageNotFoundError
		if !errors.As(directoryErr, &notFoundErr) {
			return nil, directoryErr
		}
	}

	row, err := resolveMediaBySystemAndPath(env, ref.System, ref.Path)
	if err != nil {
		return nil, err
	}
	return loadRawMediaImageRow(env, row, prefs, maxBytes)
}

func loadRawMediaImageRow(
	env *requests.RequestEnv,
	row *database.MediaFullRow,
	prefs []string,
	maxBytes int64,
) (*rawMediaImage, error) {
	db := env.Database.MediaDB
	mediaPropSources, err := mediaImagePropSources(env, row)
	if err != nil {
		return nil, err
	}
	titleProps, err := db.GetMediaTitleProperties(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title properties: %w", err)
	}
	return selectRawMediaImageFromSources(
		env.Context, afero.NewOsFs(), env.Platform, db, row, mediaPropSources, titleProps, prefs, maxBytes,
	)
}

func loadRawMediaImageByID(
	env *requests.RequestEnv,
	ref mediaRefParam,
	prefs []string,
	maxBytes int64,
) (*rawMediaImage, error) {
	resolved, err := resolveMediaRefs(env, []mediaRefParam{ref})
	if err != nil {
		return nil, err
	}
	if resolved[0].Err != nil {
		return nil, resolved[0].Err
	}
	db := env.Database.MediaDB
	row := resolved[0].Row
	mediaPropSources, err := mediaImagePropSources(env, row)
	if err != nil {
		return nil, err
	}
	titleProps, err := db.GetMediaTitleProperties(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title properties: %w", err)
	}
	return selectRawMediaImageFromSources(
		env.Context, afero.NewOsFs(), env.Platform, db, row, mediaPropSources, titleProps, prefs, maxBytes,
	)
}

func imagePrefs(topLevel, itemLevel []string) []string {
	if len(itemLevel) > 0 {
		return itemLevel
	}
	if len(topLevel) > 0 {
		return topLevel
	}
	return defaultImageTypes
}

func readMediaBinaryFile(fs afero.Fs, path string, maxBytes int64) ([]byte, error) {
	info, err := fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat media binary file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("media binary file %q is not a regular file", path)
	}
	if info.Size() > maxBytes {
		return nil, &mediaBinaryTooLargeError{path: path, size: info.Size(), max: maxBytes}
	}

	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return nil, fmt.Errorf("read media binary file %q: %w", path, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, &mediaBinaryTooLargeError{path: path, size: int64(len(data)), max: maxBytes}
	}
	return data, nil
}

func mediaImageReadError(path string, err error) error {
	var tooLarge *mediaBinaryTooLargeError
	if errors.As(err, &tooLarge) {
		return models.ClientErrf("media.image: image file too large: %s", tooLarge.Error())
	}
	return fmt.Errorf("media.image: read image file %q: %w", path, err)
}

func mediaImageBlobTooLargeError(prop *database.MediaProperty, maxBytes int64) error {
	return models.ClientErrf(
		"media.image: image blob too large for %s: %d bytes exceeds %d byte limit",
		prop.TypeTag, prop.BlobSize, maxBytes,
	)
}

func mediaImagePropSources(env *requests.RequestEnv, row *database.MediaFullRow) ([][]database.MediaProperty, error) {
	mediaIDs, err := equivalentMediaIDs(env, row)
	if err != nil {
		return nil, err
	}
	if len(mediaIDs) == 1 {
		props, propErr := env.Database.MediaDB.GetMediaProperties(env.Context, row.DBID)
		if propErr != nil {
			return nil, fmt.Errorf("failed to get media properties: %w", propErr)
		}
		return [][]database.MediaProperty{props}, nil
	}

	propsByID, err := env.Database.MediaDB.GetMediaPropertiesByMediaDBIDs(env.Context, mediaIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get media properties: %w", err)
	}
	sources := make([][]database.MediaProperty, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		sources = append(sources, propsByID[id])
	}
	return sources, nil
}

func selectRawMediaImageFromSources(
	ctx context.Context,
	fs afero.Fs,
	platform platforms.Platform,
	db database.MediaDBI,
	row *database.MediaFullRow,
	mediaPropSources [][]database.MediaProperty,
	titleProps []database.MediaProperty,
	prefs []string,
	maxBytes int64,
) (*rawMediaImage, error) {
	sources := make([]mediaImageSource, 0, len(mediaPropSources)+1)
	for _, props := range mediaPropSources {
		sources = append(sources, mediaImageSource{buildPropsMap(props), true})
	}
	sources = append(sources, mediaImageSource{buildPropsMap(titleProps), false})

	fileBackedCandidatesChecked := false
	seen := make(map[string]bool, len(prefs))
	for _, t := range prefs {
		typeTag, ok := resolveImageTypeTag(t)
		if !ok || seen[typeTag] {
			continue
		}
		seen[typeTag] = true

		for _, src := range sources {
			prop, ok := src.propMap[typeTag]
			if !ok {
				continue
			}

			if prop.Text != "" {
				fileBackedCandidatesChecked = true
			}
			raw, stale, err := loadRawMediaImageProperty(ctx, fs, platform, db, row, &prop, src, typeTag, maxBytes)
			if stale {
				delete(src.propMap, typeTag)
				continue
			}
			if err != nil {
				return nil, err
			}
			if raw != nil {
				return raw, nil
			}
		}
	}

	mediaImageProps := make([]string, 0)
	for _, props := range mediaPropSources {
		mediaImageProps = append(mediaImageProps, imagePropertyTypeTags(props)...)
	}
	log.Debug().
		Str("system", row.System.SystemID).
		Str("path", row.Path).
		Int64("mediaDBID", row.DBID).
		Int64("titleDBID", row.Title.DBID).
		Strs("prefs", prefs).
		Strs("mediaImageProps", mediaImageProps).
		Strs("titleImageProps", imagePropertyTypeTags(titleProps)).
		Msg("media.image: no image found")

	return nil, mediaImageNoImageError(row, fileBackedCandidatesChecked)
}

func loadRawMediaImageProperty(
	ctx context.Context,
	fs afero.Fs,
	platform platforms.Platform,
	db database.MediaDBI,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	src mediaImageSource,
	typeTag string,
	maxBytes int64,
) (*rawMediaImage, bool, error) {
	var binary []byte
	contentType := mediaContentType(prop.ContentType, prop.Text)

	switch {
	case len(prop.Binary) > 0:
		if int64(len(prop.Binary)) > maxBytes {
			return nil, false, mediaImageBlobTooLargeError(prop, maxBytes)
		}
		binary = prop.Binary
	case prop.BlobDBID != nil:
		if prop.BlobSize <= 0 {
			return nil, false, models.ClientErrf("media.image: image blob has unknown size for %s", prop.TypeTag)
		}
		if prop.BlobSize > maxBytes {
			return nil, false, mediaImageBlobTooLargeError(prop, maxBytes)
		}
		var err error
		binary, contentType, err = db.GetMediaBlobDataCapped(ctx, *prop.BlobDBID, maxBytes)
		if errors.Is(err, database.ErrMediaBlobTooLarge) {
			return nil, false, mediaImageBlobTooLargeError(prop, maxBytes)
		}
		if err != nil {
			return nil, false, fmt.Errorf("media.image: read image blob %d: %w", *prop.BlobDBID, err)
		}
		if len(binary) == 0 {
			logStaleMediaImageProperty(row, prop, src.isMedia, typeTag, "empty blob data")
			return nil, true, nil
		}
	case prop.Text != "":
		data, stale, err := loadMediaImageFile(ctx, fs, platform, row, prop, src.isMedia, typeTag, maxBytes)
		if stale || err != nil {
			return nil, stale, err
		}
		binary = data
	default:
		return nil, false, nil
	}

	return &rawMediaImage{
		binary:      binary,
		contentType: contentType,
		text:        prop.Text,
		typeTag:     typeTag,
		system:      row.System.SystemID,
		mediaDBID:   row.DBID,
	}, false, nil
}

func loadMediaImageFile(
	ctx context.Context,
	fs afero.Fs,
	platform platforms.Platform,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	isMedia bool,
	typeTag string,
	maxBytes int64,
) (data []byte, stale bool, err error) {
	if platforms.IsSourcePath(prop.Text) {
		return loadSourceMediaImageFile(ctx, platform, row, prop, isMedia, typeTag, maxBytes)
	}
	info, err := fs.Stat(prop.Text)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logStaleMediaImageProperty(row, prop, isMedia, typeTag, err.Error())
			return nil, true, nil
		}
		return nil, false, mediaImageReadError(prop.Text, err)
	}
	if !info.Mode().IsRegular() {
		err := fmt.Errorf("media binary file %q is not a regular file", prop.Text)
		return nil, false, mediaImageReadError(prop.Text, err)
	}
	if info.Size() > maxBytes {
		return nil, false, mediaImageReadError(prop.Text, &mediaBinaryTooLargeError{
			path: prop.Text,
			size: info.Size(),
			max:  maxBytes,
		})
	}
	data, readErr := readMediaBinaryFile(fs, prop.Text, maxBytes)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			logStaleMediaImageProperty(row, prop, isMedia, typeTag, readErr.Error())
			return nil, true, nil
		}
		return nil, false, mediaImageReadError(prop.Text, readErr)
	}
	return data, false, nil
}

// loadSourceMediaImageFile is loadMediaImageFile's counterpart for a folder
// cover or media image stored under a source root: a platform.SourceFileReader
// read instead of an afero.Fs one, since those bytes live behind the host
// (e.g. SAF on Android), not on a filesystem Core can open directly. A
// platform without the capability, or a read failure (the root may no longer
// be granted), is treated as a stale property - the same graceful skip a
// missing real file already gets - not a hard error.
func loadSourceMediaImageFile(
	ctx context.Context,
	platform platforms.Platform,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	isMedia bool,
	typeTag string,
	maxBytes int64,
) (data []byte, stale bool, err error) {
	reader, ok := platform.(platforms.SourceFileReader)
	if !ok {
		logStaleMediaImageProperty(row, prop, isMedia, typeTag, "platform cannot read source root files")
		return nil, true, nil
	}
	content, readErr := reader.ReadSourceFile(ctx, prop.Text, maxBytes)
	if readErr != nil {
		logStaleMediaImageProperty(row, prop, isMedia, typeTag, readErr.Error())
		return nil, true, nil
	}
	if int64(len(content)) > maxBytes {
		return nil, false, mediaImageReadError(prop.Text, &mediaBinaryTooLargeError{
			path: prop.Text,
			size: int64(len(content)),
			max:  maxBytes,
		})
	}
	return content, false, nil
}

func logStaleMediaImageProperty(
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	isMedia bool,
	typeTag string,
	reason string,
) {
	level := "title"
	if isMedia {
		level = "media"
	}
	log.Debug().
		Str("system", row.System.SystemID).
		Str("path", row.Path).
		Int64("mediaDBID", row.DBID).
		Int64("titleDBID", row.Title.DBID).
		Str("typeTag", typeTag).
		Str("text", prop.Text).
		Str("source", level).
		Str("reason", reason).
		Msg("media.image: stale image property ignored")
}

func mediaImageMaxBytes(pl platforms.Platform) int64 {
	if pl != nil && pl.ID() == ids.Mister {
		return misterMediaImageMaxBytes
	}
	return defaultMediaImageMaxBytes
}
