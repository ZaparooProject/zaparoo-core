/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

// Package audio provides cross-platform audio playback using malgo.
package audio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
	"github.com/rs/zerolog/log"
)

const (
	targetSampleRate         = 48000
	resampleQuality          = 4
	periodSizeInMilliseconds = 50
	periodCount              = 4
)

// Player is the interface for audio playback, allowing tests to mock sound output.
type Player interface {
	PlayBytes(data []byte) error
	PlayFile(path string) error
	ClearFileCache()
	SetVolume(volume float64)
}

// MalgoPlayer plays short audio via the shared malgo output device. Decoded and
// resampled PCM samples are cached per file path so repeated playback (e.g. scan
// feedback sounds) does not re-decode or re-resample; the realtime audio callback
// only sums already-prepared samples into the mix buffer.
//
// A new Play call cancels the previous one-shot; only one scan sound plays at a time.
type MalgoPlayer struct {
	currentSrc  *oneshotSource
	pcmCache    map[string][][2]float64
	volume      float64
	playbackGen uint64
	pcmCacheMu  syncutil.RWMutex
	playbackMu  syncutil.Mutex
}

// NewMalgoPlayer creates a new MalgoPlayer instance.
func NewMalgoPlayer() *MalgoPlayer {
	return &MalgoPlayer{
		pcmCache: make(map[string][][2]float64),
		volume:   1.0,
	}
}

// SetVolume sets the playback volume (0.0–2.0). Applies to subsequent playback calls.
func (p *MalgoPlayer) SetVolume(volume float64) {
	p.playbackMu.Lock()
	defer p.playbackMu.Unlock()
	p.volume = volume
}

// playWAV decodes a WAV stream to PCM and plays it through the shared device.
// The reader is closed before this function returns.
func (p *MalgoPlayer) playWAV(r io.ReadCloser) error {
	samples, err := decodeWAV(r)
	if err != nil {
		return err
	}
	return p.playPCM(samples)
}

// PlayBytes plays audio from a byte slice asynchronously, detecting format from
// magic bytes. Supports WAV, OGG (Vorbis), MP3, and FLAC.
func (p *MalgoPlayer) PlayBytes(data []byte) error {
	samples, err := decodeBytesByMagic(data)
	if err != nil {
		return err
	}
	return p.playPCM(samples)
}

// PlayFile plays an audio file asynchronously, detecting format by extension.
// Supports WAV, MP3, OGG (Vorbis), and FLAC. Cancels any currently playing sound.
// Decoded+resampled PCM is cached per path so repeat plays skip the decode work.
func (p *MalgoPlayer) PlayFile(path string) error {
	samples, err := p.loadPCMFromFile(path)
	if err != nil {
		return err
	}
	return p.playPCM(samples)
}

// ClearFileCache clears the in-memory PCM cache, forcing subsequent PlayFile
// calls to re-read and re-decode from disk. Called after settings reload to
// pick up new files.
func (p *MalgoPlayer) ClearFileCache() {
	p.pcmCacheMu.Lock()
	defer p.pcmCacheMu.Unlock()
	p.pcmCache = make(map[string][][2]float64)
}

// loadPCMFromFile returns decoded+resampled stereo samples for the given file
// path, using the PCM cache to avoid repeated decode work.
func (p *MalgoPlayer) loadPCMFromFile(path string) ([][2]float64, error) {
	p.pcmCacheMu.RLock()
	if cached, ok := p.pcmCache[path]; ok {
		p.pcmCacheMu.RUnlock()
		return cached, nil
	}
	p.pcmCacheMu.RUnlock()

	//nolint:gosec // G304: callers are responsible for path sanitization
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	samples, err := decodeBytesByExt(data, strings.ToLower(filepath.Ext(path)))
	if err != nil {
		return nil, err
	}

	p.pcmCacheMu.Lock()
	p.pcmCache[path] = samples
	p.pcmCacheMu.Unlock()

	return samples, nil
}

// playPCM registers a one-shot source for the given pre-decoded samples on the
// shared output device. Any previously playing sound is cancelled.
func (p *MalgoPlayer) playPCM(samples [][2]float64) error {
	p.playbackMu.Lock()
	vol := p.volume
	oldSrc := p.currentSrc
	p.playbackGen++
	src := &oneshotSource{
		samples: samples,
		volume:  vol,
	}
	// Self-clearing: remove reference when this source finishes naturally.
	src.onDrain = func() {
		p.playbackMu.Lock()
		if p.currentSrc == src {
			p.currentSrc = nil
		}
		p.playbackMu.Unlock()
	}
	p.currentSrc = src
	p.playbackMu.Unlock()

	// Cancel the previous source. It will drain immediately on the next callback
	// and be removed by the manager, keeping the device open for the new source.
	if oldSrc != nil {
		oldSrc.cancel()
	}

	globalDevice.register(src)
	return nil
}

// oneshotSource is a pre-decoded PCM buffer that plays once through the shared device.
// It implements mixSource; the callback sums it into the mix buffer until exhausted.
type oneshotSource struct {
	onDrain func()
	samples [][2]float64
	pos     int
	volume  float64
	mu      syncutil.Mutex
	stopped bool
}

