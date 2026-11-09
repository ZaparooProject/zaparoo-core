// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// Push decoders for endless network streams: the caller appends encoded
// bytes as they arrive and reads whatever audio they complete. MP3 uses
// dr_mp3's low-level frame decoder, Ogg Vorbis stb_vorbis's pushdata API
// and AAC libfaad on ADTS frames. The input is untrusted: every length is
// checked against the buffer, garbage is skipped while resyncing, and a
// long run without audio is a hard error rather than an endless scan.

#include <limits.h>
#include <stdlib.h>
#include <string.h>

#include "dr_mp3.h"

#define STB_VORBIS_HEADER_ONLY
#include "stb_vorbis.c"

#include "faad2_prefix.h"
#include "neaacdec.h"

#include "decoders.h"
#include "streamdec.h"

/* Largest decoded frame, per channel: a Vorbis long block (8192 / 2), an
   HE-AAC frame (2048) and an MP3 frame (1152) all fit. */
#define NDS_PCM_MAX 8192
/* Zeroed bytes kept past the input, so a decoder that reads a word ahead
   of what it was handed never leaves the allocation. */
#define NDS_SLACK 64
/* Input consumed with no audio before the stream counts as undecodable. */
#define NDS_JUNK_LIMIT (1u << 20)
/* Consecutive frames that failed to decode before the same. */
#define NDS_BAD_FRAME_LIMIT 128
/* MP3 data buffered before decoding: minimp3 confirms a sync by matching
   the frames that follow, and drops what it cannot match, so a partial
   buffer must not reach it. Once in sync one frame (at most 2881 bytes,
   free format included) plus the next header is enough. */
#define NDS_MP3_SYNC_BYTES 16384
#define NDS_MP3_FRAME_BYTES 4096
#define NDS_ADTS_MAX 8191
/* All memory one Vorbis decoder may use, headers and decode buffers both.
   stb_vorbis sizes its codebooks from the setup header, which a hostile
   stream can inflate to gigabytes; with an arena that fails the open
   instead. Ordinary streams need a few hundred KiB. */
#define NDS_VORBIS_ARENA (2u << 20)

struct nd_stream {
	int kind;
	unsigned char* in;
	size_t cap;   /* most input bytes held */
	size_t alloc; /* allocated input bytes, without the slack */
	size_t start; /* first unread byte */
	size_t len;   /* unread bytes */
	int ended;
	int failed; /* sticky NDS_ERROR or NDS_UNSUPPORTED */

	float pcm[NDS_PCM_MAX * 2];
	int pcm_frames, pcm_pos;
	unsigned pcm_rate, pcm_channels;
	unsigned out_rate, out_channels;
	unsigned hdr_rate, hdr_channels;

	size_t junk;
	int bad_frames;

	drmp3dec* mp3;
	int mp3_synced;
	short mp3_pcm[DRMP3_MAX_SAMPLES_PER_FRAME];

	NeAACDecHandle aac;
	unsigned char aac_sig[4]; /* the ADTS header start the decoder was set up for */
	int aac_synced;

	stb_vorbis* vorbis;
	char* vorbis_mem; /* NDS_VORBIS_ARENA bytes backing the decoder */
	size_t bos_scanned; /* bytes from start already searched for a new stream */
};

/* Internal step results, distinct from the NDS_ codes a step also returns. */
enum { STEP_AUDIO = 100, STEP_CONTINUE = 101 };

static void consume(nd_stream* s, size_t n)
{
	if (n > s->len) {
		n = s->len;
	}
	s->start += n;
	s->len -= n;
	if (s->len == 0) {
		s->start = 0;
	}
	s->bos_scanned = s->bos_scanned > n ? s->bos_scanned - n : 0;
}

/* Counts bytes consumed without audio; NDS_ERROR once there are too many. */
static int junk(nd_stream* s, size_t n)
{
	s->junk += n;
	return s->junk > NDS_JUNK_LIMIT ? NDS_ERROR : STEP_CONTINUE;
}

static int bad_frame(nd_stream* s, size_t n)
{
	s->bad_frames++;
	if (s->bad_frames > NDS_BAD_FRAME_LIMIT) {
		return NDS_ERROR;
	}
	return junk(s, n);
}

