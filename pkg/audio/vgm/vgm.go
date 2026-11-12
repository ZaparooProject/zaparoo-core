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

// Package vgm plays VGM and VGZ files, register logs of arcade, console
// and computer sound chips, by emulating the chips with cores from libvgm
// and emu2413. The command interpreter, DAC stream engine, data block
// handling, resampling and mixing are our own. A Decoder is a beep
// StreamSeekCloser; it is not safe for concurrent use.
package vgm

/*
#cgo CFLAGS: -O2 -w -I${SRCDIR}/csrc/emu/cores -I${SRCDIR}/csrc/emu -I${SRCDIR}/csrc
#cgo CFLAGS: -I${SRCDIR}/third_party/libvgm/cores -I${SRCDIR}
#cgo linux LDFLAGS: -lm
#include "engine.h"
*/
import "C"

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"unsafe"
)

const (
	defaultSampleRate = 48000
	defaultLoops      = 2
	defaultFade       = 8.0
	minSampleRate     = 8000
	maxSampleRate     = 192000
	maxLoops          = 256
	maxFadeSeconds    = 60

	// MaxFileBytes caps a VGM file, and a VGZ file after decompression.
	MaxFileBytes = 64 << 20

	renderFrames = int(C.ZVGM_MAX_FRAMES)
	maxWrites    = 16384
)

var (
	// ErrClosed is returned by Seek on a closed Decoder.
	ErrClosed = errors.New("vgm: decoder closed")

	errTooLarge = errors.New("vgm: file too large")
	errNoData   = errors.New("vgm: file has no playable commands")
	errEngine   = errors.New("vgm: cannot start the sound engine")
)

// Options configures a Decoder. Zero values select the defaults.
type Options struct {
	// SampleRate is the output rate in Hz; 0 means 48000.
	SampleRate int
	// Loops is how many times a looping file plays its loop; 0 means 2.
	// The header's loop modifier and loop base adjust it.
	Loops int
	// FadeSeconds is the fade-out after the last loop; 0 means 8.
	FadeSeconds float64
	// AccurateYM2612 uses the Nuked OPN2 core for the YM2612 and YM3438.
	// It is closer to the hardware and several times slower.
	AccurateYM2612 bool
}

func (o Options) withDefaults() Options {
	if o.SampleRate <= 0 {
		o.SampleRate = defaultSampleRate
	}
	o.SampleRate = min(max(o.SampleRate, minSampleRate), maxSampleRate)
	if o.Loops <= 0 {
		o.Loops = defaultLoops
	}
	o.Loops = min(o.Loops, maxLoops)
	if !(o.FadeSeconds > 0) {
		o.FadeSeconds = defaultFade
	}
	o.FadeSeconds = min(o.FadeSeconds, maxFadeSeconds)
	return o
}

// Decoder renders a VGM file as a stereo stream. It is not safe for
// concurrent use.
type Decoder struct {
	eng    *C.zvgm_engine
	hdr    *header
	st     *playState
	err    error
	info   Info
	data   []byte
	cfgs   []chipCfg
	writes []C.zvgm_write
	mix    []float32
	// Destination of the frames being rendered; nil while seeking.
	out  [][2]float64
	opts Options

	outBase   int64
	lastWrite int64 // latest frame among the queued writes
	gain      float64
	fadeStart int64 // output frame where the fade starts, -1 for none
	fadeLen   int64
	length    int
	pos       int

	handle [numChips][2]int // engine chip index, -1 when absent
	ssg    [numChips][2]int // engine index of an OPN chip's SSG
}

// Open reads a .vgm or gzip-compressed .vgz file.
func Open(path string, opts Options) (*Decoder, error) {
	data, err := readFile(path, MaxFileBytes)
	if err != nil {
		return nil, err
	}
	return newDecoder(data, opts)
}

// OpenBytes decodes a VGM or VGZ file held in memory. The Decoder keeps
// its own copy of uncompressed data.
func OpenBytes(data []byte, opts Options) (*Decoder, error) {
	raw, err := unpack(data, MaxFileBytes)
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 && &raw[0] == &data[0] {
		raw = bytes.Clone(raw)
	}
	return newDecoder(raw, opts)
}

// ReadInfo reads only the GD3 tag of a file, without starting any chips.
// maxBytes caps the bytes read, and decompressed for a VGZ file; 0 or less
// means MaxFileBytes.
func ReadInfo(path string, maxBytes int64) (Info, error) {
	if maxBytes <= 0 || maxBytes > MaxFileBytes {
		maxBytes = MaxFileBytes
	}
	data, err := readFile(path, maxBytes)
	if err != nil {
		return Info{}, err
	}
	h, err := parseHeader(data)
	if err != nil {
		return Info{}, fmt.Errorf("vgm: %w", err)
	}
	if h.gd3Ofs == 0 {
		return Info{}, nil
	}
	info, err := parseGD3(data[h.gd3Ofs:])
	if err != nil {
		return Info{}, fmt.Errorf("vgm: %w", err)
	}
	return info, nil
}

