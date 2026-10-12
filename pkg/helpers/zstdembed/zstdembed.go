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

// Package zstdembed reads and writes the zstd-compressed data files Core
// embeds. Generators under scripts/ write them with Compress; the packages
// that embed them read them back with NewReader.
package zstdembed

import (
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// maxDecodedMemory bounds what one embedded file may make the decoder
// allocate. Embedded files are a few megabytes decoded.
const maxDecodedMemory = 64 << 20

// windowSize is the match window files are written with. The decoder holds a
// window of this size, so a small one keeps reading cheap on low-memory
// devices; on Core's catalogs it costs under 3% in file size against the
// encoder's default.
const windowSize = 256 << 10

// NewReader decodes one embedded file. It decodes on the calling goroutine
// and keeps its buffers small, which suits the single-core, low-memory
// devices Core runs on. Close it when done.
func NewReader(r io.Reader) (io.ReadCloser, error) {
	dec, err := zstd.NewReader(r,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
		zstd.WithDecoderMaxMemory(maxDecodedMemory),
	)
	if err != nil {
		return nil, fmt.Errorf("zstd reader: %w", err)
	}
	return dec.IOReadCloser(), nil
}

// Compress encodes data at the best ratio the encoder offers. The output is
// the same for the same input, so a regenerated file only changes when its
// content does.
func Compress(data []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedBestCompression),
		zstd.WithEncoderConcurrency(1),
		zstd.WithWindowSize(windowSize),
	)
	if err != nil {
		return nil, fmt.Errorf("zstd writer: %w", err)
	}
	out := enc.EncodeAll(data, nil)
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("zstd writer: %w", err)
	}
	return out, nil
}
