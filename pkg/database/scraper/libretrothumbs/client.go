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

package libretrothumbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// DefaultBaseURL is the libretro thumbnail server RetroArch downloads from.
const DefaultBaseURL = "https://thumbnails.libretro.com"

const (
	indexMaxAge   = 7 * 24 * time.Hour
	indexMaxBytes = 64 << 20
	imageMaxBytes = 16 << 20
	requestWait   = 60 * time.Second
)

// kinds are the thumbnail folders imported, in download order.
var kinds = []string{"Named_Boxarts", "Named_Snaps", "Named_Titles"}

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// errNotFound is a thumbnail the server does not have.
var errNotFound = errors.New("thumbnail not found")

// client fetches playlist indexes and images and keeps both under dir, so a
// later run reuses them instead of downloading again.
type client struct {
	http *http.Client
	fs   afero.Fs
	now  func() time.Time
	// local maps a lowercased thumbnail name, per playlist and kind folder, to
	// the spelling of the PNG already stored there. localNames fills it while
	// an index is built, so image can find a file whose name differs from the
	// listed one only in letter case without listing the folder again.
	local   map[string]string
	baseURL string
	dir     string
	localMu syncutil.RWMutex
}

func newClient(fs afero.Fs, dir, baseURL string) *client {
	return &client{
		http: &http.Client{Timeout: requestWait, Transport: useragent.Transport(nil)},
		fs:   fs, now: time.Now, local: make(map[string]string),
		baseURL: strings.TrimRight(baseURL, "/"), dir: dir,
	}
}

func (c *client) get(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request %s: status %d", rawURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("read %s: larger than %d bytes", rawURL, maxBytes)
	}
	return data, nil
}

func (c *client) folderURL(playlist, kind string) string {
	return c.baseURL + "/" + url.PathEscape(playlist) + "/" + kind + "/"
}

// writeFile replaces path atomically, so an interrupted run never leaves a
// truncated file that a later run would reuse. The temporary name is unique
// per call, not just per path: region variants and title-slug matches often
// resolve to the same thumbnail, so two workers can write it at once, and a
// shared "path.part" would have one rename fail (or the file mix both
// writers' bytes) instead of both succeeding.
func (c *client) writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := c.fs.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmpFile, err := afero.TempFile(c.fs, dir, filepath.Base(path)+".*.part")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmp := tmpFile.Name()
	_, writeErr := tmpFile.Write(data)
	if closeErr := tmpFile.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = c.fs.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, writeErr)
	}
	if err := c.fs.Rename(tmp, path); err != nil {
		_ = c.fs.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

var indexEntry = regexp.MustCompile(`href="([^"/?]+)\.png"`)

// parseIndex reads the thumbnail names from the server's directory listing.
// A name is rejected, not just its raw href: unescaping can turn an encoded
// separator or ".." into a real one, and a rejected name is later joined
// onto a local directory to read or write a file.
func parseIndex(listing []byte) []string {
	matches := indexEntry.FindAllSubmatch(listing, -1)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		name, err := url.PathUnescape(string(m[1]))
		if err != nil {
			continue
		}
		names = append(names, name)
	}
	return validThumbnailNames(names)
}

// validThumbnailName reports whether a decoded thumbnail name is safe to
// join onto a local directory: non-empty, no path separator, and not a
// "." or ".." component.
func validThumbnailName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, "/\\")
}

// validThumbnailNames filters names to the ones validThumbnailName accepts,
// the check every path a thumbnail name reaches this client from (a fresh
// listing or a cached one) shares before it is joined onto a local directory.
func validThumbnailNames(names []string) []string {
	kept := names[:0]
	for _, name := range names {
		if validThumbnailName(name) {
			kept = append(kept, name)
		}
	}
	return kept
}

// index returns a playlist's thumbnail names: the server's listing (from a
// cache younger than a week when there is one) plus every PNG already on disk
// under the playlist's thumbnail folders, so a picture a user dropped in is
// matched without being listed anywhere. Every kind shares the box art names.
// An unreachable server only fails the playlist when nothing local can stand
// in for it.
func (c *client) index(ctx context.Context, playlist string) (*index, error) {
	local := c.localNames(playlist)
	listed, err := c.listing(ctx, playlist)
	if err != nil {
		if len(local) == 0 {
			return nil, err
		}
		log.Warn().Err(err).Str("playlist", playlist).
			Msg("libretro thumbnail listing unavailable, using local thumbnails only")
		listed = nil
	}
	return newIndex(mergeNames(listed, local)), nil
}

