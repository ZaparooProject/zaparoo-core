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


// Our VGM chip engine and per-chip glue. See engine.h.

#include "csrc/zvgm_prefix.h"

#include <stdlib.h>
#include <string.h>

#include "engine.h"

#include "csrc/stdtype.h"
#include "csrc/emu/EmuStructs.h"
#include "csrc/emu/EmuHelper.h"
#include "csrc/emu/SoundDevs.h"
#include "csrc/emu/cores/ayintf.h"
#include "csrc/emu/cores/ay8910.h"
#include "csrc/emu/cores/c140.h"
#include "csrc/emu/cores/c219.h"
#include "csrc/emu/cores/c352.h"
#include "csrc/emu/cores/c6280_mame.h"
#include "csrc/emu/cores/es5503.h"
#include "csrc/emu/cores/fmopl.h"
#include "csrc/emu/cores/fmopn.h"
#include "csrc/emu/cores/gbintf.h"
#include "csrc/emu/cores/gb_mame.h"
#include "csrc/emu/cores/iremga20.h"
#include "csrc/emu/cores/k051649.h"
#include "csrc/emu/cores/k053260.h"
#include "csrc/emu/cores/k054539.h"
#include "csrc/emu/cores/mikey.h"
#include "csrc/emu/cores/multipcm.h"
#include "csrc/emu/cores/nes_apu.h"
#include "csrc/emu/cores/okim6258.h"
#include "csrc/emu/cores/okim6295.h"
#include "csrc/emu/cores/pokey.h"
#include "csrc/emu/cores/pwm.h"
#include "csrc/emu/cores/qsound_mame.h"
#include "csrc/emu/cores/rf5c68.h"
#include "csrc/emu/cores/saa1099_mame.h"
#include "csrc/emu/cores/scsp.h"
#include "csrc/emu/cores/segapcm.h"
#include "csrc/emu/cores/sn764intf.h"
#include "csrc/emu/cores/sn76496.h"
#include "csrc/emu/cores/upd7759.h"
#include "csrc/emu/cores/vsu.h"
#include "csrc/emu/cores/x1_010.h"
#include "csrc/emu/cores/ym2151.h"
#include "csrc/emu/cores/ym3438.h"
#include "csrc/emu/cores/ymf262.h"
#include "csrc/emu/cores/ymf271.h"
#include "csrc/emu/cores/ymz280b.h"
#include "third_party/emu2413/emu2413.h"

#define ONE ((uint64_t)1 << 32)
#define NBUF 1024
#define NES_RAM_SIZE 0x10000

typedef void (*w8_fn)(void *, UINT8, UINT8);
typedef void (*wm8_fn)(void *, UINT16, UINT8);
typedef void (*wd16_fn)(void *, UINT8, UINT16);
typedef void (*wm16_fn)(void *, UINT16, UINT16);
typedef void (*memsize_fn)(void *, UINT32);
typedef void (*block_fn)(void *, UINT32, UINT32, const UINT8 *);

typedef struct zchip {
	zvgm_engine *eng;
	UINT8 type;
	UINT8 kind; // glue family, see K_* below
	UINT8 swapRom;
	UINT8 deadWrites;
	void *chip;
	DEVFUNC_UPDATE update;
	DEVFUNC_CTRL stop;
	w8_fn w8;
	wm8_fn wm8;
	wd16_fn wd16;
	wm16_fn wm16;
	memsize_fn romSize;
	block_fn romWrite;
	block_fn ramWrite;
	memsize_fn dtSize;
	block_fn dtWrite;
	UINT8 *nesRam;
	int grp;        // index of the chip whose writes drive this one
	int rendered;   // frames rendered in the current zvgm_render call

	UINT32 rate;
	UINT32 pendingRate;
	UINT8 rateChanged;
	INT32 volL, volR;

	// Resampler.
	uint64_t step;
	uint64_t phase; // box: unused fraction of cur; linear: position between prev and cur
	INT32 cur[2], prev[2];
	UINT32 nbPos, nbLen;
	DEV_SMPL nb[2][NBUF];
} zchip;

enum {
	K_GENERIC = 0,
	K_OPLL,
	K_OPN,
	K_NUKED,
	K_OPL,
	K_OPL3,
	K_NES,
};

