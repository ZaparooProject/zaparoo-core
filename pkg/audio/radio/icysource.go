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
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// icyReadSize is how much of a stream is read at a time.
const icyReadSize = 16 << 10

// errStaleProbe is a probe connection that failed before giving audio.
var errStaleProbe = errors.New("radio: probe connection went stale")

// icySource plays a direct Icecast or Shoutcast stream.
type icySource struct {
	probe *Probe
	url   string
	buf   []byte
	opts  Options
}

func (c *icySource) close() {
	c.probe.Close()
}

func (c *icySource) run(ctx context.Context, sk *sink, _ bool) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var (
		resp      *http.Response
		head      []byte
		reqCancel context.CancelFunc
		fromProbe bool
	)
	if c.probe != nil {
		resp, head, reqCancel = c.probe.take()
		c.probe.Close()
		c.probe = nil
		fromProbe = resp != nil
	}
	if resp == nil {
		hctx, hcancel := context.WithTimeout(ctx, c.opts.timing.open)
		var err error
		resp, reqCancel, err = get(hctx, c.opts.HTTPClient, c.url, true)
		hcancel()
		if err != nil {
			return err
		}
	}
	stop := context.AfterFunc(ctx, reqCancel)
	defer stop()
	defer reqCancel()
	defer func() { _ = resp.Body.Close() }()

	codec := codecNone
	if v := resp.Header.Get("Content-Type"); v != "" {
		mt, _, err := mime.ParseMediaType(v)
		if err != nil {
			mt = strings.TrimSpace(strings.Split(v, ";")[0])
		}
		codec = codecForType(strings.ToLower(mt))
	}
	if codec == codecNone {
		if head == nil {
			head = make([]byte, sniffBytes)
			n, _ := readAtLeast(resp.Body, head)
			head = head[:n]
		}
		codec = sniffCodec(head)
		if codec == codecNone {
			return fmt.Errorf("radio: %s no longer serves audio", c.url)
		}
	}
	if name := sanitizeText(decodeText([]byte(resp.Header.Get("icy-name")))); name != "" {
		sk.s.setName(name)
	}

	var body io.Reader = resp.Body
	if len(head) > 0 {
		body = io.MultiReader(bytes.NewReader(head), resp.Body)
	}
	body = newICYReader(body, parseMetaint(resp.Header.Get("icy-metaint")), func(t string) {
		if c.opts.OnMeta != nil {
			title, artist := splitTitle(t)
			c.opts.OnMeta(title, artist)
		}
	})
	idle := newIdleReader(body, c.opts.timing.idle, func() { cancel(errIdle) })
	defer idle.stop()

	log.Debug().Str("url", c.url).Int("codec", int(codec)).Bool("reused", fromProbe).Msg("radio: stream connected")
	if c.buf == nil {
		c.buf = make([]byte, icyReadSize)
	}
	got := 0
	for {
		n, err := idle.Read(c.buf)
		if n > 0 {
			got += n
			if werr := sk.write(ctx, codec, c.buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if cause := context.Cause(ctx); cause != nil {
				return cause //nolint:wrapcheck // the stream's own stop signal
			}
			if fromProbe && got == 0 {
				return errStaleProbe
			}
			if errors.Is(err, io.EOF) {
				return errors.New("radio: server closed the stream")
			}
			return fmt.Errorf("radio: read stream: %w", err)
		}
	}
}

// idleReader calls onIdle when a single Read waits longer than d.
type idleReader struct {
	r io.Reader
	t *time.Timer
	d time.Duration
}

func newIdleReader(r io.Reader, d time.Duration, onIdle func()) *idleReader {
	t := time.AfterFunc(d, onIdle)
	t.Stop()
	return &idleReader{r: r, t: t, d: d}
}

func (ir *idleReader) Read(p []byte) (int, error) {
	ir.t.Reset(ir.d)
	n, err := ir.r.Read(p)
	ir.t.Stop()
	return n, err //nolint:wrapcheck // the caller wraps
}

func (ir *idleReader) stop() {
	ir.t.Stop()
}
