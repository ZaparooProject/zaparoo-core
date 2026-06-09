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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
	"github.com/rs/zerolog/log"
)

const (
	// ringBufferFrames is the streaming ring buffer size (4 s × 48 kHz = 192 000 frames,
	// ~3 MB per slot). This bounds memory use regardless of track length.
	ringBufferFrames = 4 * targetSampleRate
	// decodeChunkFrames is the number of frames decoded per prefetch iteration (100 ms).
	decodeChunkFrames = 100 * targetSampleRate / 1000
)

// PlaybackOptions configures a long-form Play call.
type PlaybackOptions struct {
	// Volume is the playback gain (0.0–2.0); 0 defaults to 1.0.
	Volume float64
}

// PlaybackState describes the current state of a slot.
type PlaybackState struct {
	Path     string
	Position time.Duration
	Duration time.Duration
	Playing  bool
	Paused   bool
}

// PlaybackManager is the interface for managing long-form audio slots.
type PlaybackManager interface {
	Play(slot, path string, opts PlaybackOptions) error
	Stop(slot string) error
	Pause(slot string) error
	Resume(slot string) error
	TogglePause(slot string) error
	Seek(slot string, offset time.Duration) error
	State(slot string) PlaybackState
}

// LongformPlaybackManager manages primary and background long-form audio slots.
// Each slot streams audio through the shared global output device via a ring-buffer
// prefetch goroutine — no full-file decode into RAM.
type LongformPlaybackManager struct {
	primary        *streamingSource
	background     *streamingSource
	drainCallbacks map[string]func()
	mu             syncutil.Mutex
}

// NewLongformPlaybackManager creates a new LongformPlaybackManager.
func NewLongformPlaybackManager() *LongformPlaybackManager {
	return &LongformPlaybackManager{
		drainCallbacks: make(map[string]func()),
	}
}

// SetDrainCallback registers fn to be called when slot drains naturally (track ends).
// This is not part of the PlaybackManager interface and is called at service startup.
func (m *LongformPlaybackManager) SetDrainCallback(slot string, fn func()) {
	m.mu.Lock()
	m.drainCallbacks[slot] = fn
	m.mu.Unlock()
}

func (m *LongformPlaybackManager) Play(slot, path string, opts PlaybackOptions) error {
	if _, err := m.slotKey(slot); err != nil {
		return err
	}

	volume := opts.Volume
	if volume == 0 {
		volume = 1.0
	}

	src, err := newStreamingSource(path, volume)
	if err != nil {
		return fmt.Errorf("open streaming source: %w", err)
	}

	m.mu.Lock()
	old := m.getSourceLocked(slot)
	m.setSourceLocked(slot, src)
	drainCb := m.drainCallbacks[slot]
	m.mu.Unlock()

	// Wire the drain callback on the source before registering with the device,
	// so the callback is in place before the source could possibly drain.
	src.onDrain = func() {
		m.mu.Lock()
		if m.getSourceLocked(slot) == src {
			m.setSourceLocked(slot, nil)
		}
		cb := m.drainCallbacks[slot]
		m.mu.Unlock()
		_ = drainCb // capture for correctness; use the live callback from the map
		if cb != nil {
			cb()
		}
	}

	// Stop and deregister the previous source for this slot.
	if old != nil {
		old.stopAndDeregister()
	}

	globalDevice.register(src)
	src.startPrefetch()
	return nil
}

func (m *LongformPlaybackManager) Stop(slot string) error {
	if _, err := m.slotKey(slot); err != nil {
		return err
	}
	m.mu.Lock()
	src := m.getSourceLocked(slot)
	m.setSourceLocked(slot, nil)
	m.mu.Unlock()
	if src != nil {
		src.stopAndDeregister()
	}
	return nil
}

func (m *LongformPlaybackManager) Pause(slot string) error {
	if _, err := m.slotKey(slot); err != nil {
		return err
	}
	src := m.readSource(slot)
	if src == nil {
		return nil
	}
	src.setPaused(true)
	globalDevice.releaseIfAllPaused()
	return nil
}

func (m *LongformPlaybackManager) Resume(slot string) error {
	if _, err := m.slotKey(slot); err != nil {
		return err
	}
	src := m.readSource(slot)
	if src == nil {
		return nil
	}
	src.setPaused(false)
	globalDevice.openIfNeeded()
	return nil
}

func (m *LongformPlaybackManager) TogglePause(slot string) error {
	if _, err := m.slotKey(slot); err != nil {
		return err
	}
	src := m.readSource(slot)
	if src == nil {
		return nil
	}
	nowPaused := src.togglePause()
	if nowPaused {
		globalDevice.releaseIfAllPaused()
	} else {
		globalDevice.openIfNeeded()
	}
	return nil
}

func (m *LongformPlaybackManager) Seek(slot string, offset time.Duration) error {
	if _, err := m.slotKey(slot); err != nil {
		return err
	}
	src := m.readSource(slot)
	if src == nil {
		return nil
	}
	src.seek(offset)
	return nil
}

