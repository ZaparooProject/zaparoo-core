//go:build linux

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
	"github.com/rs/zerolog/log"
)

const backgroundModeWallpaper byte = 2

// wallpaperPaths holds filesystem paths used by wallpaper operations.
// Production code uses defaultWallpaperPaths(); tests can override.
type wallpaperPaths struct {
	sdRoot        string
	wallpapersDir string
	menuCfgFile   string
}

func defaultWallpaperPaths() wallpaperPaths {
	return wallpaperPaths{
		sdRoot:        misterconfig.SDRootDir,
		wallpapersDir: misterconfig.WallpapersDir,
		menuCfgFile:   misterconfig.MenuConfigFile,
	}
}

// CmdWallpaper sets or unsets the MiSTer main menu wallpaper.
//
// With an argument (e.g. "mister.wallpaper|filename.png"), sets that file from
// the wallpapers directory as the active wallpaper. Without arguments, unsets
// the current wallpaper.
//
// The mechanism works by creating a symlink at /media/fat/menu.{ext} pointing
// to the target file in /media/fat/wallpapers/, then setting the background
// mode byte in MENU.CFG to wallpaper mode. If the user is on the main menu,
// the menu core is relaunched to display the change immediately.
func CmdWallpaper(_ platforms.Platform, env *platforms.CmdEnv) (platforms.CmdResult, error) {
	paths := defaultWallpaperPaths()

	if len(env.Cmd.Args) == 0 || env.Cmd.Args[0] == "" {
		return unsetWallpaper(paths)
	}

	return setWallpaper(paths, env.Cmd.Args[0])
}

func setWallpaper(paths wallpaperPaths, filename string) (platforms.CmdResult, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".png" && ext != ".jpg" {
		return platforms.CmdResult{}, fmt.Errorf("unsupported wallpaper format %q, must be .png or .jpg", ext)
	}

	wallpaperPath := filepath.Join(paths.wallpapersDir, filename)
	cleanWallpapers := filepath.Clean(paths.wallpapersDir) + string(filepath.Separator)
	if !strings.HasPrefix(filepath.Clean(wallpaperPath)+string(filepath.Separator), cleanWallpapers) {
		return platforms.CmdResult{}, fmt.Errorf("invalid wallpaper filename: %s", filename)
	}

	if _, err := os.Stat(wallpaperPath); err != nil {
		return platforms.CmdResult{}, fmt.Errorf("wallpaper file not found: %s", filename)
	}

	if err := cleanupMenuFile(filepath.Join(paths.sdRoot, "menu.png"), paths.wallpapersDir); err != nil {
		return platforms.CmdResult{}, fmt.Errorf("cleanup menu.png: %w", err)
	}
	if err := cleanupMenuFile(filepath.Join(paths.sdRoot, "menu.jpg"), paths.wallpapersDir); err != nil {
		return platforms.CmdResult{}, fmt.Errorf("cleanup menu.jpg: %w", err)
	}

	symlinkPath := filepath.Join(paths.sdRoot, "menu"+ext)
	if err := os.Symlink(wallpaperPath, symlinkPath); err != nil {
		return platforms.CmdResult{}, fmt.Errorf("create wallpaper symlink: %w", err)
	}

	log.Info().
		Str("wallpaper", filename).
		Str("symlink", symlinkPath).
		Msg("wallpaper set")

	if err := setBackgroundMode(paths.menuCfgFile, backgroundModeWallpaper); err != nil {
		log.Warn().Err(err).Msg("failed to set background mode in MENU.CFG")
	}

	relaunchMenuIfActive()

	return platforms.CmdResult{}, nil
}

func unsetWallpaper(paths wallpaperPaths) (platforms.CmdResult, error) {
	removed := false

	for _, name := range []string{"menu.png", "menu.jpg"} {
		path := filepath.Join(paths.sdRoot, name)

		fi, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return platforms.CmdResult{}, fmt.Errorf("stat %s: %w", name, err)
		}

		if fi.Mode()&os.ModeSymlink == 0 {
			log.Debug().Str("path", path).Msg("skipping non-symlink menu file")
			continue
		}

		if err := os.Remove(path); err != nil {
			return platforms.CmdResult{}, fmt.Errorf("remove wallpaper symlink %s: %w", name, err)
		}

		log.Info().Str("path", path).Msg("wallpaper symlink removed")
		removed = true
	}

	if !removed {
		log.Debug().Msg("no wallpaper symlink to remove")
	}

	relaunchMenuIfActive()

	return platforms.CmdResult{}, nil
}

// cleanupMenuFile handles an existing menu.png or menu.jpg file.
// If it's a symlink (created by us), it's deleted. If it's a regular file
// (placed by the user), it's moved into the wallpapers directory to preserve it.
func cleanupMenuFile(path, wallpapersDir string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove symlink %s: %w", path, err)
		}
		return nil
	}

	// Regular file — move to wallpapers dir with timestamp to avoid collisions
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	ext := filepath.Ext(path)
	dest := filepath.Join(
		wallpapersDir,
		fmt.Sprintf("%s_%d%s", base, time.Now().Unix(), ext),
	)

	if err := os.MkdirAll(wallpapersDir, 0o750); err != nil {
		return fmt.Errorf("create wallpapers dir: %w", err)
	}

	if err := os.Rename(path, dest); err != nil {
		return fmt.Errorf("move %s to %s: %w", path, dest, err)
	}

	log.Info().Str("from", path).Str("to", dest).Msg("preserved user wallpaper file")
	return nil
}

// setBackgroundMode writes the background mode byte to the first byte of
// the given MENU.CFG file. This is a binary config file used by MiSTer's
// main menu to determine which background to display.
func setBackgroundMode(menuCfgFile string, mode byte) error {
	if err := os.MkdirAll(filepath.Dir(menuCfgFile), 0o750); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := os.ReadFile(menuCfgFile) //nolint:gosec // G304: path from misterconfig constant
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read MENU.CFG: %w", err)
		}
		data = []byte{0}
	}

	if len(data) == 0 {
		data = []byte{0}
	}

	data[0] = mode

	//nolint:gosec // G306: MENU.CFG is a system config file, 0644 matches MiSTer conventions
	if err := os.WriteFile(menuCfgFile, data, 0o644); err != nil {
		return fmt.Errorf("write MENU.CFG: %w", err)
	}

	return nil
}

// relaunchMenuIfActive relaunches the menu core if the user is currently
// on the main menu, so the wallpaper change is visible immediately.
func relaunchMenuIfActive() {
	coreName, err := mistermain.ReadCoreName()
	if err != nil {
		log.Debug().Err(err).Msg("could not read core name, skipping menu relaunch")
		return
	}

	if coreName != misterconfig.MenuCore {
		return
	}

	if err := mistermain.LaunchMenu(); err != nil {
		log.Warn().Err(err).Msg("failed to relaunch menu after wallpaper change")
	}
}
