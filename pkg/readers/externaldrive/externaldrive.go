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

package externaldrive

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
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
	// Maximum retries for reading token file (handles I/O errors during device transition)
	maxReadRetries = 3
	// Initial delay before first retry (doubles each retry: 500ms, 1s, 2s)
	initialRetryDelay = 500 * time.Millisecond
)

var (
	// platformSupported caches whether this platform supports mount detection
	platformSupported     bool
	platformSupportedOnce sync.Once

	// partitionSuffixRegex matches partition numbers at the end of device paths
	// e.g., /dev/sda1 -> /dev/sda, /dev/nvme0n1p2 -> /dev/nvme0n1
	partitionSuffixRegex = regexp.MustCompile(`(p?\d+)$`)
)

// sysBlockPath is the sysfs path for block devices.
const sysBlockPath = "/sys/block"

// isBlockDevicePresent checks if the underlying block device still exists.
// For device paths like "/dev/sda1", it checks if "/sys/block/sda" exists.
// Returns true if device appears present, false if definitely gone.
// Returns true for empty strings (assume present if we can't check).
func isBlockDevicePresent(deviceNode string) bool {
	// Only check /dev/sdX style device nodes
	if !strings.HasPrefix(deviceNode, "/dev/") {
		return true // Can't validate non-path device nodes, assume present
	}

	// Extract base device: /dev/sda1 -> sda, /dev/nvme0n1p1 -> nvme0n1
	devName := strings.TrimPrefix(deviceNode, "/dev/")
	baseDev := partitionSuffixRegex.ReplaceAllString(devName, "")

	// Check if block device exists in sysfs
	sysPath := filepath.Join(sysBlockPath, baseDev)
	_, err := os.Stat(sysPath)
	return err == nil
}

// canSafelyUnmount checks if a stale mount can be safely unmounted.
// Returns true only if ALL conditions are met:
//  1. Mount path is in a removable media location (/media/, /mnt/, /run/media/)
//  2. Device node looks like removable media (/dev/sd*, /dev/mmcblk*)
//  3. Device node no longer exists (physically unplugged)
//  4. Sysfs entry no longer exists (kernel confirms device removed)
func canSafelyUnmount(deviceNode, mountPath string) bool {
	if !strings.HasPrefix(mountPath, "/media/") &&
		!strings.HasPrefix(mountPath, "/mnt/") &&
		!strings.HasPrefix(mountPath, "/run/media/") {
		return false
	}

	if !strings.HasPrefix(deviceNode, "/dev/sd") &&
		!strings.HasPrefix(deviceNode, "/dev/mmcblk") {
		return false
	}

	if _, err := os.Stat(deviceNode); err == nil {
		return false
	}

	if isBlockDevicePresent(deviceNode) {
		return false
	}

	return true
}

// Reader implements the readers.Reader interface for external drive devices.
type Reader struct {
	detector     MountDetector
	cfg          *config.Instance
	cmdExecutor  command.Executor
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
		cmdExecutor:  &command.RealExecutor{},
		activeTokens: make(map[string]*tokens.Token),
	}
}

// attemptStaleUnmount tries to clean up a stale mount point using lazy unmount.
// Only unmounts if canSafelyUnmount returns true.
func (r *Reader) attemptStaleUnmount(deviceNode, mountPath string) {
	if !canSafelyUnmount(deviceNode, mountPath) {
		log.Debug().
			Str("device_node", deviceNode).
			Str("mount_path", mountPath).
			Msg("mount does not meet safety criteria for unmount")
		return
	}

	log.Info().
		Str("device_node", deviceNode).
		Str("mount_path", mountPath).
		Msg("attempting lazy unmount of stale mount point")

	// Lazy unmount (-l) detaches the filesystem immediately but defers
	// cleanup until it's no longer in use.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := r.cmdExecutor.Run(ctx, "umount", "-l", mountPath)
	if err != nil {
		log.Warn().
			Err(err).
			Str("device_node", deviceNode).
			Str("mount_path", mountPath).
			Msg("failed to unmount stale mount point")
		return
	}

	log.Info().
		Str("device_node", deviceNode).
		Str("mount_path", mountPath).
		Msg("successfully unmounted stale mount point")
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
			// Create a copy of the event for the goroutine to avoid data races
			eventCopy := event
			r.wg.Add(1)
			go r.handleMountEvent(&eventCopy)

		case deviceID, ok := <-r.detector.Unmounts():
			if !ok {
				return
			}

			r.handleUnmountEvent(deviceID)
		}
	}
}