struct zvgm_engine {
	UINT32 outRate;
	int count;
	zchip *chips[ZVGM_MAX_CHIPS];
	INT32 chipOut[2][ZVGM_MAX_FRAMES * 2];
	INT64 mix[ZVGM_MAX_FRAMES * 2];
};

static void set_step(zchip *c)
{
	if (c->rate == 0 || c->eng->outRate == 0) {
		c->step = 0;
		return;
	}
	c->step = (((uint64_t)c->rate << 32) + c->eng->outRate / 2) / c->eng->outRate;
	if (c->step == 0)
		c->step = 1;
}

static void srate_changed(void *param, UINT32 rate)
{
	zchip *c = (zchip *)param;
	c->pendingRate = rate;
	c->rateChanged = 1;
}

static void apply_rate_change(zchip *c)
{
	if (!c->rateChanged)
		return;
	c->rateChanged = 0;
	if (c->pendingRate == c->rate || c->pendingRate > ZVGM_MAX_NATIVE_RATE)
		return;
	c->rate = c->pendingRate;
	c->phase = 0;
	set_step(c);
}

// ---- OPLL (emu2413) adapters ----

static void opll_update(void *chip, UINT32 n, DEV_SMPL **out)
{
	UINT32 i;
	for (i = 0; i < n; i++) {
		INT32 s = OPLL_calc((OPLL *)chip);
		out[0][i] = s;
		out[1][i] = s;
	}
}

static void opll_write(void *chip, UINT8 a, UINT8 d)
{
	OPLL_writeIO((OPLL *)chip, a, d);
}

static void opll_stop(void *chip)
{
	OPLL_delete((OPLL *)chip);
}

// ---- NES adapters ----

static void nes_write(void *chip, UINT8 a, UINT8 d)
{
	// Offsets past the APU registers belong to the FDS, which is not emulated.
	if (a < 0x20)
		nes_apu_write(chip, a, d);
}

// ---- generic DEV_DEF binding ----

static void bind_funcs(zchip *c, const DEV_DEF *def)
{
	const DEVDEF_RWFUNC *f;
	wm8_fn memW8 = NULL;

	c->update = def->Update;
	c->stop = def->Stop;
	if (def->SetSRateChgCB != NULL)
		def->SetSRateChgCB(c->chip, srate_changed, c);
	for (f = def->rwFuncs; f != NULL && f->funcPtr != NULL; f++) {
		if (f->funcType == (RWF_REGISTER | RWF_WRITE) && f->user == 0) {
			switch (f->rwType) {
			case DEVRW_A8D8:
				if (c->w8 == NULL)
					c->w8 = (w8_fn)f->funcPtr;
				break;
			case DEVRW_A16D8:
				if (c->wm8 == NULL)
					c->wm8 = (wm8_fn)f->funcPtr;
				break;
			case DEVRW_A8D16:
				if (c->wd16 == NULL)
					c->wd16 = (wd16_fn)f->funcPtr;
				break;
			case DEVRW_A16D16:
				if (c->wm16 == NULL)
					c->wm16 = (wm16_fn)f->funcPtr;
				break;
			}
		} else if (f->funcType == (RWF_MEMORY | RWF_WRITE)) {
			if (f->rwType == DEVRW_A16D8 && memW8 == NULL)
				memW8 = (wm8_fn)f->funcPtr;
			else if (f->rwType == DEVRW_BLOCK)
				c->romWrite = (block_fn)f->funcPtr;
			else if (f->rwType == DEVRW_MEMSIZE)
				c->romSize = (memsize_fn)f->funcPtr;
		}
	}
	// RF5C68 sample RAM is written byte-wise through its memory port.
	if (c->wm8 == NULL)
		c->wm8 = memW8;
	if (c->romWrite != NULL && c->romSize == NULL) {
		// Fixed-size RAM written at absolute offsets (RF5C68, SCSP, ES5503).
		c->ramWrite = c->romWrite;
		c->romWrite = NULL;
	}
	if (def->Reset != NULL)
		def->Reset(c->chip);
}

static int start_def(zchip *c, const DEV_DEF *def, const DEV_GEN_CFG *cfg)
{
	DEV_INFO inf;

	if (def == NULL || def->Start == NULL)
		return -1;
	memset(&inf, 0, sizeof(inf));
	if (def->Start(cfg, &inf) != 0 || inf.dataPtr == NULL)
		return -1;
	c->chip = inf.dataPtr;
	c->rate = inf.sampleRate;
	bind_funcs(c, def);
	return 0;
}

