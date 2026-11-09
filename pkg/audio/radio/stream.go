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

package radio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// Buffer limits.
const (
	// ringSeconds is how much decoded audio the stream buffers.
	ringSeconds = 4
	// maxRingFrames caps the buffer for high sample rates: 4 MiB.
	maxRingFrames = 1 << 18
	// minRate and maxRate bound the sample rates a stream may have.
	minRate = 8000
	maxRate = 192000
	// endedSlack is added to Buffered once no more audio will arrive, so a
	// reader that pulls at most Buffered frames still pulls past the end
	// and sees it.
	endedSlack = 1 << 20
)

var (
	// errEnded is a stream that finished: an HLS playlist with an end.
	errEnded = errors.New("radio: stream ended")
	// errSuspended stops the producer when Suspend drops the connection.
	errSuspended = errors.New("radio: stream suspended")
	// errIdle is a connection that sent nothing for too long.
	errIdle = errors.New("radio: no data from server")
)

// permanentError is a failure reconnecting cannot fix.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// source is where a Stream's audio comes from. run plays from the network
// into sk until it fails, the stream ends (errEnded) or ctx is done. It is
// called again to reconnect; fresh says to start at the live point, as
// after a Suspend.
type source interface {
	run(ctx context.Context, sk *sink, fresh bool) error
}

// Stream is a playing radio stream: a beep.StreamSeekCloser whose audio a
// background goroutine reads from the network, decodes and buffers.
// Stream never blocks on the network.
type Stream struct {
	err        error
	ctx        context.Context
	src        source
	cancel     context.CancelFunc
	connCancel context.CancelCauseFunc
	ready      chan struct{}
	done       chan struct{}
	space      chan struct{}
	resume     chan struct{}
	name       string
	ring       [][2]float64
	timing     timing
	opts       Options
	head       int
	count      int
	position   int
	rate       int
	gen        uint64
	mu         syncutil.Mutex
	readyOnce  bool
	ended      bool
	closed     bool
	suspended  bool
}

