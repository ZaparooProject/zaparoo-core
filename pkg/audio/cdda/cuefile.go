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
	"io"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/trackmeta"
	"github.com/spf13/afero"
)

// CueOptions configure OpenCue.
type CueOptions struct {
	// Fs is the filesystem the sheet and its files are read from; nil is
	// the OS filesystem.
	Fs afero.Fs
	// Track plays only the track with this number; 0 plays every audio
	// track.
	Track int
}

// ReadCue reads and parses a cue sheet and checks the files it names.
func ReadCue(fsys afero.Fs, cuePath string) (*Album, []string, error) {
	if fsys == nil {
		fsys = afero.NewOsFs()
	}
	album, files, err := loadCue(fsys, cuePath)
	if err != nil {
		return nil, nil, err
	}
	paths := make([]string, len(files))
	for i := range files {
		paths[i] = files[i].path
	}
	return album, paths, nil
}

func loadCue(fsys afero.Fs, cuePath string) (*Album, []resolvedFile, error) {
	f, err := fsys.Open(cuePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open cue sheet: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxCueBytes+1))
	_ = f.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("read cue sheet: %w", err)
	}
	sheet, err := ParseCue(data)
	if err != nil {
		return nil, nil, err
	}
	album, files, err := resolveCue(fsys, cuePath, sheet)
	if err != nil {
		return nil, nil, err
	}
	album.HasArt = trackmeta.HasFolderArt(cuePath)
	return album, files, nil
}

// OpenCue opens the audio of a cue sheet rip.
func OpenCue(cuePath string, opts CueOptions) (*Decoder, error) {
	fsys := opts.Fs
	if fsys == nil {
		fsys = afero.NewOsFs()
	}
	album, files, err := loadCue(fsys, cuePath)
	if err != nil {
		return nil, err
	}
	if opts.Track > 0 {
		if onlyErr := album.only(opts.Track); onlyErr != nil {
			return nil, onlyErr
		}
	}
	starts, err := trackStarts(album)
	if err != nil {
		return nil, err
	}
	return newDecoder(album, starts, &cueReader{fs: fsys, files: files, open: make([]afero.File, len(files))}), nil
}

// cueReader reads frames from a rip's files, opening each the first time it
// is needed.
type cueReader struct {
	fs    afero.Fs
	files []resolvedFile
	open  []afero.File
}

func (r *cueReader) readFrames(t *Track, off int64, p []byte) (int, error) {
	f := r.open[t.source]
	if f == nil {
		var err error
		f, err = r.fs.Open(r.files[t.source].path)
		if err != nil {
			return 0, fmt.Errorf("open %s: %w", r.files[t.source].path, err)
		}
		r.open[t.source] = f
	}
	n, err := f.ReadAt(p, off*bytesPerFrame)
	if n == len(p) {
		return n, nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return n - n%bytesPerFrame, fmt.Errorf("read %s: %w", r.files[t.source].path, err)
}

func (r *cueReader) close() error {
	var errs []error
	for i, f := range r.open {
		if f != nil {
			errs = append(errs, f.Close())
			r.open[i] = nil
		}
	}
	return errors.Join(errs...)
}