// listing returns the server's names for a playlist, cached in index.txt. A
// cache older than a week is refreshed, and is still used when the refresh
// fails, so being offline does not discard names already known.
func (c *client) listing(ctx context.Context, playlist string) ([]string, error) {
	cache := filepath.Join(c.dir, playlist, "index.txt")
	var cached []string
	var haveCache, fresh bool
	if info, err := c.fs.Stat(cache); err == nil {
		if data, readErr := afero.ReadFile(c.fs, cache); readErr == nil {
			cached, haveCache = parseCachedIndex(data), true
			fresh = c.now().Sub(info.ModTime()) < indexMaxAge
		}
	}
	if fresh {
		return cached, nil
	}
	listing, err := c.get(ctx, c.folderURL(playlist, kinds[0]), indexMaxBytes)
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		if haveCache {
			log.Warn().Err(err).Str("playlist", playlist).Msg("libretro thumbnail refresh failed, using older listing")
			return cached, nil
		}
		return nil, err
	}
	names := parseIndex(listing)
	if err := c.writeFile(cache, []byte(strings.Join(names, "\n")+"\n")); err != nil {
		return nil, err
	}
	return names, nil
}

// parseCachedIndex reads names from index.txt. Each line is trimmed, so a file
// saved with Windows line endings or stray spaces still matches. A name is
// validated again, not trusted as already clean: it was, but a tampered or
// pre-fix cache file must not defeat the same check parseIndex applies to a
// fresh listing.
func parseCachedIndex(data []byte) []string {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return validThumbnailNames(lines)
}

// localNames lists the thumbnails already stored for a playlist, under any
// kind's folder, as names without the .png extension.
func (c *client) localNames(playlist string) []string {
	seen := make(map[string]struct{})
	var names []string
	foundKind := false
	for _, kind := range kinds {
		entries, err := afero.ReadDir(c.fs, filepath.Join(c.dir, playlist, kind))
		if err != nil {
			continue
		}
		foundKind = true
		for _, entry := range entries {
			name, ok := strings.CutSuffix(entry.Name(), ".png")
			if !ok || entry.IsDir() || entry.Size() == 0 || !validThumbnailName(name) {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
			c.rememberLocal(playlist, kind, name)
		}
	}
	if !foundKind {
		c.logUnrecognisedFolders(playlist)
	}
	return names
}

func localKey(playlist, kind, name string) string {
	return playlist + "\x00" + kind + "\x00" + strings.ToLower(name)
}

// rememberLocal records the spelling of a PNG found in a kind's folder. When
// two files differ only in case, the first listed wins.
func (c *client) rememberLocal(playlist, kind, name string) {
	key := localKey(playlist, kind, name)
	c.localMu.Lock()
	defer c.localMu.Unlock()
	if _, taken := c.local[key]; !taken {
		c.local[key] = name
	}
}

// localSpelling returns the name of a PNG already stored in the kind's folder
// that equals name ignoring letter case. The folder and the ".png" suffix stay
// case-sensitive.
func (c *client) localSpelling(playlist, kind, name string) (string, bool) {
	c.localMu.RLock()
	defer c.localMu.RUnlock()
	actual, ok := c.local[localKey(playlist, kind, name)]
	return actual, ok
}

// logUnrecognisedFolders says what a playlist's folder holds when none of it
// is a thumbnail folder Core reads, which is what a wrong folder name looks
// like (the names are case-sensitive).
func (c *client) logUnrecognisedFolders(playlist string) {
	entries, err := afero.ReadDir(c.fs, filepath.Join(c.dir, playlist))
	if err != nil {
		return
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	if len(dirs) > 0 {
		log.Debug().Str("playlist", playlist).Strs("folders", dirs).Strs("expected", kinds).
			Msg("libretro thumbnails: no recognised thumbnail folder")
	}
}

// mergeNames returns listed followed by the local names it does not already
// hold.
func mergeNames(listed, local []string) []string {
	if len(local) == 0 {
		return listed
	}
	have := make(map[string]struct{}, len(listed))
	for _, name := range listed {
		have[name] = struct{}{}
	}
	merged := listed[:len(listed):len(listed)]
	for _, name := range local {
		if _, ok := have[name]; !ok {
			merged = append(merged, name)
		}
	}
	return merged
}

// image returns the local path of one thumbnail, downloading it unless an
// earlier run already did (or force asks again). It answers errNotFound when
// the server has no such image.
func (c *client) image(ctx context.Context, playlist, kind, name string, force bool) (string, error) {
	path := filepath.Join(c.dir, playlist, kind, name+".png")
	if info, err := c.fs.Stat(path); err == nil && info.Size() > 0 && !force {
		return path, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	// A file the user saved with different letter casing than the listed name
	// is the same thumbnail; fetching it again would ignore their copy.
	if actual, ok := c.localSpelling(playlist, kind, name); ok && actual != name && !force {
		alt := filepath.Join(c.dir, playlist, kind, actual+".png")
		if info, err := c.fs.Stat(alt); err == nil && info.Size() > 0 {
			return alt, nil
		}
	}
	data, err := c.get(ctx, c.folderURL(playlist, kind)+url.PathEscape(name+".png"), imageMaxBytes)
	if err != nil {
		return "", err
	}
	if !bytes.HasPrefix(data, pngMagic) {
		return "", fmt.Errorf("%s/%s/%s is not a PNG image", playlist, kind, name)
	}
	if err := c.writeFile(path, data); err != nil {
		return "", err
	}
	return path, nil
}
