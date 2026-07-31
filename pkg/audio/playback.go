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
	//
	// If streaming still stutters under heavy load on low-power devices, the
	// remaining untried options are: raising this to 8 s (~6 MB per slot),
	// boosting the prefetch goroutine's thread priority (runtime.LockOSThread
	// plus per-thread setpriority), and prioritizing decode file reads over
	// database queries with ioprio_set.
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
	primary         *streamingSource
	background      *streamingSource
	drainCallbacks  map[string]func(natural bool)
	resampleQuality int
	mu              syncutil.Mutex
}

// NewLongformPlaybackManager creates a new LongformPlaybackManager. lowPowerAudio
// selects a cheaper resampler for streaming decode on CPU-constrained platforms.
func NewLongformPlaybackManager(lowPowerAudio bool) *LongformPlaybackManager {
	quality := resampleQuality
	if lowPowerAudio {
		quality = lowPowerResampleQuality
	}
	return &LongformPlaybackManager{
		drainCallbacks:  make(map[string]func(natural bool)),
		resampleQuality: quality,
	}
}

// SetDrainCallback registers fn to be called when a slot drains.
// natural is true when the track reached EOF on its own; false when stopped or replaced.
// This is not part of the PlaybackManager interface and is called at service startup.
func (m *LongformPlaybackManager) SetDrainCallback(slot string, fn func(natural bool)) {
	key, err := m.slotKey(slot)
	if err != nil {
		return
	}
	m.mu.Lock()
	m.drainCallbacks[key] = fn
	m.mu.Unlock()
}

func (m *LongformPlaybackManager) Play(slot, path string, opts PlaybackOptions) error {
	key, err := m.slotKey(slot)
	if err != nil {
		return err
	}

	volume := opts.Volume
	if volume == 0 {
		volume = 1.0
	}

	src, err := newStreamingSource(path, volume, m.resampleQuality)
	if err != nil {
		return fmt.Errorf("open streaming source: %w", err)
	}

	m.mu.Lock()
	old := m.getSourceLocked(key)
	m.setSourceLocked(key, src)
	m.mu.Unlock()

	// Wire the drain callback on the source before registering with the device,
	// so the callback is in place before the source could possibly drain.
	src.onDrain = func(natural bool) {
		m.mu.Lock()
		if m.getSourceLocked(key) == src {
			m.setSourceLocked(key, nil)
		}
		cb := m.drainCallbacks[key]
		m.mu.Unlock()
		if cb != nil {
			cb(natural)
		}
	}

	// Start decoding before touching the current source or opening ALSA. Device
	// startup and previous-source cleanup then provide time to build a cushion,
	// so the first callback does not race an empty ring.
	src.startPrefetch()
	if old != nil {
		old.stopAndDeregister()
	}

	globalDevice.register(src)
	return nil
}

func (m *LongformPlaybackManager) Stop(slot string) error {
	key, err := m.slotKey(slot)
	if err != nil {
		return err
	}
	m.mu.Lock()
	src := m.getSourceLocked(key)
	m.setSourceLocked(key, nil)
	m.mu.Unlock()
	if src != nil {
		src.stopAndDeregister()
	}
	return nil
}

func (m *LongformPlaybackManager) Pause(slot string) error {
	src, err := m.readSource(slot)
	if err != nil {
		return err
	}
	if src == nil {
		return nil
	}
	src.setPaused(true)
	globalDevice.releaseIfAllPaused()
	return nil
}

func (m *LongformPlaybackManager) Resume(slot string) error {
	src, err := m.readSource(slot)
	if err != nil {
		return err
	}
	if src == nil {
		return nil
	}
	src.setPaused(false)
	globalDevice.openIfNeeded()
	return nil
}

func (m *LongformPlaybackManager) TogglePause(slot string) error {
	src, err := m.readSource(slot)
	if err != nil {
		return err
	}
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
	src, err := m.readSource(slot)
	if err != nil {
		return err
	}
	if src == nil {
		return nil
	}
	src.seek(offset)
	return nil
}

