// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

#ifndef ZAPAROO_NATIVEDEC_STREAM_H
#define ZAPAROO_NATIVEDEC_STREAM_H

#include <stddef.h>

/* ND_MP3 and ND_VORBIS come from decoders.h; AAC is stream-only. */
enum { ND_AAC = 4 };

enum {
	NDS_OK = 0,          /* frames were read */
	NDS_NEED_MORE = 1,   /* the buffered input is exhausted */
	NDS_ERROR = 2,       /* the input cannot be decoded */
	NDS_UNSUPPORTED = 3, /* the input is a codec this decoder lacks */
	NDS_EOF = 4          /* nd_stream_end was called and all input is used */
};

typedef struct nd_stream nd_stream;

/* Opens a push decoder for kind (ND_MP3, ND_VORBIS or ND_AAC) that buffers
   at most cap bytes of encoded input. NULL on a bad kind or no memory. */
nd_stream* nd_stream_open(int kind, size_t cap);
/* How many more bytes nd_stream_feed accepts. */
size_t nd_stream_space(const nd_stream* s);
/* Appends n bytes; -1 when they do not fit or memory runs out. */
int nd_stream_feed(nd_stream* s, const unsigned char* p, size_t n);
/* Marks the input finished, so the last buffered bytes decode. */
void nd_stream_end(nd_stream* s);
/* Reads up to frames interleaved stereo float frames into out. *got is how
   many; every frame of one call has the same format. Returns an NDS_ code;
   NDS_OK means *got > 0. */
int nd_stream_read(nd_stream* s, float* out, int frames, int* got);
/* The format of the frames the last read returned, or of the stream's
   headers before then; zeros while unknown. */
void nd_stream_format(const nd_stream* s, unsigned* rate, unsigned* channels);
/* Drops buffered input, decoded audio and decoder state. */
void nd_stream_reset(nd_stream* s);
void nd_stream_close(nd_stream* s);

#endif