// Open starts playing a probed stream and takes ownership of p. It
// resolves a playlist to the first entry that plays, and returns once
// audio is decoded, so the sample rate is known; ctx bounds only that
// wait. It reuses the probe's connection when it is still usable.
func Open(ctx context.Context, p *Probe, opts Options) (*Stream, error) {
	if p == nil {
		return nil, errors.New("radio: nil probe")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = NewHTTPClient()
	}
	opts.timing = opts.timing.withDefaults()
	openCtx, cancel := context.WithTimeout(ctx, opts.timing.open)
	defer cancel()

	src, name, err := resolveSource(openCtx, p, opts, 0)
	if err != nil {
		return nil, err
	}
	s := newStream(src, opts)
	s.name = name
	go s.loop()

	select {
	case <-s.ready:
	case <-openCtx.Done():
		_ = s.Close()
		return nil, fmt.Errorf("radio: no audio from %s: %w", p.URL(), context.Cause(openCtx))
	}
	s.mu.Lock()
	err, rate := s.err, s.rate
	s.mu.Unlock()
	if rate == 0 {
		// The stream ended or failed before any audio. Once there is
		// some, a later failure is reported by Stream and Err instead.
		if err == nil {
			err = fmt.Errorf("radio: %s ended without audio", p.URL())
		}
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func newStream(src source, opts Options) *Stream {
	ctx, cancel := context.WithCancel(context.Background())
	return &Stream{
		src:    src,
		opts:   opts,
		timing: opts.timing.withDefaults(),
		ctx:    ctx,
		cancel: cancel,
		ready:  make(chan struct{}),
		done:   make(chan struct{}),
		space:  make(chan struct{}, 1),
		resume: make(chan struct{}, 1),
	}
}

// resolveSource turns a probe into something to play, trying a playlist's
// entries in order. It closes p.
func resolveSource(ctx context.Context, p *Probe, opts Options, depth int) (source, string, error) {
	switch p.Kind() {
	case KindStream:
		reconnectURL := p.requested
		if reconnectURL == "" {
			reconnectURL = p.URL()
		}
		return &icySource{opts: opts, url: reconnectURL, probe: p}, "", nil
	case KindHLS:
		body, u := p.body, p.url
		p.Close()
		return newHLSSource(opts, u, body), "", nil
	case KindPlaylist:
		body, u := p.body, p.url
		p.Close()
		if depth >= maxPlaylistDepth {
			return nil, "", fmt.Errorf("radio: playlists nested deeper than %d", maxPlaylistDepth)
		}
		entries := parsePLS(body, u)
		if !isPLS(body) {
			entries = parseM3U(body, u)
		}
		if len(entries) == 0 {
			return nil, "", fmt.Errorf("radio: playlist %s has no stream urls", u)
		}
		var errs []error
		for _, e := range entries {
			if ctx.Err() != nil {
				break
			}
			child, err := ProbeURL(ctx, opts.HTTPClient, e.url)
			if err != nil {
				log.Debug().Err(err).Str("url", e.url).Msg("radio: playlist entry failed")
				errs = append(errs, err)
				continue
			}
			if child.Kind() == KindNotAudio {
				child.Close()
				errs = append(errs, fmt.Errorf("radio: %s is not audio", e.url))
				continue
			}
			src, name, err := resolveSource(ctx, child, opts, depth+1)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if name == "" {
				name = e.title
			}
			return src, name, nil
		}
		if err := context.Cause(ctx); err != nil {
			errs = append(errs, err)
		}
		return nil, "", fmt.Errorf("radio: no playable entry in playlist %s: %w", u, errors.Join(errs...))
	default:
		p.Close()
		return nil, "", fmt.Errorf("radio: %s is not an audio stream", p.URL())
	}
}

// loop runs the source, reconnecting with backoff, until the stream ends,
// fails for good or is closed.
func (s *Stream) loop() {
	defer close(s.done)
	defer s.markReady()
	if c, ok := s.src.(interface{ close() }); ok {
		defer c.close()
	}
	attempt := 0
	fresh := false
	var lastGen uint64
	for {
		gen, ok := s.waitResumed()
		if !ok {
			return
		}
		// A pause and resume while waiting to reconnect still joins the
		// stream at the live point.
		if gen != lastGen {
			fresh = true
			lastGen = gen
		}
		connCtx, cancel := context.WithCancelCause(s.ctx)
		s.mu.Lock()
		if s.gen != gen {
			s.mu.Unlock()
			cancel(errSuspended)
			continue
		}
		s.connCancel = cancel
		s.mu.Unlock()

		sk := newSink(s, gen)
		err := s.src.run(connCtx, sk, fresh)
		if cause := context.Cause(connCtx); cause != nil && s.ctx.Err() == nil && !errors.Is(err, errEnded) {
			err = cause
		}
		cancel(nil)
		sk.close()
		fresh = false

		if s.ctx.Err() != nil {
			return
		}
		if errors.Is(err, errSuspended) {
			fresh = true
			attempt = 0
			continue
		}
		if errors.Is(err, errEnded) {
			s.finish(nil)
			return
		}
		var perm *permanentError
		if errors.As(err, &perm) {
			s.finish(perm.err)
			return
		}
		if sk.played() {
			attempt = 0
		}
		if errors.Is(err, errStaleProbe) {
			// The probe's connection went stale before it gave any audio:
			// connect again at once.
			continue
		}
		if attempt >= len(s.timing.retry) {
			s.finish(fmt.Errorf("radio: giving up after %d reconnects: %w", attempt, err))
			return
		}
		wait := s.timing.retry[attempt]
		attempt++
		log.Warn().Err(err).Dur("retryIn", wait).Msg("radio: stream dropped, reconnecting")
		t := time.NewTimer(wait)
		select {
		case <-s.ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// waitResumed blocks while the stream is suspended, returning the current
// generation, or false once the stream is closed.
func (s *Stream) waitResumed() (uint64, bool) {
	for {
		s.mu.Lock()
		suspended, gen := s.suspended, s.gen
		s.mu.Unlock()
		if !suspended {
			return gen, true
		}
		select {
		case <-s.ctx.Done():
			return 0, false
		case <-s.resume:
		}
	}
}

// finish ends the stream: err nil is a clean end, anything else a failure.
func (s *Stream) finish(err error) {
	s.mu.Lock()
	if err != nil {
		s.err = err
		log.Error().Err(err).Msg("radio: stream failed")
	}
	s.ended = true
	s.mu.Unlock()
	s.markReady()
}

func (s *Stream) markReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.readyOnce {
		s.readyOnce = true
		close(s.ready)
	}
}

// setName records the station name the first connection announces.
func (s *Stream) setName(name string) {
	if name == "" {
		return
	}
	s.mu.Lock()
	if s.name == "" {
		s.name = name
	}
	s.mu.Unlock()
}

// push adds decoded frames at rate to the buffer, waiting for room. The
// first frames fix the stream's rate; sk resamples later ones to it.
func (s *Stream) push(ctx context.Context, gen uint64, frames [][2]float64) error {
	for len(frames) > 0 {
		s.mu.Lock()
		if s.gen != gen {
			s.mu.Unlock()
			return errSuspended
		}
		n := min(len(frames), len(s.ring)-s.count)
		for i := range n {
			s.ring[(s.head+s.count+i)%len(s.ring)] = frames[i]
		}
		s.count += n
		first := !s.readyOnce
		s.mu.Unlock()
		if first && n > 0 {
			s.markReady()
		}
		frames = frames[n:]
		if len(frames) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx) //nolint:wrapcheck // the stream's own stop signal
		case <-s.space:
		}
	}
	return nil
}

// fixRate sets the stream's rate the first time audio arrives and returns
// it, allocating the buffer.
func (s *Stream) fixRate(rate int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rate == 0 {
		s.rate = rate
		s.ring = make([][2]float64, min(rate*ringSeconds, maxRingFrames))
	}
	return s.rate
}

// Stream copies buffered audio into samples. It never waits: with nothing
// buffered it returns (0, true). It returns (0, false) once the stream is
// closed, or has ended or failed for good (see Err) and the buffered audio
// is used up.
func (s *Stream) Stream(samples [][2]float64) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, false
	}
	n := min(len(samples), s.count)
	for i := range n {
		samples[i] = s.ring[(s.head+i)%len(s.ring)]
	}
	if n > 0 {
		s.head = (s.head + n) % len(s.ring)
		s.count -= n
		s.position += n
		select {
		case s.space <- struct{}{}:
		default:
		}
		return n, true
	}
	if s.ended {
		return 0, false
	}
	return 0, true
}

