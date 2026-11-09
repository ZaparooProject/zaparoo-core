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
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
	"github.com/rs/zerolog/log"
)

// HLS playback limits.
const (
	// maxSegmentBytes bounds one media segment.
	maxSegmentBytes = 16 << 20
	// maxInitBytes bounds an fMP4 init segment.
	maxInitBytes = 1 << 20
	// maxMasterDepth bounds master playlists that point at masters.
	maxMasterDepth = 3
	// liveStartSegments is how far from the end a live stream starts.
	liveStartSegments = 3
	// maxSegmentFailures is how many segments in a row may fail to load
	// before the stream reconnects.
	maxSegmentFailures = 3
	// maxCached bounds the keys and init segments kept.
	maxCached = 16
	// playlistTimeout and segmentTimeout bound one request.
	playlistTimeout = 15 * time.Second
	segmentTimeout  = 30 * time.Second
	// stallTargets is how many target durations a live playlist may go
	// without a new segment before the stream reconnects.
	stallTargets = 6
)

// segmentError is a segment that could not be loaded; the stream skips it.
type segmentError struct{ err error }

func (e *segmentError) Error() string { return e.err.Error() }
func (e *segmentError) Unwrap() error { return e.err }

// hlsSource plays an HLS playlist.
type hlsSource struct {
	url       *url.URL
	mediaURL  *url.URL
	keys      map[string][]byte
	inits     map[string]*mp4Track
	ts        *tsDemuxer
	lastTitle string
	body      []byte // the probe's playlist, used for the first load
	opts      Options
	lastHash  uint64
	nextSeq   uint64
	failures  int
	started   bool
	// vod is a playlist with an end, which resumes where it was.
	vod bool
}

func newHLSSource(opts Options, u *url.URL, body []byte) *hlsSource {
	return &hlsSource{
		opts:  opts,
		url:   u,
		body:  body,
		keys:  map[string][]byte{},
		inits: map[string]*mp4Track{},
	}
}

func (h *hlsSource) run(ctx context.Context, sk *sink, fresh bool) error {
	if fresh && !h.vod {
		h.started = false
	}
	h.ts = newTSDemuxer(func(codec nativedec.Kind, es []byte) error {
		return sk.write(ctx, codec, es)
	})
	lastNew := time.Now()
	for {
		loadStart := time.Now()
		pl, hash, err := h.loadMedia(ctx)
		if err != nil {
			return err
		}
		changed := hash != h.lastHash
		h.lastHash = hash
		h.vod = pl.endList
		h.position(pl, sk)

		played := false
		for i := range pl.segments {
			seg := &pl.segments[i]
			if seg.seq < h.nextSeq {
				continue
			}
			if err := h.playSegment(ctx, sk, seg); err != nil {
				var segErr *segmentError
				if !errors.As(err, &segErr) {
					return err
				}
				h.failures++
				log.Warn().Err(err).Str("url", seg.uri).Msg("radio: skipping hls segment")
				if h.failures >= maxSegmentFailures {
					h.failures = 0
					return err
				}
			} else {
				h.failures = 0
			}
			h.nextSeq = seg.seq + 1
			played = true
		}
		if pl.endList {
			if err := sk.flush(ctx); err != nil {
				return err
			}
			return errEnded
		}
		if played {
			lastNew = time.Now()
		} else if time.Since(lastNew) > stallTargets*pl.targetDuration {
			return errors.New("radio: hls playlist stopped updating")
		}
		// RFC 8216 6.3.4: reload after the target duration, or half of it
		// when the last reload brought nothing new.
		wait := pl.targetDuration
		if !changed {
			wait /= 2
		}
		wait = time.Duration(float64(wait)*h.opts.timing.reloadScale) - time.Since(loadStart)
		if wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return context.Cause(ctx) //nolint:wrapcheck // the stream's own stop signal
			case <-t.C:
			}
		}
	}
}

