// Copyright (C) 2025 Zaparoo Core contributors
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
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core. If not, see <https://www.gnu.org/licenses/>.

//go:build windows

package externaldrive

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

const (
	// WMI event types
	wmiEventInsert = 2 // Device arrival
	wmiEventRemove = 3 // Device removal
)

// windowsMountDetector implements MountDetector for Windows using WMI.
type windowsMountDetector struct {
	events      chan MountEvent
	unmounts    chan string
	stopChan    chan struct{}
	wg          sync.WaitGroup
	mu          sync.RWMutex
	mountedDevs map[string]MountEvent // deviceID -> MountEvent
	stopOnce    sync.Once
}

// NewMountDetector creates a new Windows mount detector using WMI.
func NewMountDetector() (MountDetector, error) {
	return &windowsMountDetector{
		events:      make(chan MountEvent, 10),
		unmounts:    make(chan string, 10),
		stopChan:    make(chan struct{}),
		mountedDevs: make(map[string]MountEvent),
	}, nil
}

func (d *windowsMountDetector) Events() <-chan MountEvent {
	return d.events
}

func (d *windowsMountDetector) Unmounts() <-chan string {
	return d.unmounts
}

func (d *windowsMountDetector) Start() error {
	// Initialize COM
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		return fmt.Errorf("failed to initialize COM: %w", err)
	}

	// Start WMI event watcher
	d.wg.Add(1)
	go d.watchVolumeChanges()

	return nil
}

func (d *windowsMountDetector) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopChan)
		d.wg.Wait()
		ole.CoUninitialize()
		close(d.events)
		close(d.unmounts)
	})
}

func (d *windowsMountDetector) watchVolumeChanges() {
	defer d.wg.Done()

	// Initialize COM for this goroutine
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		log.Error().Err(err).Msg("Failed to initialize COM for volume watcher")
		return
	}
	defer ole.CoUninitialize()

	// Connect to WMI
	unknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		log.Error().Err(err).Msg("Failed to create WMI locator")
		return
	}
	defer unknown.Release()

	wmi, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query WMI interface")
		return
	}
	defer wmi.Release()

	// Connect to local WMI service
	serviceRaw, err := oleutil.CallMethod(wmi, "ConnectServer")
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to WMI service")
		return
	}
	service := serviceRaw.ToIDispatch()
	defer service.Release()

	// Query for Win32_VolumeChangeEvent
	// EventType: 2 = device arrival, 3 = device removal
	queryRaw, err := oleutil.CallMethod(service, "ExecNotificationQuery",
		"SELECT * FROM Win32_VolumeChangeEvent WHERE EventType = 2 OR EventType = 3")
	if err != nil {
		log.Error().Err(err).Msg("Failed to execute WMI query")
		return
	}
	eventSink := queryRaw.ToIDispatch()
	defer eventSink.Release()

	log.Debug().Msg("Started watching for Windows volume changes")

	for {
		select {
		case <-d.stopChan:
			return
		default:
			// Wait for next event (timeout: 1000ms)
			nextRaw, err := oleutil.CallMethod(eventSink, "NextEvent", 1000)
			if err != nil {
				// Timeout or error - check if we should stop
				select {
				case <-d.stopChan:
					return
				default:
					continue
				}
			}

			if nextRaw.VT == ole.VT_NULL || nextRaw.VT == ole.VT_EMPTY {
				continue
			}

			event := nextRaw.ToIDispatch()
			d.handleVolumeEvent(event)
			event.Release()
		}
	}
}

func (d *windowsMountDetector) handleVolumeEvent(event *ole.IDispatch) {
	// Get event type
	eventTypeRaw, err := oleutil.GetProperty(event, "EventType")
	if err != nil {
		log.Debug().Err(err).Msg("Failed to get event type")
		return
	}
	eventType := int(eventTypeRaw.Val)

	// Get drive name (e.g., "E:")
	driveNameRaw, err := oleutil.GetProperty(event, "DriveName")
	if err != nil {
		log.Debug().Err(err).Msg("Failed to get drive name")
		return
	}
	driveName := driveNameRaw.ToString()

	// Ensure drive name has backslash for Windows path
	if !strings.HasSuffix(driveName, "\\") {
		driveName += "\\"
	}

	switch eventType {
	case wmiEventInsert:
		d.handleDriveInsert(driveName)
	case wmiEventRemove:
		d.handleDriveRemove(driveName)
	}
}

func (d *windowsMountDetector) handleDriveInsert(driveName string) {
	// Check if drive is removable
	if !d.isRemovableDrive(driveName) {
		return
	}

	// Get drive info
	deviceID, volumeLabel := d.getDriveInfo(driveName)
	if deviceID == "" {
		// Use drive name as fallback ID
		deviceID = strings.TrimSuffix(driveName, "\\")
	}

	event := MountEvent{
		DeviceID:    deviceID,
		MountPath:   driveName,
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
			Str("drive", driveName).
			Str("label", volumeLabel).
			Msg("Drive insertion detected")
	case <-d.stopChan:
		return
	}
}

func (d *windowsMountDetector) handleDriveRemove(driveName string) {
	deviceID := strings.TrimSuffix(driveName, "\\")

	d.mu.Lock()
	// Try to find by mount path
	var foundID string
	for id, event := range d.mountedDevs {
		if event.MountPath == driveName || id == deviceID {
			foundID = id
			break
		}
	}

	if foundID != "" {
		delete(d.mountedDevs, foundID)
		d.mu.Unlock()

		select {
		case d.unmounts <- foundID:
			log.Debug().
				Str("device_id", foundID).
				Str("drive", driveName).
				Msg("Drive removal detected")
		case <-d.stopChan:
			return
		}
	} else {
		d.mu.Unlock()
	}
}

func (d *windowsMountDetector) isRemovableDrive(drivePath string) bool {
	drivePathPtr, err := windows.UTF16PtrFromString(drivePath)
	if err != nil {
		return false
	}

	driveType := windows.GetDriveType(drivePathPtr)
	return driveType == windows.DRIVE_REMOVABLE
}

func (d *windowsMountDetector) getDriveInfo(drivePath string) (deviceID, volumeLabel string) {
	drivePathPtr, err := windows.UTF16PtrFromString(drivePath)
	if err != nil {
		return "", ""
	}

	// Get volume information
	var volumeNameBuf [windows.MAX_PATH + 1]uint16
	var volumeSerialNumber uint32
	var maxComponentLength uint32
	var fileSystemFlags uint32
	var fileSystemNameBuf [windows.MAX_PATH + 1]uint16

	err = windows.GetVolumeInformation(
		drivePathPtr,
		&volumeNameBuf[0],
		uint32(len(volumeNameBuf)),
		&volumeSerialNumber,
		&maxComponentLength,
		&fileSystemFlags,
		&fileSystemNameBuf[0],
		uint32(len(fileSystemNameBuf)),
	)

	if err == nil {
		volumeLabel = windows.UTF16ToString(volumeNameBuf[:])
		// Use serial number as device ID (more stable than drive letter)
		deviceID = fmt.Sprintf("%X", volumeSerialNumber)
	}

	return deviceID, volumeLabel
}
