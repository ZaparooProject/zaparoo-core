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

// Package zapdisplay drives a Zaparoo display accessory over USB serial.
//
// The accessory is display-only: it never produces scans. On every media change
// it is sent the title, system name and cover art that Core already holds, so
// unlike a picture-pack display it works for any scraped media without shipping
// per-system artwork of its own.
package zapdisplay

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	driverID = "zapdisplay"

	// artworkMaxSize is the longest edge asked of Core's artwork pipeline. It is
	// the tier above the cover slot's 180px, so the image never needs upscaling
	// and the decode stays cheap on constrained devices.
	artworkMaxSize = 256

	// launchingDelay is how long a media change may run before the display puts
	// up a launching frame. Below this the work finishes fast enough that the
	// extra frame reads as a flash rather than as feedback.
	launchingDelay = 200 * time.Millisecond

	// espressifVID is the USB vendor ID the display enumerates under. The
	// firmware uses the ESP32-S3's native USB peripheral, which reports
	// Espressif's stock ID and no product string, so this is the only thing
	// that can be checked before opening a port.
	//
	// Probing writes a line to whatever is on the other end, and /dev/ttyACM*
	// is also where 3D printers, Arduino-class boards and CNC controllers
	// appear. Narrowing to this vendor keeps the handshake away from them.
	espressifVID = "303a"

	// refreshInterval re-sends the current scene so the firmware's uptime-based
	// anti-retention offset advances during a long session. It never re-uploads
	// the cover.
	refreshInterval = 2 * time.Minute
)

var (
	_ readers.Reader          = (*Reader)(nil)
	_ readers.ArtworkConsumer = (*Reader)(nil)
)

// vendorLookup reports the USB vendor and product IDs behind a serial device,
// or ok=false when they cannot be determined. It is a field on the reader, like
// the port factory and the clock, so a test can exercise the vendor filter
// without a real device attached.
type vendorLookup func(path string) (vid, pid string, ok bool)

// Reader is a Zaparoo display accessory.
type Reader struct {
	cfg          *config.Instance
	portFactory  testutils.SerialPortFactory
	clock        clockwork.Clock
	vendorIDs    vendorLookup
	artwork      readers.ArtworkSource
	session      *session
	workerCtx    context.Context
	workerCancel context.CancelFunc
	// desired is what the display should be showing; nil means idle. wake
	// carries a single coalescing signal, so any burst of media changes
	// collapses into one render of the newest state.
	desired    *models.ActiveMedia
	wake       chan struct{}
	appliedKey string
	cachedKey  string
	cachedID   string
	path       string
	info       string
	cachedPix  []byte
	// Dimensions the cached cover was encoded at. Artwork is not a fixed slot,
	// so re-selecting a cover the device already holds has to restate its size.
	cachedW        int
	cachedH        int
	mu             syncutil.RWMutex
	wg             sync.WaitGroup
	connected      atomic.Bool
	defaultEnabled bool
}

// NewReader allocates a driver instance. It performs no I/O and starts no
// goroutines: auto-detection builds the full reader list every second and
// discards the instances it does not use.
func NewReader(cfg *config.Instance) *Reader {
	return NewReaderWithDefaults(cfg, false)
}

// NewReaderWithDefaults allocates a driver instance whose enabled-by-default
// state is chosen by the platform. Platforms that ship the display as a
// first-party accessory enable it so it is plug and play; elsewhere it stays
// opt-in, because detection has to write to a serial port to identify it.
func NewReaderWithDefaults(cfg *config.Instance, defaultEnabled bool) *Reader {
	return &Reader{
		cfg:            cfg,
		portFactory:    testutils.DefaultSerialPortFactory,
		clock:          clockwork.NewRealClock(),
		vendorIDs:      helpers.SerialDeviceVIDPID,
		wake:           make(chan struct{}, 1),
		defaultEnabled: defaultEnabled,
	}
}

func (r *Reader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                driverID,
		DefaultEnabled:    r.defaultEnabled,
		DefaultAutoDetect: true,
		Description:       "Zaparoo display accessory",
	}
}

func (*Reader) IDs() []string {
	return []string{driverID}
}

func (*Reader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityDisplay}
}

// SetArtworkSource implements readers.ArtworkConsumer. Core calls it when the
// reader is registered; the driver renders title and system only until then.
func (r *Reader) SetArtworkSource(source readers.ArtworkSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.artwork = source
}

func (r *Reader) Path() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.path
}

func (r *Reader) Connected() bool {
	return r.connected.Load()
}