func readFile(path string, limit int64) ([]byte, error) {
	//nolint:gosec // G304: callers validate media paths before opening.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("vgm: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("vgm: read: %w", err)
	}
	if int64(len(data)) > limit {
		// A VGZ file may still fit after decompression only if it is
		// smaller than the cap, so an oversized file is rejected outright.
		return nil, errTooLarge
	}
	return unpack(data, limit)
}

// unpack returns data, gunzipped if it is a gzip stream, capped at limit.
func unpack(data []byte, limit int64) ([]byte, error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		if int64(len(data)) > limit {
			return nil, errTooLarge
		}
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("vgm: gzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, limit+1))
	if err != nil {
		return nil, fmt.Errorf("vgm: gzip: %w", err)
	}
	if int64(len(out)) > limit {
		return nil, errTooLarge
	}
	return out, nil
}

func newDecoder(data []byte, opts Options) (*Decoder, error) {
	h, err := parseHeader(data)
	if err != nil {
		return nil, fmt.Errorf("vgm: %w", err)
	}
	if err := prescan(h, data); err != nil {
		return nil, fmt.Errorf("vgm: %w", err)
	}
	opts = opts.withDefaults()
	d := &Decoder{
		data:   data,
		hdr:    h,
		opts:   opts,
		writes: make([]C.zvgm_write, 0, maxWrites),
		mix:    make([]float32, renderFrames*2),
		gain:   math.Exp2(float64(h.volMod) / 32),
	}
	if h.gd3Ofs != 0 {
		if info, err := parseGD3(data[h.gd3Ofs:]); err == nil {
			d.info = info
		}
	}
	d.cfgs = chipConfigs(h, opts)
	if len(d.cfgs) == 0 {
		return nil, fmt.Errorf("vgm: %w", errNoChips)
	}
	d.setLength()
	if err := d.restart(); err != nil {
		return nil, err
	}
	return d, nil
}

// setLength works out the stream length: the intro, the loop body played
// the configured number of times and, for looping files, the fade.
func (d *Decoder) setLength() {
	h := d.hdr
	ticks := h.totalTicks
	d.fadeStart = -1
	if h.loopOfs != 0 {
		n := int64(h.loopCount(d.opts.Loops))
		ticks += h.loopTicks * (n - 1)
		ticks = min(ticks, maxTotalTicks)
		d.fadeStart = d.frameOf(ticks)
		d.fadeLen = int64(d.opts.FadeSeconds * float64(d.opts.SampleRate))
	}
	n := d.frameOf(ticks)
	if d.fadeStart >= 0 {
		n += d.fadeLen
	}
	d.length = int(min(n, math.MaxInt32))
}

func (d *Decoder) frameOf(tick int64) int64 {
	return tick * int64(d.opts.SampleRate) / vgmRate
}

// restart frees the engine, if any, and starts the chips from power-on.
func (d *Decoder) restart() error {
	if d.eng != nil {
		C.zvgm_free(d.eng)
		d.eng = nil
	}
	eng := C.zvgm_new(C.uint32_t(d.opts.SampleRate))
	if eng == nil {
		return errEngine
	}
	d.eng = eng
	for t := range d.handle {
		d.handle[t] = [2]int{-1, -1}
		d.ssg[t] = [2]int{-1, -1}
	}
	started := 0
	for i := range d.cfgs {
		c := &d.cfgs[i]
		idx := int(C.zvgm_add_chip(eng, &c.c))
		if idx < 0 {
			continue
		}
		started++
		d.handle[c.typ][c.num] = idx
		if c.hasSSG {
			d.ssg[c.typ][c.num] = idx + 1
		}
	}
	if started == 0 {
		C.zvgm_free(eng)
		d.eng = nil
		return fmt.Errorf("vgm: %w", errNoChips)
	}
	d.st = newPlayState(d.hdr)
	d.writes = d.writes[:0]
	d.lastWrite = 0
	d.pos = 0
	d.err = nil
	return nil
}

// Stream fills samples with the next frames of audio.
func (d *Decoder) Stream(samples [][2]float64) (n int, ok bool) {
	if d.eng == nil || d.err != nil {
		return 0, false
	}
	n = min(len(samples), d.length-d.pos)
	if n <= 0 {
		return 0, false
	}
	d.out = samples[:n]
	d.outBase = d.st.rendered
	d.render(int64(n))
	d.out = nil
	d.pos += n
	return n, true
}

// Err returns the error that stopped the stream, if any.
func (d *Decoder) Err() error { return d.err }

// Len is the stream length in frames, including loops and the fade.
func (d *Decoder) Len() int { return d.length }

// Position is the current frame.
func (d *Decoder) Position() int { return d.pos }

// SampleRate is the output rate in Hz.
func (d *Decoder) SampleRate() int { return d.opts.SampleRate }

// Info is the file's GD3 tag.
func (d *Decoder) Info() Info { return d.info }

// Seek moves playback to frame p. Seeking backwards restarts the chips;
// either way the chips are emulated up to p without producing output.
func (d *Decoder) Seek(p int) error {
	if d.eng == nil {
		return ErrClosed
	}
	if p < 0 || p > d.length {
		return fmt.Errorf("vgm: seek position %d out of range [0, %d]", p, d.length)
	}
	if p < d.pos {
		if err := d.restart(); err != nil {
			d.err = err
			return err
		}
	}
	d.render(int64(p - d.pos))
	d.pos = p
	return nil
}

// Close frees the chips. It is safe to call more than once.
func (d *Decoder) Close() error {
	if d.eng != nil {
		C.zvgm_free(d.eng)
		d.eng = nil
	}
	d.st = nil
	return nil
}

// render advances n frames, writing them to d.out when it is set.
func (d *Decoder) render(n int64) {
	st := d.st
	target := st.rendered + n
	for st.rendered < target {
		end := min(target, st.rendered+int64(renderFrames))
		d.advance(end)
		d.flush(end)
	}
}

// flush renders the chips up to frame f, applying the queued writes.
func (d *Decoder) flush(f int64) {
	st := d.st
	if len(d.writes) > 0 {
		f = max(f, d.lastWrite)
	}
	frames := int(f - st.rendered)
	if frames <= 0 && len(d.writes) == 0 {
		return
	}
	frames = max(frames, 0)
	for i := range d.writes {
		d.writes[i].frame -= C.uint32_t(st.rendered)
	}
	var wp *C.zvgm_write
	if len(d.writes) > 0 {
		wp = &d.writes[0]
	}
	var op *C.float
	keep := d.out != nil
	if keep {
		op = (*C.float)(unsafe.Pointer(&d.mix[0]))
	}
	C.zvgm_render(d.eng, wp, C.int(len(d.writes)), op, C.int(frames))
	d.writes = d.writes[:0]
	d.lastWrite = 0
	if keep {
		d.copyOut(st.rendered, frames)
	}
	st.rendered += int64(frames)
}

// copyOut applies the master gain and fade to rendered frames and stores
// them in the output slice.
func (d *Decoder) copyOut(start int64, frames int) {
	dst := d.out[start-d.outBase:]
	for i := range frames {
		g := d.gain
		if d.fadeStart >= 0 {
			if t := start + int64(i) - d.fadeStart; t > 0 {
				x := 1 - float64(t)/float64(d.fadeLen)
				if x <= 0 {
					g = 0
				} else {
					g *= x * x
				}
			}
		}
		dst[i][0] = clamp(float64(d.mix[2*i]) * g)
		dst[i][1] = clamp(float64(d.mix[2*i+1]) * g)
	}
}

func clamp(v float64) float64 {
	return min(max(v, -1), 1)
}

// queue adds a register write to the next render call.
func (d *Decoder) queue(chip int, kind uint8, addr, data uint16, frame int64) {
	if len(d.writes) == cap(d.writes) {
		d.flush(frame)
	}
	// Streams queue in turn, so a write can trail one already queued or
	// rendered; it then applies at the current position.
	frame = max(frame, d.st.rendered)
	d.lastWrite = max(d.lastWrite, frame)
	d.writes = append(d.writes, C.zvgm_write{
		frame: C.uint32_t(frame),
		addr:  C.uint16_t(addr),
		data:  C.uint16_t(data),
		chip:  C.uint8_t(chip),
		kind:  C.uint8_t(kind),
	})
}

// memWrite loads data into a chip's ROM or RAM at the current frame.
func (d *Decoder) memWrite(chip, region int, romSize, start uint32, data []byte, frame int64) {
	if len(data) == 0 {
		return
	}
	d.flush(frame)
	C.zvgm_mem(d.eng, C.int(chip), C.int(region), C.uint32_t(romSize), C.uint32_t(start),
		(*C.uint8_t)(unsafe.Pointer(&data[0])), C.uint32_t(len(data)))
}
