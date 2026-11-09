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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hlsServer serves HLS fixtures and logs the paths requested.
type hlsServer struct {
	*httptest.Server
	mux  *http.ServeMux
	reqs []string
	mu   syncutil.Mutex
}

func newHLSServer(t *testing.T) *hlsServer {
	t.Helper()
	h := &hlsServer{mux: http.NewServeMux()}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.reqs = append(h.reqs, r.URL.Path)
		h.mu.Unlock()
		h.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hlsServer) file(path, contentType string, body []byte) {
	h.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	})
}

func (h *hlsServer) requests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.reqs...)
}

func (h *hlsServer) count(path string) int {
	n := 0
	for _, r := range h.requests() {
		if r == path {
			n++
		}
	}
	return n
}

// toneChunks splits the AAC fixture's frames into n groups.
func toneChunks(t *testing.T, n int) [][][]byte {
	t.Helper()
	frames := adtsSplit(t, fixture(t, "tone.aac"))
	per := (len(frames) + n - 1) / n
	var out [][][]byte
	for len(frames) > 0 {
		k := min(per, len(frames))
		out = append(out, frames[:k])
		frames = frames[k:]
	}
	return out
}

// toneFrames is about how many frames the AAC fixture decodes to.
const toneFrames = 44100

func assertWholeTone(t *testing.T, got int) {
	t.Helper()
	assert.GreaterOrEqual(t, got, toneFrames*9/10)
	assert.LessOrEqual(t, got, toneFrames*11/10)
}

func vodPlaylist(header string, segs ...string) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:1\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	sb.WriteString(header)
	for _, s := range segs {
		if !strings.HasPrefix(s, "#") {
			sb.WriteString("#EXTINF:0.25,\n")
		}
		sb.WriteString(s + "\n")
	}
	sb.WriteString("#EXT-X-ENDLIST\n")
	return sb.String()
}

func TestHLSVODPackedAudioEnds(t *testing.T) {
	t.Parallel()
	h := newHLSServer(t)
	chunks := toneChunks(t, 4)
	segs := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		body := append(id3Tag("Packed Song", "Packed Band"), bytes.Join(chunk, nil)...)
		h.file(fmt.Sprintf("/a%d.aac", i), "audio/aac", body)
		segs = append(segs, fmt.Sprintf("a%d.aac", i))
	}
	h.file("/vod.m3u8", "application/vnd.apple.mpegurl", []byte(vodPlaylist("", segs...)))
	var meta metaLog
	s := openURL(t, h.URL+"/vod.m3u8", Options{OnMeta: meta.on})
	assert.Equal(t, 44100, s.SampleRate())
	got, ended := readFrames(t, s, 10*toneFrames)
	require.True(t, ended)
	require.NoError(t, s.Err())
	assertWholeTone(t, got)
	assert.Equal(t, []string{"Packed Band|Packed Song"}, meta.titles())
	assert.Greater(t, s.Buffered(), endedSlack-1)
	assert.Equal(t, 1, h.count("/vod.m3u8"), "the probe's playlist is reused, and a VOD is not reloaded")
}

func TestHLSVODTSWithDiscontinuityAndMissingSegment(t *testing.T) {
	t.Parallel()
	h := newHLSServer(t)
	b := newTSBuilder()
	chunks := toneChunks(t, 4)
	for i, chunk := range chunks {
		h.file(fmt.Sprintf("/s%d.ts", i), "video/mp2t", b.segment(chunk, 0x0F))
	}
	h.file("/vod.m3u8", "application/vnd.apple.mpegurl", []byte(vodPlaylist("",
		"s0.ts", "s1.ts", "#EXT-X-DISCONTINUITY", "missing.ts", "s2.ts", "s3.ts")))
	s := openURL(t, h.URL+"/vod.m3u8", Options{})
	got, ended := readFrames(t, s, 10*toneFrames)
	require.True(t, ended)
	require.NoError(t, s.Err())
	assertWholeTone(t, got)
	assert.Equal(t, 1, h.count("/missing.ts"))
}

