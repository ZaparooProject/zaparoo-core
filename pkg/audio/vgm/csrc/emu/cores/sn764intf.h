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

// Declarations for the vendored SN76496 core.

#ifndef ZVGM_SN764INTF_H
#define ZVGM_SN764INTF_H

#include "../EmuStructs.h"

typedef struct sn76496_config {
	DEV_GEN_CFG _genCfg;
	UINT16 noiseTaps;
	UINT8 shiftRegWidth;
	UINT8 negate;
	UINT8 clkDiv;
	UINT8 ncrPSG;
	UINT8 segaPSG;
	UINT8 stereo;
	// For a T6W28 noise half: the started tone half's chip pointer.
	void *t6w28_tone;
} SN76496_CFG;

#define SN76496_W_REG 0x00
#define SN76496_W_GGST 0x01

#endif
