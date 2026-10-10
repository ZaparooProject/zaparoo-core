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

package hoststatus

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

// DiskUsage is the size of a filesystem and how much of it is free.
type DiskUsage struct {
	Total uint64
	// Free is every unallocated byte; Available is the part of it the calling
	// user may use.
	Free      uint64
	Available uint64
}

// StorageReader reports the filesystems a set of directories live on.
type StorageReader struct {
	Usage func(path string) (DiskUsage, error)
	// FilesystemID returns the filesystem's identity and, where it is known,
	// its mount point.
	FilesystemID func(path string) (id, mountPoint string, err error)
	// IsEmpty reports whether a directory holds nothing. It is optional.
	IsEmpty func(path string) bool
	// Resolve follows the symlinks in a path, as FilesystemID does before it
	// names a mount point. It is optional.
	Resolve func(path string) (string, error)
}

// NewStorageReader returns a reader over the real filesystem.
func NewStorageReader() *StorageReader {
	return &StorageReader{
		Usage: DiskUsageOf, FilesystemID: FilesystemID, IsEmpty: dirIsEmpty, Resolve: filepath.EvalSymlinks,
	}
}

// Read returns one volume per distinct filesystem, in the order its first
// root was given, named by its mount point where that is known. A root that cannot be read, such as a share that is not
// mounted, is left out.
func (r *StorageReader) Read(roots []StorageRoot) ([]Volume, error) {
	volumes := make([]Volume, 0, len(roots))
	index := make(map[string]int, len(roots))
	for _, root := range roots {
		if root.Path == "" {
			continue
		}
		id, mountPoint, err := r.FilesystemID(root.Path)
		if err != nil {
			log.Debug().Err(err).Str("path", root.Path).Msg("skipping unreadable storage root")
			continue
		}
		if root.Role == RoleMedia && r.isPlaceholder(root.Path, mountPoint) {
			continue
		}
		if at, seen := index[id]; seen {
			volumes[at].Roles = addRole(volumes[at].Roles, root.Role)
			continue
		}
		usage, err := r.Usage(root.Path)
		if err != nil {
			log.Debug().Err(err).Str("path", root.Path).Msg("skipping unreadable storage root")
			continue
		}
		used := uint64(0)
		if usage.Total > usage.Free {
			used = usage.Total - usage.Free
		}
		if mountPoint == "" {
			mountPoint = root.Path
		}
		index[id] = len(volumes)
		volumes = append(volumes, Volume{
			Path:  mountPoint,
			Roles: addRole(nil, root.Role),
			Total: usage.Total,
			Free:  usage.Available,
			Used:  used,
		})
	}
	return volumes, nil
}

// isPlaceholder reports whether a media root is an empty directory on a
// filesystem mounted somewhere else, such as the mount point of a drive that
// is not plugged in. Its filesystem holds none of the device's media. An empty
// directory that is itself the mount point is a real, empty volume, and one
// whose mount point is not known is given the benefit of the doubt, as is one
// that cannot be resolved. The mount point is that of the resolved path, so a
// root that is a symlink to a mount point is compared after following it.
func (r *StorageReader) isPlaceholder(path, mountPoint string) bool {
	if r.IsEmpty == nil || mountPoint == "" {
		return false
	}
	resolved := path
	if r.Resolve != nil {
		var err error
		if resolved, err = r.Resolve(path); err != nil {
			return false
		}
	}
	if filepath.Clean(mountPoint) == filepath.Clean(resolved) {
		return false
	}
	return r.IsEmpty(path)
}

func dirIsEmpty(path string) bool {
	dir, err := os.Open(path) //nolint:gosec // A configured media root.
	if err != nil {
		return false
	}
	defer func() { _ = dir.Close() }()
	_, err = dir.Readdirnames(1)
	return errors.Is(err, io.EOF)
}

func addRole(roles []string, role string) []string {
	if role == "" {
		return roles
	}
	for _, existing := range roles {
		if existing == role {
			return roles
		}
	}
	return append(roles, role)
}
