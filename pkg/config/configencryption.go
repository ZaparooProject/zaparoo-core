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

package config

// cloneEncryptionValue copies the optional encryption value so unmarshalling
// into active values cannot mutate the stored platform default through a
// shared pointer.
//
//nolint:gocritic // Values is intentionally copied before replacing one pointer.
func cloneEncryptionValue(values Values) Values {
	if values.Service.Encryption != nil {
		enabled := *values.Service.Encryption
		values.Service.Encryption = &enabled
	}
	return values
}

// EncryptionEnabled returns whether WebSocket encryption is enabled. When
// true, remote WebSocket clients must send an encrypted first frame derived
// from a paired key; plaintext WebSocket connections from non-loopback
// addresses are rejected. A nil value uses the platform default supplied to
// the config instance; the base default is false.
//
// Localhost connections are always allowed plaintext regardless of this
// setting.
func (c *Instance) EncryptionEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Service.Encryption == nil {
		return false
	}
	return *c.vals.Service.Encryption
}

// SetEncryptionEnabled toggles WebSocket encryption. The explicit value is
// retained even when false so it can override a platform default of true. The
// caller is responsible for calling Save() to persist the change.
func (c *Instance) SetEncryptionEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Service.Encryption = &enabled
}