static void gen_cfg(DEV_GEN_CFG *g, const zvgm_chip_cfg *cfg, UINT32 outRate)
{
	memset(g, 0, sizeof(*g));
	g->srMode = DEVRI_SRMODE_NATIVE;
	g->clock = cfg->clock;
	g->smplRate = outRate;
	g->flags = (UINT8)cfg->flags;
}

static zchip *new_chip(zvgm_engine *e, UINT8 type)
{
	zchip *c;

	if (e->count >= ZVGM_MAX_CHIPS)
		return NULL;
	c = (zchip *)calloc(1, sizeof(zchip));
	if (c == NULL)
		return NULL;
	c->eng = e;
	c->type = type;
	return c;
}

static void free_chip(zchip *c)
{
	if (c == NULL)
		return;
	if (c->chip != NULL && c->stop != NULL)
		c->stop(c->chip);
	free(c->nesRam);
	free(c);
}

static void push_chip(zvgm_engine *e, zchip *c, INT32 volL, INT32 volR)
{
	c->grp = e->count;
	c->volL = volL;
	c->volR = volR;
	set_step(c);
	e->chips[e->count++] = c;
}

static const ssg_callbacks ssg_funcs = {
	ay8910_set_clock,
	ay8910_write,
	ay8910_read,
	ay8910_reset,
};

// Starts the AY8910 that renders the SSG part of an OPN chip.
static zchip *start_ssg(zvgm_engine *e, UINT8 ayType, UINT8 ayFlags, UINT32 clock)
{
	AY8910_CFG acfg;
	zchip *s = new_chip(e, DEVID_AY8910);

	if (s == NULL)
		return NULL;
	memset(&acfg, 0, sizeof(acfg));
	acfg._genCfg.srMode = DEVRI_SRMODE_NATIVE;
	acfg._genCfg.clock = clock;
	acfg._genCfg.smplRate = e->outRate;
	acfg.chipType = ayType;
	acfg.chipFlags = ayFlags;
	if (start_def(s, &devDef_AY8910_MAME, (const DEV_GEN_CFG *)&acfg) != 0) {
		free(s);
		return NULL;
	}
	// The OPN core writes SSG registers through ssg_funcs, not the engine.
	s->w8 = NULL;
	return s;
}

static const DEV_DEF *decl_core(const DEV_DECL *d)
{
	return d->cores[0];
}

