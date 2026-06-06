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
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/gen2brain/malgo"
	"github.com/rs/zerolog/log"
)

type PlaybackOptions struct {
	Volume float64
}

type PlaybackState struct {
	Path     string
	Position time.Duration
	Duration time.Duration
	Playing  bool
	Paused   bool
}

type PlaybackManager interface {
	Play(slot, path string, opts PlaybackOptions) error
	Stop(slot string) error
	Pause(slot string) error
	Resume(slot string) error
	TogglePause(slot string) error
	Seek(slot string, offset time.Duration) error
	State(slot string) PlaybackState
}

type LongformPlaybackManager struct {
	primary    *playbackSlot
	background *playbackSlot
}

type playbackSlot struct {
	cancel  context.CancelFunc
	done    <-chan struct{}
	path    string
	samples [][2]float64
	volume  float64
	pos     int
	mu      syncutil.Mutex
	paused  bool
	playing bool
}

func NewLongformPlaybackManager() *LongformPlaybackManager {
	return &LongformPlaybackManager{
		primary:    &playbackSlot{},
		background: &playbackSlot{},
	}
}

func (m *LongformPlaybackManager) Play(slot, path string, opts PlaybackOptions) error {
	s, err := m.slot(slot)
	if err != nil {
		return err
	}

	//nolint:gosec // G304: callers validate media paths before launching.
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read audio file: %w", err)
	}
	samples, err := decodeBytesByExt(data, strings.ToLower(filepath.Ext(path)))
	if err != nil {
		return err
	}
	volume := opts.Volume
	if volume == 0 {
		volume = 1.0
	}

	s.stop()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	s.mu.Lock()
	s.cancel = cancel
	s.done = done
	s.samples = samples
	s.path = path
	s.volume = volume
	s.pos = 0
	s.paused = false
	s.playing = true
	s.mu.Unlock()

	go func() {
		defer close(done)
		if err := playControlledPCM(ctx, s); err != nil && !errors.Is(err, context.Canceled) {
			log.Warn().Err(err).Str("slot", slot).Msg("long-form audio playback failed")
		}
		s.mu.Lock()
		s.playing = false
		s.cancel = nil
		s.mu.Unlock()
	}()

	return nil
}

func (m *LongformPlaybackManager) Stop(slot string) error {
	s, err := m.slot(slot)
	if err != nil {
		return err
	}
	s.stop()
	return nil
}

func (m *LongformPlaybackManager) Pause(slot string) error {
	s, err := m.slot(slot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	return nil
}

func (m *LongformPlaybackManager) Resume(slot string) error {
	s, err := m.slot(slot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
	return nil
}

func (m *LongformPlaybackManager) TogglePause(slot string) error {
	s, err := m.slot(slot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.paused = !s.paused
	s.mu.Unlock()
	return nil
}

func (m *LongformPlaybackManager) Seek(slot string, offset time.Duration) error {
	s, err := m.slot(slot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pos := s.pos + beepSamples(offset)
	if pos < 0 {
		pos = 0
	}
	if pos > len(s.samples) {
		pos = len(s.samples)
	}
	s.pos = pos
	return nil
}

func (m *LongformPlaybackManager) State(slot string) PlaybackState {
	s, err := m.slot(slot)
	if err != nil {
		return PlaybackState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return PlaybackState{
		Path:     s.path,
		Position: sampleDuration(s.pos),
		Duration: sampleDuration(len(s.samples)),
		Playing:  s.playing,
		Paused:   s.paused,
	}
}

func (m *LongformPlaybackManager) slot(slot string) (*playbackSlot, error) {
	switch slot {
	case "", "primary":
		return m.primary, nil
	case "background":
		return m.background, nil
	default:
		return nil, fmt.Errorf("unsupported media slot: %s", slot)
	}
}

func (s *playbackSlot) stop() {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.playing = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			log.Warn().Msg("timeout waiting for long-form audio cleanup")
		}
	}
}

func beepSamples(d time.Duration) int {
	return int(d.Seconds() * targetSampleRate)
}

func sampleDuration(samples int) time.Duration {
	return time.Duration(samples) * time.Second / targetSampleRate
}

func playControlledPCM(ctx context.Context, slot *playbackSlot) error {
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

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatF32
	deviceConfig.Playback.Channels = 2
	deviceConfig.SampleRate = targetSampleRate
	deviceConfig.Alsa.NoMMap = 1
	deviceConfig.PeriodSizeInMilliseconds = periodSizeInMilliseconds
	deviceConfig.Periods = periodCount

	done := make(chan struct{})
	var (
		mu       syncutil.Mutex
		finished bool
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

		offset := 0
		written := 0
		slot.mu.Lock()
		if !slot.paused {
			for written < int(frameCount) && slot.pos < len(slot.samples) {
				l := float32(slot.samples[slot.pos][0] * slot.volume)
				r := float32(slot.samples[slot.pos][1] * slot.volume)
				binary.LittleEndian.PutUint32(pOutputSample[offset:], math.Float32bits(l))
				offset += 4
				binary.LittleEndian.PutUint32(pOutputSample[offset:], math.Float32bits(r))
				offset += 4
				slot.pos++
				written++
			}
		}
		drained := slot.pos >= len(slot.samples)
		slot.mu.Unlock()

		for i := offset; i < len(pOutputSample); i++ {
			pOutputSample[i] = 0
		}
		if drained && written < int(frameCount) {
			finished = true
			close(done)
		}
	}

	device, err := malgo.InitDevice(malgoCtx.Context, deviceConfig, malgo.DeviceCallbacks{Data: onSamples})
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
	}
	if err := device.Stop(); err != nil {
		log.Warn().Err(err).Msg("failed to stop audio device")
	}
	return ctx.Err()
}
