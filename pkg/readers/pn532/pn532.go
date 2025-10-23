// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package pn532

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	_ "github.com/ZaparooProject/go-pn532/detection/uart"
	"github.com/ZaparooProject/go-pn532/polling"
	"github.com/ZaparooProject/go-pn532/transport/i2c"
	"github.com/ZaparooProject/go-pn532/transport/spi"
	"github.com/ZaparooProject/go-pn532/transport/uart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

const (
	quickDetectionTimeout = 5 * time.Second
	ndefReadTimeout       = 2 * time.Second
	writeTimeout          = 30 * time.Second
	deviceTimeout         = 5 * time.Second
)

// PN532Device abstracts the pn532.Device for testing.
type PN532Device interface {
	Init() error
	SetTimeout(timeout time.Duration) error
	Close() error
}

// PollingSession abstracts the polling.Session for testing.
type PollingSession interface {
	Start(ctx context.Context) error
	Close() error
	SetOnCardDetected(callback func(*pn532.DetectedTag) error)
	SetOnCardRemoved(callback func())
	SetOnCardChanged(callback func(*pn532.DetectedTag) error)
	WriteToNextTag(
		ctx, writeCtx context.Context,
		timeout time.Duration,
		writeFunc func(context.Context, pn532.Tag) error,
	) error
}

// TransportFactory creates a transport from device info.
type TransportFactory func(deviceInfo detection.DeviceInfo) (pn532.Transport, error)

// DeviceFactory creates a PN532 device from a transport.
type DeviceFactory func(transport pn532.Transport) (PN532Device, error)

// SessionFactory creates a polling session from a device.
type SessionFactory func(device PN532Device, sessionConfig *polling.Config) PollingSession

// DefaultTransportFactory creates a real transport.
func DefaultTransportFactory(deviceInfo detection.DeviceInfo) (pn532.Transport, error) {
	switch deviceInfo.Transport {
	case "uart":
		transport, err := uart.New(deviceInfo.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create UART transport: %w", err)
		}
		return transport, nil
	case "i2c":
		transport, err := i2c.New(deviceInfo.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create I2C transport: %w", err)
		}
		return transport, nil
	case "spi":
		transport, err := spi.New(deviceInfo.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create SPI transport: %w", err)
		}
		return transport, nil
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", deviceInfo.Transport)
	}
}

// DefaultDeviceFactory creates a real pn532.Device.
func DefaultDeviceFactory(transport pn532.Transport) (PN532Device, error) {
	device, err := pn532.New(transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create PN532 device: %w", err)
	}
	return device, nil
}

// realSession wraps polling.Session to implement PollingSession interface.
type realSession struct {
	session *polling.Session
}

func (s *realSession) Start(ctx context.Context) error {
	if err := s.session.Start(ctx); err != nil {
		return fmt.Errorf("failed to start polling session: %w", err)
	}
	return nil
}

func (s *realSession) Close() error {
	if err := s.session.Close(); err != nil {
		return fmt.Errorf("failed to close polling session: %w", err)
	}
	return nil
}

func (s *realSession) SetOnCardDetected(callback func(*pn532.DetectedTag) error) {
	s.session.OnCardDetected = callback
}

func (s *realSession) SetOnCardRemoved(callback func()) {
	s.session.OnCardRemoved = callback
}

func (s *realSession) SetOnCardChanged(callback func(*pn532.DetectedTag) error) {
	s.session.OnCardChanged = callback
}

func (s *realSession) WriteToNextTag(
	ctx, writeCtx context.Context,
	timeout time.Duration,
	writeFunc func(context.Context, pn532.Tag) error,
) error {
	if err := s.session.WriteToNextTag(ctx, writeCtx, timeout, writeFunc); err != nil {
		return fmt.Errorf("failed to write to tag: %w", err)
	}
	return nil
}

