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

var (
	// currentCancel cancels the currently playing sound
	currentCancel context.CancelFunc
	// playbackGen tracks the generation of the current playback
	playbackGen uint64
	// playbackMu protects access to currentCancel and playbackGen
	playbackMu syncutil.Mutex
)

// PlayWAV plays a WAV audio stream from an io.ReadCloser asynchronously.
// The function returns immediately after starting playback.
// The reader will be closed after playback completes.
// The audio device is created, used, and released for each playback.
// If another sound is already playing, it will be cancelled and replaced by this sound.
func PlayWAV(r io.ReadCloser) error {
	// Decode WAV stream
	streamer, format, err := wav.Decode(r)
	if err != nil {
		if closeErr := r.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close audio reader on decode error")
		}
		return fmt.Errorf("failed to decode WAV stream: %w", err)
	}

	// Resample to 48000 Hz for compatibility with HDMI audio and systems like MiSTer
	resampled := beep.Resample(4, format.SampleRate, beep.SampleRate(48000), streamer)

	// Cancel any currently playing sound and set up new cancellation
	playbackMu.Lock()
	if currentCancel != nil {
		currentCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	currentCancel = cancel
	playbackGen++
	thisGen := playbackGen
	playbackMu.Unlock()

	// Play asynchronously in a goroutine
	go func() {
		defer func() {
			if err := streamer.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio streamer")
			}
			if err := r.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio reader")
			}
			// Clear current cancel if this is still the active playback
			playbackMu.Lock()
			if playbackGen == thisGen {
				currentCancel = nil
			}
			playbackMu.Unlock()
		}()

		if err := playWAVWithMalgo(ctx, resampled); err != nil {
			// Don't log errors if playback was cancelled
			if ctx.Err() != context.Canceled {
				log.Warn().Err(err).Msg("failed to play audio")
			}
			return
		}

		log.Debug().Msg("completed audio playback")
	}()

	return nil
}

// PlayWAVBytes plays a WAV audio file from a byte slice asynchronously.
// The function returns immediately after starting playback.
func PlayWAVBytes(data []byte) error {
	return PlayWAV(io.NopCloser(bytes.NewReader(data)))
}

// PlayWAVFile plays a WAV audio file from a file path asynchronously.
// The function returns immediately after starting playback.
func PlayWAVFile(path string) error {
	// Open WAV file
	//nolint:gosec // G304: Potential file inclusion via variable path - callers are responsible for path sanitization
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open audio file: %w", err)
	}

	log.Debug().Str("path", path).Msg("started audio playback")
	return PlayWAV(f)
}

// PlayFile plays an audio file from a file path asynchronously.
// Supports WAV, MP3, OGG (Vorbis), and FLAC formats.
// The function returns immediately after starting playback.
// Format is detected by file extension.
// If another sound is already playing, it will be cancelled and replaced by this sound.
func PlayFile(path string) error {
	// Open audio file
	//nolint:gosec // G304: Potential file inclusion via variable path - callers are responsible for path sanitization
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open audio file: %w", err)
	}

	// Detect format by extension
	ext := strings.ToLower(filepath.Ext(path))

	var streamer beep.StreamSeekCloser
	var format beep.Format

	switch ext {
	case ".wav":
		streamer, format, err = wav.Decode(f)
	case ".mp3":
		streamer, format, err = mp3.Decode(f)
	case ".ogg":
		streamer, format, err = vorbis.Decode(f)
	case ".flac":
		streamer, format, err = flac.Decode(f)
	default:
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close file after unsupported format detection")
		}
		return fmt.Errorf("unsupported audio format: %s (supported: .wav, .mp3, .ogg, .flac)", ext)
	}

	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close audio file on decode error")
		}
		return fmt.Errorf("failed to decode audio file: %w", err)
	}

	// Resample to 48000 Hz for compatibility with HDMI audio and systems like MiSTer
	resampled := beep.Resample(4, format.SampleRate, beep.SampleRate(48000), streamer)

	// Cancel any currently playing sound and set up new cancellation
	playbackMu.Lock()
	if currentCancel != nil {
		currentCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	currentCancel = cancel
	playbackGen++
	thisGen := playbackGen
	playbackMu.Unlock()

	// Play asynchronously in a goroutine
	go func() {
		defer func() {
			if err := streamer.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio streamer")
			}
			if err := f.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio file")
			}
			// Clear current cancel if this is still the active playback
			playbackMu.Lock()
			if playbackGen == thisGen {
				currentCancel = nil
			}
			playbackMu.Unlock()
		}()

		if err := playWAVWithMalgo(ctx, resampled); err != nil {
			// Don't log errors if playback was cancelled
			if ctx.Err() != context.Canceled {
				log.Warn().Err(err).Msg("failed to play audio")
			}
			return
		}

		log.Debug().Str("path", path).Msg("completed audio playback")
	}()

	return nil
}

