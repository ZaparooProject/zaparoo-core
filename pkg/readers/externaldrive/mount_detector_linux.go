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

//go:build linux

package externaldrive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
)

const (
	udisks2Service        = "org.freedesktop.UDisks2"
	udisks2Path           = "/org/freedesktop/UDisks2"
	udisks2BlockInterface = "org.freedesktop.UDisks2.Block"
	udisks2FSInterface    = "org.freedesktop.UDisks2.Filesystem"
	dbusObjectManager     = "org.freedesktop.DBus.ObjectManager"
)

// linuxMountDetector implements MountDetector for Linux using D-Bus/UDisks2.
type linuxMountDetector struct {
	conn         *dbus.Conn
	events       chan MountEvent
	unmounts     chan string
	stopChan     chan struct{}
	mountedDevs  map[string]MountEvent
	pathMappings map[dbus.ObjectPath]string // objectPath -> deviceID mapping for reliable unmount detection
	wg           sync.WaitGroup
	mu           sync.RWMutex
	stopOnce     sync.Once
}

// isDBusAvailable quickly checks if D-Bus is available on the system.
func isDBusAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan bool, 1)
	go func() {
		conn, err := dbus.SystemBus()
		if err != nil {
			done <- false
			return
		}
		_ = conn.Close()
		done <- true
	}()

	select {
	case available := <-done:
		return available
	case <-ctx.Done():
		return false
	}
}

// NewMountDetector creates a new Linux mount detector.
// It tries D-Bus/UDisks2 first, and falls back to inotify if D-Bus is unavailable.
func NewMountDetector() (MountDetector, error) {
	// Try D-Bus first (preferred method for full Linux systems)
	if isDBusAvailable() {
		log.Debug().Msg("Using D-Bus/UDisks2 for mount detection")
		return &linuxMountDetector{
			events:       make(chan MountEvent, 10),
			unmounts:     make(chan string, 10),
			stopChan:     make(chan struct{}),
			mountedDevs:  make(map[string]MountEvent),
			pathMappings: make(map[dbus.ObjectPath]string),
		}, nil
	}

	// Fall back to inotify-based detection (for minimal Linux systems)
	log.Debug().Msg("D-Bus unavailable, using inotify fallback for mount detection")
	return newLinuxMountDetectorFallback()
}

func (d *linuxMountDetector) Events() <-chan MountEvent {
	return d.events
}

func (d *linuxMountDetector) Unmounts() <-chan string {
	return d.unmounts
}

func (d *linuxMountDetector) Start() error {
	// Connect to system D-Bus
	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system D-Bus: %w", err)
	}
	d.conn = conn

	// Subscribe to UDisks2 signals
	if err := d.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(udisks2Path),
		dbus.WithMatchInterface(dbusObjectManager),
		dbus.WithMatchMember("InterfacesAdded"),
	); err != nil {
		_ = d.conn.Close()
		return fmt.Errorf("failed to add match for InterfacesAdded: %w", err)
	}

	if err := d.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(udisks2Path),
		dbus.WithMatchInterface(dbusObjectManager),
		dbus.WithMatchMember("InterfacesRemoved"),
	); err != nil {
		_ = d.conn.Close()
		return fmt.Errorf("failed to add match for InterfacesRemoved: %w", err)
	}

	// Create signal channel
	signalChan := make(chan *dbus.Signal, 10)
	d.conn.Signal(signalChan)

	// Start listening goroutine
	d.wg.Add(1)
	go d.listenForSignals(signalChan)

	return nil
}

func (d *linuxMountDetector) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopChan)
		d.wg.Wait()
		if d.conn != nil {
			_ = d.conn.Close()
		}
		close(d.events)
		close(d.unmounts)
	})
}

func (d *linuxMountDetector) listenForSignals(signalChan chan *dbus.Signal) {
	defer d.wg.Done()

	for {
		select {
		case <-d.stopChan:
			return
		case signal := <-signalChan:
			if signal == nil {
				return
			}

			switch signal.Name {
			case dbusObjectManager + ".InterfacesAdded":
				d.handleInterfacesAdded(signal)
			case dbusObjectManager + ".InterfacesRemoved":
				d.handleInterfacesRemoved(signal)
			}
		}
	}
}