func TestHLSMasterFMP4(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name        string
		want        string
		constrained bool
	}{
		{name: "default", want: "/mid.m3u8"},
		{name: "constrained", want: "/low.m3u8", constrained: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHLSServer(t)
			chunks := toneChunks(t, 4)
			h.file("/init.mp4", "video/mp4", fmp4Init(ascFromADTS(chunks[0][0])))
			segs := make([]string, 0, len(chunks))
			for i, chunk := range chunks {
				h.file(fmt.Sprintf("/f%d.m4s", i), "video/iso.segment", fmp4Media(chunk))
				segs = append(segs, fmt.Sprintf("f%d.m4s", i))
			}
			media := []byte(vodPlaylist("#EXT-X-MAP:URI=\"init.mp4\"\n", segs...))
			h.file("/low.m3u8", "application/vnd.apple.mpegurl", media)
			h.file("/mid.m3u8", "application/vnd.apple.mpegurl", media)
			h.file("/master.m3u8", "application/vnd.apple.mpegurl", []byte(`#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=900000,CODECS="avc1.4d401f,mp4a.40.2"
video.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=64000,CODECS="mp4a.40.5"
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=128000,CODECS="mp4a.40.2"
mid.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=256000,CODECS="mp4a.40.2"
high.m3u8
`))
			s := openURL(t, h.URL+"/master.m3u8", Options{ResourceConstrained: tt.constrained})
			got, ended := readFrames(t, s, 10*toneFrames)
			require.True(t, ended)
			require.NoError(t, s.Err())
			assertWholeTone(t, got)
			assert.Equal(t, 1, h.count(tt.want))
			assert.Equal(t, 1, h.count("/init.mp4"), "the init segment is cached")
			assert.Zero(t, h.count("/high.m3u8"))
			assert.Zero(t, h.count("/video.m3u8"))
		})
	}
}

func TestHLSAES128(t *testing.T) {
	t.Parallel()
	h := newHLSServer(t)
	key := []byte("0123456789abcdef")
	iv := bytes.Repeat([]byte{0x42}, 16)
	b := newTSBuilder()
	chunks := toneChunks(t, 4)
	for i, chunk := range chunks {
		segIV := iv
		if i >= 2 {
			// No IV attribute: the media sequence number is the IV.
			segIV = make([]byte, 16)
			segIV[15] = u8(10 + i)
		}
		h.file(fmt.Sprintf("/e%d.ts", i), "video/mp2t", encryptAES128(t, b.segment(chunk, 0x0F), key, segIV))
	}
	h.file("/key", "application/octet-stream", key)
	h.file("/vod.m3u8", "application/vnd.apple.mpegurl", []byte(vodPlaylist(
		"#EXT-X-MEDIA-SEQUENCE:10\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\",IV=0x42424242424242424242424242424242\n",
		"e0.ts", "e1.ts", "#EXT-X-KEY:METHOD=AES-128,URI=\"key\"", "e2.ts", "e3.ts")))
	s := openURL(t, h.URL+"/vod.m3u8", Options{})
	got, ended := readFrames(t, s, 10*toneFrames)
	require.True(t, ended)
	require.NoError(t, s.Err())
	assertWholeTone(t, got)
	assert.Equal(t, 1, h.count("/key"), "the key is cached")
}