// position picks the segment to play next: the start, or the live point
// on a first load, after a suspend, and when the stream fell out of the
// playlist's window or its sequence went backwards.
func (h *hlsSource) position(pl *hlsPlaylist, sk *sink) {
	if len(pl.segments) == 0 {
		return
	}
	first := pl.segments[0].seq
	last := pl.segments[len(pl.segments)-1].seq
	if h.started && h.nextSeq >= first && h.nextSeq <= last+1 {
		return
	}
	start := 0
	if !pl.endList {
		start = max(0, len(pl.segments)-liveStartSegments)
	}
	if h.started {
		log.Debug().Uint64("next", h.nextSeq).Uint64("first", first).Uint64("last", last).
			Msg("radio: hls stream out of the playlist window, jumping to the live point")
		sk.reset()
		h.ts.reset()
	}
	h.started = true
	h.nextSeq = pl.segments[start].seq
}

// loadMedia loads the media playlist, choosing a variant from a master.
func (h *hlsSource) loadMedia(ctx context.Context) (*hlsPlaylist, uint64, error) {
	var (
		body []byte
		base *url.URL
		err  error
	)
	switch {
	case h.mediaURL != nil:
		body, base, err = h.fetch(ctx, h.mediaURL.String(), nil, maxPlaylistBytes, playlistTimeout)
	case h.body != nil:
		body, base = h.body, h.url
		h.body = nil
	default:
		body, base, err = h.fetch(ctx, h.url.String(), nil, maxPlaylistBytes, playlistTimeout)
	}
	if err != nil {
		return nil, 0, err
	}
	for range maxMasterDepth + 1 {
		pl, perr := parseM3U8(body, base)
		if perr != nil {
			return nil, 0, fmt.Errorf("radio: playlist %s: %w", base, perr)
		}
		if !pl.isMaster() {
			h.mediaURL = base
			hash := fnv.New64a()
			_, _ = hash.Write(body)
			return pl, hash.Sum64(), nil
		}
		uri, ok := pickVariant(pl, h.opts.ResourceConstrained)
		if !ok {
			return nil, 0, permanent(fmt.Errorf("%w: no audio variant in %s", ErrUnsupported, base))
		}
		body, base, err = h.fetch(ctx, uri, nil, maxPlaylistBytes, playlistTimeout)
		if err != nil {
			return nil, 0, err
		}
	}
	return nil, 0, permanent(fmt.Errorf("radio: master playlists nested deeper than %d", maxMasterDepth))
}

// playSegment loads, decrypts, demuxes and decodes one segment. A load
// failure is a *segmentError.
func (h *hlsSource) playSegment(ctx context.Context, sk *sink, seg *hlsSegment) error {
	if seg.discontinuity {
		sk.reset()
		h.ts.reset()
	}
	data, _, err := h.fetch(ctx, seg.uri, seg.rng, maxSegmentBytes, segmentTimeout)
	if err != nil {
		return h.loadErr(ctx, err)
	}
	if seg.key != nil {
		if data, err = h.decrypt(ctx, seg, data); err != nil {
			return err
		}
	}
	if seg.initMap != nil {
		t, ierr := h.initTrack(ctx, seg.initMap)
		if ierr != nil {
			return ierr
		}
		_, err = fmp4Samples(data, t, func(frame []byte) error {
			return sk.write(ctx, t.codec, frame)
		})
		return err
	}
	if looksLikeTS(data) {
		return h.ts.write(data)
	}
	if child(data, "moov") != nil || child(data, "moof") != nil {
		t, perr := parseInit(data)
		if perr != nil {
			return &segmentError{err: perr}
		}
		_, err = fmp4Samples(data, t, func(frame []byte) error {
			return sk.write(ctx, t.codec, frame)
		})
		return err
	}
	data = h.packedID3(data)
	codec := sniffCodec(data)
	if codec == codecNone || codec == nativedec.Vorbis {
		codec = codecForExt(seg.uri)
	}
	if codec == codecNone {
		return &segmentError{err: fmt.Errorf("radio: unknown segment format at %s", seg.uri)}
	}
	return sk.write(ctx, codec, data)
}