func (m *LongformPlaybackManager) State(slot string) PlaybackState {
	src := m.readSource(slot)
	if src == nil {
		return PlaybackState{}
	}
	return src.state()
}

// readSource returns the current source for slot without holding the lock long.
func (m *LongformPlaybackManager) readSource(slot string) *streamingSource {
	m.mu.Lock()
	s := m.getSourceLocked(slot)
	m.mu.Unlock()
	return s
}

func (m *LongformPlaybackManager) getSourceLocked(slot string) *streamingSource {
	switch slot {
	case "", mediaslot.Primary:
		return m.primary
	case mediaslot.Background:
		return m.background
	}
	return nil
}

func (m *LongformPlaybackManager) setSourceLocked(slot string, src *streamingSource) {
	switch slot {
	case "", mediaslot.Primary:
		m.primary = src
	case mediaslot.Background:
		m.background = src
	}
}

func (*LongformPlaybackManager) slotKey(slot string) (string, error) {
	switch slot {
	case "", mediaslot.Primary:
		return mediaslot.Primary, nil
	case mediaslot.Background:
		return mediaslot.Background, nil
	default:
		return "", fmt.Errorf("unsupported media slot: %s", slot)
	}
}

// streamingSource is a long-form audio source backed by a ring buffer prefilled by a
// background goroutine. This bounds memory use to ringBufferFrames regardless of
// file length and keeps decoding off the malgo audio thread.
type streamingSource struct {
	resampler    beep.Streamer
	decoder      beep.StreamSeekCloser
	onDrain      func()
	file         *os.File
	wakeCh       chan struct{}
	doneCh       chan struct{}
	cancelFn     context.CancelFunc
	path         string
	ring         [][2]float64
	chunk        [][2]float64
	volume       float64
	totalFrames  int64
	sourceRate   int
	seekSrcFrame int64
	played       int64
	filled       int
	wpos         int
	rpos         int
	mu           syncutil.Mutex
	seekPending  bool
	eof          bool
	stopped      bool
	paused       bool
}

// newStreamingSource opens path for streaming decode and returns a ready source.
// The prefetch goroutine is NOT started yet — call startPrefetch after registering
// with the device.
func newStreamingSource(path string, volume float64) (*streamingSource, error) {
	//nolint:gosec // G304: callers validate media paths before launching.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	var (
		decoder beep.StreamSeekCloser
		format  beep.Format
	)
	switch ext {
	case ".wav":
		decoder, format, err = wav.Decode(f)
	case ".mp3":
		decoder, format, err = mp3.Decode(f)
	case ".ogg":
		decoder, format, err = vorbis.Decode(f)
	case ".flac":
		decoder, format, err = flac.Decode(f)
	default:
		_ = f.Close()
		return nil, fmt.Errorf("unsupported audio format: %s (supported: .wav, .mp3, .ogg, .flac)", ext)
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("decode %s: %w", ext, err)
	}

	totalFrames := int64(-1)
	if n := decoder.Len(); n >= 0 {
		totalFrames = int64(float64(n) * float64(targetSampleRate) / float64(format.SampleRate))
	}

	resampler := beep.Resample(resampleQuality, format.SampleRate, beep.SampleRate(targetSampleRate), decoder)

	return &streamingSource{
		ring:        make([][2]float64, ringBufferFrames),
		path:        path,
		volume:      volume,
		totalFrames: totalFrames,
		sourceRate:  int(format.SampleRate),
		wakeCh:      make(chan struct{}, 1),
		decoder:     decoder,
		file:        f,
		resampler:   resampler,
		chunk:       make([][2]float64, decodeChunkFrames),
	}, nil
}

// startPrefetch launches the background goroutine that fills the ring buffer.
// Must be called after the source is registered with the device.
func (s *streamingSource) startPrefetch() {
	ctx, cancelFn := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.cancelFn = cancelFn
	s.doneCh = done
	s.mu.Unlock()
	go s.prefetch(ctx, done)
}