// getBaseDevice extracts the base device path from a partition device path.
// For example: /dev/sda1 -> /dev/sda, /dev/nvme0n1p2 -> /dev/nvme0n1
// Returns the original string if it doesn't look like a partition path.
func getBaseDevice(deviceID string) string {
	if !strings.HasPrefix(deviceID, "/dev/") {
		return deviceID
	}
	return partitionSuffixRegex.ReplaceAllString(deviceID, "")
}

// findTokenFileInDir searches for zaparoo.txt (case-insensitive) in the given directory.
// Returns the full path if found, or empty string if not found.
func findTokenFileInDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), tokenFileName) {
			return filepath.Join(dir, entry.Name())
		}
	}
	return ""
}

// findTokenFile searches for zaparoo.txt (case-insensitive) on the given mount path
// and any sibling partitions (other partitions of the same physical device).
// Returns the path where the token file was found, or empty string if not found.
func findTokenFile(primaryMount string) (tokenPath, foundMount string) {
	// Build list of mount paths to check, starting with primary
	mountsToCheck := []string{primaryMount}

	// Always search sibling mounts in /media/ and /mnt/ for removable devices.
	// Multi-partition USB drives (like Ventoy) may have zaparoo.txt on a different
	// partition than the one that triggered the mount event. Device IDs can be
	// UUIDs or /dev/ paths, so we search all siblings regardless of format.
	for _, mediaDir := range []string{"/media", "/mnt"} {
		entries, err := os.ReadDir(mediaDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			mountPath := filepath.Join(mediaDir, entry.Name())
			if mountPath != primaryMount {
				mountsToCheck = append(mountsToCheck, mountPath)
			}
		}
	}

	// Check each mount path for zaparoo.txt (case-insensitive)
	for _, mountPath := range mountsToCheck {
		tokenPath := findTokenFileInDir(mountPath)
		if tokenPath == "" {
			continue
		}

		fileInfo, err := os.Lstat(tokenPath)
		if err != nil {
			continue
		}

		// Skip symlinks for security
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			log.Warn().
				Str("path", tokenPath).
				Msg("skipping symlink zaparoo.txt for security")
			continue
		}

		// Skip files that are too large
		if fileInfo.Size() > maxFileSize {
			log.Warn().
				Str("path", tokenPath).
				Int64("size", fileInfo.Size()).
				Msg("skipping token file - exceeds maximum size")
			continue
		}

		return tokenPath, mountPath
	}

	return "", ""
}

