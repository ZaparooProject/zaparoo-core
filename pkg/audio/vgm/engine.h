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

// Our VGM chip engine: starts chip cores, applies timed register writes,
// renders every chip at its native rate, resamples each chip to the output
// rate and mixes them into one stereo stream.

#ifndef ZVGM_ENGINE_H
#define ZVGM_ENGINE_H

#include <stdint.h>

#define ZVGM_MAX_FRAMES 4096
#define ZVGM_MAX_CHIPS 128
#define ZVGM_MAX_NATIVE_RATE 4000000 // chips rendering faster than this are refused
// Cores derive their rates and step sizes by dividing the clock, so a clock
// far below any real chip's reaches a division by zero inside their start.
#define ZVGM_MIN_CLOCK 100000 // every real chip runs at 500 kHz or more

// Write kinds.
#define ZVGM_W8 0   // 8-bit address, 8-bit data
#define ZVGM_WM8 1  // 16-bit address, 8-bit data
#define ZVGM_WD16 2 // 8-bit address, 16-bit data
#define ZVGM_WM16 3 // 16-bit address, 16-bit data
#define ZVGM_AYSTEREO 4 // AY8910 stereo mask in data

// Memory regions for zvgm_mem.
#define ZVGM_MEM_ROM 0   // main sample ROM (YM2610: ADPCM-A)
#define ZVGM_MEM_DELTAT 1 // YM2608/YM2610/Y8950 ADPCM-B memory
#define ZVGM_MEM_RAM 2   // fixed-size RAM written at an absolute offset

typedef struct zvgm_write {
	uint32_t frame; // output frame, relative to the render call
	uint16_t addr;
	uint16_t data;
	uint8_t chip;
	uint8_t kind;
	uint8_t pad[2];
} zvgm_write;

typedef struct zvgm_chip_cfg {
	uint32_t clock;
	uint32_t flags;
	int32_t volL; // 8.8 fixed point
	int32_t volR;
	int32_t ssgVolL;
	int32_t ssgVolR;
	uint8_t type; // VGM chip index
	uint8_t accurate;
	uint8_t p[6]; // chip-specific header bytes
} zvgm_chip_cfg;

typedef struct zvgm_engine zvgm_engine;

zvgm_engine *zvgm_new(uint32_t outRate);
void zvgm_free(zvgm_engine *e);
int zvgm_add_chip(zvgm_engine *e, const zvgm_chip_cfg *cfg);
void zvgm_mem(zvgm_engine *e, int chip, int region, uint32_t romSize, uint32_t start,
	const uint8_t *data, uint32_t len);
int zvgm_render(zvgm_engine *e, const zvgm_write *w, int nw, float *out, int frames);

#endif
