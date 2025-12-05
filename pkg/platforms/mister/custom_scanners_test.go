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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeoGeoScanner_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("returns early when context is cancelled at start", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		// Find the NeoGeo launcher
		var neoGeoLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "NeoGeo" {
				neoGeoLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, neoGeoLauncher, "NeoGeo launcher should exist")
		require.NotNil(t, neoGeoLauncher.Scanner, "NeoGeo launcher should have a Scanner")

		// Create an already-cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Call the scanner
		results, err := neoGeoLauncher.Scanner(ctx, cfg, "NeoGeo", []platforms.ScanResult{})

		// Should return context.Canceled error
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, results)
	})

	t.Run("respects context cancellation during processing", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		var neoGeoLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "NeoGeo" {
				neoGeoLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, neoGeoLauncher)
		require.NotNil(t, neoGeoLauncher.Scanner)

		// Create a context with short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		// Wait for context to be cancelled
		<-ctx.Done()

		// Call the scanner after context is cancelled
		results, err := neoGeoLauncher.Scanner(ctx, cfg, "NeoGeo", []platforms.ScanResult{})

		// Should return context error
		require.Error(t, err)
		assert.Empty(t, results)
	})
}

func TestAmigaScanner_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("returns early when context is cancelled at start", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		// Find the Amiga launcher
		var amigaLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "Amiga" {
				amigaLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, amigaLauncher, "Amiga launcher should exist")
		require.NotNil(t, amigaLauncher.Scanner, "Amiga launcher should have a Scanner")

		// Create an already-cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Call the scanner
		results, err := amigaLauncher.Scanner(ctx, cfg, "Amiga", []platforms.ScanResult{})

		// Should return context.Canceled error
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, results)
	})
}

func TestNeoGeoScanner_HandlesEmptyDirectory(t *testing.T) {
	t.Parallel()

	// This test verifies the scanner handles empty directories gracefully

	t.Run("handles empty directory without error", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		var neoGeoLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "NeoGeo" {
				neoGeoLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, neoGeoLauncher)
		require.NotNil(t, neoGeoLauncher.Scanner)

		ctx := context.Background()

		// Call scanner - it should handle non-existent paths gracefully
		results, err := neoGeoLauncher.Scanner(ctx, cfg, "NeoGeo", []platforms.ScanResult{})

		// Should not error, just return empty/original results
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestAmigaScanner_HandlesNoPaths(t *testing.T) {
	t.Parallel()

	t.Run("handles no Amiga paths without error", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		var amigaLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "Amiga" {
				amigaLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, amigaLauncher)
		require.NotNil(t, amigaLauncher.Scanner)

		ctx := context.Background()

		// Call scanner - it should handle non-existent paths gracefully
		results, err := amigaLauncher.Scanner(ctx, cfg, "Amiga", []platforms.ScanResult{})

		// Should not error, just return empty/original results
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}
