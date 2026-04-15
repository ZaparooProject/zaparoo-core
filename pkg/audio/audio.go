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
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/gen2brain/malgo"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
	"github.com/rs/zerolog/log"
)

// Player is the interface for audio playback, allowing tests to mock sound output.
type Player interface {
	PlayBytes(data []byte) error
	PlayFile(path string) error
	ClearFileCache()
	SetVolume(volume float64)
}

// MalgoPlayer implements Player using malgo for real audio hardware output.
type MalgoPlayer struct {
	currentCancel context.CancelFunc
	currentDone   <-chan struct{} // closed when current playback goroutine finishes
	fileCache     map[string][]byte
	volume        float64 // 0.0-1.0, protected by playbackMu
	playbackGen   uint64
	fileCacheMu   syncutil.RWMutex
	playbackMu    syncutil.Mutex
}

// NewMalgoPlayer creates a new MalgoPlayer instance.
func NewMalgoPlayer() *MalgoPlayer {
	return &MalgoPlayer{
		fileCache: make(map[string][]byte),
		volume:    1.0,
	}
}

// SetVolume sets the playback volume (0.0-1.0). Applies to subsequent playback calls.
func (p *MalgoPlayer) SetVolume(volume float64) {
	p.playbackMu.Lock()
	defer p.playbackMu.Unlock()
	p.volume = volume
}

func (p *MalgoPlayer) playWAV(r io.ReadCloser) error {
	streamer, format, err := wav.Decode(r)
	if err != nil {
		if closeErr := r.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close audio reader on decode error")
		}
		return fmt.Errorf("failed to decode WAV stream: %w", err)
	}

	// Resample to 48000 Hz for HDMI audio compatibility (MiSTer, etc.)
	resampled := beep.Resample(4, format.SampleRate, beep.SampleRate(48000), streamer)

	// Cancel any currently playing sound and capture previous done channel.
	p.playbackMu.Lock()
	if p.currentCancel != nil {
		p.currentCancel()
	}
	prevDone := p.currentDone
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: stored in p.currentCancel
	p.currentCancel = cancel
	done := make(chan struct{})
	p.currentDone = done
	p.playbackGen++
	thisGen := p.playbackGen
	vol := p.volume
	p.playbackMu.Unlock()

	// Wait for the previous playback goroutine to fully release the audio
	// device before initializing a new one. Concurrent ALSA device access
	// from overlapping init/uninit calls crashes miniaudio on MiSTer.
	if prevDone != nil {
		select {
		case <-prevDone:
		case <-time.After(3 * time.Second):
			log.Warn().Msg("timeout waiting for previous audio playback cleanup")
		}
	}

	go func() {
		// Outermost: recover from any panic so audio issues never kill the service.
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Any("panic", rec).Msg("recovered panic in audio playback")
			}
		}()

		// Signal that this goroutine has finished and the device is released.
		defer close(done)

		defer func() {
			if err := streamer.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio streamer")
			}
			if err := r.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio reader")
			}
			p.playbackMu.Lock()
			if p.playbackGen == thisGen {
				p.currentCancel = nil
			}
			p.playbackMu.Unlock()
		}()

		if err := playWAVWithMalgo(ctx, resampled, vol); err != nil {
			if ctx.Err() != context.Canceled {
				log.Warn().Err(err).Msg("failed to play audio")
			}
			return
		}

		log.Debug().Msg("completed audio playback")
	}()

	return nil
}

