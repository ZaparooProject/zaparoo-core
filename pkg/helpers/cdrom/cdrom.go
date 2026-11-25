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

// Package cdrom reads optical drives without mounting them: drive status,
// the table of contents, CD-Text and raw sectors. Everything read from a disc
// is untrusted input.
package cdrom

import (
	"errors"
	"hash/fnv"
	"strconv"
)

const (
	// DataSectorSize is the user data in a Mode 1 or Mode 2 Form 1 sector.
	DataSectorSize = 2048
	// RawSectorSize is a full CD sector, and the size of one CD-DA frame.
	RawSectorSize = 2352
	// SectorsPerSecond is the CD frame rate.
	SectorsPerSecond = 75
	// MaxTracks is the most tracks a CD can hold.
	MaxTracks = 99
)

// ErrUnsupported is returned where the platform has no optical drive access.
var ErrUnsupported = errors.New("optical drive access is not supported on this platform")

// Status is the state of a drive's tray and media.
type Status int

const (
	// StatusUnknown means the drive did not say.
	StatusUnknown Status = iota
	StatusNoDisc
	StatusTrayOpen
	StatusNotReady
	StatusDiscOK
)

func (s Status) String() string {
	switch s {
	case StatusNoDisc:
		return "no_disc"
	case StatusTrayOpen:
		return "tray_open"
	case StatusNotReady:
		return "not_ready"
	case StatusDiscOK:
		return "disc_ok"
	default:
		return "unknown"
	}
}

// Track is one TOC entry. End is the LBA of the next track or the lead-out.
type Track struct {
	Number int
	Start  int
	End    int
	Data   bool
}

// Sectors is the track length in sectors.
func (t Track) Sectors() int {
	if t.End <= t.Start {
		return 0
	}
	return t.End - t.Start
}

// TOC is a disc's table of contents.
type TOC struct {
	Tracks  []Track
	LeadOut int
}

// FirstDataTrack returns the first data track, if any.
func (t TOC) FirstDataTrack() (Track, bool) {
	for _, tr := range t.Tracks {
		if tr.Data {
			return tr, true
		}
	}
	return Track{}, false
}

// AudioTracks returns the audio tracks in order.
func (t TOC) AudioTracks() []Track {
	out := make([]Track, 0, len(t.Tracks))
	for _, tr := range t.Tracks {
		if !tr.Data {
			out = append(out, tr)
		}
	}
	return out
}

// Track returns the track with the given number.
func (t TOC) Track(number int) (Track, bool) {
	for _, tr := range t.Tracks {
		if tr.Number == number {
			return tr, true
		}
	}
	return Track{}, false
}

// IsAudio reports whether the disc has tracks and none of them is data.
func (t TOC) IsAudio() bool {
	if len(t.Tracks) == 0 {
		return false
	}
	_, hasData := t.FirstDataTrack()
	return !hasData
}

// Fingerprint identifies a pressing by its track layout. Two copies of the
// same audio CD share it; it is not unique across all discs.
func (t TOC) Fingerprint() string {
	h := fnv.New64a()
	buf := make([]byte, 0, 16*len(t.Tracks)+8)
	buf = strconv.AppendInt(buf, int64(t.LeadOut), 16)
	for _, tr := range t.Tracks {
		buf = append(buf, ':')
		buf = strconv.AppendInt(buf, int64(tr.Start), 16)
		if tr.Data {
			buf = append(buf, 'd')
		}
	}
	_, _ = h.Write(buf)
	return strconv.FormatUint(h.Sum64(), 16)
}

// finishTOC fills in each track's End and checks the layout makes sense.
func finishTOC(tracks []Track, leadOut int) (TOC, error) {
	if len(tracks) == 0 || len(tracks) > MaxTracks {
		return TOC{}, errors.New("table of contents has no usable tracks")
	}
	for i := range tracks {
		end := leadOut
		if i+1 < len(tracks) {
			end = tracks[i+1].Start
		}
		if tracks[i].Start < 0 || end < tracks[i].Start {
			return TOC{}, errors.New("table of contents is out of order")
		}
		tracks[i].End = end
	}
	return TOC{Tracks: tracks, LeadOut: leadOut}, nil
}