func (d *linuxMountDetector) handleInterfacesAdded(signal *dbus.Signal) {
	if len(signal.Body) < 2 {
		return
	}

	objectPath, ok := signal.Body[0].(dbus.ObjectPath)
	if !ok {
		return
	}

	interfaces, ok := signal.Body[1].(map[string]map[string]dbus.Variant)
	if !ok {
		return
	}

	// Check if this is a filesystem device
	blockProps, hasBlock := interfaces[udisks2BlockInterface]
	_, hasFS := interfaces[udisks2FSInterface]

	if !hasBlock || !hasFS {
		return
	}

	// Check if device is removable (not a system device)
	if hintSystem, ok := blockProps["HintSystem"]; ok {
		if isSystem, ok := hintSystem.Value().(bool); ok && isSystem {
			return
		}
	}

	if hintIgnore, ok := blockProps["HintIgnore"]; ok {
		if shouldIgnore, ok := hintIgnore.Value().(bool); ok && shouldIgnore {
			return
		}
	}

	// Extract mount points
	mountPoints := d.getMountPoints(string(objectPath))
	if len(mountPoints) == 0 {
		return
	}

	// Extract device information
	deviceID := d.getDeviceID(blockProps)
	if deviceID == "" {
		log.Debug().Str("path", string(objectPath)).Msg("Device has no ID, skipping")
		return
	}

	volumeLabel := d.getVolumeLabel(blockProps)
	deviceType := d.getDeviceType(blockProps)

	// Create mount event
	event := MountEvent{
		DeviceID:    deviceID,
		MountPath:   mountPoints[0], // Use first mount point
		VolumeLabel: volumeLabel,
		DeviceType:  deviceType,
	}

	// Store and emit event
	d.mu.Lock()
	d.mountedDevs[deviceID] = event
	d.pathMappings[objectPath] = deviceID
	d.mu.Unlock()

	select {
	case d.events <- event:
		log.Debug().
			Str("device_id", deviceID).
			Str("mount_path", event.MountPath).
			Str("label", volumeLabel).
			Msg("Mount event detected")
	case <-d.stopChan:
		return
	}
}

func (d *linuxMountDetector) handleInterfacesRemoved(signal *dbus.Signal) {
	if len(signal.Body) < 2 {
		return
	}

	objectPath, ok := signal.Body[0].(dbus.ObjectPath)
	if !ok {
		return
	}

	interfaces, ok := signal.Body[1].([]string)
	if !ok {
		return
	}

	// Check if filesystem interface was removed
	hasFS := false
	for _, iface := range interfaces {
		if iface == udisks2FSInterface {
			hasFS = true
			break
		}
	}

	if !hasFS {
		return
	}

	// Use deterministic mapping to find device by object path
	d.mu.Lock()
	deviceID, exists := d.pathMappings[objectPath]
	if exists {
		delete(d.mountedDevs, deviceID)
		delete(d.pathMappings, objectPath)
	}
	d.mu.Unlock()

	if exists {
		select {
		case d.unmounts <- deviceID:
			log.Debug().
				Str("device_id", deviceID).
				Msg("Unmount event detected")
		case <-d.stopChan:
			return
		}
	}
}

func (d *linuxMountDetector) getMountPoints(objectPath string) []string {
	obj := d.conn.Object(udisks2Service, dbus.ObjectPath(objectPath))
	var mountPoints [][]byte

	if err := obj.Call(udisks2FSInterface+".GetMountPoints", 0).Store(&mountPoints); err != nil {
		return nil
	}

	result := make([]string, 0, len(mountPoints))
	for _, mp := range mountPoints {
		if len(mp) > 0 {
			// Remove trailing null byte if present
			path := string(mp)
			path = strings.TrimRight(path, "\x00")
			result = append(result, path)
		}
	}

	return result
}

