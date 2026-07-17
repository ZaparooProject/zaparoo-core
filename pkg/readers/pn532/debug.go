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

package pn532

import (
	"strings"
	"sync"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/rs/zerolog/log"
)

// debugWriterOnce ensures the go-pn532 debug writer is only installed once,
// even if multiple PN532 readers are opened.
var debugWriterOnce sync.Once

// zerologDebugWriter forwards go-pn532's always-on debug output into Core's
// zerolog logger at debug level. go-pn532 writes the exact bytes it reads from
// tags (e.g. "NTAG readNDEFHeader: user data = [...]") through this path, which
// is otherwise never captured because Core creates no session log file.
type zerologDebugWriter struct{}

func (zerologDebugWriter) Write(p []byte) (int, error) {
	for line := range strings.Lines(string(p)) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		log.Debug().Str("src", "pn532").Msg(line)
	}
	return len(p), nil
}

// enablePN532DebugLogging routes go-pn532 debug output into Core's logger when
// debug logging is enabled in the config. This is how raw NDEF/TLV byte dumps
// reach Core's log for diagnosing tag read failures.
func enablePN532DebugLogging() {
	debugWriterOnce.Do(func() {
		pn532.SetDebugWriter(zerologDebugWriter{})
	})
}
