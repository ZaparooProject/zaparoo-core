/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
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

package tty2oled

import "sync/atomic"

// ConnectionState represents the current state of the TTY2OLED device connection
type ConnectionState int32

const (
	// StateDisconnected indicates the device is not connected
	StateDisconnected ConnectionState = iota
	// StateDetecting indicates we are attempting to detect the device
	StateDetecting
	// StateConnecting indicates we are establishing the serial connection
	StateConnecting
	// StateHandshaking indicates we are performing the initial handshake
	StateHandshaking
	// StateInitializing indicates we are sending initialization commands
	StateInitializing
	// StateConnected indicates the device is fully ready for operations
	StateConnected
)

// String returns a human-readable representation of the connection state
func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "Disconnected"
	case StateDetecting:
		return "Detecting"
	case StateConnecting:
		return "Connecting"
	case StateHandshaking:
		return "Handshaking"
	case StateInitializing:
		return "Initializing"
	case StateConnected:
		return "Connected"
	default:
		return "Unknown"
	}
}

// IsValidTransition checks if transitioning from one state to another is valid
func IsValidTransition(from, to ConnectionState) bool {
	switch from {
	case StateDisconnected:
		// From disconnected, can only go to detecting or directly to connecting
		return to == StateDetecting || to == StateConnecting
	case StateDetecting:
		// From detecting, can go to connecting or back to disconnected (if detection fails)
		return to == StateConnecting || to == StateDisconnected
	case StateConnecting:
		// From connecting, can go to handshaking or back to disconnected (if connection fails)
		return to == StateHandshaking || to == StateDisconnected
	case StateHandshaking:
		// From handshaking, can go to initializing or back to disconnected (if handshake fails)
		return to == StateInitializing || to == StateDisconnected
	case StateInitializing:
		// From initializing, can go to connected or back to disconnected (if initialization fails)
		return to == StateConnected || to == StateDisconnected
	case StateConnected:
		// From connected, can only go back to disconnected (for any failure or explicit disconnect)
		// Or to detecting (for auto-reconnection scenarios)
		return to == StateDisconnected || to == StateDetecting
	default:
		return false
	}
}

// StateManager provides thread-safe state management for the connection
type StateManager struct {
	state int32 // Use int32 for atomic operations
}

// NewStateManager creates a new state manager initialized to StateDisconnected
func NewStateManager() *StateManager {
	return &StateManager{
		state: int32(StateDisconnected),
	}
}

// GetState returns the current connection state
func (sm *StateManager) GetState() ConnectionState {
	return ConnectionState(atomic.LoadInt32(&sm.state))
}

// SetState atomically sets the connection state if the transition is valid
func (sm *StateManager) SetState(newState ConnectionState) bool {
	for {
		current := ConnectionState(atomic.LoadInt32(&sm.state))
		if !IsValidTransition(current, newState) {
			return false
		}

		if atomic.CompareAndSwapInt32(&sm.state, int32(current), int32(newState)) {
			return true
		}
		// Retry if the state changed between our read and compare-and-swap
	}
}

// ForceState atomically sets the connection state without validation
// This should only be used in exceptional cases where the state machine
// needs to be reset to a known state
func (sm *StateManager) ForceState(newState ConnectionState) {
	atomic.StoreInt32(&sm.state, int32(newState))
}
