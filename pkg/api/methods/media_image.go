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
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/wuffspng"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/jonboulle/clockwork"
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
	// mediaThumbCacheDirName is the sub-directory of the image folder where
	// resized thumbnail files are persisted across restarts. Core owns it:
	// reapStaleVersions removes everything in it but the live version.
	mediaThumbCacheDirName = config.ImageThumbsDir
	// mediaThumbCacheVersion identifies the on-disk encode scheme. The live cache
	// directory is thumbs/v<N>; bumping this constant changes the live directory
	// so a new build invalidates the cache automatically (the old v<N-1> dir is
	// reaped on start and thumbnails regenerate lazily) without users reindexing.
	// Bump whenever the resized output changes: format, quality, or resize
	// algorithm. v3 names entries after their source image instead of the media
	// row and encodes opaque thumbnails as JPEG; v2 grouped entries by system
	// for targeted invalidation; v1
	// introduced lossy WebP output (replacing lossless PNG / JPEG q85).
	mediaThumbCacheVersion = 3
	// mediaThumbWebPQuality and mediaThumbWebPMethod are the lossy VP8 settings
	// used to re-encode resized thumbnails with transparency. q75/method2 was
	// selected by benchmarking real covers as the best size/speed point: ~13x
	// smaller than the previous lossless-PNG output while preserving alpha, and
	// faster to encode than the higher methods.
	mediaThumbWebPQuality = 75
	mediaThumbWebPMethod  = 2
	// mediaThumbJPEGQuality is the quality opaque thumbnails are encoded at.
	mediaThumbJPEGQuality = 80
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
var mediaThumbCachePointer atomic.Pointer[mediaThumbCache]

const (
	// mediaThumbMemoMax bounds the in-memory resolved-thumbnail memo.
	mediaThumbMemoMax = 8192
	// The cache's size cap ([media] thumb_cache_max_mb, 1 GiB by default) is
	// soft and counted in allocated blocks (see thumbFileDiskBytes). Past the
	// cap, the reaper removes the least recently used files until the cache is
	// back down to mediaThumbCacheTrimPercent of it, but never one used within
	// mediaThumbCacheMinAge, so a day of heavy browsing can hold it above the
	// cap. On a MiSTer, where every thumbnail takes a 128 KiB cluster, 1 GiB
	// holds about 8,000.
	mediaThumbCacheTrimPercent = 80
	// mediaThumbCacheMinAge protects recently used files from the reaper. A
	// hit refreshes a file's mtime once it is older than this, so a file handed
	// out as a local path is never reaped while a client may still read it.
	mediaThumbCacheMinAge = 24 * time.Hour
	// mediaThumbReapInterval is the least time between two orphan reaps
	// requested by an index or scrape completing.
	mediaThumbReapInterval = 24 * time.Hour
	// mediaThumbCapReapInterval is the least time between two orphan reaps
	// requested by new thumbnails taking the cache past its cap. A cache held
	// over the cap by recently used files would otherwise be walked on every
	// write.
	mediaThumbCapReapInterval = time.Hour
	// mediaThumbStartupReapDelay keeps the first orphan reap off startup.
	mediaThumbStartupReapDelay = 2 * time.Minute
	// mediaThumbTempMaxAge is the age after which a temporary file that was
	// never renamed into place is treated as abandoned.
	mediaThumbTempMaxAge = time.Hour
)

// mediaThumbCache persists resized cover images to disk so that the expensive
// decode+bilinear-resize+encode is only performed once per source image and
// size tier. The cache survives core restarts and reboots.
//
// A file is named after its source, not after the media row asking for it: a
// file-backed image by path, size and modification time, any other image by a
// hash of its bytes. A scrape or reindex that leaves an image alone therefore
// keeps its thumbnail, a changed image gets a new name and is rebuilt, and a
// media DBID reused for different media can never pick up another row's file.
// Nothing is wiped; files no longer referenced are left to reapOrphans, which
// keeps the cache under a size cap.
//
// The live directory is thumbs/v<mediaThumbCacheVersion> — deterministic across
// restarts. On start, reapStaleVersions removes any sibling that is not the
// current version, so a version bump invalidates the cache and directories left
// by older builds are reaped. Files are sharded by the first two hex characters
// of their name so no single directory grows large.
//
// resolved maps an early key (identity+prefs+maxSize) to the file a request
// resolved to. It enables a pre-semaphore cache hit that skips the entire
// image-load pipeline for warm repeat requests, and is forgotten whenever an
// index or scrape completes.
type resolvedThumb struct {
	system      string
	typeTag     string
	path        string
	contentType string
}

type mediaThumbCache struct {
	createdAt time.Time
	clock     clockwork.Clock
	fs        afero.Fs
	reapCtx   context.Context
	resolved  map[string]resolvedThumb
	resizeSem chan struct{}
	reapDone  chan struct{}
	dir       string
	reapWG    sync.WaitGroup
	// maxBytes is the size cap; a settings change updates it in place.
	maxBytes atomic.Int64
	// clusterBytes rounds file sizes up to the volume's allocation unit
	// where a stat does not report allocated blocks (Windows).
	clusterBytes int64
	// cacheBytes is the cache's disk use as of the last reap plus every
	// file written since, so a write can tell when the cap is passed.
	cacheBytes atomic.Int64
	lastReap   atomic.Int64
	resolvedMu syncutil.RWMutex
	reapMu     syncutil.Mutex
	reaping    atomic.Bool
	stopped    bool
}

// resizeSemaphore returns the channel bounding concurrent resize work.
func (c *mediaThumbCache) resizeSemaphore() chan struct{} {
	if c == nil || c.resizeSem == nil {
		return defaultMediaImageResizeSem
	}
	return c.resizeSem
}