// mixAdd sums up to n frames from the buffer into buf. Returns drained when exhausted.
// Called on the malgo audio thread under devMu — must not block or alloc.
func (s *oneshotSource) mixAdd(buf [][2]float64, n int) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return 0, true
	}
	written := 0
	for written < n && s.pos < len(s.samples) {
		buf[written][0] += s.samples[s.pos][0] * s.volume
		buf[written][1] += s.samples[s.pos][1] * s.volume
		s.pos++
		written++
	}
	return written, s.pos >= len(s.samples)
}

// isActive always returns true: one-shot sources are active until drained or cancelled.
func (*oneshotSource) isActive() bool {
	return true
}

// cancel marks the source as stopped so it drains immediately on the next callback.
func (s *oneshotSource) cancel() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
}

// onDrained implements the optional drainCallback interface used by the device manager.
func (s *oneshotSource) onDrained() {
	if s.onDrain != nil {
		s.onDrain()
	}
}

// decodeWAV decodes a WAV reader fully into PCM samples and closes the reader.
func decodeWAV(r io.ReadCloser) ([][2]float64, error) {
	streamer, format, err := wav.Decode(r)
	if err != nil {
		if closeErr := r.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close audio reader on decode error")
		}
		return nil, fmt.Errorf("failed to decode WAV stream: %w", err)
	}
	defer func() {
		if closeErr := streamer.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close audio streamer")
		}
		if closeErr := r.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close audio reader")
		}
	}()
	return drainStream(streamer, format), nil
}

// decodeBytesByMagic decodes a buffer whose format is detected from leading bytes.
func decodeBytesByMagic(data []byte) ([][2]float64, error) {
	switch detectAudioFormat(data) {
	case "wav":
		streamer, format, err := wav.Decode(io.NopCloser(bytes.NewReader(data)))
		if err != nil {
			return nil, fmt.Errorf("failed to decode WAV stream: %w", err)
		}
		defer closeStreamer(streamer)
		return drainStream(streamer, format), nil
	case "ogg":
		streamer, format, err := vorbis.Decode(io.NopCloser(bytes.NewReader(data)))
		if err != nil {
			return nil, fmt.Errorf("failed to decode OGG stream: %w", err)
		}
		defer closeStreamer(streamer)
		return drainStream(streamer, format), nil
	case "mp3":
		streamer, format, err := mp3.Decode(io.NopCloser(bytes.NewReader(data)))
		if err != nil {
			return nil, fmt.Errorf("failed to decode MP3 stream: %w", err)
		}
		defer closeStreamer(streamer)
		return drainStream(streamer, format), nil
	case "flac":
		streamer, format, err := flac.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to decode FLAC stream: %w", err)
		}
		defer closeStreamer(streamer)
		return drainStream(streamer, format), nil
	default:
		return nil, errors.New("unsupported audio format (expected WAV, OGG, MP3, or FLAC)")
	}
}

// decodeBytesByExt decodes a buffer using the decoder selected by file extension.
func decodeBytesByExt(data []byte, ext string) ([][2]float64, error) {
	var (
		streamer beep.StreamSeekCloser
		format   beep.Format
		err      error
	)
	switch ext {
	case ".wav":
		streamer, format, err = wav.Decode(io.NopCloser(bytes.NewReader(data)))
	case ".mp3":
		streamer, format, err = mp3.Decode(io.NopCloser(bytes.NewReader(data)))
	case ".ogg":
		streamer, format, err = vorbis.Decode(io.NopCloser(bytes.NewReader(data)))
	case ".flac":
		streamer, format, err = flac.Decode(bytes.NewReader(data))
	default:
		return nil, fmt.Errorf("unsupported audio format: %s (supported: .wav, .mp3, .ogg, .flac)", ext)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to decode audio file: %w", err)
	}
	defer closeStreamer(streamer)
	return drainStream(streamer, format), nil
}

func closeStreamer(s beep.StreamSeekCloser) {
	if err := s.Close(); err != nil {
		log.Warn().Err(err).Msg("failed to close audio streamer")
	}
}

// drainStream resamples a streamer to targetSampleRate and reads all samples
// into a slice. Runs once at decode time, never on the realtime audio thread,
// so the per-period cost of sinc resampling never causes ALSA underruns.
func drainStream(streamer beep.Streamer, format beep.Format) [][2]float64 {
	resampled := beep.Resample(resampleQuality, format.SampleRate, beep.SampleRate(targetSampleRate), streamer)

	const chunk = 4096
	var (
		buf [chunk][2]float64
		out [][2]float64
	)
	for {
		n, ok := resampled.Stream(buf[:])
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if !ok || n == 0 {
			break
		}
	}
	return out
}

// detectAudioFormat returns a format name based on file magic bytes.
func detectAudioFormat(data []byte) string {
	if len(data) >= 4 && string(data[:4]) == "RIFF" {
		return "wav"
	}
	if len(data) >= 4 && string(data[:4]) == "OggS" {
		return "ogg"
	}
	if len(data) >= 4 && string(data[:4]) == "fLaC" {
		return "flac"
	}
	if len(data) >= 3 && string(data[:3]) == "ID3" {
		return "mp3"
	}
	if len(data) >= 2 && data[0] == 0xFF && (data[1]&0xE0) == 0xE0 {
		return "mp3"
	}
	return ""
}