static int start_chip(zvgm_engine *e, zchip *c, const zvgm_chip_cfg *cfg, zchip **ssgOut)
{
	DEV_GEN_CFG g;
	UINT32 clk = cfg->clock;

	gen_cfg(&g, cfg, e->outRate);
	*ssgOut = NULL;

	switch (cfg->type) {
	case DEVID_SN76496: {
		SN76496_CFG s;
		memset(&s, 0, sizeof(s));
		s._genCfg = g;
		s.noiseTaps = (UINT16)(cfg->p[0] | (cfg->p[1] << 8));
		// The width comes from the file; the core shifts by it, so keep it
		// to widths that fit an int (real chips use 15 or 16).
		s.shiftRegWidth = (cfg->p[2] == 0 || cfg->p[2] > 31) ? 16 : cfg->p[2];
		s.segaPSG = !(cfg->p[3] & 0x01);
		s.negate = (cfg->p[3] & 0x02) ? 1 : 0;
		s.stereo = !(cfg->p[3] & 0x04);
		s.clkDiv = (cfg->p[3] & 0x08) ? 1 : 8;
		s.ncrPSG = (cfg->p[3] & 0x10) ? 1 : 0;
		if (cfg->p[4] && e->count > 0) {
			// T6W28: the second instance is the noise half of the first and
			// reads its state while rendering, so they render together.
			int i;
			for (i = e->count - 1; i >= 0; i--) {
				if (e->chips[i]->type == DEVID_SN76496 && e->chips[i]->grp == i) {
					s.t6w28_tone = e->chips[i]->chip;
					c->grp = i;
					break;
				}
			}
			if (s.t6w28_tone == NULL)
				return -1;
		}
		return start_def(c, &devDef_SN76496_MAME, (const DEV_GEN_CFG *)&s);
	}
	case DEVID_YM2413: {
		OPLL *o;
		if (clk < 72)
			return -1;
		o = OPLL_new(clk, clk / 72);
		if (o == NULL)
			return -1;
		if (cfg->flags) {
			OPLL_setChipType(o, 1);
			OPLL_resetPatch(o, 1);
		}
		OPLL_reset(o);
		c->kind = K_OPLL;
		c->chip = o;
		c->rate = clk / 72;
		c->update = opll_update;
		c->stop = opll_stop;
		c->w8 = opll_write;
		return 0;
	}
	case DEVID_YM2612:
		if (clk < 144)
			return -1;
		c->rate = clk / 144;
		if (cfg->accurate) {
			c->kind = K_NUKED;
			c->chip = nukedopn2_init(clk, c->rate);
			if (c->chip == NULL)
				return -1;
			nukedopn2_set_options(c->chip, cfg->flags ? 0x10 : 0x00);
			nukedopn2_reset_chip(c->chip);
			c->update = nukedopn2_update;
			c->stop = nukedopn2_shutdown;
			c->w8 = nukedopn2_write;
			return 0;
		}
		c->kind = K_OPN;
		c->chip = ym2612_init(NULL, clk, c->rate, NULL, NULL);
		if (c->chip == NULL)
			return -1;
		ym2612_set_options(c->chip, cfg->p[0] ? 0x80 : 0x00);
		ym2612_reset_chip(c->chip);
		c->update = ym2612_update_one;
		c->stop = ym2612_shutdown;
		c->w8 = ym2612_write;
		return 0;
	case DEVID_YM2203:
	case DEVID_YM2608:
	case DEVID_YM2610: {
		zchip *s;
		UINT8 ayType;
		UINT32 ssgClk;
		UINT32 div = (cfg->type == DEVID_YM2203) ? 72 : 144;
		if (clk < div)
			return -1;
		c->kind = K_OPN;
		c->rate = clk / div;
		if (cfg->type == DEVID_YM2203) {
			ayType = AYTYPE_YM2203;
			ssgClk = clk / 2;
		} else {
			ayType = (cfg->type == DEVID_YM2608) ? AYTYPE_YM2608 : AYTYPE_YM2610;
			ssgClk = clk / 4;
		}
		s = start_ssg(e, ayType, cfg->p[0], ssgClk);
		if (s == NULL)
			return -1;
		if (cfg->type == DEVID_YM2203) {
			c->chip = ym2203_init(NULL, clk, c->rate, NULL, NULL);
			if (c->chip == NULL) {
				free_chip(s);
				return -1;
			}
			ym2203_set_srchg_cb(c->chip, srate_changed, c);
			ym2203_link_ssg(c->chip, &ssg_funcs, s->chip);
			ym2203_reset_chip(c->chip);
			c->update = ym2203_update_one;
			c->stop = ym2203_shutdown;
			c->w8 = ym2203_write;
		} else if (cfg->type == DEVID_YM2608) {
			c->chip = ym2608_init(NULL, clk, c->rate, NULL, NULL);
			if (c->chip == NULL) {
				free_chip(s);
				return -1;
			}
			ym2608_set_srchg_cb(c->chip, srate_changed, c);
			ym2608_link_ssg(c->chip, &ssg_funcs, s->chip);
			ym2608_reset_chip(c->chip);
			c->update = ym2608_update_one;
			c->stop = ym2608_shutdown;
			c->w8 = ym2608_write;
			c->dtSize = ym2608_alloc_pcmromb;
			c->dtWrite = ym2608_write_pcmromb;
		} else {
			c->chip = ym2610_init(NULL, clk, c->rate, NULL, NULL);
			if (c->chip == NULL) {
				free_chip(s);
				return -1;
			}
			ym2610_link_ssg(c->chip, &ssg_funcs, s->chip);
			ym2610_reset_chip(c->chip);
			c->update = cfg->flags ? ym2610b_update_one : ym2610_update_one;
			c->stop = ym2610_shutdown;
			c->w8 = ym2610_write;
			c->romSize = ym2610_alloc_pcmroma;
			c->romWrite = ym2610_write_pcmroma;
			c->dtSize = ym2610_alloc_pcmromb;
			c->dtWrite = ym2610_write_pcmromb;
		}
		*ssgOut = s;
		return 0;
	}
	case DEVID_YM3812:
	case DEVID_YM3526:
	case DEVID_Y8950:
		if (clk < 72)
			return -1;
		c->kind = K_OPL;
		c->rate = clk / 72;
		if (cfg->type == DEVID_YM3812) {
			c->chip = ym3812_init(clk, c->rate);
			c->update = ym3812_update_one;
			c->stop = ym3812_shutdown;
			c->w8 = ym3812_write;
		} else if (cfg->type == DEVID_YM3526) {
			c->chip = ym3526_init(clk, c->rate);
			c->update = ym3526_update_one;
			c->stop = ym3526_shutdown;
			c->w8 = ym3526_write;
		} else {
			c->chip = y8950_init(clk, c->rate);
			c->update = y8950_update_one;
			c->stop = y8950_shutdown;
			c->w8 = y8950_write;
			c->dtSize = y8950_alloc_pcmrom;
			c->dtWrite = y8950_write_pcmrom;
		}
		if (c->chip == NULL)
			return -1;
		if (cfg->type == DEVID_YM3812)
			ym3812_reset_chip(c->chip);
		else if (cfg->type == DEVID_YM3526)
			ym3526_reset_chip(c->chip);
		else
			y8950_reset_chip(c->chip);
		return 0;
	case DEVID_YMF262:
		if (clk < 288)
			return -1;
		c->kind = K_OPL3;
		c->rate = clk / 288;
		c->chip = ymf262_init(clk, c->rate);
		if (c->chip == NULL)
			return -1;
		ymf262_reset_chip(c->chip);
		c->update = ymf262_update_one;
		c->stop = ymf262_shutdown;
		c->w8 = ymf262_write;
		return 0;
	case DEVID_NES_APU:
		if (clk < 16)
			return -1;
		c->kind = K_NES;
		c->rate = clk / 16;
		c->nesRam = (UINT8 *)calloc(1, NES_RAM_SIZE);
		if (c->nesRam == NULL)
			return -1;
		c->chip = device_start_nesapu(clk, c->rate);
		if (c->chip == NULL)
			return -1;
		nesapu_set_rom(c->chip, c->nesRam);
		device_reset_nesapu(c->chip);
		c->update = nes_apu_update;
		c->stop = device_stop_nesapu;
		c->w8 = nes_write;
		return 0;
	case DEVID_SEGAPCM: {
		SEGAPCM_CFG s;
		memset(&s, 0, sizeof(s));
		s._genCfg = g;
		// The shift comes from the file; past 21 the 21-bit bank mask is
		// empty anyway, and larger shifts are undefined in C.
		s.bnkshift = cfg->p[0] > 21 ? 21 : cfg->p[0];
		s.bnkmask = cfg->p[1];
		return start_def(c, decl_core(&sndDev_SegaPCM), (const DEV_GEN_CFG *)&s);
	}
	case DEVID_RF5C68:
	case DEVID_RF5C164:
		g.flags = (cfg->type == DEVID_RF5C164) ? 1 : 0;
		return start_def(c, &devDef_RF5C68_MAME, &g);
	case DEVID_YM2151:
		return start_def(c, &devDef_YM2151_MAME, &g);
	case DEVID_YMF271:
		return start_def(c, decl_core(&sndDev_YMF271), &g);
	case DEVID_YMZ280B:
		return start_def(c, decl_core(&sndDev_YMZ280B), &g);
	case DEVID_32X_PWM:
		return start_def(c, decl_core(&sndDev_32X_PWM), &g);
	case DEVID_AY8910: {
		AY8910_CFG a;
		memset(&a, 0, sizeof(a));
		a._genCfg = g;
		a.chipType = cfg->p[0];
		a.chipFlags = cfg->p[1];
		return start_def(c, &devDef_AY8910_MAME, (const DEV_GEN_CFG *)&a);
	}
	case DEVID_GB_DMG:
		return start_def(c, &devDef_GB_MAME, &g);
	case DEVID_YMW258:
		return start_def(c, decl_core(&sndDev_YMW258), &g);
	case DEVID_uPD7759:
		return start_def(c, decl_core(&sndDev_uPD7759), &g);
	case DEVID_MSM6258: {
		MSM6258_CFG m;
		memset(&m, 0, sizeof(m));
		m._genCfg = g;
		m.divider = cfg->p[0] & 0x03;
		m.adpcmBits = (cfg->p[0] & 0x04) ? MSM6258_ADPCM_4B : MSM6258_ADPCM_3B;
		m.outputBits = (cfg->p[0] & 0x08) ? MSM6258_OUT_12B : MSM6258_OUT_10B;
		return start_def(c, decl_core(&sndDev_MSM6258), (const DEV_GEN_CFG *)&m);
	}
	case DEVID_MSM6295:
		return start_def(c, decl_core(&sndDev_MSM6295), &g);
	case DEVID_K051649:
		return start_def(c, decl_core(&sndDev_K051649), &g);
	case DEVID_K054539:
		return start_def(c, decl_core(&sndDev_K054539), &g);
	case DEVID_C6280:
		return start_def(c, &devDef_C6280_MAME, &g);
	case DEVID_C140:
		if (cfg->p[0] == 2) {
			c->swapRom = 1;
			return start_def(c, decl_core(&sndDev_C219), &g);
		}
		return start_def(c, decl_core(&sndDev_C140), &g);
	case DEVID_K053260:
		return start_def(c, decl_core(&sndDev_K053260), &g);
	case DEVID_POKEY:
		return start_def(c, decl_core(&sndDev_Pokey), &g);
	case DEVID_QSOUND:
		return start_def(c, &devDef_QSound_MAME, &g);
	case DEVID_SCSP:
		return start_def(c, decl_core(&sndDev_SCSP), &g);
	case DEVID_VBOY_VSU:
		return start_def(c, decl_core(&sndDev_VBoyVSU), &g);
	case DEVID_SAA1099:
		return start_def(c, &devDef_SAA1099_MAME, &g);
	case DEVID_ES5503:
		return start_def(c, decl_core(&sndDev_ES5503), &g);
	case DEVID_X1_010:
		return start_def(c, decl_core(&sndDev_X1_010), &g);
	case DEVID_C352:
		return start_def(c, decl_core(&sndDev_C352), &g);
	case DEVID_GA20:
		return start_def(c, decl_core(&sndDev_GA20), &g);
	case DEVID_MIKEY:
		return start_def(c, decl_core(&sndDev_Mikey), &g);
	}
	return -1;
}

