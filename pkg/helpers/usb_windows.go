//go:build windows

/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package helpers

import (
	"regexp"
	"strings"
	"unsafe"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

// usbPortPattern matches USB port segments like "USB(1)", "USB(2)", etc.
var usbPortPattern = regexp.MustCompile(`USB\((\d+)\)`)

// usbRootPattern matches USBROOT segments like "USBROOT(0)", "USBROOT(1)", etc.
var usbRootPattern = regexp.MustCompile(`USBROOT\((\d+)\)`)

// GetUSBTopologyPath resolves a device path (e.g., COM3) to its USB port
// topology path. This path is stable across reboots as long as the device
// stays in the same physical USB port.
//
// Returns empty string if topology cannot be determined (e.g., non-USB device,
// or missing location information).
func GetUSBTopologyPath(devicePath string) string {
	if devicePath == "" {
		return ""
	}

	// Normalize COM port names (e.g., "COM3" -> "COM3")
	devicePath = strings.ToUpper(strings.TrimSpace(devicePath))

	// Get device info set for all present ports
	classGUID := windows.GUID{
		Data1: 0x4d36e978,
		Data2: 0xe325,
		Data3: 0x11ce,
		Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18},
	} // GUID_DEVCLASS_PORTS

	devInfo, err := windows.SetupDiGetClassDevsEx(
		&classGUID,
		"",
		0,
		windows.DIGCF_PRESENT,
		0,
		"",
	)
	if err != nil {
		log.Debug().Err(err).Msg("failed to get device info set")
		return ""
	}
	defer func() {
		_ = windows.SetupDiDestroyDeviceInfoList(devInfo)
	}()

	// Enumerate devices to find our COM port
	for i := 0; ; i++ {
		deviceInfoData, err := windows.SetupDiEnumDeviceInfo(devInfo, i)
		if err != nil {
			break // No more devices
		}

		// Get the port name to match against devicePath
		portName := getDevicePortName(devInfo, deviceInfoData)
		if portName == "" || !strings.EqualFold(portName, devicePath) {
			continue
		}

		// Found our device, get location paths
		locationPaths, err := windows.SetupDiGetDeviceRegistryProperty(
			devInfo,
			deviceInfoData,
			windows.SPDRP_LOCATION_PATHS,
		)
		if err != nil {
			log.Debug().Err(err).Str("port", devicePath).Msg("failed to get location paths")
			return ""
		}

		// Location paths is a multi-string, take the first one
		if paths, ok := locationPaths.([]string); ok && len(paths) > 0 {
			return extractWindowsUSBTopology(paths[0])
		}
		if path, ok := locationPaths.(string); ok {
			return extractWindowsUSBTopology(path)
		}
	}

	log.Debug().Str("path", devicePath).Msg("device not found in enumeration")
	return ""
}

// getDevicePortName retrieves the COM port name from the device registry.
func getDevicePortName(devInfo windows.DevInfo, deviceInfoData *windows.DevInfoData) string {
	// Open the device's hardware key
	key, err := windows.SetupDiOpenDevRegKey(
		devInfo,
		deviceInfoData,
		windows.DICS_FLAG_GLOBAL,
		0,
		windows.DIREG_DEV,
		windows.KEY_READ,
	)
	if err != nil {
		return ""
	}
	defer func() {
		_ = windows.RegCloseKey(key)
	}()

	// Read the PortName value
	var buf [256]uint16
	bufLen := uint32(len(buf) * 2)
	var valType uint32
	err = windows.RegQueryValueEx(
		key,
		windows.StringToUTF16Ptr("PortName"),
		nil,
		&valType,
		(*byte)(unsafe.Pointer(&buf[0])), //nolint:gosec // required for Windows API
		&bufLen,
	)
	if err != nil {
		return ""
	}

	return windows.UTF16ToString(buf[:])
}

// extractWindowsUSBTopology extracts USB topology from a Windows location path.
// Input format: "PCIROOT(0)#PCI(1400)#USBROOT(0)#USB(1)#USB(2)#USB(3)"
// Output format: "1-2.3" (matching Linux format for consistency)
func extractWindowsUSBTopology(locationPath string) string {
	if locationPath == "" {
		return ""
	}

	// Find all USB(n) segments after USBROOT
	usbrootIdx := strings.Index(locationPath, "USBROOT(")
	if usbrootIdx == -1 {
		return ""
	}

	// Get the portion after USBROOT
	usbPortion := locationPath[usbrootIdx:]

	// Extract all USB port numbers
	matches := usbPortPattern.FindAllStringSubmatch(usbPortion, -1)
	if len(matches) == 0 {
		return ""
	}

	// Build topology string in Linux-like format: "bus-port.port.port"
	// Extract USBROOT number as bus ID, then join USB port numbers
	ports := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) >= 2 {
			ports = append(ports, match[1])
		}
	}

	if len(ports) == 0 {
		return ""
	}

	// Extract USBROOT number for bus ID
	busMatch := usbRootPattern.FindStringSubmatch(usbPortion)
	busID := "0"
	if len(busMatch) >= 2 {
		busID = busMatch[1]
	}

	// Format: "bus-port" or "bus-port.port.port..."
	if len(ports) == 1 {
		return busID + "-" + ports[0]
	}
	return busID + "-" + ports[0] + "." + strings.Join(ports[1:], ".")
}
