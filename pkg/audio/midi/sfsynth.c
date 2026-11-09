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

#include <stdlib.h>
#include <string.h>

/* SF3 SoundFonts store Ogg Vorbis samples. stb_vorbis is already compiled
   into the binary by pkg/audio/nativedec, which this package imports, so
   only its declarations are pulled in here; a second implementation would
   define every stb_vorbis symbol twice. The configuration macro must match
   the one nativedec builds with. */
#define STB_VORBIS_HEADER_ONLY
#define STB_VORBIS_NO_PUSHDATA_API
#include "../nativedec/third_party/stb_vorbis.c"

/* zm_sf_load can parse a SoundFont under a memory budget, so one that
   would decode to more than the device can hold fails, and the song falls
   back to FM, rather than exhausting memory. Each TinySoundFont block
   records its size ahead of it so frees and reallocs can be counted. */
typedef union {
	size_t size;
	long double align_ld;
	void* align_p;
} zm_hdr;

static _Thread_local int zm_budgeted;
static _Thread_local size_t zm_budget_left;

static int zm_take(size_t n)
{
	if (!zm_budgeted) return 1;
	if (n > zm_budget_left) return 0;
	zm_budget_left -= n;
	return 1;
}

static void zm_give(size_t n)
{
	if (zm_budgeted) zm_budget_left += n;
}

static void* zm_malloc(size_t n)
{
	zm_hdr* h;
	if (n > (size_t)-1 - sizeof(zm_hdr) || !zm_take(n)) return NULL;
	h = (zm_hdr*)malloc(sizeof(zm_hdr) + n);
	if (h == NULL) { zm_give(n); return NULL; }
	h->size = n;
	return h + 1;
}

static void zm_free(void* p)
{
	zm_hdr* h;
	if (p == NULL) return;
	h = (zm_hdr*)p - 1;
	zm_give(h->size);
	free(h);
}

static void* zm_realloc(void* p, size_t n)
{
	zm_hdr *h, *nh;
	size_t old;
	if (p == NULL) return zm_malloc(n);
	h = (zm_hdr*)p - 1;
	old = h->size;
	if (n > (size_t)-1 - sizeof(zm_hdr)) return NULL;
	if (n > old && !zm_take(n - old)) return NULL;
	nh = (zm_hdr*)realloc(h, sizeof(zm_hdr) + n);
	if (nh == NULL) {
		if (n > old) zm_give(n - old);
		return NULL;
	}
	if (n < old) zm_give(old - n);
	nh->size = n;
	return nh + 1;
}

#define TSF_MALLOC zm_malloc
#define TSF_REALLOC zm_realloc
#define TSF_FREE zm_free
#define TSF_STATIC
#define TSF_NO_STDIO
#define TSF_IMPLEMENTATION
#include "third_party/tinysoundfont/tsf.h"

#define TML_STATIC
#define TML_NO_STDIO
#define TML_IMPLEMENTATION
#include "third_party/tinysoundfont/tml.h"

#include "sfsynth.h"

struct zm_sf {
	tsf* f;
};

struct zm_song {
	tsf* f;
	tml_message* first;
	tml_message* next;
	double ms_per_frame;
	double now_ms;
};

zm_sf* zm_sf_load(const void* data, int size, long long budget)
{
	zm_sf* sf;
	tsf* f;
	if (data == NULL || size <= 0) return NULL;
	zm_budgeted = budget > 0;
	zm_budget_left = budget > 0 ? (size_t)budget : 0;
	f = tsf_load_memory(data, size);
	zm_budgeted = 0;
	if (f == NULL) return NULL;
	if (tsf_get_presetcount(f) <= 0) { tsf_close(f); return NULL; }
	sf = (zm_sf*)calloc(1, sizeof(zm_sf));
	if (sf == NULL) { tsf_close(f); return NULL; }
	sf->f = f;
	return sf;
}

