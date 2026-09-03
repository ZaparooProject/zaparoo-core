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
	"context"
	"errors"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// ErrTagNotDetected and ErrUnsupportedTagType are expected user conditions
// during a tag write (no tag was presented, or an unsupported tag was). Reader
// drivers return these so the write path can log them at Warn rather than Error,
// keeping them out of Sentry while genuine hardware failures remain at Error.
var (
	ErrTagNotDetected     = errors.New("could not detect a tag")
	ErrUnsupportedTagType = errors.New("unsupported tag type")
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

// ScanProperty is typed metadata a reader identified about a scanned object.
// It has the same key/value shape as a stored media property.
type ScanProperty struct {
	System string // optional scope; empty means unscoped
	Name   string
	Value  string
}

type Scan struct {
	Error             error
	Token             *tokens.Token
	Source            string
	ReaderID          string
	Properties        []ScanProperty
	ReaderError       bool // True when Token is nil due to reader error/disconnect vs normal token removal
	WrittenTagRemoved bool // True when a successful write's physical tag has left the reader
}

type WriteOptions struct {
	TargetUID  string
	ExcludeUID string
}

type TargetWriter interface {
	WriteTarget(context.Context, string, WriteOptions) (*tokens.Token, error)
}

// OpenOpts provides options for opening a reader connection.
type OpenOpts struct {
	// Probing indicates this is an auto-detection probe. Readers should
	// log connection failures at Trace level instead of Error since
	// failures are expected when probing devices without a matching reader.
	Probing bool
}

type Reader interface {
	// Metadata returns static configuration for this driver.
	Metadata() DriverMetadata
	// IDs returns the device string prefixes supported by this reader.
	IDs() []string
	// Open any necessary connections to the device and start polling.
	// Takes a device connection string and a channel to send scanned tokens.
	Open(config.ReadersConnect, chan<- Scan, OpenOpts) error
	// Close any open connections to the device and stop polling.
	Close() error
	// Detect attempts to search for a connected device and returns the device
	// connection string. If no device is found, an empty string is returned.
	// Takes a list of currently connected device strings.
	Detect([]string) string
	// Path returns the connection path used to open this reader.
	// This is the physical resource identifier (e.g., "/dev/ttyUSB0", PCSC
	// reader name, MQTT broker URL) used to prevent multiple drivers from
	// competing for the same device.
	Path() string
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

// NormalizeDriverID returns a driver ID in the form driver IDs are compared in.
//
// Underscores are dropped for backwards compatibility with the legacy format,
// so "simple_serial" and "simpleserial" work interchangeably, and case is
// folded because a driver ID is something a user types into a config file:
// "PN532" and "pn532" name the same driver.
//
// config.normalizeDriverID applies the same rule for [[readers.connect]] and
// [readers.drivers.<id>]; pkg/config cannot import this package, so the two
// must be kept in step.
func NormalizeDriverID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "_", ""))
}

// DriverIDs returns every driver ID a reader answers to, canonical ID first.
//
// A reader opens under whichever spelling the config used (Open checks
// MatchesDriverID against IDs), so anything looking that reader's settings up
// in config has to try the whole set or an entry written with an alias is
// silently ignored. IDs alone is not enough: libnfc in ACR122 mode reports a
// canonical ID of "libnfcacr122" that its IDs list does not contain.
func DriverIDs(r Reader) []string {
	if r == nil {
		return nil
	}

	aliases := r.IDs()
	ids := make([]string, 0, len(aliases)+1)
	seen := make(map[string]struct{}, len(aliases)+1)
	for _, id := range append([]string{r.Metadata().ID}, aliases...) {
		if id == "" {
			continue
		}
		normalized := NormalizeDriverID(id)
		if _, dup := seen[normalized]; dup {
			continue
		}
		seen[normalized] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// MatchesDriverID reports whether driver names one of ids, comparing them the
// way NormalizeDriverID does. Drivers use this to check the ID a config entry
// asked for against the IDs they answer to, so a reader configured as "PN532"
// or "simple_serial" opens rather than being silently skipped.
func MatchesDriverID(ids []string, driver string) bool {
	normalized := NormalizeDriverID(driver)
	for _, id := range ids {
		if NormalizeDriverID(id) == normalized {
			return true
		}
	}
	return false
}
