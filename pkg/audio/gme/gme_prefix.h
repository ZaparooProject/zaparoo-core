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


// Renames the global symbols of the emu2413 copy built for Game_Music_Emu
// so they cannot clash with another emu2413 linked into the same binary.
// Regenerate from `nm -g --defined-only` over the compiled emu2413.c.

#ifndef ZGME_PREFIX_H
#define ZGME_PREFIX_H

#define OPLL_calc zgme_OPLL_calc
#define OPLL_calcStereo zgme_OPLL_calcStereo
#define OPLL_copyPatch zgme_OPLL_copyPatch
#define OPLL_delete zgme_OPLL_delete
#define OPLL_dumpToPatch zgme_OPLL_dumpToPatch
#define OPLL_forceRefresh zgme_OPLL_forceRefresh
#define OPLL_getDefaultPatch zgme_OPLL_getDefaultPatch
#define OPLL_new zgme_OPLL_new
#define OPLL_patchToDump zgme_OPLL_patchToDump
#define OPLL_RateConv_delete zgme_OPLL_RateConv_delete
#define OPLL_RateConv_getData zgme_OPLL_RateConv_getData
#define OPLL_RateConv_new zgme_OPLL_RateConv_new
#define OPLL_RateConv_putData zgme_OPLL_RateConv_putData
#define OPLL_RateConv_reset zgme_OPLL_RateConv_reset
#define OPLL_reset zgme_OPLL_reset
#define OPLL_resetPatch zgme_OPLL_resetPatch
#define OPLL_setChipType zgme_OPLL_setChipType
#define OPLL_setMask zgme_OPLL_setMask
#define OPLL_setPan zgme_OPLL_setPan
#define OPLL_setPanFine zgme_OPLL_setPanFine
#define OPLL_setPatch zgme_OPLL_setPatch
#define OPLL_setQuality zgme_OPLL_setQuality
#define OPLL_setRate zgme_OPLL_setRate
#define OPLL_toggleMask zgme_OPLL_toggleMask
#define OPLL_writeIO zgme_OPLL_writeIO
#define OPLL_writeReg zgme_OPLL_writeReg

#endif
