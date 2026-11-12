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

// Declarations for the vendored OPN family (fmopn.c) core.

#ifndef ZVGM_FMOPN_H
#define ZVGM_FMOPN_H

#include "../../stdtype.h"
#include "../snddef.h"
#include "../EmuStructs.h"

#define BUILD_YM2203 1
#define BUILD_YM2608 1
#define BUILD_YM2610 1
#define BUILD_YM2610B 1
#define BUILD_YM2612 1
#define BUILD_YM3438 1

#define FM_INTERNAL_TIMER 1

// Hooks the OPN cores use to drive the SSG (AY-3-8910 compatible) part,
// which is emulated by a separate AY8910 instance.
typedef struct _ssg_callbacks ssg_callbacks;
struct _ssg_callbacks {
	DEVFUNC_WRITE_CLOCK set_clock;
	DEVFUNC_WRITE_A8D8 write;
	DEVFUNC_READ_A8D8 read;
	DEVFUNC_CTRL reset;
};

typedef void (*FM_TIMERHANDLER)(void *param, UINT8 c, INT32 cnt, UINT32 clock);
typedef void (*FM_IRQHANDLER)(void *param, UINT8 irq);

void *ym2203_init(void *param, UINT32 baseclock, UINT32 rate, FM_TIMERHANDLER TimerHandler, FM_IRQHANDLER IRQHandler);
void ym2203_link_ssg(void *chip, const ssg_callbacks *ssg, void *ssg_param);
void ym2203_set_srchg_cb(void *chip, DEVCB_SRATE_CHG cbFunc, void *dataPtr);
void ym2203_shutdown(void *chip);
void ym2203_reset_chip(void *chip);
void ym2203_update_one(void *chip, UINT32 length, DEV_SMPL **buffer);
void ym2203_write(void *chip, UINT8 a, UINT8 v);
UINT8 ym2203_read(void *chip, UINT8 a);
UINT8 ym2203_timer_over(void *chip, UINT8 c);
void ym2203_set_mute_mask(void *chip, UINT32 MuteMask);
void ym2203_set_log_cb(void *chip, DEVCB_LOG func, void *param);

void *ym2608_init(void *param, UINT32 baseclock, UINT32 rate, FM_TIMERHANDLER TimerHandler, FM_IRQHANDLER IRQHandler);
void ym2608_link_ssg(void *chip, const ssg_callbacks *ssg, void *ssg_param);
void ym2608_set_srchg_cb(void *chip, DEVCB_SRATE_CHG cbFunc, void *dataPtr);
void ym2608_shutdown(void *chip);
void ym2608_reset_chip(void *chip);
void ym2608_update_one(void *chip, UINT32 length, DEV_SMPL **buffer);
void ym2608_write(void *chip, UINT8 a, UINT8 v);
UINT8 ym2608_read(void *chip, UINT8 a);
UINT8 ym2608_timer_over(void *chip, UINT8 c);
void ym2608_alloc_pcmromb(void *chip, UINT32 memsize);
void ym2608_write_pcmromb(void *chip, UINT32 offset, UINT32 length, const UINT8 *data);
void ym2608_set_mute_mask(void *chip, UINT32 MuteMask);
void ym2608_set_log_cb(void *chip, DEVCB_LOG func, void *param);

void *ym2610_init(void *param, UINT32 baseclock, UINT32 rate, FM_TIMERHANDLER TimerHandler, FM_IRQHANDLER IRQHandler);
void ym2610_link_ssg(void *chip, const ssg_callbacks *ssg, void *ssg_param);
void ym2610_shutdown(void *chip);
void ym2610_reset_chip(void *chip);
void ym2610_update_one(void *chip, UINT32 length, DEV_SMPL **buffer);
void ym2610b_update_one(void *chip, UINT32 length, DEV_SMPL **buffer);
void ym2610_write(void *chip, UINT8 a, UINT8 v);
UINT8 ym2610_read(void *chip, UINT8 a);
UINT8 ym2610_timer_over(void *chip, UINT8 c);
void ym2610_alloc_pcmroma(void *chip, UINT32 memsize);
void ym2610_write_pcmroma(void *chip, UINT32 offset, UINT32 length, const UINT8 *data);
void ym2610_alloc_pcmromb(void *chip, UINT32 memsize);
void ym2610_write_pcmromb(void *chip, UINT32 offset, UINT32 length, const UINT8 *data);
void ym2610_set_mute_mask(void *chip, UINT32 MuteMask);
void ym2610_set_log_cb(void *chip, DEVCB_LOG func, void *param);

void *ym2612_init(void *param, UINT32 baseclock, UINT32 rate, FM_TIMERHANDLER TimerHandler, FM_IRQHANDLER IRQHandler);
void ym2612_shutdown(void *chip);
void ym2612_reset_chip(void *chip);
void ym2612_update_one(void *chip, UINT32 length, DEV_SMPL **buffer);
void ym2612_write(void *chip, UINT8 a, UINT8 v);
UINT8 ym2612_read(void *chip, UINT8 a);
UINT8 ym2612_timer_over(void *chip, UINT8 c);
void ym2612_set_mute_mask(void *chip, UINT32 MuteMask);
void ym2612_set_options(void *chip, UINT32 Flags);
void ym2612_set_log_cb(void *chip, DEVCB_LOG func, void *param);

#endif
