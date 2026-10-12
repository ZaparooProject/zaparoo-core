/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

// Package ccid is the reader driver for USB CCID contactless readers.
//
// It reaches a reader through the system's PC/SC service where there is one,
// and by talking CCID over USB itself where there is not. Either way it uses
// only the standard PC/SC storage card commands, which the reader's firmware
// carries out, so it needs no knowledge of any particular reader.
package ccid

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// DriverID is the driver's config and API name.
	DriverID = "ccid"

	pollInterval = 250 * time.Millisecond
	openTimeout  = 5 * time.Second
	ioTimeout    = 5 * time.Second
	writeTimeout = 30 * time.Second

	// removalMisses is how many polls in a row must find no tag before it is
	// reported removed, so one dropped poll is not a removal.
	removalMisses = 2
	// pollFailureLimit is how many polls in a row may fail before the reader
	// is treated as disconnected.
	pollFailureLimit = 3
)

var (
	errNotOpen        = errors.New("reader is not open")
	errNoTagPresent   = errors.New("no contactless tag on an untested reader")
	errNotContactless = errors.New("card is not a contactless tag")
	errReaderMissing  = errors.New("reader not found")
)

// Reader is one CCID reader, or a probe for one during auto-detect.
//
//nolint:govet // fields are grouped by the lock that guards them
type Reader struct {
	newBackends func() []backend
	registry    *registry
	// writeWait is how long a write waits for a tag to be presented.
	writeWait time.Duration

	// mu guards the connection fields.
	mu       syncutil.RWMutex
	backends []backend
	dev      device
	info     readerInfo
	cancel   context.CancelFunc
	done     chan struct{}
	open     bool

	// ioMu serialises use of the device between polling and writing, and
	// guards the tag state below.
	ioMu                 syncutil.Mutex
	uid                  []byte
	misses               int
	pollFailures         int
	present              bool
	suppressUntilRemoval bool
	diagnosed            bool

	// writeMu guards the write-in-progress fields.
	writeMu     syncutil.Mutex
	writeCancel context.CancelFunc
	writing     bool
}

var (
	_ readers.Reader       = (*Reader)(nil)
	_ readers.TargetWriter = (*Reader)(nil)
)

// NewReader returns the driver for the current platform.
func NewReader(_ *config.Instance) *Reader {
	return &Reader{
		newBackends: platformBackends,
		registry:    defaultRegistry,
		writeWait:   writeTimeout,
	}
}

func (*Reader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                DriverID,
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
		Description:       "USB CCID NFC readers",
	}
}

// IDs includes the names of the PC/SC ACR122 driver this one replaced, so
// connection entries written for it keep working.
func (*Reader) IDs() []string {
	return []string{DriverID, "acr122pcsc", "acr122_pcsc"}
}

func (r *Reader) backendList() []backend {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.backends == nil {
		r.backends = r.newBackends()
	}
	return r.backends
}

// listing is what the backends can currently see.
type listing struct {
	// paths is every reader found, usable or not.
	paths  map[string]bool
	owners []backend
	infos  []readerInfo
}

// listAll returns every usable reader with the backend that reaches it.
//
// A backend that cannot list is reported once and skipped: no PC/SC service
// is the normal state of a system this driver talks USB on. A reader that was
// found and cannot be used is reported once too, so one that is not picked up
// leaves a trace. The exception is a reader another program holds while every
// backend is working: that is the PC/SC service doing its job.
func (r *Reader) listAll(ctx context.Context) listing {
	found := listing{paths: make(map[string]bool)}
	failed := false
	var unusable []readerInfo
	var unusableOwners []backend

	for _, b := range r.backendList() {
		listed, err := b.List(ctx)
		if err != nil {
			failed = true
			if r.registry.firstTime("list-error:" + b.Name() + ":" + err.Error()) {
				log.Info().Err(err).Str("backend", b.Name()).Msg("ccid: backend not available")
			}
			continue
		}
		for i := range listed {
			found.paths[listed[i].Path] = true
			if listed[i].Unusable != "" {
				unusable = append(unusable, listed[i])
				unusableOwners = append(unusableOwners, b)
				continue
			}
			found.owners = append(found.owners, b)
			found.infos = append(found.infos, listed[i])
		}
	}

	for i := range unusable {
		if unusable[i].Busy && (!failed || r.registry.isHeld(unusable[i].Path)) {
			continue
		}
		r.logUnusable(unusableOwners[i], &unusable[i])
	}
	return found
}

