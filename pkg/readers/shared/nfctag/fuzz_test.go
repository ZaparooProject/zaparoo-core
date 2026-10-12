/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package nfctag_test

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag/tagsim"
)

// Tag memory is whatever was last written by anyone, so every byte of it is
// untrusted.
func fuzzSeeds(f *testing.F) {
	f.Helper()
	f.Add(textTLV("@NES/Lode Runner"))
	f.Add(textTLV(""))
	f.Add([]byte{0x03, 0xFF, 0xFF, 0xFF, 0xD1, 0x01})
	f.Add([]byte{0x01, 0x03, 0xA0, 0x0C, 0x34, 0x03, 0x13, 0xD1, 0x01, 0x0F, 0x54, 0x02, 0x65, 0x6E})
	f.Add([]byte{0x03, 0x05, 0xD1, 0x01, 0x01, 0x55, 0xFF, 0xFE})
	f.Add([]byte{0xFE})
	f.Add([]byte{})
}

func FuzzTextFromMemory(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, mem []byte) {
		text := nfctag.TextFromMemory(mem)
		if len(text) > len(mem)*8+256 {
			t.Fatalf("%d bytes of memory became %d bytes of text", len(mem), len(text))
		}
		_ = utf8.ValidString(text)
	})
}

func FuzzReadType2(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, user []byte) {
		sim := tagsim.NewType2(tagsim.PagesNTAG215)
		sim.SetUser(user)

		res, err := nfctag.Read(context.Background(), type2Tag(sim))
		if err != nil {
			t.Fatalf("an in-memory tag cannot fail to read: %v", err)
		}
		// 504 bytes of user memory, read in whole blocks.
		if len(res.Data) > 512 {
			t.Fatalf("read %d bytes from a tag with 504 of user memory", len(res.Data))
		}
		if sim.Reads > 40 {
			t.Fatalf("%d reads of a 135 page tag", sim.Reads)
		}
	})
}

func FuzzReadClassic(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		sim := tagsim.NewClassic(16)
		sim.Format()
		sim.SetData(data)

		res, err := nfctag.Read(context.Background(), classicTag(sim, nfctag.KindClassic1K))
		if err != nil {
			t.Fatalf("an in-memory tag cannot fail to read: %v", err)
		}
		if len(res.Data) > 720 {
			t.Fatalf("read %d bytes from a tag with 720 of data", len(res.Data))
		}
	})
}