void zm_sf_close(zm_sf* sf)
{
	if (sf == NULL) return;
	if (sf->f != NULL) tsf_close(sf->f);
	free(sf);
}

static void zm_song_setup_channels(zm_song* s)
{
	int ch;
	/* TinySoundFont ignores notes on a channel no earlier event has set
	   up, so every channel starts on General MIDI program 0. */
	for (ch = 0; ch < 16; ch++)
		if (ch != 9) tsf_channel_set_presetnumber(s->f, ch, 0, 0);
	/* Channel 10 is the General MIDI drum channel. */
	tsf_channel_set_bank_preset(s->f, 9, 128, 0);
}

zm_song* zm_song_open(zm_sf* sf, const void* data, int size, int rate,
	int max_voices, unsigned int* length_ms)
{
	zm_song* s;
	tml_message* first;
	unsigned int len = 0;
	if (sf == NULL || data == NULL || size <= 0 || rate <= 0) return NULL;
	first = tml_load_memory(data, size);
	if (first == NULL) return NULL;
	s = (zm_song*)calloc(1, sizeof(zm_song));
	if (s == NULL) { tml_free(first); return NULL; }
	tml_get_info(first, NULL, NULL, NULL, NULL, &len);
	s->first = first;
	s->next = first;
	s->ms_per_frame = 1000.0 / (double)rate;
	s->f = sf->f;
	sf->f = NULL;
	zm_sf_close(sf);
	tsf_set_output(s->f, TSF_STEREO_INTERLEAVED, rate, 0.0f);
	if (max_voices > 0) tsf_set_max_voices(s->f, max_voices);
	zm_song_setup_channels(s);
	if (length_ms != NULL) *length_ms = len;
	return s;
}

static void zm_apply(zm_song* s, const tml_message* m, int notes)
{
	switch (m->type) {
	case TML_PROGRAM_CHANGE:
		tsf_channel_set_presetnumber(s->f, m->channel, m->program, m->channel == 9);
		break;
	case TML_NOTE_ON:
		if (notes) tsf_channel_note_on(s->f, m->channel, m->key, m->velocity / 127.0f);
		break;
	case TML_NOTE_OFF:
		if (notes) tsf_channel_note_off(s->f, m->channel, m->key);
		break;
	case TML_PITCH_BEND:
		tsf_channel_set_pitchwheel(s->f, m->channel, m->pitch_bend);
		break;
	case TML_CONTROL_CHANGE:
		tsf_channel_midi_control(s->f, m->channel, m->control, m->control_value);
		break;
	default:
		break;
	}
}

void zm_song_render(zm_song* s, float* out, int frames)
{
	while (frames > 0) {
		int block = frames < TSF_RENDER_EFFECTSAMPLEBLOCK ? frames : TSF_RENDER_EFFECTSAMPLEBLOCK;
		s->now_ms += block * s->ms_per_frame;
		for (; s->next != NULL && s->now_ms >= s->next->time; s->next = s->next->next)
			zm_apply(s, s->next, 1);
		tsf_render_float(s->f, out, block, 0);
		out += block * 2;
		frames -= block;
	}
}

void zm_song_seek(zm_song* s, long long frame)
{
	double target = (double)frame * s->ms_per_frame;
	struct tsf_voice *v = s->f->voices, *vEnd = v + s->f->voiceNum;
	/* A seek is a cut: silence every voice now rather than letting
	   tsf_reset fade them out over the first milliseconds at the target. */
	for (; v != vEnd; v++)
		tsf_voice_kill(v);
	tsf_reset(s->f);
	zm_song_setup_channels(s);
	s->next = s->first;
	for (; s->next != NULL && target > s->next->time; s->next = s->next->next)
		zm_apply(s, s->next, 0);
	s->now_ms = target;
}

void zm_song_close(zm_song* s)
{
	if (s == NULL) return;
	if (s->f != NULL) tsf_close(s->f);
	if (s->first != NULL) tml_free(s->first);
	free(s);
}
