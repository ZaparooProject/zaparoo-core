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

#ifndef ZAPAROO_WUFFSPNG_H
#define ZAPAROO_WUFFSPNG_H

#include <stddef.h>
#include <stdint.h>

// wuffspng_config reads a PNG's dimensions and the work buffer its decode
// needs. It returns NULL on success and Wuffs' status message otherwise.
const char* wuffspng_config(const uint8_t* data, size_t len, uint32_t* width, uint32_t* height,
                            uint64_t* workbuf_len);

// wuffspng_decode decodes a PNG into pix as premultiplied RGBA, 4 bytes per
// pixel with no row padding. pix and workbuf are sized from
// wuffspng_config. It returns NULL on success and Wuffs' status message
// otherwise.
const char* wuffspng_decode(const uint8_t* data, size_t len, uint8_t* pix, size_t pix_len, uint8_t* workbuf,
                            size_t workbuf_len);

#endif
