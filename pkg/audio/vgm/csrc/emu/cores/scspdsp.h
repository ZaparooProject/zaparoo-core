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

// Declarations for the vendored SCSP DSP core.

#ifndef ZVGM_SCSPDSP_H
#define ZVGM_SCSPDSP_H

#include "../../stdtype.h"

typedef struct _SCSPDSP {
	UINT16 *SCSPRAM;
	UINT32 SCSPRAM_LENGTH;
	UINT32 RBP;
	UINT32 RBL;
	INT16 COEF[64];
	UINT16 MADRS[32];
	UINT16 MPRO[128 * 4];
	INT32 TEMP[128];
	INT32 MEMS[32];
	UINT32 DEC;
	INT32 MIXS[16];
	INT16 EXTS[2];
	INT16 EFREG[16];
	int Stopped;
	int LastStep;
} SCSPDSP;

void SCSPDSP_Init(SCSPDSP *DSP);
void SCSPDSP_SetSample(SCSPDSP *DSP, INT32 sample, INT32 SEL, INT32 MXL);
void SCSPDSP_Step(SCSPDSP *DSP);
void SCSPDSP_Start(SCSPDSP *DSP);

#endif
