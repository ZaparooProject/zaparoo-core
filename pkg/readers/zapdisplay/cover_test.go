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
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func solidPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func solidJPEG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}

func TestEncodeCoverPreservesSourceAspect(t *testing.T) {
	t.Parallel()

	// Artwork is not a fixed slot. The source aspect is what the device lays
	// out around, so it must survive encoding rather than being cropped to a
	// shape the panel does not actually require.
	for _, tc := range []struct {
		name         string
		w, h         int
		wantW, wantH int
	}{
		{"portrait box art", 400, 600, 192, coverMaxHeight},
		{"japanese n64 box art", 234, 320, 211, coverMaxHeight},
		{"us n64 box art", 320, 234, 394, coverMaxHeight},
		{"square", 512, 512, coverMaxHeight, coverMaxHeight},
		{"4:3 screenshot", 640, 480, 384, coverMaxHeight},
		// Too wide to fill the height without overrunning the width cap, so it
		// is scaled down and letterboxed instead of losing its sides.
		{"3d box render", 300, 162, coverMaxWidth, 276},
		{"marquee", 1600, 400, coverMaxWidth, 128},
		{"tiny", 8, 12, 192, coverMaxHeight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := encodeCover(solidPNG(t, tc.w, tc.h, color.RGBA{R: 10, G: 20, B: 30, A: 255}), "image/png")
			require.NoError(t, err)
			assert.Equal(t, tc.wantW, out.width)
			assert.Equal(t, tc.wantH, out.height)
			assert.Len(t, out.pixels, out.width*out.height*2)
			assert.LessOrEqual(t, len(out.pixels), coverMaxBytes)
		})
	}
}

func TestEncodeCoverPacksLittleEndianRGB565(t *testing.T) {
	t.Parallel()

	// Pure red is 0xF800 in RGB565, so every pixel must be 0x00 then 0xF8.
	out, err := encodeCover(solidPNG(t, 60, 90, color.RGBA{R: 255, A: 255}), "image/png")
	require.NoError(t, err)
	require.Len(t, out.pixels, out.width*out.height*2)

	for i := 0; i < len(out.pixels); i += 2 {
		require.Equalf(t, byte(0x00), out.pixels[i], "low byte at %d", i)
		require.Equalf(t, byte(0xF8), out.pixels[i+1], "high byte at %d", i)
	}
}

func TestEncodeCoverDetectsFormatFromMagicBytes(t *testing.T) {
	t.Parallel()

	// A wrong or missing content type must not stop a decodable image.
	out, err := encodeCover(solidPNG(t, 120, 180, color.RGBA{B: 255, A: 255}), "application/octet-stream")
	require.NoError(t, err)
	assert.Len(t, out.pixels, out.width*out.height*2)

	out, err = encodeCover(solidJPEG(t, 120, 180, color.RGBA{G: 255, A: 255}), "")
	require.NoError(t, err)
	assert.Len(t, out.pixels, out.width*out.height*2)
}

func TestEncodeCoverRejectsUndecodableInput(t *testing.T) {
	t.Parallel()

	_, err := encodeCover([]byte("not an image at all"), "image/png")
	require.Error(t, err)

	// Truncated PNG: the header matches but the body does not decode.
	full := solidPNG(t, 40, 40, color.White)
	_, err = encodeCover(full[:20], "image/png")
	require.Error(t, err)
}

func TestFitCoverStaysInsideTheDeviceBounds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ w, h int }{
		{1, 1},
		{4000, 3000},
		{3000, 4000},
		{10000, 1},
		{1, 10000},
		{coverMaxWidth, coverMaxHeight},
	} {
		w, h := fitCover(tc.w, tc.h)
		assert.LessOrEqual(t, w, coverMaxWidth)
		assert.LessOrEqual(t, h, coverMaxHeight)
		assert.Positive(t, w)
		assert.Positive(t, h)
		assert.LessOrEqual(t, w*h*2, coverMaxBytes)
	}
}

func TestFitCoverRejectsEmptySources(t *testing.T) {
	t.Parallel()

	w, h := fitCover(0, 100)
	assert.Zero(t, w)
	assert.Zero(t, h)
}

// assetIDPattern is the identifier charset and length the firmware accepts.
// coverAssetID is built to satisfy it; this is the assertion that it does.
var assetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,47}$`)

func TestCoverAssetIDIsDeviceLegalAndStable(t *testing.T) {
	t.Parallel()

	pixels := make([]byte, 1024)
	first := coverAssetID("snes", "/games/SNES/Super Metroid.sfc", pixels)
	second := coverAssetID("snes", "/games/SNES/Super Metroid.sfc", pixels)

	assert.Equal(t, first, second, "same media and artwork must reuse the same asset ID")
	assert.Regexp(t, assetIDPattern, first)

	// Different artwork for the same media must not reuse the ID, or the
	// device would keep showing the stale cover it already holds.
	changed := make([]byte, 1024)
	changed[0] = 0xFF
	assert.NotEqual(t, first, coverAssetID("snes", "/games/SNES/Super Metroid.sfc", changed))

	// Long paths must still produce a legal ID.
	long := coverAssetID("snes", "/"+string(make([]byte, 500)), pixels)
	assert.Regexp(t, assetIDPattern, long)
}

// colorRed is the fixture colour for cover tests.
func colorRed() color.Color {
	return color.RGBA{R: 255, A: 255}
}
