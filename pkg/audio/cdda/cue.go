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

// Package cdda plays CD audio: rips described by a cue sheet and discs in an
// optical drive. Both carry raw 16-bit little-endian stereo PCM at 44.1 kHz
// in 2352-byte sectors.
package cdda

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// SampleRate is the CD audio sample rate.
	SampleRate = 44100
	// FramesPerSector is how many stereo frames one raw sector holds.
	FramesPerSector = 588
	// SectorSize is the size of one raw sector.
	SectorSize = 2352
	// bytesPerFrame is one 16-bit stereo frame.
	bytesPerFrame = 4

	// MaxCueBytes bounds the size of a cue sheet.
	MaxCueBytes = 256 << 10
	// MaxTracks bounds the tracks of a cue sheet, as on a disc.
	MaxTracks = 99
	// MaxFiles bounds the FILE entries of a cue sheet.
	MaxFiles = 99
	// maxLineBytes bounds one line of a cue sheet.
	maxLineBytes = 4096
	// maxMinutes bounds an index time. A disc holds at most about 80
	// minutes; a rip file can be no longer than that.
	maxMinutes = 999
)

// ErrMalformedCue is returned for a cue sheet that cannot be read.
var ErrMalformedCue = errors.New("malformed cue sheet")

// Sheet is a parsed cue sheet.
type Sheet struct {
	Title     string
	Performer string
	Files     []CueFile
}

// CueFile is one FILE entry and the tracks stored in it.
type CueFile struct {
	Name   string
	Tracks []CueTrack
	// Binary is false for a file type other than BINARY, whose tracks
	// cannot be played.
	Binary bool
}

// CueTrack is one TRACK entry. Index positions are in sectors from the start
// of the file; Index0 is -1 when the track has no pregap index.
type CueTrack struct {
	Title     string
	Performer string
	Number    int
	Index0    int
	Index1    int
	Audio     bool
}

func malformed(line int, format string, args ...any) error {
	return fmt.Errorf("%w: line %d: %s", ErrMalformedCue, line, fmt.Sprintf(format, args...))
}

// ParseCue parses a cue sheet. Only raw 2352-byte sectors are accepted;
// commands it does not use, such as REM, are skipped.
func ParseCue(data []byte) (*Sheet, error) {
	if len(data) > MaxCueBytes {
		return nil, fmt.Errorf("%w: larger than %d bytes", ErrMalformedCue, MaxCueBytes)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	sheet := &Sheet{}
	var (
		file      *CueFile
		track     *CueTrack
		tracks    int
		lastTrack int
	)
	for lineNo := 1; len(data) > 0; lineNo++ {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			line, data = data, nil
		}
		if len(line) > maxLineBytes {
			return nil, malformed(lineNo, "line longer than %d bytes", maxLineBytes)
		}
		fields := splitCueLine(string(bytes.TrimSpace(line)))
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "FILE":
			if len(sheet.Files) >= MaxFiles {
				return nil, malformed(lineNo, "more than %d files", MaxFiles)
			}
			if len(fields) < 3 {
				return nil, malformed(lineNo, "FILE needs a name and a type")
			}
			if err := endFile(file, lineNo); err != nil {
				return nil, err
			}
			name := strings.Join(fields[1:len(fields)-1], " ")
			if name == "" {
				return nil, malformed(lineNo, "FILE has an empty name")
			}
			sheet.Files = append(sheet.Files, CueFile{
				Name:   name,
				Binary: strings.EqualFold(fields[len(fields)-1], "BINARY"),
			})
			file = &sheet.Files[len(sheet.Files)-1]
			track = nil
		case "TRACK":
			if file == nil {
				return nil, malformed(lineNo, "TRACK before FILE")
			}
			if len(fields) != 3 {
				return nil, malformed(lineNo, "TRACK needs a number and a mode")
			}
			if tracks >= MaxTracks {
				return nil, malformed(lineNo, "more than %d tracks", MaxTracks)
			}
			n, err := strconv.Atoi(fields[1])
			if err != nil || n < 1 || n > MaxTracks {
				return nil, malformed(lineNo, "bad track number %q", fields[1])
			}
			if n <= lastTrack {
				return nil, malformed(lineNo, "track %d out of order", n)
			}
			audio, err := trackMode(fields[2])
			if err != nil {
				return nil, malformed(lineNo, "%v", err)
			}
			if err := endTrack(track, lineNo); err != nil {
				return nil, err
			}
			file.Tracks = append(file.Tracks, CueTrack{Number: n, Audio: audio, Index0: -1, Index1: -1})
			track = &file.Tracks[len(file.Tracks)-1]
			tracks++
			lastTrack = n
		case "INDEX":
			if track == nil {
				return nil, malformed(lineNo, "INDEX before TRACK")
			}
			if len(fields) != 3 {
				return nil, malformed(lineNo, "INDEX needs a number and a time")
			}
			if err := setIndex(track, fields[1], fields[2]); err != nil {
				return nil, malformed(lineNo, "%v", err)
			}
		case "TITLE", "PERFORMER":
			if len(fields) < 2 {
				continue
			}
			text := cueText(strings.Join(fields[1:], " "))
			isTitle := strings.EqualFold(fields[0], "TITLE")
			switch {
			case track != nil && isTitle:
				track.Title = text
			case track != nil:
				track.Performer = text
			case isTitle:
				sheet.Title = text
			default:
				sheet.Performer = text
			}
		}
	}
	if err := endFile(file, 0); err != nil {
		return nil, err
	}
	if tracks == 0 {
		return nil, fmt.Errorf("%w: no tracks", ErrMalformedCue)
	}
	return sheet, nil
}

