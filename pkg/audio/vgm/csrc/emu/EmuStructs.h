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

// Device structures shared by the vendored chip cores and our glue. The
// cores fill these tables positionally, so member order is part of the
// contract.

#ifndef ZVGM_EMUSTRUCTS_H
#define ZVGM_EMUSTRUCTS_H

#include <stddef.h>

#include "../stdtype.h"
#include "snddef.h"
#include "logging.h"

typedef struct zvgm_dev_def DEV_DEF;
typedef struct zvgm_dev_info DEV_INFO;
typedef struct zvgm_dev_gen_cfg DEV_GEN_CFG;

typedef void (*DEVCB_SRATE_CHG)(void *userParam, UINT32 newSRate);

typedef UINT8 (*DEVFUNC_START)(const DEV_GEN_CFG *cfg, DEV_INFO *retDevInf);
typedef void (*DEVFUNC_CTRL)(void *info);
typedef void (*DEVFUNC_UPDATE)(void *info, UINT32 samples, DEV_SMPL **outputs);
typedef void (*DEVFUNC_OPTMASK)(void *info, UINT32 optionBits);
typedef void (*DEVFUNC_PANALL)(void *info, const INT16 *panPos);
typedef void (*DEVFUNC_SRCCB)(void *info, DEVCB_SRATE_CHG func, void *param);
typedef void (*DEVFUNC_SETLOGCB)(void *info, DEVCB_LOG func, void *param);
typedef UINT8 (*DEVFUNC_LINKDEV)(void *info, UINT8 linkID, const DEV_INFO *devInfLink);

typedef UINT8 (*DEVFUNC_READ_A8D8)(void *info, UINT8 addr);
typedef UINT16 (*DEVFUNC_READ_A8D16)(void *info, UINT8 addr);
typedef UINT8 (*DEVFUNC_READ_A16D8)(void *info, UINT16 addr);
typedef UINT16 (*DEVFUNC_READ_A16D16)(void *info, UINT16 addr);
typedef UINT32 (*DEVFUNC_READ_VALUE)(void *info);
typedef void (*DEVFUNC_WRITE_A8D8)(void *info, UINT8 addr, UINT8 data);
typedef void (*DEVFUNC_WRITE_A8D16)(void *info, UINT8 addr, UINT16 data);
typedef void (*DEVFUNC_WRITE_A16D8)(void *info, UINT16 addr, UINT8 data);
typedef void (*DEVFUNC_WRITE_A16D16)(void *info, UINT16 addr, UINT16 data);
typedef void (*DEVFUNC_WRITE_MEMSIZE)(void *info, UINT32 memsize);
typedef void (*DEVFUNC_WRITE_BLOCK)(void *info, UINT32 offset, UINT32 length, const UINT8 *data);
typedef void (*DEVFUNC_WRITE_VALUE)(void *info, UINT32 value);
typedef void (*DEVFUNC_WRITE_CLOCK)(void *info, UINT32 clock);

// Function kinds in DEVDEF_RWFUNC.funcType.
#define RWF_WRITE 0x00
#define RWF_READ 0x01
#define RWF_QUICKWRITE (0x02 | RWF_WRITE)
#define RWF_QUICKREAD (0x02 | RWF_READ)
#define RWF_REGISTER 0x00
#define RWF_MEMORY 0x10
#define RWF_CLOCK 0x80
#define RWF_SRATE 0x90
#define RWF_VOLUME 0xA0
#define RWF_VOL_LR 0xB0
#define RWF_CHN_MUTE 0xC0
#define RWF_CHN_PAN 0xD0

// Argument shapes in DEVDEF_RWFUNC.rwType.
#define DEVRW_VALUE 0x00
#define DEVRW_ALL 0x00
#define DEVRW_A8D8 0x11
#define DEVRW_A8D16 0x12
#define DEVRW_A16D8 0x21
#define DEVRW_A16D16 0x22
#define DEVRW_BLOCK 0x80
#define DEVRW_MEMSIZE 0x81

// Sample rate selection in DEV_GEN_CFG.srMode.
#define DEVRI_SRMODE_NATIVE 0x00
#define DEVRI_SRMODE_CUSTOM 0x01
#define DEVRI_SRMODE_HIGHEST 0x02

typedef struct zvgm_devdef_rwfunc {
	UINT8 funcType;
	UINT8 rwType;
	UINT16 user;
	void *funcPtr;
} DEVDEF_RWFUNC;

struct zvgm_dev_def {
	const char *name;
	const char *author;
	UINT32 coreID;

	DEVFUNC_START Start;
	DEVFUNC_CTRL Stop;
	DEVFUNC_CTRL Reset;
	DEVFUNC_UPDATE Update;

	DEVFUNC_OPTMASK SetOptionBits;
	DEVFUNC_OPTMASK SetMuteMask;
	DEVFUNC_PANALL SetPanning;
	DEVFUNC_SRCCB SetSRateChgCB;
	DEVFUNC_SETLOGCB SetLogCB;
	DEVFUNC_LINKDEV LinkDevice;

	const DEVDEF_RWFUNC *rwFuncs;
};

typedef struct zvgm_devlink_info {
	UINT8 devID;
	UINT8 linkID;
	DEV_GEN_CFG *cfg;
} DEVLINK_INFO;

struct zvgm_dev_info {
	DEV_DATA *dataPtr;
	UINT32 sampleRate;
	const DEV_DEF *devDef;
	UINT32 linkDevCount;
	DEVLINK_INFO *linkDevs;
};

struct zvgm_dev_gen_cfg {
	UINT32 emuCore;
	UINT8 srMode;
	UINT8 flags;
	UINT32 clock;
	UINT32 smplRate;
};

typedef struct zvgm_devlink_ids {
	UINT32 count;
	const UINT8 *ids;
} DEVLINK_IDS;

typedef struct zvgm_dev_decl {
	UINT8 deviceID;
	const char *(*name)(const DEV_GEN_CFG *devCfg);
	UINT16 (*channelCount)(const DEV_GEN_CFG *devCfg);
	const char **(*channelNames)(const DEV_GEN_CFG *devCfg);
	const DEVLINK_IDS *(*linkIDs)(const DEV_GEN_CFG *devCfg);
	const DEV_DEF *cores[8];
} DEV_DECL;

#endif
