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

import (
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptionEnabled_DefaultsFalse(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	assert.False(t, cfg.EncryptionEnabled(), "missing field should default to false")
}

func TestSetEncryptionEnabled(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}

	cfg.SetEncryptionEnabled(true)
	assert.True(t, cfg.EncryptionEnabled())

	cfg.SetEncryptionEnabled(false)
	assert.False(t, cfg.EncryptionEnabled())
}

// TestServiceEncryption_OmitEmpty pins the omitempty behavior of the
// Service.Encryption field. A fresh install (or a config with encryption
// disabled) must NOT write `encryption = false` into config.toml. Without
// this test, an accidental drop of the omitempty tag would silently change
// config file contents for every user on the next save.
func TestServiceEncryption_OmitEmpty(t *testing.T) {
	t.Parallel()

	// Case 1: zero-value Service must not emit encryption.
	data, err := toml.Marshal(Service{})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "encryption",
		"zero-value Service must omit the encryption field")

	// Case 2: Service{Encryption: true} must emit encryption = true.
	data, err = toml.Marshal(Service{Encryption: true})
	require.NoError(t, err)
	assert.Contains(t, string(data), "encryption = true",
		"Service{Encryption: true} must emit encryption = true")

	// Case 3: round-trip — marshalling then unmarshalling must preserve
	// the enabled state.
	var got Service
	require.NoError(t, toml.Unmarshal(data, &got))
	assert.True(t, got.Encryption, "round-trip must preserve encryption = true")
}
