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
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// maxServedBytes bounds what a test stream server writes to one client.
const maxServedBytes = 32 << 20

// icyServer streams audio in a loop with ICY metadata. titles change every
// metadata block; conns counts connections; limit, when set, ends a
// connection after that many audio bytes.
type icyServer struct {
	contentType string
	name        string
	audio       []byte
	titles      []string
	metaint     int
	// limitFirst ends the first connection after that many audio bytes.
	limitFirst int
	// stallAfter blocks a connection after that many audio bytes until
	// the client goes away.
	stallAfter int
	conns      atomic.Int32
	// failAfterFirst answers every later request with 503.
	failAfterFirst bool
}

func (s *icyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := s.conns.Add(1)
	if n > 1 && s.failAfterFirst {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", s.contentType)
	if s.name != "" {
		w.Header().Set("icy-name", s.name)
	}
	metaint := 0
	if r.Header.Get("Icy-MetaData") == "1" && s.metaint > 0 {
		metaint = s.metaint
		w.Header().Set("icy-metaint", strconv.Itoa(metaint))
	}
	limit := maxServedBytes
	if n == 1 && s.limitFirst > 0 {
		limit = s.limitFirst
	}
	stall := s.stallAfter
	sent, sinceMeta, block := 0, 0, 0
	for sent < limit {
		chunk := s.audio[sent%len(s.audio):]
		chunk = chunk[:min(len(chunk), limit-sent, 4096)]
		if metaint > 0 {
			chunk = chunk[:min(len(chunk), metaint-sinceMeta)]
		}
		if stall > 0 && sent+len(chunk) > stall {
			chunk = chunk[:stall-sent]
		}
		if _, err := w.Write(chunk); err != nil {
			return
		}
		sent += len(chunk)
		sinceMeta += len(chunk)
		if metaint > 0 && sinceMeta == metaint {
			meta := ""
			if len(s.titles) > 0 {
				meta = s.titles[block%len(s.titles)]
			}
			block++
			sinceMeta = 0
			if _, err := w.Write(icyBlock(meta)); err != nil {
				return
			}
		}
		if stall > 0 && sent == stall {
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
	}
}

func openURL(t *testing.T, rawURL string, opts Options) *Stream {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	p, err := ProbeURL(ctx, nil, rawURL)
	cancel() // the caller cancels at once; the stream must survive it
	require.NoError(t, err)
	if opts.timing.retry == nil {
		opts.timing = fastTiming()
	}
	s, err := Open(context.Background(), p, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// metaLog records OnMeta calls.
type metaLog struct {
	got []string
	mu  syncutil.Mutex
}

func (m *metaLog) on(title, artist string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.got = append(m.got, artist+"|"+title)
}

func (m *metaLog) titles() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.got...)
}

func TestIcecastStreamWithMetadata(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(&icyServer{
		contentType: "audio/mpeg",
		name:        "  Test FM ",
		audio:       fixture(t, "tone.mp3"),
		metaint:     2000,
		titles:      []string{"StreamTitle='Band - First';", "StreamTitle='Band - First';", "StreamTitle='Second';"},
	})
	t.Cleanup(srv.Close)
	var meta metaLog
	s := openURL(t, srv.URL+"/live", Options{OnMeta: meta.on})
	assert.Equal(t, 44100, s.SampleRate())
	assert.Equal(t, "Test FM", s.Name())
	assert.True(t, s.Live())
	assert.Equal(t, -1, s.Len())
	require.ErrorIs(t, s.Seek(0), ErrLive)

	got, ended := readFrames(t, s, 3*44100)
	assert.False(t, ended)
	assert.GreaterOrEqual(t, got, 3*44100)
	assert.Equal(t, got, s.Position())
	require.NoError(t, s.Err())
	require.Eventually(t, func() bool { return len(meta.titles()) >= 2 }, 5*time.Second, time.Millisecond)
	assert.Equal(t, []string{"Band|First", "|Second"}, meta.titles()[:2])
}

func TestStreamFillsWhatIsBuffered(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(&icyServer{contentType: "audio/aac", audio: fixture(t, "tone.aac")})
	t.Cleanup(srv.Close)
	s := openURL(t, srv.URL, Options{})
	assert.Equal(t, 44100, s.SampleRate())
	require.Eventually(t, func() bool { return s.Buffered() >= 8192 }, 5*time.Second, time.Millisecond)
	buf := make([][2]float64, s.Buffered())
	n, ok := s.Stream(buf)
	assert.True(t, ok)
	assert.Equal(t, len(buf), n)
	var energy float64
	for _, f := range buf {
		energy += f[0] * f[0]
	}
	assert.Greater(t, energy/float64(len(buf)), 0.001, "decoded audio is not silence")
}

func TestShoutcastICYStatusLine(t *testing.T) {
	t.Parallel()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	audio := fixture(t, "tone.mp3")
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				br := bufio.NewReader(c)
				for {
					line, err := br.ReadString('\n')
					if err != nil || line == "\r\n" {
						break
					}
				}
				_, _ = c.Write([]byte("ICY 200 OK\r\nicy-name:Shout Radio\r\n" +
					"content-type:audio/mpeg\r\nicy-metaint:0\r\n\r\n"))
				for sent := 0; sent < maxServedBytes; sent += len(audio) {
					if _, err := c.Write(audio); err != nil {
						return
					}
				}
			}(c)
		}
	}()
	s := openURL(t, "http://"+ln.Addr().String()+"/;stream.mp3", Options{})
	assert.Equal(t, "Shout Radio", s.Name())
	got, _ := readFrames(t, s, 44100)
	assert.GreaterOrEqual(t, got, 44100)
}

