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
	"time"
)

// benchmarkRender renders a dense eight-channel song one second at a time
// and reports how many seconds of audio one second of CPU produces.
func benchmarkRender(b *testing.B, opts Options) {
	song := denseSong(30)
	d, err := OpenBytes(song, opts)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	buf := make([][2]float64, rate)
	b.ReportAllocs()
	start := time.Now()
	rendered := 0
	for b.Loop() {
		n, ok := d.Stream(buf)
		if !ok {
			if err := d.Seek(0); err != nil {
				b.Fatal(err)
			}
			continue
		}
		rendered += n
	}
	if el := time.Since(start).Seconds(); el > 0 {
		b.ReportMetric(float64(rendered)/float64(rate)/el, "x_realtime")
	}
}

func BenchmarkRenderFM(b *testing.B) {
	benchmarkRender(b, Options{})
}

func BenchmarkRenderSoundFont(b *testing.B) {
	sf := writeTemp(b, "sine.sf2", tinySF2())
	benchmarkRender(b, Options{SoundFontPath: sf})
}
