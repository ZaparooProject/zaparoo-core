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

// Package midi renders Standard MIDI Files (.mid, .midi, .kar and RIFF
// .rmi) to stereo float audio. By default notes are played on an emulated
// OPL3 FM chip by libADLMIDI with an embedded General MIDI bank, so MIDI
// works with nothing installed; when a SoundFont is configured the song is
// rendered from its samples by TinySoundFont instead. A Decoder is a beep
// StreamSeekCloser.
package midi

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gopxl/beep/v2"
	"github.com/rs/zerolog/log"
)

const (
	// DefaultSampleRate is the output rate used when Options.SampleRate is 0.
	DefaultSampleRate = 48000
	// DefaultMaxVoices is the SoundFont voice cap used when
	// Options.MaxVoices is 0.
	DefaultMaxVoices = 64
	// MaxFileBytes is the largest MIDI file Open reads.
	MaxFileBytes = 8 << 20
	// MaxSoundFontBytes is the largest SoundFont file that is loaded.
	MaxSoundFontBytes = 512 << 20
	// DefaultSoundFontMemory is the SoundFont memory budget used when
	// Options.SoundFontMemory is 0.
	DefaultSoundFontMemory = 2 << 30

	// RendererFM and RendererSoundFont are the values of Info.Renderer.
	RendererFM        = "fm"
	RendererSoundFont = "soundfont"

	// tailSeconds of audio follow the last MIDI event so released notes
	// can ring out instead of being cut off. libADLMIDI already counts one
	// second of its own into the song length, so this is added for
	// TinySoundFont only.
	tailSeconds = 1
	// maxSongSeconds bounds the length of one song. A crafted file can
	// claim any length; this keeps frame counts well inside a 32-bit int.
	maxSongSeconds = 6 * 60 * 60
	// maxSampleRate bounds Options.SampleRate.
	maxSampleRate = 384000
)

var (
	// ErrSoundFont is wrapped by the error logged when the configured
	// SoundFont cannot be used. Open does not return it: the song is
	// rendered with the FM synthesizer instead.
	ErrSoundFont = errors.New("midi: cannot load SoundFont")
	// ErrClosed is returned by a Decoder used after Close.
	ErrClosed = errors.New("midi: decoder closed")
	// ErrTooLarge is returned for a MIDI file above MaxFileBytes.
	ErrTooLarge = errors.New("midi: file too large")
)

// Options configures a Decoder.
type Options struct {
	// SoundFontPath selects SoundFont rendering; "" uses FM synthesis.
	SoundFontPath string
	// SampleRate is the output rate in Hz; 0 means DefaultSampleRate.
	SampleRate int
	// MaxVoices caps simultaneous SoundFont voices; 0 means
	// DefaultMaxVoices. It does not apply to FM synthesis.
	MaxVoices int
	// SoundFontMemory caps the memory loading the SoundFont may take: the
	// file plus the samples it decodes to, which for SF3 can be many times
	// the file. A SoundFont over it renders with FM instead. 0 means
	// DefaultSoundFontMemory.
	SoundFontMemory int64
}

// Info describes a song.
type Info struct {
	// Title comes from the karaoke title, track name or text meta events;
	// it is "" when the file has none.
	Title string
	// Renderer is RendererFM or RendererSoundFont for a Decoder, and ""
	// from ReadInfo, which does not pick a renderer.
	Renderer string
}

// Supported reports whether Open handles files with extension ext, which
// includes the dot and is matched case-insensitively.
func Supported(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mid", ".midi", ".kar", ".rmi":
		return true
	default:
		return false
	}
}

// renderer is one synthesizer bound to one song.
type renderer interface {
	// render fills out with frames stereo interleaved frames.
	render(out []float32, frames int)
	// seek moves playback to frame, which is within the song.
	seek(frame int)
	close()
}

// Decoder streams one rendered song. It is not safe for concurrent use.
type Decoder struct {
	r      renderer
	err    error
	info   Info
	buf    []float32
	rate   int
	length int
	pos    int
}

var _ beep.StreamSeekCloser = (*Decoder)(nil)

// Open renders the MIDI file at path.
func Open(path string, opts Options) (*Decoder, error) {
	data, err := readCapped(path, MaxFileBytes)
	if err != nil {
		return nil, err
	}
	return OpenBytes(data, opts)
}

