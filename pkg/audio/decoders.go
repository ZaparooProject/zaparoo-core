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
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/radio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/trackmeta"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/wav"
)

// ErrLiveStream is returned for controls a live stream cannot honour, such
// as seeking.
var ErrLiveStream = errors.New("not supported for live streams")

// SupportedExtensions lists every file extension long-form playback opens.
var SupportedExtensions = []string{
	".wav", ".mp3", ".ogg", ".flac",
}

// PlaylistExtensions lists the playlist files kept with audio. They are not
// decoded: launching one plays its entries as a playlist.
var PlaylistExtensions = []string{".pls", ".m3u"}

// radioProbeTimeout bounds finding out what a stream URL serves.
const radioProbeTimeout = 10 * time.Second

// liveDecoder is a decoder whose audio arrives in real time.
type liveDecoder interface {
	// Buffered is how many source frames can be read without a short read.
	Buffered() int
	Suspend()
	Resume()
}

type openedDecoder struct {
	decoder beep.StreamSeekCloser
	file    *os.File
	// meta is what the decoder knows of the track at open; nil means the
	// tags are read from the file.
	meta *trackmeta.Meta
	live liveDecoder
	rate beep.SampleRate
}

type decodeOptions struct {
	radio    *radio.Probe
	onMeta   func(*trackmeta.Meta)
	lowPower bool
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

	ext := strings.ToLower(filepath.Ext(path))
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