// playWAVWithMalgo plays audio using malgo with proper device lifecycle management.
// The device is initialized, used, and released within this function.
// Playback will be interrupted if ctx is cancelled.
func playWAVWithMalgo(ctx context.Context, streamer beep.Streamer) error {
	// Create malgo context
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

	// Configure device for 48kHz, F32, 2ch (stereo)
	// Using F32 to avoid buggy S16->S32 conversion in miniaudio on PulseAudio
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatF32
	deviceConfig.Playback.Channels = 2
	deviceConfig.SampleRate = 48000
	deviceConfig.Alsa.NoMMap = 1 // Disable mmap for better compatibility

	// Channel to signal completion
	done := make(chan struct{})

	// Buffer to store samples for conversion
	var (
		mu       syncutil.Mutex
		finished bool
		samples  [][2]float64
	)

	// Callback function to feed audio samples
	onSamples := func(pOutputSample, _ []byte, frameCount uint32) {
		mu.Lock()
		defer mu.Unlock()

		if finished {
			return
		}

		// Check if context was cancelled
		select {
		case <-ctx.Done():
			finished = true
			close(done)
			return
		default:
		}

		// Allocate buffer if needed or if too small
		if len(samples) < int(frameCount) {
			samples = make([][2]float64, frameCount)
		}

		// Read samples from beep streamer
		n, ok := streamer.Stream(samples[:frameCount])
		if !ok || n == 0 {
			finished = true
			close(done)
			return
		}

		// Convert beep samples ([][2]float64) to F32 PCM bytes
		offset := 0
		for i := range n {
			// Left channel - convert float64 to float32
			sample := float32(samples[i][0])
			binary.LittleEndian.PutUint32(pOutputSample[offset:], math.Float32bits(sample))
			offset += 4 // 4 bytes per F32 sample

			// Right channel
			sample = float32(samples[i][1])
			binary.LittleEndian.PutUint32(pOutputSample[offset:], math.Float32bits(sample))
			offset += 4
		}

		// Fill remaining buffer with silence if needed
		for i := offset; i < len(pOutputSample); i++ {
			pOutputSample[i] = 0
		}
	}

	// Initialize device
	device, err := malgo.InitDevice(malgoCtx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onSamples,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize audio device: %w", err)
	}
	defer device.Uninit() // Critical: releases ALSA device

	// Start playback
	if err := device.Start(); err != nil {
		return fmt.Errorf("failed to start audio device: %w", err)
	}

	// Wait for playback to complete or context cancellation
	select {
	case <-done:
		// Playback completed normally
	case <-ctx.Done():
		// Context cancelled - mark as finished
		mu.Lock()
		if !finished {
			finished = true
		}
		mu.Unlock()
	}

	// Stop playback
	if err := device.Stop(); err != nil {
		log.Warn().Err(err).Msg("failed to stop audio device")
	}

	// Return context error if cancelled, otherwise nil
	if ctx.Err() == context.Canceled {
		return context.Canceled
	}

	return nil
}
