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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// sniffBytes is how much of a stream with a generic Content-Type is read
// to tell what it is.
const sniffBytes = 512

// codecNone means the codec is not known.
const codecNone nativedec.Kind = -1

// Probe is a URL opened and classified by ProbeURL. A stream probe holds
// its response open so Open can play from it without connecting again.
type Probe struct {
	resp        *http.Response
	cancel      context.CancelFunc
	url         *url.URL
	opened      time.Time
	contentType string
	name        string
	head        []byte // bytes already read from resp.Body
	body        []byte // a playlist's whole body
	mu          syncutil.Mutex
	kind        Kind
	codec       nativedec.Kind
	closed      bool
}

// ProbeURL requests rawURL, with ICY metadata asked for, and classifies
// the response by Content-Type, sniffing its first bytes when the type is
// generic. ctx bounds only the request, headers and sniffing: the stream
// response the probe keeps is not tied to it, so the caller may cancel ctx
// once ProbeURL returns. A KindNotAudio probe is still open; Close it.
func ProbeURL(ctx context.Context, c *http.Client, rawURL string) (*Probe, error) {
	if c == nil {
		c = NewHTTPClient()
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("radio: parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("radio: unsupported url scheme %q", u.Scheme)
	}
	resp, cancel, err := get(ctx, c, u.String(), true) //nolint:bodyclose // the Probe owns the body; Close closes it
	if err != nil {
		return nil, err
	}
	p := &Probe{
		resp:   resp,
		cancel: cancel,
		url:    resp.Request.URL,
		opened: time.Now(),
		codec:  codecNone,
		name:   sanitizeText(decodeText([]byte(resp.Header.Get("icy-name")))),
	}
	// Sniffing reads the body, which outlives ctx, so watch ctx again.
	stop := context.AfterFunc(ctx, cancel)
	err = p.classify()
	if !stop() && err == nil {
		err = fmt.Errorf("radio: probe %s: %w", rawURL, context.Cause(ctx))
	}
	if err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// get makes a GET whose response body is not bound to ctx: ctx bounds the
// wait for headers, and the returned cancel ends the request after that.
func get(ctx context.Context, c *http.Client, rawURL string, icy bool) (*http.Response, context.CancelFunc, error) {
	reqCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("radio: new request: %w", err)
	}
	if icy {
		req.Header.Set("Icy-MetaData", "1")
	}
	stop := context.AfterFunc(ctx, cancel)
	resp, err := c.Do(req)
	stopped := stop()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("radio: get %s: %w", rawURL, err)
	}
	if !stopped {
		_ = resp.Body.Close()
		cancel()
		return nil, nil, fmt.Errorf("radio: get %s: %w", rawURL, context.Cause(ctx))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_ = resp.Body.Close()
		cancel()
		return nil, nil, fmt.Errorf("radio: get %s: status %s", rawURL, resp.Status)
	}
	return resp, cancel, nil
}

// Kind is what the URL serves.
func (p *Probe) Kind() Kind {
	if p == nil {
		return KindNotAudio
	}
	return p.kind
}

// Name is the station name the server sent, or "" when it sent none.
func (p *Probe) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

// URL is the URL after redirects.
func (p *Probe) URL() string {
	if p == nil || p.url == nil {
		return ""
	}
	return p.url.String()
}

// Close releases the probe's connection. It is idempotent and safe on nil.
func (p *Probe) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.resp != nil {
		_ = p.resp.Body.Close()
	}
	if p.cancel != nil {
		p.cancel()
	}
}

// take hands the open response to a player, or nil when it is closed or
// already taken.
func (p *Probe) take() (resp *http.Response, head []byte, cancel context.CancelFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.resp == nil {
		return nil, nil, nil
	}
	resp, head, cancel = p.resp, p.head, p.cancel
	p.resp, p.head, p.cancel = nil, nil, nil
	return resp, head, cancel
}

func (p *Probe) classify() error {
	ct := ""
	if v := p.resp.Header.Get("Content-Type"); v != "" {
		if mt, _, err := mime.ParseMediaType(v); err == nil {
			ct = strings.ToLower(mt)
		} else {
			ct = strings.ToLower(strings.TrimSpace(strings.Split(v, ";")[0]))
		}
	}
	p.contentType = ct
	switch ct {
	case "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl",
		"audio/x-scpls", "application/pls+xml", "application/pls", "audio/scpls":
		return p.readPlaylist()
	}
	if codec := codecForType(ct); codec != codecNone {
		p.kind = KindStream
		p.codec = codec
		return nil
	}
	if !genericType(ct) {
		p.kind = KindNotAudio
		return nil
	}
	head := make([]byte, sniffBytes)
	n, err := readAtLeast(p.resp.Body, head)
	head = head[:n]
	p.head = head
	if n == 0 && err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("radio: read %s: %w", p.url, err)
	}
	if looksLikeText(head) {
		return p.readPlaylist()
	}
	if codec := sniffCodec(head); codec != codecNone {
		p.kind = KindStream
		p.codec = codec
		return nil
	}
	p.kind = KindNotAudio
	return nil
}

