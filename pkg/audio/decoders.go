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

package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/gme"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/midi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/radio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/trackmeta"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/vgm"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/wav"
)

// ErrLiveStream is returned for controls a live stream cannot honour, such
// as seeking.
var ErrLiveStream = errors.New("not supported for live streams")

// ErrSingleTrack is returned when skipping tracks on a source that holds
// only one.
var ErrSingleTrack = errors.New("source has a single track")

// SupportedExtensions lists every file extension long-form playback opens.
var SupportedExtensions = []string{
	".wav", ".mp3", ".ogg", ".flac",
	".vgm", ".vgz",
	".nsf", ".nsfe", ".gbs", ".hes", ".kss", ".spc",
	".mid", ".midi", ".kar", ".rmi",
}

// PlaylistExtensions lists the playlist files kept with audio. They are not
// decoded: launching one plays its entries as a playlist.
var PlaylistExtensions = []string{".pls", ".m3u"}

// radioProbeTimeout bounds finding out what a stream URL serves.
const radioProbeTimeout = 10 * time.Second

// multiTrackExtensions are the formats whose single tracks are addressed as
// <file>/<n>, n counting from 1.
var multiTrackExtensions = []string{".nsf", ".nsfe", ".gbs", ".hes", ".kss"}

// liveDecoder is a decoder whose audio arrives in real time.
type liveDecoder interface {
	// Buffered is how many source frames can be read without a short read.
	Buffered() int
	Suspend()
	Resume()
}

// trackDecoder is a decoder that plays several tracks back to back.
type trackDecoder interface {
	// TrackStarts is the source frame each track begins at, in order.
	TrackStarts() []int
}

type openedDecoder struct {
	decoder beep.StreamSeekCloser
	file    *os.File
	// meta is what the decoder knows of the track at open; nil means the
	// tags are read from the file.
	meta   *trackmeta.Meta
	live   liveDecoder
	tracks trackDecoder
	rate   beep.SampleRate
}

type decodeOptions struct {
	radio         *radio.Probe
	onMeta        func(*trackmeta.Meta)
	midiSoundFont string
	lowPower      bool
}

// midiVoices caps SoundFont polyphony.
func midiVoices(lowPower bool) int {
	if lowPower {
		return 48
	}
	return 128
}

// SplitTrackPath splits a <file>/<n> single-track path of a multi-track
// format into the file and its 0-based track. ok is false when path is not
// such a path.
func SplitTrackPath(path string) (file string, track int, ok bool) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(dir))
	if !isMultiTrackExt(ext) {
		return "", 0, false
	}
	n, err := strconv.Atoi(base)
	if err != nil || n < 1 || strconv.Itoa(n) != base {
		return "", 0, false
	}
	return dir, n - 1, true
}

func isMultiTrackExt(ext string) bool {
	for _, e := range multiTrackExtensions {
		if e == ext {
			return true
		}
	}
	return false
}

// IsStreamURL reports whether path is an http(s) URL played as a stream.
func IsStreamURL(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func openDecoder(path string, opts decodeOptions) (*openedDecoder, error) {
	if opts.radio != nil || IsStreamURL(path) {
		return openRadio(path, opts)
	}

	track := -1
	if file, n, ok := SplitTrackPath(path); ok {
		if _, err := os.Stat(path); err != nil {
			path, track = file, n
		}
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case ext == ".vgm" || ext == ".vgz":
		d, err := vgm.Open(path, vgm.Options{SampleRate: targetSampleRate})
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", ext, err)
		}
		info := d.Info()
		return &openedDecoder{decoder: d, rate: beep.SampleRate(d.SampleRate()), meta: vgmMeta(path, &info)}, nil
	case gme.Supported(ext):
		d, err := gme.Open(path, track, gme.Options{SampleRate: targetSampleRate})
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", ext, err)
		}
		info := d.Info()
		od := &openedDecoder{decoder: d, rate: beep.SampleRate(d.SampleRate()), meta: gmeMeta(path, &info)}
		if track < 0 && len(d.TrackStarts()) > 1 {
			od.tracks = d
		}
		return od, nil
	case midi.Supported(ext):
		d, err := midi.Open(path, midi.Options{
			SampleRate:    targetSampleRate,
			SoundFontPath: opts.midiSoundFont,
			MaxVoices:     midiVoices(opts.lowPower),
		})
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", ext, err)
		}
		return &openedDecoder{decoder: d, rate: beep.SampleRate(d.SampleRate()), meta: midiMeta(path, d.Info())}, nil
	}

	if kind, native := nativedec.KindForExt(ext); native {
		// MP3, FLAC and Vorbis decode natively, reading the file themselves.
		d, err := nativedec.Open(path, kind)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", ext, err)
		}
		return &openedDecoder{decoder: d, rate: nativeFormat(d).SampleRate}, nil
	}
	if ext != ".wav" {
		return nil, fmt.Errorf("unsupported audio format: %s (supported: %s)", ext,
			strings.Join(SupportedExtensions, ", "))
	}
	//nolint:gosec // G304: callers validate media paths before launching.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio file: %w", err)
	}
	d, format, err := wav.Decode(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("decode %s: %w", ext, err)
	}
	return &openedDecoder{decoder: d, file: f, rate: format.SampleRate}, nil
}