// loadErr wraps a load failure as a segment error unless the stream is
// stopping.
func (*hlsSource) loadErr(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause //nolint:wrapcheck // the stream's own stop signal
	}
	var perm *permanentError
	if errors.As(err, &perm) {
		return err
	}
	return &segmentError{err: err}
}

// codecForExt guesses a packed audio segment's codec from its name.
func codecForExt(rawURL string) nativedec.Kind {
	u, err := url.Parse(rawURL)
	if err != nil {
		return codecNone
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".aac", ".adts":
		return nativedec.AAC
	case ".mp3", ".mpga":
		return nativedec.MP3
	default:
		return codecNone
	}
}

func (h *hlsSource) decrypt(ctx context.Context, seg *hlsSegment, data []byte) ([]byte, error) {
	if seg.key.method != "AES-128" || seg.key.uri == "" {
		return nil, permanent(fmt.Errorf("%w: hls encryption %q", ErrUnsupported, seg.key.method))
	}
	key, ok := h.keys[seg.key.uri]
	if !ok {
		b, _, err := h.fetch(ctx, seg.key.uri, nil, 16, playlistTimeout)
		if err != nil {
			return nil, h.loadErr(ctx, fmt.Errorf("radio: load key: %w", err))
		}
		if len(b) != 16 {
			return nil, &segmentError{err: fmt.Errorf("radio: hls key is %d bytes, not 16", len(b))}
		}
		if len(h.keys) >= maxCached {
			clear(h.keys)
		}
		h.keys[seg.key.uri] = b
		key = b
	}
	iv := seg.key.iv
	if iv == nil {
		iv = make([]byte, 16)
		binary.BigEndian.PutUint64(iv[8:], seg.seq)
	}
	out, err := decryptAES128(data, key, iv)
	if err != nil {
		return nil, &segmentError{err: err}
	}
	return out, nil
}

// decryptAES128 decrypts AES-128-CBC with PKCS7 padding in place.
func decryptAES128(data, key, iv []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("radio: encrypted segment of %d bytes is not whole blocks", len(data))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("radio: aes key: %w", err)
	}
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(data, data)
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize {
		return nil, errors.New("radio: bad padding in decrypted segment")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, errors.New("radio: bad padding in decrypted segment")
		}
	}
	return data[:len(data)-pad], nil
}

// initTrack loads and parses an fMP4 init segment, caching it.
func (h *hlsSource) initTrack(ctx context.Context, m *hlsMap) (*mp4Track, error) {
	key := m.uri
	if m.rng != nil {
		key = fmt.Sprintf("%s@%d-%d", m.uri, m.rng.offset, m.rng.length)
	}
	if t, ok := h.inits[key]; ok {
		return t, nil
	}
	data, _, err := h.fetch(ctx, m.uri, m.rng, maxInitBytes, segmentTimeout)
	if err != nil {
		return nil, h.loadErr(ctx, fmt.Errorf("radio: load init segment: %w", err))
	}
	t, err := parseInit(data)
	if err != nil {
		return nil, permanent(fmt.Errorf("%w: %w", ErrUnsupported, err))
	}
	if len(h.inits) >= maxCached {
		clear(h.inits)
	}
	h.inits[key] = t
	return t, nil
}

