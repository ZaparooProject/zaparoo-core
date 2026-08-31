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
	"math"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/gen2brain/malgo"
	"github.com/rs/zerolog/log"
)

// mixSource is implemented by all audio sources that contribute to the shared output device.
// All methods may be called on the malgo audio thread and must not block or allocate.
type mixSource interface {
	// mixAdd sums up to n stereo frames into buf[0:n] (adding to, not replacing, existing values).
	// Returns frames added and whether the source is permanently exhausted.
	mixAdd(buf [][2]float64, n int) (written int, drained bool)
	// isActive returns false when the source will produce no more audio (e.g. paused).
	// A paused source is still registered; it returns (0, false) from mixAdd.
	isActive() bool
}

type mixSourceEntry struct {
	source  mixSource
	drained atomic.Bool
}

type mixSourceList []*mixSourceEntry

// sharedDevice is a single on-demand malgo output device shared by all audio sources.
// It opens when the first source registers, mixes all sources in the audio callback,
// and releases when all sources finish or are explicitly closed.
// This prevents concurrent ALSA device opens, which crash miniaudio on MiSTer.
//
// Control paths serialize mutations with devMu, then publish an immutable source
// snapshot. The realtime callback only reads that snapshot and never takes devMu.
type sharedDevice struct {
	initContext  func([]malgo.Backend, malgo.ContextConfig, malgo.LogProc) (*malgo.AllocatedContext, error)
	malgoCtx     *malgo.AllocatedContext
	device       *malgo.Device
	cancelFn     context.CancelFunc
	devDone      chan struct{}
	prevDone     <-chan struct{}
	sourceView   atomic.Pointer[mixSourceList]
	sources      []*mixSourceEntry
	mixBuf       [][2]float64
	devMu        syncutil.Mutex
	drainPending atomic.Bool
	opening      bool
}

func newSharedDevice() *sharedDevice {
	d := &sharedDevice{}
	d.publishSourcesLocked()
	return d
}

// publishSourcesLocked replaces the callback's immutable source snapshot.
// Caller must hold devMu unless the device has not yet been published.
func (d *sharedDevice) publishSourcesLocked() {
	view := make(mixSourceList, len(d.sources))
	copy(view, d.sources)
	d.sourceView.Store(&view)
}

// globalDevice is the process-wide audio output device shared by all players.
var globalDevice = newSharedDevice()

// register adds src and opens the device if not already open.
// Safe to call concurrently.
func (d *sharedDevice) register(src mixSource) {
	d.devMu.Lock()
	d.sources = append(d.sources, &mixSourceEntry{source: src})
	d.publishSourcesLocked()
	needOpen := src.isActive() && !d.opening && d.device == nil
	if needOpen {
		d.opening = true
	}
	d.devMu.Unlock()

	if needOpen {
		go d.open()
	}
}

// deregister removes src and closes the device when no sources remain.
func (d *sharedDevice) deregister(src mixSource) {
	d.devMu.Lock()
	removed := d.removeSourceLocked(src)
	empty := len(d.sources) == 0
	d.devMu.Unlock()

	if removed && empty {
		d.closeDevice()
	}
}

func (d *sharedDevice) removeSourceLocked(src mixSource) bool {
	for i, candidate := range d.sources {
		if candidate.source == src {
			d.sources = append(d.sources[:i], d.sources[i+1:]...)
			d.publishSourcesLocked()
			return true
		}
	}
	return false
}

// releaseIfAllPaused closes the device when no source is currently active.
// Call after pausing a source so ALSA is freed when the user pauses background music.
func (d *sharedDevice) releaseIfAllPaused() {
	d.devMu.Lock()
	if d.device == nil {
		d.devMu.Unlock()
		return
	}
	for _, entry := range d.sources {
		if entry.source.isActive() {
			d.devMu.Unlock()
			return
		}
	}
	d.devMu.Unlock()
	d.closeDevice()
}

func hasActiveSource(sources []*mixSourceEntry) bool {
	for _, entry := range sources {
		if entry.source.isActive() {
			return true
		}
	}
	return false
}

// openIfNeeded re-opens the device when a paused source is resumed.
func (d *sharedDevice) openIfNeeded() {
	d.devMu.Lock()
	needOpen := hasActiveSource(d.sources) && !d.opening && d.device == nil
	if needOpen {
		d.opening = true
	}
	d.devMu.Unlock()

	if needOpen {
		go d.open()
	}
}

