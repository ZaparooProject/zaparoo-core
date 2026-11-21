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
// Every install writes its candidate and backup binaries next to the one it
// replaces, so the install directory has to accept new files on every
// platform. A distribution or Nix package lives somewhere read-only, and
// learning that at the swap would come after the download and the database
// snapshot.
//
// Windows will not let a running executable be overwritten, so the swap there
// moves the outgoing binary to a sibling name and gives the incoming one the
// name it vacated. That also needs permission to rename the target itself,
// which a target-specific ACL can deny even when the directory probe succeeds.
//
// The checks are probes rather than permission calculations because effective
// permissions are the only true answer, particularly on Windows.
func preflightPlatform(fs afero.Fs, goos, targetPath string) error {
	checkTarget := func(string) error { return nil }
	if _, ok := fs.(*afero.OsFs); ok {
		checkTarget = checkTargetRenameAllowed
	}
	return preflightPlatformWith(fs, goos, targetPath, checkTarget)
}

func preflightPlatformWith(
	fs afero.Fs,
	goos string,
	targetPath string,
	checkTargetRename func(string) error,
) error {
	if targetPath == "" {
		return nil
	}
	dir := filepath.Dir(targetPath)
	if err := checkInstallDirWritable(fs, dir); err != nil {
		log.Warn().Err(err).Str("dir", dir).
			Msg("cannot update in place because the install directory is not writable")
		if goos != "windows" {
			return fmt.Errorf(
				"%w: Zaparoo cannot write to %s, so update it the same way it was installed",
				ErrPlatformUnsupported, dir,
			)
		}
		return fmt.Errorf(
			"%w: Zaparoo cannot write to %s, so install this release with the Windows installer instead",
			ErrPlatformUnsupported, dir,
		)
	}
	// Off Windows the swap is a single rename, which the directory probe has
	// already covered.
	if goos != "windows" {
		return nil
	}
	if err := checkTargetRename(targetPath); err != nil {
		log.Warn().Err(err).Str("target", targetPath).
			Msg("cannot update in place because the executable cannot be renamed")
		return fmt.Errorf(
			"%w: Zaparoo cannot rename %s, so install this release with the Windows installer instead",
			ErrPlatformUnsupported, targetPath,
		)
	}
	return nil
}

// checkInstallDirWritable reports whether this process can create and remove a
// sibling in dir. Probing the effective permission is more reliable than
// interpreting directory mode bits; target rename access is checked separately.
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