func TestProbeClassification(t *testing.T) {
	t.Parallel()
	mp3 := fixture(t, "tone.mp3")
	routes := map[string]struct {
		ct   string
		body []byte
	}{
		"/mpeg":        {"audio/mpeg", mp3},
		"/aacp":        {"audio/aacp; charset=x", fixture(t, "tone.aac")},
		"/ogg":         {"application/ogg", fixture(t, "tone.ogg")},
		"/octet":       {"application/octet-stream", mp3[len(mp3)/2:]},
		"/sniffog":     {"", fixture(t, "tone.ogg")},
		"/html":        {"text/html", []byte("<html><body>hi</body></html>")},
		"/garbage":     {"application/octet-stream", []byte(strings.Repeat("x", 1000))},
		"/pls":         {"audio/x-scpls", []byte("[playlist]\nFile1=http://a/b\n")},
		"/m3u":         {"audio/x-mpegurl", []byte("#EXTM3U\nhttp://a/b\n")},
		"/hls":         {"audio/mpegurl", []byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\na.ts\n")},
		"/textpls":     {"text/plain", []byte("[playlist]\nFile1=http://a/b\n")},
		"/bare":        {"application/octet-stream", []byte("http://a/b\n")},
		"/hlsplain":    {"text/plain", []byte("#EXTM3U\n#EXT-X-VERSION:3\n")},
		"/mpegurlhtml": {"application/vnd.apple.mpegurl", []byte("<html>")},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rt, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if rt.ct != "" {
			w.Header().Set("Content-Type", rt.ct)
		} else {
			w.Header()["Content-Type"] = nil
		}
		_, _ = w.Write(rt.body)
	}))
	t.Cleanup(srv.Close)
	want := map[string]Kind{
		"/mpeg": KindStream, "/aacp": KindStream, "/ogg": KindStream, "/octet": KindStream,
		"/sniffog": KindStream, "/html": KindNotAudio, "/garbage": KindNotAudio, "/pls": KindPlaylist,
		"/m3u": KindPlaylist, "/hls": KindHLS, "/textpls": KindPlaylist, "/bare": KindPlaylist,
		"/hlsplain": KindHLS, "/mpegurlhtml": KindNotAudio,
	}
	for path, kind := range want {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			p, err := ProbeURL(context.Background(), nil, srv.URL+path)
			require.NoError(t, err)
			defer p.Close()
			assert.Equal(t, kind, p.Kind(), "kind is %s", p.Kind())
			assert.Equal(t, srv.URL+path, p.URL())
		})
	}

	_, err := ProbeURL(context.Background(), nil, srv.URL+"/missing")
	require.Error(t, err)
	_, err = ProbeURL(context.Background(), nil, "file:///etc/passwd")
	require.Error(t, err)

	var nilProbe *Probe
	nilProbe.Close()
	assert.Equal(t, KindNotAudio, nilProbe.Kind())
}

// The station name a server sends is kept, made safe to show.
func TestProbeName(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		if r.URL.Path == "/named" {
			w.Header().Set("icy-name", " Caf\xe9 Radio ")
		}
		_, _ = w.Write(make([]byte, 64))
	}))
	t.Cleanup(srv.Close)

	p, err := ProbeURL(context.Background(), nil, srv.URL+"/named")
	require.NoError(t, err)
	defer p.Close()
	assert.Equal(t, "Café Radio", p.Name(), "Latin-1 is decoded and the name trimmed")

	unnamed, err := ProbeURL(context.Background(), nil, srv.URL+"/plain")
	require.NoError(t, err)
	defer unnamed.Close()
	assert.Empty(t, unnamed.Name())

	var nilProbe *Probe
	assert.Empty(t, nilProbe.Name())
}

