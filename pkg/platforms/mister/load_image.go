//go:build linux && !android

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

package mister

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// imageExt is the extension of the disk images whose contents are listed as
// games, and so are shown as a folder.
const imageExt = ".vhd"

func isImagePath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), imageExt)
}

func isRoot(dir string) bool {
	return dir == "" || dir == "." || dir == string(filepath.Separator)
}

// splitImagePath splits a path into the image file it is inside and the path
// within it. A path that is the image itself, or is not inside one, is not ok.
func splitImagePath(fs afero.Fs, path string) (image, inner string, ok bool) {
	rest := ""
	for dir := path; !isRoot(dir); dir = filepath.Dir(dir) {
		if rest != "" && isImagePath(dir) {
			if info, err := fs.Stat(dir); err == nil && info.Mode().IsRegular() {
				return dir, rest, true
			}
		}
		base := filepath.Base(dir)
		if rest == "" {
			rest = base
		} else {
			rest = base + "/" + rest
		}
	}
	return "", "", false
}

// insideImage reports whether path names something inside a disk image, without
// reading the image. It is what a launcher's Test runs for every unmatched
// path, so it stays cheap: a name check, then one stat.
func insideImage(fs afero.Fs, path string) bool {
	if isImagePath(path) {
		return false
	}
	for dir := filepath.Dir(path); !isRoot(dir); dir = filepath.Dir(dir) {
		if isImagePath(dir) {
			info, err := fs.Stat(dir)
			return err == nil && info.Mode().IsRegular()
		}
	}
	return false
}

// imageDiscoverer finds the disk images in a system's folders. The image Main
// mounts by itself, boot.vhd, is left out of the scan on purpose, so the images
// a scan hands the launcher are not the whole set.
type imageDiscoverer func(ctx context.Context, cfg *config.Instance, systemID string) []string

// discoverImages lists the disk images directly inside the folders of a system.
func discoverImages(pl platforms.Platform, fs afero.Fs) imageDiscoverer {
	return func(ctx context.Context, cfg *config.Instance, systemID string) []string {
		system, err := systemdefs.GetSystem(systemID)
		if err != nil {
			return nil
		}
		var images []string
		roots := mediascanner.GetSystemPaths(ctx, cfg, pl, pl.RootDirs(cfg), []systemdefs.System{*system})
		for i := range roots {
			entries, readErr := afero.ReadDir(fs, roots[i].Path)
			if readErr != nil {
				continue
			}
			for _, entry := range entries {
				if entry.Mode().IsRegular() && isImagePath(entry.Name()) {
					images = append(images, filepath.Join(roots[i].Path, entry.Name()))
				}
			}
		}
		return images
	}
}

// imageLister lists the games inside one disk image as paths within it.
type imageLister func(fs afero.Fs, image string) []string

// extendForImages makes a launcher show the games inside its disk images as
// children of the image: the scanner adds them after the images it finds, and
// the test claims them. The image itself stays launchable as it always was.
//
// hasExt decides whether a listed name ends in a real file extension, which the
// index then splits off; a title with none keeps its whole name.
func extendForImages(
	fs afero.Fs, list imageLister, hasExt func(name string) bool, discover imageDiscoverer,
) func(*platforms.Launcher) {
	return func(l *platforms.Launcher) {
		prior := l.Scanner
		l.Scanner = func(
			ctx context.Context,
			cfg *config.Instance,
			systemID string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			if prior != nil {
				var err error
				if results, err = prior(ctx, cfg, systemID, results); err != nil {
					return results, err
				}
			}
			images := make([]string, 0, len(results))
			seen := make(map[string]struct{}, len(results))
			for i := range results {
				if isImagePath(results[i].Path) {
					seen[results[i].Path] = struct{}{}
					images = append(images, results[i].Path)
				}
			}
			if discover != nil {
				for _, image := range discover(ctx, cfg, systemID) {
					if _, dup := seen[image]; !dup {
						seen[image] = struct{}{}
						images = append(images, image)
					}
				}
			}

			for _, image := range images {
				if err := ctx.Err(); err != nil {
					return results, err //nolint:wrapcheck // cancellation passes through the scanner pipeline
				}
				inner := list(fs, image)
				for _, name := range inner {
					results = append(results, platforms.ScanResult{
						Path:  image + "/" + name,
						NoExt: hasExt == nil || !hasExt(name),
					})
				}
				log.Debug().Str("image", image).Int("games", len(inner)).Msg("listed games inside disk image")
			}
			return results, nil
		}

		priorTest := l.Test
		l.Test = func(cfg *config.Instance, path string) bool {
			if insideImage(fs, path) {
				return true
			}
			return priorTest != nil && priorTest(cfg, path)
		}
	}
}