func (r *Reader) logUnusable(b backend, info *readerInfo) {
	if !r.registry.firstTime(logKey("unusable", info.Path)) {
		return
	}
	event := log.Info().
		Str("path", info.Path).
		Str("name", info.Name).
		Str("backend", b.Name()).
		Str("reason", info.Unusable)
	for key, value := range info.Details {
		event = event.Str(key, value)
	}
	event.Msg("ccid: found a smart card reader that cannot be used")
}

func (r *Reader) logFirstSight(b backend, info *readerInfo) {
	if !r.registry.firstTime(logKey("seen", info.Path)) {
		return
	}
	event := log.Info().
		Str("path", info.Path).
		Str("name", info.Name).
		Str("backend", b.Name()).
		Bool("known", info.Known)
	for key, value := range info.Details {
		event = event.Str(key, value)
	}
	event.Msg("ccid: found smart card reader")
}

func (r *Reader) ignore(info *readerInfo, reason string, atr []byte) {
	r.registry.ignore(info.Path, reason)
	if !r.registry.firstTime(logKey("ignored", info.Path)) {
		return
	}
	log.Info().
		Str("path", info.Path).
		Str("name", info.Name).
		Str("reason", reason).
		Hex("atr", atr).
		Msg("ccid: ignoring reader")
}

// Detect returns a reader to attach, or an empty string.
//
// A known contactless reader is returned as soon as it is plugged in. Any
// other smart card reader is returned only while it holds a card, and Open
// then decides from the card whether it is an NFC reader at all. A laptop's
// card slot or a security key is never attached.
func (r *Reader) Detect(connected []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()

	found := r.listAll(ctx)
	candidate := ""

	for i := range found.infos {
		info := &found.infos[i]
		r.logFirstSight(found.owners[i], info)

		if candidate != "" || r.registry.isIgnored(info.Path) {
			continue
		}
		if helpers.Contains(connected, DriverID+":"+info.Path) {
			continue
		}
		if r.wantsAttach(ctx, found.owners[i], info) {
			candidate = DriverID + ":" + info.Path
		}
	}

	r.registry.prune(found.paths)
	return candidate
}

func (r *Reader) wantsAttach(ctx context.Context, b backend, info *readerInfo) bool {
	if info.Known || r.registry.hasSeen(info.Path) {
		return true
	}
	if info.ATR != nil {
		if isContactlessATR(info.ATR) {
			return true
		}
		r.ignore(info, "card is not a contactless tag", info.ATR)
		return false
	}
	present, err := b.CardPresent(ctx, info)
	if err != nil {
		log.Trace().Err(err).Str("path", info.Path).Msg("ccid: could not check reader for a card")
		return false
	}
	return present
}

func (r *Reader) find(ctx context.Context, path string) (backend, *readerInfo, error) {
	found := r.listAll(ctx)
	owners, infos := found.owners, found.infos
	fallback := -1
	for i := range infos {
		if path != "" {
			if infos[i].Path == path {
				return owners[i], &infos[i], nil
			}
			continue
		}
		if infos[i].Known {
			return owners[i], &infos[i], nil
		}
		if fallback < 0 {
			fallback = i
		}
	}
	if path == "" && fallback >= 0 {
		return owners[fallback], &infos[fallback], nil
	}
	return nil, nil, fmt.Errorf("%w: %q", errReaderMissing, path)
}

