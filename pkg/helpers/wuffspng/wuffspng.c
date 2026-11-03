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

// Only the modules a PNG decode needs are compiled.
#define WUFFS_IMPLEMENTATION
#define WUFFS_CONFIG__MODULES
#define WUFFS_CONFIG__MODULE__BASE
#define WUFFS_CONFIG__MODULE__ADLER32
#define WUFFS_CONFIG__MODULE__CRC32
#define WUFFS_CONFIG__MODULE__DEFLATE
#define WUFFS_CONFIG__MODULE__PNG
#define WUFFS_CONFIG__MODULE__ZLIB
#include "wuffs-v0.4.c"

#include "wuffspng.h"

static const char* wuffspng_out_of_memory = "wuffspng: out of memory";

// wuffspng_start allocates a decoder and reads the image config from src.
static const char* wuffspng_start(wuffs_png__decoder** out, wuffs_base__image_config* config,
                                  wuffs_base__io_buffer* src) {
  size_t size = sizeof__wuffs_png__decoder();
  wuffs_png__decoder* dec = (wuffs_png__decoder*)malloc(size);
  if (!dec) {
    return wuffspng_out_of_memory;
  }
  wuffs_base__status status = wuffs_png__decoder__initialize(
      dec, size, WUFFS_VERSION, WUFFS_INITIALIZE__LEAVE_INTERNAL_BUFFERS_UNINITIALIZED);
  if (status.repr) {
    free(dec);
    return status.repr;
  }
  status = wuffs_png__decoder__decode_image_config(dec, config, src);
  if (status.repr) {
    free(dec);
    return status.repr;
  }
  *out = dec;
  return NULL;
}

const char* wuffspng_config(const uint8_t* data, size_t len, uint32_t* width, uint32_t* height,
                            uint64_t* workbuf_len) {
  wuffs_png__decoder* dec = NULL;
  wuffs_base__image_config config;
  wuffs_base__io_buffer src = wuffs_base__ptr_u8__reader((uint8_t*)data, len, true);
  const char* err = wuffspng_start(&dec, &config, &src);
  if (err) {
    return err;
  }
  *width = wuffs_base__pixel_config__width(&config.pixcfg);
  *height = wuffs_base__pixel_config__height(&config.pixcfg);
  *workbuf_len = wuffs_png__decoder__workbuf_len(dec).max_incl;
  free(dec);
  return NULL;
}

const char* wuffspng_decode(const uint8_t* data, size_t len, uint8_t* pix, size_t pix_len, uint8_t* workbuf,
                            size_t workbuf_len) {
  wuffs_png__decoder* dec = NULL;
  wuffs_base__image_config config;
  wuffs_base__io_buffer src = wuffs_base__ptr_u8__reader((uint8_t*)data, len, true);
  const char* err = wuffspng_start(&dec, &config, &src);
  if (err) {
    return err;
  }
  uint32_t width = wuffs_base__pixel_config__width(&config.pixcfg);
  uint32_t height = wuffs_base__pixel_config__height(&config.pixcfg);
  wuffs_base__pixel_config__set(&config.pixcfg, WUFFS_BASE__PIXEL_FORMAT__RGBA_PREMUL,
                                WUFFS_BASE__PIXEL_SUBSAMPLING__NONE, width, height);
  wuffs_base__pixel_buffer pb;
  wuffs_base__status status =
      wuffs_base__pixel_buffer__set_from_slice(&pb, &config.pixcfg, wuffs_base__make_slice_u8(pix, pix_len));
  if (!status.repr) {
    status = wuffs_png__decoder__decode_frame(dec, &pb, &src, WUFFS_BASE__PIXEL_BLEND__SRC,
                                              wuffs_base__make_slice_u8(workbuf, workbuf_len), NULL);
  }
  free(dec);
  return status.repr;
}
