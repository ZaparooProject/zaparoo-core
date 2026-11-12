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

#ifndef ZVGM_AY8910_H
#define ZVGM_AY8910_H

#include "../../stdtype.h"
#include "../snddef.h"
#include "../EmuStructs.h"

extern DEV_DEF devDef_AY8910_MAME;

#define ALL_8910_CHANNELS -1
#define AY8910_INTERNAL_RESISTANCE (356)
#define YM2149_INTERNAL_RESISTANCE (353)

// AY8910 output flags (VGM header byte 0x79 and friends).
#define AY8910_LEGACY_OUTPUT (0x01)
#define AY8910_SINGLE_OUTPUT (0x02)
#define AY8910_DISCRETE_OUTPUT (0x04)
#define AY8910_RESISTOR_OUTPUT (0x08)

typedef struct _ay8910_context ay8910_context;

UINT8 device_start_ay8910_mame(const AY8910_CFG *cfg, DEV_INFO *retDevInf);
UINT32 ay8910_start(void **chip, UINT32 clock, UINT8 ay_type, UINT8 ay_flags);
void ay8910_stop(void *chip);
void ay8910_reset(void *chip);
void ay8910_set_clock(void *chip, UINT32 clock);
UINT32 ay8910_get_sample_rate(void *chip);
void ay8910_write(void *chip, UINT8 addr, UINT8 data);
UINT8 ay8910_read(void *chip, UINT8 addr);
void ay8910_write_reg(ay8910_context *psg, UINT8 r, UINT8 v);
void ay8910_update_one(void *param, UINT32 samples, DEV_SMPL **outputs);
void ay8910_set_mute_mask(void *chip, UINT32 MuteMask);
void ay8910_set_stereo_mask(void *chip, UINT32 StereoMask);
void ay8910_set_srchg_cb(void *chip, DEVCB_SRATE_CHG CallbackFunc, void *DataPtr);
void ay8910_set_log_cb(void *chip, DEVCB_LOG func, void *param);

#endif
