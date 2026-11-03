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

//go:build cgo

package wuffspng

/*
#include "wuffspng.h"
*/
import "C"

import (
	"fmt"
	"image"
	"math"
	"unsafe"
)

// Decode decodes a PNG into a premultiplied RGBA image, the form image
// scaling and compositing work in.
func Decode(data []byte) (*image.RGBA, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty input", ErrDecode)
	}
	src := (*C.uint8_t)(unsafe.Pointer(&data[0]))
	var width, height C.uint32_t
	var workbufLen C.uint64_t
	if msg := C.wuffspng_config(src, C.size_t(len(data)), &width, &height, &workbufLen); msg != nil {
		return nil, fmt.Errorf("%w: %s", ErrDecode, C.GoString(msg))
	}
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("%w: empty image", ErrDecode)
	}
	pixLen := uint64(width) * uint64(height) * 4
	if pixLen > math.MaxInt32 || uint64(workbufLen) > math.MaxInt32 {
		return nil, ErrTooLarge
	}
	img := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	var workbuf *C.uint8_t
	if workbufLen > 0 {
		buf := make([]byte, int(workbufLen))
		workbuf = (*C.uint8_t)(unsafe.Pointer(&buf[0]))
	}
	if msg := C.wuffspng_decode(
		src, C.size_t(len(data)),
		(*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.size_t(len(img.Pix)),
		workbuf, C.size_t(workbufLen), //nolint:unconvert // uint64 to size_t differs on 32-bit
	); msg != nil {
		return nil, fmt.Errorf("%w: %s", ErrDecode, C.GoString(msg))
	}
	return img, nil
}
