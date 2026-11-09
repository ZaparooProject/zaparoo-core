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

// Declarations for the vendored OKIM6258 core.

#ifndef ZVGM_OKIM6258_H
#define ZVGM_OKIM6258_H

#include "../EmuStructs.h"

#define MSM6258_DIV_1024 0
#define MSM6258_DIV_768 1
#define MSM6258_DIV_512 2
#define MSM6258_ADPCM_3B 3
#define MSM6258_ADPCM_4B 4
#define MSM6258_OUT_10B 10
#define MSM6258_OUT_12B 12

typedef struct msm6258_config {
	DEV_GEN_CFG _genCfg;
	UINT8 divider;
	UINT8 adpcmBits;
	UINT8 outputBits;
} MSM6258_CFG;

#define OPT_MSM6258_FORCE_12BIT 0x01

extern const DEV_DECL sndDev_MSM6258;

#endif
