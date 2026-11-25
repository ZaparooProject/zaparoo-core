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

// Package sim is a software reader for the desktop simulation harness. It
// holds virtual reader devices in a process-wide Hub; tests and the sim
// control socket tap, hold and remove virtual tags on them, unplug them, and
// complete writes. Scans go through the same channel real drivers use, so
// everything after the driver (dedup, hold mode, launch guard, hooks, pending
// writes) runs for real.
//
// It is registered only by the sim platform (pkg/platforms/sim).
package sim

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

// DriverID is the reader driver name used in [[readers.connect]] entries.
const DriverID = "sim"

// Errors returned by Hub operations.
var (
	ErrNoDevice     = errors.New("no such sim reader")
	ErrDeviceExists = errors.New("sim reader already exists")
	ErrNotOpen      = errors.New("sim reader is not connected to Core")
	ErrNoTag        = errors.New("no tag on sim reader")
	ErrNotRemovable = errors.New("sim reader is not removable")
	ErrNoWrite      = errors.New("sim reader has no write waiting")
)

// WriteFailure names a failure to inject into the next write on a device.
type WriteFailure string

const (
	// WriteFailNone clears any injected failure.
	WriteFailNone WriteFailure = ""
	// WriteFailError makes the write fail with a generic error.
	WriteFailError WriteFailure = "error"
	// WriteFailRemoved makes the write fail as if the tag left mid-write.
	WriteFailRemoved WriteFailure = "removed"
	// WriteFailNoTag makes the write fail at once as if no tag was presented.
	WriteFailNoTag WriteFailure = "no-tag"
	// WriteFailUnsupported makes the write fail with an unsupported tag type.
	WriteFailUnsupported WriteFailure = "unsupported"
)

// Tag is a virtual tag presented to a sim reader.
type Tag struct {
	UID  string `json:"uid,omitempty"`
	Text string `json:"text,omitempty"`
	Data string `json:"data,omitempty"`
	Type string `json:"type,omitempty"`
	// Properties are what the reader identified about the object, as an
	// optical drive reports a disc's kind, system and drive.
	Properties []readers.ScanProperty `json:"properties,omitempty"`
}

// DeviceOptions configures a virtual reader device.
type DeviceOptions struct {
	// WriteWait is how long a write waits for a tag. Zero uses
	// config.ReaderWriteWait.
	WriteWait time.Duration
	// NotRemovable drops the removable capability, like a barcode scanner.
	NotRemovable bool
	// NoWrite drops the write capability.
	NoWrite bool
	// NoAutoDetect hides the device from auto-detection, so only a
	// [[readers.connect]] entry opens it.
	NoAutoDetect bool
}

// WriteRecord is one write a device completed or failed.
type WriteRecord struct {
	Time  time.Time `json:"time"`
	Text  string    `json:"text"`
	UID   string    `json:"uid,omitempty"`
	Error string    `json:"error,omitempty"`
}

// DeviceStatus describes a device for listings.
type DeviceStatus struct {
	Present      *Tag          `json:"present,omitempty"`
	Name         string        `json:"name"`
	ReaderID     string        `json:"readerId"`
	PendingFault string        `json:"pendingFault,omitempty"`
	Writes       []WriteRecord `json:"writes,omitempty"`
	Plugged      bool          `json:"plugged"`
	Open         bool          `json:"open"`
	Writing      bool          `json:"writing"`
	Removable    bool          `json:"removable"`
	CanWrite     bool          `json:"canWrite"`
	AutoDetect   bool          `json:"autoDetect"`
}

type pendingWrite struct {
	tagCh chan Tag
	text  string
	opts  readers.WriteOptions
}

// Device is one virtual reader. Its state outlives the Reader instances
// Core opens on it, the way a USB reader outlives a driver reconnect.
type Device struct {
	present   *Tag
	attached  *Reader
	write     *pendingWrite
	holdTimer *time.Timer
	name      string
	fault     WriteFailure
	writes    []WriteRecord
	opts      DeviceOptions
	mu        syncutil.Mutex
	plugged   bool
	suppress  bool // a written tag is still on the reader
}

// Hub holds the virtual devices for a process.
type Hub struct {
	devices map[string]*Device
	mu      syncutil.Mutex
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{devices: map[string]*Device{}}
}

