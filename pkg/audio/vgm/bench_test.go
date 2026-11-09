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

package vgm

import (
	"testing"
	"time"
)

// benchSeconds is long enough that a benchmark run never reaches the end.
const benchSeconds = 600

type benchCase struct {
	file *vgmBuilder
	name string
	opts Options
}

// benchCases is one sustained voice per chip family. A single voice is
// the cheapest load a chip sees; chips whose cost grows with active voices
// (sample players) cost more in real files.
func benchCases() []benchCase {
	cases := make([]benchCase, 0, 7+len(chipCases))
	cases = append(cases, []benchCase{
		{name: "YM2612", file: ym2612Note()},
		{name: "YM2612Nuked", file: ym2612Note(), opts: Options{AccurateYM2612: true}},
		{name: "YM2151", file: ym2151Note()},
		{name: "YM3812", file: opl2Note()},
		{name: "OKIM6295", file: okiFile()},
		{name: "SegaPCM", file: segaPCMFile()},
		{name: "DACStream", file: dacStreamFile(false)},
	}...)
	for _, tc := range chipCases {
		b := newVGM(0x171).clock(tc.typ, tc.clock)
		tc.setup(b)
		cases = append(cases, benchCase{name: tc.name, file: b})
	}
	return cases
}

// BenchmarkRender renders one second per iteration and reports how many
// seconds of audio one second of CPU produces.
func BenchmarkRender(b *testing.B) {
	for _, bc := range benchCases() {
		data := bc.file.wait(benchSeconds * oneSecond).bytes()
		b.Run(bc.name, func(b *testing.B) {
			bc.opts.SampleRate = testRate
			d, err := OpenBytes(data, bc.opts)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = d.Close() }()
			buf := make([][2]float64, testRate)
			b.ReportAllocs()
			start := time.Now()
			rendered := 0
			for b.Loop() {
				n, ok := d.Stream(buf)
				rendered += n
				if !ok {
					if err := d.Seek(0); err != nil {
						b.Fatal(err)
					}
				}
			}
			if el := time.Since(start).Seconds(); el > 0 {
				b.ReportMetric(float64(rendered)/testRate/el, "x_realtime")
			}
		})
	}
}
