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

package decks

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// CoverMaxBytes is the largest cover image a deck keeps, and the largest
	// one fetched for a deck from elsewhere.
	CoverMaxBytes = 10 << 20
	// coverPruneGrace keeps a cover file written moments ago through a
	// prune: its deck row may not have been committed yet.
	coverPruneGrace = 10 * time.Minute
)

var (
	// ErrCoverNotImage reports cover bytes that are not a supported image.
	ErrCoverNotImage = errors.New("cover is not a JPEG, PNG, GIF or WebP image")
	// ErrCoverTooLarge reports a cover over CoverMaxBytes.
	ErrCoverTooLarge = errors.New("cover image is larger than 10 MiB")
	// ErrCoverNotFound reports a cover file that is not stored.
	ErrCoverNotFound = errors.New("cover image not found")
)

// coverExtensions maps the image types a cover may be to the extension its
// file is stored with.
var coverExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Covers stores deck cover images on this device: user data in the data
// directory, never the thumbnail cache, which may be emptied at any time. A
// file is named after its deck and its content, so a new cover never
// overwrites one a deck row still names, and files no deck names are pruned.
type Covers struct {
	FS  afero.Fs
	Dir string
}

// NewCovers returns the cover store under the platform's data directory.
func NewCovers(pl platforms.Platform) *Covers {
	return &Covers{FS: afero.NewOsFs(), Dir: filepath.Join(helpers.DataDir(pl), "decks", "covers")}
}

// CoverContentType reports the image type of cover bytes, or
// ErrCoverNotImage.
func CoverContentType(data []byte) (string, error) {
	contentType := http.DetectContentType(data)
	if _, ok := coverExtensions[contentType]; !ok {
		return "", ErrCoverNotImage
	}
	return contentType, nil
}

// Save stores a deck's cover and returns the file name to keep on the deck.
func (c *Covers) Save(deckID string, data []byte) (string, error) {
	if len(data) > CoverMaxBytes {
		return "", ErrCoverTooLarge
	}
	contentType, err := CoverContentType(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	name := strings.ToLower(deckID) + "-" + hex.EncodeToString(sum[:8]) + coverExtensions[contentType]
	if !validCoverName(name) {
		return "", fmt.Errorf("invalid deck id for a cover: %q", deckID)
	}
	if err = c.FS.MkdirAll(c.Dir, 0o750); err != nil {
		return "", fmt.Errorf("create deck covers folder: %w", err)
	}
	tmp, err := afero.TempFile(c.FS, c.Dir, ".cover-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create deck cover file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = c.FS.Remove(tmpPath) }()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write deck cover file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", fmt.Errorf("close deck cover file: %w", err)
	}
	if err = c.FS.Rename(tmpPath, filepath.Join(c.Dir, name)); err != nil {
		return "", fmt.Errorf("store deck cover file: %w", err)
	}
	return name, nil
}

// Path returns the full path of a stored cover file, or ErrCoverNotFound.
func (c *Covers) Path(name string) (string, error) {
	if !validCoverName(name) {
		return "", ErrCoverNotFound
	}
	path := filepath.Join(c.Dir, name)
	info, err := c.FS.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrCoverNotFound
	}
	return path, nil
}

// Read returns a stored cover's bytes, or ErrCoverNotFound.
func (c *Covers) Read(name string) ([]byte, error) {
	path, err := c.Path(name)
	if err != nil {
		return nil, err
	}
	data, err := afero.ReadFile(c.FS, path)
	if err != nil {
		return nil, fmt.Errorf("read deck cover: %w", err)
	}
	return data, nil
}

// Prune removes cover files no deck names. Files written in the last few
// minutes stay, since the deck that names one may not be stored yet.
func (c *Covers) Prune(keep map[string]struct{}, now time.Time) {
	entries, err := afero.ReadDir(c.FS, c.Dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Debug().Err(err).Msg("failed to read deck covers folder")
		}
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || now.Sub(entry.ModTime()) < coverPruneGrace {
			continue
		}
		if _, kept := keep[entry.Name()]; kept {
			continue
		}
		if err := c.FS.Remove(filepath.Join(c.Dir, entry.Name())); err != nil {
			log.Debug().Err(err).Str("file", entry.Name()).Msg("failed to remove unused deck cover")
		}
	}
}

// validCoverName accepts a bare file name, never a path.
func validCoverName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`) &&
		filepath.Base(name) == name
}
