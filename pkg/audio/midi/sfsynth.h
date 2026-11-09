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

#ifndef ZAPAROO_MIDI_SFSYNTH_H
#define ZAPAROO_MIDI_SFSYNTH_H

typedef struct zm_sf zm_sf;
typedef struct zm_song zm_song;

/* Loads a SoundFont (SF2, or SF3 with Ogg Vorbis samples) from memory. The
   data is copied; the caller keeps ownership. NULL on failure. */
zm_sf* zm_sf_load(const void* data, int size, long long budget);
void zm_sf_close(zm_sf* sf);

/* Parses a Standard MIDI File and binds it to a SoundFont, which the song
   then owns. On failure NULL is returned and the SoundFont is left to the
   caller. length_ms receives the song length. */
zm_song* zm_song_open(zm_sf* sf, const void* data, int size, int rate,
	int max_voices, unsigned int* length_ms);

/* Renders frames stereo interleaved float frames. */
void zm_song_render(zm_song* s, float* out, int frames);

/* Moves playback to frame: all sound stops, then every message before the
   target is replayed except note-ons, so programs, controllers and pitch
   bends match what the song would have set. */
void zm_song_seek(zm_song* s, long long frame);

/* Frees the song and its SoundFont. */
void zm_song_close(zm_song* s);

#endif
