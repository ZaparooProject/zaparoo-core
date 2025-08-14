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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	// Import detection packages to register detectors
	_ "github.com/ZaparooProject/go-pn532/detection/i2c"
	_ "github.com/ZaparooProject/go-pn532/detection/spi"
	_ "github.com/ZaparooProject/go-pn532/detection/uart"
	"github.com/ZaparooProject/go-pn532/tagops"
	"github.com/ZaparooProject/go-pn532/transport/i2c"
	"github.com/ZaparooProject/go-pn532/transport/spi"
	"github.com/ZaparooProject/go-pn532/transport/uart"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/hsanjuan/go-ndef"
	"github.com/rs/zerolog/log"
)

const (
	maxErrorCount         = 5
	pollTimeout           = 50 * time.Millisecond
	pollInterval          = 50 * time.Millisecond
	detectionTimeout      = 10 * time.Second
	quickDetectionTimeout = 5 * time.Second
	detectionCacheTimeout = 30 * time.Second
	errorBackoffDelay     = 500 * time.Millisecond
)

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

type tagState struct {
	lastUID  string
	lastType string
	present  bool
}

type Reader struct {
	ctx         context.Context
	writeCtx    context.Context
	device      *pn532.Device
	cfg         *config.Instance
	lastToken   *tokens.Token
	cancel      context.CancelFunc
	writeCancel context.CancelFunc
	deviceInfo  config.ReadersConnect
	name        string
	tagState    tagState
	mutex       sync.RWMutex
	writeMutex  sync.RWMutex
}

func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg: cfg,
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

func (*Reader) createTransport(deviceInfo detection.DeviceInfo) (pn532.Transport, error) {
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

func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Parse device path to determine transport and path
	var transport pn532.Transport
	var err error

	// Check if this is an auto-detected device or a manual path
	if device.Path == "" {
		// Auto-detect device
		opts := detection.DefaultOptions()
		opts.Timeout = detectionTimeout
		opts.Mode = detection.Safe
		opts.Blocklist = createVIDPIDBlocklist()

		devices, detectErr := detection.DetectAll(&opts)
		if detectErr != nil {
			return fmt.Errorf("failed to detect PN532 devices: %w", detectErr)
		}

		if len(devices) == 0 {
			return errors.New("no PN532 devices found")
		}

		// Use the first detected device
		deviceInfo := devices[0]
		transport, err = r.createTransport(deviceInfo)
		if err != nil {
			return fmt.Errorf("failed to create transport: %w", err)
		}

		r.name = fmt.Sprintf("%s:%s", deviceInfo.Transport, deviceInfo.Path)
		log.Debug().Msgf("auto-detected PN532 device: %s", r.name)
	} else {
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

		transport, err = r.createTransport(deviceInfo)
		if err != nil {
			return fmt.Errorf("failed to create transport: %w", err)
		}

		r.name = device.ConnectionString()
		log.Debug().Msgf("opening manual PN532 device: %s", r.name)
	}

	// Create PN532 device
	r.device, err = pn532.New(transport)
	if err != nil {
		if transport != nil {
			_ = transport.Close()
		}
		return fmt.Errorf("failed to create PN532 device: %w", err)
	}

	// Initialize device
	err = r.device.Init()
	if err != nil {
		_ = r.device.Close()
		return fmt.Errorf("failed to initialize PN532 device: %w", err)
	}

	// Configure optimized continuous polling (from nfctest)
	pollConfig := pn532.DefaultContinuousPollConfig()
	pollConfig.PollCount = 1
	pollConfig.PollPeriod = 1
	if err := r.device.SetPollConfig(pollConfig); err != nil {
		log.Warn().Err(err).Msg("failed to set optimized poll config, using defaults")
	}

	r.deviceInfo = device
	r.ctx, r.cancel = context.WithCancel(context.Background())

	// Start polling goroutine
	go r.pollLoop(iq)

	log.Info().Msgf("PN532 reader opened: %s", r.name)
	return nil
}

