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

import (
	"testing"
)

// fuzzMaxInput and fuzzMaxFrames bound one fuzz iteration: inputs larger
// than a small song add nothing, and rendering is cut off after a quarter
// second so a crafted length cannot stall the fuzzer or exhaust memory.
const (
	fuzzMaxInput  = 64 << 10
	fuzzMaxFrames = rate / 4
)

func renderBounded(d *Decoder) {
	buf := make([][2]float64, 512)
	for done := 0; done < fuzzMaxFrames; {
		n, ok := d.Stream(buf)
		done += n
		if !ok {
			return
		}
	}
}

func FuzzOpenFM(f *testing.F) {
	f.Add(type0Song())
	f.Add(type1Song())
	f.Add(karSong())
	f.Add(rmi(type1Song()))
	f.Add([]byte("MThd\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60MTrk\x00\x00\x00\x04\x00\xff\x2f\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzMaxInput {
			return
		}
		d, err := OpenBytes(data, Options{})
		if err != nil {
			return
		}
		defer func() { _ = d.Close() }()
		if d.Len() < 0 {
			t.Fatalf("negative length %d", d.Len())
		}
		renderBounded(d)
		if err := d.Seek(d.Len() / 2); err != nil {
			t.Fatalf("seek inside the song: %v", err)
		}
		renderBounded(d)
		_ = sanitize([]byte(d.Info().Title))
	})
}

func FuzzRMI(f *testing.F) {
	f.Add(rmi(type0Song()))
	f.Add(rmi(karSong()))
	f.Add([]byte("RIFF\x10\x00\x00\x00RMIDdata\xff\xff\xff\x7fMThd"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzMaxInput {
			return
		}
		for _, lenient := range []bool{false, true} {
			song, err := unwrapRMI(data, lenient)
			if err != nil {
				continue
			}
			if len(song) > len(data) {
				t.Fatalf("unwrapped %d bytes from %d", len(song), len(data))
			}
			if title, err := smfTitle(song); err == nil && len(title) > maxTextBytes {
				t.Fatalf("title of %d bytes", len(title))
			}
		}
		d, err := OpenBytes(data, Options{})
		if err != nil {
			return
		}
		defer func() { _ = d.Close() }()
		renderBounded(d)
	})
}