func (r *Reader) Info() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.connected.Load() {
		return driverID + " (disconnected)"
	}
	if r.info != "" {
		return r.info
	}
	return driverID + " (" + r.path + ")"
}

func (r *Reader) ReaderID() string {
	path := r.Path()
	stablePath := helpers.GetUSBTopologyPath(path)
	if stablePath == "" {
		stablePath = path
	}
	return readers.GenerateReaderID(driverID, stablePath)
}

func (*Reader) Write(string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on display-only reader")
}

func (*Reader) CancelWrite() {
	// No-op: this reader cannot write tokens.
}

// Detect looks for a display on a serial port no other driver has claimed.
func (r *Reader) Detect(connected []string) string {
	ports, err := helpers.GetUSBCDCDeviceList()
	if err != nil {
		log.Warn().Err(err).Msg("zapdisplay: failed to list serial ports")
		return ""
	}

	refreshFailedProbes()
	now := r.clock.Now()

	for _, path := range ports {
		if pathClaimed(connected, path) {
			continue
		}
		if probeFailedRecently(path, now) {
			continue
		}
		if !r.couldBeDisplay(path) {
			continue
		}
		if r.probe(path) {
			clearFailedProbe(path)
			return driverID + ":" + path
		}
		recordFailedProbe(path, now)
	}
	return ""
}

// couldBeDisplay reports whether a port is worth opening at all.
//
// Auto-detect runs every second, so without this the driver would write its
// handshake to every unrelated serial device on the machine, repeatedly. When
// the vendor cannot be determined the port is still probed: that is the case on
// platforms without udevadm, where refusing would make the display
// undetectable.
func (r *Reader) couldBeDisplay(path string) bool {
	vid, _, ok := r.vendorIDs(path)
	if !ok {
		return true
	}
	return vid == espressifVID
}

// pathClaimed reports whether any connected reader already owns this port.
// Connection strings are "driver:path", so the driver prefix is dropped first.
func pathClaimed(connected []string, path string) bool {
	for _, entry := range connected {
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) == 2 && parts[1] == path {
			return true
		}
	}
	return false
}

// probe opens a port and runs the handshake, then closes it again. Open
// re-opens the winner, matching how auto-detection drives every other driver.
func (r *Reader) probe(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	defer cancel()

	sess, err := r.dial(path)
	if err != nil {
		log.Trace().Err(err).Str("port", path).Msg("zapdisplay: port not usable")
		return false
	}
	defer func() {
		if closeErr := sess.close(); closeErr != nil {
			log.Trace().Err(closeErr).Str("port", path).Msg("zapdisplay: error closing probe port")
		}
	}()

	if err := sess.claim(ctx); err != nil {
		log.Trace().Err(err).Str("port", path).Msg("zapdisplay: handshake failed")
		return false
	}
	return true
}

func (r *Reader) dial(path string) (*session, error) {
	port, err := r.portFactory(path, &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return nil, fmt.Errorf("open serial port %s: %w", path, err)
	}
	if err := port.SetReadTimeout(readTimeout); err != nil {
		if closeErr := port.Close(); closeErr != nil {
			log.Trace().Err(closeErr).Msg("zapdisplay: error closing port after timeout failure")
		}
		return nil, fmt.Errorf("set read timeout on %s: %w", path, err)
	}
	return newSession(port), nil
}

// Open claims a display and starts the render worker.
func (r *Reader) Open(device config.ReadersConnect, _ chan<- readers.Scan, opts readers.OpenOpts) error {
	if !readers.MatchesDriverID(r.IDs(), device.Driver) {
		return fmt.Errorf("wrong driver for zapdisplay reader: %s", device.Driver)
	}

	logFailure := func(err error, msg string) {
		if opts.Probing {
			log.Trace().Err(err).Str("port", device.Path).Msg(msg)
			return
		}
		log.Error().Err(err).Str("port", device.Path).Msg(msg)
	}

	sess, err := r.dial(device.Path)
	if err != nil {
		logFailure(err, "zapdisplay: failed to open display")
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout*3)
	claimErr := sess.claim(ctx)
	cancel()
	if claimErr != nil {
		if closeErr := sess.close(); closeErr != nil {
			log.Trace().Err(closeErr).Msg("zapdisplay: error closing port after failed handshake")
		}
		logFailure(claimErr, "zapdisplay: handshake failed")
		return claimErr
	}

	r.mu.Lock()
	r.path = device.Path
	r.session = sess
	r.info = sess.info
	// A reconnect must not inherit the previous device's cover state: the new
	// device holds nothing.
	r.appliedKey = ""
	r.cachedKey = ""
	r.cachedID = ""
	r.cachedPix = nil
	// Always a fresh context so re-opening after Close works.
	r.workerCtx, r.workerCancel = context.WithCancel(context.Background())
	r.mu.Unlock()

	r.connected.Store(true)

	if err := r.renderIdle(r.workerCtx, sess); err != nil {
		log.Warn().Err(err).Msg("zapdisplay: failed to render idle scene")
	}

	r.wg.Add(1)
	go r.worker()

	log.Info().Str("port", device.Path).Str("info", sess.info).Msg("zapdisplay display connected")
	return nil
}

