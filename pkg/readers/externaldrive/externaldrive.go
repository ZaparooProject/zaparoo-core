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

package externaldrive

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

const (
	DriverID  = "externaldrive"
	TokenType = "externaldrive"
	// Maximum file size for zaparoo.txt (1MB) to prevent memory exhaustion
	maxFileSize = 1 * 1024 * 1024
	// Name of the file to look for on mounted devices
	//nolint:gosec // Not a credential, just a filename
	tokenFileName = "zaparoo.txt"
)

var (
	// platformSupported caches whether this platform supports mount detection
	platformSupported     bool
	platformSupportedOnce sync.Once
)

// Reader implements the readers.Reader interface for external drive devices.
type Reader struct {
	detector     MountDetector
	cfg          *config.Instance
	scanChan     chan<- readers.Scan
	stopChan     chan struct{}
	activeTokens map[string]*tokens.Token
	device       config.ReadersConnect
	wg           sync.WaitGroup
	mu           syncutil.RWMutex
}

// NewReader creates a new external drive reader instance.
func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg:          cfg,
		activeTokens: make(map[string]*tokens.Token),
	}
}

func (*Reader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                "externaldrive",
		DefaultEnabled:    false, // Opt-in only
		DefaultAutoDetect: true,  // Automatically detects mounted devices
		Description:       "External drive reader (USB sticks, SD cards, external HDDs)",
	}
}

func (*Reader) IDs() []string {
	return []string{"externaldrive", "external_drive"}
}

func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	// Create platform-specific detector
	detector, err := NewMountDetector()
	if err != nil {
		return fmt.Errorf("failed to create mount detector: %w", err)
	}

	r.detector = detector
	r.device = device
	r.scanChan = iq
	r.stopChan = make(chan struct{})

	// Start the detector
	if err := r.detector.Start(); err != nil {
		return fmt.Errorf("failed to start mount detector: %w", err)
	}

	// Start event processing goroutine
	r.wg.Add(1)
	go r.processEvents()

	log.Info().Str("driver", device.Driver).Msg("external drive reader started")

	return nil
}

func (r *Reader) Close() error {
	// Check if Open() was called (stopChan is only initialized in Open)
	if r.stopChan != nil {
		close(r.stopChan)
		r.wg.Wait()

		if r.detector != nil {
			r.detector.Stop()
		}

		log.Info().Msg("external drive reader stopped")
	}

	return nil
}

func (r *Reader) processEvents() {
	defer r.wg.Done()

	for {
		select {
		case <-r.stopChan:
			return

		case event, ok := <-r.detector.Events():
			if !ok {
				return
			}

			// Process mount event in separate goroutine to avoid blocking
			r.wg.Add(1)
			go r.handleMountEvent(event)

		case deviceID, ok := <-r.detector.Unmounts():
			if !ok {
				return
			}

			r.handleUnmountEvent(deviceID)
		}
	}
}

func (r *Reader) handleMountEvent(event MountEvent) {
	defer r.wg.Done()

	// Create context with timeout for file operations
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check if device was unmounted while we were processing
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-r.stopChan:
			cancel()
		case <-done:
		}
	}()
	defer close(done)

	// Look for zaparoo.txt in the mount path
	tokenPath := filepath.Join(event.MountPath, tokenFileName)

	// Security: Check for symlinks
	fileInfo, err := os.Lstat(tokenPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Debug().
				Err(err).
				Str("device_id", event.DeviceID).
				Str("path", tokenPath).
				Msg("failed to stat token file")
		}
		return
	}

	// Security: Reject symlinks to prevent path traversal attacks
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		log.Warn().
			Str("device_id", event.DeviceID).
			Str("path", tokenPath).
			Msg("rejecting symlink zaparoo.txt for security")
		return
	}

	// Security: Check file size limit
	if fileInfo.Size() > maxFileSize {
		log.Warn().
			Str("device_id", event.DeviceID).
			Int64("size", fileInfo.Size()).
			Msg("token file exceeds maximum size limit")
		return
	}

	// Read the file with a short delay to ensure filesystem is ready
	// (some systems fire mount events before filesystem is fully accessible)
	time.Sleep(100 * time.Millisecond)

	//nolint:gosec // Safe: We've validated path is not a symlink and file size
	contents, err := os.ReadFile(tokenPath)
	if err != nil {
		log.Debug().
			Err(err).
			Str("device_id", event.DeviceID).
			Str("path", tokenPath).
			Msg("failed to read token file")
		return
	}

	text := strings.TrimSpace(string(contents))
	if text == "" {
		log.Debug().
			Str("device_id", event.DeviceID).
			Msg("token file is empty")
		return
	}

	// Create token
	token := &tokens.Token{
		Type:     TokenType,
		Text:     text,
		Data:     hex.EncodeToString(contents),
		ScanTime: time.Now(),
		Source:   r.device.ConnectionString(),
	}

	// Before adding token, verify device is still mounted (race condition protection)
	if _, err := os.Stat(event.MountPath); err != nil {
		// Device was unmounted while we were reading the file
		log.Debug().
			Str("device_id", event.DeviceID).
			Str("mount_path", event.MountPath).
			Msg("device unmounted during token read, discarding")
		return
	}

	// Store active token
	r.mu.Lock()
	r.activeTokens[event.DeviceID] = token
	r.mu.Unlock()

	// Emit scan event
	select {
	case r.scanChan <- readers.Scan{
		Source: r.device.ConnectionString(),
		Token:  token,
	}:
		log.Info().
			Str("device_id", event.DeviceID).
			Str("label", event.VolumeLabel).
			Str("mount_path", event.MountPath).
			Msg("external drive token detected")
	case <-r.stopChan:
		return
	}
}

func (r *Reader) handleUnmountEvent(deviceID string) {
	r.mu.Lock()
	_, exists := r.activeTokens[deviceID]
	if exists {
		delete(r.activeTokens, deviceID)
	}
	r.mu.Unlock()

	if !exists {
		return
	}

	// Emit removal scan
	select {
	case r.scanChan <- readers.Scan{
		Source: r.device.ConnectionString(),
		Token:  nil,
	}:
		log.Info().
			Str("device_id", deviceID).
			Msg("external drive token removed")
	case <-r.stopChan:
		return
	}
}

func (*Reader) Detect(_ []string) string {
	// Check platform support once and cache the result
	platformSupportedOnce.Do(func() {
		// Try to create a detector to verify platform support
		detector, err := NewMountDetector()
		if err != nil {
			platformSupported = false
			return
		}
		// Clean up immediately - we just wanted to verify it works
		detector.Stop()
		platformSupported = true
	})

	if !platformSupported {
		return ""
	}

	// Return driver:path format (empty path since we monitor all mounts)
	return DriverID + ":"
}

func (r *Reader) Device() string {
	return r.device.ConnectionString()
}

func (r *Reader) Connected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.detector != nil
}

func (r *Reader) Info() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return fmt.Sprintf("External Drive Reader (%d active devices)", len(r.activeTokens))
}

func (*Reader) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (*Reader) CancelWrite() {
	// no-op, writing not supported
}

func (*Reader) Capabilities() []readers.Capability {
	return []readers.Capability{}
}

func (*Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}
