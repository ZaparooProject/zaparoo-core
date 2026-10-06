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
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/apigatt"
	"github.com/rs/zerolog/log"
)

// Faults the client can inject, one per run.
const (
	faultPlaintext  = "plaintext"  // a plaintext method call
	faultWSLabel    = "wslabel"    // a first frame bound to the WebSocket transport
	faultWrongPIN   = "wrongpin"   // a pairing confirmation that does not verify
	faultNoAck      = "noack"      // stop acknowledging mid-transfer
	faultBadAck     = "badack"     // acknowledge chunks never sent
	faultMidMessage = "midmessage" // disconnect halfway through sending a message
)

type clientOptions struct {
	creds     string
	pin       string
	name      string
	fault     string
	requests  stringList
	link      linkOptions
	pipeline  int
	upload    int
	listen    time.Duration
	pingEvery time.Duration
	timeout   time.Duration
}

func runClient(ctx context.Context, args []string) error {
	var o clientOptions
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	fs.StringVar(&o.link.address, "addr", "", "Core's Bluetooth address; scans for the Zaparoo service when empty")
	fs.StringVar(&o.link.name, "match", "", "when scanning, only accept an advertised name containing this")
	fs.IntVar(&o.link.mtu, "mtu", 0, "ATT MTU to chunk for; read from BlueZ when 0")
	fs.DurationVar(&o.link.scanFor, "scan", 30*time.Second, "how long to scan before giving up")
	fs.BoolVar(&o.link.withResponse, "write-requests", false, "send every chunk as a write request")
	fs.StringVar(&o.creds, "creds", "bletest-creds.json", "where pairing credentials are kept")
	fs.StringVar(&o.pin, "pair", "", "pair first using this PIN and save the credentials")
	fs.StringVar(&o.name, "client-name", "bletest", "client name to pair under")
	fs.StringVar(&o.fault, "fault", "", "fault to inject: "+strings.Join([]string{
		faultPlaintext, faultWSLabel, faultWrongPIN, faultNoAck, faultBadAck, faultMidMessage,
	}, ", "))
	fs.Var(&o.requests, "req", `request to send, as "method" or "method {params}"; repeatable`)
	fs.IntVar(&o.pipeline, "pipeline", 0, "send this many version requests back to back before reading any reply")
	fs.IntVar(&o.upload, "upload", 0, "send a version request padded to this many bytes, to load the client-to-Core direction")
	fs.DurationVar(&o.listen, "listen", 0, "stay connected this long printing notifications")
	fs.DurationVar(&o.pingEvery, "ping-every", 30*time.Second, "heartbeat interval while listening")
	fs.DurationVar(&o.timeout, "timeout", 10*time.Minute, "longest wait for any one response")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	adapter, err := openAdapter(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = adapter.Close() }()
	central, err := adapter.Central()
	if err != nil {
		return fmt.Errorf("central role: %w", err)
	}
	l, err := dial(ctx, central, o.link)
	if err != nil {
		return err
	}
	defer l.close()

	return o.run(ctx, l)
}

