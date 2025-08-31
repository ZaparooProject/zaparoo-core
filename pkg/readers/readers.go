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

package readers

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
)

type Capability string

const (
	CapabilityWrite   Capability = "write"
	CapabilityDisplay Capability = "display"
)

type DriverMetadata struct {
	ID                string
	Description       string
	DefaultEnabled    bool
	DefaultAutoDetect bool
}

type Scan struct {
	Error  error
	Token  *tokens.Token
	Source string
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
}
