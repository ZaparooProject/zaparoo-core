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

// Package library reads a ROM tree laid out the ES-DE way, a folder per
// system under one root, so fakes can serve what is in it.
package library

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
)

// File is one ROM file in the tree.
type File struct {
	// Folder is the ES-DE system folder the file is under, such as "snes".
	Folder string
	// SystemID is the Zaparoo system the folder maps to.
	SystemID string
	// Rel is the file's path below its system folder, slash-separated.
	Rel string
	// Path is the file's full path on disk.
	Path string
	// Name is the file's base name.
	Name string
	// Size is the file's size in bytes.
	Size int64
}

// Scan lists every ROM file under root, sorted by folder then path. A folder
// that is not an ES-DE system is skipped, and so are hidden entries, media
// folders and files without one of the system's extensions.
func Scan(root string) ([]File, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read library root: %w", err)
	}
	var files []File
	for _, entry := range entries {
		if !entry.IsDir() || hidden(entry.Name()) {
			continue
		}
		info, ok := esde.LookupByFolderName(entry.Name())
		if !ok {
			continue
		}
		found, err := scanSystem(filepath.Join(root, entry.Name()), entry.Name(), info)
		if err != nil {
			return nil, err
		}
		files = append(files, found...)
	}
	slices.SortFunc(files, func(a, b File) int {
		if c := strings.Compare(a.Folder, b.Folder); c != 0 {
			return c
		}
		return strings.Compare(a.Rel, b.Rel)
	})
	return files, nil
}

func scanSystem(dir, folder string, info esde.SystemInfo) ([]File, error) {
	var files []File
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		if hidden(d.Name()) || (d.IsDir() && skippedDir(d.Name())) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !wanted(d.Name(), info.Extensions) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", p, err)
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return fmt.Errorf("relative path of %s: %w", p, err)
		}
		files = append(files, File{
			Folder:   folder,
			SystemID: info.SystemID,
			Rel:      filepath.ToSlash(rel),
			Path:     p,
			Name:     d.Name(),
			Size:     fi.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	return files, nil
}

func hidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func skippedDir(name string) bool {
	switch strings.ToLower(name) {
	case "media", "images", "videos", "manuals", "downloaded_images", "downloaded_media":
		return true
	}
	return false
}

func wanted(name string, exts []string) bool {
	if len(exts) == 0 {
		return !strings.EqualFold(name, "gamelist.xml")
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext != "" && slices.Contains(exts, ext)
}

// BaseName is a file name without its extension.
func BaseName(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// PNG is a small, valid PNG whose colour is derived from seed, so different
// titles get visibly different images.
func PNG(seed string) []byte {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	sum := h.Sum(nil)
	fill := color.RGBA{R: sum[0], G: sum[1], B: sum[2], A: 0xff}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(fmt.Sprintf("encode generated png: %v", err))
	}
	return buf.Bytes()
}
