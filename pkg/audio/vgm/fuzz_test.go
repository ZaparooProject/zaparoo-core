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
	"math"
	"testing"
	"unicode"
	"unicode/utf8"
)

const (
	maxFuzzInput  = 64 << 10
	maxFuzzFrames = 2048
)

func FuzzOpen(f *testing.F) {
	seeds := []*vgmBuilder{
		snTone(0, 1), ym2612Note(), ym2151Note(), opl2Note(), ayTone(), okiFile(),
		segaPCMFile(), dacStreamFile(false), dacStreamFile(true), loopFile(),
	}
	for _, b := range seeds {
		f.Add(b.bytes())
	}
	for _, tc := range chipCases {
		b := newVGM(0x171).clock(tc.typ, tc.clock)
		tc.setup(b)
		if p := b.wait(oneSecond / 100).bytes(); len(p) <= maxFuzzInput {
			f.Add(p)
		}
	}
	f.Add(mixedFile())
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
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
		buf := make([][2]float64, 512)
		rendered := 0
		for rendered < maxFuzzFrames {
			n, ok := d.Stream(buf)
			for _, s := range buf[:n] {
				for _, v := range s {
					if math.IsNaN(v) || v < -1 || v > 1 {
						t.Fatalf("sample %v out of range", v)
					}
				}
			}
			rendered += n
			if !ok {
				break
			}
		}
		if err := d.Seek(min(d.Len(), 300)); err != nil {
			t.Fatalf("seek: %v", err)
		}
		_, _ = d.Stream(buf)
	})
}

func FuzzGD3(f *testing.F) {
	f.Add(newVGM(0x171).setGD3("Title", "タイトル", "Game", "", "System", "", "Author", "", "1994/03/01").bytes())
	f.Add([]byte("Gd3 \x00\x01\x00\x00\x04\x00\x00\x00A\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		if i := indexGD3(data); i >= 0 {
			data = data[i:]
		}
		info, err := parseGD3(data)
		if err != nil {
			return
		}
		for _, s := range []string{
			info.Title, info.Game, info.System, info.Author, info.Date, info.Year, info.Notes, info.TrackTitle,
		} {
			if !utf8.ValidString(s) {
				t.Fatalf("invalid UTF-8 %q", s)
			}
			if len(s) > maxTextBytes {
				t.Fatalf("field is %d bytes", len(s))
			}
			for _, r := range s {
				if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
					t.Fatalf("control character in %q", s)
				}
			}
		}
		if info.TrackTitle != info.Title {
			t.Fatalf("TrackTitle %q != Title %q", info.TrackTitle, info.Title)
		}
	})
}

// indexGD3 finds the GD3 magic so whole-file seeds exercise the tag parser.
func indexGD3(b []byte) int {
	for i := 0; i+4 <= len(b); i++ {
		if string(b[i:i+4]) == "Gd3 " {
			return i
		}
	}
	return -1
}