func (o *clientOptions) run(ctx context.Context, l *link) error {
	switch o.fault {
	case faultPlaintext:
		req, err := rpcRequest(1, "version", nil)
		if err != nil {
			return err
		}
		if err := l.send(ctx, req); err != nil {
			return err
		}
		return expectDropped(ctx, l, "a plaintext method call")
	case faultMidMessage:
		// Only the first chunk of a message, then gone.
		err := l.chunker.Split(bytes.Repeat([]byte("x"), 4096), apigatt.DefaultMTU, func(chunk []byte) error {
			if writeErr := l.write(ctx, chunk); writeErr != nil {
				return writeErr
			}
			return errStopSending
		})
		if !errors.Is(err, errStopSending) {
			return fmt.Errorf("send partial message: %w", err)
		}
		log.Info().Msg("sent one chunk of a message and is now disconnecting")
		return nil
	case faultBadAck:
		if err := l.write(ctx, apigatt.EncodeAck(l.tag, 60000)); err != nil {
			return err
		}
		return expectDropped(ctx, l, "an acknowledgement of chunks never sent")
	}

	if o.pin != "" {
		creds, err := pair(ctx, l, o.pin, o.name, o.fault == faultWrongPIN)
		if o.fault == faultWrongPIN {
			if err == nil {
				return errors.New("a corrupted confirmation was accepted")
			}
			log.Info().Err(err).Msg("PASS: core refused the wrong confirmation")
			return nil
		}
		if err != nil {
			return err
		}
		if err := creds.save(o.creds); err != nil {
			return err
		}
		log.Info().Str("file", o.creds).Str("deviceId", creds.DeviceID).Msg("paired")
	}

	creds, err := loadCredentials(o.creds)
	if err != nil {
		return err
	}
	if creds.DeviceID != l.info.DeviceID {
		return fmt.Errorf("credentials are for device %s, connected to %s", creds.DeviceID, l.info.DeviceID)
	}
	label := transportLabel
	if o.fault == faultWSLabel {
		label = "ws"
	}
	s, err := newSession(l, creds, label)
	if err != nil {
		return err
	}
	c := &caller{s: s, timeout: o.timeout}

	// The first frame authenticates the session.
	if _, err := c.call(ctx, "version", nil); err != nil {
		if o.fault == faultWSLabel {
			log.Info().Err(err).Msg("PASS: core refused a frame bound to the websocket transport")
			return nil
		}
		return err
	}
	if o.fault == faultWSLabel {
		return errors.New("a frame bound to the websocket transport was accepted")
	}

	for _, spec := range o.requests {
		method, params, err := parseRequest(spec)
		if err != nil {
			return err
		}
		if o.fault == faultNoAck {
			l.noAck.Store(true)
			if _, err := c.call(ctx, method, params); err == nil {
				return errors.New("a response arrived in full without any acknowledgement")
			}
			log.Info().Int64("chunksReceived", l.chunksIn.Load()).Int("window", l.info.Window).
				Msg("PASS: core stopped sending and dropped the client that stopped acknowledging")
			return nil
		}
		if _, err := c.call(ctx, method, params); err != nil {
			log.Error().Err(err).Str("method", method).Msg("request failed")
		}
	}

	if o.upload > 0 {
		pad := map[string]string{"pad": strings.Repeat("x", o.upload)}
		started := time.Now()
		if _, err := c.call(ctx, "version", pad); err != nil {
			return err
		}
		took := time.Since(started)
		log.Info().Int("bytes", o.upload).Dur("took", took).
			Float64("kBps", float64(o.upload)/1024/took.Seconds()).Msg("upload answered")
	}

	if o.pipeline > 0 {
		if err := c.pipeline(ctx, o.pipeline); err != nil {
			return err
		}
	}
	if o.listen > 0 {
		return c.listen(ctx, o.listen, o.pingEvery)
	}
	return nil
}

var errStopSending = errors.New("stop sending")

// expectDropped passes when Core ends the connection.
func expectDropped(ctx context.Context, l *link, what string) error {
	select {
	case <-l.dev.Disconnected():
		log.Info().Msgf("PASS: core dropped the connection after %s", what)
		return nil
	case msg := <-l.msgs:
		return fmt.Errorf("core answered %s with %q", what, truncate(msg))
	case <-time.After(45 * time.Second):
		return fmt.Errorf("core kept the connection open after %s", what)
	case <-ctx.Done():
		return fmt.Errorf("interrupted: %w", ctx.Err())
	}
}

// parseRequest splits "method {params}".
func parseRequest(spec string) (method string, params json.RawMessage, err error) {
	method, rest, _ := strings.Cut(strings.TrimSpace(spec), " ")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return method, nil, nil
	}
	if !json.Valid([]byte(rest)) {
		return "", nil, fmt.Errorf("params of %q are not valid JSON", method)
	}
	return method, json.RawMessage(rest), nil
}

