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

// Declarations for the vendored AY-3-8910 core.

#ifndef ZVGM_AYINTF_H
#define ZVGM_AYINTF_H

#include "../EmuStructs.h"

#define AYTYPE_AY8910 0x00
#define AYTYPE_AY8912 0x01
#define AYTYPE_AY8913 0x02
#define AYTYPE_AY8930 0x03
#define AYTYPE_AY8914 0x04
#define AYTYPE_YM2149 0x10
#define AYTYPE_YM3439 0x11
#define AYTYPE_YMZ284 0x12
#define AYTYPE_YMZ294 0x13
#define AYTYPE_YM2203 0x20
#define AYTYPE_YM2608 0x21
#define AYTYPE_YM2610 0x22

#define YM2149_PIN26_HIGH 0x00
#define YM2149_PIN26_LOW 0x10
#define AY8910_ZX_STEREO 0x80

typedef struct ay8910_config {
	DEV_GEN_CFG _genCfg;
	UINT8 chipType;
	UINT8 chipFlags;
} AY8910_CFG;

#define OPT_AY8910_PCM3CH_DETECT 0x01

#endif
