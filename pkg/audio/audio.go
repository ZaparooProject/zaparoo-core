/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"

	"github.com/gen2brain/malgo"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/wav"
	"github.com/rs/zerolog/log"
)

// PlayWAV plays a WAV audio stream from an io.ReadCloser asynchronously.
// The function returns immediately after starting playback.
// The reader will be closed after playback completes.
// The audio device is created, used, and released for each playback.
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

	// Play asynchronously in a goroutine
	go func() {
		defer func() {
			if err := streamer.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio streamer")
			}
			if err := r.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close audio reader")
			}
		}()

		if err := playWAVWithMalgo(resampled); err != nil {
			log.Warn().Err(err).Msg("failed to play audio")
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

// playWAVWithMalgo plays audio using malgo with proper device lifecycle management.
// The device is initialized, used, and released within this function.
func playWAVWithMalgo(streamer beep.Streamer) error {
	// Create malgo context
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize malgo context: %w", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
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
		mu       sync.Mutex
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
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{
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

	// Wait for playback to complete
	<-done

	// Stop playback
	if err := device.Stop(); err != nil {
		log.Warn().Err(err).Msg("failed to stop audio device")
	}

	return nil
}