func openRadio(url string, opts decodeOptions) (*openedDecoder, error) {
	probe := opts.radio
	client := radio.NewHTTPClient()
	if probe == nil {
		ctx, cancel := context.WithTimeout(context.Background(), radioProbeTimeout)
		p, err := radio.ProbeURL(ctx, client, url)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("probe stream: %w", err)
		}
		if p.Kind() == radio.KindNotAudio {
			return nil, fmt.Errorf("not an audio stream: %s", url)
		}
		probe = p
	}
	var name atomic.Pointer[string]
	s, err := radio.Open(context.Background(), probe, radio.Options{
		ResourceConstrained: opts.lowPower,
		HTTPClient:          client,
		OnMeta: func(title, artist string) {
			if opts.onMeta != nil {
				station := ""
				if n := name.Load(); n != nil {
					station = *n
				}
				opts.onMeta(&trackmeta.Meta{Title: title, Artist: artist, Album: station})
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	station := s.Name()
	name.Store(&station)
	od := &openedDecoder{decoder: s, rate: beep.SampleRate(s.SampleRate()), live: s}
	if station != "" {
		od.meta = &trackmeta.Meta{Album: station}
	}
	return od, nil
}

func nonEmptyMeta(m *trackmeta.Meta) *trackmeta.Meta {
	if m == nil || *m == (trackmeta.Meta{}) {
		return nil
	}
	return m
}

// vgmMeta maps a VGM file's GD3 tag: the game is the album and the composer
// the artist.
func vgmMeta(path string, info *vgm.Info) *trackmeta.Meta {
	return chipMeta(path, &trackmeta.Meta{
		Title:  info.Title,
		Artist: info.Author,
		Album:  info.Game,
		Year:   leadingYear(info.Year),
	})
}

// gmeMeta maps a chiptune file's header or tags the same way as VGM. The
// copyright field usually starts with the year.
func gmeMeta(path string, info *gme.Info) *trackmeta.Meta {
	return chipMeta(path, &trackmeta.Meta{
		Title:  info.Title,
		Artist: info.Author,
		Album:  info.Game,
		Year:   leadingYear(info.Copyright),
	})
}

func midiMeta(path string, info midi.Info) *trackmeta.Meta {
	return chipMeta(path, &trackmeta.Meta{Title: info.Title})
}

// chipMeta finishes the tags of a format whose decoder reads them itself:
// art can only come from an image beside the file. A file with no tags at
// all still reports its art.
func chipMeta(path string, m *trackmeta.Meta) *trackmeta.Meta {
	m.HasArt = trackmeta.HasFolderArt(path)
	return nonEmptyMeta(m)
}

// leadingYear reads a four-digit year at the start of s, such as "1991" or
// "1991 Sega", and returns 0 when there is none.
func leadingYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil || y < 1950 || y > 2100 {
		return 0
	}
	if len(s) > 4 && s[4] >= '0' && s[4] <= '9' {
		return 0
	}
	return y
}