zvgm_engine *zvgm_new(uint32_t outRate)
{
	zvgm_engine *e;

	if (outRate == 0)
		return NULL;
	e = (zvgm_engine *)calloc(1, sizeof(zvgm_engine));
	if (e == NULL)
		return NULL;
	e->outRate = outRate;
	return e;
}

void zvgm_free(zvgm_engine *e)
{
	int i;

	if (e == NULL)
		return;
	// Free in reverse so an OPN chip goes before the SSG it writes to and
	// a T6W28 noise half before its tone half.
	for (i = e->count - 1; i >= 0; i--)
		free_chip(e->chips[i]);
	free(e);
}

int zvgm_add_chip(zvgm_engine *e, const zvgm_chip_cfg *cfg)
{
	zchip *c;
	zchip *ssg = NULL;
	int idx;

	if (e == NULL || cfg == NULL || e->count + 2 > ZVGM_MAX_CHIPS)
		return -1;
	if (cfg->clock < ZVGM_MIN_CLOCK)
		return -1;
	c = new_chip(e, cfg->type);
	if (c == NULL)
		return -1;
	if (start_chip(e, c, cfg, &ssg) != 0) {
		free_chip(c);
		return -1;
	}
	if (c->rate == 0 || c->rate > ZVGM_MAX_NATIVE_RATE ||
		(ssg != NULL && (ssg->rate == 0 || ssg->rate > ZVGM_MAX_NATIVE_RATE))) {
		// Absurd clocks would make rendering crawl; real chips stay far below.
		free_chip(c);
		free_chip(ssg);
		return -1;
	}
	{
		int linked = c->grp;
		int tw = (cfg->type == DEVID_SN76496 && cfg->p[4]);
		idx = e->count;
		push_chip(e, c, cfg->volL, cfg->volR);
		if (tw) {
			// The noise half joins the tone half's group; the group leader
			// must come first in the chip list, which it does.
			c->grp = linked;
		}
	}
	if (ssg != NULL) {
		// The SSG renders after its OPN chip and is driven by its writes.
		push_chip(e, ssg, cfg->ssgVolL, cfg->ssgVolR);
		ssg->grp = idx;
	}
	return idx;
}

