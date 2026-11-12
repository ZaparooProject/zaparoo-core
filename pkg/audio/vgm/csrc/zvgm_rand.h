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


// Deterministic noise source for cores that call rand(). The state is a
// relaxed atomic so concurrent decoders never race on it; noise does not
// need to be reproducible across them.

#ifndef ZVGM_RAND_H
#define ZVGM_RAND_H

#include <stdint.h>

static uint32_t zvgm_rand_state = 0x12345678u;

static inline int zvgm_rand(void)
{
	uint32_t x = __atomic_load_n(&zvgm_rand_state, __ATOMIC_RELAXED);
	x ^= x << 13;
	x ^= x >> 17;
	x ^= x << 5;
	__atomic_store_n(&zvgm_rand_state, x, __ATOMIC_RELAXED);
	return (int)(x & 0x7FFFFFFF);
}

#endif
