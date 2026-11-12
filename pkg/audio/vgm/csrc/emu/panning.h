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

// Constant-power stereo panning for the vendored chip cores.

#ifndef ZVGM_PANNING_H
#define ZVGM_PANNING_H

#include <math.h>

#include "../stdtype.h"

#define PANNING_BITS 10
#define PANNING_NORMAL (1 << PANNING_BITS)

#define APPLY_PANNING_S(x, panval) (((x) * (panval)) >> PANNING_BITS)
#define APPLY_PANNING_L(x, panval) (INT32)(((INT64)(x) * (panval)) >> PANNING_BITS)

// position runs from -256 (left) through 0 (centre) to 256 (right).
static inline void Panning_Calculate(INT32 channels[2], INT16 position)
{
	double angle;

	if (position < -256)
		position = -256;
	else if (position > 256)
		position = 256;
	angle = (double)(position + 256) / 512.0 * (3.14159265358979323846 / 2.0);
	channels[0] = (INT32)(cos(angle) * 1.41421356237309504880 * PANNING_NORMAL);
	channels[1] = (INT32)(sin(angle) * 1.41421356237309504880 * PANNING_NORMAL);
}

static inline void Panning_Centre(INT32 channels[2])
{
	channels[0] = PANNING_NORMAL;
	channels[1] = PANNING_NORMAL;
}

#endif