static void got_audio(nd_stream* s, int frames, unsigned rate, unsigned channels)
{
	s->pcm_frames = frames;
	s->pcm_pos = 0;
	s->pcm_rate = rate;
	s->pcm_channels = channels;
	s->junk = 0;
	s->bad_frames = 0;
}

static int full(const nd_stream* s)
{
	return s->len >= s->cap;
}

/* ---- MP3 ---- */

static int mp3_step(nd_stream* s)
{
	drmp3dec_frame_info info;
	const unsigned char* p = s->in + s->start;
	int avail, samples, i, ch;
	size_t need = s->mp3_synced ? NDS_MP3_FRAME_BYTES : NDS_MP3_SYNC_BYTES;

	if (s->len == 0 || (!s->ended && s->len < need && !full(s))) {
		return NDS_NEED_MORE;
	}
	avail = s->len > INT_MAX ? INT_MAX : (int)s->len;
	memset(&info, 0, sizeof(info));
	samples = drmp3dec_decode_frame(s->mp3, p, avail, s->mp3_pcm, &info);
	if (info.frame_bytes <= 0) {
		/* Only a short buffer gets here; at the end it holds no frame. */
		if (s->ended) {
			consume(s, s->len);
		}
		return NDS_NEED_MORE;
	}
	if (info.frame_bytes > avail) {
		info.frame_bytes = avail;
	}
	consume(s, (size_t)info.frame_bytes);
	ch = info.channels;
	if (samples <= 0 || ch < 1 || ch > 2 || info.sample_rate <= 0 || samples > DRMP3_MAX_PCM_FRAMES_PER_MP3_FRAME) {
		s->mp3_synced = 0;
		return junk(s, (size_t)info.frame_bytes);
	}
	s->mp3_synced = 1;
	for (i = 0; i < samples; i++) {
		float l = (float)s->mp3_pcm[i * ch] / 32768.0f;
		float r = ch == 2 ? (float)s->mp3_pcm[i * ch + 1] / 32768.0f : l;
		s->pcm[i * 2] = l;
		s->pcm[i * 2 + 1] = r;
	}
	if (s->hdr_rate == 0) {
		s->hdr_rate = (unsigned)info.sample_rate;
		s->hdr_channels = (unsigned)ch;
	}
	got_audio(s, samples, (unsigned)info.sample_rate, (unsigned)ch);
	return STEP_AUDIO;
}

/* ---- AAC (ADTS) ---- */

/* The length of the ADTS frame whose header starts at p, or 0 when p is not
   a plausible header. */
static size_t adts_frame_length(const unsigned char* p)
{
	size_t length, header;
	if (p[0] != 0xFF || (p[1] & 0xF6) != 0xF0) {
		return 0;
	}
	if (((p[2] >> 2) & 0x0F) >= 12) {
		return 0; /* reserved or 7350 Hz, which libfaad rejects */
	}
	header = (p[1] & 0x01) ? 7 : 9;
	length = ((size_t)(p[3] & 0x03) << 11) | ((size_t)p[4] << 3) | ((size_t)p[5] >> 5);
	return length > header ? length : 0;
}

/* Whether two ADTS headers agree on the fields that are fixed for a stream:
   MPEG version, profile, sample rate and channels. */
static int adts_same_stream(const unsigned char* a, const unsigned char* b)
{
	return (a[1] & 0x08) == (b[1] & 0x08) && (a[2] & 0xFD) == (b[2] & 0xFD) && (a[3] & 0xC0) == (b[3] & 0xC0);
}

static void aac_close(nd_stream* s)
{
	if (s->aac != NULL) {
		NeAACDecClose(s->aac);
		s->aac = NULL;
	}
}

static int aac_setup(nd_stream* s, unsigned char* p, size_t length)
{
	NeAACDecConfigurationPtr cfg;
	unsigned long rate = 0;
	unsigned char channels = 0;

	aac_close(s);
	s->aac = NeAACDecOpen();
	if (s->aac == NULL) {
		return 0;
	}
	cfg = NeAACDecGetCurrentConfiguration(s->aac);
	cfg->outputFormat = FAAD_FMT_FLOAT;
	cfg->downMatrix = 1;
	NeAACDecSetConfiguration(s->aac, cfg);
	if (NeAACDecInit(s->aac, p, (unsigned long)length, &rate, &channels) < 0 || rate == 0 || channels == 0) {
		aac_close(s);
		return 0;
	}
	memcpy(s->aac_sig, p, 4);
	s->hdr_rate = (unsigned)rate;
	s->hdr_channels = channels;
	return 1;
}