// Buffered is how many frames Stream can return now. Once no more audio
// will arrive it is larger, so a reader pulling at most Buffered frames
// also reaches the end.
func (s *Stream) Buffered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ended {
		return s.count + endedSlack
	}
	return s.count
}

// Err is why the stream stopped, or nil while it plays, after a clean end
// and after Close.
func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Len is -1: a live stream has no length.
func (*Stream) Len() int { return -1 }

// Position is how many frames Stream has returned.
func (s *Stream) Position() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.position
}

// Seek fails with ErrLive.
func (*Stream) Seek(int) error { return ErrLive }

// SampleRate is the rate of the frames Stream returns, fixed for the life
// of the stream.
func (s *Stream) SampleRate() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rate
}

// Name is the station name the server or playlist gave, or "".
func (s *Stream) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

// Live is true: a radio stream plays in real time.
func (*Stream) Live() bool { return true }

// Suspend drops the connection and the buffered audio, as for a pause.
func (s *Stream) Suspend() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ended || s.suspended {
		return
	}
	s.suspended = true
	s.gen++
	s.head, s.count = 0, 0
	if s.connCancel != nil {
		s.connCancel(errSuspended)
	}
}

// Resume reconnects a suspended stream at the live point.
func (s *Stream) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.suspended {
		return
	}
	s.suspended = false
	s.gen++
	select {
	case s.resume <- struct{}{}:
	default:
	}
}

// Close stops the stream and waits for its goroutine. It is safe to call
// more than once and while the network is blocked.
func (s *Stream) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	<-s.done
	return nil
}

// sink decodes one connection's audio into its Stream, resampling it to
// the stream's rate when the source changes rate.
type sink struct {
	s       *Stream
	dec     *nativedec.StreamDecoder
	pcm     [][2]float64
	rs      resampler
	gen     uint64
	frames  int
	srcRate int
}

func newSink(s *Stream, gen uint64) *sink {
	return &sink{s: s, gen: gen}
}

// played reports whether the connection produced at least a second of
// audio, which resets the reconnect backoff.
func (k *sink) played() bool {
	return k.srcRate > 0 && k.frames >= k.srcRate
}

