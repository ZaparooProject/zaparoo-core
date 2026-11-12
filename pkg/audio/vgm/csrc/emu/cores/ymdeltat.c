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


// Yamaha ADPCM-B decoder written for Zaparoo from the documented chip
// behaviour: 4-bit samples, a step size scaled by a fixed table after every
// nibble, and a delta-N register that sets the playback rate.

#include <stddef.h>
#include <string.h>

#include "ymdeltat.h"

#define DT_STEP_MIN 127
#define DT_STEP_MAX 24576
#define DT_PHASE_BITS 16
#define DT_PHASE_ONE (1u << DT_PHASE_BITS)

/* Step-size multipliers (out of 64) indexed by the low three nibble bits. */
static const INT32 dt_scale[8] = { 57, 57, 57, 57, 77, 102, 128, 153 };

static void dt_status_set(YM_DELTAT *D, UINT8 bits)
{
	if (bits && D->status_set_handler != NULL)
		D->status_set_handler(D->status_change_which_chip, bits);
}

static void dt_status_reset(YM_DELTAT *D, UINT8 bits)
{
	if (bits && D->status_reset_handler != NULL)
		D->status_reset_handler(D->status_change_which_chip, bits);
}

static UINT32 dt_shift(const YM_DELTAT *D)
{
	return (UINT32)(D->portshift - D->DRAMportshift);
}

static void dt_update_addresses(YM_DELTAT *D)
{
	UINT32 sh = dt_shift(D);
	UINT32 fill = (1u << sh) - 1u;

	D->start = ((UINT32)D->reg[0x03] << 8 | D->reg[0x02]) << sh;
	D->end = (((UINT32)D->reg[0x05] << 8 | D->reg[0x04]) << sh) + fill;
	/* Only chips that program the limit register wrap at it. */
	if (D->limit_set)
		D->limit = (((UINT32)D->reg[0x0D] << 8 | D->reg[0x0C]) << sh) + fill;
}

static UINT8 dt_mem_read(const YM_DELTAT *D, UINT32 byteAddr)
{
	if (D->memory == NULL || D->memory_size == 0)
		return 0x80;
	byteAddr &= D->memory_mask;
	if (byteAddr >= D->memory_size)
		return 0x80;
	return D->memory[byteAddr];
}

static void dt_restart(YM_DELTAT *D)
{
	D->now_addr = D->start << 1;
	D->now_step = 0;
	D->acc = 0;
	D->prev_acc = 0;
	D->adpcml = 0;
	D->adpcmd = DT_STEP_MIN;
}

void YM_DELTAT_calc_mem_mask(YM_DELTAT *D)
{
	UINT32 mask = 1;

	while (mask < D->memory_size && mask < 0x80000000u)
		mask <<= 1;
	D->memory_mask = mask - 1u;
}

void YM_DELTAT_ADPCM_Init(YM_DELTAT *D, UINT8 mode, UINT8 portshift,
	INT32 *output_pointer, INT32 output_range)
{
	D->emulation_mode = mode;
	D->portshift = portshift;
	D->output_pointer = output_pointer;
	D->output_range = output_range;
	D->pan = output_pointer;
}

void YM_DELTAT_ADPCM_Reset(YM_DELTAT *D, int pan)
{
	memset(D->reg, 0, sizeof(D->reg));
	D->now_addr = 0;
	D->now_step = 0;
	D->step = 0;
	D->start = 0;
	D->end = 0;
	D->limit = 0xFFFFFFFFu;
	D->limit_set = 0;
	D->delta = 0;
	D->volume = 0;
	D->acc = 0;
	D->prev_acc = 0;
	D->adpcmd = DT_STEP_MIN;
	D->adpcml = 0;
	D->now_data = 0;
	D->cpu_data = 0;
	D->memread = 0;
	D->PCM_BSY = 0;
	D->pan = (pan >= 0 && pan < 4) ? &D->output_pointer[pan] : D->output_pointer;
	if (D->emulation_mode == YM_DELTAT_EMULATION_MODE_YM2610) {
		D->portstate = 0x20;
		D->control2 = 0x01;
		D->DRAMportshift = 0;
	} else {
		D->portstate = 0x00;
		D->control2 = 0x00;
		/* 1-bit DRAM addresses in 32-byte steps instead of 256. */
		D->DRAMportshift = 3;
	}
}

static void dt_write_control1(YM_DELTAT *D, UINT8 v)
{
	if (D->emulation_mode == YM_DELTAT_EMULATION_MODE_YM2610)
		v |= 0x20;
	D->portstate = v & ~0x01;
	dt_status_reset(D, D->status_change_EOS_bit | D->status_change_BRDY_bit);

	if (v & 0x80) {
		dt_restart(D);
		D->memread = 2;
		D->PCM_BSY = (v & 0x20) ? 1 : 0;
		/* Memory transfer or CPU-fed playback signals ready at once. */
		if ((v & 0x60) != 0x20)
			dt_status_set(D, D->status_change_BRDY_bit);
	}
	if (v & 0x01) {
		D->portstate = 0x00;
		D->PCM_BSY = 0;
		D->adpcml = 0;
		D->prev_acc = 0;
		D->acc = 0;
		if (D->emulation_mode == YM_DELTAT_EMULATION_MODE_YM2610)
			D->portstate = 0x20;
	}
}

