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

package helpers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewTestConfig demonstrates the need for a standard test config helper
func TestNewTestConfig(t *testing.T) {
	t.Parallel()

	// Setup in-memory filesystem (for future filesystem-based config support)
	fs := NewMemoryFS()

	// Create a temporary directory for config
	configDir := t.TempDir()

	// This should create a proper config instance for testing
	cfg, err := NewTestConfig(fs, configDir)

	// Verify the config was created properly
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, config.DefaultAPIPort, cfg.APIPort())

	// Verify the config file exists on the real filesystem
	configPath := filepath.Join(configDir, config.CfgFile)
	_, err = os.Stat(configPath)
	assert.NoError(t, err, "config file should exist")
}
