//go:build linux && !android

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

package sim

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	simreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/sim"
)

// Client talks to a sim's control socket.
type Client struct {
	http *http.Client
	base string
}

// NewClient returns a client for the control socket at path.
func NewClient(socket string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{http: &http.Client{Transport: useragent.Transport(tr)}, base: "http://sim"}
}

// NewHandlerClient returns a client that calls a control handler directly,
// for tests that do not serve the socket.
func NewHandlerClient(h http.Handler) *Client {
	return &Client{http: &http.Client{Transport: useragent.Transport(handlerTransport{h})}, base: "http://sim"}
}

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	pr, pw := io.Pipe()
	rw := &pipeResponse{header: http.Header{}, pw: pw, ready: make(chan struct{})}
	go func() {
		t.h.ServeHTTP(rw, r)
		rw.start()
		_ = pw.Close()
	}()
	select {
	case <-rw.ready:
	case <-r.Context().Done():
		return nil, r.Context().Err() //nolint:wrapcheck // context error
	}
	return &http.Response{
		StatusCode: rw.status, Header: rw.header, Body: pr, Request: r,
	}, nil
}

type pipeResponse struct {
	header http.Header
	pw     *io.PipeWriter
	ready  chan struct{}
	status int
}

func (w *pipeResponse) Header() http.Header { return w.header }

func (w *pipeResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		close(w.ready)
	}
}

func (w *pipeResponse) start() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
}

func (w *pipeResponse) Write(b []byte) (int, error) {
	w.start()
	return w.pw.Write(b) //nolint:wrapcheck // pipe error
}

func (*pipeResponse) Flush() {}

// Call sends a raw request and returns the response body. A non-2xx status
// becomes an error carrying the server's message.
func (c *Client) Call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sim control %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e ErrorResponse
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("sim control %s %s: %s", method, path, e.Error)
		}
		return nil, fmt.Errorf("sim control %s %s: status %d: %s", method, path, resp.StatusCode,
			strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}
	data, err := c.Call(ctx, method, path, body)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func readerPath(name, action string) string {
	return "/readers/" + url.PathEscape(name) + "/" + action
}

// Status returns the sim's state.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var st Status
	err := c.do(ctx, http.MethodGet, "/status", nil, &st)
	return st, err
}

// Readers lists the sim reader devices.
func (c *Client) Readers(ctx context.Context) ([]simreader.DeviceStatus, error) {
	var out []simreader.DeviceStatus
	err := c.do(ctx, http.MethodGet, "/readers", nil, &out)
	return out, err
}

// AddReader creates a sim reader device. Core connects it by auto-detection
// unless NoAutoDetect is set.
func (c *Client) AddReader(ctx context.Context, req AddReaderRequest) (simreader.DeviceStatus, error) {
	var out simreader.DeviceStatus
	err := c.do(ctx, http.MethodPost, "/readers", req, &out)
	return out, err
}

// RemoveReader deletes a device, disconnecting it from Core.
func (c *Client) RemoveReader(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/readers/"+url.PathEscape(name), nil, nil)
}

// Tap presents a tag. A zero hold leaves it on the reader.
func (c *Client) Tap(
	ctx context.Context, reader string, tag *simreader.Tag, hold time.Duration,
) (simreader.Tag, error) {
	var out simreader.Tag
	err := c.do(ctx, http.MethodPost, readerPath(reader, "tap"),
		TapRequest{Tag: *tag, HoldMS: int(hold / time.Millisecond)}, &out)
	return out, err
}

// Remove takes the tag off a reader.
func (c *Client) Remove(ctx context.Context, reader string) error {
	return c.do(ctx, http.MethodPost, readerPath(reader, "remove"), nil, nil)
}

// ReaderError sends a reader failure, as a device that stops answering does.
func (c *Client) ReaderError(ctx context.Context, reader string) error {
	return c.do(ctx, http.MethodPost, readerPath(reader, "error"), nil, nil)
}

// ScanError sends a scan that carries an error.
func (c *Client) ScanError(ctx context.Context, reader, message string) error {
	return c.do(ctx, http.MethodPost, readerPath(reader, "scan-error"), ScanErrorRequest{Message: message}, nil)
}