static void dt_write_control2(YM_DELTAT *D, UINT8 v)
{
	if (D->emulation_mode == YM_DELTAT_EMULATION_MODE_YM2610) {
		D->pan = &D->output_pointer[(v >> 6) & 0x03];
		return;
	}
	D->pan = &D->output_pointer[(v >> 6) & 0x03];
	D->control2 = v;
	D->DRAMportshift = ((v & 0x03) == 0) ? 3 : 0;
	dt_update_addresses(D);
}

static void dt_write_data(YM_DELTAT *D, UINT8 v)
{
	D->cpu_data = v;
	/* Recording into external memory: store the byte and advance. */
	if ((D->portstate & 0xE0) == 0x60) {
		UINT32 byteAddr = D->now_addr >> 1;
		if (D->memory != NULL && byteAddr < D->memory_size)
			D->memory[byteAddr] = v;
		if (byteAddr >= D->end) {
			dt_status_set(D, D->status_change_EOS_bit);
		} else {
			D->now_addr += 2;
			dt_status_set(D, D->status_change_BRDY_bit);
		}
	}
}

void YM_DELTAT_ADPCM_Write(YM_DELTAT *D, int r, int v)
{
	if (r < 0 || r >= 0x10)
		return;
	v &= 0xFF;
	D->reg[r] = (UINT8)v;

	switch (r) {
	case 0x00:
		dt_write_control1(D, (UINT8)v);
		break;
	case 0x01:
		dt_write_control2(D, (UINT8)v);
		break;
	case 0x0C: case 0x0D:
		D->limit_set = 1;
		dt_update_addresses(D);
		break;
	case 0x02: case 0x03: case 0x04: case 0x05:
		dt_update_addresses(D);
		break;
	case 0x08:
		dt_write_data(D, (UINT8)v);
		break;
	case 0x09: case 0x0A:
		D->delta = (UINT16)(D->reg[0x0A] << 8 | D->reg[0x09]);
		D->step = (UINT32)((double)D->delta * D->freqbase);
		break;
	case 0x0B:
		D->volume = (INT32)(((INT64)v * (D->output_range / 256)) / 32768);
		break;
	default:
		break;
	}
}

UINT8 YM_DELTAT_ADPCM_Read(YM_DELTAT *D)
{
	UINT8 v = 0;

	if ((D->portstate & 0xE0) != 0x20)
		return 0;
	/* The first two reads after start return stale latch contents. */
	if (D->memread) {
		D->memread--;
		return 0;
	}
	v = dt_mem_read(D, D->now_addr >> 1);
	if ((D->now_addr >> 1) < D->end) {
		D->now_addr += 2;
		dt_status_set(D, D->status_change_BRDY_bit);
	} else {
		dt_status_set(D, D->status_change_EOS_bit);
	}
	return v;
}

/* Decodes one nibble. Returns 0 when playback stopped. */
static int dt_next_nibble(YM_DELTAT *D)
{
	UINT8 nib;
	INT32 diff;

	if (D->limit != 0xFFFFFFFFu && D->now_addr == ((D->limit + 1u) << 1))
		D->now_addr = 0;
	if (D->now_addr >= ((D->end + 1u) << 1)) {
		if (D->portstate & 0x10) {
			dt_restart(D);
		} else {
			dt_status_set(D, D->status_change_EOS_bit);
			D->portstate &= 0x20; /* keep the memory-mode bit on the YM2610 */
			D->PCM_BSY = 0;
			D->adpcml = 0;
			D->prev_acc = 0;
			D->acc = 0;
			return 0;
		}
	}

	if (D->now_addr & 1u) {
		nib = D->now_data & 0x0F;
	} else {
		D->now_data = dt_mem_read(D, D->now_addr >> 1);
		nib = D->now_data >> 4;
	}
	D->now_addr = (D->now_addr + 1u) & 0x1FFFFFFu;

	diff = ((INT32)((nib & 0x07) * 2 + 1) * D->adpcmd) / 8;
	D->prev_acc = D->acc;
	D->acc += (nib & 0x08) ? -diff : diff;
	if (D->acc > 32767)
		D->acc = 32767;
	else if (D->acc < -32768)
		D->acc = -32768;

	D->adpcmd = (D->adpcmd * dt_scale[nib & 0x07]) / 64;
	if (D->adpcmd < DT_STEP_MIN)
		D->adpcmd = DT_STEP_MIN;
	else if (D->adpcmd > DT_STEP_MAX)
		D->adpcmd = DT_STEP_MAX;
	return 1;
}

void YM_DELTAT_ADPCM_CALC(YM_DELTAT *D)
{
	INT64 mixed;

	/* Only external-memory playback is emulated; CPU-fed ADPCM is silent. */
	if ((D->portstate & 0xE0) != 0xA0)
		return;

	D->now_step += D->step;
	if (D->now_step >= DT_PHASE_ONE) {
		UINT32 count = D->now_step >> DT_PHASE_BITS;
		D->now_step &= DT_PHASE_ONE - 1u;
		while (count--) {
			if (!dt_next_nibble(D))
				return;
		}
	}

	/* Linear interpolation between the last two decoded values. */
	mixed = (INT64)D->prev_acc * (INT64)(DT_PHASE_ONE - D->now_step) +
		(INT64)D->acc * (INT64)D->now_step;
	D->adpcml = (INT32)((mixed >> DT_PHASE_BITS) * D->volume);
	*D->pan += D->adpcml;
}