func (r *Reader) pollLoop(iq chan<- readers.Scan) {
	errCount := 0

	for {
		select {
		case <-r.ctx.Done():
			log.Debug().Msg("PN532 polling cancelled")
			return
		default:
		}

		if errCount >= maxErrorCount {
			log.Error().Msg("too many errors, exiting PN532 reader")
			break
		}

		pollCtx, cancel := context.WithTimeout(r.ctx, pollTimeout)
		detectedTag, err := r.device.DetectTagContext(pollCtx)
		cancel()

		if err != nil {
			r.handlePollingError(err, iq, &errCount)
			continue
		}

		// Tag detected successfully
		errCount = 0
		r.processDetectedTag(detectedTag, iq)

		// Short sleep to prevent excessive CPU usage
		time.Sleep(pollInterval)
	}
}

func (r *Reader) handlePollingError(err error, iq chan<- readers.Scan, errCount *int) {
	if errors.Is(err, context.Canceled) {
		log.Debug().Msg("PN532 polling cancelled")
		return
	}

	if errors.Is(err, pn532.ErrNoTagDetected) {
		// Handle tag removal
		r.handleTagRemoval(iq)
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		// Timeout is normal - just handle as no tag
		r.handleTagRemoval(iq)
		return
	}

	// Actual error
	log.Error().Err(err).Msg("failed to detect tag")
	*errCount++
	time.Sleep(errorBackoffDelay)
}

func (r *Reader) handleTagRemoval(iq chan<- readers.Scan) {
	if r.tagState.present {
		log.Info().Msgf("tag removed: %s", r.tagState.lastUID)
		iq <- readers.Scan{
			Source: r.deviceInfo.ConnectionString(),
			Token:  nil,
		}
		r.resetTagState()
	}
}

func (r *Reader) resetTagState() {
	r.tagState.present = false
	r.tagState.lastUID = ""
	r.tagState.lastType = ""
	r.lastToken = nil
}

func (r *Reader) processDetectedTag(detectedTag *pn532.DetectedTag, iq chan<- readers.Scan) {
	currentUID := detectedTag.UID
	tagType := string(detectedTag.Type)

	// Check if this is a new tag or tag change
	tagChanged := r.updateTagState(currentUID, tagType)
	if !tagChanged {
		// Same tag as before, no need to reprocess
		return
	}

	// Process the new/changed tag
	r.processNewTag(detectedTag, iq)
}

func (r *Reader) updateTagState(currentUID, tagType string) bool {
	if !r.tagState.present {
		// New tag detected
		log.Info().Msgf("new tag detected: %s (%s)", tagType, currentUID)
		r.tagState.present = true
		r.tagState.lastUID = currentUID
		r.tagState.lastType = tagType
		return true
	}

	if r.tagState.lastUID != currentUID {
		// Different tag detected
		log.Info().Msgf("different tag detected: %s (%s)", tagType, currentUID)
		r.tagState.lastUID = currentUID
		r.tagState.lastType = tagType
		return true
	}

	// Same tag as before
	return false
}

func (r *Reader) processNewTag(detectedTag *pn532.DetectedTag, iq chan<- readers.Scan) {
	// Convert tag type
	tokenType := r.convertTagType(detectedTag.Type)

	// Try to read NDEF data using unified tagops approach
	ndefText, rawData := r.readNDEFData(detectedTag)

	// Create token
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

	r.lastToken = token
}

func (*Reader) convertTagType(tagType pn532.TagType) string {
	switch tagType {
	case pn532.TagTypeNTAG:
		return tokens.TypeNTAG
	case pn532.TagTypeMIFARE:
		return tokens.TypeMifare
	case pn532.TagTypeFeliCa:
		return tokens.TypeFeliCa
	case pn532.TagTypeUnknown, pn532.CardTypeAny:
		return tokens.TypeUnknown
	default:
		return tokens.TypeUnknown
	}
}

func (r *Reader) readNDEFData(detectedTag *pn532.DetectedTag) (text string, data []byte) {
	tagOps := tagops.New(r.device)

	// Detect tag first
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := tagOps.DetectTag(ctx); err != nil {
		log.Debug().Err(err).Msg("failed to detect tag for NDEF reading")
		return "", detectedTag.TargetData
	}

	// Read NDEF message
	ndefMessage, err := tagOps.ReadNDEF(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("failed to read NDEF data")
		return "", detectedTag.TargetData
	}

	if ndefMessage == nil || len(ndefMessage.Records) == 0 {
		return "", detectedTag.TargetData
	}

	// Process NDEF records and convert to token text
	tokenText := r.convertNDEFToTokenText(ndefMessage)

	// Return the token text and original target data
	return tokenText, detectedTag.TargetData
}

