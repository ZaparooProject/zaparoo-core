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
