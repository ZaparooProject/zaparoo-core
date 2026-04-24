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

package shared

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

// pngIENDTail is the fixed 12-byte sequence that ends every valid PNG:
// 4 bytes data length (0), 4 bytes chunk type "IEND", 4 bytes CRC.
var pngIENDTail = []byte{0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}

// ScreenshotFileComplete avoids reading a partial file when inotify fires
// before the writer has flushed and closed.
func ScreenshotFileComplete(path, ext string) (bool, error) {
	switch ext {
	case ".png":
		return PNGFileComplete(path)
	case ".bmp":
		return BMPFileComplete(path)
	default:
		return false, fmt.Errorf("unsupported screenshot format: %s", ext)
	}
}

func PNGFileComplete(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat png: %w", err)
	}
	if info.Size() < int64(len(pngIENDTail)) {
		return false, nil
	}

	//nolint:gosec // Safe: reads screenshot from controlled application directory
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open png: %w", err)
	}
	defer func() { _ = f.Close() }()

	tail := make([]byte, len(pngIENDTail))
	if _, err := f.ReadAt(tail, info.Size()-int64(len(tail))); err != nil {
		return false, fmt.Errorf("read png tail: %w", err)
	}

	return bytes.Equal(tail, pngIENDTail), nil
}

// BMPFileComplete checks the file size against the total size declared at
// bytes 2-5 of the BMP header (little-endian uint32).
func BMPFileComplete(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat bmp: %w", err)
	}
	if info.Size() < 6 {
		return false, nil
	}

	//nolint:gosec // Safe: reads screenshot from controlled application directory
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open bmp: %w", err)
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, 6)
	if _, err := f.ReadAt(header, 0); err != nil {
		return false, fmt.Errorf("read bmp header: %w", err)
	}

	if header[0] != 'B' || header[1] != 'M' {
		return false, nil
	}

	declaredSize := int64(binary.LittleEndian.Uint32(header[2:6]))
	if declaredSize == 0 {
		return false, nil
	}

	return info.Size() == declaredSize, nil
}