// DefaultSessionFactory creates a real polling.Session.
func DefaultSessionFactory(device PN532Device, sessionConfig *polling.Config) PollingSession {
	// Cast back to *pn532.Device for the real session
	if dev, ok := device.(*pn532.Device); ok {
		return &realSession{session: polling.NewSession(dev, sessionConfig)}
	}
	return nil
}

func createVIDPIDBlocklist() []string {
	return []string{
		"16C0:0F38", // Sinden Lightgun
		"16C0:0F39", // Sinden Lightgun
		"16C0:0F01", // Sinden Lightgun
		"16C0:0F02", // Sinden Lightgun
		"16D0:0F38", // Sinden Lightgun
		"16D0:0F39", // Sinden Lightgun
		"16D0:0F01", // Sinden Lightgun
		"16D0:0F02", // Sinden Lightgun
		"16D0:1094", // Sinden Lightgun
		"16D0:1095", // Sinden Lightgun
		"16D0:1096", // Sinden Lightgun
		"16D0:1097", // Sinden Lightgun
		"16D0:1098", // Sinden Lightgun
		"16D0:1099", // Sinden Lightgun
		"16D0:109A", // Sinden Lightgun
		"16D0:109B", // Sinden Lightgun
		"16D0:109C", // Sinden Lightgun
		"16D0:109D", // Sinden Lightgun
	}
}

type Reader struct {
	session          PollingSession
	writeCtx         context.Context
	device           PN532Device
	ctx              context.Context
	writeCancel      context.CancelFunc
	cfg              *config.Instance
	lastToken        *tokens.Token
	cancel           context.CancelFunc
	realDevice       *pn532.Device
	transportFactory TransportFactory
	deviceFactory    DeviceFactory
	sessionFactory   SessionFactory
	deviceInfo       config.ReadersConnect
	name             string
	wg               sync.WaitGroup
	mutex            sync.RWMutex
	writeMutex       sync.Mutex
}

func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:              cfg,
		transportFactory: DefaultTransportFactory,
		deviceFactory:    DefaultDeviceFactory,
		sessionFactory:   DefaultSessionFactory,
	}
}

func (*Reader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                "pn532",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
		Description:       "PN532 NFC reader (UART/I2C/SPI)",
	}
}

func (*Reader) IDs() []string {
	return []string{
		"pn532",
		"pn532_uart",
		"pn532_i2c",
		"pn532_spi",
	}
}

func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Parse device path to determine transport and path
	var transport pn532.Transport
	var err error

	// Manual device specification
	// Extract transport type from driver (e.g., "pn532_uart" -> "uart")
	transportType := strings.TrimPrefix(device.Driver, "pn532_")
	if transportType == device.Driver {
		// If no prefix was removed, assume it's just "pn532" and default to uart
		transportType = "uart"
	}

	deviceInfo := detection.DeviceInfo{
		Transport: transportType,
		Path:      device.Path,
	}

	transport, err = r.transportFactory(deviceInfo)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

	r.name = device.ConnectionString()
	log.Debug().Msgf("opening PN532 device: %s", r.name)

	// Create PN532 device
	r.device, err = r.deviceFactory(transport)
	if err != nil {
		if transport != nil {
			_ = transport.Close()
		}
		return fmt.Errorf("failed to create PN532 device: %w", err)
	}

	// Store real device if it's a *pn532.Device (for tag operations)
	if realDev, ok := r.device.(*pn532.Device); ok {
		r.realDevice = realDev
	}

	// Initialize device
	err = r.device.Init()
	if err != nil {
		_ = r.device.Close()
		return fmt.Errorf("failed to initialize PN532 device: %w", err)
	}

	// Set timeout to match cmd/reader behavior (prevents constant LED blinking)
	err = r.device.SetTimeout(deviceTimeout)
	if err != nil {
		_ = r.device.Close()
		return fmt.Errorf("failed to set device timeout: %w", err)
	}

	r.deviceInfo = device
	r.ctx, r.cancel = context.WithCancel(context.Background())

	// Create session configuration
	sessionConfig := polling.DefaultConfig()

	// Create session with callbacks
	r.session = r.sessionFactory(r.device, sessionConfig)

	// Set up callbacks
	r.session.SetOnCardDetected(func(detectedTag *pn532.DetectedTag) error {
		return r.handleTagDetected(detectedTag, iq)
	})

	r.session.SetOnCardRemoved(func() {
		r.handleTagRemoved(iq)
	})

	r.session.SetOnCardChanged(func(detectedTag *pn532.DetectedTag) error {
		return r.handleTagDetected(detectedTag, iq)
	})

	// Start session
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if err := r.session.Start(r.ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Error().Err(err).Msg("PN532 session ended with error")

				// Send reader error notification to prevent triggering on_remove/exit
				r.mutex.Lock()
				hasActiveToken := r.lastToken != nil
				if hasActiveToken {
					log.Warn().Msg("reader session error with active token - " +
						"sending error signal to keep media running")
					iq <- readers.Scan{
						Source:      r.deviceInfo.ConnectionString(),
						Token:       nil,
						ReaderError: true,
					}
					r.lastToken = nil
				}
				r.mutex.Unlock()
			}
		}
	}()

	log.Info().Msgf("PN532 reader opened: %s", r.name)
	return nil
}