static void write_block(zchip *c, block_fn fn, UINT32 start, const UINT8 *data, UINT32 len)
{
	UINT8 *tmp;
	UINT32 i;

	if (!c->swapRom) {
		fn(c->chip, start, len, data);
		return;
	}
	// C219 sample ROMs are stored byte-swapped per 16-bit word.
	tmp = (UINT8 *)malloc(len ? len : 1);
	if (tmp == NULL)
		return;
	for (i = 0; i < len; i++) {
		UINT32 a = start + i;
		UINT32 src = (a & 1) ? i - 1 : i + 1;
		tmp[i] = (src < len) ? data[src] : 0;
	}
	fn(c->chip, start, len, tmp);
	free(tmp);
}

void zvgm_mem(zvgm_engine *e, int chip, int region, uint32_t romSize, uint32_t start,
	const uint8_t *data, uint32_t len)
{
	zchip *c;

	if (e == NULL || chip < 0 || chip >= e->count || data == NULL)
		return;
	c = e->chips[chip];
	if (start > romSize || len > romSize - start)
		return;
	switch (region) {
	case ZVGM_MEM_ROM:
		if (c->romSize == NULL || c->romWrite == NULL)
			return;
		// The cores mask ROM addresses with the smallest power-of-two mask
		// covering the size, so a size between powers of two lets masked
		// reads run past the allocation. Bytes past the file's size read as
		// the cores' fill value instead.
		c->romSize(c->chip, pow2_mask(romSize) + 1);
		write_block(c, c->romWrite, start, data, len);
		break;
	case ZVGM_MEM_DELTAT:
		if (c->dtSize == NULL || c->dtWrite == NULL)
			return;
		c->dtSize(c->chip, pow2_mask(romSize) + 1);
		c->dtWrite(c->chip, start, len, data);
		break;
	case ZVGM_MEM_RAM:
		if (c->nesRam != NULL) {
			if (start >= NES_RAM_SIZE)
				return;
			if (len > NES_RAM_SIZE - start)
				len = NES_RAM_SIZE - start;
			memcpy(c->nesRam + start, data, len);
		} else if (c->ramWrite != NULL) {
			c->ramWrite(c->chip, start, len, data);
		}
		break;
	}
}

