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

// Package trs80cas reads program headers from TRS-80 cassette images.
package trs80cas

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/afero"
)

const (
	scanLimit    = 4096
	syncByte     = 0xA5
	systemMarker = 0x55
	basicMarker  = 0xD3
	systemName   = 6
)

// ErrUnknownFormat is returned when no SYSTEM or BASIC header is found.
var ErrUnknownFormat = errors.New("trs80cas: unknown tape format")

// Kind is the type of program on the tape.
type Kind int

const (
	System Kind = iota + 1
	BASIC
)

// Program is the program header found at the start of a tape.
type Program struct {
	Name string
	Kind Kind
}

// ProgramName reads the program kind and name from the tape image at path.
func ProgramName(fs afero.Fs, path string) (Program, error) {
	f, err := fs.Open(path)
	if err != nil {
		return Program{}, fmt.Errorf("open tape image: %w", err)
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, scanLimit)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Program{}, fmt.Errorf("read tape image: %w", err)
	}
	buf = buf[:n]

	rest := bytes.TrimLeft(buf, "\x00")
	if len(rest) < 2 || rest[0] != syncByte {
		return Program{}, ErrUnknownFormat
	}
	rest = rest[1:]

	switch rest[0] {
	case systemMarker:
		if len(rest) < 1+systemName {
			return Program{}, ErrUnknownFormat
		}
		return Program{Kind: System, Name: cleanName(rest[1 : 1+systemName])}, nil
	case basicMarker:
		if len(rest) < 4 || rest[1] != basicMarker || rest[2] != basicMarker {
			return Program{}, ErrUnknownFormat
		}
		return Program{Kind: BASIC, Name: cleanName(rest[3:4])}, nil
	}
	return Program{}, ErrUnknownFormat
}

func cleanName(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c < 0x20 || c > 0x7E {
			c = '?'
		}
		sb.WriteByte(c)
	}
	return strings.TrimRight(sb.String(), " ")
}
