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
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

// ErrInsufficientSpace is returned before anything is downloaded, so a full
// disk costs a manifest fetch rather than an archive.
var ErrInsufficientSpace = errors.New("insufficient disk space for the update")

const bytesPerMB = 1024 * 1024

// spaceNeeds is what an install is about to write, and where.
type spaceNeeds struct {
	// free defaults to helpers.FreeDiskSpace. Injected so the check can be
	// tested without filling a disk.
	free        func(string) (uint64, error)
	targetPath  string
	stagingRoot string
	userDBPath  string
	archiveSize int64
}

// required totals every byte the install adds while the old binary and the old
// database are still on disk, which is the peak: nothing is reclaimed until the
// update is confirmed.
//
// The archive is counted twice because the manifest carries no uncompressed
// size, so the compressed size stands in for the payload it expands into. Our
// archives hold one compressed binary, so that underestimates the payload by
// roughly the compression ratio, and the two binary-sized sidecars below absorb
// the difference. The binary is counted twice for the staged candidate and the
// rollback copy of the running binary, and the database once for the update
// snapshot, which VACUUM INTO can only make smaller than its source.
func (n *spaceNeeds) required() int64 {
	binarySize := fileSizeOrZero(n.targetPath)
	return 2*n.archiveSize + 2*binarySize + fileSizeOrZero(n.userDBPath)
}

// checkFreeSpace charges the whole requirement to every directory the install
// writes to. On MiSTer they are all one volume and that is exact; where they are
// separate volumes it is over-strict by the size of the parts that live
// elsewhere, which is tens of megabytes on hardware with gigabytes free. There
// is no portable way to tell two paths are the same filesystem, and refusing an
// update that would have fit is a much cheaper mistake than starting one that
// does not.
func checkFreeSpace(n *spaceNeeds) error {
	// A manifest with no size is rejected by the download for the same reason;
	// there is nothing to check against here.
	if n.archiveSize <= 0 {
		return nil
	}
	required := n.required()
	if required <= 0 {
		return nil
	}
	//nolint:gosec // required is positive above
	need := uint64(required)

	free := n.free
	if free == nil {
		free = helpers.FreeDiskSpace
	}

	checked := make(map[string]struct{}, 3)
	for _, path := range []string{
		n.stagingRoot,
		filepath.Dir(n.targetPath),
		filepath.Dir(n.userDBPath),
	} {
		if path == "" {
			continue
		}
		dir := nearestExistingDir(path)
		if _, seen := checked[dir]; seen {
			continue
		}
		checked[dir] = struct{}{}

		available, err := free(dir)
		if err != nil {
			// A filesystem that will not report its free space must not make
			// updating impossible; the install still fails safely on ENOSPC.
			log.Warn().Err(err).Str("dir", dir).
				Msg("could not check free space before updating")
			continue
		}
		if available < need {
			return fmt.Errorf("%w: %s has %d MB free, need at least %d MB",
				ErrInsufficientSpace, dir, available/bytesPerMB, need/bytesPerMB)
		}
	}
	return nil
}

// nearestExistingDir walks up until it finds a directory that exists. Free space
// is a property of the filesystem rather than the leaf, and both the staging
// root and the snapshot directory are created later in the install.
func nearestExistingDir(path string) string {
	for {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

// fileSizeOrZero leaves a missing or unreadable file out of the requirement
// rather than failing the update over it.
func fileSizeOrZero(path string) int64 {
	if path == "" {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		log.Debug().Err(err).Str("path", path).
			Msg("update space check could not size a file")
		return 0
	}
	return info.Size()
}
