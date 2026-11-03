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

package wuffspng_test

import (
	"bytes"
	"errors"
	"go/build"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/wuffspng"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngTestdata returns the PNG test images shipped with Go's image/png: the
// PngSuite set (every colour type and bit depth, with and without
// transparency) plus interlaced and invalid files.
func pngTestdata(t testing.TB) []string {
	t.Helper()
	dir := filepath.Join(build.Default.GOROOT, "src", "image", "png", "testdata")
	files, err := filepath.Glob(filepath.Join(dir, "*.png"))
	require.NoError(t, err)
	suite, err := filepath.Glob(filepath.Join(dir, "pngsuite", "*.png"))
	require.NoError(t, err)
	files = append(files, suite...)
	if len(files) == 0 {
		t.Skipf("no image/png testdata under %s", dir)
	}
	return files
}

// rgbaOf is c as 8-bit premultiplied RGBA.
func rgbaOf(t *testing.T, c color.Color) color.RGBA {
	t.Helper()
	rgba, ok := color.RGBAModel.Convert(c).(color.RGBA)
	require.True(t, ok)
	return rgba
}

// near reports whether two premultiplied pixels differ by at most one step
// per channel: premultiplying in 8 and in 16 bits rounds differently.
func near(got, want color.RGBA) bool {
	within := func(x, y uint8) bool {
		d := int(x) - int(y)
		return d >= -1 && d <= 1
	}
	return within(got.R, want.R) && within(got.G, want.G) && within(got.B, want.B) && got.A == want.A
}

func requireAvailable(t *testing.T) {
	t.Helper()
	if _, err := wuffspng.Decode([]byte{0}); errors.Is(err, wuffspng.ErrUnavailable) {
		t.Skip("built without cgo")
	}
}

// Every image image/png decodes, Wuffs decodes to the same pixels.
func TestDecode_MatchesImagePNG(t *testing.T) {
	t.Parallel()
	requireAvailable(t)

	compared := 0
	for _, path := range pngTestdata(t) {
		if strings.HasPrefix(filepath.Base(path), "invalid-") {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path) //nolint:gosec // Go's own testdata
			require.NoError(t, err)
			want, err := png.Decode(bytes.NewReader(data))
			require.NoError(t, err)

			got, err := wuffspng.Decode(data)
			require.NoError(t, err)
			require.Equal(t, want.Bounds().Size(), got.Bounds().Size())
			bounds := want.Bounds()
			for y := range bounds.Dy() {
				for x := range bounds.Dx() {
					wantPx := rgbaOf(t, want.At(bounds.Min.X+x, bounds.Min.Y+y))
					gotPx := got.RGBAAt(x, y)
					if !near(gotPx, wantPx) {
						t.Fatalf("pixel (%d,%d): got %v, want %v (%T)", x, y, gotPx, wantPx, want)
					}
				}
			}
		})
		compared++
	}
	assert.Positive(t, compared)
}

// Untrusted input that is not a decodable PNG is an error, not a crash.
func TestDecode_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	requireAvailable(t)

	var valid bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	require.NoError(t, png.Encode(&valid, img))
	badCRC := bytes.Clone(valid.Bytes())
	badCRC[len("\x89PNG\r\n\x1a\n")+8+13] ^= 0xff

	tests := map[string][]byte{
		"empty":     nil,
		"not a png": []byte("not a png at all"),
		"signature": []byte("\x89PNG\r\n\x1a\n"),
		"truncated": valid.Bytes()[:len(valid.Bytes())/2],
		"bad crc":   badCRC,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := wuffspng.Decode(data)
			require.ErrorIs(t, err, wuffspng.ErrDecode)
		})
	}
}

func FuzzDecode(f *testing.F) {
	for _, path := range pngTestdata(f) {
		data, err := os.ReadFile(path) //nolint:gosec // Go's own testdata
		if err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := wuffspng.Decode(data)
		if err != nil {
			return
		}
		// Anything Wuffs accepts is a well-formed image of the size the
		// stream declares, whatever image/png makes of it.
		bounds := got.Bounds()
		require.Len(t, got.Pix, bounds.Dx()*bounds.Dy()*4)
		if cfg, cfgErr := png.DecodeConfig(bytes.NewReader(data)); cfgErr == nil {
			assert.Equal(t, image.Pt(cfg.Width, cfg.Height), bounds.Size())
		}
	})
}
