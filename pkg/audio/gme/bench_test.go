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

import (
	"testing"
	"time"
)

const benchRate = 48000

// benchmarkRender plays a fixture one second at a time and reports how
// many seconds of audio one second of CPU produces. Tracks are 5 seconds
// and restart at the end, so the SPC fixture's tone never decays into the
// silence that Game_Music_Emu stops emulating. The fixtures are minimal
// programs: the CPU idles, so NSF, GBS, HES and KSS figures are upper
// bounds; SPC cost is mostly the DSP and does not depend on the program.
func benchmarkRender(b *testing.B, data []byte, ext string) {
	d, err := OpenBytes(data, ext, 0, Options{SampleRate: benchRate, DefaultTrackSeconds: 5, FadeSeconds: 1})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	buf := make([][2]float64, benchRate)
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
		b.ReportMetric(float64(rendered)/benchRate/el, "x_realtime")
	}
}

func BenchmarkRenderNSF(b *testing.B) {
	benchmarkRender(b, nsfFile("G", "A", "C", 0xFD, 0x7E), ".nsf")
}

func BenchmarkRenderGBS(b *testing.B) { benchmarkRender(b, gbsFile(0x00, 0x80), ".gbs") }

func BenchmarkRenderHES(b *testing.B) { benchmarkRender(b, hesFile(), ".hes") }

func BenchmarkRenderKSS(b *testing.B) { benchmarkRender(b, kssFile(), ".kss") }

func BenchmarkRenderSPC(b *testing.B) { benchmarkRender(b, spcFile(), ".spc") }
