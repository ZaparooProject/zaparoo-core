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

package updater

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// ErrPlatformUnsupported is returned when this build cannot finish an install
// no matter how the release turns out.
var ErrPlatformUnsupported = errors.New("this platform cannot install updates in place")

// preflightPlatform refuses an install this build cannot complete.
//
// Windows will not let a running executable be overwritten, so the swap moves
// the outgoing binary to a sibling name and gives the incoming one the name it
// vacated. Both halves are renames in the directory holding the executable, so
// the install needs write access to that directory rather than to the file. An
// installation under Program Files does not have it unless Core was started
// elevated, and Core refuses to run elevated.
//
// The check is a probe rather than a permission calculation because on Windows
// the effective permission is the only true one, and it runs here rather than
// at the swap so a user learns their install has to go through the installer
// before a release has been downloaded and their database snapshotted.
func preflightPlatform(fs afero.Fs, goos, targetPath string) error {
	// Everywhere else replaces the running binary with a single rename, which
	// needs nothing the install did not already need.
	if goos != "windows" || targetPath == "" {
		return nil
	}
	dir := filepath.Dir(targetPath)
	if err := checkInstallDirWritable(fs, dir); err != nil {
		log.Warn().Err(err).Str("dir", dir).
			Msg("cannot update in place because the install directory is not writable")
		return fmt.Errorf(
			"%w: Zaparoo cannot write to %s, so install this release with the Windows installer instead",
			ErrPlatformUnsupported, dir,
		)
	}
	return nil
}

// checkInstallDirWritable reports whether this process can put a new file in
// dir. Creating one is the only honest answer: the permission that decides the
// swap is the effective one, which no amount of reading the directory's own
// mode describes.
func checkInstallDirWritable(fs afero.Fs, dir string) error {
	probe, err := afero.TempFile(fs, dir, ".zaparoo-update-probe-*")
	if err != nil {
		return fmt.Errorf("creating a write probe in %q: %w", dir, err)
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := fs.Remove(name)
	var probeErr error
	if closeErr != nil {
		probeErr = errors.Join(probeErr, fmt.Errorf("closing a write probe in %q: %w", dir, closeErr))
	}
	if removeErr != nil {
		probeErr = errors.Join(probeErr, fmt.Errorf("removing write probe %q: %w", name, removeErr))
	}
	return probeErr
}
