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

package pinup

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	menuExeName   = "PinUpMenu.exe"
	databaseName  = "PUPDatabase.db"
	serverExeName = "PuPServer.exe"
	mediaDirName  = "POPMedia"
)

// ErrNotInstalled is returned when no PinUP System folder can be found.
var ErrNotInstalled = errors.New("PinUP Popper is not installed")

// Locator finds the PinUP System folder. Registry, when set, resolves the
// folder PinUP Player registered itself from; Candidates are the default
// install locations tried last.
type Locator struct {
	FS         afero.Fs
	Registry   func() (string, error)
	Candidates []string
}

// Locate resolves the install. An explicitly configured directory is
// authoritative: when it is set but does not hold Popper, the error says so
// rather than silently falling back to another install.
func (l Locator) Locate(configDir string) (Install, error) {
	if configDir != "" {
		inst, ok := l.inspect(configDir)
		if !ok {
			return Install{}, fmt.Errorf(
				"configured PinUP Popper install_dir %q does not contain %s and %s",
				configDir, menuExeName, databaseName,
			)
		}
		return inst, nil
	}

	if l.Registry != nil {
		dir, err := l.Registry()
		switch {
		case err != nil:
			log.Debug().Err(err).Msg("PinUP Popper registry lookup failed")
		case dir != "":
			if inst, ok := l.inspect(dir); ok {
				return inst, nil
			}
			log.Debug().Str("dir", dir).Msg("PinUP Popper registry path does not hold an install")
		}
	}

	for _, dir := range l.Candidates {
		if inst, ok := l.inspect(dir); ok {
			return inst, nil
		}
	}
	return Install{}, ErrNotInstalled
}

// inspect reports whether dir holds a Popper install and describes it.
func (l Locator) inspect(dir string) (Install, bool) {
	dir = filepath.Clean(dir)
	inst := Install{
		Dir:      dir,
		DBPath:   filepath.Join(dir, databaseName),
		MenuExe:  filepath.Join(dir, menuExeName),
		MediaDir: filepath.Join(dir, mediaDirName),
	}
	if !l.isFile(inst.MenuExe) || !l.isFile(inst.DBPath) {
		return Install{}, false
	}
	if server := filepath.Join(dir, serverExeName); l.isFile(server) {
		inst.ServerExe = server
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

// DefaultInstallDirs lists where PinUP System is normally installed: the
// Baller Installer's vPinball tree and a bare PinUPSystem folder, on each of
// the first few drive letters.
func DefaultInstallDirs() []string {
	dirs := make([]string, 0, 12)
	for _, drive := range []string{"C", "D", "E", "F", "G", "H"} {
		dirs = append(dirs,
			drive+`:\vPinball\PinUPSystem`,
			drive+`:\PinUPSystem`,
		)
	}
	return dirs
}

// ParseLocalServer32 extracts the executable path from a COM LocalServer32
// registry value, which may be quoted and may carry command-line arguments.
func ParseLocalServer32(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value[0] == '"' {
		rest := value[1:]
		if end := strings.IndexByte(rest, '"'); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
		return strings.TrimSpace(rest)
	}
	if idx := strings.Index(strings.ToLower(value), ".exe"); idx >= 0 {
		return value[:idx+len(".exe")]
	}
	return value
}

// InstallDirFromServerPath derives the PinUP System folder from the registered
// PinUP Player executable path. It splits on either separator itself so the
// Windows value can be parsed on any platform.
func InstallDirFromServerPath(value string) string {
	exe := ParseLocalServer32(value)
	idx := strings.LastIndexAny(exe, `\/`)
	if idx <= 0 {
		return ""
	}
	return exe[:idx]
}