// now reads the cache's clock; a cache built without one uses wall time.
func (c *mediaThumbCache) now() time.Time {
	if c.clock == nil {
		return time.Now()
	}
	return c.clock.Now()
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

// mediaImageDir is the configured image folder. A nil config, which only
// tests pass, falls back to the default under the data directory.
func mediaImageDir(pl platforms.Platform, cfg *config.Instance) string {
	if pl == nil {
		return ""
	}
	dataDir := helpers.DataDir(pl)
	if cfg == nil {
		return config.ResolveImageDir("", dataDir)
	}
	return cfg.ImageDir(dataDir)
}

// requestImageDir is mediaImageDir for a request.
func requestImageDir(env *requests.RequestEnv) string {
	return mediaImageDir(env.Platform, env.Config)
}

// mediaThumbCacheDir is the live cache directory under the image folder.
func mediaThumbCacheDir(pl platforms.Platform, cfg *config.Instance) string {
	return filepath.Join(mediaImageDir(pl, cfg), mediaThumbCacheDirName, mediaThumbCacheVersionDir())
}

// mediaThumbCacheMaxBytes is the configured size cap of the cache.
func mediaThumbCacheMaxBytes(cfg *config.Instance) int64 {
	if cfg == nil {
		return int64(config.DefaultThumbCacheMaxMB) << 20
	}
	return cfg.ThumbCacheMaxBytes()
}

func newMediaThumbCacheWithFS(pl platforms.Platform, cfg *config.Instance, fs afero.Fs) *mediaThumbCache {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir := mediaThumbCacheDir(pl, cfg)
	concurrency := mediaImageResizeConcurrency
	if pl != nil && pl.Settings().ResourceConstrained {
		concurrency = mediaImageResizeConcurrencyConstrained
	}
	clock := clockwork.NewRealClock()
	cache := &mediaThumbCache{
		fs:           fs,
		clock:        clock,
		dir:          dir,
		resolved:     make(map[string]resolvedThumb),
		resizeSem:    make(chan struct{}, concurrency),
		clusterBytes: thumbCacheClusterBytes(dir),
		createdAt:    clock.Now(),
	}
	cache.maxBytes.Store(mediaThumbCacheMaxBytes(cfg))
	return cache
}

// reapStaleVersions ensures the live versioned directory exists and removes
// every sibling under thumbs/ that is not the current version. This invalidates
// the cache after a mediaThumbCacheVersion bump and reaps directories left by
// an older build (earlier versions and their moved-aside wipe directories).
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
	// The parent sits in a user-chosen image folder beside scraped images.
	// Only ever empty Core's own thumbs folder, whatever c.dir was built from.
	if filepath.Base(parent) != mediaThumbCacheDirName || live != mediaThumbCacheVersionDir() {
		log.Warn().Str("dir", c.dir).Msg("media.image: thumb cache: not reaping outside the thumbs folder")
		return
	}
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
		if info.IsDir() || !isThumbTempFile(info.Name()) {
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

func isThumbTempFile(name string) bool {
	return strings.HasPrefix(name, ".thumb-") && strings.HasSuffix(name, ".tmp")
}

// requestReap starts a background orphan reap unless one ran within
// interval, one is already running, or the cache was never started with
// InitMediaThumbCache.
func (c *mediaThumbCache) requestReap(interval time.Duration) {
	if last := c.lastReap.Load(); last != 0 &&
		c.now().Sub(time.Unix(0, last)) < interval {
		return
	}
	c.reapMu.Lock()
	defer c.reapMu.Unlock()
	if c.reapCtx == nil || c.stopped {
		return
	}
	c.reapWG.Add(1)
	go func() {
		defer c.reapWG.Done()
		c.tryReapOrphans(c.reapCtx)
	}()
}

// tryReapOrphans runs reapOrphans unless another reap is already running.
func (c *mediaThumbCache) tryReapOrphans(ctx context.Context) {
	if !c.reaping.CompareAndSwap(false, true) {
		return
	}
	defer c.reaping.Store(false)
	c.reapOrphans(ctx)
}

// stopReaps cancels background reaps and waits for them to return.
func (c *mediaThumbCache) stopReaps(cancel context.CancelFunc) {
	c.reapMu.Lock()
	c.stopped = true
	c.reapMu.Unlock()
	cancel()
	c.reapWG.Wait()
}

type thumbCacheFile struct {
	modTime time.Time
	path    string
	size    int64
}

// reapOrphans keeps the cache under its size cap. Files are never invalidated
// directly — a changed source simply stops matching its old file — so this is
// what removes thumbnails nothing asks for any more. Under the cap it deletes
// nothing. Over it, the least recently used files go first (a hit refreshes a
// file's mtime, see touchThumbFile) until the cache is at
// mediaThumbCacheTrimPercent of the cap, and nothing used within
// mediaThumbCacheMinAge is removed. Abandoned temporary files are removed too.
func (c *mediaThumbCache) reapOrphans(ctx context.Context) {
	now := c.now()
	c.lastReap.Store(now.UnixNano())
	var files []thumbCacheFile
	var total int64
	err := afero.Walk(c.fs, c.dir, func(path string, info os.FileInfo, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if walkErr != nil {
			return walkErr
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if isThumbTempFile(info.Name()) {
			if now.Sub(info.ModTime()) > mediaThumbTempMaxAge {
				if err := c.fs.Remove(path); err != nil {
					log.Debug().Err(err).Str("path", path).
						Msg("media.image: thumb cache: failed to reap temporary file")
				}
			}
			return nil
		}
		size := thumbFileDiskBytes(info, c.clusterBytes)
		files = append(files, thumbCacheFile{path: path, size: size, modTime: info.ModTime()})
		total += size
		return nil
	})
	if err != nil {
		if ctx.Err() == nil {
			log.Warn().Err(err).Str("dir", c.dir).Msg("media.image: thumb cache: failed to walk for reap")
		}
		return
	}
	defer func() { c.cacheBytes.Store(total) }()
	maxBytes := c.maxBytes.Load()
	if total <= maxBytes {
		log.Debug().Int("files", len(files)).Int64("bytes", total).
			Msg("media.image: thumb cache: under size cap")
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	target := maxBytes * mediaThumbCacheTrimPercent / 100
	removed := 0
	var removedBytes int64
	for _, file := range files {
		if total <= target || ctx.Err() != nil || now.Sub(file.modTime) < mediaThumbCacheMinAge {
			break
		}
		if err := c.fs.Remove(file.path); err != nil {
			log.Debug().Err(err).Str("path", file.path).Msg("media.image: thumb cache: failed to reap file")
			continue
		}
		total -= file.size
		removed++
		removedBytes += file.size
	}
	log.Info().Int("removed", removed).Int64("removedBytes", removedBytes).Int64("bytes", total).
		Msg("media.image: thumb cache: reaped least recently used thumbnails")
}

// earlyThumbKey returns a cache key derived only from request-time parameters
// (no typeTag needed). Used for the pre-semaphore resolved-thumbnail memo.
func earlyThumbKey(ref mediaRefParam, prefs []string, maxSize int) string {
	var identity string
	switch {
	case isDirectoryMediaRef(ref):
		identity = fmt.Sprintf("dir:%s:%s:id:%d", ref.System, filepath.ToSlash(ref.Path), *ref.MediaID)
	case ref.MediaID != nil:
		identity = fmt.Sprintf("id:%d", *ref.MediaID)
	default:
		identity = fmt.Sprintf("path:%s:%s", ref.System, filepath.ToSlash(ref.Path))
	}
	return fmt.Sprintf("early:%s:[%s]:%d", identity, strings.Join(prefs, ","), maxSize)
}

// getResolvedThumb returns the cache file this request resolved to before, or
// false when not yet memoized.
func (c *mediaThumbCache) getResolvedThumb(
	ref mediaRefParam, prefs []string, maxSize int,
) (resolvedThumb, bool) {
	c.resolvedMu.RLock()
	defer c.resolvedMu.RUnlock()
	resolved, ok := c.resolved[earlyThumbKey(ref, prefs, maxSize)]
	return resolved, ok
}

// setResolvedThumb records the cache file a request resolved to for future
// pre-semaphore hits.
func (c *mediaThumbCache) setResolvedThumb(ref mediaRefParam, prefs []string, maxSize int, resolved resolvedThumb) {
	key := earlyThumbKey(ref, prefs, maxSize)
	c.resolvedMu.Lock()
	defer c.resolvedMu.Unlock()
	if _, exists := c.resolved[key]; !exists && len(c.resolved) >= mediaThumbMemoMax {
		for existing := range c.resolved {
			delete(c.resolved, existing)
			break
		}
	}
	c.resolved[key] = resolved
}

// clearResolved forgets memoized thumbnails for the given systems, or all of
// them when systems is nil.
func (c *mediaThumbCache) clearResolved(systems map[string]struct{}) {
	c.resolvedMu.Lock()
	defer c.resolvedMu.Unlock()
	if systems == nil {
		c.resolved = make(map[string]resolvedThumb)
		return
	}
	for key, resolved := range c.resolved {
		if _, ok := systems[resolved.system]; ok {
			delete(c.resolved, key)
		}
	}
}

// fileThumbSourceKey identifies a file-backed image by path, size,
// modification time and, where the platform has one, status change time, so
// a replaced file gets a new key without being read. A replacement that keeps
// the size and modification time is only told apart by the change time, which
// exFAT and FAT (and Windows) do not provide.
//
// An images:// path is used as it is, not resolved, so a thumbnail survives
// the image folder moving with its modification times intact.
func fileThumbSourceKey(path string, info os.FileInfo) string {
	if !config.IsImagePath(path) {
		path = filepath.ToSlash(filepath.Clean(path))
	}
	return fmt.Sprintf("f:%d:%d:%d:%s", info.Size(), info.ModTime().UnixNano(), thumbFileChangeNanos(info), path)
}

// dataThumbSourceKey identifies an image by its content, for sources that
// cannot be stat'ed: blobs and files under a source root. The content type is
// length-prefixed so the two fields cannot run together.
func dataThumbSourceKey(contentType string, data []byte) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%d:%s", len(contentType), contentType)
	_, _ = h.Write(data)
	return "d:" + hex.EncodeToString(h.Sum(nil))
}

// isFileThumbSourceKey reports whether key came from fileThumbSourceKey.
func isFileThumbSourceKey(key string) bool {
	return strings.HasPrefix(key, "f:")
}

// thumbFileHash names the cache file for one source at one size tier.
func thumbFileHash(sourceKey string, maxSize int) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d", sourceKey, maxSize))
	return hex.EncodeToString(h[:])
}

func (c *mediaThumbCache) shardDir(hash string) string {
	return filepath.Join(c.dir, hash[:2])
}

type thumbCacheFormat struct {
	ext         string
	contentType string
}

// thumbCacheFormats lists WebP first because it is what the resize writes.
var thumbCacheFormats = []thumbCacheFormat{
	{ext: ".webp", contentType: "image/webp"},
	{ext: ".jpg", contentType: "image/jpeg"},
	{ext: ".png", contentType: "image/png"},
	{ext: ".gif", contentType: "image/gif"},
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

// touchThumbFile marks a hit file as recently used for reapOrphans. The write
// happens at most once per mediaThumbCacheMinAge per file.
func (c *mediaThumbCache) touchThumbFile(path string, info os.FileInfo) {
	now := c.now()
	if now.Sub(info.ModTime()) < mediaThumbCacheMinAge {
		return
	}
	if err := c.fs.Chtimes(path, now, now); err != nil {
		log.Debug().Err(err).Str("path", path).Msg("media.image: thumb cache: failed to touch file")
	}
}

// lookupPath returns a regular cached thumbnail path without reading its bytes.
func (c *mediaThumbCache) lookupPath(sourceKey string, maxSize int) (path, contentType string, found bool) {
	if sourceKey == "" {
		return "", "", false
	}
	hash := thumbFileHash(sourceKey, maxSize)
	dir := c.shardDir(hash)
	for _, format := range thumbCacheFormats {
		//nolint:gosec // path is constructed from controlled dirs + SHA-256 hash + fixed extension
		candidate := filepath.Join(dir, hash+format.ext)
		info, err := thumbCacheFileInfo(c.fs, candidate)
		if err == nil && info.Mode().IsRegular() {
			c.touchThumbFile(candidate, info)
			return candidate, format.contentType, true
		}
	}
	return "", "", false
}

// read returns cached thumbnail bytes. A deletion between lookup and read is a miss.
func (c *mediaThumbCache) read(sourceKey string, maxSize int) (data []byte, contentType string, found bool) {
	path, contentType, found := c.lookupPath(sourceKey, maxSize)
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
func (c *mediaThumbCache) get(sourceKey string, maxSize int) (data []byte, contentType string, found bool) {
	return c.read(sourceKey, maxSize)
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
func (c *mediaThumbCache) set(sourceKey string, maxSize int, data []byte, contentType string) (string, error) {
	if sourceKey == "" {
		return "", errors.New("thumbnail source key is empty")
	}
	ext := thumbCacheExtension(contentType, data)
	if ext == "" {
		return "", fmt.Errorf("unsupported thumbnail content type %q", contentType)
	}
	hash := thumbFileHash(sourceKey, maxSize)
	dir := c.shardDir(hash)
	if err := c.fs.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // cache dir, 0o750 is intentional
		return "", fmt.Errorf("create thumbnail cache directory: %w", err)
	}
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
	c.noteWritten(path)
	return path, nil
}

// noteWritten adds a new file to the cache's running size, and requests a
// reap once that passes the cap, so browsing cannot grow the cache past it
// for long between index and scrape completions.
func (c *mediaThumbCache) noteWritten(path string) {
	info, err := thumbCacheFileInfo(c.fs, path)
	if err != nil {
		return
	}
	if c.cacheBytes.Add(thumbFileDiskBytes(info, c.clusterBytes)) > c.maxBytes.Load() {
		c.requestReap(mediaThumbCapReapInterval)
	}
}

// mediaThumbCacheRun owns the caches one InitMediaThumbCache started: the
// live one and any a settings change replaced whose reaps are still winding
// down, so stopping the service waits for all of them.
type mediaThumbCacheRun struct {
	pl      platforms.Platform
	stop    func()
	retired sync.WaitGroup
	mu      syncutil.Mutex
	stopped bool
}

// activeMediaThumbCacheRun is the run ReloadMediaThumbCache moves, the one
// the most recent InitMediaThumbCache started.
var activeMediaThumbCacheRun atomic.Pointer[mediaThumbCacheRun]

// startMediaThumbCache builds a cache from the config, installs it as the
// process-wide cache and starts its background reaps. The returned function
// cancels them and waits for them to return.
func startMediaThumbCache(pl platforms.Platform, cfg *config.Instance) (stop func()) {
	cache := newMediaThumbCacheWithFS(pl, cfg, nil)
	if err := cache.fs.MkdirAll(cache.dir, 0o750); err != nil { //nolint:gosec // cache dir, 0o750 is intentional
		log.Debug().Err(err).Str("dir", cache.dir).Msg("media.image: thumb cache: failed to create dir")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cache.reapCtx = ctx
	cache.reapDone = make(chan struct{})
	mediaThumbCachePointer.Store(cache)
	go func() {
		defer close(cache.reapDone)
		cache.reapStaleVersions()
		select {
		case <-ctx.Done():
			return
		case <-cache.clock.After(mediaThumbStartupReapDelay):
		}
		cache.tryReapOrphans(ctx)
	}()
	return func() {
		cache.stopReaps(cancel)
		<-cache.reapDone
	}
}

// InitMediaThumbCache creates or replaces the process-wide thumb cache for the
// given platform, in the image folder and with the size cap cfg sets. Must be
// called once during service start, before any media.image requests are
// handled. The cache is usable as soon as this returns; stale-version
// directories and abandoned temporary files are reaped in the background so a
// large cache does not delay startup, followed after
// mediaThumbStartupReapDelay by the first orphan reap. The returned function
// cancels pending reaps and waits for them, including those of a cache that
// ReloadMediaThumbCache replaced, so a stopping service does not leave them
// writing to the image folder. Uses atomic store so concurrent StartWithReady
// calls in tests do not race.
func InitMediaThumbCache(pl platforms.Platform, cfg *config.Instance) (stop func()) {
	run := &mediaThumbCacheRun{pl: pl}
	run.mu.Lock()
	run.stop = startMediaThumbCache(pl, cfg)
	run.mu.Unlock()
	activeMediaThumbCacheRun.Store(run)
	return func() {
		activeMediaThumbCacheRun.CompareAndSwap(run, nil)
		run.mu.Lock()
		run.stopped = true
		stopCache := run.stop
		run.stop = nil
		run.mu.Unlock()
		if stopCache != nil {
			stopCache()
		}
		run.retired.Wait()
	}
}

// ReloadMediaThumbCache applies a changed image folder or size cap to the
// running cache and reports whether anything changed. A new cap is applied in
// place. A new folder gets a new cache there: it replaces the old one at once,
// and the old one's reaps are stopped in the background. Files are never
// moved or copied; the old folder's thumbnails are simply no longer used.
func ReloadMediaThumbCache(cfg *config.Instance) bool {
	run := activeMediaThumbCacheRun.Load()
	if run == nil {
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.stopped {
		return false
	}
	current := mediaThumbCachePointer.Load()
	maxBytes := mediaThumbCacheMaxBytes(cfg)
	if current != nil && current.dir == mediaThumbCacheDir(run.pl, cfg) {
		if current.maxBytes.Swap(maxBytes) == maxBytes {
			return false
		}
		log.Info().Int64("maxBytes", maxBytes).Msg("media.image: thumb cache: size cap changed")
		if current.cacheBytes.Load() > maxBytes {
			current.requestReap(0)
		}
		return true
	}
	stopOld := run.stop
	run.stop = startMediaThumbCache(run.pl, cfg)
	if next := mediaThumbCachePointer.Load(); next != nil {
		log.Info().Str("dir", next.dir).Msg("media.image: thumb cache: moved to a new folder")
	}
	if stopOld != nil {
		run.retired.Add(1)
		go func() {
			defer run.retired.Done()
			stopOld()
		}()
	}
	return true
}

// InvalidateMediaThumbMemo forgets every resolved thumbnail so the next request
// for each cover resolves its source again. Called after an index or scrape
// completes. Cached files are kept: one whose source is unchanged is still a
// hit, and one whose source changed is never matched again and is left to the
// orphan reap this also requests.
func InvalidateMediaThumbMemo() {
	if cache := mediaThumbCachePointer.Load(); cache != nil {
		cache.clearResolved(nil)
		cache.requestReap(mediaThumbReapInterval)
	}
}

// InvalidateMediaThumbMemoSystems is InvalidateMediaThumbMemo for the supplied
// systems only.
func InvalidateMediaThumbMemoSystems(systemIDs []string) {
	cache := mediaThumbCachePointer.Load()
	if cache == nil {
		return
	}
	systems := make(map[string]struct{}, len(systemIDs))
	for _, systemID := range systemIDs {
		if systemID != "" {
			systems[systemID] = struct{}{}
		}
	}
	if len(systems) == 0 {
		return
	}
	cache.clearResolved(systems)
	cache.requestReap(mediaThumbReapInterval)
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
	sourceKey   string // names the image's thumbnails; see mediaThumbCache
	binary      []byte
	mediaDBID   int64 // 0 when the image belongs to a directory, not a media row
	// cached is true when binary was left unread because a thumbnail for
	// sourceKey was already cached; see mediaImageCacheProbe.
	cached bool
}

// mediaImageCacheProbe reports whether a thumbnail for sourceKey is already
// cached at the requested size. A file-backed source whose thumbnail exists is
// then never read: a stat names it, and the read is the slow part on SD and
// USB storage. nil disables the probe.
type mediaImageCacheProbe func(sourceKey string) bool

// mediaImageFile is a loaded image file and the key naming its thumbnails.
// data is nil when the probe found its thumbnail already cached.
type mediaImageFile struct {
	sourceKey string
	data      []byte
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
	decodeStart := time.Now()
	src, err := decodeResizableImage(binary, contentType)
	if err != nil {
		return binary, contentType, nil
	}
	decodeDur := time.Since(decodeStart)
	scaleStart := time.Now()
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
	scaleDur := time.Since(scaleStart)
	// A small source that already fits skips the encode; see
	// thumbPassthroughMaxBytes. The colour still comes from the decoded frame.
	// The cache names its files by format, so a source it could not store keeps
	// going through the encode rather than missing the cache on every request.
	if newW == w && newH == h && len(binary) <= thumbPassthroughMaxBytes &&
		thumbCacheExtension(contentType, binary) != "" {
		return binary, contentType, avgColor
	}
	encodeStart := time.Now()
	out, outputType, err := encodeResizedImage(frame)
	encodeDur := time.Since(encodeStart)
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
		Dur("decode", decodeDur).
		Dur("scale", scaleDur).
		Dur("encode", encodeDur).
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

// encodeResizedImage encodes a resized frame: an opaque one as JPEG, one with
// transparency as lossy WebP so its alpha survives. On a MiSTer the pure-Go
// WebP encode was the largest step of a cold cover (a median 142 ms for a
// 256 px thumbnail of a 320 px JPEG), and covers are nearly always opaque.
// Isolated in one function so the encoder can be swapped without touching the
// resize pipeline.
func encodeResizedImage(img image.Image) (data []byte, contentType string, err error) {
	if imageHasTransparency(img) {
		return encodeWebPImage(img)
	}
	var out bytes.Buffer
	if encErr := jpeg.Encode(&out, img, &jpeg.Options{Quality: mediaThumbJPEGQuality}); encErr != nil {
		return nil, "", fmt.Errorf("encode resized jpeg: %w", encErr)
	}
	return out.Bytes(), "image/jpeg", nil
}

// encodeWebPImage encodes img as lossy WebP (VP8) with alpha preserved.
func encodeWebPImage(img image.Image) (data []byte, contentType string, err error) {
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

// mediaImageMaxPixels bounds the frame a source image may declare. The byte
// caps only limit the compressed file, and every decoder allocates the whole
// frame from the header before reading pixels, so a small file declaring a
// huge frame could otherwise take hundreds of megabytes. 4096x4096 is far past
// any cover art.
const mediaImageMaxPixels = 4096 * 4096

// checkImageDimensions reads an image's header and rejects a frame larger
// than mediaImageMaxPixels before any decoder allocates it.
func checkImageDimensions(format string, binary []byte, decodeConfig func(io.Reader) (image.Config, error)) error {
	cfg, err := decodeConfig(bytes.NewReader(binary))
	if err != nil {
		return fmt.Errorf("decode %s header for resize: %w", format, err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > mediaImageMaxPixels {
		return fmt.Errorf("%s for resize is %dx%d, past the %d pixel limit",
			format, cfg.Width, cfg.Height, mediaImageMaxPixels)
	}
	return nil
}

func decodeResizableImage(binary []byte, contentType string) (image.Image, error) {
	decodeJPEG := func() (image.Image, error) {
		if err := checkImageDimensions("JPEG", binary, jpeg.DecodeConfig); err != nil {
			return nil, err
		}
		img, err := jpeg.Decode(bytes.NewReader(binary))
		if err != nil {
			return nil, fmt.Errorf("decode JPEG for resize: %w", err)
		}
		return img, nil
	}
	decodePNG := func() (image.Image, error) {
		if err := checkImageDimensions("PNG", binary, png.DecodeConfig); err != nil {
			return nil, err
		}
		// Wuffs decodes cover art several times faster than image/png on ARM.
		// Anything it rejects still gets image/png's verdict.
		if img, err := wuffspng.Decode(binary); err == nil {
			return img, nil
		} else if !errors.Is(err, wuffspng.ErrUnavailable) {
			log.Debug().Err(err).Msg("media.image: wuffs png decode failed, using image/png")
		}
		img, err := png.Decode(bytes.NewReader(binary))
		if err != nil {
			return nil, fmt.Errorf("decode PNG for resize: %w", err)
		}
		return img, nil
	}
	decodeWebP := func() (image.Image, error) {
		if err := checkImageDimensions("WebP", binary, webp.DecodeConfig); err != nil {
			return nil, err
		}
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
	if isDirectoryMediaRef(ref) {
		return fmt.Sprintf("dir:%s\x00%s\x00%d\x00%s",
			ref.System, filepath.ToSlash(ref.Path), *ref.MediaID, strings.Join(prefs, "\x00"))
	}
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

// mediaImageNotFound reports whether err is a no-image result, and whether
// that lookup checked file-backed candidates.
func mediaImageNotFound(err error) (notFound, fileBackedCandidatesChecked bool) {
	var noImage *mediaImageNotFoundError
	if !errors.As(err, &noImage) {
		return false, false
	}
	return true, noImage.fileBackedCandidatesChecked
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

// thumbFileResponse serves an existing cache file, as a local path when asked
// for one and inline otherwise. It reports false when the file is gone.
func thumbFileResponse(
	cache *mediaThumbCache, path, contentType, typeTag, sourcePath string, localPath bool,
) (models.MediaImageResponse, bool) {
	info, err := thumbCacheFileInfo(cache.fs, path)
	if err != nil || !info.Mode().IsRegular() {
		return models.MediaImageResponse{}, false
	}
	cache.touchThumbFile(path, info)
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

// cachedMediaImageResponse serves the cached thumbnail for sourceKey, and
// returns the file it served so the caller can memoize it.
func cachedMediaImageResponse(
	cache *mediaThumbCache,
	sourceKey string,
	typeTag string,
	maxSize int,
	sourcePath string,
	localPath bool,
) (response models.MediaImageResponse, resolved resolvedThumb, found bool) {
	path, contentType, found := cache.lookupPath(sourceKey, maxSize)
	if !found {
		return models.MediaImageResponse{}, resolvedThumb{}, false
	}
	response, found = thumbFileResponse(cache, path, contentType, typeTag, sourcePath, localPath)
	if !found {
		return models.MediaImageResponse{}, resolvedThumb{}, false
	}
	return response, resolvedThumb{typeTag: typeTag, path: path, contentType: contentType}, true
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
	// uses (cache keys, resolved-thumbnail memo, resize) see the snapped value.
	if ref.MaxSize != nil {
		snapped := snapThumbMaxSize(*ref.MaxSize)
		ref.MaxSize = &snapped
		maxSizeForLog = int(snapped)
	}
	prefs := imagePrefs(nil, ref.ImageTypes)

	// cache is nil unless this request resizes and a thumbnail cache exists.
	var cache *mediaThumbCache
	maxSize := 0
	if ref.MaxSize != nil && *ref.MaxSize > 0 {
		maxSize = int(*ref.MaxSize)
		cache = mediaThumbCachePointer.Load()
	}

	// Pre-semaphore fast path: if this exact request was served before and the
	// disk thumbnail is still present, return it without loading the original.
	if cache != nil {
		if resolved, ok := cache.getResolvedThumb(ref, prefs, maxSize); ok {
			if response, found := thumbFileResponse(
				cache, resolved.path, resolved.contentType, resolved.typeTag, "", localPath,
			); found {
				return response, nil
			}
		}
	}

	noImageKey := mediaImageNoImageRequestKey(ref, prefs)
	if ok, cachedErr := mediaImageNoImages.get(noImageKey); ok {
		return nil, cachedMediaImageNoImageError(cachedErr)
	}

	// Recorded fast path: after a restart the in-memory memo is empty, but the
	// cover's resolved type and source are recorded in MediaDB. Confirm they
	// still match what a full lookup would pick and serve the disk thumbnail
	// without reading the original artwork or waiting for the lookup semaphore.
	if cache != nil {
		if response, found := recordedThumbResponse(&env, cache, ref, prefs, maxSize, localPath); found {
			return response, nil
		}
	}

	var probe mediaImageCacheProbe
	if cache != nil {
		probe = func(sourceKey string) bool {
			_, _, found := cache.lookupPath(sourceKey, maxSize)
			return found
		}
	}
	raw, err := loadRawMediaImageLimited(&env, ref, prefs, noImageKey, probe)
	if err != nil {
		return nil, err
	}

	if cache != nil {
		if response, resolved, found := cachedMediaImageResponse(
			cache, raw.sourceKey, raw.typeTag, maxSize, raw.text, localPath,
		); found {
			resolved.system = raw.system
			cache.setResolvedThumb(ref, prefs, maxSize, resolved)
			recordCachedCoverThumb(&env, cache, raw, prefs, maxSize)
			return response, nil
		}
	}
	if raw.cached {
		// The probe found a thumbnail that has gone since: read the source.
		raw, err = loadRawMediaImageLimited(&env, ref, prefs, noImageKey, nil)
		if err != nil {
			return nil, err
		}
	}

	binary, contentType := raw.binary, raw.contentType
	if maxSize > 0 {
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
			writeStart := time.Now()
			path, cacheErr := cache.set(raw.sourceKey, maxSize, binary, contentType)
			log.Debug().Dur("duration", time.Since(writeStart)).Int("bytes", len(binary)).
				Msg("media.image: wrote thumbnail cache file")
			if cacheErr != nil {
				log.Debug().Err(cacheErr).Msg("media.image: failed to materialize thumbnail cache file")
			} else {
				cache.setResolvedThumb(ref, prefs, maxSize, resolvedThumb{
					system: raw.system, typeTag: raw.typeTag, path: path, contentType: contentType,
				})
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

// loadRawMediaImageLimited resolves and loads a request's image under the
// lookup semaphore, caching a no-image result for later requests.
func loadRawMediaImageLimited(
	env *requests.RequestEnv,
	ref mediaRefParam,
	prefs []string,
	noImageKey string,
	probe mediaImageCacheProbe,
) (*rawMediaImage, error) {
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

	maxBytes := MediaImageMaxBytes(env.Platform)
	var raw *rawMediaImage
	var err error
	switch {
	case isDirectoryMediaRef(ref):
		raw, err = loadRawMediaImageDirectoryRef(env, ref, prefs, maxBytes, probe)
	case ref.MediaID == nil:
		raw, err = loadRawMediaImageSinglePath(env, ref, prefs, maxBytes, probe)
	default:
		raw, err = loadRawMediaImageByID(env, ref, prefs, maxBytes, probe)
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
	return raw, nil
}

// recordedThumbResponse serves a cached thumbnail using the cover recorded in
// MediaDB, without loading the original image. It reports false whenever the
// record cannot be proven to match what a full lookup would resolve, and the
// caller then takes the full path.
func recordedThumbResponse(
	env *requests.RequestEnv,
	cache *mediaThumbCache,
	ref mediaRefParam,
	prefs []string,
	maxSize int,
	localPath bool,
) (models.MediaImageResponse, bool) {
	// A directory's own artwork outranks the cover recorded for its media.
	if env.Database == nil || env.Database.MediaDB == nil || isDirectoryMediaRef(ref) {
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
	if !recordedSourceCurrent(afero.NewOsFs(), requestImageDir(env), &thumb) {
		return models.MediaImageResponse{}, false
	}
	response, resolved, cached := cachedMediaImageResponse(
		cache, thumb.SourceKey, thumb.TypeTag, maxSize, "", localPath,
	)
	if !cached {
		return models.MediaImageResponse{}, false
	}
	resolved.system = thumb.SystemID
	cache.setResolvedThumb(ref, prefs, maxSize, resolved)
	return response, true
}

// recordedSourceCurrent reports whether a recorded cover's source is still
// the image a full lookup would load. Only file-backed sources qualify: one
// stat proves the file the recorded type resolves to now is the one recorded,
// with the same size and modification time. Anything else needs the full path.
// An images:// source is statted in imageDir but keyed by its images:// path,
// as loadMediaImageFile keys it.
func recordedSourceCurrent(fs afero.Fs, imageDir string, thumb *database.MediaCoverThumb) bool {
	if !isFileThumbSourceKey(thumb.SourceKey) || thumb.SourcePath == "" ||
		platforms.IsSourcePath(thumb.SourcePath) {
		return false
	}
	statPath := thumb.SourcePath
	if config.IsImagePath(statPath) {
		resolved, ok := config.ResolveImagePath(imageDir, statPath)
		if !ok {
			return false
		}
		statPath = resolved
	}
	info, err := fs.Stat(statPath)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return fileThumbSourceKey(thumb.SourcePath, info) == thumb.SourceKey
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
		context.WithoutCancel(env.Context), raw.mediaDBID, raw.typeTag, raw.sourceKey, avgColor,
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
	raw *rawMediaImage,
	prefs []string,
	maxSize int,
) {
	if !coverRecordEligible(env, raw, prefs) {
		return
	}
	var avgColor *uint32
	if data, contentType, found := cache.read(raw.sourceKey, maxSize); found {
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
	if err := validateDirectoryMediaRef(ref); err != nil {
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
	probe mediaImageCacheProbe,
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
		return loadRawMediaImageRow(env, row, prefs, maxBytes, probe)
	}

	raw, directoryFileBacked, err := loadRawMediaImageDirectory(env, system, ref.Path, prefs, maxBytes, probe)
	if raw != nil || err != nil {
		return raw, err
	}

	row, err := resolveMediaBySystemAndPath(env, ref.System, ref.Path)
	if err != nil {
		return nil, err
	}
	raw, err = loadRawMediaImageRow(env, row, prefs, maxBytes, probe)
	return raw, markFileBackedCandidatesChecked(err, directoryFileBacked)
}

// loadRawMediaImageDirectory loads a directory's own artwork. It returns nil
// and no error when the directory has none of the requested types, and
// reports whether that lookup checked file-backed candidates, so the caller's
// final no-image result stays out of the no-image cache.
func loadRawMediaImageDirectory(
	env *requests.RequestEnv,
	system database.System,
	path string,
	prefs []string,
	maxBytes int64,
	probe mediaImageCacheProbe,
) (raw *rawMediaImage, fileBackedCandidatesChecked bool, err error) {
	db := env.Database.MediaDB
	directoryProps, err := db.GetDirectoryProperties(env.Context, system.DBID, path)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get directory properties: %w", err)
	}
	if len(directoryProps) == 0 {
		return nil, false, nil
	}
	directoryRow := &database.MediaFullRow{
		Path: path,
		System: database.System{
			DBID: system.DBID, SystemID: system.SystemID, Name: system.Name,
		},
	}
	raw, err = selectRawMediaImageFromSources(
		env.Context, afero.NewOsFs(), requestImageDir(env), env.Platform, db, directoryRow, nil, directoryProps,
		prefs, maxBytes, probe,
	)
	if notFound, fileBacked := mediaImageNotFound(err); notFound {
		return nil, fileBacked, nil
	}
	return raw, false, err
}

// markFileBackedCandidatesChecked marks a no-image result as having checked
// file-backed candidates when an earlier lookup for the same request did.
func markFileBackedCandidatesChecked(err error, checked bool) error {
	if !checked {
		return err
	}
	var noImage *mediaImageNotFoundError
	if errors.As(err, &noImage) {
		noImage.fileBackedCandidatesChecked = true
	}
	return err
}

// loadRawMediaImageDirectoryRef loads the image for a directory entry and the
// media it launches: the directory's own artwork, then that media's, then the
// artwork of the media the path alone resolves to. The last keeps a
// multi-disc folder's tile from going blank when the disc it launches has no
// artwork of its own but the first disc does, which is what media.browse's
// hasCover reports.
func loadRawMediaImageDirectoryRef(
	env *requests.RequestEnv,
	ref mediaRefParam,
	prefs []string,
	maxBytes int64,
	probe mediaImageCacheProbe,
) (*rawMediaImage, error) {
	resolved, err := resolveMediaRefs(env, []mediaRefParam{ref})
	if err != nil {
		return nil, err
	}
	if resolved[0].Err != nil {
		return nil, resolved[0].Err
	}
	row := resolved[0].Row

	raw, directoryFileBacked, err := loadRawMediaImageDirectory(env, row.System, ref.Path, prefs, maxBytes, probe)
	if raw != nil || err != nil {
		return raw, err
	}

	raw, err = loadRawMediaImageRow(env, row, prefs, maxBytes, probe)
	notFound, fileBacked := mediaImageNotFound(err)
	if !notFound {
		return raw, err
	}
	fileBacked = fileBacked || directoryFileBacked
	notFoundErr := markFileBackedCandidatesChecked(err, fileBacked)

	pathRow, pathErr := resolveMediaBySystemAndPath(env, ref.System, ref.Path)
	if pathErr != nil {
		var clientErr *models.ClientError
		if errors.As(pathErr, &clientErr) {
			return nil, notFoundErr
		}
		return nil, pathErr
	}
	if pathRow.DBID == row.DBID {
		return nil, notFoundErr
	}
	raw, err = loadRawMediaImageRow(env, pathRow, prefs, maxBytes, probe)
	if notFound, pathFileBacked := mediaImageNotFound(err); notFound {
		return nil, mediaImageNoImageError(row, fileBacked || pathFileBacked)
	}
	return raw, err
}

func loadRawMediaImageRow(
	env *requests.RequestEnv,
	row *database.MediaFullRow,
	prefs []string,
	maxBytes int64,
	probe mediaImageCacheProbe,
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
		env.Context, afero.NewOsFs(), requestImageDir(env), env.Platform, db, row, mediaPropSources, titleProps,
		prefs, maxBytes, probe,
	)
}

func loadRawMediaImageByID(
	env *requests.RequestEnv,
	ref mediaRefParam,
	prefs []string,
	maxBytes int64,
	probe mediaImageCacheProbe,
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
		env.Context, afero.NewOsFs(), requestImageDir(env), env.Platform, db, row, mediaPropSources, titleProps,
		prefs, maxBytes, probe,
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
	imageDir string,
	platform platforms.Platform,
	db database.MediaDBI,
	row *database.MediaFullRow,
	mediaPropSources [][]database.MediaProperty,
	titleProps []database.MediaProperty,
	prefs []string,
	maxBytes int64,
	probe mediaImageCacheProbe,
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
			raw, stale, err := loadRawMediaImageProperty(
				ctx, fs, imageDir, platform, db, row, &prop, src, typeTag, maxBytes, probe,
			)
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
	imageDir string,
	platform platforms.Platform,
	db database.MediaDBI,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	src mediaImageSource,
	typeTag string,
	maxBytes int64,
	probe mediaImageCacheProbe,
) (*rawMediaImage, bool, error) {
	var binary []byte
	var sourceKey string
	contentType := mediaContentType(prop.ContentType, prop.Text)

	switch {
	case len(prop.Binary) > 0:
		if int64(len(prop.Binary)) > maxBytes {
			return nil, false, mediaImageBlobTooLargeError(prop, maxBytes)
		}
		binary = prop.Binary
		sourceKey = dataThumbSourceKey(contentType, binary)
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
		sourceKey = dataThumbSourceKey(contentType, binary)
	case prop.Text != "":
		file, stale, err := loadMediaImageFile(
			ctx, fs, imageDir, platform, row, prop, src.isMedia, typeTag, maxBytes, probe,
		)
		if stale || err != nil {
			return nil, stale, err
		}
		binary = file.data
		sourceKey = file.sourceKey
	default:
		return nil, false, nil
	}

	return &rawMediaImage{
		binary:      binary,
		contentType: contentType,
		text:        prop.Text,
		typeTag:     typeTag,
		system:      row.System.SystemID,
		sourceKey:   sourceKey,
		mediaDBID:   row.DBID,
		cached:      binary == nil,
	}, false, nil
}

func loadMediaImageFile(
	ctx context.Context,
	fs afero.Fs,
	imageDir string,
	platform platforms.Platform,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	isMedia bool,
	typeTag string,
	maxBytes int64,
	probe mediaImageCacheProbe,
) (file mediaImageFile, stale bool, err error) {
	if platforms.IsSourcePath(prop.Text) {
		data, sourceStale, sourceErr := loadSourceMediaImageFile(ctx, platform, row, prop, isMedia, typeTag, maxBytes)
		if sourceStale || sourceErr != nil {
			return mediaImageFile{}, sourceStale, sourceErr
		}
		return mediaImageFile{
			data:      data,
			sourceKey: dataThumbSourceKey(mediaContentType(prop.ContentType, prop.Text), data),
		}, false, nil
	}
	// An images:// path names a file in the image folder. It is read from
	// there, but keyed and reported by its images:// path.
	path := prop.Text
	if config.IsImagePath(path) {
		resolved, ok := config.ResolveImagePath(imageDir, path)
		if !ok {
			logStaleMediaImageProperty(row, prop, isMedia, typeTag, "image path outside the image folder")
			return mediaImageFile{}, true, nil
		}
		path = resolved
	}
	info, err := fs.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logStaleMediaImageProperty(row, prop, isMedia, typeTag, err.Error())
			return mediaImageFile{}, true, nil
		}
		return mediaImageFile{}, false, mediaImageReadError(path, err)
	}
	if !info.Mode().IsRegular() {
		err := fmt.Errorf("media binary file %q is not a regular file", path)
		return mediaImageFile{}, false, mediaImageReadError(path, err)
	}
	if info.Size() > maxBytes {
		return mediaImageFile{}, false, mediaImageReadError(path, &mediaBinaryTooLargeError{
			path: path,
			size: info.Size(),
			max:  maxBytes,
		})
	}
	file.sourceKey = fileThumbSourceKey(prop.Text, info)
	if probe != nil && probe(file.sourceKey) {
		return file, false, nil
	}
	readStart := time.Now()
	data, readErr := readMediaBinaryFile(fs, path, maxBytes)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			logStaleMediaImageProperty(row, prop, isMedia, typeTag, readErr.Error())
			return mediaImageFile{}, true, nil
		}
		return mediaImageFile{}, false, mediaImageReadError(path, readErr)
	}
	log.Debug().Dur("duration", time.Since(readStart)).Int("bytes", len(data)).Str("path", path).
		Msg("media.image: read source image")
	file.data = data
	return file, false, nil
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

// MediaImageMaxBytes is the largest image a media image request reads on pl.
func MediaImageMaxBytes(pl platforms.Platform) int64 {
	if pl != nil && pl.ID() == ids.Mister {
		return misterMediaImageMaxBytes
	}
	return defaultMediaImageMaxBytes
}
