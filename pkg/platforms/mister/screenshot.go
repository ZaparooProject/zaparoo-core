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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

const screenshotTimeout = 3 * time.Second

// screenshotWatchDirs returns the screenshot directories Main may write to for
// the given core and RBF names, without duplicates. An empty rbfName is ignored.
func screenshotWatchDirs(coreName, rbfName string) []string {
	dirs := []string{filepath.Join(misterconfig.ScreenshotsDir, coreName)}
	if rbfName != "" && rbfName != coreName {
		dirs = append(dirs, filepath.Join(misterconfig.ScreenshotsDir, rbfName))
	}
	return dirs
}

// Screenshot triggers a MiSTer screenshot via the command interface and waits
// for the resulting file to appear in the screenshots directory. The full image
// is read into memory and returned as bytes. Typical PNGs are ~200-500KB which
// is fine for single-client usage on ARM. If screenshots grow significantly
// larger, consider a streaming approach instead.
func (*Platform) Screenshot() (*platforms.ScreenshotResult, error) {
	coreName, err := mistermain.ReadCoreName()
	if err != nil {
		return nil, fmt.Errorf("read core name: %w", err)
	}

	rbfName, rbfErr := mistermain.ReadRBFName()
	if rbfErr != nil {
		log.Debug().Err(rbfErr).Msg("rbf name unavailable, watching core name dir only")
		rbfName = ""
	}

	// An MGL setname with same_dir (e.g. RA_SNES) changes CORENAME, but Main
	// still writes screenshots under the original RBF name. Watch both.
	watchDirs := screenshotWatchDirs(coreName, rbfName)
	var created []string
	defer func() {
		// Only removes directories left empty; one holding a screenshot stays.
		for _, dir := range created {
			_ = os.Remove(dir)
		}
	}()
	for _, dir := range watchDirs {
		if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
			created = append(created, dir)
		}
		if mkErr := os.MkdirAll(dir, 0o750); mkErr != nil {
			return nil, fmt.Errorf("create screenshots dir: %w", mkErr)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create file watcher: %w", err)
	}
	defer func() {
		if closeErr := watcher.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close screenshot watcher")
		}
	}()

	for _, dir := range watchDirs {
		if err := watcher.Add(dir); err != nil {
			return nil, fmt.Errorf("watch screenshots dir: %w", err)
		}
	}

	if err := mistermain.RunDevCmd("screenshot", "scaled"); err != nil {
		return nil, fmt.Errorf("trigger screenshot: %w", err)
	}

	log.Debug().Strs("dirs", watchDirs).Msg("waiting for screenshot file")

	timeout := time.NewTimer(screenshotTimeout)
	defer timeout.Stop()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil, errors.New("watcher closed unexpectedly")
			}

			if event.Op&fsnotify.Create != fsnotify.Create {
				continue
			}

			ext := strings.ToLower(filepath.Ext(event.Name))
			if ext != ".png" && ext != ".bmp" {
				continue
			}

			log.Info().Str("path", event.Name).Msg("screenshot captured")

			// fsnotify fires Create when the inode appears, before the writer
			// has closed — poll until the file is fully written.
			pollInterval := 250 * time.Millisecond
			for {
				complete, checkErr := shared.ScreenshotFileComplete(event.Name, ext)
				if checkErr != nil {
					return nil, fmt.Errorf("check screenshot file: %w", checkErr)
				}
				if complete {
					break
				}

				select {
				case <-timeout.C:
					return nil, fmt.Errorf("screenshot file incomplete after %s", screenshotTimeout)
				case <-time.After(pollInterval):
				}
			}

			//nolint:gosec // Safe: reads screenshot from controlled application directory
			data, readErr := os.ReadFile(event.Name)
			if readErr != nil {
				return nil, fmt.Errorf("read screenshot file: %w", readErr)
			}

			return &platforms.ScreenshotResult{
				Path: event.Name,
				Data: data,
			}, nil

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil, errors.New("watcher error channel closed")
			}
			return nil, fmt.Errorf("file watcher error: %w", watchErr)

		case <-timeout.C:
			return nil, fmt.Errorf("screenshot timed out after %s", screenshotTimeout)
		}
	}
}