func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan, opts readers.OpenOpts) error {
	if !readers.MatchesDriverID(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()

	b, info, err := r.find(ctx, device.Path)
	if err != nil {
		return err
	}
	r.logFirstSight(b, info)

	dev, err := b.Open(ctx, info)
	if err != nil {
		return fmt.Errorf("open %s: %w", info.Path, err)
	}

	if opts.Probing && !info.Known && !r.registry.hasSeen(info.Path) {
		if err := r.vet(ctx, dev, info); err != nil {
			_ = dev.Close()
			return err
		}
	}

	pollCtx, pollCancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	r.mu.Lock()
	r.dev = dev
	r.info = *info
	r.cancel = pollCancel
	r.done = done
	r.open = true
	r.mu.Unlock()
	r.registry.hold(info.Path)

	go r.run(pollCtx, iq, done)
	return nil
}

// vet decides whether an untested reader that holds a card is an NFC reader.
// It is the one place a card is activated before the driver knows what it is,
// because without the PC/SC service the answer to reset cannot be seen
// otherwise. It happens once: the reader is then either attached or ignored.
func (r *Reader) vet(ctx context.Context, dev device, info *readerInfo) error {
	state, err := dev.Poll(ctx)
	if err != nil {
		return fmt.Errorf("poll %s: %w", info.Path, err)
	}
	switch {
	case isContactlessATR(state.ATR):
		r.registry.markSeen(info.Path)
		return nil
	case state.ATR != nil:
		r.ignore(info, "card is not a contactless tag", state.ATR)
		return errNotContactless
	}
	if r.registry.powerFailed(info.Path) {
		r.ignore(info, "slot reports a card that cannot be activated", nil)
	}
	return errNoTagPresent
}

func (r *Reader) run(ctx context.Context, iq chan<- readers.Scan, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if r.tick(ctx, iq) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Reader) send(ctx context.Context, iq chan<- readers.Scan, scan *readers.Scan) {
	scan.Source = tokens.SourceReader
	scan.ReaderID = r.ReaderID()
	select {
	case iq <- *scan:
	case <-ctx.Done():
	}
}

func (r *Reader) isWriting() bool {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.writing
}

// tick polls the reader once and reports whether polling should stop.
func (r *Reader) tick(ctx context.Context, iq chan<- readers.Scan) bool {
	if ctx.Err() != nil {
		return true
	}
	// A write owns the tag from the moment it is requested: the tag the user
	// presents for it must not be scanned and launched first.
	if r.isWriting() {
		return false
	}

	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	dev := r.device()
	if dev == nil {
		return true
	}

	ioCtx, cancel := context.WithTimeout(ctx, ioTimeout)
	defer cancel()

	state, err := dev.Poll(ioCtx)
	if err != nil {
		return r.pollFailed(ctx, iq, err)
	}
	r.pollFailures = 0

	if !state.Present || !isContactlessATR(state.ATR) {
		r.missed(ctx, iq)
		return false
	}

	uid, err := readUID(ioCtx, dev)
	if err != nil {
		// The tag left between the poll and the command, or the reader has
		// not finished identifying it. Either way, start again next time.
		dev.Reactivate(ioCtx)
		r.missed(ctx, iq)
		return false
	}
	r.misses = 0

	if r.present && bytes.Equal(uid, r.uid) {
		return false
	}

	r.present = true
	r.uid = uid
	if r.suppressUntilRemoval {
		// A different tag replaced the written one without a gap.
		r.suppressUntilRemoval = false
	}

	token := r.readToken(ioCtx, dev, state.ATR, uid)
	r.send(ctx, iq, &readers.Scan{Token: token})
	return false
}

func (r *Reader) device() device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.open {
		return nil
	}
	return r.dev
}

