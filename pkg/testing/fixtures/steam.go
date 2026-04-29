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

package fixtures

import (
	"bytes"
	"encoding/binary"
	"strconv"
)

const (
	VDFMapStartMarker byte = 0x00
	VDFStringMarker   byte = 0x01
	VDFUint32Marker   byte = 0x02
	VDFMapEndMarker   byte = 0x08
)

type TestShortcut struct {
	AppName       string
	Exe           string
	StartDir      string
	LaunchOptions string
	AppID         uint32
	IsHidden      bool
	Optional      bool
}

func writeVDFString(buf *bytes.Buffer, key, value string) {
	_ = buf.WriteByte(VDFStringMarker)
	_, _ = buf.WriteString(key)
	_ = buf.WriteByte(VDFMapStartMarker)
	_, _ = buf.WriteString(value)
	_ = buf.WriteByte(VDFMapStartMarker)
}

func writeVDFUint32(buf *bytes.Buffer, key string, value uint32) {
	_ = buf.WriteByte(VDFUint32Marker)
	_, _ = buf.WriteString(key)
	_ = buf.WriteByte(VDFMapStartMarker)
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], value)
	_, _ = buf.Write(raw[:])
}

func writeVDFBool(buf *bytes.Buffer, key string, value bool) {
	if value {
		writeVDFUint32(buf, key, 1)
		return
	}

	writeVDFUint32(buf, key, 0)
}

func writeEmptyVDFMap(buf *bytes.Buffer, key string) {
	_ = buf.WriteByte(VDFMapStartMarker)
	_, _ = buf.WriteString(key)
	_ = buf.WriteByte(VDFMapStartMarker)
	_ = buf.WriteByte(VDFMapEndMarker)
}

func BuildShortcutsVDF(shortcuts []TestShortcut) []byte {
	var buf bytes.Buffer

	_ = buf.WriteByte(VDFMapStartMarker)
	_, _ = buf.WriteString("shortcuts")
	_ = buf.WriteByte(VDFMapStartMarker)

	for i, shortcut := range shortcuts {
		_ = buf.WriteByte(VDFMapStartMarker)
		_, _ = buf.WriteString(strconv.Itoa(i))
		_ = buf.WriteByte(VDFMapStartMarker)

		writeVDFUint32(&buf, "appid", shortcut.AppID)
		writeVDFString(&buf, "appname", shortcut.AppName)
		writeVDFString(&buf, "exe", shortcut.Exe)
		writeVDFString(&buf, "startdir", shortcut.StartDir)
		writeVDFString(&buf, "launchoptions", shortcut.LaunchOptions)

		if shortcut.Optional {
			writeVDFString(&buf, "icon", "")
			writeVDFString(&buf, "shortcutpath", "")
			writeVDFBool(&buf, "ishidden", shortcut.IsHidden)
			writeVDFUint32(&buf, "allowdesktopconfig", 1)
			writeVDFUint32(&buf, "allowoverlay", 1)
			writeEmptyVDFMap(&buf, "tags")
		}

		_ = buf.WriteByte(VDFMapEndMarker)
	}

	_ = buf.WriteByte(VDFMapEndMarker)
	_ = buf.WriteByte(VDFMapEndMarker)

	return buf.Bytes()
}
