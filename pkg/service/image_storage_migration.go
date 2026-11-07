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

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// legacyLibretroThumbsDir is where the libretro thumbnail scraper stored its
// downloads, under the data directory, before the image folder existed.
const legacyLibretroThumbsDir = "libretro-thumbnails"

// imagePropertyRewriter is the MediaDB capability the image storage migration
// needs: rewriting stored image paths in the same transaction as the move.
type imagePropertyRewriter interface {
	RewriteImagePropertyPrefix(ctx context.Context, oldPrefix, newPrefix, sep string, apply func() error) (int64, error)
}

// migrateImageStorage moves image storage from its old places under the data
// directory into the image folder, once, before the API serves anything:
//   - <data>/libretro-thumbnails becomes <images>/libretro, and the image
//     properties pointing into it become images:// paths in the same step.
//   - <data>/cache/thumbs becomes <images>/thumbs.
//
// Each move is a rename, never a copy, so it only succeeds when the image
// folder is on the same filesystem (always true for the default). Any failure
// leaves the files and the database as they were and Core keeps working: old
// absolute image paths still load. Once a move is done its source is gone, so
// running this again does nothing.
func migrateImageStorage(ctx context.Context, fs afero.Fs, dataDir, imageDir string, mediaDB database.MediaDBI) {
	if dataDir == "" || imageDir == "" {
		return
	}
	migrateLibretroThumbnails(ctx, fs, dataDir, imageDir, mediaDB)
	oldThumbs := filepath.Join(dataDir, config.CacheDir, config.ImageThumbsDir)
	newThumbs := filepath.Join(imageDir, config.ImageThumbsDir)
	if pending, err := migrationPending(fs, oldThumbs, newThumbs); err != nil || !pending {
		return
	}
	if err := renameIntoImageDir(fs, imageDir, oldThumbs, newThumbs); err != nil {
		log.Warn().Err(err).Str("from", oldThumbs).Str("to", newThumbs).
			Msg("image storage: could not move the thumbnail cache, it will be rebuilt")
		return
	}
	log.Info().Str("from", oldThumbs).Str("to", newThumbs).Msg("image storage: moved the thumbnail cache")
}

func migrateLibretroThumbnails(
	ctx context.Context, fs afero.Fs, dataDir, imageDir string, mediaDB database.MediaDBI,
) {
	oldDir := filepath.Join(dataDir, legacyLibretroThumbsDir)
	newDir := filepath.Join(imageDir, config.ImageLibretroDir)
	pending, err := migrationPending(fs, oldDir, newDir)
	if err != nil || !pending {
		return
	}
	rewriter, ok := mediaDB.(imagePropertyRewriter)
	if !ok {
		// Moving the files without rewriting their paths would lose every
		// downloaded cover, so both happen or neither does.
		log.Warn().Str("dir", oldDir).Msg("image storage: media database unavailable, libretro thumbnails not moved")
		return
	}
	moved := false
	rewritten, err := rewriter.RewriteImagePropertyPrefix(ctx,
		oldDir+string(filepath.Separator),
		config.ImagePath(config.ImageLibretroDir)+"/",
		string(filepath.Separator),
		func() error {
			if renameErr := renameIntoImageDir(fs, imageDir, oldDir, newDir); renameErr != nil {
				return renameErr
			}
			moved = true
			return nil
		},
	)
	if err != nil {
		if moved {
			// The files moved but the paths did not: put the files back so
			// the stored paths find them again.
			if backErr := fs.Rename(newDir, oldDir); backErr != nil {
				log.Error().Err(backErr).Str("from", newDir).Str("to", oldDir).
					Msg("image storage: could not move libretro thumbnails back after a failed database update")
			}
		}
		log.Warn().Err(err).Str("from", oldDir).Str("to", newDir).
			Msg("image storage: could not move libretro thumbnails, leaving them where they are")
		return
	}
	log.Info().Str("from", oldDir).Str("to", newDir).Int64("properties", rewritten).
		Msg("image storage: moved libretro thumbnails")
}

// migrationPending reports whether from is a folder still to be moved to an
// absent to. A destination that already exists is left alone: the user, or an
// earlier run, put something there, and merging is not Core's to do.
func migrationPending(fs afero.Fs, from, to string) (bool, error) {
	fromInfo, err := fs.Stat(from)
	if err != nil || !fromInfo.IsDir() {
		return false, nil //nolint:nilerr // nothing to move
	}
	if _, err := fs.Stat(to); err == nil {
		log.Info().Str("from", from).Str("to", to).
			Msg("image storage: destination already exists, leaving the old folder in place")
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Str("to", to).Msg("image storage: could not check the destination")
		return false, fmt.Errorf("stat %s: %w", to, err)
	}
	return true, nil
}

// renameIntoImageDir renames from to to, creating the image folder first.
func renameIntoImageDir(fs afero.Fs, imageDir, from, to string) error {
	if err := fs.MkdirAll(imageDir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", imageDir, err)
	}
	if err := fs.Rename(from, to); err != nil {
		return fmt.Errorf("rename %s to %s: %w", from, to, err)
	}
	return nil
}
