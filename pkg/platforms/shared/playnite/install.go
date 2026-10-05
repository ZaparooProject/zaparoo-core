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

package playnite

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	desktopExeName    = "Playnite.DesktopApp.exe"
	fullscreenExeName = "Playnite.FullscreenApp.exe"
)

// ErrNotInstalled is returned when no Playnite folder can be found.
var ErrNotInstalled = errors.New("no Playnite install found")

// Locator finds the Playnite folder. Registry, when set, resolves the folder
// the installer registered; Candidates are the default install locations
// tried last.
type Locator struct {
	FS         afero.Fs
	Registry   func() (string, error)
	Candidates []string
}

// Locate resolves the install. An explicitly configured directory is
// authoritative: when it is set but does not hold Playnite, the error says so
// rather than silently falling back to another install.
func (l Locator) Locate(configDir string) (Install, error) {
	if configDir != "" {
		inst, ok := l.inspect(configDir)
		if !ok {
			return Install{}, fmt.Errorf(
				"configured Playnite install_dir %q does not contain %s", configDir, desktopExeName,
			)
		}
		return inst, nil
	}

	if l.Registry != nil {
		dir, err := l.Registry()
		switch {
		case err != nil:
			log.Debug().Err(err).Msg("Playnite registry lookup failed")
		case dir != "":
			if inst, ok := l.inspect(dir); ok {
				return inst, nil
			}
			log.Debug().Str("dir", dir).Msg("Playnite registry path does not hold an install")
		}
	}

	for _, dir := range l.Candidates {
		if inst, ok := l.inspect(dir); ok {
			return inst, nil
		}
	}
	return Install{}, ErrNotInstalled
}

// inspect reports whether dir holds a Playnite install and describes it.
func (l Locator) inspect(dir string) (Install, bool) {
	dir = filepath.Clean(dir)
	inst := Install{Dir: dir, DesktopExe: filepath.Join(dir, desktopExeName)}
	if !l.isFile(inst.DesktopExe) {
		return Install{}, false
	}
	if fullscreen := filepath.Join(dir, fullscreenExeName); l.isFile(fullscreen) {
		inst.FullscreenExe = fullscreen
	}
	return inst, true
}

func (l Locator) isFile(path string) bool {
	fs := l.FS
	if fs == nil {
		fs = afero.NewOsFs()
	}
	info, err := fs.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// DefaultInstallDirs lists where Playnite's installer puts it for a user
// whose local application data folder is localAppData.
func DefaultInstallDirs(localAppData string) []string {
	if localAppData == "" {
		return nil
	}
	return []string{filepath.Join(localAppData, "Playnite")}
}
