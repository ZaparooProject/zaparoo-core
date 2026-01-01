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

package discovery

import (
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		platformID string
	}{
		{"mister platform", "mister"},
		{"linux platform", "linux"},
		{"steamos platform", "steamos"},
		{"windows platform", "windows"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := New(nil, tt.platformID)

			assert.NotNil(t, svc)
			assert.Equal(t, tt.platformID, svc.platformID)
		})
	}
}

func TestServiceType(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "_zaparoo._tcp", ServiceType)
}

func TestStopIdempotent(t *testing.T) {
	t.Parallel()

	svc := New(nil, "test")

	// Stop should be safe to call multiple times even when not started
	svc.Stop()
	svc.Stop()
	svc.Stop()

	// No panic means success
	assert.Nil(t, svc.server)
}

func TestInstanceNameBeforeStart(t *testing.T) {
	t.Parallel()

	svc := New(nil, "test")

	// InstanceName should return empty string before Start() is called
	assert.Empty(t, svc.InstanceName())
}

func TestInstanceNameWhenDiscoveryDisabled(t *testing.T) {
	t.Parallel()

	// Create a test config with discovery disabled
	configDir := t.TempDir()
	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)

	// Disable discovery
	cfg.SetDiscoveryEnabled(false)

	svc := New(cfg, "test")
	err = svc.Start()
	require.NoError(t, err)

	// InstanceName should be empty when discovery is disabled
	assert.Empty(t, svc.InstanceName())
}

func TestInstanceNameUsesHostname(t *testing.T) {
	t.Parallel()

	// Create a test config with discovery enabled (default)
	configDir := t.TempDir()
	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)

	// Get expected hostname
	expectedHostname, err := os.Hostname()
	require.NoError(t, err)

	svc := New(cfg, "test")

	// Start will fail at zeroconf.Register, but instanceName is set
	// before Register is called, so we can still verify the resolution
	_ = svc.Start() // Ignore error - Register may fail without network

	// instanceName should be set to the hostname (since no config override)
	assert.Equal(t, expectedHostname, svc.InstanceName())
}

func TestInstanceNameUsesConfigOverride(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	cfg, err := config.NewConfig(configDir, config.BaseDefaults)
	require.NoError(t, err)

	// Set a custom instance name in config
	cfg.SetDiscoveryInstanceName("my-custom-name")

	svc := New(cfg, "test")
	_ = svc.Start() // Ignore error - Register may fail without network

	// instanceName should use the config override, not hostname
	assert.Equal(t, "my-custom-name", svc.InstanceName())
}
