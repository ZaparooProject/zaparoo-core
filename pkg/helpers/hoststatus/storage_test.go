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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorageReader_Read(t *testing.T) {
	t.Parallel()

	games := filepath.Join(string(filepath.Separator), "media", "fat", "games")
	data := filepath.Join(string(filepath.Separator), "media", "fat", "zaparoo")
	usb := filepath.Join(string(filepath.Separator), "media", "usb0", "games")
	gone := filepath.Join(string(filepath.Separator), "media", "network")
	sdMount := filepath.Join(string(filepath.Separator), "media", "fat")

	filesystems := map[string]string{games: "sd", data: "sd", usb: "usb"}
	reader := &StorageReader{
		FilesystemID: func(path string) (id, mountPoint string, err error) {
			id, ok := filesystems[path]
			if !ok {
				return "", "", errors.New("not mounted")
			}
			if id == "sd" {
				return id, sdMount, nil
			}
			return id, "", nil
		},
		Usage: func(path string) (DiskUsage, error) {
			if filesystems[path] == "sd" {
				return DiskUsage{Total: 1000, Free: 400, Available: 350}, nil
			}
			return DiskUsage{Total: 500, Free: 500, Available: 500}, nil
		},
	}

	volumes, err := reader.Read([]StorageRoot{
		{Path: games, Role: RoleMedia},
		{Path: gone, Role: RoleMedia},
		{Path: usb, Role: RoleMedia},
		{Path: "", Role: RoleMedia},
		{Path: data, Role: RoleData},
	})
	require.NoError(t, err)
	assert.Equal(t, []Volume{
		{Path: sdMount, Roles: []string{RoleMedia, RoleData}, Total: 1000, Free: 350, Used: 600},
		{Path: usb, Roles: []string{RoleMedia}, Total: 500, Free: 500, Used: 0},
	}, volumes)
}

func TestStorageReader_RealFilesystem(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	volumes, err := NewStorageReader().Read([]StorageRoot{
		{Path: dir, Role: RoleData},
		{Path: filepath.Join(dir, "missing"), Role: RoleMedia},
	})
	require.NoError(t, err)
	require.Len(t, volumes, 1)
	assert.Equal(t, []string{RoleData}, volumes[0].Roles)
	assert.NotZero(t, volumes[0].Total)
	assert.LessOrEqual(t, volumes[0].Free, volumes[0].Total)
}

// A platform lists places media may turn up, such as the mount points of
// drives that are not plugged in. An empty one that is not a mount point of
// its own is a directory on some other filesystem, which holds no media and
// must not be reported as a media volume.
func TestStorageReader_LeavesOutEmptyPlaceholderRoots(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	games := filepath.Join(sep, "media", "fat", "games")
	sdMount := filepath.Join(sep, "media", "fat")
	unplugged := filepath.Join(sep, "media", "usb0")
	plugged := filepath.Join(sep, "media", "usb1")
	data := filepath.Join(sep, "media", "fat", "zaparoo")
	emptyOnData := filepath.Join(sep, "media", "fat", "empty")

	mounts := map[string]string{games: sdMount, data: sdMount, emptyOnData: sdMount, unplugged: sep, plugged: plugged}
	empty := map[string]bool{unplugged: true, plugged: true, emptyOnData: true}
	reader := &StorageReader{
		FilesystemID: func(path string) (id, mountPoint string, err error) {
			return mounts[path], mounts[path], nil
		},
		Usage: func(path string) (DiskUsage, error) {
			if mounts[path] == sep {
				return DiskUsage{Total: 300, Free: 0, Available: 0}, nil
			}
			return DiskUsage{Total: 1000, Free: 400, Available: 400}, nil
		},
		IsEmpty: func(path string) bool { return empty[path] },
	}

	volumes, err := reader.Read([]StorageRoot{
		{Path: unplugged, Role: RoleMedia},
		{Path: plugged, Role: RoleMedia},
		{Path: emptyOnData, Role: RoleMedia},
		{Path: data, Role: RoleData},
		{Path: games, Role: RoleMedia},
	})
	require.NoError(t, err)
	assert.Equal(t, []Volume{
		{Path: plugged, Roles: []string{RoleMedia}, Total: 1000, Free: 400, Used: 600},
		{Path: sdMount, Roles: []string{RoleData, RoleMedia}, Total: 1000, Free: 400, Used: 600},
	}, volumes, "an empty drive that is mounted is still a media volume")
}
