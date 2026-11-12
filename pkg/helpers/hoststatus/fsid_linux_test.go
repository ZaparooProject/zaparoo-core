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

package hoststatus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleMountInfo = `22 1 0:21 /root / rw,relatime shared:1 - btrfs /dev/nvme0n1p3 rw,ssd,subvol=/root
45 22 0:40 /home /home rw,relatime shared:20 - btrfs /dev/nvme0n1p3 rw,ssd,subvol=/home
30 22 0:25 / /tmp rw,nosuid,nodev shared:5 - tmpfs tmpfs rw,size=31683864k
60 22 8:1 / /media/fat rw,noatime - exfat /dev/mmcblk0p1 rw,uid=0
61 60 0:55 / /media/fat/cifs rw - cifs //nas/share rw,vers=3.0
62 22 8:17 / /media/usb\040drive rw - vfat /dev/sda1 rw
63 22 8:33 / /media/over rw - ext4 /dev/sdb1 rw
64 22 8:49 / /media/over rw - ext4 /dev/sdc1 rw
malformed line
`

func TestMountSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path   string
		source string
		mount  string
	}{
		{path: "/", source: "/dev/nvme0n1p3", mount: "/"},
		{path: "/usr/share", source: "/dev/nvme0n1p3", mount: "/"},
		{path: "/home/user/games", source: "/dev/nvme0n1p3", mount: "/home"},
		{path: "/homework", source: "/dev/nvme0n1p3", mount: "/"},
		{path: "/media/fat/games/SNES", source: "/dev/mmcblk0p1", mount: "/media/fat"},
		{path: "/media/usb drive/roms", source: "/dev/sda1", mount: "/media/usb drive"},
		{path: "/media/over/x", source: "/dev/sdc1", mount: "/media/over"},
		{path: "/tmp/scratch", source: "tmpfs", mount: "/tmp"},
		{path: "/media/fat/cifs/games", source: "//nas/share", mount: "/media/fat/cifs"},
	}
	for _, tt := range tests {
		source, mount, ok := MountSource(sampleMountInfo, tt.path)
		assert.True(t, ok, tt.path)
		assert.Equal(t, tt.source, source, tt.path)
		assert.Equal(t, tt.mount, mount, tt.path)
	}

	_, _, ok := MountSource("", "/")
	assert.False(t, ok)
}

func TestUnescapeMountField(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/plain", unescapeMountField("/plain"))
	assert.Equal(t, "/a b", unescapeMountField(`/a\040b`))
	assert.Equal(t, `/a\b`, unescapeMountField(`/a\134b`))
	assert.Equal(t, `/a\9zzb`, unescapeMountField(`/a\9zzb`))
	assert.Equal(t, `/a\04`, unescapeMountField(`/a\04`))
}

// Two directories on different subvolumes of one filesystem share its space
// and must be reported once.
func TestFilesystemID_RealPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first, mount, err := FilesystemID(dir)
	require.NoError(t, err)
	second, _, err := FilesystemID(dir)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.NotEmpty(t, first)
	assert.NotEmpty(t, mount)

	_, _, err = FilesystemID(dir + "/missing")
	require.Error(t, err)
}

func FuzzMountSource(f *testing.F) {
	f.Add(sampleMountInfo, "/home/user")
	f.Add("1 2 3 / / rw - ext4 /dev/sda1 rw", "/")
	f.Fuzz(func(_ *testing.T, mountInfo, path string) {
		MountSource(mountInfo, path)
	})
}
