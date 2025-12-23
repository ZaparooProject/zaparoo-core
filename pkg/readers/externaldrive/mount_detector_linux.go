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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	udisks2Service        = "org.freedesktop.UDisks2"
	udisks2Path           = "/org/freedesktop/UDisks2"
	udisks2BlockInterface = "org.freedesktop.UDisks2.Block"
	udisks2FSInterface    = "org.freedesktop.UDisks2.Filesystem"
	dbusObjectManager     = "org.freedesktop.DBus.ObjectManager"

	// fallbackRescanInterval is the maximum time between mount rescans.
	// This ensures mounts are detected even when poll() doesn't fire events
	// on some minimal Linux systems (like Batocera).
	fallbackRescanInterval = 1 * time.Second
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
	mu           syncutil.RWMutex
	stopOnce     sync.Once
}

// isDBusAvailable quickly checks if D-Bus and UDisks2 are available on the system.
func isDBusAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan bool, 1)
	go func() {
		// Use SystemBusPrivate to create a new connection that we can safely close
		// without affecting the shared connection used by Start()
		conn, err := dbus.SystemBusPrivate()
		if err != nil {
			done <- false
			return
		}
		defer func() { _ = conn.Close() }()

		// Auth must be called after SystemBusPrivate
		if err := conn.Auth(nil); err != nil {
			done <- false
			return
		}

		// Hello must be called after Auth to complete the connection setup
		if err := conn.Hello(); err != nil {
			done <- false
			return
		}

		// Verify UDisks2 service is actually available
		obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
		call := obj.CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0)
		if call.Err != nil {
			done <- false
			return
		}

		var names []string
		if err := call.Store(&names); err != nil {
			done <- false
			return
		}

		// Check if UDisks2 service is in the list
		for _, name := range names {
			if name == udisks2Service {
				done <- true
				return
			}
		}

		done <- false
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
		log.Debug().Msg("using D-Bus/UDisks2 for mount detection")
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

func (d *linuxMountDetector) Forget(deviceID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Remove from mounted devices
	delete(d.mountedDevs, deviceID)

	// Also remove from path mappings if present
	for path, id := range d.pathMappings {
		if id == deviceID {
			delete(d.pathMappings, path)
			break
		}
	}

	log.Debug().Str("device_id", deviceID).Msg("forgot stale mount from D-Bus tracking")
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
		log.Debug().Str("path", string(objectPath)).Msg("device has no ID, skipping")
		return
	}

	deviceNode := d.getDeviceNode(blockProps)
	volumeLabel := d.getVolumeLabel(blockProps)
	deviceType := d.getDeviceType(blockProps)

	// Create mount event
	event := MountEvent{
		DeviceID:    deviceID,
		DeviceNode:  deviceNode,
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
			Msg("mount event detected")
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
				Msg("unmount event detected")
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

// getDeviceNode extracts the block device path (e.g., "/dev/sda1") from D-Bus properties.
func (*linuxMountDetector) getDeviceNode(props map[string]dbus.Variant) string {
	if device, ok := props["Device"]; ok {
		if devicePath, ok := device.Value().([]byte); ok && len(devicePath) > 0 {
			// Remove trailing null byte if present
			path := string(devicePath)
			return strings.TrimRight(path, "\x00")
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

// linuxMountDetectorFallback implements MountDetector for Linux using poll() on /proc/mounts.
// This is used when D-Bus/UDisks2 is not available (minimal Linux systems like MiSTer).
type linuxMountDetectorFallback struct {
	lastScan    time.Time
	mountsFile  *os.File
	events      chan MountEvent
	unmounts    chan string
	stopChan    chan struct{}
	mountedDevs map[string]MountEvent
	wg          sync.WaitGroup
	mu          syncutil.RWMutex
	stopOnce    sync.Once
}

// newLinuxMountDetectorFallback creates a new poll()-based mount detector for Linux.
func newLinuxMountDetectorFallback() (MountDetector, error) {
	return &linuxMountDetectorFallback{
		events:      make(chan MountEvent, 10),
		unmounts:    make(chan string, 10),
		stopChan:    make(chan struct{}),
		mountedDevs: make(map[string]MountEvent),
	}, nil
}

func (d *linuxMountDetectorFallback) Events() <-chan MountEvent {
	return d.events
}

func (d *linuxMountDetectorFallback) Unmounts() <-chan string {
	return d.unmounts
}

func (d *linuxMountDetectorFallback) Start() error {
	// Open /proc/mounts for polling
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return fmt.Errorf("failed to open /proc/mounts: %w", err)
	}
	d.mountsFile = file

	log.Debug().Msg("watching /proc/mounts for mount events via poll()")

	// Start event loop
	d.wg.Add(1)
	go d.pollMountChanges()

	return nil
}

func (d *linuxMountDetectorFallback) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopChan)
		d.wg.Wait() // Wait for polling goroutine to finish BEFORE closing file
		if d.mountsFile != nil {
			_ = d.mountsFile.Close()
		}
		close(d.events)
		close(d.unmounts)
	})
}

