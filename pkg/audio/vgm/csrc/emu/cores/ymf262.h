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

// Declarations for the vendored YMF262 (OPL3) core.

#ifndef ZVGM_YMF262_H
#define ZVGM_YMF262_H

#include "../../stdtype.h"
#include "../snddef.h"
#include "../logging.h"

typedef void (*OPL3_TIMERHANDLER)(void *param, UINT8 timer, UINT32 period);
typedef void (*OPL3_IRQHANDLER)(void *param, UINT8 irq);
typedef void (*OPL3_UPDATEHANDLER)(void *param);

void *ymf262_init(UINT32 clock, UINT32 rate);
void ymf262_shutdown(void *chip);
void ymf262_reset_chip(void *chip);
void ymf262_write(void *chip, UINT8 a, UINT8 v);
UINT8 ymf262_read(void *chip, UINT8 a);
UINT8 ymf262_timer_over(void *chip, UINT8 c);
void ymf262_update_one(void *_chip, UINT32 length, DEV_SMPL **buffers);
void ymf262_set_timer_handler(void *chip, OPL3_TIMERHANDLER TimerHandler, void *param);
void ymf262_set_irq_handler(void *chip, OPL3_IRQHANDLER IRQHandler, void *param);
void ymf262_set_update_handler(void *chip, OPL3_UPDATEHANDLER UpdateHandler, void *param);
void ymf262_set_mute_mask(void *chip, UINT32 MuteMask);
void ymf262_set_volume(void *chip, INT32 volume);
void ymf262_set_vol_lr(void *chip, INT32 volLeft, INT32 volRight);
void ymf262_set_log_cb(void *chip, DEVCB_LOG func, void *param);

#endif
