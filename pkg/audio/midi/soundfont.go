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

package midi

/*
#include <stdlib.h>
#include "sfsynth.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"unsafe"

	// stb_vorbis, which decodes SF3 samples, is compiled in nativedec.
	_ "github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
)

type sfRenderer struct {
	s *C.zm_song
}

// openSoundFont loads opts.SoundFontPath and binds song to it. Errors
// loading the SoundFont wrap ErrSoundFont; an error parsing the song does
// not, so the caller can tell a bad SoundFont from a song TinyMidiLoader
// cannot read.
func openSoundFont(song []byte, opts Options) (renderer, int, error) {
	sf, err := loadSoundFont(opts.SoundFontPath, opts.SoundFontMemory)
	if err != nil {
		return nil, 0, err
	}
	cdata := C.CBytes(song)
	defer C.free(cdata)
	var lengthMS C.uint
	s := C.zm_song_open(sf, cdata, C.int(len(song)), C.int(opts.SampleRate),
		C.int(opts.MaxVoices), &lengthMS)
	if s == nil {
		C.zm_sf_close(sf)
		return nil, 0, errors.New("midi: TinyMidiLoader cannot parse song")
	}
	return &sfRenderer{s: s}, songFrames(float64(lengthMS)/1000+tailSeconds, opts.SampleRate), nil
}

// loadSoundFont reads a SoundFont straight into C memory, so a large one
// is not held twice, and parses it. The file and everything parsing it
// allocates stay within budget bytes.
func loadSoundFont(path string, budget int64) (*C.zm_sf, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from the user's settings
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSoundFont, err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSoundFont, err)
	}
	size := st.Size()
	if !st.Mode().IsRegular() || size <= 0 {
		return nil, fmt.Errorf("%w: %s is not a regular, non-empty file", ErrSoundFont, path)
	}
	if limit := min(MaxSoundFontBytes, budget); size > limit {
		return nil, fmt.Errorf("%w: %d bytes is over the %d byte limit", ErrSoundFont, size, limit)
	}
	mem := C.malloc(C.size_t(size))
	if mem == nil {
		return nil, fmt.Errorf("%w: out of memory", ErrSoundFont)
	}
	defer C.free(mem)
	buf := unsafe.Slice((*byte)(mem), int(size))
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSoundFont, err)
	}
	sf := C.zm_sf_load(mem, C.int(size), C.longlong(budget-size))
	if sf == nil {
		return nil, fmt.Errorf("%w: %s is not a usable SF2 or SF3 file, or decodes to more than %d bytes",
			ErrSoundFont, path, budget)
	}
	return sf, nil
}

func (r *sfRenderer) render(out []float32, frames int) {
	C.zm_song_render(r.s, (*C.float)(unsafe.Pointer(&out[0])), C.int(frames))
}

func (r *sfRenderer) seek(frame int) {
	C.zm_song_seek(r.s, C.longlong(frame))
}

func (r *sfRenderer) close() {
	C.zm_song_close(r.s)
	r.s = nil
}