// trackMode reports whether a TRACK mode is audio. Only modes with raw
// 2352-byte sectors are accepted, so that every index maps to a byte offset
// the same way.
func trackMode(mode string) (audio bool, err error) {
	switch strings.ToUpper(mode) {
	case "AUDIO":
		return true, nil
	case "MODE1/2352", "MODE2/2352":
		return false, nil
	default:
		return false, fmt.Errorf("unsupported track mode %q", mode)
	}
}

func setIndex(track *CueTrack, num, at string) error {
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 || n > 99 {
		return fmt.Errorf("bad index number %q", num)
	}
	sector, err := parseMSF(at)
	if err != nil {
		return err
	}
	switch n {
	case 0:
		if track.Index0 >= 0 || track.Index1 >= 0 {
			return errors.New("INDEX 00 repeated or after INDEX 01")
		}
		track.Index0 = sector
	case 1:
		if track.Index1 >= 0 {
			return errors.New("INDEX 01 repeated")
		}
		if track.Index0 > sector {
			return errors.New("INDEX 01 before INDEX 00")
		}
		track.Index1 = sector
	default:
		// Later indexes only mark points inside the track.
		if track.Index1 < 0 || sector < track.Index1 {
			return fmt.Errorf("INDEX %02d before INDEX 01", n)
		}
	}
	return nil
}

func endTrack(track *CueTrack, lineNo int) error {
	if track != nil && track.Index1 < 0 {
		return malformed(lineNo, "track %d has no INDEX 01", track.Number)
	}
	return nil
}

// endFile checks the last track of a file and that the file's tracks start
// in order.
func endFile(file *CueFile, lineNo int) error {
	if file == nil {
		return nil
	}
	if len(file.Tracks) > 0 {
		if err := endTrack(&file.Tracks[len(file.Tracks)-1], lineNo); err != nil {
			return err
		}
	}
	prev := 0
	for i := range file.Tracks {
		t := &file.Tracks[i]
		first := t.Index1
		if t.Index0 >= 0 {
			first = t.Index0
		}
		if first < prev {
			return malformed(lineNo, "track %d starts before the track ahead of it", t.Number)
		}
		prev = t.Index1
	}
	return nil
}

// parseMSF reads an mm:ss:ff time as a sector count.
func parseMSF(s string) (int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("bad time %q", s)
	}
	limits := [3]int{maxMinutes, 59, 74}
	var v [3]int
	for i, p := range parts {
		if p == "" || len(p) > 3 {
			return 0, fmt.Errorf("bad time %q", s)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > limits[i] || strings.ContainsAny(p, "+-") {
			return 0, fmt.Errorf("bad time %q", s)
		}
		v[i] = n
	}
	return (v[0]*60+v[1])*75 + v[2], nil
}

// splitCueLine splits a line into fields, keeping a double-quoted field
// whole. An unterminated quote runs to the end of the line.
func splitCueLine(line string) []string {
	var fields []string
	for {
		line = strings.TrimLeft(line, " \t")
		if line == "" {
			return fields
		}
		if line[0] == '"' {
			end := strings.IndexByte(line[1:], '"')
			if end < 0 {
				return append(fields, line[1:])
			}
			fields = append(fields, line[1:1+end])
			line = line[2+end:]
			continue
		}
		end := strings.IndexAny(line, " \t")
		if end < 0 {
			return append(fields, line)
		}
		fields = append(fields, line[:end])
		line = line[end:]
	}
}

// cueText decodes a cue text field. Sheets written by older rippers are
// often Latin-1 rather than UTF-8; control characters are dropped.
func cueText(s string) string {
	if !utf8.ValidString(s) {
		var b strings.Builder
		b.Grow(len(s) * 2)
		for i := range len(s) {
			b.WriteRune(rune(s[i]))
		}
		s = b.String()
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
}