func (*linuxMountDetector) getDeviceID(props map[string]dbus.Variant) string {
	// Try UUID first (most stable)
	if idUUID, ok := props["IdUUID"]; ok {
		if uuid, ok := idUUID.Value().(string); ok && uuid != "" {
			return uuid
		}
	}

	// Fall back to serial number
	if serial, ok := props["IdSerial"]; ok {
		if serialNum, ok := serial.Value().(string); ok && serialNum != "" {
			return serialNum
		}
	}

	// Last resort: device name
	if device, ok := props["Device"]; ok {
		if devicePath, ok := device.Value().([]byte); ok && len(devicePath) > 0 {
			return string(devicePath)
		}
	}

	return ""
}

func (*linuxMountDetector) getVolumeLabel(props map[string]dbus.Variant) string {
	if idLabel, ok := props["IdLabel"]; ok {
		if label, ok := idLabel.Value().(string); ok {
			return label
		}
	}
	return ""
}

func (*linuxMountDetector) getDeviceType(props map[string]dbus.Variant) string {
	// Try to determine device type from connection bus
	if connectionBus, ok := props["ConnectionBus"]; ok {
		if bus, ok := connectionBus.Value().(string); ok {
			switch bus {
			case "usb":
				return "USB"
			case "sdio":
				return "SD"
			default:
				return "removable"
			}
		}
	}

	// Check if it's a removable drive
	if removable, ok := props["Removable"]; ok {
		if isRemovable, ok := removable.Value().(bool); ok && isRemovable {
			return "removable"
		}
	}

	return "unknown"
}

// linuxMountDetectorFallback implements MountDetector for Linux using inotify (fsnotify).
// This is used when D-Bus/UDisks2 is not available (minimal Linux systems).
type linuxMountDetectorFallback struct {
	watcher     *fsnotify.Watcher
	events      chan MountEvent
	unmounts    chan string
	stopChan    chan struct{}
	mountedDevs map[string]MountEvent
	watchDirs   []string
	wg          sync.WaitGroup
	mu          sync.RWMutex
	stopOnce    sync.Once
}

// newLinuxMountDetectorFallback creates a new inotify-based mount detector for Linux.
func newLinuxMountDetectorFallback() (MountDetector, error) {
	// Determine which directories to watch based on what exists
	var watchDirs []string

	// Try user-specific media directories first
	mediaDir := "/media"
	runMediaDir := "/run/media"

	if username := os.Getenv("USER"); username != "" {
		userMedia := filepath.Join(mediaDir, username)
		if _, err := os.Stat(userMedia); err == nil {
			watchDirs = append(watchDirs, userMedia)
		}

		userRunMedia := filepath.Join(runMediaDir, username)
		if _, err := os.Stat(userRunMedia); err == nil {
			watchDirs = append(watchDirs, userRunMedia)
		}
	}

	// Add common mount directories if they exist
	commonDirs := []string{"/media", "/mnt"}
	for _, dir := range commonDirs {
		if _, err := os.Stat(dir); err == nil {
			// Only add if not already in watchDirs
			found := false
			for _, existing := range watchDirs {
				if existing == dir {
					found = true
					break
				}
			}
			if !found {
				watchDirs = append(watchDirs, dir)
			}
		}
	}

	if len(watchDirs) == 0 {
		return nil, errors.New("no suitable mount directories found to watch")
	}

	return &linuxMountDetectorFallback{
		events:      make(chan MountEvent, 10),
		unmounts:    make(chan string, 10),
		stopChan:    make(chan struct{}),
		mountedDevs: make(map[string]MountEvent),
		watchDirs:   watchDirs,
	}, nil
}

func (d *linuxMountDetectorFallback) Events() <-chan MountEvent {
	return d.events
}

func (d *linuxMountDetectorFallback) Unmounts() <-chan string {
	return d.unmounts
}

func (d *linuxMountDetectorFallback) Start() error {
	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	d.watcher = watcher

	// Watch all mount directories
	for _, dir := range d.watchDirs {
		if err := d.watcher.Add(dir); err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("Failed to watch directory")
			continue
		}
		log.Debug().Str("dir", dir).Msg("Watching directory for mount events")
	}

	// Start event loop
	d.wg.Add(1)
	go d.watchFileSystemEvents()

	return nil
}

