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
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/rs/zerolog/log"
)

// Nordic UART Service, as the simpleserial_ble driver expects it.
const (
	nusServiceUUID = "6e400001-b5a3-f393-e0a9-e50e24dcca9e"
	nusRXUUID      = "6e400002-b5a3-f393-e0a9-e50e24dcca9e"
	nusTXUUID      = "6e400003-b5a3-f393-e0a9-e50e24dcca9e"

	// scanRepeat is how often a present token is repeated; the driver
	// treats one second of silence as removal.
	scanRepeat = 250 * time.Millisecond
)

// readerStep is one stretch of the script: a token on the reader, or none.
type readerStep struct {
	uid string
	d   time.Duration
}

// parseScript reads "uid:duration,-:duration,...", where "-" is no token.
func parseScript(script string) ([]readerStep, error) {
	var steps []readerStep
	for _, part := range strings.Split(script, ",") {
		uid, dur, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return nil, fmt.Errorf("script step %q is not uid:duration", part)
		}
		d, err := time.ParseDuration(dur)
		if err != nil {
			return nil, fmt.Errorf("script step %q: %w", part, err)
		}
		if uid == "-" {
			uid = ""
		}
		steps = append(steps, readerStep{uid: uid, d: d})
	}
	return steps, nil
}

// readerHandler logs what the connected Core does.
type readerHandler struct{}

func (readerHandler) OnWrite(peer ble.Peer, _ string, value []byte, _ int) {
	log.Info().Str("peer", peer.Address).Bytes("value", value).Msg("core wrote to the reader")
}

func (readerHandler) OnRead(ble.Peer, string) ([]byte, error) { return nil, ble.ErrNotFound }

func (readerHandler) OnSubscribe(_ ble.Peer, _ string, subscribed bool) {
	log.Info().Bool("subscribed", subscribed).Msg("core subscription changed")
}

func (readerHandler) OnDisconnect(peer ble.Peer) {
	log.Info().Str("peer", peer.Address).Msg("core disconnected")
}

func runReader(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("reader", flag.ExitOnError)
	name := fs.String("name", "bletest-reader", "advertised local name")
	script := fs.String("script", "abc123:5s,-:3s", "what the reader sees, as uid:duration steps; - is no token")
	loop := fs.Bool("loop", true, "repeat the script until interrupted")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	steps, err := parseScript(*script)
	if err != nil {
		return err
	}

	adapter, err := openAdapter(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = adapter.Close() }()
	peripheral, err := adapter.Peripheral()
	if err != nil {
		return fmt.Errorf("peripheral role: %w", err)
	}

	app := ble.Application{Services: []ble.Service{{
		UUID:    nusServiceUUID,
		Primary: true,
		Characteristics: []ble.Characteristic{
			{UUID: nusRXUUID, Flags: []string{ble.FlagWrite, ble.FlagWriteWithoutResponse}},
			{UUID: nusTXUUID, Flags: []string{ble.FlagNotify}},
		},
	}}}
	adv := ble.Advertisement{LocalName: *name, ServiceUUIDs: []string{nusServiceUUID}}

	served := make(chan error, 1)
	go func() { served <- peripheral.Serve(ctx, app, adv, readerHandler{}) }()
	log.Info().Str("address", adapter.Address()).Str("name", *name).
		Msg("serving; set this address as the simpleserial_ble reader path")

	for {
		for _, step := range steps {
			if err := playStep(ctx, peripheral, step); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			select {
			case err := <-served:
				if err != nil {
					return fmt.Errorf("serve: %w", err)
				}
				return nil
			default:
			}
		}
		if !*loop {
			return nil
		}
	}
}

// playStep holds one token on the reader, or none, for the step's duration.
func playStep(ctx context.Context, peripheral ble.Peripheral, step readerStep) error {
	log.Info().Str("uid", step.uid).Dur("for", step.d).Msg("reader state")
	deadline := time.After(step.d)
	ticker := time.NewTicker(scanRepeat)
	defer ticker.Stop()
	for {
		if step.uid != "" {
			line := "SCAN\tuid=" + step.uid + "\n"
			err := peripheral.Notify(ble.Peer{}, nusTXUUID, []byte(line))
			if err != nil && !errors.Is(err, ble.ErrNotFound) {
				return fmt.Errorf("notify: %w", err)
			}
		}
		select {
		case <-deadline:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("interrupted: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