var defaultHub = NewHub()

// DefaultHub is the hub readers created with NewReader use.
func DefaultHub() *Hub {
	return defaultHub
}

// Add plugs in a new device. Core finds it through auto-detection unless
// NoAutoDetect is set, or through a [[readers.connect]] entry naming it.
func (h *Hub) Add(name string, opts DeviceOptions) (*Device, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, ":/\\") {
		return nil, fmt.Errorf("invalid sim reader name %q", name)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if d, ok := h.devices[name]; ok {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.plugged {
			return nil, fmt.Errorf("%w: %s", ErrDeviceExists, name)
		}
		d.plugged = true
		d.opts = opts
		return d, nil
	}
	d := &Device{name: name, opts: opts, plugged: true}
	h.devices[name] = d
	return d, nil
}

// Device returns a device by name.
func (h *Hub) Device(name string) (*Device, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.devices[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoDevice, name)
	}
	return d, nil
}

// Remove unplugs a device and forgets it.
func (h *Hub) Remove(name string) error {
	d, err := h.Device(name)
	if err != nil {
		return err
	}
	d.Unplug()
	h.mu.Lock()
	delete(h.devices, name)
	h.mu.Unlock()
	return nil
}

// List returns every device, sorted by name.
func (h *Hub) List() []DeviceStatus {
	h.mu.Lock()
	devices := make([]*Device, 0, len(h.devices))
	for _, d := range h.devices {
		devices = append(devices, d)
	}
	h.mu.Unlock()
	out := make([]DeviceStatus, 0, len(devices))
	for _, d := range devices {
		out = append(out, d.Status())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (h *Hub) detect(excluded []string) string {
	h.mu.Lock()
	names := make([]string, 0, len(h.devices))
	for name := range h.devices {
		names = append(names, name)
	}
	h.mu.Unlock()
	sort.Strings(names)
	for _, name := range names {
		d, err := h.Device(name)
		if err != nil {
			continue
		}
		d.mu.Lock()
		ok := d.plugged && d.attached == nil && !d.opts.NoAutoDetect
		d.mu.Unlock()
		if ok && !isExcluded(name, excluded) {
			return DriverID + ":" + name
		}
	}
	return ""
}

func isExcluded(name string, excluded []string) bool {
	for _, e := range excluded {
		if e == name {
			return true
		}
		if _, p, ok := strings.Cut(e, ":"); ok && p == name {
			return true
		}
	}
	return false
}

// Name returns the device name.
func (d *Device) Name() string { return d.name }

// Status describes the device.
func (d *Device) Status() DeviceStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := DeviceStatus{
		Name:         d.name,
		ReaderID:     readers.GenerateReaderID(DriverID, d.name),
		Plugged:      d.plugged,
		Open:         d.attached != nil,
		Writing:      d.write != nil,
		Removable:    !d.opts.NotRemovable,
		CanWrite:     !d.opts.NoWrite,
		AutoDetect:   !d.opts.NoAutoDetect,
		PendingFault: string(d.fault),
		Writes:       append([]WriteRecord(nil), d.writes...),
	}
	if d.present != nil {
		t := *d.present
		st.Present = &t
	}
	return st
}

// Tap presents a tag. A tag already on the reader is replaced. If a write is
// waiting, the tag is offered to the write instead of being scanned. A
// positive hold removes the tag again after that long.
func (d *Device) Tap(in *Tag, hold time.Duration) (Tag, error) {
	tag := normalizeTag(in)
	d.mu.Lock()
	if d.attached == nil {
		d.mu.Unlock()
		return tag, ErrNotOpen
	}
	if d.holdTimer != nil {
		d.holdTimer.Stop()
		d.holdTimer = nil
	}
	if w := d.write; w != nil {
		d.present = &tag
		select {
		case w.tagCh <- tag:
		default:
		}
		d.mu.Unlock()
		d.scheduleRemove(tag.UID, hold)
		return tag, nil
	}
	d.present = &tag
	d.suppress = false
	r := d.attached
	d.mu.Unlock()
	r.send(&readers.Scan{
		Source:     tokens.SourceReader,
		ReaderID:   r.ReaderID(),
		Token:      r.token(&tag),
		Properties: tag.Properties,
	})
	d.scheduleRemove(tag.UID, hold)
	return tag, nil
}

func (d *Device) scheduleRemove(uid string, hold time.Duration) {
	if hold <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.holdTimer = time.AfterFunc(hold, func() {
		d.mu.Lock()
		same := d.present != nil && d.present.UID == uid
		d.mu.Unlock()
		if same {
			if err := d.RemoveTag(); err != nil {
				log.Debug().Err(err).Str("reader", d.name).Msg("sim auto-remove")
			}
		}
	})
}

// RemoveTag takes the tag off the reader.
func (d *Device) RemoveTag() error {
	d.mu.Lock()
	if d.attached == nil {
		d.mu.Unlock()
		return ErrNotOpen
	}
	if d.opts.NotRemovable {
		d.mu.Unlock()
		return ErrNotRemovable
	}
	if d.present == nil {
		d.mu.Unlock()
		return ErrNoTag
	}
	d.present = nil
	written := d.suppress
	d.suppress = false
	r := d.attached
	d.mu.Unlock()
	r.send(&readers.Scan{
		Source:            tokens.SourceReader,
		ReaderID:          r.ReaderID(),
		WrittenTagRemoved: written,
	})
	return nil
}

// ReaderError reports a reader fault. The active token, if any, is dropped
// with ReaderError set, as a driver does when it loses the device.
func (d *Device) ReaderError() error {
	d.mu.Lock()
	r := d.attached
	d.present = nil
	d.suppress = false
	d.mu.Unlock()
	if r == nil {
		return ErrNotOpen
	}
	r.send(&readers.Scan{Source: tokens.SourceReader, ReaderID: r.ReaderID(), ReaderError: true})
	return nil
}

// ScanError sends a scan carrying an error, as a driver does for a bad read.
func (d *Device) ScanError(msg string) error {
	d.mu.Lock()
	r := d.attached
	d.mu.Unlock()
	if r == nil {
		return ErrNotOpen
	}
	if msg == "" {
		msg = "sim read error"
	}
	r.send(&readers.Scan{Source: tokens.SourceReader, ReaderID: r.ReaderID(), Error: errors.New(msg)})
	return nil
}

// Unplug disconnects the device. An open reader reports a reader error for a
// tag that was on it and then reports itself disconnected, so Core closes it.
func (d *Device) Unplug() {
	d.mu.Lock()
	d.plugged = false
	r := d.attached
	had := d.present != nil
	d.present = nil
	d.suppress = false
	if d.write != nil {
		close(d.write.tagCh)
		d.write = nil
	}
	d.mu.Unlock()
	if r != nil && had {
		r.send(&readers.Scan{Source: tokens.SourceReader, ReaderID: r.ReaderID(), ReaderError: true})
	}
}

// Plug reconnects an unplugged device.
func (d *Device) Plug() {
	d.mu.Lock()
	d.plugged = true
	d.mu.Unlock()
}

// FailNextWrite injects a failure into the next write.
func (d *Device) FailNextWrite(f WriteFailure) {
	d.mu.Lock()
	d.fault = f
	d.mu.Unlock()
}

// CompleteWrite presents a tag to a waiting write. An empty UID makes a new
// blank tag.
func (d *Device) CompleteWrite(uid string) (Tag, error) {
	d.mu.Lock()
	waiting := d.write != nil
	d.mu.Unlock()
	if !waiting {
		return Tag{}, ErrNoWrite
	}
	return d.Tap(&Tag{UID: uid}, 0)
}

func (d *Device) attach(r *Reader) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.plugged {
		return fmt.Errorf("sim reader %s is unplugged", d.name)
	}
	if d.attached != nil && d.attached != r {
		return fmt.Errorf("sim reader %s is already open", d.name)
	}
	d.attached = r
	return nil
}