// fetch GETs rawURL, or the byte range rng of it, reading at most limit
// bytes. It returns the body and the URL after redirects.
func (h *hlsSource) fetch(
	ctx context.Context, rawURL string, rng *hlsByteRange, limit int64, timeout time.Duration,
) ([]byte, *url.URL, error) {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, nil, fmt.Errorf("radio: new request: %w", err)
	}
	if rng != nil {
		if rng.length > limit {
			return nil, nil, ErrTooLarge
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rng.offset, rng.offset+rng.length-1))
	}
	resp, err := h.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("radio: get %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body io.Reader = resp.Body
	switch {
	case rng != nil && resp.StatusCode == http.StatusPartialContent:
	case rng != nil && resp.StatusCode == http.StatusOK:
		// The server sent the whole resource: cut the range out of it.
		if rng.offset > limit-rng.length {
			return nil, nil, ErrTooLarge
		}
		if _, cerr := io.CopyN(io.Discard, resp.Body, rng.offset); cerr != nil {
			return nil, nil, fmt.Errorf("radio: get %s: %w", rawURL, cerr)
		}
		body = io.LimitReader(resp.Body, rng.length)
	case resp.StatusCode >= 200 && resp.StatusCode <= 299:
	default:
		return nil, nil, fmt.Errorf("radio: get %s: status %s", rawURL, resp.Status)
	}
	data, err := readLimited(body, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("radio: get %s: %w", rawURL, err)
	}
	return data, resp.Request.URL, nil
}

// packedID3 skips the ID3 tags that start a packed audio segment,
// announcing a title they carry.
func (h *hlsSource) packedID3(data []byte) []byte {
	rest, title, artist := skipID3(data)
	if title != "" && title != h.lastTitle {
		h.lastTitle = title
		if h.opts.OnMeta != nil {
			h.opts.OnMeta(title, artist)
		}
	}
	return rest
}

// skipID3 strips leading ID3v2 tags, returning the rest and any TIT2 and
// TPE1 text in them.
func skipID3(b []byte) (rest []byte, title, artist string) {
	for range 8 {
		if len(b) < 10 || !bytes.HasPrefix(b, []byte("ID3")) {
			break
		}
		version, flags := b[3], b[5]
		size := syncsafe(b[6:10])
		total := 10 + size
		if flags&0x10 != 0 {
			total += 10
		}
		if size < 0 || total > len(b) {
			return nil, title, artist
		}
		if flags&0x80 == 0 && (version == 3 || version == 4) {
			t, a := id3Frames(b[10:10+size], version)
			if t != "" {
				title = t
			}
			if a != "" {
				artist = a
			}
		}
		b = b[total:]
	}
	return b, title, artist
}

func syncsafe(b []byte) int {
	if b[0]|b[1]|b[2]|b[3] >= 0x80 {
		return -1
	}
	return int(b[0])<<21 | int(b[1])<<14 | int(b[2])<<7 | int(b[3])
}

// id3Frames reads TIT2 and TPE1 from an ID3v2.3 or v2.4 tag body.
func id3Frames(b []byte, version byte) (title, artist string) {
	for len(b) >= 10 && b[0] != 0 {
		id := string(b[:4])
		var size int
		if version == 4 {
			size = syncsafe(b[4:8])
		} else {
			size = int(binary.BigEndian.Uint32(b[4:8]) & 0x7FFFFFFF)
		}
		if size < 0 || size > len(b)-10 {
			return title, artist
		}
		body := b[10 : 10+size]
		switch id {
		case "TIT2":
			title = id3Text(body)
		case "TPE1":
			artist = id3Text(body)
		}
		b = b[10+size:]
	}
	return title, artist
}

// id3Text decodes an ID3 text frame's first string.
func id3Text(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	enc, b := b[0], b[1:]
	var s string
	switch enc {
	case 1, 2:
		bigEndian := enc == 2
		if len(b) >= 2 {
			switch {
			case b[0] == 0xFF && b[1] == 0xFE:
				bigEndian, b = false, b[2:]
			case b[0] == 0xFE && b[1] == 0xFF:
				bigEndian, b = true, b[2:]
			}
		}
		units := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u := binary.LittleEndian.Uint16(b[i:])
			if bigEndian {
				u = binary.BigEndian.Uint16(b[i:])
			}
			if u == 0 {
				break
			}
			units = append(units, u)
		}
		s = string(utf16.Decode(units))
	case 3:
		s, _, _ = strings.Cut(string(b), "\x00")
	default:
		s, _, _ = strings.Cut(decodeLatin1(b), "\x00")
	}
	return sanitizeText(s)
}

func decodeLatin1(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}
