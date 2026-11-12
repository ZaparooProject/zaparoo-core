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

// Declarations for the vendored Sega PCM core.

#ifndef ZVGM_SEGAPCM_H
#define ZVGM_SEGAPCM_H

#include "../../stdtype.h"
#include "../EmuStructs.h"

// SEGAPCM_CFG.bnkshift values.
#define SEGAPCM_BANK_256 11
#define SEGAPCM_BANK_512 12
#define SEGAPCM_BANK_12M 13
// SEGAPCM_CFG.bnkmask values.
#define SEGAPCM_BANK_MASK7 0x70
#define SEGAPCM_BANK_MASKF 0xF0
#define SEGAPCM_BANK_MASKF8 0xF8

typedef struct segapcm_config {
	DEV_GEN_CFG _genCfg;
	UINT8 bnkshift;
	UINT8 bnkmask;
} SEGAPCM_CFG;

extern const DEV_DECL sndDev_SegaPCM;

#endif