func TestProbePlaylistTooLarge(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n"))
		line := []byte(strings.Repeat("#", 1023) + "\n")
		for range maxPlaylistBytes/len(line) + 1 {
			if _, err := w.Write(line); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	_, err := ProbeURL(context.Background(), nil, srv.URL)
	require.ErrorIs(t, err, ErrTooLarge)
}

func TestProbeHeaderTimeoutFollowsCtx(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ProbeURL(ctx, nil, srv.URL)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestPlaylistIndirection(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.Handle("/stream", &icyServer{contentType: "audio/mpeg", audio: fixture(t, "tone.mp3")})
	mux.HandleFunc("/outer.pls", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/x-scpls")
		_, _ = w.Write([]byte("[playlist]\nFile1=/dead\nFile2=/html\nFile3=/inner.m3u\nTitle3=Outer\n"))
	})
	mux.HandleFunc("/inner.m3u", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1,Inner Station\n/stream\n"))
	})
	mux.HandleFunc("/html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>"))
	})
	mux.HandleFunc("/loop.m3u", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		_, _ = w.Write([]byte("http://" + r.Host + "/loop.m3u\n")) //nolint:gosec // test server echoes its own host
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	s := openURL(t, srv.URL+"/outer.pls", Options{})
	assert.Equal(t, "Inner Station", s.Name())
	got, _ := readFrames(t, s, 44100)
	assert.GreaterOrEqual(t, got, 44100)

	// A playlist that names itself ends at the depth limit.
	p, err := ProbeURL(context.Background(), nil, srv.URL+"/loop.m3u")
	require.NoError(t, err)
	_, err = Open(context.Background(), p, Options{timing: fastTiming()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nested")
}

func TestOpusIsUnsupported(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(&icyServer{contentType: "audio/ogg", audio: fixture(t, "tone-opus.ogg")})
	t.Cleanup(srv.Close)
	p, err := ProbeURL(context.Background(), nil, srv.URL)
	require.NoError(t, err)
	_, err = Open(context.Background(), p, Options{timing: fastTiming()})
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestOpenNotAudio(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>"))
	}))
	t.Cleanup(srv.Close)
	p, err := ProbeURL(context.Background(), nil, srv.URL)
	require.NoError(t, err)
	_, err = Open(context.Background(), p, Options{})
	require.Error(t, err)
	_, err = Open(context.Background(), nil, Options{})
	require.Error(t, err)
}

func TestReconnectAfterDrop(t *testing.T) {
	t.Parallel()
	audio := fixture(t, "tone.mp3")
	srv := &icyServer{contentType: "audio/mpeg", audio: audio, limitFirst: 2 * len(audio)}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	s := openURL(t, ts.URL, Options{})
	got, ended := readFrames(t, s, 2*44100)
	assert.False(t, ended)
	assert.GreaterOrEqual(t, got, 2*44100)
	assert.GreaterOrEqual(t, srv.conns.Load(), int32(2))
	require.NoError(t, s.Err())
}

func TestGiveUpAfterRetries(t *testing.T) {
	t.Parallel()
	audio := fixture(t, "tone.mp3")
	srv := &icyServer{contentType: "audio/mpeg", audio: audio, limitFirst: 2 * len(audio), failAfterFirst: true}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	s := openURL(t, ts.URL, Options{})
	_, ended := readFrames(t, s, 100*44100)
	assert.True(t, ended)
	require.Error(t, s.Err())
	assert.Contains(t, s.Err().Error(), "giving up")
	assert.Equal(t, 1+len(fastTiming().retry), int(srv.conns.Load()))
	assert.Greater(t, s.Buffered(), endedSlack-1)
	n, ok := s.Stream(make([][2]float64, 10))
	assert.Zero(t, n)
	assert.False(t, ok)
}

func TestIdleConnectionReconnects(t *testing.T) {
	t.Parallel()
	audio := fixture(t, "tone.mp3")
	srv := &icyServer{contentType: "audio/mpeg", audio: audio, stallAfter: 3 * len(audio)}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	tm := fastTiming()
	tm.idle = 100 * time.Millisecond
	s := openURL(t, ts.URL, Options{timing: tm})
	require.Eventually(t, func() bool {
		buf := make([][2]float64, 4096)
		_, _ = s.Stream(buf)
		return srv.conns.Load() >= 2
	}, 10*time.Second, time.Millisecond)
	require.NoError(t, s.Err())
}

func TestCloseWhileBlocked(t *testing.T) {
	audio := fixture(t, "tone.mp3")
	srv := &icyServer{contentType: "audio/mpeg", audio: audio, stallAfter: 3 * len(audio)}
	ts := httptest.NewServer(srv)
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	s := openURL(t, ts.URL, Options{})
	done := make(chan struct{})
	go func() {
		_ = s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked")
	}
	require.NoError(t, s.Close(), "Close twice")
	n, ok := s.Stream(make([][2]float64, 10))
	assert.Zero(t, n)
	assert.False(t, ok)
	ts.Close()
	s.opts.HTTPClient.CloseIdleConnections()
}

func TestSuspendResume(t *testing.T) {
	t.Parallel()
	srv := &icyServer{contentType: "audio/mpeg", audio: fixture(t, "tone.mp3")}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	s := openURL(t, ts.URL, Options{})
	require.Eventually(t, func() bool { return s.Buffered() > 0 }, 5*time.Second, time.Millisecond)
	s.Suspend()
	assert.Zero(t, s.Buffered())
	n, ok := s.Stream(make([][2]float64, 10))
	assert.Zero(t, n)
	assert.True(t, ok, "a suspended stream has not ended")
	assert.Equal(t, int32(1), srv.conns.Load())
	s.Resume()
	got, _ := readFrames(t, s, 44100)
	assert.GreaterOrEqual(t, got, 44100)
	assert.Equal(t, int32(2), srv.conns.Load())
	require.NoError(t, s.Err())
}
