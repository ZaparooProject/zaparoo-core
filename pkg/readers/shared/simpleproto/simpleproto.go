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

// Package simpleproto implements the "simple serial" line protocol shared by
// every reader that streams newline-delimited SCAN lines, whatever the
// transport underneath (serial port, Bluetooth LE, ...).
package simpleproto

import (
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

const (
	// scanPrefix starts every line that carries a token.
	scanPrefix = "SCAN\t"

	// DefaultMaxLineLength bounds one line of input. Anything longer is
	// discarded up to the next newline so a misbehaving device cannot grow
	// memory without bound.
	DefaultMaxLineLength = 4096
)

// Line is one parsed protocol line. Removable is set only when the line
// carried a removable= argument, so the caller can keep its previous value
// otherwise.
type Line struct {
	Token     *tokens.Token
	Removable *bool
}

// ParseLine parses one line of the protocol. It reports false for blank
// lines and for lines that do not carry a token, which are not errors: the
// stream may contain chatter the reader does not understand.
func ParseLine(line, readerID string) (Line, bool) {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "\r")

	if line == "" || !strings.HasPrefix(line, scanPrefix) {
		return Line{}, false
	}

	args := line[len(scanPrefix):]
	if args == "" {
		return Line{}, false
	}

	t := tokens.Token{
		Data:     line,
		ScanTime: time.Now(),
		Source:   tokens.SourceReader,
		ReaderID: readerID,
	}

	var removable *bool
	hasArg := false
	for arg := range strings.SplitSeq(args, "\t") {
		arg = strings.TrimSpace(arg)
		switch {
		case strings.HasPrefix(arg, "uid="):
			t.UID = strings.TrimPrefix(arg, "uid=")
			hasArg = true
		case strings.HasPrefix(arg, "text="):
			t.Text = strings.TrimPrefix(arg, "text=")
			hasArg = true
		case strings.HasPrefix(arg, "removable="):
			value := strings.TrimPrefix(arg, "removable=") != "no"
			removable = &value
			hasArg = true
		}
	}

	// Without any named argument the whole payload is the token text.
	if !hasArg {
		t.Text = args
	}

	return Line{Token: &t, Removable: removable}, true
}

// LineSplitter turns an arbitrary byte stream into complete lines. It keeps
// a partial line between calls and drops any line longer than the limit.
type LineSplitter struct {
	buf      []byte
	maxLine  int
	dropping bool
}

// NewLineSplitter returns a splitter with the given line limit; a limit of
// zero or less uses DefaultMaxLineLength.
func NewLineSplitter(maxLine int) *LineSplitter {
	if maxLine <= 0 {
		maxLine = DefaultMaxLineLength
	}
	return &LineSplitter{maxLine: maxLine}
}

// Feed appends data to the stream and returns every line completed by it,
// without their trailing newline. Carriage returns are left for ParseLine.
func (s *LineSplitter) Feed(data []byte) []string {
	var lines []string
	for _, b := range data {
		if b == '\n' {
			if !s.dropping {
				lines = append(lines, string(s.buf))
			}
			s.buf = s.buf[:0]
			s.dropping = false
			continue
		}
		if s.dropping {
			continue
		}
		if len(s.buf) >= s.maxLine {
			s.buf = s.buf[:0]
			s.dropping = true
			continue
		}
		s.buf = append(s.buf, b)
	}
	return lines
}
