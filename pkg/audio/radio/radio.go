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

// Package radio plays internet radio: Icecast and Shoutcast streams, pls
// and m3u playlists of them, and audio HLS. A background goroutine owns
// the network and the decoder and fills a bounded buffer of PCM, so the
// audio player reading a Stream never waits on the network.
package radio

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/tlsroots"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
)

// Kind is what a probed URL serves.
type Kind int

const (
	// KindNotAudio is anything this package cannot play, such as a web page.
	KindNotAudio Kind = iota
	// KindStream is a direct Icecast or Shoutcast stream of MP3, AAC or Ogg
	// Vorbis.
	KindStream
	// KindHLS is an HTTP Live Streaming playlist.
	KindHLS
	// KindPlaylist is a pls or m3u playlist of stream URLs.
	KindPlaylist
)

func (k Kind) String() string {
	switch k {
	case KindStream:
		return "stream"
	case KindHLS:
		return "hls"
	case KindPlaylist:
		return "playlist"
	default:
		return "not audio"
	}
}

var (
	// ErrLive is returned by Stream.Seek: a live stream has no positions.
	ErrLive = errors.New("radio: live streams cannot seek")
	// ErrUnsupported is a stream in a codec or format this package does not
	// play, such as Opus.
	ErrUnsupported = errors.New("radio: unsupported stream format")
	// ErrTooLarge is a response bigger than its limit.
	ErrTooLarge = errors.New("radio: response too large")
	// ErrClosed is returned once a Stream is closed.
	ErrClosed = errors.New("radio: stream closed")
)

// Network limits. A stream is untrusted, so every read is bounded.
const (
	dialTimeout           = 10 * time.Second
	tlsTimeout            = 10 * time.Second
	responseHeaderTimeout = 15 * time.Second
	maxRedirects          = 10
	// maxHeaderBytes bounds the response headers, ICY ones included.
	maxHeaderBytes = 64 << 10
)

// Options configures Open.
type Options struct {
	// OnMeta is called with the new title whenever the stream announces
	// one (ICY StreamTitle, HLS ID3 tags). It runs on the network
	// goroutine, so it must not block or call Close.
	OnMeta func(title, artist string)
	// HTTPClient makes every request; nil uses NewHTTPClient.
	HTTPClient *http.Client
	// timing holds test overrides; the zero value is the real timing.
	timing timing
	// ResourceConstrained picks the lowest bandwidth HLS variant.
	ResourceConstrained bool
}

// timing is how long the stream waits for things. Tests shorten it.
type timing struct {
	// retry is the wait before each reconnect attempt; after the last the
	// stream gives up.
	retry []time.Duration
	// open bounds how long Open waits for the first audio.
	open time.Duration
	// idle bounds a read that gets no data.
	idle time.Duration
	// reloadScale multiplies HLS playlist reload intervals.
	reloadScale float64
}

func (t timing) withDefaults() timing {
	if t.retry == nil {
		t.retry = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	}
	if t.open <= 0 {
		t.open = 20 * time.Second
	}
	if t.idle <= 0 {
		t.idle = 20 * time.Second
	}
	if t.reloadScale <= 0 {
		t.reloadScale = 1
	}
	return t
}

// NewHTTPClient returns the client radio uses: the Zaparoo User-Agent, the
// configured TLS roots, bounded dial, TLS and header waits, at most ten
// redirects, and plain http allowed, as many radio stations still serve
// it. It reads Shoutcast v1 servers, which answer "ICY 200 OK" instead of
// an HTTP status line. There is no overall timeout: a stream is endless.
// It is meant for audio only.
func NewHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, fmt.Errorf("dial %s: %w", addr, err)
			}
			return newICYConn(c), nil
		},
		TLSHandshakeTimeout:    tlsTimeout,
		ResponseHeaderTimeout:  responseHeaderTimeout,
		MaxResponseHeaderBytes: maxHeaderBytes,
		MaxIdleConns:           8,
		IdleConnTimeout:        30 * time.Second,
		ExpectContinueTimeout:  time.Second,
	}
	t := tlsroots.Transport(base)
	// TLS runs below the ICY rewrite, so the transport cannot do the
	// handshake itself on the wrapped connection.
	tlsConf := t.TLSClientConfig
	t.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		raw, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", addr, err)
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		var conf *tls.Config
		if tlsConf != nil {
			conf = tlsConf.Clone()
		} else {
			conf = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		if conf.ServerName == "" {
			conf.ServerName = host
		}
		tc := tls.Client(raw, conf)
		hctx, cancel := context.WithTimeout(ctx, tlsTimeout)
		defer cancel()
		if err := tc.HandshakeContext(hctx); err != nil {
			_ = raw.Close()
			return nil, fmt.Errorf("tls handshake with %s: %w", addr, err)
		}
		return newICYConn(tc), nil
	}
	return &http.Client{
		Transport: useragent.Transport(t),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("radio: more than %d redirects", maxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("radio: redirect to unsupported scheme %q", req.URL.Scheme)
			}
			return nil
		},
	}
}

// icyConn rewrites a Shoutcast v1 status line, "ICY 200 OK", to one
// net/http accepts. Only the first bytes the connection reads are looked
// at: a keep-alive connection's later responses are left alone.
type icyConn struct {
	net.Conn
	pending []byte
	checked bool
}

func newICYConn(c net.Conn) net.Conn {
	return &icyConn{Conn: c}
}

var (
	icyStatus  = []byte("ICY ")
	httpStatus = []byte("HTTP/1.0 ")
)

func (c *icyConn) Read(p []byte) (int, error) {
	if !c.checked {
		c.checked = true
		head := make([]byte, len(icyStatus))
		n, err := readAtLeast(c.Conn, head)
		head = head[:n]
		if n == len(icyStatus) && bytes.Equal(head, icyStatus) {
			head = append(append([]byte{}, httpStatus...), head[len(icyStatus):]...)
		}
		c.pending = head
		if len(c.pending) == 0 && err != nil {
			return 0, err //nolint:wrapcheck // a net.Conn passes its errors through
		}
	}
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	return c.Conn.Read(p) //nolint:wrapcheck // a net.Conn passes its errors through
}

// readAtLeast fills buf unless the reader stops first; it returns what it
// read and the error that stopped it.
func readAtLeast(r interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	got := 0
	for got < len(buf) {
		n, err := r.Read(buf[got:])
		got += n
		if err != nil {
			return got, err //nolint:wrapcheck // a net.Conn passes its errors through
		}
		if n == 0 {
			break
		}
	}
	return got, nil
}