static void apply_write(zchip *c, const zvgm_write *w)
{
	switch (w->kind) {
	case ZVGM_W8:
		if (c->w8 != NULL)
			c->w8(c->chip, (UINT8)w->addr, (UINT8)w->data);
		break;
	case ZVGM_WM8:
		if (c->wm8 != NULL)
			c->wm8(c->chip, w->addr, (UINT8)w->data);
		break;
	case ZVGM_WD16:
		if (c->wd16 != NULL)
			c->wd16(c->chip, (UINT8)w->addr, w->data);
		break;
	case ZVGM_WM16:
		if (c->wm16 != NULL)
			c->wm16(c->chip, w->addr, w->data);
		break;
	case ZVGM_AYSTEREO:
		if (c->type == DEVID_AY8910 && c->kind == K_GENERIC && c->chip != NULL)
			ay8910_set_stereo_mask(c->chip, w->data);
		break;
	}
}

// Native samples the resampler must fetch to produce n output frames.
static UINT32 native_needed(const zchip *c, UINT32 n)
{
	uint64_t t = (uint64_t)n * c->step;

	if (c->step > ONE) {
		if (t <= c->phase)
			return 0;
		return (UINT32)((t - c->phase + ONE - 1) >> 32);
	}
	return (UINT32)((c->phase + t) >> 32);
}

static void fill_native(zchip *c, UINT32 count)
{
	DEV_SMPL *bufs[2];

	if (count > NBUF)
		count = NBUF;
	if (count == 0)
		count = 1;
	memset(c->nb[0], 0, count * sizeof(DEV_SMPL));
	memset(c->nb[1], 0, count * sizeof(DEV_SMPL));
	bufs[0] = c->nb[0];
	bufs[1] = c->nb[1];
	if (c->update != NULL && count > 0)
		c->update(c->chip, count, bufs);
	c->nbPos = 0;
	c->nbLen = count;
}

