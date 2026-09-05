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

// Package simpleserialble is the simple serial reader protocol carried over
// Bluetooth Low Energy. The reader is a peripheral exposing the Nordic UART
// Service; Core connects as a central, subscribes to its TX characteristic
// and reads the same newline-delimited SCAN lines a wired simple serial
// reader would send.
//
// Readers are connected by explicit configuration only:
//
//	[[readers.connect]]
//	driver = "simpleserial_ble"
//	path = "AA:BB:CC:DD:EE:FF"
//
// The service UUID is shared by thousands of unrelated devices, so scanning
// for it and connecting to whatever answers is not an option. Advertising a
// recognisable local name is the natural way to add detection later.
//
// Unlike a serial port, the device may be out of range or switched off for
// long stretches, so the driver owns the link: Open attaches to the adapter
// and returns at once, and a background loop finds, connects to and
// reconnects the device with a growing pause between attempts. Each attempt
// scans, and scanning shares the radio with the BLE API transport's
// advertising, so a reader that is missing makes the device a little
// harder to discover until it turns up.
package simpleserialble

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/simpleproto"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// DriverID is the reader driver identifier.
	DriverID = "simpleserial_ble"

	// Nordic UART Service: the de facto serial port over BLE.
	nusServiceUUID = "6e400001-b5a3-f393-e0a9-e50e24dcca9e"
	nusTXUUID      = "6e400003-b5a3-f393-e0a9-e50e24dcca9e"

	// removalTimeout matches the wired simple serial driver: a token with
	// no fresh line for this long is treated as removed.
	removalTimeout = 1 * time.Second
	removalPoll    = 250 * time.Millisecond

	// defaultFindTimeout bounds one scan for the device.
	defaultFindTimeout = 15 * time.Second

	// Pauses between attempts to reach a device that is not answering,
	// doubling from the first to the last.
	initialRetryPause = 1 * time.Second
	maxRetryPause     = 30 * time.Second
)

// Reader is a simple serial reader reached over BLE.
type Reader struct {
	cfg         *config.Instance
	open        func(ctx context.Context) (bluez.Adapter, error)
	clock       clockwork.Clock
	adapter     bluez.Adapter
	cancel      context.CancelFunc
	done        chan struct{}
	lastToken   *tokens.Token
	lastSeenAt  time.Time
	device      config.ReadersConnect
	address     string
	findTimeout time.Duration
	mu          syncutil.RWMutex
	attached    bool
	linked      bool
	removable   bool
}

// NewReader builds a reader over the real BlueZ layer. It never powers the
// adapter on: a reader that keeps retrying must not keep switching a radio
// back on that the user turned off.
func NewReader(cfg *config.Instance) *Reader {
	return newReaderWith(cfg, func(ctx context.Context) (bluez.Adapter, error) {
		return bluez.Open(ctx)
	}, clockwork.NewRealClock())
}

func newReaderWith(
	cfg *config.Instance,
	open func(ctx context.Context) (bluez.Adapter, error),
	clock clockwork.Clock,
) *Reader {
	return &Reader{
		cfg:         cfg,
		open:        open,
		clock:       clock,
		findTimeout: defaultFindTimeout,
		removable:   true,
	}
}

func (*Reader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                DriverID,
		DefaultEnabled:    true,
		DefaultAutoDetect: false,
		Description:       "Simple serial protocol over Bluetooth LE (Nordic UART Service)",
	}
}

func (*Reader) IDs() []string {
	return []string{DriverID}
}

// Open attaches to the Bluetooth adapter and starts looking for the device.
// It fails only for what cannot be retried by waiting: a bad address, no
// adapter, an adapter that cannot act as a central. Whether the device is
// in range is the background loop's business, so the reader manager, which
// calls Open on its own tick, is never held up by a device that is off.
func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan, _ readers.OpenOpts) error {
	if !readers.MatchesDriverID(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}
	address, err := bluez.NormalizeAddress(device.Path)
	if err != nil {
		return fmt.Errorf("reader path: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	adapter, err := r.open(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("open bluetooth adapter: %w", err)
	}
	central, err := adapter.Central()
	if err != nil {
		_ = adapter.Close()
		cancel()
		return fmt.Errorf("bluetooth central: %w", err)
	}

	done := make(chan struct{})
	r.mu.Lock()
	r.device = device
	r.address = address
	r.adapter = adapter
	r.cancel = cancel
	r.done = done
	r.attached = true
	r.mu.Unlock()

	log.Info().Str("address", address).Msg("bluetooth simple serial reader attached, looking for device")
	go r.run(ctx, central, done, iq)
	return nil
}

// run keeps the device connected for as long as the reader is attached.
// What it needs is passed in rather than read from the reader, so Close
// can clear the reader's fields without racing this goroutine.
func (r *Reader) run(ctx context.Context, central bluez.Central, done chan<- struct{}, iq chan<- readers.Scan) {
	defer close(done)

	pause := initialRetryPause
	reported := false
	for {
		dev, values, err := r.connect(ctx, central)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			event := log.Debug()
			if !reported {
				event = log.Info()
				reported = true
			}
			event.Err(err).Str("address", r.address).Dur("retryIn", pause).
				Msg("bluetooth simple serial reader not reachable, will retry")
			select {
			case <-ctx.Done():
				return
			case <-r.clock.After(pause):
			}
			pause = min(pause*2, maxRetryPause)
			continue
		}

		pause = initialRetryPause
		reported = false
		r.setLinked(true)
		log.Info().Str("address", r.address).Msg("bluetooth simple serial reader connected")

		r.readLoop(ctx, iq, dev, values)
		r.setLinked(false)
		if ctx.Err() != nil {
			r.disconnect(dev)
			return
		}
		r.linkLost(iq)
	}
}

