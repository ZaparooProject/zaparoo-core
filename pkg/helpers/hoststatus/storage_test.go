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