// convertNDEFToTokenText converts NDEF message to token text:
// - Text and URI records pass through directly
// - All other types (WiFi, VCard, etc.) convert to JSON
func (*Reader) convertNDEFToTokenText(ndefMessage *ndef.Message) string {
	if ndefMessage == nil || len(ndefMessage.Records) == 0 {
		return ""
	}

	// Process first record (primary content)
	record := ndefMessage.Records[0]
	tnf := record.TNF()
	typeField := record.Type()

	// Handle text records - pass through directly
	if tnf == ndef.NFCForumWellKnownType && typeField == "T" {
		payload, err := record.Payload()
		if err != nil {
			return ""
		}
		payloadBytes := payload.Marshal()
		if len(payloadBytes) > 3 {
			// Skip language code to get actual text
			langLen := int(payloadBytes[0] & 0x3F)
			if len(payloadBytes) > langLen+1 {
				return string(payloadBytes[langLen+1:])
			}
		}
		return ""
	}

	// Handle URI records - pass through directly
	if tnf == ndef.NFCForumWellKnownType && typeField == "U" {
		payload, err := record.Payload()
		if err != nil {
			return ""
		}
		return string(payload.Marshal())
	}

	// Handle WiFi credentials - convert to JSON
	if typeField == "application/vnd.wfa.wsc" {
		return convertWiFiToJSON(record)
	}

	// Handle VCard records - convert to JSON
	if typeField == "text/vcard" || typeField == "text/x-vcard" {
		return convertVCardToJSON(record)
	}

	// Handle Smart Poster records - convert to JSON
	if tnf == ndef.NFCForumWellKnownType && typeField == "Sp" {
		return convertSmartPosterToJSON(record)
	}

	// For any other complex types, convert to generic JSON
	return convertGenericRecordToJSON(record)
}

func convertWiFiToJSON(record *ndef.Record) string {
	payload, err := record.Payload()
	if err != nil {
		return ""
	}

	wifiData := map[string]any{
		"type": "wifi",
		"raw":  hex.EncodeToString(payload.Marshal()),
	}

	// Try to parse WiFi credentials if possible
	// This is a simplified approach - full parsing would require WSC binary format parsing
	wifiData["note"] = "WiFi credentials (binary format)"

	jsonBytes, err := json.Marshal(wifiData)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

func convertVCardToJSON(record *ndef.Record) string {
	payload, err := record.Payload()
	if err != nil {
		return ""
	}

	vcardText := string(payload.Marshal())

	vcardData := map[string]any{
		"type":  "vcard",
		"vcard": vcardText,
	}

	// Try to extract basic contact info
	lines := strings.Split(vcardText, "\n")
	contact := make(map[string]string)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "FN:"):
			contact["name"] = strings.TrimPrefix(line, "FN:")
		case strings.HasPrefix(line, "TEL:"):
			contact["phone"] = strings.TrimPrefix(line, "TEL:")
		case strings.HasPrefix(line, "EMAIL:"):
			contact["email"] = strings.TrimPrefix(line, "EMAIL:")
		}
	}

	if len(contact) > 0 {
		vcardData["contact"] = contact
	}

	jsonBytes, err := json.Marshal(vcardData)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