// Renders n output frames of one chip into out (interleaved, may be NULL
// for frames that are not kept, which still advance the chip identically).
static void render_chip(zchip *c, INT32 *out, UINT32 n)
{
	UINT32 need, i;

	if (n == 0)
		return;
	if (c->step == 0) {
		if (out != NULL)
			memset(out, 0, (size_t)n * 2 * sizeof(INT32));
		return;
	}
	need = native_needed(c, n);
	for (i = 0; i < n; i++) {
		INT32 l, r;
		if (c->step > ONE) {
			// Box filter: average the native samples covering this frame.
			INT64 sl = 0, sr = 0;
			uint64_t want = c->step;
			while (want > 0) {
				uint64_t take;
				if (c->phase == 0) {
					if (c->nbPos >= c->nbLen)
						fill_native(c, need);
					need--;
					c->cur[0] = c->nb[0][c->nbPos];
					c->cur[1] = c->nb[1][c->nbPos];
					c->nbPos++;
					c->phase = ONE;
				}
				take = want < c->phase ? want : c->phase;
				sl += (INT64)c->cur[0] * (INT64)(take >> 16);
				sr += (INT64)c->cur[1] * (INT64)(take >> 16);
				want -= take;
				c->phase -= take;
			}
			l = (INT32)(sl / (INT64)(c->step >> 16));
			r = (INT32)(sr / (INT64)(c->step >> 16));
		} else {
			// Linear interpolation between the last two native samples.
			INT64 f = (INT64)(c->phase >> 16);
			l = c->prev[0] + (INT32)(((INT64)(c->cur[0] - c->prev[0]) * f) >> 16);
			r = c->prev[1] + (INT32)(((INT64)(c->cur[1] - c->prev[1]) * f) >> 16);
			c->phase += c->step;
			while (c->phase >= ONE) {
				c->phase -= ONE;
				if (c->nbPos >= c->nbLen)
					fill_native(c, need);
				need--;
				c->prev[0] = c->cur[0];
				c->prev[1] = c->cur[1];
				c->cur[0] = c->nb[0][c->nbPos];
				c->cur[1] = c->nb[1][c->nbPos];
				c->nbPos++;
			}
		}
		if (out != NULL) {
			out[i * 2] = l;
			out[i * 2 + 1] = r;
		}
	}
}

static void render_member(zvgm_engine *e, zchip *c, int slot, int to, int keep)
{
	if (to > c->rendered) {
		render_chip(c, keep ? e->chipOut[slot] + c->rendered * 2 : NULL,
			(UINT32)(to - c->rendered));
		c->rendered = to;
	}
}

int zvgm_render(zvgm_engine *e, const zvgm_write *w, int nw, float *out, int frames)
{
	int ci, i, k, m;
	int keep = (out != NULL);

	if (e == NULL || frames < 0 || frames > ZVGM_MAX_FRAMES || nw < 0 || (nw > 0 && w == NULL))
		return -1;
	if (keep)
		memset(e->mix, 0, (size_t)frames * 2 * sizeof(INT64));

	// Chips render one group at a time. A group is a chip plus the chips its
	// writes or state drive (an OPN chip's SSG, a T6W28 noise half); members
	// advance in lockstep so a write lands at the same frame for all of them.
	for (ci = 0; ci < e->count; ci++) {
		zchip *members[2];
		int nm = 0;

		if (e->chips[ci]->grp != ci)
			continue;
		for (m = ci; m < e->count && nm < 2; m++) {
			if (e->chips[m]->grp == ci) {
				e->chips[m]->rendered = 0;
				members[nm++] = e->chips[m];
			}
		}

		for (k = 0; k < nw; k++) {
			int f;
			zchip *target;
			if (w[k].chip >= e->count || e->chips[w[k].chip]->grp != ci)
				continue;
			target = e->chips[w[k].chip];
			f = (int)(w[k].frame > (uint32_t)frames ? (uint32_t)frames : w[k].frame);
			for (m = 0; m < nm; m++)
				render_member(e, members[m], m, f, keep);
			apply_write(target, &w[k]);
			for (m = 0; m < nm; m++)
				apply_rate_change(members[m]);
		}
		for (m = 0; m < nm; m++) {
			zchip *c = members[m];
			render_member(e, c, m, frames, keep);
			apply_rate_change(c);
			if (keep && (c->volL != 0 || c->volR != 0)) {
				const INT32 *src = e->chipOut[m];
				for (i = 0; i < frames; i++) {
					e->mix[i * 2] += (INT64)src[i * 2] * c->volL;
					e->mix[i * 2 + 1] += (INT64)src[i * 2 + 1] * c->volR;
				}
			}
		}
	}

	if (keep) {
		const float scale = 1.0f / (32768.0f * 256.0f);
		for (i = 0; i < frames * 2; i++)
			out[i] = (float)e->mix[i] * scale;
	}
	return 0;
}
#include "csrc/zvgm_suffix.h"