static int aac_step(nd_stream* s)
{
	unsigned char* p = s->in + s->start;
	size_t avail = s->len, i = 0, length = 0;
	NeAACDecFrameInfo info;
	float* out;
	unsigned long frames, k;
	unsigned ch;
	int st;

	/* Find a frame: in sync, the next frame starts right here; otherwise a
	   header counts only once the frame after it starts with a matching
	   one, or the input ends right after it. */
	for (;;) {
		if (avail - i < 7) {
			break;
		}
		length = adts_frame_length(p + i);
		if (length == 0) {
			s->aac_synced = 0;
			i++;
			continue;
		}
		if (avail - i < length) {
			if (s->ended || full(s)) {
				s->aac_synced = 0;
				i++;
				continue;
			}
			break;
		}
		if (s->aac_synced && i == 0) {
			break;
		}
		if (avail - i < length + 7) {
			if (s->ended && avail - i == length) {
				break;
			}
			if (s->ended || full(s)) {
				i++;
				continue;
			}
			break;
		}
		if (adts_frame_length(p + i + length) != 0 && adts_same_stream(p + i, p + i + length)) {
			break;
		}
		i++;
	}
	if (i > 0) {
		consume(s, i);
		st = junk(s, i);
		if (st != STEP_CONTINUE) {
			return st;
		}
		p = s->in + s->start;
		avail = s->len;
	}
	if (avail < 7 || length == 0 || avail < length) {
		if (s->ended) {
			consume(s, s->len);
		}
		return NDS_NEED_MORE;
	}
	if (!s->aac_synced && avail < length + 7 && !s->ended) {
		return NDS_NEED_MORE;
	}

	if (s->aac == NULL || !adts_same_stream(s->aac_sig, p)) {
		if (!aac_setup(s, p, length)) {
			consume(s, length);
			s->aac_synced = 0;
			return bad_frame(s, length);
		}
	}
	memset(&info, 0, sizeof(info));
	out = (float*)NeAACDecDecode(s->aac, &info, p, (unsigned long)length);
	consume(s, length);
	if (info.error != 0 || out == NULL) {
		/* The framing still holds; only this frame's payload is bad. */
		s->aac_synced = 1;
		if (info.error != 0) {
			NeAACDecPostSeekReset(s->aac, 0);
		}
		return bad_frame(s, length);
	}
	s->aac_synced = 1;
	ch = info.channels;
	if (info.samples == 0 || ch == 0 || info.samplerate == 0) {
		return junk(s, length);
	}
	frames = info.samples / ch;
	if (frames > NDS_PCM_MAX) {
		frames = NDS_PCM_MAX;
	}
	for (k = 0; k < frames; k++) {
		float l = out[k * ch];
		s->pcm[k * 2] = l;
		s->pcm[k * 2 + 1] = ch >= 2 ? out[k * ch + 1] : l;
	}
	got_audio(s, (int)frames, (unsigned)info.samplerate, ch);
	return STEP_AUDIO;
}

/* ---- Ogg Vorbis ---- */

/* CRC-32 of Ogg pages (poly 0x04C11DB7, MSB first), a nibble at a time. The
   table is constant so decoders on different threads share it safely. */
static const unsigned ogg_crc_nibble[16] = {
	0x00000000u, 0x04C11DB7u, 0x09823B6Eu, 0x0D4326D9u,
	0x130476DCu, 0x17C56B6Bu, 0x1A864DB2u, 0x1E475005u,
	0x2608EDB8u, 0x22C9F00Fu, 0x2F8AD6D6u, 0x2B4BCB61u,
	0x350C9B64u, 0x31CD86D3u, 0x3C8EA00Au, 0x384FBDBDu,
};

