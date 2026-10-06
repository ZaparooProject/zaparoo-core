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
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/apigatt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/rs/zerolog/log"
)

// link is one connection to Core's GATT service: it finds the device,
// chunks what it sends, reassembles what it receives and acknowledges it.
type link struct {
	dev    bluez.Device
	rx     bluez.RemoteCharacteristic
	msgs   chan []byte
	cancel context.CancelFunc
	info   apigatt.Info
	mtu    int
	// bytesIn counts reassembled bytes and chunksIn data chunks received,
	// for throughput figures.
	bytesIn  atomic.Int64
	chunksIn atomic.Int64
	chunker  apigatt.Chunker
	// noAck stops the client acknowledging, to prove Core stops sending.
	noAck atomic.Bool
	tag   uint16
	// withResponse sends every chunk as a write request.
	withResponse bool
}

type linkOptions struct {
	address      string
	name         string
	mtu          int
	scanFor      time.Duration
	withResponse bool
}

// dial scans for Core, connects and reads the Info characteristic.
func dial(ctx context.Context, central bluez.Central, opts linkOptions) (*link, error) {
	address := opts.address
	if address == "" {
		found, err := discover(ctx, central, opts)
		if err != nil {
			return nil, err
		}
		address = found
	}

	dev, err := central.Find(ctx, address, []string{apigatt.ServiceUUID})
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", address, err)
	}
	started := time.Now()
	if err := dev.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect %s: %w", address, err)
	}
	log.Info().Str("address", address).Dur("took", time.Since(started)).Msg("connected")

	l := &link{dev: dev, msgs: make(chan []byte, 256), withResponse: opts.withResponse}
	if err := l.attach(ctx, opts.mtu); err != nil {
		l.close()
		return nil, err
	}
	return l, nil
}

// discover scans for a device advertising the Zaparoo service.
func discover(ctx context.Context, central bluez.Central, opts linkOptions) (string, error) {
	scanCtx, cancel := context.WithTimeout(ctx, opts.scanFor)
	defer cancel()
	started := time.Now()
	results, err := central.Scan(scanCtx, bluez.ScanFilter{ServiceUUIDs: []string{apigatt.ServiceUUID}})
	if err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}
	for r := range results {
		log.Info().Str("address", r.Address).Str("name", r.Name).Int16("rssi", r.RSSI).
			Dur("after", time.Since(started)).Msg("found zaparoo service")
		if opts.name == "" || strings.Contains(strings.ToLower(r.Name), strings.ToLower(opts.name)) {
			return r.Address, nil
		}
	}
	return "", errors.New("no device advertising the zaparoo service was found")
}

// attach resolves the characteristics, reads Info and starts receiving.
func (l *link) attach(ctx context.Context, mtuOverride int) error {
	rx, err := l.dev.Characteristic(apigatt.ServiceUUID, apigatt.RXCharUUID)
	if err != nil {
		return fmt.Errorf("rx characteristic: %w", err)
	}
	tx, err := l.dev.Characteristic(apigatt.ServiceUUID, apigatt.TXCharUUID)
	if err != nil {
		return fmt.Errorf("tx characteristic: %w", err)
	}
	infoChar, err := l.dev.Characteristic(apigatt.ServiceUUID, apigatt.InfoCharUUID)
	if err != nil {
		return fmt.Errorf("info characteristic: %w", err)
	}
	raw, err := infoChar.Read(ctx)
	if err != nil {
		return fmt.Errorf("read info: %w", err)
	}
	if err = json.Unmarshal(raw, &l.info); err != nil {
		return fmt.Errorf("decode info %q: %w", raw, err)
	}

	l.mtu = mtuOverride
	if l.mtu == 0 {
		if l.mtu, err = tx.MTU(ctx); err != nil {
			return fmt.Errorf("read mtu: %w", err)
		}
	}
	if l.mtu == 0 {
		l.mtu = apigatt.DefaultMTU
		log.Warn().Msg("bluetoothd does not report the MTU; using 23. Pass -mtu to override")
	}

	var tag [2]byte
	if _, err = rand.Read(tag[:]); err != nil {
		return fmt.Errorf("pick session tag: %w", err)
	}
	l.tag = binary.BigEndian.Uint16(tag[:]) | 1
	l.chunker.Tag = l.tag
	l.rx = rx

	recvCtx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	chunks, err := tx.Subscribe(recvCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("subscribe to tx: %w", err)
	}
	go l.receive(recvCtx, chunks)

	log.Info().Interface("info", l.info).Int("mtu", l.mtu).Uint16("tag", l.tag).Msg("link ready")
	return nil
}

// receive reassembles Core's chunks and acknowledges them.
func (l *link) receive(ctx context.Context, chunks <-chan []byte) {
	defer close(l.msgs)
	reasm := apigatt.NewReassembler(0)
	var acked uint16
	for chunk := range chunks {
		h, payload, err := apigatt.ParseChunk(chunk)
		if err != nil {
			log.Error().Err(err).Hex("chunk", chunk).Msg("unparseable chunk from core")
			return
		}
		if h.Tag != l.tag {
			// Another client's traffic: every central sees every chunk.
			continue
		}
		msgs, err := reasm.Push(h, payload)
		if err != nil {
			log.Error().Err(err).Msg("reassembly failed")
			return
		}
		l.chunksIn.Add(1)
		for _, msg := range msgs {
			l.bytesIn.Add(int64(len(msg)))
			select {
			case l.msgs <- msg:
			case <-ctx.Done():
				return
			}
		}
		window := uint16(max(l.info.Window, 2)) //nolint:gosec // a small window size
		if next := reasm.Next(); next-acked >= window/2 && !l.noAck.Load() {
			if err := l.write(ctx, apigatt.EncodeAck(l.tag, next)); err != nil {
				log.Error().Err(err).Msg("acknowledgement failed")
				return
			}
			acked = next
		}
	}
}

func (l *link) write(ctx context.Context, chunk []byte) error {
	if err := l.rx.Write(ctx, chunk, l.withResponse); err != nil {
		return fmt.Errorf("write chunk: %w", err)
	}
	return nil
}

// send chunks one message onto RX.
func (l *link) send(ctx context.Context, msg []byte) error {
	//nolint:wrapcheck // write errors are wrapped where they happen
	return l.chunker.Split(msg, l.mtu, func(chunk []byte) error {
		return l.write(ctx, chunk)
	})
}

// recv waits for the next complete message.
func (l *link) recv(ctx context.Context) ([]byte, error) {
	select {
	case msg, ok := <-l.msgs:
		if !ok {
			return nil, errors.New("link closed")
		}
		return msg, nil
	case <-l.dev.Disconnected():
		return nil, errors.New("core disconnected")
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for a message: %w", ctx.Err())
	}
}

func (l *link) close() {
	if l.cancel != nil {
		l.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), bluez.DefaultCallTimeout)
	defer cancel()
	if err := l.dev.Disconnect(ctx); err != nil {
		log.Debug().Err(err).Msg("disconnect failed")
	}
}