func (d *linuxMountDetectorFallback) Stop() {
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

func (d *linuxMountDetectorFallback) watchFileSystemEvents() {
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

			// Only process events in watched directories (not subdirectories)
			parentDir := filepath.Dir(event.Name)
			isWatchedDir := false
			for _, dir := range d.watchDirs {
				if parentDir == dir {
					isWatchedDir = true
					break
				}
			}

			if !isWatchedDir {
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
				d.checkMount(path)
			}
			pendingChecks = make(map[string]bool)
		}
	}
}

func (d *linuxMountDetectorFallback) checkMount(mountPath string) {
	// Check if mount exists
	info, err := os.Stat(mountPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Mount was removed
			d.handleMountRemoval(mountPath)
		}
		return
	}

	// Only process directories
	if !info.IsDir() {
		return
	}

	// Get device information from /proc/mounts
	deviceID, deviceType := d.getMountInfo(mountPath)
	if deviceID == "" {
		// Not a removable device or couldn't get info
		return
	}

	// Check if already mounted
	d.mu.RLock()
	_, exists := d.mountedDevs[deviceID]
	d.mu.RUnlock()

	if exists {
		return
	}

	// New mount detected
	volumeLabel := filepath.Base(mountPath)
	event := MountEvent{
		DeviceID:    deviceID,
		MountPath:   mountPath,
		VolumeLabel: volumeLabel,
		DeviceType:  deviceType,
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
			Msg("Mount detected (inotify)")
	case <-d.stopChan:
		return
	}
}

func (d *linuxMountDetectorFallback) handleMountRemoval(mountPath string) {
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
				Msg("Unmount detected (inotify)")
		case <-d.stopChan:
			return
		}
	}
}

// getMountInfo extracts device information from /proc/mounts for a given mount path.
// Returns deviceID and deviceType, or empty strings if not a removable device.
func (d *linuxMountDetectorFallback) getMountInfo(mountPath string) (deviceID, deviceType string) {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return "", ""
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		device := fields[0]
		mount := fields[1]
		fstype := fields[2]

		// Check if this is our mount path
		if mount != mountPath {
			continue
		}

		// Skip system filesystems
		systemFSTypes := []string{
			"sysfs", "proc", "devtmpfs", "devpts", "tmpfs", "cgroup",
			"cgroup2", "pstore", "bpf", "configfs", "selinuxfs", "debugfs",
			"tracefs", "fusectl", "fuse.portal", "mqueue", "hugetlbfs",
			"autofs", "efivarfs", "binfmt_misc", "overlay",
		}
		isSystemFS := false
		for _, sysFS := range systemFSTypes {
			if fstype == sysFS {
				isSystemFS = true
				break
			}
		}
		if isSystemFS {
			continue
		}

		// Skip non-device mounts (network, etc)
		if !strings.HasPrefix(device, "/dev/") {
			continue
		}

		// Try to get UUID from /dev/disk/by-uuid/
		uuid := d.getDeviceUUID(device)
		if uuid != "" {
			deviceID = uuid
		} else {
			// Fall back to device name
			deviceID = device
		}

		// Determine device type based on filesystem
		removableFSTypes := []string{"vfat", "exfat", "ntfs", "ext2", "ext3", "ext4", "hfs", "hfsplus"}
		for _, rmFS := range removableFSTypes {
			if strings.HasPrefix(fstype, rmFS) {
				deviceType = "removable"
				break
			}
		}

		if deviceType == "" {
			// If we can't determine type, assume removable for mounts in watched directories
			deviceType = "removable"
		}

		return deviceID, deviceType
	}

	return "", ""
}

// getDeviceUUID attempts to find the UUID for a device by checking /dev/disk/by-uuid/.
func (*linuxMountDetectorFallback) getDeviceUUID(device string) string {
	byUUIDPath := "/dev/disk/by-uuid"
	entries, err := os.ReadDir(byUUIDPath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		linkPath := filepath.Join(byUUIDPath, entry.Name())
		target, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			continue
		}

		// Check if this symlink points to our device
		if target == device {
			return entry.Name()
		}
	}

	return ""
}
