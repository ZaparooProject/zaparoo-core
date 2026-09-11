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

package misterdocs

import (
	"encoding/xml"
	"io"
	"os"
	"strings"

	"github.com/spf13/afero"
)

// maxMRABytes caps how much of an MRA is parsed. Real MRAs are a few kilobytes;
// the cap bounds a corrupt or hostile file without rejecting a legitimate one.
const maxMRABytes = int64(256 * 1024)

// mraSetNameElement is the element arcade artwork is keyed on. The pack files
// arcade games under the MAME parent setname, which lives inside the MRA and
// not in its filename, so the setname has to be read out of the XML.
const mraSetNameElement = "setname"

// mraExt is the MiSTer arcade descriptor extension.
const mraExt = ".mra"

// readMRASetName extracts <setname> from an MRA. It streams the document and
// stops at the first match, so a malformed tail costs nothing. ok is false when
// the file is unreadable, is not XML, or carries no setname.
func readMRASetName(fs afero.Fs, path string) (setName string, ok bool) {
	info, err := lstat(fs, path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	if info.Size() > maxMRABytes {
		return "", false
	}
	file, err := fs.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()

	decoder := xml.NewDecoder(io.LimitReader(file, maxMRABytes))
	// MRAs in the wild declare legacy encodings and leave tags unclosed. The
	// setname is ASCII either way, so read the bytes as they are rather than
	// rejecting the file over its header.
	decoder.Strict = false
	decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	for {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return "", false
		}
		start, isStart := token.(xml.StartElement)
		if !isStart || !strings.EqualFold(start.Name.Local, mraSetNameElement) {
			continue
		}
		var value string
		if decodeErr := decoder.DecodeElement(&value, &start); decodeErr != nil {
			return "", false
		}
		value = strings.TrimSpace(value)
		return value, value != ""
	}
}