static unsigned ogg_crc(const unsigned char* p, size_t n)
{
	unsigned crc = 0;
	size_t i;
	for (i = 0; i < n; i++) {
		unsigned char b = (i >= 22 && i < 26) ? 0 : p[i];
		crc ^= (unsigned)b << 24;
		crc = (crc << 4) ^ ogg_crc_nibble[crc >> 28];
		crc = (crc << 4) ^ ogg_crc_nibble[crc >> 28];
	}
	return crc;
}

/* Whether p starts an Ogg page with the beginning-of-stream flag: 1 yes, 0
   no, -1 when more bytes are needed to tell. *page_len is its length. */
static int ogg_bos_page(const unsigned char* p, size_t avail, size_t* page_len)
{
	size_t n, i, body = 0;
	unsigned stored;
	if (avail < 4) {
		return memcmp(p, "OggS", avail) == 0 ? -1 : 0;
	}
	if (memcmp(p, "OggS", 4) != 0) {
		return 0;
	}
	if (avail < 27) {
		return -1;
	}
	if (p[4] != 0 || (p[5] & 0x02) == 0) {
		return 0;
	}
	n = p[26];
	if (avail < 27 + n) {
		return -1;
	}
	for (i = 0; i < n; i++) {
		body += p[27 + i];
	}
	if (avail < 27 + n + body) {
		return -1;
	}
	stored = (unsigned)p[22] | ((unsigned)p[23] << 8) | ((unsigned)p[24] << 16) | ((unsigned)p[25] << 24);
	if (ogg_crc(p, 27 + n + body) != stored) {
		return 0;
	}
	*page_len = 27 + n + body;
	return 1;
}

/* The offset of the next plausible beginning-of-stream page after the
   first byte, or avail when there is none. A complete page whose CRC fails
   is passed over; an incomplete one still counts, so the old stream's
   decoder never reads into what may be a new stream's headers. */
static size_t ogg_next_bos(nd_stream* s, const unsigned char* p, size_t avail)
{
	size_t i = s->bos_scanned > 1 ? s->bos_scanned : 1, page;
	for (; i + 4 <= avail; i++) {
		if (p[i] == 'O' && memcmp(p + i, "OggS", 4) == 0) {
			int r = ogg_bos_page(p + i, avail - i, &page);
			if (r != 0) {
				s->bos_scanned = i;
				return i;
			}
		}
	}
	s->bos_scanned = avail > 3 ? avail - 3 : 0;
	return avail;
}

static void vorbis_close(nd_stream* s)
{
	if (s->vorbis != NULL) {
		stb_vorbis_close(s->vorbis);
		s->vorbis = NULL;
	}
}

static int vorbis_open(nd_stream* s)
{
	const unsigned char* p = s->in + s->start;
	size_t avail = s->len, page = 0, i, skip;
	const unsigned char* packet;
	stb_vorbis_info vi;
	stb_vorbis_alloc arena;
	int used = 0, err = 0, r;

	/* Skip to the first page that begins a stream. */
	for (i = 0; i < avail; i++) {
		if (p[i] != 'O') {
			continue;
		}
		r = ogg_bos_page(p + i, avail - i, &page);
		if (r != 0) {
			break;
		}
	}
	skip = i;
	if (skip > 0) {
		consume(s, skip);
		if (junk(s, skip) != STEP_CONTINUE) {
			return NDS_ERROR;
		}
		p = s->in + s->start;
		avail = s->len;
	}
	if (avail == 0) {
		return NDS_NEED_MORE;
	}
	r = ogg_bos_page(p, avail, &page);
	if (r < 0) {
		if (s->ended || full(s)) {
			consume(s, 1);
			return junk(s, 1);
		}
		return NDS_NEED_MORE;
	}
	packet = p + 27 + p[26];
	if (page < 27 + (size_t)p[26] + 8 || memcmp(packet, "\x01vorbis", 7) != 0) {
		if (page >= 27 + (size_t)p[26] + 8 && memcmp(packet, "OpusHead", 8) == 0) {
			return NDS_UNSUPPORTED;
		}
		if (page >= 27 + (size_t)p[26] + 5 && memcmp(packet, "\x7f" "FLAC", 5) == 0) {
			return NDS_UNSUPPORTED;
		}
		consume(s, page);
		return junk(s, page);
	}
	arena.alloc_buffer = s->vorbis_mem;
	arena.alloc_buffer_length_in_bytes = (int)NDS_VORBIS_ARENA;
	s->vorbis = stb_vorbis_open_pushdata(p, avail > INT_MAX ? INT_MAX : (int)avail, &used, &err, &arena);
	if (s->vorbis == NULL) {
		if (err == VORBIS_need_more_data && !s->ended && !full(s)) {
			return NDS_NEED_MORE;
		}
		consume(s, page);
		return bad_frame(s, page);
	}
	vi = stb_vorbis_get_info(s->vorbis);
	if (vi.channels < 1 || vi.sample_rate == 0) {
		vorbis_close(s);
		consume(s, page);
		return bad_frame(s, page);
	}
	s->hdr_rate = vi.sample_rate;
	s->hdr_channels = (unsigned)vi.channels;
	consume(s, used > 0 ? (size_t)used : page);
	s->bos_scanned = 0;
	return STEP_CONTINUE;
}