// PlayBytes plays audio from a byte slice asynchronously, detecting format
// from magic bytes. Supports WAV, OGG (Vorbis), MP3, and FLAC.
func (p *MalgoPlayer) PlayBytes(data []byte) error {
	r := io.NopCloser(bytes.NewReader(data))

	switch detectAudioFormat(data) {
	case "wav":
		return p.playWAV(r)
	case "ogg":
		streamer, format, err := vorbis.Decode(r)
		if err != nil {
			return fmt.Errorf("failed to decode OGG stream: %w", err)
		}
		return p.playStream(streamer, format)
	case "mp3":
		streamer, format, err := mp3.Decode(r)
		if err != nil {
			return fmt.Errorf("failed to decode MP3 stream: %w", err)
		}
		return p.playStream(streamer, format)
	case "flac":
		streamer, format, err := flac.Decode(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("failed to decode FLAC stream: %w", err)
		}
		return p.playStream(streamer, format)
	default:
		return errors.New("unsupported audio format (expected WAV, OGG, MP3, or FLAC)")
	}
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

// playStream plays a decoded audio stream asynchronously, resampling to 48000 Hz.
func (p *MalgoPlayer) playStream(streamer beep.StreamSeekCloser, format beep.Format) error {
	resampled := beep.Resample(4, format.SampleRate, beep.SampleRate(48000), streamer)

	p.playbackMu.Lock()
	if p.currentCancel != nil {
		p.currentCancel()
	}
	prevDone := p.currentDone
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: stored in p.currentCancel
	p.currentCancel = cancel
	done := make(chan struct{})
	p.currentDone = done
	p.playbackGen++
	thisGen := p.playbackGen
	vol := p.volume
	p.playbackMu.Unlock()

	if prevDone != nil {
		select {
		case <-prevDone:
		case <-time.After(3 * time.Second):
			log.Warn().Msg("timeout waiting for previous audio playback cleanup")
		}
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Any("panic", rec).Msg("recovered panic in audio playback")
			}
		}()
		defer close(done)
		defer func() {
			if err := streamer.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio streamer")
			}
			p.playbackMu.Lock()
			if p.playbackGen == thisGen {
				p.currentCancel = nil
			}
			p.playbackMu.Unlock()
		}()

		if err := playWAVWithMalgo(ctx, resampled, vol); err != nil {
			if ctx.Err() != context.Canceled {
				log.Warn().Err(err).Msg("failed to play audio")
			}
			return
		}
		log.Debug().Msg("completed audio playback")
	}()

	return nil
}

// PlayFile plays an audio file asynchronously, detecting format by extension.
// Supports WAV, MP3, OGG (Vorbis), and FLAC. Cancels any currently playing sound.
// File bytes are cached per-instance to avoid repeated disk reads for the same path.
func (p *MalgoPlayer) PlayFile(path string) error {
	data, err := p.readFileWithCache(path)
	if err != nil {
		return fmt.Errorf("failed to read audio file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))

	var streamer beep.StreamSeekCloser
	var format beep.Format

	switch ext {
	case ".wav":
		streamer, format, err = wav.Decode(bytes.NewReader(data))
	case ".mp3":
		streamer, format, err = mp3.Decode(io.NopCloser(bytes.NewReader(data)))
	case ".ogg":
		streamer, format, err = vorbis.Decode(io.NopCloser(bytes.NewReader(data)))
	case ".flac":
		streamer, format, err = flac.Decode(bytes.NewReader(data))
	default:
		return fmt.Errorf("unsupported audio format: %s (supported: .wav, .mp3, .ogg, .flac)", ext)
	}

	if err != nil {
		return fmt.Errorf("failed to decode audio file: %w", err)
	}

	// Resample to 48000 Hz for HDMI audio compatibility (MiSTer, etc.)
	resampled := beep.Resample(4, format.SampleRate, beep.SampleRate(48000), streamer)

	// Cancel any currently playing sound and capture previous done channel.
	p.playbackMu.Lock()
	if p.currentCancel != nil {
		p.currentCancel()
	}
	prevDone := p.currentDone
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: stored in p.currentCancel
	p.currentCancel = cancel
	done := make(chan struct{})
	p.currentDone = done
	p.playbackGen++
	thisGen := p.playbackGen
	vol := p.volume
	p.playbackMu.Unlock()

	// Wait for the previous playback goroutine to fully release the audio
	// device before initializing a new one. Concurrent ALSA device access
	// from overlapping init/uninit calls crashes miniaudio on MiSTer.
	if prevDone != nil {
		select {
		case <-prevDone:
		case <-time.After(3 * time.Second):
			log.Warn().Msg("timeout waiting for previous audio playback cleanup")
		}
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Any("panic", rec).Msg("recovered panic in audio playback")
			}
		}()

		defer close(done)

		defer func() {
			if err := streamer.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio streamer")
			}
			p.playbackMu.Lock()
			if p.playbackGen == thisGen {
				p.currentCancel = nil
			}
			p.playbackMu.Unlock()
		}()

		if err := playWAVWithMalgo(ctx, resampled, vol); err != nil {
			if ctx.Err() != context.Canceled {
				log.Warn().Err(err).Msg("failed to play audio")
			}
			return
		}

		log.Debug().Str("path", path).Msg("completed audio playback")
	}()

	return nil
}

