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

package gme

import "testing"

const fuzzMaxBytes = 0x10200 + 1024

func fuzzOpen(f *testing.F, seeds ...[]byte) {
	for _, s := range seeds {
		f.Add(s, 0)
	}
	f.Fuzz(func(_ *testing.T, data []byte, track int) {
		if len(data) > fuzzMaxBytes {
			return
		}
		d, err := OpenBytes(data, "", track%4, Options{SampleRate: 8000, DefaultTrackSeconds: 1, FadeSeconds: 1})
		if err != nil {
			return
		}
		defer func() { _ = d.Close() }()
		buf := make([][2]float64, 512)
		for range 4 { // rate/4 frames at most
			if _, ok := d.Stream(buf); !ok {
				break
			}
		}
		_ = d.Seek(d.Len() / 2)
		_, _ = d.Stream(buf)
	})
}

func FuzzOpenNSF(f *testing.F) {
	fuzzOpen(f, nsfFile("G", "A", "C", 0xFD, 0x7E),
		nsfeFile([]byte{0xFD, 0x7E}, []string{"One", "Two"}, []int32{2000, 3000}))
}

func FuzzOpenGBS(f *testing.F) { fuzzOpen(f, gbsFile(0x00, 0x80)) }

func FuzzOpenHES(f *testing.F) { fuzzOpen(f, hesFile()) }

func FuzzOpenKSS(f *testing.F) { fuzzOpen(f, kssFile()) }

func FuzzOpenSPC(f *testing.F) { fuzzOpen(f, spcFile()) }