func (r *Reader) handleTagDetected(detectedTag *pn532.DetectedTag, iq chan<- readers.Scan) error {
	log.Info().Msgf("new tag detected: %s (%s)", detectedTag.Type, detectedTag.UID)
	r.processNewTag(detectedTag, iq)
	return nil
}

func (r *Reader) handleTagRemoved(iq chan<- readers.Scan) {
	log.Info().Msg("tag removed")
	iq <- readers.Scan{
		Source: r.deviceInfo.ConnectionString(),
		Token:  nil,
	}

	r.mutex.Lock()
	r.lastToken = nil
	r.mutex.Unlock()
}

func (r *Reader) processNewTag(detectedTag *pn532.DetectedTag, iq chan<- readers.Scan) {
	tokenType := r.convertTagType(detectedTag.Type)
	ndefText, rawData := r.readNDEFData(detectedTag)

	token := &tokens.Token{
		Type:     tokenType,
		UID:      detectedTag.UID,
		Text:     ndefText,
		Data:     hex.EncodeToString(rawData),
		ScanTime: time.Now(),
		Source:   r.deviceInfo.ConnectionString(),
	}

	log.Info().Msgf("detected %s tag: %s", token.Type, token.UID)
	if token.Text != "" {
		log.Info().Msgf("NDEF text: %s", token.Text)
	}

	iq <- readers.Scan{
		Source: r.deviceInfo.ConnectionString(),
		Token:  token,
	}

	r.mutex.Lock()
	r.lastToken = token
	r.mutex.Unlock()
}

func (r *Reader) Close() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.cancel != nil {
		r.cancel()
	}

	if r.session != nil {
		err := r.session.Close()
		if err != nil {
			return fmt.Errorf("failed to close PN532 session: %w", err)
		}
	}

	// Wait for session goroutine to complete
	r.wg.Wait()

	return nil
}

