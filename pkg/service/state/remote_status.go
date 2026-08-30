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

package state

import "time"

// Remote control states, as last observed by the remote operations poller.
// They describe why the device is or isn't reachable for remote commands
// right now, so the owner can see the difference between "waiting for a
// command" and "the server won't talk to this device".
const (
	// RemoteStateUnknown means the poller has not reported yet.
	RemoteStateUnknown = "unknown"
	// RemoteStateDisabled means the owner has not enabled remote control.
	RemoteStateDisabled = "disabled"
	// RemoteStateUnlinked means there is no linked-account credential.
	RemoteStateUnlinked = "unlinked"
	// RemoteStateConnecting means the capability is being advertised.
	RemoteStateConnecting = "connecting"
	// RemoteStateWaiting means the device is polling for commands normally.
	RemoteStateWaiting = "waiting"
	// RemoteStateNotRemoteDevice means the server refused the poll because
	// this device is not the account's designated remote device.
	RemoteStateNotRemoteDevice = "not_remote_device"
	// RemoteStateUnavailable means the server reports the feature as off.
	RemoteStateUnavailable = "unavailable"
	// RemoteStateCredentialRejected means the server rejected the device
	// credential; the account must be linked again.
	RemoteStateCredentialRejected = "credential_rejected" //nolint:gosec // G101: a state name, not a credential
	// RemoteStateError means the last poll failed for another reason (no
	// network, a server error, rate limiting).
	RemoteStateError = "error"
)

// RemoteStatus is the poller's last observation. LastContactAt is the last
// time the server answered a poll normally; LastErrorCode is the server's
// error code (or a short local one) for the states that carry one.
type RemoteStatus struct {
	LastContactAt time.Time
	UpdatedAt     time.Time
	State         string
	LastErrorCode string
}

// SetRemoteStatus records the poller's latest observation. A waiting state
// also counts as a successful contact.
func (s *State) SetRemoteStatus(remoteState, errorCode string) {
	s.remoteStatusMu.Lock()
	defer s.remoteStatusMu.Unlock()
	now := time.Now()
	s.remoteStatus.State = remoteState
	s.remoteStatus.LastErrorCode = errorCode
	s.remoteStatus.UpdatedAt = now
	if remoteState == RemoteStateWaiting {
		s.remoteStatus.LastContactAt = now
	}
}

// RemoteStatus returns the poller's last observation. State is empty until
// the poller has reported once.
func (s *State) RemoteStatus() RemoteStatus {
	s.remoteStatusMu.RLock()
	defer s.remoteStatusMu.RUnlock()
	return s.remoteStatus
}