// closeDevice cancels the current device context and waits for full release.
func (d *sharedDevice) closeDevice() {
	d.devMu.Lock()
	cancelFn := d.cancelFn
	prevDone := d.devDone
	d.cancelFn = nil
	d.devMu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}
	if prevDone != nil {
		select {
		case <-prevDone:
		case <-time.After(3 * time.Second):
			log.Warn().Msg("timeout waiting for audio device release")
		}
	}
}

// failAllSources snapshots all registered sources, clears the list, clears opening,
// and fires fail() on each in a goroutine. Called when the device fails to open so
// prefetch goroutines exit and media state is cleared instead of leaking silently.
func (d *sharedDevice) failAllSources() {
	d.devMu.Lock()
	srcs := make([]mixSource, len(d.sources))
	for i, entry := range d.sources {
		srcs[i] = entry.source
	}
	d.sources = d.sources[:0]
	d.publishSourcesLocked()
	d.opening = false
	d.devMu.Unlock()

	for _, src := range srcs {
		if f, ok := src.(interface{ fail() }); ok {
			go f.fail()
		}
	}
}

// open initialises the malgo context and device, then runs the manager goroutine.
// Runs in its own goroutine; waits for prevDone before opening to serialise ALSA access.
func (d *sharedDevice) open() {
	// Recover from any panic so audio issues never crash the service.
	defer func() {
		if rec := recover(); rec != nil {
			log.Error().Any("panic", rec).Msg("recovered panic in audio device open")
			d.failAllSources()
		}
	}()

	// Serialise: wait for the previous malgo device to fully release ALSA.
	d.devMu.Lock()
	prevDone := d.prevDone
	d.devMu.Unlock()

	if prevDone != nil {
		select {
		case <-prevDone:
		case <-time.After(3 * time.Second):
			log.Warn().Msg("timeout waiting for previous audio device release")
		}
	}

	// If all sources disappeared while we were waiting, bail out.
	d.devMu.Lock()
	if len(d.sources) == 0 {
		d.opening = false
		d.devMu.Unlock()
		return
	}
	d.devMu.Unlock()

	// Realtime priority puts the audio callback thread on SCHED_FIFO (Linux),
	// so playback keeps running when the rest of the process is busy (e.g.
	// heavy API queries on a CPU-constrained device). The default "highest"
	// priority is a no-op under SCHED_OTHER, which has no priority levels.
	// miniaudio falls back to a normal thread if realtime is not permitted.
	ctxConfig := malgo.ContextConfig{ThreadPriority: malgo.ThreadPriorityRealtime}
	initContext := d.initContext
	if initContext == nil {
		initContext = malgo.InitContext
	}
	malgoCtx, err := initContext(nil, ctxConfig, nil)
	if err != nil {
		log.Warn().Err(err).Msg("failed to initialize audio context")
		d.failAllSources()
		return
	}
	if malgoCtx == nil {
		log.Warn().Msg("malgo context is nil after initialization")
		d.failAllSources()
		return
	}

	// Pre-allocate scratch mix buffer — one period's worth of frames.
	// Avoids alloc in the realtime callback.
	maxFrames := periodSizeInMilliseconds * targetSampleRate / 1000
	mixBuf := make([][2]float64, maxFrames)

	ctx, cancelFn := context.WithCancel(context.Background()) //nolint:gocritic
	done := make(chan struct{})

	d.devMu.Lock()
	d.malgoCtx = malgoCtx
	d.cancelFn = cancelFn
	d.devDone = done
	d.prevDone = done // next open waits for this one
	d.mixBuf = mixBuf
	d.devMu.Unlock()

	// F32 format, stereo, fixed sample rate with MiSTer-tuned period settings.
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = 2
	cfg.SampleRate = targetSampleRate
	cfg.Alsa.NoMMap = 1
	cfg.PeriodSizeInMilliseconds = periodSizeInMilliseconds
	cfg.Periods = periodCount

	device, err := malgo.InitDevice(malgoCtx.Context, cfg, malgo.DeviceCallbacks{
		Data: d.onSamples,
	})
	if err != nil {
		log.Warn().Err(err).Msg("failed to initialize audio device")
		d.failAllSources()
		cancelFn()
		go d.cleanup(done, malgoCtx, nil)
		return
	}

	if err := device.Start(); err != nil {
		log.Warn().Err(err).Msg("failed to start audio device")
		device.Uninit()
		d.failAllSources()
		cancelFn()
		go d.cleanup(done, malgoCtx, nil)
		return
	}

	// Clear opening and publish the device pointer atomically so no concurrent
	// register/openIfNeeded can observe opening==false && device==nil.
	d.devMu.Lock()
	d.device = device
	d.opening = false
	d.devMu.Unlock()

	go d.manage(ctx, device, malgoCtx, done)
}

