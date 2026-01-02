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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

type Capability string

const (
	CapabilityWrite     Capability = "write"
	CapabilityDisplay   Capability = "display"
	CapabilityRemovable Capability = "removable"
)

type DriverMetadata struct {
	ID                string
	Description       string
	DefaultEnabled    bool
	DefaultAutoDetect bool
}

type Scan struct {
	Error       error
	Token       *tokens.Token
	Source      string
	ReaderError bool // True when Token is nil due to reader error/disconnect vs normal token removal
}

type Reader interface {
	// Metadata returns static configuration for this driver.
	Metadata() DriverMetadata
	// IDs returns the device string prefixes supported by this reader.
	IDs() []string
	// Open any necessary connections to the device and start polling.
	// Takes a device connection string and a channel to send scanned tokens.
	Open(config.ReadersConnect, chan<- Scan) error
	// Close any open connections to the device and stop polling.
	Close() error
	// Detect attempts to search for a connected device and returns the device
	// connection string. If no device is found, an empty string is returned.
	// Takes a list of currently connected device strings.
	Detect([]string) string
	// Device returns the device connection string.
	Device() string
	// Connected returns true if the device is connected and active.
	Connected() bool
	// Info returns a string with information about the connected device.
	Info() string
	// Write sends a string to the device to be written to a token, if
	// that device supports writing. Blocks until completion or timeout.
	Write(string) (*tokens.Token, error)
	// CancelWrite sends a request to cancel an active write request.
	CancelWrite()
	// Capabilities returns the list of capabilities supported by this reader.
	Capabilities() []Capability
	// OnMediaChange is called when the active media changes.
	OnMediaChange(*models.ActiveMedia) error
	// ReaderID returns a deterministic identifier for this reader instance.
	// The ID is stable across service restarts when the hardware stays in
	// the same port. Format: "{driver}-{hash16}" where hash16 is derived
	// from stable hardware attributes like USB topology path.
	ReaderID() string
}

// NormalizeDriverID removes underscores from driver IDs to provide backwards
// compatibility with the legacy underscore format (e.g., "simple_serial").
// This allows both "simple_serial" and "simpleserial" to work interchangeably.
func NormalizeDriverID(id string) string {
	return strings.ReplaceAll(id, "_", "")
}
