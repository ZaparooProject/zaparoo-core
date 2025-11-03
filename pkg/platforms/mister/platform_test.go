//go:build linux

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

package mister

import (
	"context"
	"os/exec"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopActiveLauncher_CustomKill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		customKillErr     error
		customKillFunc    func(*config.Instance) error
		name              string
		customKillCalled  bool
		hasTrackedProcess bool
		expectSignalKill  bool
	}{
		{
			name: "custom Kill function is called when defined",
			customKillFunc: func(_ *config.Instance) error {
				return nil
			},
			customKillCalled:  true,
			hasTrackedProcess: true,
			expectSignalKill:  false,
		},
		{
			name: "custom Kill function error is logged but not fatal",
			customKillFunc: func(_ *config.Instance) error {
				return assert.AnError
			},
			customKillCalled:  true,
			customKillErr:     assert.AnError,
			hasTrackedProcess: true,
			expectSignalKill:  false,
		},
		{
			name:              "signal-based kill used when no custom Kill defined",
			customKillFunc:    nil,
			customKillCalled:  false,
			hasTrackedProcess: true,
			expectSignalKill:  true,
		},
		{
			name:              "no kill attempted when no tracked process",
			customKillFunc:    nil,
			customKillCalled:  false,
			hasTrackedProcess: false,
			expectSignalKill:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create platform instance
			p := NewPlatform()
			p.setActiveMedia = func(_ *models.ActiveMedia) {}

			// Track if custom Kill was called
			killCalled := false
			var launcher platforms.Launcher
			if tt.customKillFunc != nil {
				launcher.Kill = func(cfg *config.Instance) error {
					killCalled = true
					return tt.customKillFunc(cfg)
				}
			}
			p.setLastLauncher(&launcher)

			// Set up tracked process if needed
			if tt.hasTrackedProcess {
				// Create a dummy process (sleep) that we can kill
				ctx := context.Background()
				cmd := exec.CommandContext(ctx, "sleep", "10")
				err := cmd.Start()
				require.NoError(t, err)
				defer func() {
					// Clean up process if still running
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
				}()
				p.SetTrackedProcess(cmd.Process)
			}

			// Call StopActiveLauncher
			err := p.StopActiveLauncher(platforms.StopForPreemption)

			// Verify no error from StopActiveLauncher itself
			require.NoError(t, err)

			// Verify custom Kill was called if expected
			assert.Equal(t, tt.customKillCalled, killCalled, "custom Kill called mismatch")
		})
	}
}

func TestScummVMLauncher_HasCustomKill(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	launcher := createScummVMLauncher(p)

	// Verify ScummVM launcher has a custom Kill function
	// This is important because ScummVM requires keyboard-based exit (Ctrl+q)
	// instead of signal-based termination to avoid VT lock issues on MiSTer
	assert.NotNil(t, launcher.Kill, "ScummVM launcher should have custom Kill function")

	// Note: We can't actually test keyboard input without initializing the
	// keyboard device, which requires uinput access. The function signature
	// and presence is what matters for the platform to use it correctly.
}
