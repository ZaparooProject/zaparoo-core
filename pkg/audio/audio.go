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

// Package audio provides cross-platform audio playback using beep.
package audio

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/wav"
	"github.com/rs/zerolog/log"
)

var (
	// initOnce ensures speaker initialization happens only once
	initOnce sync.Once
	// initErr stores the result of speaker initialization
	initErr error
)

// Initialize sets up the global audio speaker.
// This should be called once at application startup.
// If initialization fails, audio will be disabled but the application continues.
// Subsequent calls return the cached initialization result.
func Initialize() error {
	initOnce.Do(func() {
		// Initialize speaker with 44100 Hz sample rate and 100ms buffer
		sr := beep.SampleRate(44100)
		err := speaker.Init(sr, sr.N(time.Second/10))
		if err != nil {
			log.Warn().Err(err).Msg("failed to initialize audio speaker - audio will be disabled")
			initErr = fmt.Errorf("failed to initialize speaker: %w", err)
			return
		}

		log.Info().Msg("audio system initialized successfully")
	})

	return initErr
}

// Shutdown cleans up the audio system.
// This should be called during application shutdown.
func Shutdown() error {
	speaker.Clear()
	log.Debug().Msg("audio system shut down")
	return nil
}

// PlayWAV plays a WAV audio stream asynchronously from an io.ReadCloser.
// The function returns immediately after starting playback.
// The reader will be closed after playback completes.
// If the speaker was never initialized, audio simply won't play (no error).
func PlayWAV(r io.ReadCloser) error {
	// Decode WAV stream
	streamer, format, err := wav.Decode(r)
	if err != nil {
		if closeErr := r.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close audio reader on decode error")
		}
		return fmt.Errorf("failed to decode WAV stream: %w", err)
	}

	// Resample if needed (beep handles different sample rates automatically)
	resampled := beep.Resample(4, format.SampleRate, beep.SampleRate(44100), streamer)

	// Play with cleanup callback
	speaker.Play(beep.Seq(resampled, beep.Callback(func() {
		if err := streamer.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close audio streamer")
		}
		if err := r.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close underlying audio reader")
		}
	})))

	log.Debug().Msg("started audio playback")
	return nil
}

// PlayWAVBytes plays a WAV audio file from a byte slice asynchronously.
// The function returns immediately after starting playback.
func PlayWAVBytes(data []byte) error {
	return PlayWAV(io.NopCloser(bytes.NewReader(data)))
}

// PlayWAVFile plays a WAV audio file asynchronously from a file path.
// The function returns immediately after starting playback.
// If the speaker was never initialized, audio simply won't play (no error).
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