// OpenBytes renders a MIDI file held in memory. data may be a Standard
// MIDI File or an RMI file wrapping one; FM rendering also takes the other
// formats libADLMIDI reads (XMI, MUS and friends). A SoundFont that cannot
// be loaded is logged and the song is rendered with FM instead.
func OpenBytes(data []byte, opts Options) (*Decoder, error) {
	opts, err := normalize(opts)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	song, err := unwrapRMI(data, false)
	if err != nil {
		return nil, err
	}
	if len(song) == 0 {
		return nil, errNotMIDI
	}
	title, _ := smfTitle(song)

	if opts.SoundFontPath != "" && isSMF(song) {
		r, frames, sfErr := openSoundFont(song, opts)
		if sfErr == nil {
			return newDecoder(r, frames, opts.SampleRate, Info{Title: title, Renderer: RendererSoundFont}), nil
		}
		if errors.Is(sfErr, ErrSoundFont) {
			log.Warn().Err(sfErr).Str("soundfont", opts.SoundFontPath).
				Msg("midi: SoundFont unusable, rendering with FM synthesis")
		} else {
			log.Debug().Err(sfErr).Msg("midi: SoundFont renderer rejected song, trying FM")
		}
	}

	r, frames, err := openFM(song, opts.SampleRate)
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = r.title
	}
	return newDecoder(r, frames, opts.SampleRate, Info{Title: title, Renderer: RendererFM}), nil
}

// ReadInfo reads a MIDI file's metadata without rendering it. At most
// maxBytes of the file are read (MaxFileBytes when maxBytes <= 0); the
// title sits near the start, so a prefix is enough.
func ReadInfo(path string, maxBytes int64) (Info, error) {
	if maxBytes <= 0 || maxBytes > MaxFileBytes {
		maxBytes = MaxFileBytes
	}
	f, err := os.Open(path) //nolint:gosec // path comes from the media library
	if err != nil {
		return Info{}, fmt.Errorf("midi: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return Info{}, fmt.Errorf("midi: %w", err)
	}
	song, err := unwrapRMI(data, true)
	if err != nil {
		return Info{}, err
	}
	title, err := smfTitle(song)
	if err != nil {
		return Info{}, err
	}
	return Info{Title: title}, nil
}

func normalize(opts Options) (Options, error) {
	if opts.SampleRate == 0 {
		opts.SampleRate = DefaultSampleRate
	}
	if opts.SampleRate < 8000 || opts.SampleRate > maxSampleRate {
		return opts, fmt.Errorf("midi: unsupported sample rate %d", opts.SampleRate)
	}
	if opts.MaxVoices <= 0 {
		opts.MaxVoices = DefaultMaxVoices
	}
	if opts.SoundFontMemory <= 0 {
		opts.SoundFontMemory = DefaultSoundFontMemory
	}
	return opts, nil
}

// readCapped reads a whole file, refusing one above limit bytes.
func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from the media library
	if err != nil {
		return nil, fmt.Errorf("midi: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("midi: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}

// songFrames converts a stream length in seconds to frames at rate,
// bounded by maxSongSeconds.
func songFrames(seconds float64, rate int) int {
	if !(seconds > 0) { // also catches NaN
		seconds = 0
	}
	if seconds > maxSongSeconds {
		seconds = maxSongSeconds
	}
	return int(seconds*float64(rate) + 0.5)
}

func newDecoder(r renderer, frames, rate int, info Info) *Decoder {
	return &Decoder{r: r, length: frames, rate: rate, info: info}
}

// Stream fills samples with rendered audio.
func (d *Decoder) Stream(samples [][2]float64) (n int, ok bool) {
	if d.r == nil || d.err != nil {
		return 0, false
	}
	want := len(samples)
	if left := d.length - d.pos; want > left {
		want = left
	}
	if want <= 0 {
		return 0, false
	}
	if cap(d.buf) < want*2 {
		d.buf = make([]float32, want*2)
	}
	buf := d.buf[:want*2]
	d.r.render(buf, want)
	for i := range want {
		samples[i][0] = float64(buf[2*i])
		samples[i][1] = float64(buf[2*i+1])
	}
	d.pos += want
	return want, true
}

// Err returns the error that stopped the stream, if any.
func (d *Decoder) Err() error { return d.err }

// Len is the stream length in frames, including a release tail of about
// a second after the last event.
func (d *Decoder) Len() int { return d.length }

// Position is the current frame.
func (d *Decoder) Position() int { return d.pos }

// SampleRate is the output rate in Hz.
func (d *Decoder) SampleRate() int { return d.rate }

// Info describes the song and the renderer playing it.
func (d *Decoder) Info() Info { return d.info }

// Seek moves playback to frame p. Notes sounding before the seek are
// released; notes that started before p are not restarted, as in any MIDI
// sequencer.
func (d *Decoder) Seek(p int) error {
	if d.r == nil {
		return ErrClosed
	}
	if p < 0 || p > d.length {
		return fmt.Errorf("midi: seek position %d out of range [0, %d]", p, d.length)
	}
	d.r.seek(p)
	d.pos = p
	return nil
}

// Close frees the synthesizer. It is safe to call more than once.
func (d *Decoder) Close() error {
	if d.r != nil {
		d.r.close()
		d.r = nil
	}
	return nil
}