// Unplug disconnects a device without deleting it.
func (c *Client) Unplug(ctx context.Context, reader string) error {
	return c.do(ctx, http.MethodPost, readerPath(reader, "unplug"), nil, nil)
}

// Plug reconnects an unplugged device.
func (c *Client) Plug(ctx context.Context, reader string) error {
	return c.do(ctx, http.MethodPost, readerPath(reader, "plug"), nil, nil)
}

// CompleteWrite presents a tag to the write the reader is waiting on.
func (c *Client) CompleteWrite(ctx context.Context, reader, uid string) (simreader.Tag, error) {
	var out simreader.Tag
	err := c.do(ctx, http.MethodPost, readerPath(reader, "write-complete"), WriteCompleteRequest{UID: uid}, &out)
	return out, err
}

// FailNextWrite injects a failure into the reader's next write.
func (c *Client) FailNextWrite(ctx context.Context, reader string, mode simreader.WriteFailure) error {
	return c.do(ctx, http.MethodPost, readerPath(reader, "write-fail"), WriteFailRequest{Mode: mode}, nil)
}

// Journal returns events after since, optionally of the given kinds.
func (c *Client) Journal(ctx context.Context, since int64, kinds ...string) ([]Event, error) {
	var out []Event
	err := c.do(ctx, http.MethodGet, journalQuery("/journal", since, kinds, false, 0), nil, &out)
	return out, err
}

// Launches returns launch and process events after since.
func (c *Client) Launches(ctx context.Context, since int64) ([]Event, error) {
	var out []Event
	err := c.do(ctx, http.MethodGet, journalQuery("/launches", since, nil, false, 0), nil, &out)
	return out, err
}

// ErrStopFollowing ends Follow without an error when returned by its callback.
var ErrStopFollowing = errors.New("stop following")

// Follow streams events after since to fn until ctx ends or fn returns an
// error. ErrStopFollowing ends it cleanly.
func (c *Client) Follow(ctx context.Context, since int64, kinds []string, fn func(Event) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+journalQuery("/journal", since, kinds, true, 0), http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("follow journal: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return fmt.Errorf("decode event: %w", err)
		}
		if err := fn(e); err != nil {
			if errors.Is(err, ErrStopFollowing) {
				return nil
			}
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err() //nolint:wrapcheck // context error
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("follow journal: %w", err)
	}
	return nil
}

// WaitEvent waits for the first event after since that match accepts.
func (c *Client) WaitEvent(ctx context.Context, since int64, match func(Event) bool) (Event, error) {
	var found Event
	ok := false
	err := c.Follow(ctx, since, nil, func(e Event) error {
		if match(e) {
			found, ok = e, true
			return ErrStopFollowing
		}
		return nil
	})
	if err != nil {
		return Event{}, err
	}
	if !ok {
		return Event{}, errors.New("journal ended before a matching event")
	}
	return found, nil
}

func journalQuery(path string, since int64, kinds []string, follow bool, timeout time.Duration) string {
	q := url.Values{}
	if since > 0 {
		q.Set("since", strconv.FormatInt(since, 10))
	}
	if len(kinds) > 0 {
		q.Set("kind", strings.Join(kinds, ","))
	}
	if follow {
		q.Set("follow", "1")
	}
	if timeout > 0 {
		q.Set("timeout_ms", strconv.FormatInt(timeout.Milliseconds(), 10))
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// Faults lists active faults.
func (c *Client) Faults(ctx context.Context) ([]Fault, error) {
	var out []Fault
	err := c.do(ctx, http.MethodGet, "/faults", nil, &out)
	return out, err
}

// SetFault sets the fault for f.Launcher, or every launcher when empty.
func (c *Client) SetFault(ctx context.Context, f Fault) error {
	return c.do(ctx, http.MethodPost, "/faults", f, nil)
}

// ClearFault clears one launcher's fault, or all of them when id is empty.
func (c *Client) ClearFault(ctx context.Context, id string) error {
	path := "/faults"
	if id != "" {
		path += "?launcher=" + url.QueryEscape(id)
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// Stop asks the sim to shut down.
func (c *Client) Stop(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/stop", nil, nil)
}
