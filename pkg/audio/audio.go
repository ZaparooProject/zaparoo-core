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
	PlayWAVBytes(data []byte) error
	PlayFile(path string) error
	ClearFileCache()
}

// MalgoPlayer implements Player using malgo for real audio hardware output.
type MalgoPlayer struct {
	currentCancel context.CancelFunc
	fileCache     map[string][]byte
	playbackGen   uint64
	fileCacheMu   syncutil.RWMutex
	playbackMu    syncutil.Mutex
}

// NewMalgoPlayer creates a new MalgoPlayer instance.
func NewMalgoPlayer() *MalgoPlayer {
	return &MalgoPlayer{
		fileCache: make(map[string][]byte),
	}
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

	// Cancel any currently playing sound
	p.playbackMu.Lock()
	if p.currentCancel != nil {
		p.currentCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.currentCancel = cancel
	p.playbackGen++
	thisGen := p.playbackGen
	p.playbackMu.Unlock()

	go func() {
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

		if err := playWAVWithMalgo(ctx, resampled); err != nil {
			if ctx.Err() != context.Canceled {
				log.Warn().Err(err).Msg("failed to play audio")
			}
			return
		}

		log.Debug().Msg("completed audio playback")
	}()

	return nil
}

// PlayWAVBytes plays WAV audio from a byte slice asynchronously.
func (p *MalgoPlayer) PlayWAVBytes(data []byte) error {
	return p.playWAV(io.NopCloser(bytes.NewReader(data)))
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

	p.playbackMu.Lock()
	if p.currentCancel != nil {
		p.currentCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.currentCancel = cancel
	p.playbackGen++
	thisGen := p.playbackGen
	p.playbackMu.Unlock()

	go func() {
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

		if err := playWAVWithMalgo(ctx, resampled); err != nil {
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
func playWAVWithMalgo(ctx context.Context, streamer beep.Streamer) error {
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
			sample := float32(samples[i][0])
			binary.LittleEndian.PutUint32(pOutputSample[offset:], math.Float32bits(sample))
			offset += 4

			sample = float32(samples[i][1])
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
