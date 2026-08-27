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

package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Parsing is pure string handling, so these run on every platform even though
// /proc/self/mountinfo only exists on Linux.

func TestParseMountInfo(t *testing.T) {
	t.Parallel()
	data := `21 26 179:1 / /media/fat rw,noatime - exfat /dev/mmcblk0p1 rw,iocharset=utf8
36 21 0:41 /saves /media/fat/saves rw,relatime shared:1 - cifs //10.0.0.3/MiSTer rw,username=x
40 21 179:1 /System\040Volume\040Information /media/fat/svi rw - exfat /dev/mmcblk0p1 rw
garbage line
`
	entries := ParseMountInfo(data)
	require.Len(t, entries, 3)
	assert.Equal(t, MountEntry{
		Root: "/", Mountpoint: "/media/fat", Options: "rw,noatime", FSType: "exfat", Source: "/dev/mmcblk0p1",
	}, entries[0])
	assert.Equal(t, MountEntry{
		Root: "/saves", Mountpoint: "/media/fat/saves", Options: "rw,relatime", FSType: "cifs",
		Source: "//10.0.0.3/MiSTer",
	}, entries[1])
	assert.Equal(t, "/System Volume Information", entries[2].Root, "octal escapes decoded")
}

// The optional-field count between the options and the "-" separator varies
// per line, which is why the parser searches for the separator rather than
// indexing a fixed position. A line with several optional fields and one with
// none must both parse to the same shape.
func TestParseMountInfo_VariableOptionalFields(t *testing.T) {
	t.Parallel()
	data := "1 0 0:1 / /a rw shared:1 master:2 propagate_from:3 - ext4 /dev/root rw\n" +
		"2 0 0:2 / /b rw - ext4 /dev/other rw\n"
	entries := ParseMountInfo(data)
	require.Len(t, entries, 2)
	assert.Equal(t, "ext4", entries[0].FSType)
	assert.Equal(t, "/dev/root", entries[0].Source)
	assert.Equal(t, "ext4", entries[1].FSType)
	assert.Equal(t, "/dev/other", entries[1].Source)
}

func TestParseMountInfo_Empty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, ParseMountInfo(""))
	assert.Empty(t, ParseMountInfo("not a mountinfo line at all\n"))
}

func TestUnescapeMountField(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "plain", unescapeMountField("plain"))
	assert.Equal(t, "System Volume Information", unescapeMountField(`System\040Volume\040Information`))
	assert.Equal(t, `back\slash`, unescapeMountField(`back\slash`), "non-octal backslash sequence is left as-is")
}
