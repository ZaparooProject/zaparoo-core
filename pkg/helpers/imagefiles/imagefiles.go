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

// Package imagefiles stores user-supplied images as files in a folder of
// Core's data directory: deck covers and the images set on games by hand.
package imagefiles

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// MaxBytes is the largest image a store keeps.
	MaxBytes = 10 << 20
	// pruneGrace keeps a file written moments ago through a prune: the row
	// that names it may not have been committed yet.
	pruneGrace = 10 * time.Minute
)

var (
	// ErrNotImage reports bytes that are not a supported image.
	ErrNotImage = errors.New("not a JPEG, PNG, GIF or WebP image")
	// ErrTooLarge reports an image over MaxBytes.
	ErrTooLarge = errors.New("image is larger than 10 MiB")
	// ErrNotFound reports an image file that is not stored.
	ErrNotFound = errors.New("image not found")
)

// extensions maps the image types a store accepts to the extension a file is
// stored with.
var extensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Store keeps images in one folder. A file is named after a caller's prefix
// and its content, so a new image never overwrites one a row still names, and
// files no row names are pruned.
type Store struct {
	FS  afero.Fs
	Dir string
}

// ContentType reports the image type of data, or ErrNotImage.
func ContentType(data []byte) (string, error) {
	contentType := http.DetectContentType(data)
	if _, ok := extensions[contentType]; !ok {
		return "", ErrNotImage
	}
	return contentType, nil
}

// Save stores an image and returns the file name to keep.
func (s *Store) Save(prefix string, data []byte) (string, error) {
	if len(data) > MaxBytes {
		return "", ErrTooLarge
	}
	contentType, err := ContentType(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	name := strings.ToLower(prefix) + "-" + hex.EncodeToString(sum[:8]) + extensions[contentType]
	if !validName(name) {
		return "", fmt.Errorf("invalid image name prefix: %q", prefix)
	}
	if err = s.FS.MkdirAll(s.Dir, 0o750); err != nil {
		return "", fmt.Errorf("create image folder: %w", err)
	}
	tmp, err := afero.TempFile(s.FS, s.Dir, ".image-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create image file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = s.FS.Remove(tmpPath) }()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write image file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", fmt.Errorf("close image file: %w", err)
	}
	if err = s.FS.Rename(tmpPath, filepath.Join(s.Dir, name)); err != nil {
		return "", fmt.Errorf("store image file: %w", err)
	}
	return name, nil
}

// Path returns the full path of a stored image, or ErrNotFound when it is
// not stored. Any other error is a folder that could not be checked, not a
// missing image.
func (s *Store) Path(name string) (string, error) {
	if !validName(name) {
		return "", ErrNotFound
	}
	path := filepath.Join(s.Dir, name)
	info, err := s.FS.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("check image file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return path, nil
}

// Holds reports whether path names a file in the store, whether or not the
// file is still there.
func (s *Store) Holds(path string) bool {
	name := filepath.Base(path)
	return validName(name) && filepath.Join(s.Dir, name) == filepath.Clean(path)
}

// Read returns a stored image's bytes, or ErrNotFound.
func (s *Store) Read(name string) ([]byte, error) {
	path, err := s.Path(name)
	if err != nil {
		return nil, err
	}
	data, err := afero.ReadFile(s.FS, path)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	return data, nil
}

// Prune removes files not in keep. Files written in the last few minutes
// stay, since the row that names one may not be stored yet.
func (s *Store) Prune(keep map[string]struct{}, now time.Time) {
	entries, err := afero.ReadDir(s.FS, s.Dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Debug().Err(err).Str("dir", s.Dir).Msg("failed to read image folder")
		}
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || now.Sub(entry.ModTime()) < pruneGrace {
			continue
		}
		if _, kept := keep[entry.Name()]; kept {
			continue
		}
		if err := s.FS.Remove(filepath.Join(s.Dir, entry.Name())); err != nil {
			log.Debug().Err(err).Str("file", entry.Name()).Msg("failed to remove unused image")
		}
	}
}

// validName accepts a bare file name, never a path.
func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`) &&
		filepath.Base(name) == name
}
