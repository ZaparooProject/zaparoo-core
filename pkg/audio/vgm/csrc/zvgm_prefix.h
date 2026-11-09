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


// Renames every global symbol of the vendored chip cores and our ADPCM-B
// unit so they cannot clash with other C code linked into the binary.
// Regenerate from `nm -g --defined-only` over the compiled cores.

#ifndef ZVGM_PREFIX_H
#define ZVGM_PREFIX_H

// The vendored MAME cores left-shift negative samples throughout. That is
// defined in C23 and every supported compiler treats it as two's
// complement, so UBSan builds skip only that check for this package.
// Every file that includes this header ends with csrc/zvgm_suffix.h, which
// pops the attribute.
#if defined(__clang__)
#pragma clang attribute push(__attribute__((no_sanitize("shift-base"))), apply_to = function)
#endif

#define ay8910_get_sample_rate zvgm_ay8910_get_sample_rate
#define ay8910_read zvgm_ay8910_read
#define ay8910_reset zvgm_ay8910_reset
#define ay8910_set_clock zvgm_ay8910_set_clock
#define ay8910_set_log_cb zvgm_ay8910_set_log_cb
#define ay8910_set_mute_mask zvgm_ay8910_set_mute_mask
#define ay8910_set_srchg_cb zvgm_ay8910_set_srchg_cb
#define ay8910_set_stereo_mask zvgm_ay8910_set_stereo_mask
#define ay8910_start zvgm_ay8910_start
#define ay8910_stop zvgm_ay8910_stop
#define ay8910_update_one zvgm_ay8910_update_one
#define ay8910_write zvgm_ay8910_write
#define ay8910_write_reg zvgm_ay8910_write_reg
#define devDef_AY8910_MAME zvgm_devDef_AY8910_MAME
#define devDef_C6280_MAME zvgm_devDef_C6280_MAME
#define devDef_GB_MAME zvgm_devDef_GB_MAME
#define devDef_QSound_MAME zvgm_devDef_QSound_MAME
#define devDef_RF5C68_MAME zvgm_devDef_RF5C68_MAME
#define devDef_SAA1099_MAME zvgm_devDef_SAA1099_MAME
#define devDef_SN76496_MAME zvgm_devDef_SN76496_MAME
#define devDef_YM2151_MAME zvgm_devDef_YM2151_MAME
#define device_reset_nesapu zvgm_device_reset_nesapu
#define device_start_ay8910_mame zvgm_device_start_ay8910_mame
#define device_start_nesapu zvgm_device_start_nesapu
#define device_stop_nesapu zvgm_device_stop_nesapu
#define nesapu_get_mute_mask zvgm_nesapu_get_mute_mask
#define nes_apu_read zvgm_nes_apu_read
#define nesapu_set_mute_mask zvgm_nesapu_set_mute_mask
#define nesapu_set_options zvgm_nesapu_set_options
#define nesapu_set_panning zvgm_nesapu_set_panning
#define nesapu_set_rom zvgm_nesapu_set_rom
#define nes_apu_update zvgm_nes_apu_update
#define nes_apu_write zvgm_nes_apu_write
#define NOPN2_Clock zvgm_NOPN2_Clock
#define NOPN2_GenerateResampled zvgm_NOPN2_GenerateResampled
#define NOPN2_Read zvgm_NOPN2_Read
#define NOPN2_ReadIRQPin zvgm_NOPN2_ReadIRQPin
#define NOPN2_ReadTestPin zvgm_NOPN2_ReadTestPin
#define NOPN2_Reset zvgm_NOPN2_Reset
#define NOPN2_SetChipType zvgm_NOPN2_SetChipType
#define NOPN2_SetTestPin zvgm_NOPN2_SetTestPin
#define NOPN2_Write zvgm_NOPN2_Write
#define NOPN2_WriteBuffered zvgm_NOPN2_WriteBuffered
#define nukedopn2_init zvgm_nukedopn2_init
#define nukedopn2_read zvgm_nukedopn2_read
#define nukedopn2_reset_chip zvgm_nukedopn2_reset_chip
#define nukedopn2_set_mute_mask zvgm_nukedopn2_set_mute_mask
#define nukedopn2_set_options zvgm_nukedopn2_set_options
#define nukedopn2_shutdown zvgm_nukedopn2_shutdown
#define nukedopn2_update zvgm_nukedopn2_update
#define nukedopn2_write zvgm_nukedopn2_write
#define oki_adpcm_clock zvgm_oki_adpcm_clock
#define oki_adpcm_create zvgm_oki_adpcm_create
#define oki_adpcm_destroy zvgm_oki_adpcm_destroy
#define oki_adpcm_init zvgm_oki_adpcm_init
#define oki_adpcm_reset zvgm_oki_adpcm_reset
#define OPLL_calc zvgm_OPLL_calc
#define OPLL_calcStereo zvgm_OPLL_calcStereo
#define OPLL_copyPatch zvgm_OPLL_copyPatch
#define OPLL_delete zvgm_OPLL_delete
#define OPLL_dumpToPatch zvgm_OPLL_dumpToPatch
#define OPLL_forceRefresh zvgm_OPLL_forceRefresh
#define OPLL_getDefaultPatch zvgm_OPLL_getDefaultPatch
#define OPLL_load_state zvgm_OPLL_load_state
#define OPLL_new zvgm_OPLL_new
#define OPLL_patchToDump zvgm_OPLL_patchToDump
#define OPLL_RateConv_delete zvgm_OPLL_RateConv_delete
#define OPLL_RateConv_getData zvgm_OPLL_RateConv_getData
#define OPLL_RateConv_new zvgm_OPLL_RateConv_new
#define OPLL_RateConv_putData zvgm_OPLL_RateConv_putData
#define OPLL_RateConv_reset zvgm_OPLL_RateConv_reset
#define OPLL_reset zvgm_OPLL_reset
#define OPLL_resetPatch zvgm_OPLL_resetPatch
#define OPLL_save_state zvgm_OPLL_save_state
#define OPLL_setChipType zvgm_OPLL_setChipType
#define OPLL_setMask zvgm_OPLL_setMask
#define OPLL_setPan zvgm_OPLL_setPan
#define OPLL_setPanFine zvgm_OPLL_setPanFine
#define OPLL_setPatch zvgm_OPLL_setPatch
#define OPLL_setQuality zvgm_OPLL_setQuality
#define OPLL_setRate zvgm_OPLL_setRate
#define OPLL_toggleMask zvgm_OPLL_toggleMask
#define OPLL_writeIO zvgm_OPLL_writeIO
#define OPLL_writeReg zvgm_OPLL_writeReg
#define opl_set_log_cb zvgm_opl_set_log_cb
#define opl_set_mute_mask zvgm_opl_set_mute_mask
#define rf5c68_set_sample_end_callback zvgm_rf5c68_set_sample_end_callback
#define SCSPDSP_Init zvgm_SCSPDSP_Init
#define SCSPDSP_SetSample zvgm_SCSPDSP_SetSample
#define SCSPDSP_Start zvgm_SCSPDSP_Start
#define SCSPDSP_Step zvgm_SCSPDSP_Step
#define sndDev_32X_PWM zvgm_sndDev_32X_PWM
#define sndDev_C140 zvgm_sndDev_C140
#define sndDev_C219 zvgm_sndDev_C219
#define sndDev_C352 zvgm_sndDev_C352
#define sndDev_ES5503 zvgm_sndDev_ES5503
#define sndDev_GA20 zvgm_sndDev_GA20
#define sndDev_K051649 zvgm_sndDev_K051649
#define sndDev_K053260 zvgm_sndDev_K053260
#define sndDev_K054539 zvgm_sndDev_K054539
#define sndDev_Mikey zvgm_sndDev_Mikey
#define sndDev_MSM6258 zvgm_sndDev_MSM6258
#define sndDev_MSM6295 zvgm_sndDev_MSM6295
#define sndDev_Pokey zvgm_sndDev_Pokey
#define sndDev_SCSP zvgm_sndDev_SCSP
#define sndDev_SegaPCM zvgm_sndDev_SegaPCM
#define sndDev_uPD7759 zvgm_sndDev_uPD7759
#define sndDev_VBoyVSU zvgm_sndDev_VBoyVSU
#define sndDev_X1_010 zvgm_sndDev_X1_010
#define sndDev_YMF271 zvgm_sndDev_YMF271
#define sndDev_YMW258 zvgm_sndDev_YMW258
#define sndDev_YMZ280B zvgm_sndDev_YMZ280B
#define y8950_alloc_pcmrom zvgm_y8950_alloc_pcmrom
#define y8950_init zvgm_y8950_init
#define y8950_read zvgm_y8950_read
#define y8950_reset_chip zvgm_y8950_reset_chip
#define y8950_set_irq_handler zvgm_y8950_set_irq_handler
#define y8950_set_keyboard_handler zvgm_y8950_set_keyboard_handler
#define y8950_set_port_handler zvgm_y8950_set_port_handler
#define y8950_set_timer_handler zvgm_y8950_set_timer_handler
#define y8950_set_update_handler zvgm_y8950_set_update_handler
#define y8950_shutdown zvgm_y8950_shutdown
#define y8950_timer_over zvgm_y8950_timer_over
#define y8950_update_one zvgm_y8950_update_one
#define y8950_write zvgm_y8950_write
#define y8950_write_pcmrom zvgm_y8950_write_pcmrom
#define ym2151_set_irq_handler zvgm_ym2151_set_irq_handler
#define ym2203_init zvgm_ym2203_init
#define ym2203_link_ssg zvgm_ym2203_link_ssg
#define ym2203_read zvgm_ym2203_read
#define ym2203_reset_chip zvgm_ym2203_reset_chip
#define ym2203_set_log_cb zvgm_ym2203_set_log_cb
#define ym2203_set_mute_mask zvgm_ym2203_set_mute_mask
#define ym2203_set_srchg_cb zvgm_ym2203_set_srchg_cb
#define ym2203_shutdown zvgm_ym2203_shutdown
#define ym2203_timer_over zvgm_ym2203_timer_over
#define ym2203_update_one zvgm_ym2203_update_one
#define ym2203_write zvgm_ym2203_write
#define ym2608_alloc_pcmromb zvgm_ym2608_alloc_pcmromb
#define ym2608_init zvgm_ym2608_init
#define ym2608_link_ssg zvgm_ym2608_link_ssg
#define ym2608_read zvgm_ym2608_read
#define ym2608_reset_chip zvgm_ym2608_reset_chip
#define ym2608_set_log_cb zvgm_ym2608_set_log_cb
#define ym2608_set_mute_mask zvgm_ym2608_set_mute_mask
#define ym2608_set_srchg_cb zvgm_ym2608_set_srchg_cb
#define ym2608_shutdown zvgm_ym2608_shutdown
#define ym2608_timer_over zvgm_ym2608_timer_over
#define ym2608_update_one zvgm_ym2608_update_one
#define ym2608_write zvgm_ym2608_write
#define ym2608_write_pcmromb zvgm_ym2608_write_pcmromb
#define ym2610_alloc_pcmroma zvgm_ym2610_alloc_pcmroma
#define ym2610_alloc_pcmromb zvgm_ym2610_alloc_pcmromb
#define ym2610b_update_one zvgm_ym2610b_update_one
#define ym2610_init zvgm_ym2610_init
#define ym2610_link_ssg zvgm_ym2610_link_ssg
#define ym2610_read zvgm_ym2610_read
#define ym2610_reset_chip zvgm_ym2610_reset_chip
#define ym2610_set_log_cb zvgm_ym2610_set_log_cb
#define ym2610_set_mute_mask zvgm_ym2610_set_mute_mask
#define ym2610_shutdown zvgm_ym2610_shutdown
#define ym2610_timer_over zvgm_ym2610_timer_over
#define ym2610_update_one zvgm_ym2610_update_one
#define ym2610_write zvgm_ym2610_write
#define ym2610_write_pcmroma zvgm_ym2610_write_pcmroma
#define ym2610_write_pcmromb zvgm_ym2610_write_pcmromb
#define ym2612_init zvgm_ym2612_init
#define ym2612_read zvgm_ym2612_read
#define ym2612_reset_chip zvgm_ym2612_reset_chip
#define ym2612_set_log_cb zvgm_ym2612_set_log_cb
#define ym2612_set_mute_mask zvgm_ym2612_set_mute_mask
#define ym2612_set_options zvgm_ym2612_set_options
#define ym2612_shutdown zvgm_ym2612_shutdown
#define ym2612_timer_over zvgm_ym2612_timer_over
#define ym2612_update_one zvgm_ym2612_update_one
#define ym2612_write zvgm_ym2612_write
#define ym3526_clock_changed zvgm_ym3526_clock_changed
#define ym3526_init zvgm_ym3526_init
#define ym3526_read zvgm_ym3526_read
#define ym3526_reset_chip zvgm_ym3526_reset_chip
#define ym3526_set_irq_handler zvgm_ym3526_set_irq_handler
#define ym3526_set_timer_handler zvgm_ym3526_set_timer_handler
#define ym3526_set_update_handler zvgm_ym3526_set_update_handler
#define ym3526_shutdown zvgm_ym3526_shutdown
#define ym3526_timer_over zvgm_ym3526_timer_over
#define ym3526_update_one zvgm_ym3526_update_one
#define ym3526_write zvgm_ym3526_write
#define ym3812_clock_changed zvgm_ym3812_clock_changed
#define ym3812_init zvgm_ym3812_init
#define ym3812_read zvgm_ym3812_read
#define ym3812_reset_chip zvgm_ym3812_reset_chip
#define ym3812_set_irq_handler zvgm_ym3812_set_irq_handler
#define ym3812_set_timer_handler zvgm_ym3812_set_timer_handler
#define ym3812_set_update_handler zvgm_ym3812_set_update_handler
#define ym3812_shutdown zvgm_ym3812_shutdown
#define ym3812_timer_over zvgm_ym3812_timer_over
#define ym3812_update_one zvgm_ym3812_update_one
#define ym3812_write zvgm_ym3812_write
#define YM_DELTAT_ADPCM_CALC zvgm_YM_DELTAT_ADPCM_CALC
#define YM_DELTAT_ADPCM_Init zvgm_YM_DELTAT_ADPCM_Init
#define YM_DELTAT_ADPCM_Read zvgm_YM_DELTAT_ADPCM_Read
#define YM_DELTAT_ADPCM_Reset zvgm_YM_DELTAT_ADPCM_Reset
#define YM_DELTAT_ADPCM_Write zvgm_YM_DELTAT_ADPCM_Write
#define YM_DELTAT_calc_mem_mask zvgm_YM_DELTAT_calc_mem_mask
#define ymf262_init zvgm_ymf262_init
#define ymf262_read zvgm_ymf262_read
#define ymf262_reset_chip zvgm_ymf262_reset_chip
#define ymf262_set_irq_handler zvgm_ymf262_set_irq_handler
#define ymf262_set_log_cb zvgm_ymf262_set_log_cb
#define ymf262_set_mute_mask zvgm_ymf262_set_mute_mask
#define ymf262_set_timer_handler zvgm_ymf262_set_timer_handler
#define ymf262_set_update_handler zvgm_ymf262_set_update_handler
#define ymf262_set_vol_lr zvgm_ymf262_set_vol_lr
#define ymf262_set_volume zvgm_ymf262_set_volume
#define ymf262_shutdown zvgm_ymf262_shutdown
#define ymf262_timer_over zvgm_ymf262_timer_over
#define ymf262_update_one zvgm_ymf262_update_one
#define ymf262_write zvgm_ymf262_write

#endif