func (r *Reader) pollFailed(ctx context.Context, iq chan<- readers.Scan, err error) bool {
	r.pollFailures++
	if !errors.Is(err, errReaderGone) && r.pollFailures < pollFailureLimit {
		log.Debug().Err(err).Str("path", r.Path()).Msg("ccid: poll failed")
		return false
	}

	log.Warn().Err(err).Str("path", r.Path()).Msg("ccid: reader disconnected")
	r.mu.Lock()
	r.open = false
	r.mu.Unlock()
	// A disconnect is not a tag removal: it must not end running media.
	r.present = false
	r.uid = nil
	r.send(ctx, iq, &readers.Scan{ReaderError: true})
	return true
}

func (r *Reader) missed(ctx context.Context, iq chan<- readers.Scan) {
	if !r.present {
		return
	}
	r.misses++
	if r.misses < removalMisses {
		return
	}
	written := r.suppressUntilRemoval
	r.present = false
	r.uid = nil
	r.misses = 0
	r.suppressUntilRemoval = false
	log.Info().Str("path", r.Path()).Msg("ccid: tag removed")
	r.send(ctx, iq, &readers.Scan{WrittenTagRemoved: written})
}

func (r *Reader) readToken(ctx context.Context, dev device, atr, uid []byte) *tokens.Token {
	kind := tagKind(atr)
	res, err := nfctag.Read(ctx, &nfctag.Tag{IO: &apduTag{card: dev}, UID: uid, Kind: kind})
	if err != nil {
		log.Warn().Err(err).Str("path", r.Path()).Msg("ccid: could not read tag, reporting it by UID")
	}
	r.diagnose(atr, uid, res, err)

	log.Info().Msgf("ccid: detected %s tag: %s", res.Type, hex.EncodeToString(uid))
	return &tokens.Token{
		Type:     res.Type,
		UID:      hex.EncodeToString(uid),
		Text:     res.Text,
		Data:     hex.EncodeToString(res.Data),
		ScanTime: time.Now(),
		Source:   tokens.SourceReader,
		ReaderID: r.ReaderID(),
	}
}

// diagnose records how the first tag on an untested reader was understood.
// It holds lengths and the answer to reset, never the tag's contents.
func (r *Reader) diagnose(atr, uid []byte, res nfctag.Result, readErr error) {
	if r.diagnosed {
		return
	}
	r.diagnosed = true

	r.mu.RLock()
	info := r.info
	r.mu.RUnlock()
	if info.Known || !r.registry.firstTime(logKey("first-tag", info.Path)) {
		return
	}

	var event *zerolog.Event
	if readErr != nil {
		event = log.Warn().Err(readErr)
	} else {
		event = log.Info()
	}
	event.
		Str("path", info.Path).
		Str("name", info.Name).
		Hex("atr", atr).
		Str("type", res.Type).
		Int("uidLength", len(uid)).
		Int("textLength", len(res.Text)).
		Int("dataLength", len(res.Data)).
		Msg("ccid: first tag on an untested reader")
}

func (r *Reader) Close() error {
	r.CancelWrite()

	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	dev := r.dev
	backends := r.backends
	r.cancel = nil
	r.done = nil
	r.dev = nil
	r.backends = nil
	r.open = false
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}

	var errs []error
	if dev != nil {
		// Wait out any command still in flight before the device goes away.
		r.ioMu.Lock()
		err := dev.Close()
		r.ioMu.Unlock()
		r.registry.release(r.Path())
		if err != nil {
			errs = append(errs, fmt.Errorf("close reader: %w", err))
		}
	}
	for _, b := range backends {
		if err := b.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s backend: %w", b.Name(), err))
		}
	}
	return errors.Join(errs...)
}

func (r *Reader) Path() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.info.Path
}

func (r *Reader) Connected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.open
}

func (r *Reader) Info() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.info.Name
}

func (r *Reader) ReaderID() string {
	return readers.GenerateReaderID(DriverID, r.Path())
}

func (*Reader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityWrite, readers.CapabilityRemovable}
}

func (*Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}

func (r *Reader) Write(text string) (*tokens.Token, error) {
	return r.WriteTarget(context.Background(), text, readers.WriteOptions{})
}