func (d *linuxMountDetectorFallback) Forget(deviceID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.mountedDevs, deviceID)
	log.Debug().Str("device_id", deviceID).Msg("forgot stale mount from poll tracking")
}

func (d *linuxMountDetectorFallback) pollMountChanges() {
	defer d.wg.Done()

	// Initial scan of mounts
	d.scanMounts()
	d.mu.Lock()
	d.lastScan = time.Now()
	d.mu.Unlock()

	log.Debug().
		Dur("rescan_interval", fallbackRescanInterval).
		Msg("fallback mount detector started with periodic rescan")

	// Set up poll for /proc/mounts with POLLPRI (priority event) and POLLERR
	pollFds := []unix.PollFd{
		{
			Fd:     int32(d.mountsFile.Fd()),
			Events: unix.POLLPRI | unix.POLLERR,
		},
	}

	for {
		select {
		case <-d.stopChan:
			return
		default:
		}

		// Poll with 1 second timeout to check stopChan periodically
		n, err := unix.Poll(pollFds, 1000)
		if err != nil {
			if err == unix.EINTR {
				// Interrupted by signal, retry
				continue
			}
			log.Warn().Err(err).Msg("poll() on /proc/mounts failed")
			return
		}

		select {
		case <-d.stopChan:
			return
		default:
		}

		shouldRescan := false
		rescanReason := ""

		if n > 0 && pollFds[0].Revents&(unix.POLLPRI|unix.POLLERR) != 0 {
			shouldRescan = true
			rescanReason = "poll event"
		} else {
			// Periodic rescan handles systems where poll() doesn't fire events
			d.mu.RLock()
			elapsed := time.Since(d.lastScan)
			d.mu.RUnlock()

			if elapsed >= fallbackRescanInterval {
				shouldRescan = true
				rescanReason = "periodic interval"
			}
		}

		if shouldRescan {
			if _, err := d.mountsFile.Seek(0, io.SeekStart); err != nil {
				log.Warn().Err(err).Msg("failed to seek /proc/mounts")
				continue
			}

			log.Debug().Str("reason", rescanReason).Msg("rescanning mounts")
			d.scanMounts()

			d.mu.Lock()
			d.lastScan = time.Now()
			d.mu.Unlock()
		}
	}
}

func (d *linuxMountDetectorFallback) scanMounts() {
	// Read current mounts from /proc/mounts
	currentMounts := make(map[string]MountEvent)

	scanner := bufio.NewScanner(d.mountsFile)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		device := fields[0]
		mountPath := fields[1]
		fstype := fields[2]

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

		// Only watch /media and /mnt mount points (removable media)
		if !strings.HasPrefix(mountPath, "/media/") && !strings.HasPrefix(mountPath, "/mnt/") {
			continue
		}

		// Try to get UUID from /dev/disk/by-uuid/
		deviceID := d.getDeviceUUID(device)
		if deviceID == "" {
			// Fall back to device name
			deviceID = device
		}

		// Determine device type
		deviceType := "removable"
		removableFSTypes := []string{"vfat", "exfat", "ntfs", "ext2", "ext3", "ext4", "hfs", "hfsplus"}
		for _, rmFS := range removableFSTypes {
			if strings.HasPrefix(fstype, rmFS) {
				deviceType = "removable"
				break
			}
		}

		volumeLabel := filepath.Base(mountPath)
		event := MountEvent{
			DeviceID:    deviceID,
			DeviceNode:  device, // The actual block device path from /proc/mounts
			MountPath:   mountPath,
			VolumeLabel: volumeLabel,
			DeviceType:  deviceType,
		}

		currentMounts[deviceID] = event
	}

	log.Debug().
		Int("matched_mounts", len(currentMounts)).
		Msg("mount scan completed")

	// Compare with previously tracked mounts
	d.mu.Lock()
	defer d.mu.Unlock()

	// Find newly mounted devices
	for deviceID, event := range currentMounts {
		if _, exists := d.mountedDevs[deviceID]; !exists {
			// New mount detected
			d.mountedDevs[deviceID] = event

			select {
			case d.events <- event:
				log.Debug().
					Str("device_id", deviceID).
					Str("mount_path", event.MountPath).
					Str("label", event.VolumeLabel).
					Msg("mount detected (poll)")
			case <-d.stopChan:
				return
			}
		}
	}

	// Find removed mounts
	for deviceID, event := range d.mountedDevs {
		if _, exists := currentMounts[deviceID]; !exists {
			// Mount was removed
			delete(d.mountedDevs, deviceID)

			select {
			case d.unmounts <- deviceID:
				log.Debug().
					Str("device_id", deviceID).
					Str("mount_path", event.MountPath).
					Msg("unmount detected (poll)")
			case <-d.stopChan:
				return
			}
		}
	}
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
