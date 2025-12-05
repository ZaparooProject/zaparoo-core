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

//go:build darwin

package externaldrive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

const (
	volumesPath = "/Volumes"
	// macOS flags for removable/ejectable media
	mntLocal      = 0x00001000 // Local filesystem
	mntDoVolFs    = 0x00008000 // VFS handles this
	mntDontBrowse = 0x00100000 // Don't show in GUI
)

// darwinMountDetector implements MountDetector for macOS using FSEvents.
type darwinMountDetector struct {
	watcher     *fsnotify.Watcher
	events      chan MountEvent
	unmounts    chan string
	stopChan    chan struct{}
	mountedDevs map[string]MountEvent
	wg          sync.WaitGroup
	mu          syncutil.RWMutex
	stopOnce    sync.Once
}

// NewMountDetector creates a new macOS mount detector using FSEvents.
func NewMountDetector() (MountDetector, error) {
	return &darwinMountDetector{
		events:      make(chan MountEvent, 10),
		unmounts:    make(chan string, 10),
		stopChan:    make(chan struct{}),
		mountedDevs: make(map[string]MountEvent),
	}, nil
}

func (d *darwinMountDetector) Events() <-chan MountEvent {
	return d.events
}

func (d *darwinMountDetector) Unmounts() <-chan string {
	return d.unmounts
}

func (d *darwinMountDetector) Start() error {
	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	d.watcher = watcher

	// Watch /Volumes directory
	if err := d.watcher.Add(volumesPath); err != nil {
		_ = d.watcher.Close()
		return fmt.Errorf("failed to watch /Volumes: %w", err)
	}

	// Start event loop
	d.wg.Add(1)
	go d.watchFileSystemEvents()

	log.Debug().Msg("started watching macOS /Volumes for mount events")

	return nil
}

func (d *darwinMountDetector) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopChan)
		if d.watcher != nil {
			_ = d.watcher.Close()
		}
		d.wg.Wait()
		close(d.events)
		close(d.unmounts)
	})
}

func (d *darwinMountDetector) watchFileSystemEvents() {
	defer d.wg.Done()

	// Debounce timer to handle rapid events
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	pendingChecks := make(map[string]bool)

	for {
		select {
		case <-d.stopChan:
			debounceTimer.Stop()
			return

		case event, ok := <-d.watcher.Events:
			if !ok {
				return
			}

			// Check if event is in /Volumes (not a subdirectory)
			if filepath.Dir(event.Name) != volumesPath {
				continue
			}

			// Mark for debounced check
			pendingChecks[event.Name] = true
			debounceTimer.Reset(100 * time.Millisecond)

		case err, ok := <-d.watcher.Errors:
			if !ok {
				return
			}
			log.Warn().Err(err).Msg("fsnotify error")

		case <-debounceTimer.C:
			// Process pending checks
			for path := range pendingChecks {
				d.checkVolume(path)
			}
			pendingChecks = make(map[string]bool)
		}
	}
}

func (d *darwinMountDetector) checkVolume(mountPath string) {
	// Check if volume exists (mounted)
	info, err := os.Stat(mountPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Volume was unmounted
			d.handleVolumeUnmount(mountPath)
		}
		return
	}

	// Only process directories
	if !info.IsDir() {
		return
	}

	// Skip Macintosh HD and other system volumes
	volumeName := filepath.Base(mountPath)
	if d.isSystemVolume(volumeName) {
		return
	}

	// Check if this is a removable volume
	if !d.isRemovableVolume(mountPath) {
		return
	}

	// Get volume information
	deviceID, volumeLabel := d.getVolumeInfo(mountPath)
	if deviceID == "" {
		// Use mount path as fallback
		deviceID = volumeName
	}

	// Check if already mounted
	d.mu.RLock()
	_, exists := d.mountedDevs[deviceID]
	d.mu.RUnlock()

	if exists {
		return
	}

	// New mount detected
	event := MountEvent{
		DeviceID:    deviceID,
		MountPath:   mountPath,
		VolumeLabel: volumeLabel,
		DeviceType:  "removable",
	}

	d.mu.Lock()
	d.mountedDevs[deviceID] = event
	d.mu.Unlock()

	select {
	case d.events <- event:
		log.Debug().
			Str("device_id", deviceID).
			Str("mount_path", mountPath).
			Str("label", volumeLabel).
			Msg("volume mount detected")
	case <-d.stopChan:
		return
	}
}

func (d *darwinMountDetector) handleVolumeUnmount(mountPath string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Find device by mount path
	var foundID string
	for id, event := range d.mountedDevs {
		if event.MountPath == mountPath {
			foundID = id
			break
		}
	}

	if foundID != "" {
		delete(d.mountedDevs, foundID)

		select {
		case d.unmounts <- foundID:
			log.Debug().
				Str("device_id", foundID).
				Str("mount_path", mountPath).
				Msg("volume unmount detected")
		case <-d.stopChan:
			return
		}
	}
}

func (*darwinMountDetector) isSystemVolume(volumeName string) bool {
	// Common macOS system volumes to ignore
	systemVolumes := []string{
		"Macintosh HD",
		"Preboot",
		"Recovery",
		"VM",
		"Data",
		"System",
		"Update",
	}

	for _, sysVol := range systemVolumes {
		if volumeName == sysVol || strings.HasPrefix(volumeName, sysVol+" ") {
			return true
		}
	}

	return false
}

func (*darwinMountDetector) isRemovableVolume(mountPath string) bool {
	// Use statfs to get mount flags
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPath, &stat); err != nil {
		return false
	}

	// Check if it's a local filesystem
	if stat.Flags&mntLocal == 0 {
		return false
	}

	// Check filesystem type - removable drives are typically msdos, exfat, or hfs
	// Convert []int8 to string
	fstypeBytes := make([]byte, len(stat.Fstypename))
	for i, b := range stat.Fstypename {
		fstypeBytes[i] = byte(b)
	}
	fstype := string(fstypeBytes)
	fstype = strings.TrimRight(fstype, "\x00")

	removableFSTypes := []string{"msdos", "exfat", "hfs", "apfs"}
	isRemovableFS := false
	for _, t := range removableFSTypes {
		if fstype == t {
			isRemovableFS = true
			break
		}
	}

	if !isRemovableFS {
		return false
	}

	// Additional heuristic: check if MNT_DONTBROWSE is NOT set
	// (system volumes often have this flag)
	if stat.Flags&mntDontBrowse != 0 {
		return false
	}

	return true
}

func (*darwinMountDetector) getVolumeInfo(mountPath string) (deviceID, volumeLabel string) {
	// Get filesystem stats for device ID
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPath, &stat); err != nil {
		return "", ""
	}

	// Use filesystem ID as device ID (more stable than mount point)
	// Combine fsid fields to create unique ID
	deviceID = fmt.Sprintf("%x-%x", stat.Fsid.Val[0], stat.Fsid.Val[1])

	// Volume label is the mount point name on macOS
	volumeLabel = filepath.Base(mountPath)

	return deviceID, volumeLabel
}
