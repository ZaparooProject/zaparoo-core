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

// Fixed-point counter that steps one clock domain against another.

#ifndef ZVGM_RATIOCNTR_H
#define ZVGM_RATIOCNTR_H

#include "../stdtype.h"

#define ZVGM_RC_FRAC 32

typedef struct zvgm_ratio_cntr {
	UINT64 inc;
	UINT64 val;
} RATIO_CNTR;

static inline void RC_SET_RATIO(RATIO_CNTR *rc, UINT32 mul, UINT32 div)
{
	rc->inc = div ? (UINT64)((((UINT64)mul << ZVGM_RC_FRAC) + div / 2) / div) : 0;
}

static inline void RC_STEP(RATIO_CNTR *rc)
{
	rc->val += rc->inc;
}

static inline UINT32 RC_GET_VAL(const RATIO_CNTR *rc)
{
	return (UINT32)(rc->val >> ZVGM_RC_FRAC);
}

static inline void RC_RESET(RATIO_CNTR *rc)
{
	rc->val = 0;
}

static inline void RC_MASK(RATIO_CNTR *rc)
{
	rc->val &= (((UINT64)1 << ZVGM_RC_FRAC) - 1);
}

#endif