func (m *LongformPlaybackManager) State(slot string) PlaybackState {
	src, err := m.readSource(slot)
	if err != nil || src == nil {
		return PlaybackState{}
	}
	return src.state()
}

// readSource returns the current source for slot without holding the lock long.
func (m *LongformPlaybackManager) readSource(slot string) (*streamingSource, error) {
	key, err := m.slotKey(slot)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	s := m.getSourceLocked(key)
	m.mu.Unlock()
	return s, nil
}

// getSourceLocked and setSourceLocked take a normalized slot key from slotKey,
// never a raw slot string (aliases like "bg" would silently miss otherwise).
func (m *LongformPlaybackManager) getSourceLocked(key string) *streamingSource {
	switch key {
	case mediaslot.Primary:
		return m.primary
	case mediaslot.Background:
		return m.background
	}
	return nil
}

func (m *LongformPlaybackManager) setSourceLocked(key string, src *streamingSource) {
	switch key {
	case mediaslot.Primary:
		m.primary = src
	case mediaslot.Background:
		m.background = src
	}
}

func (*LongformPlaybackManager) slotKey(slot string) (string, error) {
	key, err := mediaslot.Normalize(slot)
	if err != nil {
		return "", fmt.Errorf("normalize slot: %w", err)
	}
	return key, nil
}

// streamingSource is a long-form audio source backed by a ring buffer prefilled by a
// background goroutine. This bounds memory use to ringBufferFrames regardless of
// file length and keeps decoding off the malgo audio thread.
type streamingSource struct {
	resampler      beep.Streamer
	decoder        beep.StreamSeekCloser
	onDrain        func(natural bool)
	file           *os.File
	wakeCh         chan struct{}
	doneCh         chan struct{}
	cancelFn       context.CancelFunc
	path           string
	ring           [][2]float64
	chunk          [][2]float64
	volume         float64
	totalFrames    int64
	sourceRate     int
	quality        int
	mu             syncutil.Mutex
	seekMu         syncutil.Mutex
	readPos        atomic.Uint64
	writePos       atomic.Uint64
	played         atomic.Int64
	seekSrcFrame   atomic.Int64
	underruns      atomic.Uint64
	seekPending    atomic.Bool
	consumerActive atomic.Bool
	underrunActive atomic.Bool
	eof            atomic.Bool
	stopped        atomic.Bool
	paused         atomic.Bool
}

// newStreamingSource opens path for streaming decode and returns a ready source.
// The prefetch goroutine is not started yet; start it before device registration.
func newStreamingSource(path string, volume float64, quality int) (*streamingSource, error) {
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

	resampler := beep.Resample(quality, format.SampleRate, beep.SampleRate(targetSampleRate), decoder)

	return &streamingSource{
		ring:        make([][2]float64, ringBufferFrames),
		path:        path,
		volume:      volume,
		totalFrames: totalFrames,
		sourceRate:  int(format.SampleRate),
		quality:     quality,
		wakeCh:      make(chan struct{}, 1),
		decoder:     decoder,
		file:        f,
		resampler:   resampler,
		chunk:       make([][2]float64, decodeChunkFrames),
	}, nil
}

// startPrefetch launches the background goroutine that fills the ring buffer.
// Call before device registration so playback starts with buffered audio.
func (s *streamingSource) startPrefetch() {
	ctx, cancelFn := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.cancelFn = cancelFn
	s.doneCh = done
	s.mu.Unlock()
	go s.prefetch(ctx, done)
}

