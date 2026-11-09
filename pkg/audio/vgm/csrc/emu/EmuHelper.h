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

// Helper macros the vendored chip cores use while starting up.

#ifndef ZVGM_EMUHELPER_H
#define ZVGM_EMUHELPER_H

#include "EmuStructs.h"

#define INIT_DEVINF(zInf, zData, zRate, zDef) \
	do { \
		(zInf)->dataPtr = (zData); \
		(zInf)->sampleRate = (zRate); \
		(zInf)->devDef = (zDef); \
		(zInf)->linkDevCount = 0; \
		(zInf)->linkDevs = NULL; \
	} while (0)

#define SRATE_CUSTOM_HIGHEST(srmode, rate, customrate) \
	do { \
		if ((srmode) == DEVRI_SRMODE_CUSTOM || \
		    ((srmode) == DEVRI_SRMODE_HIGHEST && (rate) < (customrate))) \
			(rate) = (customrate); \
	} while (0)

/* Smallest all-ones mask covering v: pow2_mask(0x3000) == 0x3FFF. */
static inline UINT32 pow2_mask(UINT32 v)
{
	if (v == 0)
		return 0;
	v--;
	v |= v >> 1;
	v |= v >> 2;
	v |= v >> 4;
	v |= v >> 8;
	v |= v >> 16;
	return v;
}

#endif
