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
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
)

// CoverMaxBytes is the largest cover image a deck keeps, and the largest
// one fetched for a deck from elsewhere.
const CoverMaxBytes = imagefiles.MaxBytes

var (
	// ErrCoverNotImage reports cover bytes that are not a supported image.
	ErrCoverNotImage = errors.New("cover is not a JPEG, PNG, GIF or WebP image")
	// ErrCoverTooLarge reports a cover over CoverMaxBytes.
	ErrCoverTooLarge = errors.New("cover image is larger than 10 MiB")
	// ErrCoverNotFound reports a cover file that is not stored.
	ErrCoverNotFound = errors.New("cover image not found")
)

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

func (c *Covers) store() *imagefiles.Store {
	return &imagefiles.Store{FS: c.FS, Dir: c.Dir}
}

// coverError gives an image store error its deck cover wording.
func coverError(err error) error {
	switch {
	case errors.Is(err, imagefiles.ErrNotImage):
		return ErrCoverNotImage
	case errors.Is(err, imagefiles.ErrTooLarge):
		return ErrCoverTooLarge
	case errors.Is(err, imagefiles.ErrNotFound):
		return ErrCoverNotFound
	default:
		return err
	}
}

// CoverContentType reports the image type of cover bytes, or
// ErrCoverNotImage.
func CoverContentType(data []byte) (string, error) {
	contentType, err := imagefiles.ContentType(data)
	if err != nil {
		return "", coverError(err)
	}
	return contentType, nil
}

// Save stores a deck's cover and returns the file name to keep on the deck.
func (c *Covers) Save(deckID string, data []byte) (string, error) {
	name, err := c.store().Save(deckID, data)
	if err != nil {
		if mapped := coverError(err); !errors.Is(mapped, err) {
			return "", mapped
		}
		return "", fmt.Errorf("store deck cover: %w", err)
	}
	return name, nil
}

// Path returns the full path of a stored cover file, or ErrCoverNotFound.
func (c *Covers) Path(name string) (string, error) {
	path, err := c.store().Path(name)
	if err != nil {
		return "", coverError(err)
	}
	return path, nil
}

// Read returns a stored cover's bytes, or ErrCoverNotFound.
func (c *Covers) Read(name string) ([]byte, error) {
	data, err := c.store().Read(name)
	if err != nil {
		if mapped := coverError(err); !errors.Is(mapped, err) {
			return nil, mapped
		}
		return nil, fmt.Errorf("read deck cover: %w", err)
	}
	return data, nil
}

// Prune removes cover files no deck names. Files written in the last few
// minutes stay, since the deck that names one may not be stored yet.
func (c *Covers) Prune(keep map[string]struct{}, now time.Time) {
	c.store().Prune(keep, now)
}