// Close stops the worker and releases the port. It is safe to call on a reader
// that was never opened, and safe to call twice.
func (r *Reader) Close() error {
	r.connected.Store(false)

	r.mu.Lock()
	cancel := r.workerCancel
	r.workerCancel = nil
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Wait outside the lock: the worker takes it while rendering.
	r.wg.Wait()

	r.mu.Lock()
	sess := r.session
	r.session = nil
	path := r.path
	r.mu.Unlock()

	if sess != nil {
		if err := sess.close(); err != nil {
			log.Debug().Err(err).Str("port", path).Msg("zapdisplay: error closing port")
		}
		log.Info().Str("port", path).Msg("zapdisplay display disconnected")
	}
	return nil
}

// OnMediaChange records the new state and wakes the worker.
//
// This runs on the media publish path while state locks are held, so it must
// not touch the serial port: a full cover upload takes on the order of a
// second and would stall every other media consumer.
func (r *Reader) OnMediaChange(media *models.ActiveMedia) error {
	if !r.connected.Load() {
		return errors.New("display is not connected")
	}

	r.mu.Lock()
	r.desired = media
	r.mu.Unlock()

	select {
	case r.wake <- struct{}{}:
	default:
		// A wake is already pending and the worker re-reads desired, so the
		// newest state is what gets rendered either way.
	}
	return nil
}

func (r *Reader) worker() {
	defer r.wg.Done()

	r.mu.RLock()
	ctx := r.workerCtx
	r.mu.RUnlock()

	ticker := r.clock.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
			r.guard("apply", func() { r.applyDesired(ctx) })
		case <-ticker.Chan():
			r.guard("refresh", func() { r.refreshScene(ctx) })
		}
	}
}

// guard runs one render step with panic recovery.
//
// The render path decodes PNG, JPEG and WebP from files Core scraped, which is
// the one input here that a malformed file could crash. Without this a bad
// cover would take the whole daemon down rather than one display.
//
// The link is dropped rather than retried: a panic part way through a scene
// leaves the device stream desynced, with a command possibly written and its
// response never read. The service prunes a disconnected reader and reconnects,
// which resynchronises through a fresh handshake.
func (r *Reader) guard(step string, run func()) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		log.Error().
			Str("step", step).
			Str("port", r.Path()).
			Interface("panic", recovered).
			Bytes("stack", debug.Stack()).
			Msg("zapdisplay: render panicked, dropping the link")
		r.connected.Store(false)
	}()
	run()
}

// applyDesired renders whatever media is currently wanted.
func (r *Reader) applyDesired(ctx context.Context) {
	r.mu.RLock()
	media := r.desired
	sess := r.session
	applied := r.appliedKey
	r.mu.RUnlock()

	if sess == nil {
		return
	}

	key := mediaKey(media)
	if key == applied {
		return
	}

	var err error
	switch media {
	case nil:
		err = r.renderIdle(ctx, sess)
	default:
		// The launching frame is decided during the render, once enough time
		// has actually passed to be worth acknowledging, rather than guessed
		// at up front.
		err = r.renderPlaying(ctx, sess, media, r.clock.Now())
	}
	if err != nil {
		r.handleRenderError(err)
		return
	}

	r.mu.Lock()
	r.appliedKey = key
	r.mu.Unlock()
}

// refreshScene re-sends the current scene without re-uploading artwork, so the
// firmware's image-retention offset advances during long sessions.
func (r *Reader) refreshScene(ctx context.Context) {
	r.mu.RLock()
	sess := r.session
	applied := r.appliedKey
	r.mu.RUnlock()

	if sess == nil || applied == "" {
		return
	}
	if _, err := sess.command(ctx, "SHOW", commandTimeout, "OK"); err != nil {
		r.handleRenderError(err)
	}
}