// prefetch runs in a background goroutine, filling the single-producer,
// single-consumer ring buffer from the decoder.
func (s *streamingSource) prefetch(ctx context.Context, done chan struct{}) {
	defer close(done)
	defer func() {
		if err := s.decoder.Close(); err != nil {
			log.Warn().Err(err).Str("path", s.path).Msg("close audio decoder")
		}
		if err := s.file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			log.Warn().Err(err).Str("path", s.path).Msg("close audio file")
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var reportedUnderruns uint64

prefetchLoop:
	for {
		if s.stopped.Load() {
			return
		}

		if s.seekPending.Load() {
			s.seekMu.Lock()
			seekFrame := s.seekSrcFrame.Load()
			if err := s.decoder.Seek(int(seekFrame)); err != nil {
				log.Warn().Err(err).Str("path", s.path).Msg("seek audio decoder")
			}
			s.resampler = beep.Resample(s.quality, beep.SampleRate(s.sourceRate),
				beep.SampleRate(targetSampleRate), s.decoder)
			s.readPos.Store(0)
			s.writePos.Store(0)
			s.eof.Store(false)
			s.underrunActive.Store(false)
			s.seekPending.Store(false)
			s.seekMu.Unlock()
		}

		if count := s.underruns.Load(); count > reportedUnderruns {
			log.Warn().
				Str("path", s.path).
				Uint64("underruns", count).
				Int("bufferedFrames", s.bufferedFrames()).
				Msg("audio stream buffer underrun")
			reportedUnderruns = count
		}

		// Fill all available ring space before sleeping. The producer writes frame
		// data before publishing writePos; the callback consumes published frames
		// and advances readPos. Neither side takes a mutex.
		for !s.paused.Load() && !s.eof.Load() && !s.stopped.Load() {
			if s.seekPending.Load() {
				continue prefetchLoop
			}
			readPos := s.readPos.Load()
			writePos := s.writePos.Load()
			buffered := writePos - readPos
			if buffered >= uint64(len(s.ring)) {
				break
			}

			space := len(s.ring) - boundedFrameCount(buffered, len(s.ring))
			n := min(space, len(s.chunk))
			written, ok := s.resampler.Stream(s.chunk[:n])
			if s.seekPending.Load() {
				continue prefetchLoop
			}
			if written > 0 {
				for i := range written {
					s.ring[(writePos+uint64(i))%uint64(len(s.ring))] = s.chunk[i]
				}
				s.writePos.Store(writePos + uint64(written))
			}
			if !ok {
				if err := s.decoder.Err(); err != nil {
					log.Warn().Err(err).Str("path", s.path).Msg("audio decode error")
				}
				// Publish EOF only after final frames. Loading EOF before writePos
				// lets the consumer observe every frame before reporting drained.
				s.eof.Store(true)
				break
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-s.wakeCh:
		case <-ticker.C:
		}
	}
}

func boundedFrameCount(frames uint64, limit int) int {
	if limit <= 0 || frames == 0 {
		return 0
	}
	if frames >= uint64(limit) {
		return limit
	}
	return int(frames) //nolint:gosec // frames is proven positive and below the platform-sized limit.
}

func (s *streamingSource) bufferedFrames() int {
	if s.seekPending.Load() {
		return 0
	}
	readPos := s.readPos.Load()
	writePos := s.writePos.Load()
	if writePos <= readPos {
		return 0
	}
	buffered := writePos - readPos
	return boundedFrameCount(buffered, len(s.ring))
}

func (s *streamingSource) wakePrefetch() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

// mixAdd implements mixSource. Called on the malgo realtime thread; this is the
// single ring consumer and performs no blocking synchronization or allocation.
func (s *streamingSource) mixAdd(buf [][2]float64, n int) (int, bool) {
	if s.stopped.Load() {
		return 0, true
	}
	if s.paused.Load() || s.seekPending.Load() {
		return 0, false
	}

	s.consumerActive.Store(true)
	if s.seekPending.Load() {
		s.consumerActive.Store(false)
		return 0, false
	}

	// Load EOF before writePos. When EOF is true, the producer's final writePos
	// publication is guaranteed to be visible before deciding the source drained.
	eof := s.eof.Load()
	readPos := s.readPos.Load()
	writePos := s.writePos.Load()
	available := writePos - readPos
	written := boundedFrameCount(available, n)
	for i := range written {
		sample := s.ring[(readPos+uint64(i))%uint64(len(s.ring))]
		buf[i][0] += sample[0] * s.volume
		buf[i][1] += sample[1] * s.volume
	}
	if written > 0 {
		readPos += uint64(written)
		s.readPos.Store(readPos)
		s.played.Add(int64(written))
		s.underrunActive.Store(false)
	}

	if written < n && !eof && s.underrunActive.CompareAndSwap(false, true) {
		s.underruns.Add(1)
	}

	if !eof {
		s.consumerActive.Store(false)
		return written, false
	}
	// Re-read writePos after EOF so final producer frames cannot be skipped.
	drained := s.writePos.Load() == readPos
	s.consumerActive.Store(false)
	return written, drained
}

// isActive returns false when paused (contributing silence) so the device can be
// released when all sources are idle.
func (s *streamingSource) isActive() bool {
	return !s.paused.Load() && !s.stopped.Load()
}

// fail cancels the prefetch goroutine and fires the drain callback, used when the
// device fails to open. Does not block waiting for the goroutine to exit.
func (s *streamingSource) fail() {
	s.stopped.Store(true)
	s.mu.Lock()
	cancelFn := s.cancelFn
	s.mu.Unlock()
	if cancelFn != nil {
		cancelFn()
	}
	s.onDrained()
}

// onDrained is called by the device manager goroutine when this source drains.
// It determines whether the drain was natural (track reached EOF) or explicit
// (Stop/replace), then cancels the prefetch goroutine, which parks at EOF while
// the ring tail plays out and would otherwise leak.
func (s *streamingSource) onDrained() {
	natural := !s.stopped.Swap(true)
	s.mu.Lock()
	cancelFn := s.cancelFn
	s.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}
	if s.onDrain != nil {
		s.onDrain(natural)
	}
}

// setPaused sets the paused flag.
func (s *streamingSource) setPaused(paused bool) {
	s.paused.Store(paused)
	// Wake the prefetch goroutine on resume so it can refill the ring.
	if !paused {
		s.wakePrefetch()
	}
}

// togglePause flips the paused state and returns the new value.
func (s *streamingSource) togglePause() bool {
	for {
		wasPaused := s.paused.Load()
		nowPaused := !wasPaused
		if !s.paused.CompareAndSwap(wasPaused, nowPaused) {
			continue
		}
		if !nowPaused {
			s.wakePrefetch()
		}
		return nowPaused
	}
}

// seek schedules a seek to the given offset from the current position.
// The ring buffer is flushed atomically; a brief silence occurs during the re-fill.
func (s *streamingSource) seek(offset time.Duration) {
	s.seekMu.Lock()
	defer s.seekMu.Unlock()

	// Gate new callback reads, then wait for any callback already consuming the
	// ring to finish before replacing playback position and decoder state.
	s.seekPending.Store(true)
	for s.consumerActive.Load() {
		time.Sleep(time.Millisecond)
	}

	newPlayed := s.played.Load() + int64(offset.Seconds()*targetSampleRate)
	if newPlayed < 0 {
		newPlayed = 0
	}
	if s.totalFrames > 0 && newPlayed > s.totalFrames {
		newPlayed = s.totalFrames
	}
	srcFrame := int64(float64(newPlayed) / targetSampleRate * float64(s.sourceRate))
	s.seekSrcFrame.Store(srcFrame)
	s.played.Store(newPlayed)
	s.eof.Store(false)
	s.wakePrefetch()
}

// stopAndDeregister cancels the prefetch goroutine and removes this source from
// the shared device. Blocks until the prefetch goroutine exits.
func (s *streamingSource) stopAndDeregister() {
	s.stopped.Store(true)
	s.mu.Lock()
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
	paused := s.paused.Load()
	stopped := s.stopped.Load()
	eof := s.eof.Load()
	return PlaybackState{
		Path:     s.path,
		Position: sampleDuration(int(s.played.Load())),
		Duration: sampleDuration(int(s.totalFrames)),
		Playing:  !paused && !stopped && (!eof || s.bufferedFrames() != 0),
		Paused:   paused,
	}
}

func sampleDuration(samples int) time.Duration {
	if samples <= 0 {
		return 0
	}
	return time.Duration(samples) * time.Second / targetSampleRate
}