func (d *Device) detach(r *Reader) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.attached == r {
		d.attached = nil
		d.present = nil
		d.suppress = false
	}
}

func (d *Device) isPlugged() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.plugged
}

func normalizeTag(in *Tag) Tag {
	t := *in
	if t.UID == "" {
		t.UID = randomUID()
	}
	t.UID = strings.ToLower(t.UID)
	if t.Type == "" {
		t.Type = tokens.TypeNTAG
	}
	if t.Data == "" && t.Text != "" {
		t.Data = hex.EncodeToString([]byte(t.Text))
	}
	return t
}

func randomUID() string {
	b := make([]byte, 7)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%014x", time.Now().UnixNano())
	}
	b[0] = 0x04 // NXP manufacturer byte, as on NTAG stickers
	return hex.EncodeToString(b)
}

// Reader is the Core reader driver for a sim device.
type Reader struct {
	hub    *Hub
	device *Device
	iq     chan<- readers.Scan
	done   chan struct{}
	cfg    *config.Instance
	path   string
	mu     syncutil.RWMutex
	open   bool
}

// NewReader returns a reader on the default hub.
func NewReader(cfg *config.Instance) *Reader {
	return NewReaderWithHub(cfg, DefaultHub())
}

// NewReaderWithHub returns a reader on hub.
func NewReaderWithHub(cfg *config.Instance, hub *Hub) *Reader {
	return &Reader{cfg: cfg, hub: hub}
}

