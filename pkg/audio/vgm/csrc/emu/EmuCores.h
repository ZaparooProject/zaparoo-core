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

// Four-character core identifiers the vendored chip cores tag themselves with.

#ifndef ZVGM_EMUCORES_H
#define ZVGM_EMUCORES_H

#define ZVGM_FCC(a, b, c, d) \
	(((UINT32)(a) << 24) | ((UINT32)(b) << 16) | ((UINT32)(c) << 8) | (UINT32)(d))

#define FCC_MAME ZVGM_FCC('M', 'A', 'M', 'E')
#define FCC_GENS ZVGM_FCC('G', 'E', 'N', 'S')
#define FCC_MEDN ZVGM_FCC('M', 'E', 'D', 'N')
#define FCC_LAOO ZVGM_FCC('L', 'A', 'O', 'O')
#define FCC_CTR_ ZVGM_FCC('C', 'T', 'R', '\0')
#define FCC_NUKE ZVGM_FCC('N', 'U', 'K', 'E')
#define FCC_EMU_ ZVGM_FCC('E', 'M', 'U', '\0')

#endif