static int vorbis_step(nd_stream* s)
{
	const unsigned char* p;
	size_t avail, limit, page = 0;
	float** out = NULL;
	int used, channels = 0, samples = 0, r, k;

	if (s->vorbis == NULL) {
		return vorbis_open(s);
	}
	p = s->in + s->start;
	avail = s->len;
	if (avail == 0) {
		return NDS_NEED_MORE;
	}
	/* A chained stream (Icecast starts one per song) begins again with
	   headers, which stb_vorbis cannot follow: start a new decoder. */
	r = ogg_bos_page(p, avail, &page);
	if (r > 0) {
		vorbis_close(s);
		return STEP_CONTINUE;
	}
	if (r < 0 && !s->ended && !full(s)) {
		return NDS_NEED_MORE;
	}
	limit = ogg_next_bos(s, p, avail);
	used = stb_vorbis_decode_frame_pushdata(s->vorbis, p, limit > INT_MAX ? INT_MAX : (int)limit, &channels, &out, &samples);
	if (used <= 0) {
		if (limit < avail) {
			r = ogg_bos_page(p + limit, avail - limit, &page);
			if (r > 0) {
				/* The old stream has nothing left before the next one. */
				consume(s, limit);
				return junk(s, limit);
			}
			if (r < 0 && !s->ended && !full(s)) {
				return NDS_NEED_MORE;
			}
			/* Not a new stream after all: let the decoder read past it. */
			s->bos_scanned = limit + 1;
			return STEP_CONTINUE;
		}
		if (s->ended) {
			consume(s, s->len);
		}
		return NDS_NEED_MORE;
	}
	if ((size_t)used > limit) {
		used = (int)limit;
	}
	consume(s, (size_t)used);
	if (samples <= 0 || channels < 1 || out == NULL) {
		return junk(s, (size_t)used);
	}
	if (samples > NDS_PCM_MAX) {
		samples = NDS_PCM_MAX;
	}
	for (k = 0; k < samples; k++) {
		float l = out[0][k];
		s->pcm[k * 2] = l;
		s->pcm[k * 2 + 1] = channels >= 2 ? out[1][k] : l;
	}
	got_audio(s, samples, s->hdr_rate, (unsigned)channels);
	return STEP_AUDIO;
}

/* ---- common ---- */

static int step(nd_stream* s)
{
	switch (s->kind) {
	case ND_MP3:
		return mp3_step(s);
	case ND_AAC:
		return aac_step(s);
	case ND_VORBIS:
		return vorbis_step(s);
	default:
		return NDS_ERROR;
	}
}

nd_stream* nd_stream_open(int kind, size_t cap)
{
	nd_stream* s;
	if (kind != ND_MP3 && kind != ND_AAC && kind != ND_VORBIS) {
		return NULL;
	}
	if (cap < NDS_MP3_SYNC_BYTES * 2) {
		cap = NDS_MP3_SYNC_BYTES * 2;
	}
	s = (nd_stream*)calloc(1, sizeof(nd_stream));
	if (s == NULL) {
		return NULL;
	}
	s->kind = kind;
	s->cap = cap;
	if (kind == ND_MP3) {
		s->mp3 = (drmp3dec*)calloc(1, sizeof(drmp3dec));
		if (s->mp3 == NULL) {
			free(s);
			return NULL;
		}
		drmp3dec_init(s->mp3);
	}
	if (kind == ND_VORBIS) {
		s->vorbis_mem = (char*)malloc(NDS_VORBIS_ARENA);
		if (s->vorbis_mem == NULL) {
			free(s);
			return NULL;
		}
	}
	return s;
}

