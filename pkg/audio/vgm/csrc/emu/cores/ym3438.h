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

// Declarations for the vendored Nuked OPN2 core.

#ifndef ZVGM_YM3438_H
#define ZVGM_YM3438_H

#include "../../stdtype.h"
#include "../snddef.h"

void nukedopn2_write(void *chip, UINT8 port, UINT8 data);
UINT8 nukedopn2_read(void *chip, UINT8 port);
void nukedopn2_update(void *chip, UINT32 numsamples, DEV_SMPL **sndptr);
void nukedopn2_set_options(void *chip, UINT32 flags);
void nukedopn2_set_mute_mask(void *chip, UINT32 mute);
void *nukedopn2_init(UINT32 clock, UINT32 rate);
void nukedopn2_shutdown(void *chip);
void nukedopn2_reset_chip(void *chip);

#endif
