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

package config

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
)

const (
	// ImagesDir is the default image folder under Core's data directory.
	ImagesDir = "images"
	// ImageLibretroDir holds the libretro thumbnail scraper's downloads and
	// the pictures a user drops in beside them.
	ImageLibretroDir = "libretro"
	// ImageThumbsDir holds the generated thumbnail cache. Core owns it and
	// removes anything in it that is not the live cache version.
	ImageThumbsDir = "thumbs"
	// ImageScheme names an image stored under the image folder by its path
	// relative to it, so the folder can move without rewriting the database:
	// images://libretro/<playlist>/<kind>/<name>.png.
	ImageScheme = "images"
	// DefaultThumbCacheMaxMB caps the generated thumbnail cache when
	// [media] thumb_cache_max_mb is unset.
	DefaultThumbCacheMaxMB = 1024
	// MaxThumbCacheMaxMB bounds the setting so the byte count cannot overflow.
	MaxThumbCacheMaxMB = 1024 * 1024
)

const imageSchemePrefix = ImageScheme + "://"

// IsImagePath reports whether text is an images:// path.
func IsImagePath(text string) bool {
	return strings.HasPrefix(text, imageSchemePrefix)
}

// ImagePath returns the images:// path of a file under the image folder,
// given its slash-separated path relative to that folder.
func ImagePath(rel string) string {
	return imageSchemePrefix + strings.TrimPrefix(path.Clean("/"+rel), "/")
}

// ResolveImagePath returns the file an images:// path names under imageDir.
// It answers false for anything that is not an images:// path, for an empty
// or absolute imageDir, and for a path that would leave imageDir, since the
// text comes from the database and is not trusted to stay inside it.
func ResolveImagePath(imageDir, text string) (string, bool) {
	rel, ok := strings.CutPrefix(text, imageSchemePrefix)
	if !ok || imageDir == "" || !filepath.IsAbs(imageDir) || rel == "" {
		return "", false
	}
	if strings.Contains(rel, "\\") || strings.HasPrefix(rel, "/") {
		return "", false
	}
	for segment := range strings.SplitSeq(rel, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", false
		}
	}
	return filepath.Join(imageDir, filepath.FromSlash(rel)), true
}

// ResolveImageDir is ImageDir for a configured value. A relative value
// resolves like launchers.media_dir, against the executable's folder, and the
// result is always absolute because thumbnail paths handed to clients must be.
func ResolveImageDir(configured, dataDir string) string {
	dir := filepath.Join(dataDir, ImagesDir)
	if configured = strings.TrimSpace(configured); configured != "" {
		dir = pathutil.ResolveRelativePath(configured)
	}
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return filepath.Clean(dir)
}

// ImageDir returns the folder images are stored in: [media] image_dir, or
// "images" under dataDir when it is unset.
func (c *Instance) ImageDir(dataDir string) string {
	c.mu.RLock()
	configured := c.vals.Media.ImageDir
	c.mu.RUnlock()
	return ResolveImageDir(configured, dataDir)
}

// ImageDirSetting returns [media] image_dir as written, "" when unset.
func (c *Instance) ImageDirSetting() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Media.ImageDir
}

// SetImageDir sets [media] image_dir. An empty value restores the default.
func (c *Instance) SetImageDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Media.ImageDir = strings.TrimSpace(dir)
}

// ThumbCacheMaxMB returns the generated thumbnail cache's size cap in MiB.
func (c *Instance) ThumbCacheMaxMB() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Media.ThumbCacheMaxMB <= 0 {
		return DefaultThumbCacheMaxMB
	}
	return min(c.vals.Media.ThumbCacheMaxMB, MaxThumbCacheMaxMB)
}

// ThumbCacheMaxBytes is ThumbCacheMaxMB in bytes.
func (c *Instance) ThumbCacheMaxBytes() int64 {
	return int64(c.ThumbCacheMaxMB()) << 20
}

// SetThumbCacheMaxMB sets [media] thumb_cache_max_mb. Zero or less restores
// the default.
func (c *Instance) SetThumbCacheMaxMB(mb int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Media.ThumbCacheMaxMB = max(mb, 0)
}
