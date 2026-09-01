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

package zapdisplay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"strings"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// coverAssetID derives a stable, device-legal identifier for a cover.
//
// The checksum is part of the ID so that re-scraped artwork for the same media
// produces a different ID and is uploaded rather than silently re-selected.
func coverAssetID(systemID, path string, pixels []byte) string {
	digest := sha256.Sum256([]byte(systemID + "\x00" + path))
	id := fmt.Sprintf("c%s-%08x", hex.EncodeToString(digest[:6]), crc32.ChecksumIEEE(pixels))
	if len(id) > 47 {
		id = id[:47]
	}
	return id
}

// fitCover scales a source image's dimensions into the display's artwork box.
//
// Height is filled first, because the panel is short and wide and vertical
// space is what artwork is starved of. Anything that would then be too wide for
// the device's detail column is scaled down rather than cropped: the aspect the
// artwork was drawn at is the thing worth preserving, and the device lays out
// around whatever it is sent.
func fitCover(width, height int) (fittedWidth, fittedHeight int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	scaledW := (coverMaxHeight*width + height/2) / height
	if scaledW < 1 {
		scaledW = 1
	}
	if scaledW <= coverMaxWidth {
		return scaledW, coverMaxHeight
	}
	scaledH := (coverMaxWidth*height + width/2) / width
	if scaledH < 1 {
		scaledH = 1
	}
	return coverMaxWidth, scaledH
}

// encodedCover is artwork ready for the device: packed pixels plus the size
// they were packed at. The size travels with the pixels because artwork is not
// a fixed slot, so ASSET_USE has to restate it.
type encodedCover struct {
	pixels []byte
	width  int
	height int
}

// encodeCover converts artwork into the little-endian RGB565 buffer the device
// renders, along with the dimensions it was encoded at.
//
// The device is told the size rather than assuming one, so box art keeps its
// own aspect: portrait for most console art, landscape for the roughly 4:3
// covers several systems use, and wider still for a marquee or a screenshot.
func encodeCover(data []byte, contentType string) (encodedCover, error) {
	src, err := decodeImage(data, contentType)
	if err != nil {
		return encodedCover{}, err
	}

	width, height := fitCover(src.Bounds().Dx(), src.Bounds().Dy())
	if width == 0 || height == 0 {
		return encodedCover{}, fmt.Errorf("cover has no pixels: %dx%d", src.Bounds().Dx(), src.Bounds().Dy())
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)

	out := make([]byte, 0, width*height*2)
	for y := range height {
		for x := range width {
			// RGBA() returns 16-bit premultiplied channels; the high byte is
			// the 8-bit value RGB565 is packed from. Masking to a byte before
			// each conversion keeps every step provably in range.
			r, g, b, _ := dst.At(x, y).RGBA()
			r8 := uint16((r >> 8) & 0xFF)
			g8 := uint16((g >> 8) & 0xFF)
			b8 := uint16((b >> 8) & 0xFF)
			pixel := (r8&0xF8)<<8 | (g8&0xFC)<<3 | b8>>3
			out = append(out, byte(pixel&0xFF), byte((pixel>>8)&0xFF))
		}
	}
	if len(out) != width*height*2 {
		return encodedCover{}, fmt.Errorf("encoded cover is %d bytes, expected %d", len(out), width*height*2)
	}
	return encodedCover{pixels: out, width: width, height: height}, nil
}

// decodeImage decodes the formats Core's artwork pipeline can produce. It
// dispatches on content type first and falls back to magic bytes, because a
// scraped file's recorded type is not always right.
func decodeImage(data []byte, contentType string) (image.Image, error) {
	switch {
	case strings.Contains(contentType, "png"):
		return decodePNG(data)
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return decodeJPEG(data)
	case strings.Contains(contentType, "webp"):
		return decodeWebP(data)
	}

	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return decodePNG(data)
	case len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8:
		return decodeJPEG(data)
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return decodeWebP(data)
	}
	return nil, fmt.Errorf("unsupported cover image type %q", contentType)
}

func decodePNG(data []byte) (image.Image, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode PNG cover: %w", err)
	}
	return img, nil
}

func decodeJPEG(data []byte) (image.Image, error) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode JPEG cover: %w", err)
	}
	return img, nil
}

func decodeWebP(data []byte) (image.Image, error) {
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode WebP cover: %w", err)
	}
	return img, nil
}
