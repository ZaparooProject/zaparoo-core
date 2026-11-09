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

// Package gme plays chiptune rips (NES .nsf/.nsfe, Game Boy .gbs, PC
// Engine .hes, MSX .kss and SNES .spc) by emulating the original sound
// hardware with Game_Music_Emu. A Decoder is a beep StreamSeekCloser that
// plays one track, or every track of a file back to back as one stream.
package gme

/*
// Every platform Core is built for is little-endian.
#cgo CPPFLAGS: -I${SRCDIR}/third_party/gme/gme -DNDEBUG -DBLARGG_LITTLE_ENDIAN=1
#cgo CFLAGS: -O2 -w
#cgo CXXFLAGS: -O2 -std=c++11 -fno-exceptions -fno-rtti -w
#cgo linux LDFLAGS: -lm
#include <stdlib.h>
#include "gme.h"

// zgme_open identifies data by its header and loads it into a new
// emulator. A NULL return leaves the reason in *err; data must outlive the
// emulator, as some emulators keep pointing into it.
static Music_Emu* zgme_open(const void* data, long size, int rate, const char** err)
{
	const char* ext;
	gme_type_t type;
	Music_Emu* emu;
	*err = NULL;
	if (size < 4) { *err = gme_wrong_file_type; return NULL; }
	ext = gme_identify_header(data);
	type = (ext != NULL && *ext) ? gme_identify_extension(ext) : NULL;
	if (type == NULL) { *err = gme_wrong_file_type; return NULL; }
	emu = gme_new_emu(type, rate);
	if (emu == NULL) { *err = "out of memory"; return NULL; }
	*err = gme_load_data(emu, data, size);
	if (*err != NULL) { gme_delete(emu); return NULL; }
	return emu;
}

// zgme_counts_tracks reports whether the loaded format records how many
// tracks it holds. HES and KSS do not; gme reports 256 slots for them.
static int zgme_counts_tracks(Music_Emu* emu)
{
	gme_type_t t = gme_type(emu);
	return t != gme_hes_type && t != gme_kss_type;
}

typedef struct {
	int length, intro, loop, fade;
	const char *system, *game, *song, *author, *copyright, *comment;
} zgme_info;

static gme_err_t zgme_track_info(Music_Emu* emu, int track, zgme_info* out, gme_info_t** keep)
{
	gme_info_t* i = NULL;
	gme_err_t err = gme_track_info(emu, &i, track);
	*keep = i;
	if (err != NULL) return err;
	out->length = i->length; out->intro = i->intro_length;
	out->loop = i->loop_length; out->fade = i->fade_length;
	out->system = i->system; out->game = i->game; out->song = i->song;
	out->author = i->author; out->copyright = i->copyright; out->comment = i->comment;
	return NULL;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unsafe"

	"github.com/gopxl/beep/v2"
)

const (
	// DefaultSampleRate is the output rate used when Options.SampleRate is 0.
	DefaultSampleRate = 48000
	// DefaultTrackSeconds is how long a track without a length tag plays.
	DefaultTrackSeconds = 150
	// DefaultFadeSeconds is the fade-out at the end of every track.
	DefaultFadeSeconds = 8
	// MaxFileBytes is the largest file Open reads.
	MaxFileBytes = 16 << 20

	// maxTrackSeconds bounds one track including its fade. A crafted
	// length tag can claim any length; seeking emulates forward, so the
	// bound also bounds the cost of a seek.
	maxTrackSeconds = 60 * 60
	maxFadeSeconds  = 60
	minSampleRate   = 8000
	maxSampleRate   = 96000
	// renderFrames is the most frames asked of the emulator at once.
	renderFrames = 2048
	// maxTextBytes caps every metadata string the package hands out.
	maxTextBytes = 256
)

var (
	// ErrUnsupported is returned for data that is not one of the formats
	// Supported lists.
	ErrUnsupported = errors.New("gme: unsupported or unrecognized file")
	// ErrTooLarge is returned for a file above the size cap.
	ErrTooLarge = errors.New("gme: file too large")
	// ErrClosed is returned by a Decoder used after Close.
	ErrClosed = errors.New("gme: decoder closed")
	// ErrNoTrack is returned for a track index the file does not have.
	ErrNoTrack = errors.New("gme: no such track")
)

// Options configures a Decoder.
type Options struct {
	// SampleRate is the output rate in Hz; 0 means DefaultSampleRate.
	SampleRate int
	// DefaultTrackSeconds is the play time of a track whose file gives no
	// length; 0 means DefaultTrackSeconds.
	DefaultTrackSeconds float64
	// FadeSeconds is the fade-out after each track's play time when the
	// file does not give its own; 0 means DefaultFadeSeconds.
	FadeSeconds float64
}

// TrackInfo describes one track of a file.
type TrackInfo struct {
	Title, Game, System, Author, Copyright, Comment string
	// LengthFrames is the track's play time including its fade, at the
	// decoder's sample rate (at DefaultSampleRate from ReadInfo).
	LengthFrames int
}

// Info describes a file. Title is the title of the track a Decoder plays;
// it is "" for a Decoder playing every track and from ReadInfo.
type Info struct {
	Title, Game, System, Author, Copyright string
	Tracks                                 []TrackInfo
}

// Supported reports whether files with extension ext, which includes the
// dot and is matched case-insensitively, are played by this package.
func Supported(ext string) bool {
	switch strings.ToLower(ext) {
	case ".nsf", ".nsfe", ".gbs", ".hes", ".kss", ".spc":
		return true
	default:
		return false
	}
}

type span struct {
	start, frames  int
	fadeMS, playMS int
}

// Decoder streams one track or a run of tracks. It is not safe for
// concurrent use.
type Decoder struct {
	emu    *C.Music_Emu
	data   unsafe.Pointer
	err    error
	info   Info
	tracks []int // file track index of each span
	spans  []span
	buf    []int16
	rate   int
	length int
	pos    int
	cur    int // index into spans of the track loaded in the emulator
	inTrk  int // frames played of the current span
}

var _ beep.StreamSeekCloser = (*Decoder)(nil)

// Open plays the file at path. track is a 0-based track index, or -1 to
// play every track in order as one stream. HES and KSS files do not record
// how many tracks they hold, so -1 plays only their first.
func Open(path string, track int, opts Options) (*Decoder, error) {
	data, err := readCapped(path, MaxFileBytes)
	if err != nil {
		return nil, err
	}
	return OpenBytes(data, "", track, opts)
}

// OpenBytes plays a file held in memory. The format is identified from
// the data; ext, when not "", must also be one Supported lists.
func OpenBytes(data []byte, ext string, track int, opts Options) (*Decoder, error) {
	if ext != "" && !Supported(ext) {
		return nil, ErrUnsupported
	}
	opts, err := normalize(opts)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	emu, mem, err := load(data, C.int(opts.SampleRate))
	if err != nil {
		return nil, err
	}
	d := &Decoder{emu: emu, data: mem, rate: opts.SampleRate, cur: -1}
	count := int(C.gme_track_count(emu))
	if track < -1 || track >= count {
		d.free()
		return nil, fmt.Errorf("%w: track %d of %d", ErrNoTrack, track, count)
	}
	d.info, err = fileInfo(emu, count, opts)
	if err != nil {
		d.free()
		return nil, err
	}
	if track < 0 && C.zgme_counts_tracks(emu) == 0 {
		track = 0
	}
	if track >= 0 {
		d.tracks = []int{track}
		d.info.Title = d.info.Tracks[track].Title
	} else {
		d.tracks = make([]int, count)
		for i := range d.tracks {
			d.tracks[i] = i
		}
	}
	d.spans = make([]span, len(d.tracks))
	for i, t := range d.tracks {
		ti := d.info.Tracks[t]
		play, fade := trackTimes(emu, t, opts)
		d.spans[i] = span{start: d.length, frames: ti.LengthFrames, playMS: play, fadeMS: fade}
		d.length += ti.LengthFrames
	}
	if err := d.startSpan(0); err != nil {
		d.free()
		return nil, err
	}
	return d, nil
}

// TrackCount reports how many tracks the file at path holds. HES and
// KSS files do not record a count; Game_Music_Emu reports 256 for them.
func TrackCount(path string) (int, error) {
	data, err := readCapped(path, MaxFileBytes)
	if err != nil {
		return 0, err
	}
	emu, mem, err := load(data, C.gme_info_only)
	if err != nil {
		return 0, err
	}
	defer freeEmu(emu, mem)
	return int(C.gme_track_count(emu)), nil
}

// ReadInfo reads a file's tags without starting playback. Files larger
// than maxBytes (MaxFileBytes when maxBytes <= 0 or above it) are refused,
// as the formats keep tags at both ends of the file.
func ReadInfo(path string, maxBytes int64) (Info, error) {
	if maxBytes <= 0 || maxBytes > MaxFileBytes {
		maxBytes = MaxFileBytes
	}
	data, err := readCapped(path, maxBytes)
	if err != nil {
		return Info{}, err
	}
	emu, mem, err := load(data, C.gme_info_only)
	if err != nil {
		return Info{}, err
	}
	defer freeEmu(emu, mem)
	opts, _ := normalize(Options{})
	return fileInfo(emu, int(C.gme_track_count(emu)), opts)
}

func normalize(opts Options) (Options, error) {
	if opts.SampleRate == 0 {
		opts.SampleRate = DefaultSampleRate
	}
	if opts.SampleRate < minSampleRate || opts.SampleRate > maxSampleRate {
		return opts, fmt.Errorf("gme: unsupported sample rate %d", opts.SampleRate)
	}
	if !(opts.DefaultTrackSeconds > 0) || opts.DefaultTrackSeconds > maxTrackSeconds {
		opts.DefaultTrackSeconds = DefaultTrackSeconds
	}
	if !(opts.FadeSeconds > 0) || opts.FadeSeconds > maxFadeSeconds {
		opts.FadeSeconds = DefaultFadeSeconds
	}
	return opts, nil
}

func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from the media library
	if err != nil {
		return nil, fmt.Errorf("gme: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("gme: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}

// load copies data to C memory, which the emulator may keep pointing into,
// and opens it.
func load(data []byte, rate C.int) (emu *C.Music_Emu, mem unsafe.Pointer, err error) {
	if len(data) < 4 {
		return nil, nil, ErrUnsupported
	}
	mem = C.CBytes(data)
	var cerr *C.char
	emu = C.zgme_open(mem, C.long(len(data)), rate, &cerr)
	if emu == nil {
		C.free(mem)
		if cerr == C.gme_wrong_file_type {
			return nil, nil, ErrUnsupported
		}
		return nil, nil, fmt.Errorf("gme: %s", errText(cerr))
	}
	return emu, mem, nil
}

func freeEmu(emu *C.Music_Emu, mem unsafe.Pointer) {
	C.gme_delete(emu)
	C.free(mem)
}

func errText(s *C.char) string {
	if s == nil {
		return "unknown error"
	}
	if t := sanitize([]byte(C.GoString(s))); t != "" {
		return t
	}
	return "unknown error"
}

// trackTimes returns a track's play time and fade in milliseconds, using
// the file's length tags as gme_info_t.play_length does: the length, else
// the intro plus the loop played twice, else the default.
func trackTimes(emu *C.Music_Emu, track int, opts Options) (playMS, fadeMS int) {
	var ci C.zgme_info
	var keep *C.gme_info_t
	err := C.zgme_track_info(emu, C.int(track), &ci, &keep)
	if keep != nil {
		defer C.gme_free_info(keep)
	}
	playMS = int(opts.DefaultTrackSeconds * 1000)
	fadeMS = int(opts.FadeSeconds * 1000)
	if err != nil {
		return playMS, fadeMS
	}
	switch {
	case ci.length > 0:
		playMS = int(ci.length)
	case ci.loop > 0:
		playMS = max(int(ci.intro), 0) + 2*int(ci.loop)
	}
	if ci.fade > 0 {
		fadeMS = int(ci.fade)
	}
	fadeMS = min(fadeMS, maxFadeSeconds*1000)
	playMS = min(playMS, maxTrackSeconds*1000-fadeMS)
	return playMS, fadeMS
}

func fileInfo(emu *C.Music_Emu, count int, opts Options) (Info, error) {
	if count <= 0 {
		return Info{}, ErrUnsupported
	}
	info := Info{Tracks: make([]TrackInfo, count)}
	for t := range count {
		var ci C.zgme_info
		var keep *C.gme_info_t
		err := C.zgme_track_info(emu, C.int(t), &ci, &keep)
		if err != nil {
			if keep != nil {
				C.gme_free_info(keep)
			}
			return Info{}, fmt.Errorf("gme: track %d: %s", t, errText(err))
		}
		ti := TrackInfo{
			Title:     cText(ci.song),
			Game:      cText(ci.game),
			System:    cText(ci.system),
			Author:    cText(ci.author),
			Copyright: cText(ci.copyright),
			Comment:   cText(ci.comment),
		}
		C.gme_free_info(keep)
		play, fade := trackTimes(emu, t, opts)
		ti.LengthFrames = msFrames(play+fade, opts.SampleRate)
		info.Tracks[t] = ti
	}
	first := info.Tracks[0]
	info.Game, info.System, info.Author, info.Copyright = first.Game, first.System, first.Author, first.Copyright
	return info, nil
}

func cText(s *C.char) string {
	if s == nil {
		return ""
	}
	return sanitize([]byte(C.GoString(s)))
}

func msFrames(ms, rate int) int {
	return int(int64(ms) * int64(rate) / 1000)
}

// startSpan loads span i into the emulator from its beginning.
func (d *Decoder) startSpan(i int) error {
	if err := C.gme_start_track(d.emu, C.int(d.tracks[i])); err != nil {
		return fmt.Errorf("gme: track %d: %s", d.tracks[i], errText(err))
	}
	d.cur = i
	d.inTrk = 0
	d.setFade()
	return nil
}

// setFade applies the current span's fade. Starting a track, and seeking
// backwards within one, resets the emulator's fade.
func (d *Decoder) setFade() {
	s := d.spans[d.cur]
	C.gme_set_fade_msecs(d.emu, C.int(s.playMS), C.int(s.fadeMS))
}

// Stream fills samples with the next frames of audio.
func (d *Decoder) Stream(samples [][2]float64) (n int, ok bool) {
	if d.emu == nil || d.err != nil {
		return 0, false
	}
	for n < len(samples) && d.pos < d.length {
		s := d.spans[d.cur]
		if d.inTrk >= s.frames {
			if err := d.startSpan(d.cur + 1); err != nil {
				d.err = err
				break
			}
			continue
		}
		want := min(len(samples)-n, s.frames-d.inTrk, renderFrames)
		if cap(d.buf) < want*2 {
			d.buf = make([]int16, renderFrames*2)
		}
		buf := d.buf[:want*2]
		if err := C.gme_play(d.emu, C.int(want*2), (*C.short)(unsafe.Pointer(&buf[0]))); err != nil {
			d.err = fmt.Errorf("gme: %s", errText(err))
			break
		}
		out := samples[n : n+want]
		for i := range out {
			out[i][0] = float64(buf[2*i]) / 32768
			out[i][1] = float64(buf[2*i+1]) / 32768
		}
		n += want
		d.inTrk += want
		d.pos += want
	}
	return n, n > 0
}

// Err returns the error that stopped the stream, if any.
func (d *Decoder) Err() error { return d.err }

// Len is the stream length in frames: the sum of the tracks' play times
// and fades.
func (d *Decoder) Len() int { return d.length }

// Position is the current frame.
func (d *Decoder) Position() int { return d.pos }

// SampleRate is the output rate in Hz.
func (d *Decoder) SampleRate() int { return d.rate }

// Info describes the file and the track playing.
func (d *Decoder) Info() Info { return d.info }

// TrackStarts is the first frame of each track in the stream; it has one
// element, 0, for a Decoder playing a single track.
func (d *Decoder) TrackStarts() []int {
	out := make([]int, len(d.spans))
	for i, s := range d.spans {
		out[i] = s.start
	}
	return out
}

// CurrentTrack is the index into TrackStarts of the track at Position.
func (d *Decoder) CurrentTrack() int {
	if d.cur >= 0 && d.inTrk >= d.spans[d.cur].frames && d.cur+1 < len(d.spans) {
		return d.cur + 1
	}
	return max(d.cur, 0)
}

// Seek moves playback to frame p. The emulator has no random access, so a
// seek restarts the track and emulates forward to the offset; that takes
// time in proportion to the offset.
func (d *Decoder) Seek(p int) error {
	if d.emu == nil {
		return ErrClosed
	}
	if p < 0 || p > d.length {
		return fmt.Errorf("gme: seek position %d out of range [0, %d]", p, d.length)
	}
	i := sort.Search(len(d.spans), func(i int) bool { return d.spans[i].start > p }) - 1
	i = max(i, 0)
	off := p - d.spans[i].start
	if i != d.cur {
		if err := d.startSpan(i); err != nil {
			d.err = err
			return err
		}
	}
	if err := C.gme_seek_samples(d.emu, C.int(off*2)); err != nil {
		d.err = fmt.Errorf("gme: %s", errText(err))
		return d.err
	}
	d.setFade()
	d.err = nil
	d.inTrk = off
	d.pos = p
	return nil
}

// Close frees the emulator. It is safe to call more than once.
func (d *Decoder) Close() error {
	d.free()
	return nil
}

func (d *Decoder) free() {
	if d.emu != nil {
		freeEmu(d.emu, d.data)
		d.emu, d.data = nil, nil
	}
}
