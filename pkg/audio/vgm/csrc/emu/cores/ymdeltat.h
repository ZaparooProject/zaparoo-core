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


// Our own Yamaha ADPCM-B ("DELTA-T") unit for the YM2608, YM2610 and Y8950
// cores. The field and function names follow what fmopn.c and fmopl.c use.

#ifndef ZVGM_YMDELTAT_H
#define ZVGM_YMDELTAT_H

#include "../../stdtype.h"
#include "../logging.h"

#define YM_DELTAT_EMULATION_MODE_NORMAL 0
#define YM_DELTAT_EMULATION_MODE_YM2610 1

typedef void (*STATUS_CHANGE_HANDLER)(void *chip, UINT8 status_bits);

typedef struct deltat_adpcm_state {
	DEV_LOGGER *logger;
	UINT8 *memory;
	UINT32 memory_size;
	UINT32 memory_mask;
	INT32 *output_pointer;
	INT32 *pan;
	double freqbase;
	INT32 output_range;

	UINT32 now_addr;  /* nibble address */
	UINT32 now_step;  /* 16.16 phase */
	UINT32 step;
	UINT32 start;     /* byte addresses */
	UINT32 end;
	UINT32 limit;
	INT32 acc;
	INT32 prev_acc;
	INT32 adpcmd;
	INT32 adpcml;
	INT32 volume;
	UINT16 delta;
	UINT8 now_data;
	UINT8 cpu_data;
	UINT8 portstate;
	UINT8 control2;
	UINT8 portshift;
	UINT8 DRAMportshift;
	UINT8 memread;
	UINT8 limit_set;
	UINT8 reg[16];
	UINT8 emulation_mode;
	UINT8 PCM_BSY;

	STATUS_CHANGE_HANDLER status_set_handler;
	STATUS_CHANGE_HANDLER status_reset_handler;
	void *status_change_which_chip;
	UINT8 status_change_EOS_bit;
	UINT8 status_change_BRDY_bit;
	UINT8 status_change_ZERO_bit;
} YM_DELTAT;

void YM_DELTAT_ADPCM_Init(YM_DELTAT *D, UINT8 mode, UINT8 portshift,
	INT32 *output_pointer, INT32 output_range);
void YM_DELTAT_ADPCM_Reset(YM_DELTAT *D, int pan);
void YM_DELTAT_ADPCM_Write(YM_DELTAT *D, int r, int v);
UINT8 YM_DELTAT_ADPCM_Read(YM_DELTAT *D);
void YM_DELTAT_ADPCM_CALC(YM_DELTAT *D);
void YM_DELTAT_calc_mem_mask(YM_DELTAT *D);

#endif
