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

// Package esmedia resolves local EmulationStation-style media folders and paths.
package esmedia

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/spf13/afero"
)

// File is a local media file discovered in an EmulationStation media folder.
type File struct {
	Path        string
	ContentType string
}

// ArtworkExtensions are checked in order when deriving artwork filenames.
//
//nolint:gochecknoglobals // Package-level EmulationStation media convention.
var ArtworkExtensions = []string{".png", ".jpg", ".jpeg", ".webp"}

// ArtworkDirCandidates maps each TagPropertyImage value to ordered media
// sub-directory names under <systemRootPath>/media/.
//
//nolint:gochecknoglobals // Package-level EmulationStation media convention.
var ArtworkDirCandidates = map[string][]string{
	string(tags.TagPropertyImageImage): {"image", "images", "miximages", "custom"},
	string(tags.TagPropertyImageBoxart): {
		"boxart", "boxart2d", "box2d", "boxart2dfront", "box2dfront", "cover", "covers",
	},
	string(tags.TagPropertyImageBoxart3D):   {"boxart3d", "3dbox", "3dboxes"},
	string(tags.TagPropertyImageBoxartSide): {"boxart2dside"},
	string(tags.TagPropertyImageBoxartBack): {"boxart2dback", "backcover", "backcovers"},
	string(tags.TagPropertyImageScreenshot): {"screenshot", "screenshots"},
	string(tags.TagPropertyImageThumbnail): {
		"thumbnail", "thumbnails", "box2dfront", "boxart2dfront", "supporttexture",
	},
	string(tags.TagPropertyImageMarquee): {"marquee", "marquees"},
	string(tags.TagPropertyImageWheel):   {"wheel", "wheels", "logo", "logos"},
	string(tags.TagPropertyImageFanart):  {"fanart", "fanarts"},
	string(tags.TagPropertyImageTitleshot): {
		"titleshot", "titleshots", "titlescreen", "titlescreens", "screenshottitle",
	},
	string(tags.TagPropertyImageMap): {"map", "maps"},
}

// StatMediaDirs reads <rootPath>/media and returns subdirectory name to path.
func StatMediaDirs(rootPath string) map[string]string {
	return StatMediaDirsFS(afero.NewOsFs(), rootPath)
}

// StatMediaDirsFS reads <rootPath>/media using fs and returns subdirectory name to path.
func StatMediaDirsFS(fs afero.Fs, rootPath string) map[string]string {
	mediaRoot := filepath.Join(rootPath, "media")
	entries, err := afero.ReadDir(fs, mediaRoot)
	if err != nil {
		return nil
	}
	dirs := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = filepath.Join(mediaRoot, e.Name())
		}
	}
	return dirs
}

// ArtworkFallbackNames returns candidate artwork filenames for gamePath under
// systemRootPath. For games in subdirectories, mirrored paths are checked before
// flat filenames.
func ArtworkFallbackNames(gamePath, systemRootPath string) []string {
	resolved := ResolvePath(gamePath, systemRootPath)
	if resolved == "" {
		return nil
	}

	rootAbs, err := filepath.Abs(systemRootPath)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(resolved))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}

	stem := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	if stem == "" || stem == "." {
		return nil
	}

	flatNames := FallbackArtworkNames(stem)
	dir := filepath.Dir(rel)
	if dir == "." || dir == "" {
		return flatNames
	}

	fallbackNames := make([]string, 0, len(flatNames)*2)
	for _, flat := range flatNames {
		nested := filepath.Join(dir, flat)
		if nested != flat {
			fallbackNames = append(fallbackNames, nested)
		}
	}
	fallbackNames = append(fallbackNames, flatNames...)
	return fallbackNames
}

// FallbackArtworkNames returns <stem> plus each supported artwork extension.
func FallbackArtworkNames(stem string) []string {
	if stem == "" || stem == "." {
		return nil
	}
	names := make([]string, 0, len(ArtworkExtensions))
	for _, ext := range ArtworkExtensions {
		names = append(names, stem+ext)
	}
	return names
}

// FindFileFS searches candidate directories for fallbackNames in order.
func FindFileFS(
	fs afero.Fs,
	fallbackNames []string,
	candidates []string,
	availableDirs map[string]string,
) *File {
	if len(fallbackNames) == 0 {
		return nil
	}
	for _, dir := range candidates {
		dirPath, ok := availableDirs[dir]
		if !ok {
			continue
		}
		for _, name := range fallbackNames {
			cleanName := filepath.Clean(name)
			if name == "" || cleanName == "." || cleanName == ".." ||
				strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
				continue
			}
			candidate := filepath.Join(dirPath, name)
			if exists, err := afero.Exists(fs, candidate); err == nil && exists {
				return &File{Path: filepath.ToSlash(candidate), ContentType: MimeFromExt(candidate)}
			}
		}
	}
	return nil
}

// MimeFromExt returns a MIME type based on file extension.
func MimeFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/avi"
	case ".pdf":
		return "application/pdf"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".m4b":
		return "audio/mp4"
	case ".mpg", ".mpeg":
		return "video/mpeg"
	case ".m4v":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

// ResolvePath converts an EmulationStation path to an absolute path under
// systemRootPath. Paths that cannot be resolved or escape systemRootPath return
// an empty string.
func ResolvePath(esPath, systemRootPath string) string {
	abs, ok := ResolvePathAbs(esPath, systemRootPath)
	if !ok || !PathWithinRoot(abs, systemRootPath) {
		return ""
	}
	return abs
}

// ResolvePathAbs converts an EmulationStation path to an absolute filesystem
// path without enforcing that the result stays inside systemRootPath.
func ResolvePathAbs(esPath, systemRootPath string) (string, bool) {
	if esPath == "" {
		return "", false
	}
	rootAbs, err := filepath.Abs(systemRootPath)
	if err != nil {
		return "", false
	}
	rootAbs = filepath.Clean(rootAbs)

	var abs string
	switch {
	case strings.HasPrefix(esPath, "~/"):
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", false
		}
		abs = filepath.Join(home, esPath[2:])
	case filepath.IsAbs(esPath):
		abs = filepath.Clean(esPath)
	default:
		rel := strings.TrimPrefix(esPath, "./")
		abs = filepath.Join(rootAbs, rel)
	}

	abs, err = filepath.Abs(abs)
	if err != nil || !filepath.IsAbs(abs) {
		return "", false
	}
	return filepath.Clean(abs), true
}

// PathWithinRoot reports whether path is inside root after absolute-path cleanup.
func PathWithinRoot(path, root string) bool {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs = filepath.Clean(pathAbs)
	rootAbs = filepath.Clean(rootAbs)
	rel, err := filepath.Rel(rootAbs, pathAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
