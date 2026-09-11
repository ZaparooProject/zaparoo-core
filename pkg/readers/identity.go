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

package readers

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// GenerateReaderID creates a deterministic reader ID from driver name and a
// stable path. The ID format is "{driver}-{hash}" where hash is 8 lowercase
// base32 characters (40 bits) derived from SHA-256.
//
// The stablePath should be something that persists across reboots when the
// hardware stays in the same port, such as:
//   - USB topology path (e.g., "1-2.3.1") for serial readers
//   - PCSC reader name for smart card readers
//   - File path for file-based readers
//   - Broker + topic for MQTT readers
//
// Inputs are normalized (lowercased, path separators unified) to ensure
// consistent IDs across platforms. Same inputs always produce the same ID,
// enabling deterministic reader identification across service restarts.
func GenerateReaderID(driverName, stablePath string) string {
	normalizedDriver := strings.ToLower(driverName)
	normalizedPath := strings.ToLower(strings.ReplaceAll(stablePath, "\\", "/"))

	input := fmt.Sprintf("%s\x00%s", normalizedDriver, normalizedPath)
	hash := sha256.Sum256([]byte(input))

	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hash[:5])
	encoded = strings.ToLower(encoded)

	return fmt.Sprintf("%s-%s", normalizedDriver, encoded)
}

// USBReaderID holds the ID of a reader whose device is a USB serial port.
//
// The ID comes from the USB port the device is plugged into, so it survives
// re-enumeration (ttyACM0 coming back as ttyACM1). The port can only be looked
// up while the device node exists: once the device is unplugged the lookup
// fails and a fresh derivation falls back to the device path, which is a
// different ID. The service stores a reader under the ID it reported when it
// connected and prunes it by the ID it reports after disconnecting, so a
// changed ID left the reader listed and pruned on every tick. The port is
// therefore kept once resolved.
type USBReaderID struct {
	port string
	mu   syncutil.Mutex
}

// ID returns the reader ID for driverID. port is the device's USB port as
// currently resolved, or "" when it cannot be determined; fallback identifies
// the reader when no port has ever been resolved.
func (u *USBReaderID) ID(driverID, port, fallback string) string {
	u.mu.Lock()
	defer u.mu.Unlock()

	if port != "" {
		u.port = port
	}

	stablePath := u.port
	if stablePath == "" {
		stablePath = fallback
	}
	return GenerateReaderID(driverID, stablePath)
}