func convertSmartPosterToJSON(record *ndef.Record) string {
	payload, err := record.Payload()
	if err != nil {
		return ""
	}

	posterData := map[string]any{
		"type": "smartposter",
		"raw":  hex.EncodeToString(payload.Marshal()),
	}

	jsonBytes, err := json.Marshal(posterData)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

func convertGenericRecordToJSON(record *ndef.Record) string {
	payload, err := record.Payload()
	if err != nil {
		return ""
	}

	genericData := map[string]any{
		"type":      "unknown",
		"tnf":       record.TNF(),
		"typeField": record.Type(),
		"payload":   hex.EncodeToString(payload.Marshal()),
	}

	jsonBytes, err := json.Marshal(genericData)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

func (r *Reader) Close() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.cancel != nil {
		r.cancel()
	}

	if r.device != nil {
		err := r.device.Close()
		if err != nil {
			return fmt.Errorf("failed to close PN532 device: %w", err)
		}
	}

	return nil
}

// Static cache for failed detection attempts to avoid repeated failures
var (
	detectionCacheMu  sync.RWMutex
	lastDetectionFail time.Time
)

func (*Reader) Detect(connected []string) string {
	// Check cache first
	detectionCacheMu.RLock()
	if !lastDetectionFail.IsZero() && time.Since(lastDetectionFail) < detectionCacheTimeout {
		detectionCacheMu.RUnlock()
		return ""
	}
	detectionCacheMu.RUnlock()

	// Check if a PN532 device is already connected
	for _, conn := range connected {
		if strings.HasPrefix(conn, "pn532:") {
			return ""
		}
	}

	// Try to detect PN532 devices
	opts := detection.DefaultOptions()
	opts.Timeout = quickDetectionTimeout
	opts.Mode = detection.Safe
	opts.Blocklist = createVIDPIDBlocklist()

	devices, err := detection.DetectAll(&opts)
	if err != nil {
		log.Debug().Err(err).Msg("PN532 detection failed")

		// Cache the failure
		detectionCacheMu.Lock()
		lastDetectionFail = time.Now()
		detectionCacheMu.Unlock()

		return ""
	}

	if len(devices) == 0 {
		// Cache the failure
		detectionCacheMu.Lock()
		lastDetectionFail = time.Now()
		detectionCacheMu.Unlock()

		return ""
	}

	// Clear cache on successful detection
	detectionCacheMu.Lock()
	lastDetectionFail = time.Time{}
	detectionCacheMu.Unlock()

	// Return the first detected device
	device := devices[0]
	deviceStr := fmt.Sprintf("pn532_%s:%s", device.Transport, device.Path)

	log.Debug().Msgf("detected PN532 device: %s", deviceStr)
	return deviceStr
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
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.device == nil {
		return nil, errors.New("device not connected")
	}

	if text == "" {
		return nil, errors.New("text cannot be empty")
	}

	// Create cancellable context for this write operation
	r.writeMutex.Lock()
	r.writeCtx, r.writeCancel = context.WithCancel(ctx)
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

	// Use tagops for writing to all tag types (unified approach)
	tagOps := tagops.New(r.device)

	// Detect tag first
	detectCtx, cancel := context.WithTimeout(r.writeCtx, 5*time.Second)
	defer cancel()

	if err := tagOps.DetectTag(detectCtx); err != nil {
		return nil, fmt.Errorf("failed to detect tag for writing: %w", err)
	}

	// Create NDEF message with text record
	textRecord := ndef.NewTextRecord(text, "en")
	ndefMessage := ndef.NewMessageFromRecords(textRecord)

	// Write NDEF message
	writeCtx, writeCancel := context.WithTimeout(r.writeCtx, 10*time.Second)
	defer writeCancel()

	if err := tagOps.WriteNDEF(writeCtx, ndefMessage); err != nil {
		return nil, fmt.Errorf("failed to write NDEF to tag: %w", err)
	}

	log.Info().Msgf("successfully wrote text to PN532 tag: %s", text)

	// Detect tag again to get the tag type and UID after writing
	detectCtx, detectCancel := context.WithTimeout(r.writeCtx, 2*time.Second)
	defer detectCancel()

	detectedTag, err := r.device.DetectTagContext(detectCtx)
	var uid, tagType string
	if err != nil {
		log.Warn().Err(err).Msg("failed to detect tag after writing, using fallback values")
		uid = hex.EncodeToString(tagOps.GetUID())
		tagType = tokens.TypeUnknown
	} else {
		uid = detectedTag.UID
		tagType = r.convertTagType(detectedTag.Type)
	}

	// Update tag state to prevent immediate re-detection as new tag
	r.tagState.present = true
	r.tagState.lastUID = uid
	r.tagState.lastType = tagType

	return &tokens.Token{
		UID:      uid,
		Type:     tagType,
		Text:     text,
		ScanTime: time.Now(),
		Source:   r.deviceInfo.ConnectionString(),
	}, nil
}

func (r *Reader) CancelWrite() {
	r.writeMutex.Lock()
	defer r.writeMutex.Unlock()

	if r.writeCancel != nil {
		log.Debug().Msg("cancelling ongoing write operation")
		r.writeCancel()
	}
}

func (r *Reader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityWrite}
}

func (r *Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}