// manage runs while the device is open. It removes sources marked drained by
// the callback and shuts down the device when all sources finish or ctx is cancelled.
func (d *sharedDevice) manage(
	ctx context.Context,
	device *malgo.Device,
	malgoCtx *malgo.AllocatedContext,
	done chan struct{},
) {
	defer d.cleanup(done, malgoCtx, device)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !d.drainPending.Swap(false) {
				continue
			}
			d.devMu.Lock()
			drained := make([]mixSource, 0, len(d.sources))
			remaining := d.sources[:0]
			for _, entry := range d.sources {
				if entry.drained.Load() {
					drained = append(drained, entry.source)
					continue
				}
				remaining = append(remaining, entry)
			}
			if len(drained) > 0 {
				d.sources = remaining
				d.publishSourcesLocked()
			}
			empty := len(d.sources) == 0
			d.devMu.Unlock()

			for _, src := range drained {
				// Notify asynchronously because source cleanup may block on decoder I/O.
				go func(drainedSrc mixSource) {
					if cb, ok := drainedSrc.(interface{ onDrained() }); ok {
						cb.onDrained()
					}
				}(src)
			}

			if len(drained) > 0 && empty {
				return
			}
		}
	}
}

// cleanup stops and uninitialises the device and context.
func (d *sharedDevice) cleanup(
	done chan struct{},
	malgoCtx *malgo.AllocatedContext,
	device *malgo.Device,
) {
	defer close(done)

	if device != nil {
		if err := device.Stop(); err != nil {
			log.Warn().Err(err).Msg("failed to stop audio device")
		}
		device.Uninit()
	}
	if malgoCtx != nil {
		if err := malgoCtx.Uninit(); err != nil {
			log.Warn().Err(err).Msg("failed to uninit malgo context")
		}
		malgoCtx.Free()
	}

	d.devMu.Lock()
	d.device = nil
	d.malgoCtx = nil
	// A source may have registered while a real device was tearing down (register saw
	// device!=nil and skipped open). Re-trigger open so active playback isn't silently orphaned.
	// Guard on device!=nil: init-failure paths call cleanup with device==nil (nothing ran).
	needOpen := device != nil && hasActiveSource(d.sources) && !d.opening
	if needOpen {
		d.opening = true
	}
	d.devMu.Unlock()

	if needOpen {
		go d.open()
	}
}

// onSamples is the malgo audio callback. It mixes all registered sources into the output
// buffer. Called on the malgo audio thread — must not block, allocate, or make syscalls.
func (d *sharedDevice) onSamples(output, _ []byte, frameCount uint32) {
	n := int(frameCount)
	if n > len(d.mixBuf) {
		n = len(d.mixBuf)
	}

	// Zero the scratch buffer before mixing.
	for i := range d.mixBuf[:n] {
		d.mixBuf[i] = [2]float64{}
	}

	if view := d.sourceView.Load(); view != nil {
		for _, entry := range *view {
			_, drained := entry.source.mixAdd(d.mixBuf, n)
			if drained {
				entry.drained.Store(true)
				d.drainPending.Store(true)
			}
		}
	}

	// Convert float64 mix buffer to F32 output bytes, clamping to [-1, 1].
	offset := 0
	for i := range n {
		l := float32(clampF64(d.mixBuf[i][0], -1, 1))
		r := float32(clampF64(d.mixBuf[i][1], -1, 1))
		binary.LittleEndian.PutUint32(output[offset:], math.Float32bits(l))
		offset += 4
		binary.LittleEndian.PutUint32(output[offset:], math.Float32bits(r))
		offset += 4
	}

	// Zero any trailing output bytes we didn't fill.
	for i := offset; i < len(output); i++ {
		output[i] = 0
	}
}

func clampF64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