func (r *Reader) handleMountEvent(event *MountEvent) {
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

	log.Debug().
		Str("device_id", event.DeviceID).
		Str("device_node", event.DeviceNode).
		Str("mount_path", event.MountPath).
		Str("base_device", getBaseDevice(event.DeviceID)).
		Msg("checking for token file on device")

	// Validate block device is still present (detect stale mounts from yanked USB)
	// Only check if we have a device node - some mounts may not have one
	if event.DeviceNode != "" && !isBlockDevicePresent(event.DeviceNode) {
		log.Warn().
			Str("device_id", event.DeviceID).
			Str("device_node", event.DeviceNode).
			Str("mount_path", event.MountPath).
			Msg("stale mount detected - block device no longer exists, clearing from tracking")
		r.detector.Forget(event.DeviceID)
		r.attemptStaleUnmount(event.DeviceNode, event.MountPath)
		return
	}

	// Find token file on this mount or sibling partitions
	tokenPath, foundMount := findTokenFile(event.MountPath)
	if tokenPath == "" {
		log.Debug().
			Str("device_id", event.DeviceID).
			Str("mount_path", event.MountPath).
			Msg("no token file found on device or sibling partitions")
		return
	}

	if foundMount != event.MountPath {
		log.Debug().
			Str("device_id", event.DeviceID).
			Str("primary_mount", event.MountPath).
			Str("found_on", foundMount).
			Msg("token file found on sibling partition")
	}

	// Read the file with retries (handles I/O errors during device transition)
	// Initial delay ensures filesystem is ready
	time.Sleep(100 * time.Millisecond)

	var contents []byte
	var readErr error

	for attempt := range maxReadRetries {
		// Check for cancellation before each attempt
		select {
		case <-r.stopChan:
			return
		case <-ctx.Done():
			return
		default:
		}

		//nolint:gosec // Safe: We've validated path is not a symlink and file size
		contents, readErr = os.ReadFile(tokenPath)
		if readErr == nil {
			break
		}

		// If this isn't the last attempt, wait before retrying
		if attempt < maxReadRetries-1 {
			delay := initialRetryDelay << attempt // Exponential backoff: 500ms, 1s, 2s
			log.Debug().
				Err(readErr).
				Str("device_id", event.DeviceID).
				Str("path", tokenPath).
				Int("attempt", attempt+1).
				Dur("retry_delay", delay).
				Msg("token file read failed, retrying")

			select {
			case <-time.After(delay):
			case <-r.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}

	if readErr != nil {
		// Check if this is a stale mount (device was unplugged during retries)
		// Only check if we have a device node - some mounts may not have one
		if event.DeviceNode != "" && !isBlockDevicePresent(event.DeviceNode) {
			log.Warn().
				Str("device_id", event.DeviceID).
				Str("device_node", event.DeviceNode).
				Str("mount_path", event.MountPath).
				Msg("I/O errors due to stale mount - device unplugged, clearing from tracking")
			r.detector.Forget(event.DeviceID)
			r.attemptStaleUnmount(event.DeviceNode, event.MountPath)
		} else {
			log.Warn().
				Err(readErr).
				Str("device_id", event.DeviceID).
				Str("path", tokenPath).
				Int("attempts", maxReadRetries).
				Msg("failed to read token file after retries - device may have issues")
		}
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
		Source:   tokens.SourceReader,
	}

	// Before adding token, verify device is still mounted (race condition protection)
	if _, err := os.Stat(foundMount); err != nil {
		log.Debug().
			Str("device_id", event.DeviceID).
			Str("mount_path", foundMount).
			Msg("device unmounted during token read, discarding")
		return
	}

	// Store active token using base device to prevent duplicates from sibling partitions
	baseDevice := getBaseDevice(event.DeviceID)
	r.mu.Lock()
	if _, exists := r.activeTokens[baseDevice]; exists {
		// Already have a token for this physical device
		r.mu.Unlock()
		log.Debug().
			Str("device_id", event.DeviceID).
			Str("base_device", baseDevice).
			Msg("token already active for this device, skipping")
		return
	}
	r.activeTokens[baseDevice] = token
	r.mu.Unlock()

	// Emit scan event
	select {
	case r.scanChan <- readers.Scan{
		Source: tokens.SourceReader,
		Token:  token,
	}:
		log.Info().
			Str("device_id", event.DeviceID).
			Str("label", event.VolumeLabel).
			Str("mount_path", foundMount).
			Msg("external drive token detected")
	case <-r.stopChan:
		return
	}
}

func (r *Reader) handleUnmountEvent(deviceID string) {
	// Use base device to match how tokens are stored in handleMountEvent
	baseDevice := getBaseDevice(deviceID)

	r.mu.Lock()
	_, exists := r.activeTokens[baseDevice]
	if exists {
		delete(r.activeTokens, baseDevice)
	}
	r.mu.Unlock()

	if !exists {
		return
	}

	// Emit removal scan
	select {
	case r.scanChan <- readers.Scan{
		Source: tokens.SourceReader,
		Token:  nil,
	}:
		log.Info().
			Str("device_id", deviceID).
			Str("base_device", baseDevice).
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

func (r *Reader) Path() string {
	return r.device.Path
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
	return []readers.Capability{readers.CapabilityRemovable}
}

func (*Reader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}

func (r *Reader) ReaderID() string {
	// External drive reader monitors all mounts, use driver ID as stable identifier
	return readers.GenerateReaderID(r.Metadata().ID, DriverID)
}
