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
#cgo CPPFLAGS: -I${SRCDIR}/third_party/libadlmidi/include -I${SRCDIR}/third_party/libadlmidi/src
#cgo CPPFLAGS: -DADLMIDI_BUILD -DDISABLE_EMBEDDED_BANKS -DNDEBUG
#cgo CPPFLAGS: -DADLMIDI_DISABLE_NUKED_EMULATOR -DADLMIDI_DISABLE_OPAL_EMULATOR
#cgo CPPFLAGS: -DADLMIDI_DISABLE_JAVA_EMULATOR -DADLMIDI_DISABLE_ESFMU_EMULATOR
#cgo CPPFLAGS: -DADLMIDI_DISABLE_MAME_OPL2_EMULATOR -DADLMIDI_DISABLE_YMFM_EMULATOR
#cgo CFLAGS: -O2
#cgo CXXFLAGS: -O2 -std=c++11 -fno-rtti -w
#cgo linux LDFLAGS: -lm
#include <stdlib.h>
#include "adlmidi.h"

static int zm_adl_render(struct ADL_MIDIPlayer* p, float* out, int frames, int sequenced)
{
	struct ADLMIDI_AudioFormat f;
	f.type = ADLMIDI_SampleType_F32;
	f.containerSize = sizeof(float);
	f.sampleOffset = 2 * sizeof(float);
	if (sequenced)
		return adl_playFormat(p, frames * 2, (ADL_UInt8*)out, (ADL_UInt8*)(out + 1), &f) / 2;
	return adl_generateFormat(p, frames * 2, (ADL_UInt8*)out, (ADL_UInt8*)(out + 1), &f) / 2;
}
*/
import "C"

import (
	_ "embed"
	"errors"
	"fmt"
	"unsafe"
)

// fmBank is the General MIDI instrument bank the FM synthesizer plays:
// DMXOPL3 by Shannon Freeman (MIT), from the libADLMIDI distribution.
//
//go:embed third_party/libadlmidi/banks/DMXOPL3-by-sneakernets-GS.wopl
var fmBank []byte

type fmRenderer struct {
	p *C.struct_ADL_MIDIPlayer
	// title is the song title libADLMIDI found, if any.
	title string
	rate  int
	// ended is set once the sequencer has played the last event; from then
	// on the chips run on without it to render the release tail.
	ended bool
}

func adlError(p *C.struct_ADL_MIDIPlayer) string {
	if s := C.adl_errorInfo(p); s != nil {
		if msg := sanitize([]byte(C.GoString(s))); msg != "" {
			return msg
		}
	}
	return "unknown error"
}

// openFM loads song into a new FM synthesizer and returns it with the
// stream length in frames.
func openFM(song []byte, rate int) (*fmRenderer, int, error) {
	p := C.adl_init(C.long(rate))
	if p == nil {
		return nil, 0, errors.New("midi: cannot start FM synthesizer")
	}
	ok := false
	defer func() {
		if !ok {
			C.adl_close(p)
		}
	}()
	if C.adl_switchEmulator(p, C.ADLMIDI_EMU_DOSBOX) != 0 {
		return nil, 0, fmt.Errorf("midi: FM emulator: %s", adlError(p))
	}
	bank := C.CBytes(fmBank)
	rc := C.adl_openBankData(p, bank, C.ulong(len(fmBank)))
	C.free(bank)
	if rc != 0 {
		return nil, 0, fmt.Errorf("midi: FM bank: %s", adlError(p))
	}
	C.adl_setLoopEnabled(p, 0)
	cdata := C.CBytes(song)
	rc = C.adl_openData(p, cdata, C.ulong(len(song)))
	C.free(cdata)
	if rc != 0 {
		return nil, 0, fmt.Errorf("midi: %s", adlError(p))
	}
	var title string
	if s := C.adl_metaMusicTitle(p); s != nil {
		title = sanitize([]byte(C.GoString(s)))
	}
	frames := songFrames(float64(C.adl_totalTimeLength(p)), rate)
	ok = true
	return &fmRenderer{p: p, rate: rate, title: title}, frames, nil
}

func (r *fmRenderer) render(out []float32, frames int) {
	done := 0
	for done < frames && !r.ended {
		n := int(C.zm_adl_render(r.p, (*C.float)(unsafe.Pointer(&out[done*2])), C.int(frames-done), 1))
		if n <= 0 {
			r.ended = true
			break
		}
		done += n
	}
	for done < frames {
		n := int(C.zm_adl_render(r.p, (*C.float)(unsafe.Pointer(&out[done*2])), C.int(frames-done), 0))
		if n <= 0 {
			clear(out[done*2 : frames*2])
			return
		}
		done += n
	}
}

func (r *fmRenderer) seek(frame int) {
	// adl_positionSeek silences the sounding notes itself. A target past
	// the last event makes the sequencer rewind to the start instead, so
	// that case leaves the sequencer behind and lets the chips run on, as
	// at the natural end of the song.
	seconds := float64(frame) / float64(r.rate)
	C.adl_positionSeek(r.p, C.double(seconds))
	r.ended = C.adl_atEnd(r.p) != 0 || (frame > 0 && float64(C.adl_positionTell(r.p)) < seconds/2)
}

func (r *fmRenderer) close() {
	C.adl_close(r.p)
	r.p = nil
}
