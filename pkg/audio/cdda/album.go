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

package cdda

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/trackmeta"
	"github.com/spf13/afero"
)

// ErrNoAudioTracks is returned for a sheet or disc with nothing to play.
var ErrNoAudioTracks = errors.New("no audio tracks")

// ErrBadFileName is returned for a FILE entry that names a file outside the
// cue sheet's folder.
var ErrBadFileName = errors.New("cue FILE must name a file beside the sheet")

// maxTextBytes bounds one title or artist.
const maxTextBytes = 512

// Track is one playable audio track. Start and End are frame offsets within
// its source: a rip file, or the disc from LBA 0.
type Track struct {
	Title     string
	Performer string
	Number    int
	source    int
	Start     int64
	End       int64
}

// Frames is the track's length in frames.
func (t *Track) Frames() int64 { return t.End - t.Start }

// Album is the audio tracks of a cue sheet or disc, in order.
type Album struct {
	Title     string
	Performer string
	Tracks    []Track
	// Total is the highest audio track number, so numbering follows the
	// sheet or disc even when a data track comes first.
	Total int
	// HasArt reports an image beside the cue sheet.
	HasArt bool
}

// TrackMeta is the tags of track i.
func (a *Album) TrackMeta(i int) *trackmeta.Meta {
	if i < 0 || i >= len(a.Tracks) {
		return nil
	}
	t := &a.Tracks[i]
	return &trackmeta.Meta{
		Title:       t.Title,
		Artist:      t.Performer,
		Album:       a.Title,
		AlbumArtist: a.Performer,
		TrackNumber: t.Number,
		TrackTotal:  a.Total,
		HasArt:      a.HasArt,
	}
}

// only narrows the album to the track numbered n.
func (a *Album) only(n int) error {
	for i := range a.Tracks {
		if a.Tracks[i].Number == n {
			a.Tracks = a.Tracks[i : i+1]
			return nil
		}
	}
	return fmt.Errorf("no audio track %d", n)
}

func clipText(s string) string {
	if len(s) <= maxTextBytes {
		return s
	}
	cut := maxTextBytes
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8RuneStart(b byte) bool { return b&0xc0 != 0x80 }

// ValidateFileName checks a cue FILE name: it must be a relative path that
// stays inside the sheet's folder.
func ValidateFileName(name string) error {
	if name == "" || strings.ContainsAny(name, "\\\x00:") || path.IsAbs(name) || filepath.IsAbs(name) {
		return fmt.Errorf("%w: %q", ErrBadFileName, name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("%w: %q", ErrBadFileName, name)
		}
	}
	return nil
}

// resolvedFile is one rip file of an album.
type resolvedFile struct {
	path   string
	frames int64
}

// resolveCue checks every FILE of a sheet and lays out its audio tracks. A
// track ends at the next track's INDEX 01 in the same file, or at the end of
// the file. Data tracks are skipped, and an audio track followed by one ends
// at its pregap.
func resolveCue(fsys afero.Fs, cuePath string, sheet *Sheet) (*Album, []resolvedFile, error) {
	dir := filepath.Dir(cuePath)
	album := &Album{Title: clipText(sheet.Title), Performer: clipText(sheet.Performer)}
	var files []resolvedFile
	for fi := range sheet.Files {
		f := &sheet.Files[fi]
		if !hasAudio(f.Tracks) {
			continue
		}
		if !f.Binary {
			return nil, nil, fmt.Errorf("cue FILE %q: only BINARY files are supported", f.Name)
		}
		p, size, err := statCueFile(fsys, dir, f.Name)
		if err != nil {
			return nil, nil, err
		}
		frames := size / bytesPerFrame
		src := len(files)
		files = append(files, resolvedFile{path: p, frames: frames})
		for ti := range f.Tracks {
			t := &f.Tracks[ti]
			if !t.Audio {
				continue
			}
			start := int64(t.Index1) * FramesPerSector
			end := frames
			if ti+1 < len(f.Tracks) {
				next := &f.Tracks[ti+1]
				at := next.Index1
				if !next.Audio && next.Index0 >= 0 {
					at = next.Index0
				}
				end = min(end, int64(at)*FramesPerSector)
			}
			if end <= start {
				continue
			}
			album.Tracks = append(album.Tracks, Track{
				Title:     clipText(t.Title),
				Performer: clipText(t.Performer),
				Number:    t.Number,
				source:    src,
				Start:     start,
				End:       end,
			})
			album.Total = max(album.Total, t.Number)
		}
	}
	if len(album.Tracks) == 0 {
		return nil, nil, ErrNoAudioTracks
	}
	return album, files, nil
}

func hasAudio(tracks []CueTrack) bool {
	for i := range tracks {
		if tracks[i].Audio {
			return true
		}
	}
	return false
}

// statCueFile finds a FILE entry beside the sheet or in a folder below it. A
// name that differs from the files on disk only in case, as sheets written on
// Windows often do, is matched too.
func statCueFile(fsys afero.Fs, dir, name string) (resolved string, size int64, err error) {
	if nameErr := ValidateFileName(name); nameErr != nil {
		return "", 0, nameErr
	}
	resolved = filepath.Join(dir, filepath.FromSlash(name))
	info, err := fsys.Stat(resolved)
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		if found, ok := matchFoldPath(fsys, dir, name); ok {
			resolved = found
			info, err = fsys.Stat(resolved)
		}
	}
	if err != nil {
		return "", 0, fmt.Errorf("cue FILE %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("cue FILE %q: not a regular file", name)
	}
	return resolved, info.Size(), nil
}

// matchFoldPath resolves a validated relative name below dir one part at a
// time, taking an exact match for a part when there is one.
func matchFoldPath(fsys afero.Fs, dir, name string) (string, bool) {
	cur := dir
	for _, part := range strings.Split(name, "/") {
		exact := filepath.Join(cur, part)
		if _, err := fsys.Stat(exact); err == nil {
			cur = exact
			continue
		}
		found, ok := matchFold(fsys, cur, part)
		if !ok {
			return "", false
		}
		cur = found
	}
	return cur, true
}

func matchFold(fsys afero.Fs, dir, name string) (string, bool) {
	entries, err := afero.ReadDir(fsys, dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name()), true
		}
	}
	return "", false
}