// caller issues requests on a session and matches the replies.
type caller struct {
	s       *session
	timeout time.Duration
	nextID  int
}

// next waits for one message from Core and decodes it.
func (c *caller) next(ctx context.Context) (rpcMessage, int, error) {
	raw, err := c.s.recv(ctx)
	if err != nil {
		return rpcMessage{}, 0, err
	}
	if bytes.Equal(raw, []byte("pong")) {
		return rpcMessage{Method: "pong"}, len(raw), nil
	}
	var msg rpcMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return rpcMessage{}, 0, fmt.Errorf("decode message %q: %w", truncate(raw), err)
	}
	return msg, len(raw), nil
}

func logNotification(msg *rpcMessage, size int) {
	log.Info().Str("method", msg.Method).Int("bytes", size).
		Str("params", truncate(msg.Params)).Msg("notification")
}

// call sends one request and waits for its response, reporting how long it
// took and how fast the link carried it.
func (c *caller) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	req, err := rpcRequest(id, method, params)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	started := time.Now()
	chunksBefore := c.s.l.chunksIn.Load()
	if err := c.s.send(ctx, req); err != nil {
		return nil, err
	}
	for {
		msg, size, err := c.next(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		if string(msg.ID) != strconv.Itoa(id) {
			logNotification(&msg, size)
			continue
		}
		took := time.Since(started)
		event := log.Info().Str("method", method).Int("bytes", size).
			Int64("chunks", c.s.l.chunksIn.Load()-chunksBefore).Dur("took", took)
		if took > 0 {
			event = event.Float64("kBps", float64(size)/1024/took.Seconds())
		}
		if msg.Error != nil {
			event.Msg("error response")
			return nil, msg.Error
		}
		sum := sha256.Sum256(msg.Result)
		event.Str("sha256", hex.EncodeToString(sum[:8])).Str("result", truncate(msg.Result)).Msg("response")
		return msg.Result, nil
	}
}

// pipeline sends n requests back to back, then checks every one is
// answered. Core must put them back in order however BlueZ delivers them.
func (c *caller) pipeline(ctx context.Context, n int) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	want := make(map[string]bool, n)
	started := time.Now()
	for range n {
		c.nextID++
		want[strconv.Itoa(c.nextID)] = true
		req, err := rpcRequest(c.nextID, "version", nil)
		if err != nil {
			return err
		}
		if err := c.s.send(ctx, req); err != nil {
			return err
		}
	}
	sent := time.Since(started)
	for len(want) > 0 {
		msg, size, err := c.next(ctx)
		if err != nil {
			return fmt.Errorf("pipeline: %d of %d unanswered: %w", len(want), n, err)
		}
		if !want[string(msg.ID)] {
			logNotification(&msg, size)
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("pipeline: request %s: %w", msg.ID, msg.Error)
		}
		delete(want, string(msg.ID))
	}
	log.Info().Int("requests", n).Dur("sendTook", sent).Dur("total", time.Since(started)).
		Msg("PASS: every pipelined request was answered")
	return nil
}

// listen prints notifications for d, sending the heartbeat as it goes.
func (c *caller) listen(ctx context.Context, d, pingEvery time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	nextPing := time.Now().Add(pingEvery)
	for {
		recvCtx, recvCancel := context.WithDeadline(ctx, nextPing)
		msg, size, err := c.next(recvCtx)
		recvCancel()
		switch {
		case err == nil && msg.Method == "pong":
			log.Info().Msg("pong")
		case err == nil:
			logNotification(&msg, size)
		case ctx.Err() != nil:
			log.Info().Dur("listened", d).Msg("still connected at the end of the listen period")
			return nil
		case errors.Is(err, context.DeadlineExceeded):
			if sendErr := c.s.send(ctx, []byte("ping")); sendErr != nil {
				return sendErr
			}
			nextPing = time.Now().Add(pingEvery)
		default:
			return err
		}
	}
}
