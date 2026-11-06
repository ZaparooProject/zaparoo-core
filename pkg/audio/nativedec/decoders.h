// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

#ifndef ZAPAROO_NATIVEDEC_H
#define ZAPAROO_NATIVEDEC_H

#include <stddef.h>

enum { ND_MP3 = 1, ND_FLAC = 2, ND_VORBIS = 3 };

typedef struct nd_decoder nd_decoder;

/* Opens path; NULL when it cannot be decoded. frames is -1 when the length
   is not known. */
nd_decoder* nd_open(const char* path, int kind, unsigned* channels, unsigned* rate, long long* frames);
/* The same from encoded data the decoder takes ownership of: it is freed
   with the decoder, or at once when it cannot be decoded. */
nd_decoder* nd_open_memory(void* data, size_t size, int kind, unsigned* channels, unsigned* rate, long long* frames);
/* Reads up to frames interleaved float frames; returns how many it read,
   0 at the end. */
long long nd_read(nd_decoder* d, float* out, long long frames);
/* After nd_read returned 0: nonzero when the stream failed rather than
   ended. dr_mp3 cannot tell the two apart and reports 0 for both. */
int nd_failed(nd_decoder* d);
int nd_seek(nd_decoder* d, long long frame);
void nd_close(nd_decoder* d);

#endif
