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

package zapscript

import (
	"fmt"
	"strings"
	"testing"
)

// benchScriptLengths spans an ordinary tag up to the ingest bound. Redaction
// runs twice per scanned token on the service worker and once per row on
// every history read, so its cost has to stay proportional to the text rather
// than to a parse of it.
var benchScriptLengths = []int{64, 512, 4096, MaxScriptLength}

// BenchmarkRedactToken_NoCredential covers the common path: a token that
// names no credential-bearing command never reaches the parser.
func BenchmarkRedactToken_NoCredential(b *testing.B) {
	for _, size := range benchScriptLengths {
		script := "**launch:/games/snes/" + strings.Repeat("a", size-len("**launch:/games/snes/"))
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				text, data := RedactToken(script, "deadbeef")
				_, _ = text, data
			}
		})
	}
}

// BenchmarkRedactToken_MixedCase uses a realistic media path. Real paths
// carry capitals, which is the case a case-folding pre-check has to handle
// without copying the whole script.
func BenchmarkRedactToken_MixedCase(b *testing.B) {
	for _, size := range benchScriptLengths {
		prefix := "**launch:/media/fat/games/SNES/Super Metroid "
		script := prefix + strings.Repeat("Ab", (size-len(prefix))/2)
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				text, data := RedactToken(script, "deadbeef")
				_, _ = text, data
			}
		})
	}
}

// BenchmarkRedactToken_ProfileCard covers the path that still parses, so a
// regression there stays visible.
func BenchmarkRedactToken_ProfileCard(b *testing.B) {
	script := "**profile:sw-7f3a9c21"
	b.ReportAllocs()
	for b.Loop() {
		text, data := RedactToken(script, "deadbeef")
		_, _ = text, data
	}
}

// BenchmarkRedactToken_HistoryPage is the read-path regression guard. A page
// of history is 25 rows and every row is redacted on the way out.
func BenchmarkRedactToken_HistoryPage(b *testing.B) {
	const pageSize = 25

	for _, size := range benchScriptLengths {
		rows := make([]string, pageSize)
		for i := range rows {
			rows[i] = fmt.Sprintf("**launch:/games/snes/%d", i) +
				strings.Repeat("a", size-len("**launch:/games/snes/0"))
		}
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for _, row := range rows {
					text, data := RedactToken(row, "deadbeef")
					_, _ = text, data
				}
			}
		})
	}
}
