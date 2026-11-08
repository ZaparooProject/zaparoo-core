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

// Package wuffspng decodes PNG images with Wuffs, Google's memory-safe image
// decoders, compiled from the vendored single-file C release wuffs-v0.4.c
// (google/wuffs commit 7f70dda95c77b10c087c0908790837b5e9c15713, licensed
// Apache-2.0 OR MIT; see LICENSE-APACHE and LICENSE-MIT).
//
// On a MiSTer it decoded real 512x700 cover art 2.8 times faster than
// image/png, with identical pixels. Builds without cgo report ErrUnavailable
// so callers fall back to image/png.
package wuffspng

import "errors"

var (
	// ErrUnavailable is returned by builds without cgo.
	ErrUnavailable = errors.New("wuffspng: not available without cgo")
	// ErrTooLarge is returned when the decoded image cannot be addressed.
	ErrTooLarge = errors.New("wuffspng: image too large")
	// ErrDecode wraps Wuffs' own status message for an invalid PNG.
	ErrDecode = errors.New("wuffspng: decode failed")
)
