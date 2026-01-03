//go:build darwin

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

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <stdlib.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/serial/IOSerialKeys.h>
#include <IOKit/IOBSD.h>
#include <CoreFoundation/CoreFoundation.h>

// getLocationID retrieves the locationID property from a USB device ancestor.
// Returns 0 if not found.
static unsigned int getLocationIDForDevice(const char* bsdPath) {
    CFMutableDictionaryRef matchingDict;
    io_iterator_t iter;
    io_service_t service;
    unsigned int locationID = 0;

    // Create matching dictionary for the BSD path
    matchingDict = IOServiceMatching(kIOSerialBSDServiceValue);
    if (!matchingDict) {
        return 0;
    }

    CFStringRef pathStr = CFStringCreateWithCString(kCFAllocatorDefault, bsdPath, kCFStringEncodingUTF8);
    if (!pathStr) {
        return 0;
    }
    CFDictionarySetValue(matchingDict, CFSTR(kIOCalloutDeviceKey), pathStr);
    CFRelease(pathStr);

    // Get matching services
    kern_return_t kr = IOServiceGetMatchingServices(kIOMainPortDefault, matchingDict, &iter);
    if (kr != KERN_SUCCESS) {
        return 0;
    }

    // Get the first matching service
    service = IOIteratorNext(iter);
    IOObjectRelease(iter);

    if (!service) {
        return 0;
    }

    // Walk up the IORegistry tree to find USB device with locationID
    io_service_t parent = service;
    io_service_t current = service;

    while (current) {
        CFTypeRef locationRef = IORegistryEntryCreateCFProperty(
            current,
            CFSTR("locationID"),
            kCFAllocatorDefault,
            0
        );

        if (locationRef) {
            if (CFGetTypeID(locationRef) == CFNumberGetTypeID()) {
                CFNumberGetValue((CFNumberRef)locationRef, kCFNumberIntType, &locationID);
            }
            CFRelease(locationRef);
            if (locationID != 0) {
                if (current != service) {
                    IOObjectRelease(current);
                }
                IOObjectRelease(service);
                return locationID;
            }
        }

        // Get parent
        kr = IORegistryEntryGetParentEntry(current, kIOServicePlane, &parent);
        if (current != service) {
            IOObjectRelease(current);
        }
        if (kr != KERN_SUCCESS) {
            break;
        }
        current = parent;
    }

    IOObjectRelease(service);
    return 0;
}
*/
import "C" //nolint:gocritic // cgo requires separate import block

import (
	"path/filepath"
	"strconv"
	"strings"
	"unsafe" //nolint:gocritic // required for C.free

	"github.com/rs/zerolog/log"
)

// GetUSBTopologyPath resolves a device path (e.g., /dev/cu.usbserial-1234) to
// its USB port topology path. This path is stable across reboots as long as
// the device stays in the same physical USB port.
//
// Returns empty string if topology cannot be determined (e.g., non-USB device,
// or missing location information).
func GetUSBTopologyPath(devicePath string) string {
	if devicePath == "" {
		return ""
	}

	// Extract the BSD name (e.g., "cu.usbserial-1234" from "/dev/cu.usbserial-1234")
	bsdName := devicePath
	if strings.HasPrefix(devicePath, "/dev/") {
		bsdName = filepath.Base(devicePath)
	}

	// Get location ID via IOKit
	cPath := C.CString(bsdName)
	defer C.free(unsafe.Pointer(cPath))

	locationID := uint32(C.getLocationIDForDevice(cPath))
	if locationID == 0 {
		log.Debug().Str("path", devicePath).Msg("could not get locationID")
		return ""
	}

	return FormatLocationID(locationID)
}

// FormatLocationID converts a macOS USB locationID to a Linux-like topology string.
// The locationID is a 32-bit value where:
// - Upper 8 bits: Bus number (0x00-0xFF)
// - Remaining 24 bits: Port path encoded in 4-bit nibbles
//
// Example: 0x14200000 -> Bus 0x14 (20), Port 2 -> "20-2"
// Example: 0x14230000 -> Bus 0x14 (20), Port 2, Port 3 -> "20-2.3"
func FormatLocationID(locationID uint32) string {
	if locationID == 0 {
		return ""
	}

	// Extract bus number from upper 8 bits
	bus := (locationID >> 24) & 0xFF

	// Extract port path from remaining 24 bits (6 nibbles, each 4 bits)
	// Each nibble represents a port number (0 = end of path)
	ports := make([]string, 0, 6)
	remaining := locationID & 0x00FFFFFF

	for i := range 6 {
		// Extract each 4-bit nibble from high to low
		shift := uint(20 - i*4)
		port := (remaining >> shift) & 0x0F
		if port == 0 {
			break
		}
		ports = append(ports, strconv.FormatUint(uint64(port), 10))
	}

	if len(ports) == 0 {
		return ""
	}

	// Format: "bus-port" or "bus-port.port.port..."
	busStr := strconv.FormatUint(uint64(bus), 10)
	if len(ports) == 1 {
		return busStr + "-" + ports[0]
	}
	return busStr + "-" + ports[0] + "." + strings.Join(ports[1:], ".")
}