size_t nd_stream_space(const nd_stream* s)
{
	return s->len >= s->cap ? 0 : s->cap - s->len;
}

int nd_stream_feed(nd_stream* s, const unsigned char* p, size_t n)
{
	if (n == 0) {
		return 0;
	}
	if (n > nd_stream_space(s)) {
		return -1;
	}
	if (s->start + s->len + n > s->alloc) {
		if (s->start > 0) {
			memmove(s->in, s->in + s->start, s->len);
			s->start = 0;
		}
		if (s->len + n > s->alloc) {
			size_t size = s->alloc == 0 ? 16384 : s->alloc;
			unsigned char* grown;
			while (size < s->len + n) {
				size *= 2;
			}
			if (size > s->cap) {
				size = s->cap;
			}
			grown = (unsigned char*)realloc(s->in, size + NDS_SLACK);
			if (grown == NULL) {
				return -1;
			}
			s->in = grown;
			s->alloc = size;
		}
	}
	memcpy(s->in + s->start + s->len, p, n);
	s->len += n;
	/* start + len <= alloc, so the slack after the data is allocated. */
	memset(s->in + s->start + s->len, 0, NDS_SLACK);
	return 0;
}

void nd_stream_end(nd_stream* s)
{
	s->ended = 1;
}

int nd_stream_read(nd_stream* s, float* out, int frames, int* got)
{
	int st, n;
	*got = 0;
	if (s->failed) {
		return s->failed;
	}
	while (*got < frames) {
		if (s->pcm_pos < s->pcm_frames) {
			if (*got > 0 && (s->pcm_rate != s->out_rate || s->pcm_channels != s->out_channels)) {
				break;
			}
			s->out_rate = s->pcm_rate;
			s->out_channels = s->pcm_channels;
			n = s->pcm_frames - s->pcm_pos;
			if (n > frames - *got) {
				n = frames - *got;
			}
			memcpy(out + (size_t)*got * 2, s->pcm + (size_t)s->pcm_pos * 2, (size_t)n * 2 * sizeof(float));
			s->pcm_pos += n;
			*got += n;
			continue;
		}
		st = s->in == NULL && s->len == 0 ? NDS_NEED_MORE : step(s);
		if (st == STEP_AUDIO || st == STEP_CONTINUE) {
			continue;
		}
		if (st == NDS_NEED_MORE) {
			if (*got > 0) {
				return NDS_OK;
			}
			return s->ended ? NDS_EOF : NDS_NEED_MORE;
		}
		s->failed = st;
		return *got > 0 ? NDS_OK : st;
	}
	return NDS_OK;
}

void nd_stream_format(const nd_stream* s, unsigned* rate, unsigned* channels)
{
	if (s->out_rate != 0) {
		*rate = s->out_rate;
		*channels = s->out_channels;
	} else if (s->pcm_frames > 0) {
		*rate = s->pcm_rate;
		*channels = s->pcm_channels;
	} else {
		*rate = s->hdr_rate;
		*channels = s->hdr_channels;
	}
}

void nd_stream_reset(nd_stream* s)
{
	s->start = 0;
	s->len = 0;
	s->ended = 0;
	s->failed = 0;
	s->pcm_frames = 0;
	s->pcm_pos = 0;
	s->junk = 0;
	s->bad_frames = 0;
	s->bos_scanned = 0;
	s->mp3_synced = 0;
	s->aac_synced = 0;
	if (s->mp3 != NULL) {
		drmp3dec_init(s->mp3);
	}
	aac_close(s);
	vorbis_close(s);
}

void nd_stream_close(nd_stream* s)
{
	if (s == NULL) {
		return;
	}
	aac_close(s);
	vorbis_close(s);
	free(s->mp3);
	free(s->vorbis_mem);
	free(s->in);
	free(s);
}