// Metadata implements readers.Reader.
func (*Reader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                DriverID,
		Description:       "Simulated reader (sim platform only)",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	}
}

// IDs implements readers.Reader.
func (*Reader) IDs() []string { return []string{DriverID} }

// Open implements readers.Reader.
func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan, _ readers.OpenOpts) error {
	if !readers.MatchesDriverID(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}
	d, err := r.hub.Device(device.Path)
	if err != nil {
		return err
	}
	if err := d.attach(r); err != nil {
		return err
	}
	r.mu.Lock()
	r.device = d
	r.path = device.Path
	r.iq = iq
	r.done = make(chan struct{})
	r.open = true
	r.mu.Unlock()
	log.Info().Str("reader", device.Path).Msg("sim reader opened")
	return nil
}

// Close implements readers.Reader.
func (r *Reader) Close() error {
	r.mu.Lock()
	if !r.open {
		r.mu.Unlock()
		return nil
	}
	r.open = false
	close(r.done)
	d := r.device
	r.mu.Unlock()
	r.CancelWrite()
	if d != nil {
		d.detach(r)
	}
	return nil
}

// Detect implements readers.Reader. Plugged, unopened devices are found as
// if they had just been connected.
func (r *Reader) Detect(excluded []string) string {
	return r.hub.detect(excluded)
}

// Path implements readers.Reader.
func (r *Reader) Path() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.path
}

// Connected implements readers.Reader.
func (r *Reader) Connected() bool {
	r.mu.RLock()
	d, open := r.device, r.open
	r.mu.RUnlock()
	return open && d != nil && d.isPlugged()
}

// Info implements readers.Reader.
func (r *Reader) Info() string { return "Sim reader " + r.Path() }

// ReaderID implements readers.Reader.
func (r *Reader) ReaderID() string {
	return readers.GenerateReaderID(DriverID, r.Path())
}

// Capabilities implements readers.Reader.
func (r *Reader) Capabilities() []readers.Capability {
	r.mu.RLock()
	d := r.device
	r.mu.RUnlock()
	opts := DeviceOptions{}
	if d != nil {
		d.mu.Lock()
		opts = d.opts
		d.mu.Unlock()
	}
	var caps []readers.Capability
	if !opts.NoWrite {
		caps = append(caps, readers.CapabilityWrite)
	}
	if !opts.NotRemovable {
		caps = append(caps, readers.CapabilityRemovable)
	}
	return caps
}

// OnMediaChange implements readers.Reader.
func (*Reader) OnMediaChange(*models.ActiveMedia) error { return nil }

// Write implements readers.Reader.
func (r *Reader) Write(text string) (*tokens.Token, error) {
	return r.WriteTarget(context.Background(), text, readers.WriteOptions{})
}