func TestHLSSampleAESUnsupported(t *testing.T) {
	t.Parallel()
	h := newHLSServer(t)
	h.file("/s.ts", "video/mp2t", newTSBuilder().segment(toneChunks(t, 1)[0], 0x0F))
	h.file("/vod.m3u8", "application/vnd.apple.mpegurl", []byte(vodPlaylist(
		"#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key\"\n", "s.ts")))
	p, err := ProbeURL(context.Background(), nil, h.URL+"/vod.m3u8")
	require.NoError(t, err)
	_, err = Open(context.Background(), p, Options{timing: fastTiming()})
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestHLSByteRanges(t *testing.T) {
	t.Parallel()
	h := newHLSServer(t)
	var all []byte
	chunks := toneChunks(t, 4)
	segs := make([]string, 0, 2*len(chunks))
	for _, chunk := range chunks {
		data := bytes.Join(chunk, nil)
		segs = append(segs, fmt.Sprintf("#EXT-X-BYTERANGE:%d", len(data)), "all.aac")
		all = append(all, data...)
	}
	h.file("/all.aac", "audio/aac", all)
	pl := vodPlaylist("", segs...)
	// vodPlaylist writes EXTINF only before URIs; byte range tags come
	// first, which the parser accepts in any order.
	h.file("/vod.m3u8", "application/vnd.apple.mpegurl", []byte(pl))
	s := openURL(t, h.URL+"/vod.m3u8", Options{})
	got, ended := readFrames(t, s, 10*toneFrames)
	require.True(t, ended)
	require.NoError(t, s.Err())
	assertWholeTone(t, got)
	assert.Equal(t, 4, h.count("/all.aac"))
}

// liveServer serves a sliding live playlist: each reload moves the window
// on by one segment, and jump, when set, moves the sequence far ahead
// from the third reload on.
type liveServer struct {
	*hlsServer
	segments [][]byte
	reloads  int
	jump     bool
}

func newLiveServer(t *testing.T, jump bool) *liveServer {
	t.Helper()
	l := &liveServer{hlsServer: newHLSServer(t), jump: jump}
	for _, chunk := range toneChunks(t, 4) {
		l.segments = append(l.segments, newTSBuilder().segment(chunk, 0x0F))
	}
	l.mux.HandleFunc("/live.m3u8", func(w http.ResponseWriter, _ *http.Request) {
		l.mu.Lock()
		seq := l.reloads
		if l.jump && l.reloads >= 2 {
			seq += 1000
		}
		l.reloads++
		l.mu.Unlock()
		var sb strings.Builder
		fmt.Fprintf(&sb, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:%d\n", seq)
		for i := range 5 {
			fmt.Fprintf(&sb, "#EXTINF:0.25,\nseg/%d.ts\n", seq+i)
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte(sb.String()))
	})
	l.mux.HandleFunc("/seg/", func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/seg/%d.ts", &n); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write(l.segments[n%len(l.segments)])
	})
	return l
}

func (l *liveServer) segs() []string {
	var out []string
	for _, r := range l.requests() {
		if strings.HasPrefix(r, "/seg/") {
			out = append(out, r)
		}
	}
	return out
}

func TestHLSLiveSlidingWindow(t *testing.T) {
	t.Parallel()
	l := newLiveServer(t, false)
	s := openURL(t, l.URL+"/live.m3u8", Options{})
	got, ended := readFrames(t, s, 3*toneFrames)
	assert.False(t, ended)
	assert.GreaterOrEqual(t, got, 3*toneFrames)
	require.NoError(t, s.Err())
	segs := l.segs()
	require.NotEmpty(t, segs)
	assert.Equal(t, "/seg/2.ts", segs[0], "a live stream starts three segments from the end")
	// Every segment is fetched once, in order, as the window slides.
	for i, seg := range segs {
		assert.Equal(t, fmt.Sprintf("/seg/%d.ts", i+2), seg)
	}
	assert.Greater(t, l.count("/live.m3u8"), 2)
}

func TestHLSLiveJumpsWhenSequenceMoves(t *testing.T) {
	t.Parallel()
	l := newLiveServer(t, true)
	s := openURL(t, l.URL+"/live.m3u8", Options{})
	require.Eventually(t, func() bool {
		buf := make([][2]float64, 4096)
		_, _ = s.Stream(buf)
		for _, seg := range l.segs() {
			if seg == "/seg/1004.ts" {
				return true
			}
		}
		return false
	}, 10*time.Second, time.Millisecond)
	for _, seg := range l.segs() {
		var n int
		_, err := fmt.Sscanf(seg, "/seg/%d.ts", &n)
		require.NoError(t, err)
		assert.True(t, n < 10 || n >= 1004, "segment %d skipped by the jump to the live point", n)
	}
	require.NoError(t, s.Err())
}

func TestHLSSuspendResumeJumpsToLive(t *testing.T) {
	t.Parallel()
	l := newLiveServer(t, false)
	s := openURL(t, l.URL+"/live.m3u8", Options{})
	require.Eventually(t, func() bool { return s.Buffered() > 0 }, 5*time.Second, time.Millisecond)
	s.Suspend()
	assert.Zero(t, s.Buffered())
	l.mu.Lock()
	l.reloads += 50
	l.mu.Unlock()
	s.Resume()
	got, _ := readFrames(t, s, toneFrames)
	assert.GreaterOrEqual(t, got, toneFrames)
	found := false
	for _, seg := range l.segs() {
		var n int
		_, _ = fmt.Sscanf(seg, "/seg/%d.ts", &n)
		if n >= 50 {
			found = true
		}
	}
	assert.True(t, found, "resume reloads at the live point")
}

func TestHLSFetchLimits(t *testing.T) {
	t.Parallel()
	h := newHLSServer(t)
	h.file("/big", "application/octet-stream", bytes.Repeat([]byte{1}, 2000))
	src := newHLSSource(Options{HTTPClient: NewHTTPClient()}, nil, nil)
	_, _, err := src.fetch(context.Background(), h.URL+"/big", nil, 1000, time.Second)
	require.ErrorIs(t, err, ErrTooLarge)
	ctx := context.Background()
	b, _, err := src.fetch(ctx, h.URL+"/big", &hlsByteRange{offset: 100, length: 50}, 1000, time.Second)
	require.NoError(t, err)
	assert.Len(t, b, 50)
	_, _, err = src.fetch(context.Background(), h.URL+"/big", &hlsByteRange{offset: 0, length: 5000}, 1000, time.Second)
	require.ErrorIs(t, err, ErrTooLarge)

	// A server that ignores Range still yields the range.
	h.mux.HandleFunc("/norange", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	})
	b, _, err = src.fetch(ctx, h.URL+"/norange", &hlsByteRange{offset: 3, length: 4}, 1000, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "3456", string(b))
}