func (k *sink) close() {
	if k.dec != nil {
		_ = k.dec.Close()
		k.dec = nil
	}
}

// reset drops the decoder's state, as at a discontinuity.
func (k *sink) reset() {
	if k.dec != nil {
		k.dec.Reset()
	}
	k.rs.reset()
}

// write decodes data, encoded as codec, into the stream.
func (k *sink) write(ctx context.Context, codec nativedec.Kind, data []byte) error {
	if k.dec != nil && k.dec.Kind() != codec {
		k.close()
		k.rs.reset()
	}
	if k.dec == nil {
		dec, err := nativedec.NewStreamDecoder(codec)
		if err != nil {
			return permanent(fmt.Errorf("%w: %w", ErrUnsupported, err))
		}
		k.dec = dec
	}
	for len(data) > 0 {
		space := k.dec.Space()
		if space == 0 {
			if err := k.drain(ctx); err != nil {
				return err
			}
			if space = k.dec.Space(); space == 0 {
				return fmt.Errorf("radio: %w", nativedec.ErrUndecodable)
			}
		}
		n := min(space, len(data))
		if err := k.dec.Feed(data[:n]); err != nil {
			return fmt.Errorf("radio: feed decoder: %w", err)
		}
		data = data[n:]
		if err := k.drain(ctx); err != nil {
			return err
		}
	}
	return nil
}

// flush decodes what the decoder still holds, at the end of the input.
func (k *sink) flush(ctx context.Context) error {
	if k.dec == nil {
		return nil
	}
	k.dec.Flush()
	return k.drain(ctx)
}

// drain reads every frame the buffered input decodes to.
func (k *sink) drain(ctx context.Context) error {
	if k.pcm == nil {
		k.pcm = make([][2]float64, 4096)
	}
	for {
		n, err := k.dec.Read(k.pcm)
		if n > 0 {
			if perr := k.emit(ctx, k.pcm[:n]); perr != nil {
				return perr
			}
		}
		switch {
		case err == nil:
			if n == 0 {
				return nil
			}
		case errors.Is(err, nativedec.ErrNeedMore), errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, nativedec.ErrUnsupportedCodec):
			return permanent(fmt.Errorf("%w: %w", ErrUnsupported, err))
		default:
			return fmt.Errorf("radio: decode: %w", err)
		}
	}
}

func (k *sink) emit(ctx context.Context, frames [][2]float64) error {
	rate, _, ok := k.dec.Format()
	if !ok {
		return nil
	}
	if rate < minRate || rate > maxRate {
		return fmt.Errorf("radio: unsupported sample rate %d", rate)
	}
	if rate != k.srcRate {
		k.srcRate = rate
		k.rs.reset()
	}
	k.frames += len(frames)
	target := k.s.fixRate(rate)
	if rate == target {
		return k.s.push(ctx, k.gen, frames)
	}
	return k.rs.process(rate, target, frames, func(out [][2]float64) error {
		return k.s.push(ctx, k.gen, out)
	})
}

// resampler converts between rates by linear interpolation. It is used
// only when a stream changes rate mid-way, which is rare.
type resampler struct {
	out  [][2]float64
	prev [2]float64
	pos  float64
	have bool
}

func (r *resampler) reset() {
	r.have = false
	r.pos = 0
}

// process resamples in from rate from to rate to, passing the output to
// emit in bounded chunks.
func (r *resampler) process(from, to int, in [][2]float64, emit func([][2]float64) error) error {
	const chunk = 4096
	if r.out == nil {
		r.out = make([][2]float64, 0, chunk)
	}
	step := float64(from) / float64(to)
	for _, cur := range in {
		if !r.have {
			r.prev, r.have = cur, true
			continue
		}
		for r.pos < 1 {
			r.out = append(r.out, [2]float64{
				r.prev[0] + (cur[0]-r.prev[0])*r.pos,
				r.prev[1] + (cur[1]-r.prev[1])*r.pos,
			})
			r.pos += step
			if len(r.out) == chunk {
				if err := emit(r.out); err != nil {
					return err
				}
				r.out = r.out[:0]
			}
		}
		r.pos--
		r.prev = cur
	}
	if len(r.out) > 0 {
		err := emit(r.out)
		r.out = r.out[:0]
		return err
	}
	return nil
}
