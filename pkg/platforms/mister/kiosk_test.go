//go:build linux && !android

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

package mister

import (
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFrontendStatus(t *testing.T, fs afero.Fs, body string) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, frontendStatusPath, []byte(body), 0o600))
}

func TestSetMainKiosk_SendsTheForkCommand(t *testing.T) {
	t.Parallel()
	main, fs := newTestMainCommands(t, forkCapabilities(testMainPID))

	require.NoError(t, setMainKiosk(main, true))
	assert.Equal(t, "zaparoo_kiosk on\n", sentMainCommand(t, main, fs))
}

func TestSetMainKiosk_ReleasesTheLock(t *testing.T) {
	t.Parallel()
	main, fs := newTestMainCommands(t, forkCapabilities(testMainPID))

	require.NoError(t, setMainKiosk(main, false))
	assert.Equal(t, "zaparoo_kiosk off\n", sentMainCommand(t, main, fs))
}

func TestSetMainKiosk_StockMainIsLeftAlone(t *testing.T) {
	t.Parallel()
	main, fs := newTestMainCommands(t, "")

	// Stock Main reads its command file too, and must never be sent a
	// command it would not understand.
	require.ErrorIs(t, setMainKiosk(main, true), errMainCommandUnsupported)
	assert.Empty(t, sentMainCommand(t, main, fs))
}

func TestSetMainKiosk_MainWithoutTheLock(t *testing.T) {
	t.Parallel()
	main, fs := newTestMainCommands(t, fmt.Sprintf("1 %d cheat save\n", testMainPID))

	require.ErrorIs(t, setMainKiosk(main, true), errMainCommandUnsupported)
	assert.Empty(t, sentMainCommand(t, main, fs))
}

func TestKioskUnsealed(t *testing.T) {
	t.Parallel()
	sealedCapabilities := fmt.Sprintf("1 %d cheat save mount osd kiosk kiosk_sealed frontend\n", testMainPID)
	liveFrontend := fmt.Sprintf(`{"active":true,"pid":%d}`, testMainPID)
	lockedFrontend := fmt.Sprintf(`{"active":true,"kiosk":true,"pid":%d}`, testMainPID)

	tests := []struct {
		name         string
		capabilities string
		frontend     string
		want         []string
	}{
		{
			name: "stock Main cannot lock its menu",
			want: []string{platforms.KioskUnsealedMainLock},
		},
		{
			name:         "fork Main locks the menu but not the controls",
			capabilities: forkCapabilities(testMainPID),
			want:         []string{platforms.KioskUnsealedMainControls},
		},
		{
			name:         "a frontend that does not follow the lock",
			capabilities: forkCapabilities(testMainPID),
			frontend:     liveFrontend,
			want:         []string{platforms.KioskUnsealedMainControls, platforms.KioskUnsealedFrontend},
		},
		{
			name:         "sealed Main under an older frontend",
			capabilities: sealedCapabilities,
			frontend:     liveFrontend,
			want:         []string{platforms.KioskUnsealedFrontend},
		},
		{
			name:         "sealed Main with no frontend",
			capabilities: sealedCapabilities,
			want:         []string{},
		},
		{
			name:         "sealed Main and a frontend that follows the lock",
			capabilities: sealedCapabilities,
			frontend:     lockedFrontend,
			want:         []string{},
		},
		{
			name:         "an unreadable frontend status is not a locked frontend",
			capabilities: sealedCapabilities,
			frontend:     `{"active":true,"kiosk":`,
			want:         []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			main, fs := newTestMainCommands(t, tt.capabilities)
			if tt.frontend != "" {
				writeFrontendStatus(t, fs, tt.frontend)
			}
			assert.Equal(t, tt.want, kioskUnsealed(fs, main))
		})
	}
}

func TestFrontendHonorsKiosk(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	assert.False(t, frontendHonorsKiosk(fs), "no status file")

	writeFrontendStatus(t, fs, `{"active":true,"pid":1}`)
	assert.False(t, frontendHonorsKiosk(fs), "a frontend from before the lock leaves the flag out")

	writeFrontendStatus(t, fs, `{"active":true,"kiosk":true,"pid":1}`)
	assert.True(t, frontendHonorsKiosk(fs))
}