func (*Reader) Detect(connected []string) string {
	// Extract device paths from connected list (format: "transport:path")
	ignorePaths := make([]string, 0, len(connected))
	for _, conn := range connected {
		parts := strings.SplitN(conn, ":", 2)
		if len(parts) >= 2 && parts[1] != "" {
			ignorePaths = append(ignorePaths, parts[1])
		}
	}
	log.Trace().Msgf("PN532: ignoring paths: %v", ignorePaths)

	// Try to detect PN532 devices
	opts := detection.DefaultOptions()
	opts.Timeout = quickDetectionTimeout
	opts.Mode = detection.Safe
	opts.Blocklist = createVIDPIDBlocklist()
	opts.IgnorePaths = ignorePaths

	devices, err := detection.DetectAll(&opts)
	if err != nil {
		log.Trace().Err(err).Msg("PN532 detection failed")
		return ""
	}

	if len(devices) == 0 {
		return ""
	}

	// Check each detected device to find one not already connected
	for _, device := range devices {
		deviceStr := fmt.Sprintf("pn532_%s:%s", device.Transport, device.Path)

		// Check if this device path is already in use by any connected reader
		deviceInUse := false
		for _, connectedDevice := range connected {
			// Parse connected device string (format: "driver:path")
			parts := strings.SplitN(connectedDevice, ":", 2)
			if len(parts) == 2 && parts[1] == device.Path {
				log.Trace().
					Str("device_path", device.Path).
					Str("connected_as", connectedDevice).
					Str("attempted_as", deviceStr).
					Msg("pn532: device already connected, skipping")
				deviceInUse = true
				break
			}
		}

		if !deviceInUse {
			log.Trace().Msgf("detected PN532 device: %s", deviceStr)
			return deviceStr
		}
	}

	// All detected devices are already in use
	log.Trace().Msg("pn532: all detected devices are already connected")
	return ""
}

func (r *Reader) Device() string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.deviceInfo.ConnectionString()
}

func (r *Reader) Connected() bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.device != nil && r.ctx != nil && r.ctx.Err() == nil
}

func (r *Reader) Info() string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return "PN532 (" + r.name + ")"
}

func (r *Reader) Write(text string) (*tokens.Token, error) {
	return r.WriteWithContext(context.Background(), text)
}

func (r *Reader) WriteWithContext(ctx context.Context, text string) (*tokens.Token, error) {
	if text == "" {
		return nil, errors.New("text cannot be empty")
	}

	// Lock for the entire write operation
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.session == nil {
		return nil, errors.New("session not initialized")
	}

	// Create cancellable context for this write operation under writeMutex
	r.writeMutex.Lock()
	r.writeCtx, r.writeCancel = context.WithCancel(ctx)
	writeCtx := r.writeCtx
	r.writeMutex.Unlock()

	// Ensure cleanup
	defer func() {
		r.writeMutex.Lock()
		if r.writeCancel != nil {
			r.writeCancel()
			r.writeCancel = nil
			r.writeCtx = nil
		}
		r.writeMutex.Unlock()
	}()

	var resultToken *tokens.Token
	var writeErr error

	err := r.session.WriteToNextTag(
		ctx, writeCtx, writeTimeout,
		func(writeCtx context.Context, tag pn532.Tag) error {
			// Create NDEF message with text record
			ndefMessage := &pn532.NDEFMessage{
				Records: []pn532.NDEFRecord{{
					Type: pn532.NDEFTypeText,
					Text: text,
				}},
			}

			// Write NDEF message to tag using the provided write context
			if err := tag.WriteNDEFWithContext(writeCtx, ndefMessage); err != nil {
				writeErr = fmt.Errorf("failed to write NDEF to tag: %w", err)
				return writeErr
			}

			log.Info().Msgf("successfully wrote text to PN532 tag: %s", text)

			// Create result token with UID from the tag
			tagType := tag.Type()
			resultToken = &tokens.Token{
				UID:      tag.UID(),
				Text:     text,
				Type:     r.convertTagType(tagType),
				ScanTime: time.Now(),
				Source:   r.deviceInfo.ConnectionString(),
			}

			return nil
		})
	if err != nil {
		if writeErr != nil {
			return nil, writeErr
		}
		return nil, fmt.Errorf("failed to write to tag: %w", err)
	}

	return resultToken, nil
}

func (r *Reader) CancelWrite() {
	r.writeMutex.Lock()
	defer r.writeMutex.Unlock()

	if r.writeCancel != nil {
		log.Debug().Msg("cancelling ongoing write operation")
		r.writeCancel()
	}
}

func (*Reader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityWrite}
}

func (*Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}