// connect finds and connects the device and subscribes to its TX stream.
func (r *Reader) connect(ctx context.Context, central bluez.Central) (bluez.Device, <-chan []byte, error) {
	findCtx, findCancel := context.WithTimeout(ctx, r.findTimeout)
	defer findCancel()
	dev, err := central.Find(findCtx, r.address, []string{nusServiceUUID})
	if err != nil {
		return nil, nil, fmt.Errorf("find: %w", err)
	}
	if connectErr := dev.Connect(findCtx); connectErr != nil {
		return nil, nil, fmt.Errorf("connect: %w", connectErr)
	}

	tx, err := dev.Characteristic(nusServiceUUID, nusTXUUID)
	if err != nil {
		r.disconnect(dev)
		return nil, nil, fmt.Errorf("serial characteristic: %w", err)
	}
	values, err := tx.Subscribe(ctx)
	if err != nil {
		r.disconnect(dev)
		return nil, nil, fmt.Errorf("subscribe: %w", err)
	}
	return dev, values, nil
}

// disconnect drops the link, best effort.
func (*Reader) disconnect(dev bluez.Device) {
	ctx, cancel := context.WithTimeout(context.Background(), bluez.DefaultCallTimeout)
	defer cancel()
	if err := dev.Disconnect(ctx); err != nil {
		log.Debug().Err(err).Msg("bluetooth reader disconnect failed")
	}
}

func (r *Reader) setLinked(linked bool) {
	r.mu.Lock()
	r.linked = linked
	r.mu.Unlock()
}

// readLoop turns the notification stream into scans until the link drops
// or the reader is closed.
func (r *Reader) readLoop(ctx context.Context, iq chan<- readers.Scan, dev bluez.Device, values <-chan []byte) {
	splitter := simpleproto.NewLineSplitter(0)
	ticker := r.clock.NewTicker(removalPoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-dev.Disconnected():
			return
		case data, ok := <-values:
			if !ok {
				return
			}
			for _, line := range splitter.Feed(data) {
				r.handleLine(line, iq)
			}
		case <-ticker.Chan():
			r.checkRemoval(iq)
		}
	}
}

// handleLine emits a scan for a new token. Repeats of the current token
// only refresh its presence.
func (r *Reader) handleLine(line string, iq chan<- readers.Scan) {
	parsed, ok := simpleproto.ParseLine(line, r.ReaderID())
	if !ok {
		return
	}
	if parsed.Removable != nil {
		r.mu.Lock()
		r.removable = *parsed.Removable
		r.mu.Unlock()
	}

	if !helpers.TokensEqual(parsed.Token, r.lastToken) {
		iq <- readers.Scan{
			Source:   tokens.SourceReader,
			ReaderID: r.ReaderID(),
			Token:    parsed.Token,
		}
	}
	r.lastToken = parsed.Token
	r.lastSeenAt = r.clock.Now()
}

// checkRemoval reports the token gone once the reader stops repeating it.
func (r *Reader) checkRemoval(iq chan<- readers.Scan) {
	if r.lastToken == nil || r.clock.Since(r.lastSeenAt) <= removalTimeout {
		return
	}
	iq <- readers.Scan{
		Source:   tokens.SourceReader,
		ReaderID: r.ReaderID(),
		Token:    nil,
	}
	r.lastToken = nil
}

// linkLost reports an active token as a reader error, not a removal, so
// media keeps running while the device is reconnected.
func (r *Reader) linkLost(iq chan<- readers.Scan) {
	log.Warn().Str("address", r.address).Msg("bluetooth simple serial reader lost its link, reconnecting")
	if r.lastToken == nil {
		return
	}
	iq <- readers.Scan{
		Source:      tokens.SourceReader,
		ReaderID:    r.ReaderID(),
		Token:       nil,
		ReaderError: true,
	}
	r.lastToken = nil
}

// Close stops the background loop, drops the link and releases the adapter.
func (r *Reader) Close() error {
	r.mu.Lock()
	cancel, adapter, done := r.cancel, r.adapter, r.done
	r.cancel, r.adapter, r.done = nil, nil, nil
	r.attached = false
	r.linked = false
	r.mu.Unlock()

	if cancel != nil {
		cancel()
		<-done
	}
	if adapter != nil {
		if err := adapter.Close(); err != nil {
			return fmt.Errorf("close bluetooth adapter: %w", err)
		}
	}
	return nil
}

func (*Reader) Detect(_ []string) string {
	return ""
}

func (r *Reader) Path() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.address
}

func (r *Reader) ReaderID() string {
	return readers.GenerateReaderID(DriverID, r.Path())
}

// Connected reports whether the reader is attached: opened and not closed.
// The device itself may be out of range meanwhile; Info says which.
func (r *Reader) Connected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.attached
}

func (r *Reader) Info() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := "searching"
	if r.linked {
		state = "connected"
	}
	return "BLE NUS " + r.address + " (" + state + ")"
}

func (*Reader) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (*Reader) CancelWrite() {
	// no-op, writing not supported
}

func (r *Reader) Capabilities() []readers.Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.removable {
		return []readers.Capability{readers.CapabilityRemovable}
	}
	return []readers.Capability{}
}

func (*Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}
