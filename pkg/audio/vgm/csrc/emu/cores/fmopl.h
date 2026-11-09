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

// Declarations for the vendored OPL/OPL2/MSX-AUDIO (fmopl.c) core.

#ifndef ZVGM_FMOPL_H
#define ZVGM_FMOPL_H

#include "../../stdtype.h"
#include "../snddef.h"
#include "../logging.h"

#define BUILD_YM3812 1
#define BUILD_YM3526 1
#define BUILD_Y8950 1

typedef void (*OPL_TIMERHANDLER)(void *param, UINT8 timer, UINT32 period);
typedef void (*OPL_IRQHANDLER)(void *param, UINT8 irq);
typedef void (*OPL_UPDATEHANDLER)(void *param);
typedef void (*OPL_PORTHANDLER_W)(void *param, UINT8 data);
typedef UINT8 (*OPL_PORTHANDLER_R)(void *param);

void *ym3812_init(UINT32 clock, UINT32 rate);
void ym3812_shutdown(void *chip);
void ym3812_reset_chip(void *chip);
void ym3812_write(void *chip, UINT8 a, UINT8 v);
UINT8 ym3812_read(void *chip, UINT8 a);
UINT8 ym3812_timer_over(void *chip, UINT8 c);
void ym3812_update_one(void *chip, UINT32 length, DEV_SMPL **buffer);
void ym3812_set_timer_handler(void *chip, OPL_TIMERHANDLER TimerHandler, void *param);
void ym3812_set_irq_handler(void *chip, OPL_IRQHANDLER IRQHandler, void *param);
void ym3812_set_update_handler(void *chip, OPL_UPDATEHANDLER UpdateHandler, void *param);

void *ym3526_init(UINT32 clock, UINT32 rate);
void ym3526_shutdown(void *chip);
void ym3526_reset_chip(void *chip);
void ym3526_write(void *chip, UINT8 a, UINT8 v);
UINT8 ym3526_read(void *chip, UINT8 a);
UINT8 ym3526_timer_over(void *chip, UINT8 c);
void ym3526_update_one(void *chip, UINT32 length, DEV_SMPL **buffer);
void ym3526_set_timer_handler(void *chip, OPL_TIMERHANDLER TimerHandler, void *param);
void ym3526_set_irq_handler(void *chip, OPL_IRQHANDLER IRQHandler, void *param);
void ym3526_set_update_handler(void *chip, OPL_UPDATEHANDLER UpdateHandler, void *param);

void y8950_set_port_handler(void *chip, OPL_PORTHANDLER_W PortHandler_w, OPL_PORTHANDLER_R PortHandler_r, void *param);
void y8950_set_keyboard_handler(void *chip, OPL_PORTHANDLER_W KeyboardHandler_w, OPL_PORTHANDLER_R KeyboardHandler_r, void *param);
void y8950_alloc_pcmrom(void *chip, UINT32 memsize);
void y8950_write_pcmrom(void *chip, UINT32 offset, UINT32 length, const UINT8 *data);
void *y8950_init(UINT32 clock, UINT32 rate);
void y8950_shutdown(void *chip);
void y8950_reset_chip(void *chip);
void y8950_write(void *chip, UINT8 a, UINT8 v);
UINT8 y8950_read(void *chip, UINT8 a);
UINT8 y8950_timer_over(void *chip, UINT8 c);
void y8950_update_one(void *chip, UINT32 length, DEV_SMPL **buffer);
void y8950_set_timer_handler(void *chip, OPL_TIMERHANDLER TimerHandler, void *param);
void y8950_set_irq_handler(void *chip, OPL_IRQHANDLER IRQHandler, void *param);
void y8950_set_update_handler(void *chip, OPL_UPDATEHANDLER UpdateHandler, void *param);

void opl_set_mute_mask(void *chip, UINT32 MuteMask);
void opl_set_log_cb(void *chip, DEVCB_LOG func, void *param);

#endif