// prefetch runs in a background goroutine, filling the ring buffer from the decoder.
func (s *streamingSource) prefetch(ctx context.Context, done chan struct{}) {
	defer close(done)
	defer func() {
		if err := s.decoder.Close(); err != nil {
			log.Warn().Err(err).Str("path", s.path).Msg("close audio decoder")
		}
		if err := s.file.Close(); err != nil {
			log.Warn().Err(err).Str("path", s.path).Msg("close audio file")
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		// Handle a pending seek: flush ring, reposition decoder, rebuild resampler.
		s.mu.Lock()
		seekPending := s.seekPending
		seekFrame := s.seekSrcFrame
		if seekPending {
			s.seekPending = false
			s.rpos = 0
			s.wpos = 0
			s.filled = 0
			s.eof = false
		}
		paused := s.paused
		stopped := s.stopped
		space := len(s.ring) - s.filled
		s.mu.Unlock()

		if stopped {
			return
		}
		if seekPending {
			if err := s.decoder.Seek(int(seekFrame)); err != nil {
				log.Warn().Err(err).Str("path", s.path).Msg("seek audio decoder")
			}
			s.resampler = beep.Resample(resampleQuality, beep.SampleRate(s.sourceRate),
				beep.SampleRate(targetSampleRate), s.decoder)
		}

		if !paused && space > 0 {
			n := min(space, len(s.chunk))
			written, ok := s.resampler.Stream(s.chunk[:n])
			if written > 0 {
				s.mu.Lock()
				for i := range written {
					s.ring[s.wpos] = s.chunk[i]
					s.wpos = (s.wpos + 1) % len(s.ring)
				}
				s.filled += written
				s.mu.Unlock()
			}
			if !ok {
				// Decoder exhausted; let ring drain naturally.
				s.mu.Lock()
				s.eof = true
				s.mu.Unlock()
				return
			}
		}

		// Sleep until the ring needs refilling or a seek arrives.
		select {
		case <-ctx.Done():
			return
		case <-s.wakeCh:
		case <-ticker.C:
		}
	}
}

// mixAdd implements mixSource. Called on the malgo audio thread; must not block or alloc.
func (s *streamingSource) mixAdd(buf [][2]float64, n int) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return 0, true
	}
	if s.paused || s.filled == 0 {
		// Drained only when the prefetch finished AND the ring is empty.
		return 0, s.eof && s.filled == 0
	}

	written := 0
	for written < n && s.filled > 0 {
		buf[written][0] += s.ring[s.rpos][0] * s.volume
		buf[written][1] += s.ring[s.rpos][1] * s.volume
		s.rpos = (s.rpos + 1) % len(s.ring)
		s.filled--
		s.played++
		written++
	}

	// Kick the prefetch goroutine when the ring drops below 25 % capacity.
	if s.filled < len(s.ring)/4 && !s.eof {
		select {
		case s.wakeCh <- struct{}{}:
		default:
		}
	}

	return written, s.eof && s.filled == 0
}

// isActive returns false when paused (contributing silence) so the device can be
// released when all sources are idle.
func (s *streamingSource) isActive() bool {
	s.mu.Lock()
	active := !s.paused && !s.stopped
	s.mu.Unlock()
	return active
}

// onDrained is called by the device manager goroutine when this source drains.
func (s *streamingSource) onDrained() {
	if s.onDrain != nil {
		s.onDrain()
	}
}

// setPaused sets the paused flag.
func (s *streamingSource) setPaused(paused bool) {
	s.mu.Lock()
	s.paused = paused
	s.mu.Unlock()
	// Wake the prefetch goroutine on resume so it can refill the ring.
	if !paused {
		select {
		case s.wakeCh <- struct{}{}:
		default:
		}
	}
}

// togglePause flips the paused state and returns the new value.
func (s *streamingSource) togglePause() bool {
	s.mu.Lock()
	s.paused = !s.paused
	nowPaused := s.paused
	s.mu.Unlock()
	if !nowPaused {
		select {
		case s.wakeCh <- struct{}{}:
		default:
		}
	}
	return nowPaused
}

// seek schedules a seek to the given offset from the current position.
// The ring buffer is flushed atomically; a brief silence occurs during the re-fill.
func (s *streamingSource) seek(offset time.Duration) {
	s.mu.Lock()
	newPlayed := s.played + int64(offset.Seconds()*targetSampleRate)
	if newPlayed < 0 {
		newPlayed = 0
	}
	if s.totalFrames > 0 && newPlayed > s.totalFrames {
		newPlayed = s.totalFrames
	}
	srcFrame := int64(float64(newPlayed) / targetSampleRate * float64(s.sourceRate))
	s.seekPending = true
	s.seekSrcFrame = srcFrame
	s.played = newPlayed
	// Flush ring so the callback returns silence until the prefetch refills it.
	s.rpos = 0
	s.wpos = 0
	s.filled = 0
	s.eof = false
	s.mu.Unlock()

	// Wake the prefetch goroutine immediately to process the seek.
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

// stopAndDeregister cancels the prefetch goroutine and removes this source from
// the shared device. Blocks until the prefetch goroutine exits.
func (s *streamingSource) stopAndDeregister() {
	s.mu.Lock()
	s.stopped = true
	cancelFn := s.cancelFn
	doneCh := s.doneCh
	s.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}
	if doneCh != nil {
		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
			log.Warn().Str("path", s.path).Msg("timeout waiting for audio prefetch cleanup")
		}
	}

	globalDevice.deregister(s)
}

// state returns a snapshot of the current playback state.
func (s *streamingSource) state() PlaybackState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return PlaybackState{
		Path:     s.path,
		Position: sampleDuration(int(s.played)),
		Duration: sampleDuration(int(s.totalFrames)),
		Playing:  !s.paused && !s.stopped && (!s.eof || s.filled != 0),
		Paused:   s.paused,
	}
}

func sampleDuration(samples int) time.Duration {
	if samples <= 0 {
		return 0
	}
	return time.Duration(samples) * time.Second / targetSampleRate
}