// WriteTarget implements readers.TargetWriter with the PN532 driver's rules:
// a tag already on the reader is written at once, otherwise the next tag
// presented is; an excluded or wrong tag is refused; no tag within the wait
// is ErrTagNotDetected.
func (r *Reader) WriteTarget(ctx context.Context, text string, opts readers.WriteOptions) (*tokens.Token, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if text == "" {
		return nil, errors.New("text cannot be empty")
	}
	r.mu.RLock()
	d, open, done := r.device, r.open, r.done
	r.mu.RUnlock()
	if !open || d == nil {
		return nil, readers.ErrReaderDisconnected
	}

	d.mu.Lock()
	if d.opts.NoWrite {
		d.mu.Unlock()
		return nil, errors.New("writing not supported on this reader")
	}
	if d.write != nil {
		d.mu.Unlock()
		return nil, errors.New("a write is already in progress")
	}
	fault := d.fault
	d.fault = WriteFailNone
	wait := d.opts.WriteWait
	if wait <= 0 {
		wait = config.ReaderWriteWait
	}
	pw := &pendingWrite{text: text, opts: opts, tagCh: make(chan Tag, 1)}
	d.write = pw
	present := d.present
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		if d.write == pw {
			d.write = nil
		}
		d.mu.Unlock()
	}()

	if fault == WriteFailNoTag {
		return nil, d.record(text, "", readers.ErrTagNotDetected)
	}

	var tag Tag
	if present != nil {
		tag = *present
	} else {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case t, ok := <-pw.tagCh:
			if !ok {
				return nil, d.record(text, "", readers.ErrReaderDisconnected)
			}
			tag = t
		case <-timer.C:
			return nil, d.record(text, "", readers.ErrTagNotDetected)
		case <-ctx.Done():
			return nil, d.record(text, "", fmt.Errorf("write cancelled: %w", ctx.Err()))
		case <-done:
			return nil, d.record(text, "", readers.ErrReaderDisconnected)
		}
	}

	if opts.ExcludeUID != "" && strings.EqualFold(tag.UID, opts.ExcludeUID) {
		return nil, d.record(text, tag.UID, fmt.Errorf("refusing to write excluded tag: %s", tag.UID))
	}
	if opts.TargetUID != "" && !strings.EqualFold(tag.UID, opts.TargetUID) {
		return nil, d.record(text, tag.UID,
			fmt.Errorf("refusing to write wrong tag: got %s want %s", tag.UID, opts.TargetUID))
	}
	switch fault {
	case WriteFailError:
		return nil, d.record(text, tag.UID, errors.New("sim write failed"))
	case WriteFailRemoved:
		d.mu.Lock()
		d.present = nil
		d.mu.Unlock()
		return nil, d.record(text, tag.UID, readers.ErrTagRemoved)
	case WriteFailUnsupported:
		return nil, d.record(text, tag.UID, readers.ErrUnsupportedTagType)
	case WriteFailNone, WriteFailNoTag:
	}

	tag.Text = text
	tag.Data = hex.EncodeToString([]byte(text))
	d.mu.Lock()
	d.present = &tag
	d.suppress = true
	d.mu.Unlock()
	_ = d.record(text, tag.UID, nil)
	log.Info().Str("reader", d.name).Str("uid", tag.UID).Msg("sim reader wrote tag")
	return r.token(&tag), nil
}

// CancelWrite implements readers.Reader.
func (r *Reader) CancelWrite() {
	r.mu.RLock()
	d := r.device
	r.mu.RUnlock()
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.write != nil {
		close(d.write.tagCh)
		d.write = nil
	}
}

func (d *Device) record(text, uid string, err error) error {
	rec := WriteRecord{Time: time.Now().UTC(), Text: text, UID: uid}
	if err != nil {
		rec.Error = err.Error()
	}
	d.mu.Lock()
	d.writes = append(d.writes, rec)
	if len(d.writes) > 100 {
		d.writes = slices.Clone(d.writes[len(d.writes)-100:])
	}
	d.mu.Unlock()
	return err
}

func (r *Reader) token(t *Tag) *tokens.Token {
	return &tokens.Token{
		Type:     t.Type,
		UID:      t.UID,
		Text:     t.Text,
		Data:     t.Data,
		ScanTime: time.Now(),
		Source:   tokens.SourceReader,
		ReaderID: r.ReaderID(),
	}
}

func (r *Reader) send(scan *readers.Scan) {
	r.mu.RLock()
	iq, done, open := r.iq, r.done, r.open
	r.mu.RUnlock()
	if !open {
		return
	}
	select {
	case iq <- *scan:
	case <-done:
	}
}