// readFileWithCache returns file bytes, using an in-memory cache to avoid
// repeated disk reads for files that are played frequently (e.g. scan feedback).
func (p *MalgoPlayer) readFileWithCache(path string) ([]byte, error) {
	p.fileCacheMu.RLock()
	if cached, ok := p.fileCache[path]; ok {
		p.fileCacheMu.RUnlock()
		return cached, nil
	}
	p.fileCacheMu.RUnlock()

	//nolint:gosec // G304: callers are responsible for path sanitization
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	p.fileCacheMu.Lock()
	p.fileCache[path] = data
	p.fileCacheMu.Unlock()

	return data, nil
}

// ClearFileCache clears the in-memory file cache, forcing subsequent PlayFile
// calls to re-read from disk. Called after settings reload to pick up new files.
func (p *MalgoPlayer) ClearFileCache() {
	p.fileCacheMu.Lock()
	defer p.fileCacheMu.Unlock()
	p.fileCache = make(map[string][]byte)
}

// playWAVWithMalgo plays audio samples through malgo, blocking until complete or ctx is cancelled.
// The volume parameter (0.0-1.0) scales sample amplitude before output.
func playWAVWithMalgo(ctx context.Context, streamer beep.Streamer, volume float64) error {
	malgoCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize malgo context: %w", err)
	}
	if malgoCtx == nil {
		return errors.New("malgo context is nil after initialization")
	}
	defer func() {
		_ = malgoCtx.Uninit()
		malgoCtx.Free()
	}()

	// F32 format avoids buggy S16->S32 conversion in miniaudio on PulseAudio
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatF32
	deviceConfig.Playback.Channels = 2
	deviceConfig.SampleRate = 48000
	deviceConfig.Alsa.NoMMap = 1

	done := make(chan struct{})

	var (
		mu       syncutil.Mutex
		finished bool
		samples  [][2]float64
	)

	onSamples := func(pOutputSample, _ []byte, frameCount uint32) {
		mu.Lock()
		defer mu.Unlock()

		if finished {
			return
		}

		select {
		case <-ctx.Done():
			finished = true
			close(done)
			return
		default:
		}

		if len(samples) < int(frameCount) {
			samples = make([][2]float64, frameCount)
		}

		n, ok := streamer.Stream(samples[:frameCount])
		if !ok || n == 0 {
			finished = true
			close(done)
			return
		}

		// Convert beep's [][2]float64 samples to interleaved F32 PCM
		offset := 0
		for i := range n {
			sample := float32(samples[i][0] * volume)
			binary.LittleEndian.PutUint32(pOutputSample[offset:], math.Float32bits(sample))
			offset += 4

			sample = float32(samples[i][1] * volume)
			binary.LittleEndian.PutUint32(pOutputSample[offset:], math.Float32bits(sample))
			offset += 4
		}

		for i := offset; i < len(pOutputSample); i++ {
			pOutputSample[i] = 0
		}
	}

	device, err := malgo.InitDevice(malgoCtx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onSamples,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize audio device: %w", err)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return fmt.Errorf("failed to start audio device: %w", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		mu.Lock()
		if !finished {
			finished = true
		}
		mu.Unlock()
	}

	if err := device.Stop(); err != nil {
		log.Warn().Err(err).Msg("failed to stop audio device")
	}

	if ctx.Err() == context.Canceled {
		return context.Canceled
	}

	return nil
}