func (r *Reader) CancelWrite() {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.writeCancel != nil {
		log.Debug().Msg("ccid: cancelling write")
		r.writeCancel()
	}
}

func (r *Reader) beginWrite(ctx context.Context) (context.Context, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.writing {
		return nil, errors.New("a write is already in progress")
	}
	writeCtx, cancel := context.WithTimeout(ctx, r.writeWait)
	r.writing = true
	r.writeCancel = cancel
	return writeCtx, nil
}

func (r *Reader) endWrite() {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.writeCancel != nil {
		r.writeCancel()
	}
	r.writeCancel = nil
	r.writing = false
}

// WriteTarget waits for a tag and writes text to it.
func (r *Reader) WriteTarget(ctx context.Context, text string, opts readers.WriteOptions) (*tokens.Token, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if text == "" {
		return nil, nfctag.ErrEmptyText
	}
	if r.device() == nil {
		return nil, errNotOpen
	}

	writeCtx, err := r.beginWrite(ctx)
	if err != nil {
		return nil, err
	}
	defer r.endWrite()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		token, done, writeErr := r.tryWrite(writeCtx, text, opts)
		if done {
			return token, writeErr
		}
		select {
		case <-writeCtx.Done():
			// A cancel reaches the caller as context.Canceled; running out of
			// time with no tag presented is the expected "no tag" outcome.
			if errors.Is(writeCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
				return nil, readers.ErrTagNotDetected
			}
			return nil, fmt.Errorf("write cancelled: %w", context.Canceled)
		case <-ticker.C:
		}
	}
}

// tryWrite writes to the tag in the field, if there is one. done is false
// when there was no tag to try.
func (r *Reader) tryWrite(
	ctx context.Context,
	text string,
	opts readers.WriteOptions,
) (token *tokens.Token, done bool, err error) {
	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	dev := r.device()
	if dev == nil {
		return nil, true, errNotOpen
	}

	ioCtx, cancel := context.WithTimeout(ctx, ioTimeout)
	defer cancel()

	state, err := dev.Poll(ioCtx)
	if err != nil {
		if errors.Is(err, errReaderGone) {
			return nil, true, fmt.Errorf("write: %w", err)
		}
		return nil, false, nil
	}
	if !state.Present || !isContactlessATR(state.ATR) {
		return nil, false, nil
	}
	uid, err := readUID(ioCtx, dev)
	if err != nil {
		dev.Reactivate(ioCtx)
		return nil, false, nil
	}

	uidHex := hex.EncodeToString(uid)
	if opts.ExcludeUID != "" && uidHex == opts.ExcludeUID {
		return nil, true, fmt.Errorf("refusing to write excluded tag: %s", uidHex)
	}
	if opts.TargetUID != "" && uidHex != opts.TargetUID {
		return nil, true, fmt.Errorf("refusing to write wrong tag: got %s want %s", uidHex, opts.TargetUID)
	}

	// Once a tag is being written the write runs to completion: a cancel
	// half way through would leave it with part of a message.
	tagCtx, tagCancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer tagCancel()
	res, err := nfctag.Write(tagCtx, &nfctag.Tag{IO: &apduTag{card: dev}, UID: uid, Kind: tagKind(state.ATR)}, text)
	if err != nil {
		return nil, true, fmt.Errorf("failed to write to tag: %w", err)
	}

	// The written tag is still on the reader. It stays quiet until it leaves,
	// so the write does not turn into a scan of what was just written.
	r.present = true
	r.uid = uid
	r.misses = 0
	r.suppressUntilRemoval = true

	log.Info().Str("path", r.Path()).Msg("ccid: wrote text to tag")
	return &tokens.Token{
		Type:     res.Type,
		UID:      uidHex,
		Text:     res.Text,
		Data:     hex.EncodeToString(res.Data),
		ScanTime: time.Now(),
		Source:   tokens.SourceReader,
		ReaderID: r.ReaderID(),
	}, true, nil
}
