// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// The C side of nativedec: dr_mp3, dr_flac and stb_vorbis built into one
// unit behind a small API, so the Go side never sees their structs.

#include <stdlib.h>
#include <string.h>

#define DR_MP3_IMPLEMENTATION
#include "dr_mp3.h"

#define DR_FLAC_IMPLEMENTATION
#define DR_FLAC_NO_OGG
#include "dr_flac.h"

#include "stb_vorbis.c"

#include "decoders.h"

struct nd_decoder {
	int kind;
	unsigned channels;
	drmp3 mp3;
	drmp3_seek_point* seek_points;
	int seek_table_tried;
	drflac* flac;
	stb_vorbis* vorbis;
	/* A copy of the encoded data for a decoder opened from memory, owned
	   by the decoder and freed with it. */
	void* data;
};

/* Opens a decoder from path, or from data when path is NULL. */
static nd_decoder* nd_open_any(const char* path, void* data, size_t size, int kind,
	unsigned* channels, unsigned* rate, long long* frames)
{
	nd_decoder* d = (nd_decoder*)calloc(1, sizeof(nd_decoder));
	if (d == NULL) {
		return NULL;
	}
	d->kind = kind;
	d->data = data;
	*frames = -1;
	switch (kind) {
	case ND_MP3: {
		drmp3_bool32 ok = path != NULL ? drmp3_init_file(&d->mp3, path, NULL)
			: drmp3_init_memory(&d->mp3, data, size, NULL);
		if (!ok) {
			free(d);
			return NULL;
		}
		d->channels = d->mp3.channels;
		*rate = d->mp3.sampleRate;
		/* The length is free when the file carries a Xing or Info header;
		   without one dr_mp3 walks every frame header to count, and the
		   stream is rewound afterwards. The seek table is another walk, so
		   it waits for the first seek rather than delaying playback. */
		int counted = d->mp3.totalPCMFrameCount == DRMP3_UINT64_MAX;
		drmp3_uint64 total = drmp3_get_pcm_frame_count(&d->mp3);
		*frames = (long long)total;
		if (counted) {
			drmp3_seek_to_pcm_frame(&d->mp3, 0);
		}
		break;
	}
	case ND_FLAC:
		d->flac = path != NULL ? drflac_open_file(path, NULL) : drflac_open_memory(data, size, NULL);
		if (d->flac == NULL) {
			free(d);
			return NULL;
		}
		d->channels = d->flac->channels;
		*rate = d->flac->sampleRate;
		if (d->flac->totalPCMFrameCount > 0) {
			*frames = (long long)d->flac->totalPCMFrameCount;
		}
		break;
	case ND_VORBIS: {
		int err = 0;
		d->vorbis = path != NULL ? stb_vorbis_open_filename(path, &err, NULL)
			: stb_vorbis_open_memory((const unsigned char*)data, (int)size, &err, NULL);
		if (d->vorbis == NULL) {
			free(d);
			return NULL;
		}
		stb_vorbis_info info = stb_vorbis_get_info(d->vorbis);
		d->channels = (unsigned)info.channels;
		*rate = info.sample_rate;
		unsigned int total = stb_vorbis_stream_length_in_samples(d->vorbis);
		if (total > 0) {
			*frames = (long long)total;
		}
		break;
	}
	default:
		free(d);
		return NULL;
	}
	*channels = d->channels;
	return d;
}

nd_decoder* nd_open(const char* path, int kind, unsigned* channels, unsigned* rate, long long* frames)
{
	return nd_open_any(path, NULL, 0, kind, channels, rate, frames);
}

nd_decoder* nd_open_memory(void* data, size_t size, int kind, unsigned* channels, unsigned* rate, long long* frames)
{
	nd_decoder* d = nd_open_any(NULL, data, size, kind, channels, rate, frames);
	if (d == NULL) {
		free(data);
	}
	return d;
}

long long nd_read(nd_decoder* d, float* out, long long frames)
{
	switch (d->kind) {
	case ND_MP3:
		return (long long)drmp3_read_pcm_frames_f32(&d->mp3, (drmp3_uint64)frames, out);
	case ND_FLAC:
		return (long long)drflac_read_pcm_frames_f32(d->flac, (drflac_uint64)frames, out);
	case ND_VORBIS:
		return (long long)stb_vorbis_get_samples_float_interleaved(
			d->vorbis, (int)d->channels, out, (int)(frames * d->channels));
	}
	return 0;
}

int nd_failed(nd_decoder* d)
{
	switch (d->kind) {
	case ND_FLAC:
		/* A short read before the length STREAMINFO promises means the
		   stream broke off, not that it ended. */
		return d->flac->totalPCMFrameCount > 0 && d->flac->currentPCMFrame < d->flac->totalPCMFrameCount;
	case ND_VORBIS: {
		int err = stb_vorbis_get_error(d->vorbis);
		return err != 0 && err != VORBIS_need_more_data;
	}
	}
	/* dr_mp3 reports nothing: a broken frame ends the stream like EOF. */
	return 0;
}

int nd_seek(nd_decoder* d, long long frame)
{
	switch (d->kind) {
	case ND_MP3:
		if (frame > 0 && !d->seek_table_tried) {
			/* One pass over the frame headers gives a seek table, so a seek
			   decodes from the nearest point rather than from the start of
			   the file. A failure leaves seeking to decode from the start. */
			drmp3_uint32 count = 256;
			d->seek_table_tried = 1;
			d->seek_points = (drmp3_seek_point*)malloc(sizeof(drmp3_seek_point) * count);
			if (d->seek_points != NULL && drmp3_calculate_seek_points(&d->mp3, &count, d->seek_points)) {
				drmp3_bind_seek_table(&d->mp3, count, d->seek_points);
			}
		}
		return drmp3_seek_to_pcm_frame(&d->mp3, (drmp3_uint64)frame) ? 1 : 0;
	case ND_FLAC:
		return drflac_seek_to_pcm_frame(d->flac, (drflac_uint64)frame) ? 1 : 0;
	case ND_VORBIS:
		return stb_vorbis_seek(d->vorbis, (unsigned int)frame) ? 1 : 0;
	}
	return 0;
}

void nd_close(nd_decoder* d)
{
	if (d == NULL) {
		return;
	}
	switch (d->kind) {
	case ND_MP3:
		drmp3_uninit(&d->mp3);
		free(d->seek_points);
		break;
	case ND_FLAC:
		drflac_close(d->flac);
		break;
	case ND_VORBIS:
		stb_vorbis_close(d->vorbis);
		break;
	}
	free(d->data);
	free(d);
}
