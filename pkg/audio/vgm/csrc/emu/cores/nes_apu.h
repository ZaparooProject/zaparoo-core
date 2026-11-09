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

// Declarations for the vendored NES APU core.

#ifndef ZVGM_NES_APU_H
#define ZVGM_NES_APU_H

#include "../../stdtype.h"
#include "../snddef.h"

typedef struct _nesapu_state nesapu_state;

UINT8 nes_apu_read(void *chip, UINT8 address);
void nes_apu_write(void *chip, UINT8 offset, UINT8 data);
void nes_apu_update(void *chip, UINT32 samples, DEV_SMPL **outputs);
void *device_start_nesapu(UINT32 clock, UINT32 rate);
void device_stop_nesapu(void *chip);
void device_reset_nesapu(void *chip);
void nesapu_set_rom(void *chip, const UINT8 *ROMData);
void nesapu_set_mute_mask(void *chip, UINT32 MuteMask);
UINT32 nesapu_get_mute_mask(void *chip);
void nesapu_set_options(void *chip, UINT32 Flags);
void nesapu_set_panning(void *chip, INT16 square1, INT16 square2, INT16 triangle, INT16 noise, INT16 dpcm);

#endif
