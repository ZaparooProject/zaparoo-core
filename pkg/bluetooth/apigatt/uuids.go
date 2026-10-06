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

// Package apigatt defines the GATT contract the Zaparoo app uses to reach
// the JSON-RPC API over Bluetooth Low Energy: the service and characteristic
// UUIDs, the limits, and the framing that carries one API message across
// many small ATT packets. It has no dependency on the API itself so the
// framing can be fuzzed and reused on its own.
package apigatt

// Service and characteristic UUIDs. The app must use the same values; they
// are fixed for the life of protocol version 1.
const (
	// ServiceUUID is the primary service the app scans for.
	ServiceUUID = "0da70001-b359-443b-836f-477d34b6a638"
	// RXCharUUID carries chunks from the app to Core (write, write without
	// response).
	RXCharUUID = "0da70002-b359-443b-836f-477d34b6a638"
	// TXCharUUID carries chunks from Core to the app (notify).
	TXCharUUID = "0da70003-b359-443b-836f-477d34b6a638"
	// InfoCharUUID is a read-only JSON description of the endpoint the app
	// reads before it speaks (see Info).
	InfoCharUUID = "0da70004-b359-443b-836f-477d34b6a638"
)

const (
	// ProtocolVersion is carried in every chunk header.
	ProtocolVersion = 1

	// MaxMessageSize caps one reassembled message from an authenticated
	// client. It matches the WebSocket limit. Messages from Core have no
	// cap: whatever the API returns is streamed.
	MaxMessageSize = 4 * 1024 * 1024

	// MaxUnauthenticatedMessageSize caps a message from a client that has
	// not authenticated yet, which is anyone in radio range. It covers a
	// pairing request or an encrypted first frame.
	MaxUnauthenticatedMessageSize = 64 * 1024

	// ReorderWindow is how far ahead of the expected chunk a chunk may
	// arrive and still be held. BlueZ hands each write to us on its own
	// goroutine, so chunks reach the reassembler in any order.
	ReorderWindow = 256

	// maxHeldBytes bounds the early chunks kept while waiting for the ones
	// before them.
	maxHeldBytes = 256 * 1024

	// SendWindow is how many chunks Core sends beyond the last one the
	// client acknowledged. It is what keeps a slow link from piling
	// megabytes up inside bluetoothd.
	SendWindow = 64

	// MaxProtocolErrors is how many recoverable framing mistakes one
	// connection may make before it is dropped.
	MaxProtocolErrors = 3

	// DefaultMTU is the ATT MTU every link starts with; a peer that never
	// negotiated a larger one gets 20-byte chunks.
	DefaultMTU = 23

	// attHeaderSize is what ATT itself takes from every packet.
	attHeaderSize = 3

	// MaxChunkSize is the longest value an attribute can hold. A link may
	// negotiate an MTU that would carry more (517 is common), but the
	// stack cuts a longer value short, so a chunk never exceeds this.
	MaxChunkSize = 512

	// PreferredMTU is what the Info characteristic suggests the app request.
	PreferredMTU = 512
)

// Info is the JSON document served by InfoCharUUID.
type Info struct {
	DeviceID string `json:"deviceId"`
	Version  int    `json:"v"`
	// MaxMessage and MaxUnauthenticated are the largest messages Core
	// accepts from the client, after and before authentication.
	MaxMessage         int `json:"maxMessage"`
	MaxUnauthenticated int `json:"maxUnauthenticated"`
	PreferredMTU       int `json:"preferredMtu"`
	// Window is the most chunks Core sends without an acknowledgement.
	Window int `json:"window"`
}

// NewInfo describes this endpoint for the given device.
func NewInfo(deviceID string) Info {
	return Info{
		DeviceID:           deviceID,
		Version:            ProtocolVersion,
		MaxMessage:         MaxMessageSize,
		MaxUnauthenticated: MaxUnauthenticatedMessageSize,
		PreferredMTU:       PreferredMTU,
		Window:             SendWindow,
	}
}