// handleRenderError decides whether a failure means the device has gone away.
// A protocol-level ERR is the device answering, so the link is fine; anything
// else is treated as a disconnect and lets the service prune the reader.
func (r *Reader) handleRenderError(err error) {
	var protoErr *protocolError
	if errors.As(err, &protoErr) {
		log.Warn().Err(err).Msg("zapdisplay: device rejected a command")
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	log.Warn().Err(err).Msg("zapdisplay: display link failed, marking disconnected")
	r.connected.Store(false)
}

func (*Reader) renderIdle(ctx context.Context, sess *session) error {
	if _, err := sess.command(ctx, "CLEAR", commandTimeout, "OK"); err != nil {
		return err
	}
	if _, err := sess.command(ctx, "SCENE idle", commandTimeout, "OK"); err != nil {
		return err
	}
	if _, err := sess.command(ctx, "STATUS Ready", commandTimeout, "OK"); err != nil {
		return err
	}
	// The device knows nothing about the time of day, so give it one. It has no
	// RTC and forgets this on reset, which is why it is re-sent whenever the
	// scene is rebuilt rather than only at connect.
	sess.pushClock(ctx)
	_, err := sess.command(ctx, "SHOW", commandTimeout, "OK")
	return err
}

// renderLaunching puts something on screen while the cover is still being
// fetched, scaled and pushed. That takes seconds on a fresh title, and a panel
// that sits on the previous game for the whole of it looks broken.
func (*Reader) renderLaunching(ctx context.Context, sess *session, media *models.ActiveMedia) error {
	if _, err := sess.command(ctx, "CLEAR", commandTimeout, "OK"); err != nil {
		return err
	}
	if _, err := sess.command(ctx, "SCENE launching", commandTimeout, "OK"); err != nil {
		return err
	}
	title := displayField(media.Name, maxTitleLen, "Untitled")
	if _, err := sess.command(ctx, "TITLE "+title, commandTimeout, "OK"); err != nil {
		return err
	}
	system := displayField(systemLabel(media), maxSystemLen, "Unknown system")
	if _, err := sess.command(ctx, "SYSTEM "+system, commandTimeout, "OK"); err != nil {
		return err
	}
	if _, err := sess.command(ctx, "STATUS Starting", commandTimeout, "OK"); err != nil {
		return err
	}
	_, err := sess.command(ctx, "SHOW", commandTimeout, "OK")
	return err
}

// playingStatus reports what the launcher is actually doing. A display
// insisting "Playing" over a paused game is simply wrong, and it is the kind of
// wrong a glanceable panel gets blamed for.
func playingStatus(media *models.ActiveMedia, hasCover bool) string {
	switch media.PlaybackState {
	case models.MediaPlaybackStatePaused:
		return "Paused"
	case models.MediaPlaybackStateStopped:
		return "Stopped"
	case models.MediaPlaybackStatePlaying:
	}
	if !hasCover {
		return "Playing - no cover"
	}
	return "Playing"
}

func (r *Reader) renderPlaying(
	ctx context.Context, sess *session, media *models.ActiveMedia, started time.Time,
) error {
	if _, err := sess.command(ctx, "CLEAR", commandTimeout, "OK"); err != nil {
		return err
	}

	hasCover := r.applyCover(ctx, sess, media, started)

	title := displayField(media.Name, maxTitleLen, "Untitled")
	system := displayField(systemLabel(media), maxSystemLen, "Unknown system")
	status := playingStatus(media, hasCover)

	if _, err := sess.command(ctx, "SCENE playing", commandTimeout, "OK"); err != nil {
		return err
	}
	if _, err := sess.command(ctx, "TITLE "+title, commandTimeout, "OK"); err != nil {
		return err
	}
	if _, err := sess.command(ctx, "SYSTEM "+system, commandTimeout, "OK"); err != nil {
		return err
	}
	if _, err := sess.command(ctx, "STATUS "+status, commandTimeout, "OK"); err != nil {
		return err
	}
	// Core knows when the session began, so the device is told once and counts
	// on from there. Sending a duration every minute would be the only reason
	// left to talk to it during a long session.
	if sess.features["clock"] && !media.Started.IsZero() {
		elapsed := int(time.Since(media.Started).Seconds())
		if elapsed >= 0 {
			text := fmt.Sprintf("ELAPSED %d", elapsed)
			if _, err := sess.command(ctx, text, commandTimeout, "OK"); err != nil {
				return err
			}
		}
	}
	_, err := sess.command(ctx, "SHOW", commandTimeout, "OK")
	return err
}

// applyCover gets the cover onto the device and selects it, reporting whether
// the playing scene will have artwork. A missing or unreadable cover is an
// ordinary outcome: the scene still renders, just without one.
func (r *Reader) applyCover(
	ctx context.Context, sess *session, media *models.ActiveMedia, started time.Time,
) bool {
	key := mediaKey(media)

	r.mu.RLock()
	source := r.artwork
	cachedKey := r.cachedKey
	assetID := r.cachedID
	pixels := r.cachedPix
	width := r.cachedW
	height := r.cachedH
	r.mu.RUnlock()

	if cachedKey != key || assetID == "" {
		if source == nil {
			return false
		}
		art, err := source.Artwork(ctx, media.SystemID, media.Path, artworkMaxSize)
		if err != nil {
			if !errors.Is(err, readers.ErrNoArtwork) {
				log.Debug().Err(err).Str("media", media.Name).Msg("zapdisplay: could not resolve cover")
			}
			return false
		}
		encoded, err := encodeCover(art.Data, art.ContentType)
		if err != nil {
			log.Debug().Err(err).Str("media", media.Name).Msg("zapdisplay: could not encode cover")
			return false
		}
		pixels = encoded.pixels
		width = encoded.width
		height = encoded.height
		assetID = coverAssetID(media.SystemID, media.Path, pixels)

		// Resolving artwork is the variable part of a media change: a database
		// miss or a large image decode is what makes one slow. Once that has
		// already cost more than launchingDelay, put a frame up before the
		// upload so the display is not left on the previous game.
		if r.clock.Since(started) >= launchingDelay {
			if launchErr := r.renderLaunching(ctx, sess, media); launchErr != nil {
				// Not fatal: the playing frame below is the one that matters.
				log.Debug().Err(launchErr).Msg("zapdisplay: could not show launching frame")
			}
		}

		if err := sess.uploadCover(ctx, assetID, pixels); err != nil {
			log.Debug().Err(err).Msg("zapdisplay: cover upload failed")
			return false
		}
		r.mu.Lock()
		r.cachedKey = key
		r.cachedID = assetID
		r.cachedPix = pixels
		r.cachedW = width
		r.cachedH = height
		r.mu.Unlock()
	}

	err := sess.useCover(ctx, assetID, width, height)
	if err == nil {
		return true
	}
	if !isAssetMissing(err) {
		log.Debug().Err(err).Msg("zapdisplay: could not select cover")
		return false
	}

	// The device lost the asset, which normally means it rebooted. Send it once
	// more before giving up on artwork for this scene.
	if err := sess.uploadCover(ctx, assetID, pixels); err != nil {
		log.Debug().Err(err).Msg("zapdisplay: cover re-upload failed")
		return false
	}
	if err := sess.useCover(ctx, assetID, width, height); err != nil {
		log.Debug().Err(err).Msg("zapdisplay: could not select cover after re-upload")
		return false
	}
	return true
}

// mediaKey identifies what the display is showing. Empty means idle.
func mediaKey(media *models.ActiveMedia) string {
	if media == nil {
		return ""
	}
	return media.SystemID + "\x00" + media.Path
}

func systemLabel(media *models.ActiveMedia) string {
	if media.SystemName != "" {
		return media.SystemName
	}
	return media.SystemID
}

// newASCIIFolder folds accents down to their base letters so titles such as
// "Pokémon" render as "Pokemon" rather than losing characters: the device's
// bundled font only covers printable ASCII.
//
// A fresh chain per call is deliberate. transform.Chain carries internal
// buffers and is not safe to share, and this runs a handful of times per media
// change.
func newASCIIFolder() transform.Transformer {
	return transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
}

// displayField prepares text for a protocol field: folded to ASCII, stripped of
// anything the device cannot render, and truncated to the device's buffer.
func displayField(value string, maxLen int, fallback string) string {
	folded, _, err := transform.String(newASCIIFolder(), value)
	if err != nil {
		folded = value
	}

	var b strings.Builder
	for _, r := range folded {
		switch {
		case r >= ' ' && r <= '~':
			_, _ = b.WriteRune(r)
		case unicode.IsSpace(r):
			_ = b.WriteByte(' ')
		default:
			// Unrenderable. Dropping is better than a run of '?' glyphs.
		}
	}

	out := strings.Join(strings.Fields(b.String()), " ")
	if out == "" {
		return fallback
	}
	if len(out) > maxLen {
		out = strings.TrimSpace(out[:maxLen])
	}
	return out
}