// readPlaylist reads the rest of a playlist body, at most
// maxPlaylistBytes, closes the connection, and classifies the text.
func (p *Probe) readPlaylist() error {
	body, err := readLimited(io.MultiReader(bytes.NewReader(p.head), p.resp.Body), maxPlaylistBytes)
	_ = p.resp.Body.Close()
	p.resp = nil
	p.head = nil
	if err != nil {
		return fmt.Errorf("radio: read playlist %s: %w", p.url, err)
	}
	p.body = body
	switch {
	case isHLS(body):
		p.kind = KindHLS
	case isPLS(body):
		p.kind = KindPlaylist
	case isM3U(body):
		p.kind = KindPlaylist
	default:
		p.kind = KindNotAudio
	}
	return nil
}

// readLimited reads all of r, failing with ErrTooLarge past limit bytes.
// It grows its buffer as data arrives, never trusting a declared length.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	var buf bytes.Buffer
	n, err := buf.ReadFrom(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if n > limit {
		return nil, ErrTooLarge
	}
	return buf.Bytes(), nil
}

// codecForType maps an audio Content-Type to its decoder.
func codecForType(ct string) nativedec.Kind {
	switch ct {
	case "audio/mpeg", "audio/mp3", "audio/mpeg3", "audio/x-mpeg", "audio/x-mp3", "audio/mpg":
		return nativedec.MP3
	case "audio/aac", "audio/aacp", "audio/x-aac", "audio/x-aacp", "audio/aac-adts":
		return nativedec.AAC
	case "audio/ogg", "application/ogg", "audio/x-ogg", "audio/vorbis", "audio/x-vorbis+ogg":
		return nativedec.Vorbis
	default:
		return codecNone
	}
}

// genericType reports whether ct says nothing useful, so the body decides.
func genericType(ct string) bool {
	switch ct {
	case "", "application/octet-stream", "binary/octet-stream", "application/binary",
		"text/plain", "application/x-unknown", "unknown/unknown", "audio/unknown":
		return true
	default:
		return false
	}
}

// sniffCodec recognizes the start of an Ogg, ADTS or MP3 stream.
func sniffCodec(b []byte) nativedec.Kind {
	switch {
	case bytes.HasPrefix(b, []byte("OggS")):
		return nativedec.Vorbis
	case bytes.HasPrefix(b, []byte("ID3")):
		return nativedec.MP3
	}
	// A stream may start mid-frame: look for a sync in the first bytes,
	// and want a second one where the first frame says it ends.
	for i := 0; i+7 <= len(b); i++ {
		if b[i] != 0xFF {
			continue
		}
		if isADTS(b[i:]) {
			if n := adtsFrameLen(b[i:]); n >= 7 && (i+n+2 > len(b) || isADTS(b[i+n:])) {
				return nativedec.AAC
			}
			continue
		}
		if n := mp3FrameLen(b[i:]); n > 0 && (i+n+2 > len(b) || mp3FrameLen(b[i+n:]) > 0) {
			return nativedec.MP3
		}
	}
	return codecNone
}

func isADTS(b []byte) bool {
	return len(b) >= 2 && b[0] == 0xFF && b[1]&0xF6 == 0xF0
}

func adtsFrameLen(b []byte) int {
	if len(b) < 7 {
		return 0
	}
	return int(b[3]&0x03)<<11 | int(b[4])<<3 | int(b[5])>>5
}

var (
	mp3Bitrates = [2][3][16]int{
		{ // MPEG-1: layer I, II, III
			{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0},
			{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0},
			{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
		},
		{ // MPEG-2 and 2.5
			{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0},
			{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
			{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
		},
	}
	mp3Rates = [4][3]int{{11025, 12000, 8000}, {0, 0, 0}, {22050, 24000, 16000}, {44100, 48000, 32000}}
)

// mp3FrameLen is the length of the MPEG audio frame b starts with, or 0
// when b does not start with a valid header. Free-format frames count as
// invalid: a sniff wants a sure match.
func mp3FrameLen(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return 0
	}
	version := (b[1] >> 3) & 3 // 0: 2.5, 2: 2, 3: 1
	layer := (b[1] >> 1) & 3   // 1: III, 2: II, 3: I
	brIdx := b[2] >> 4
	srIdx := (b[2] >> 2) & 3
	pad := int((b[2] >> 1) & 1)
	if version == 1 || layer == 0 || brIdx == 0 || brIdx == 15 || srIdx == 3 {
		return 0
	}
	v := 1
	if version == 3 {
		v = 0
	}
	l := 3 - int(layer)
	br := mp3Bitrates[v][l][brIdx] * 1000
	sr := mp3Rates[version][srIdx]
	switch {
	case layer == 3: // layer I
		return (12*br/sr + pad) * 4
	case layer == 1 && v == 1: // layer III, MPEG-2/2.5
		return 72*br/sr + pad
	default:
		return 144*br/sr + pad
	}
}

// looksLikeText reports whether b starts like a playlist.
func looksLikeText(b []byte) bool {
	t := bytes.TrimLeft(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), " \t\r\n")
	if bytes.HasPrefix(t, []byte("#EXTM3U")) || bytes.HasPrefix(t, []byte("#EXTINF")) {
		return true
	}
	if len(t) >= 10 && strings.EqualFold(string(t[:10]), "[playlist]") {
		return true
	}
	l := strings.ToLower(string(t[:min(len(t), 8)]))
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}
