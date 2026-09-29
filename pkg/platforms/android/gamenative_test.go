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

package android

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A GameNative export file's identity is the same plain source path any
// other system's files get, never a scheme built from the store or app ID.
func TestGameNativeLauncherMatchesBySystemFolderAndExtension(t *testing.T) {
	t.Parallel()
	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)
	launchers := platform.Launchers(nil)
	steam := launcherByID(t, launchers, gameNativeSteamID)
	windows := launcherByID(t, launchers, gameNativeWindowsID)

	cfg := &config.Instance{}
	assert.True(t, helpers.PathIsLauncher(cfg, platform, steam, identity(t, "steam", "Half-Life.steam")))
	assert.False(t, helpers.PathIsLauncher(cfg, platform, steam, identity(t, "steam", "Half-Life.epic")))
	assert.True(t, helpers.PathIsLauncher(cfg, platform, windows, identity(t, "windows", "Game.epic")))
	assert.True(t, helpers.PathIsLauncher(cfg, platform, windows, identity(t, "windows", "Game.gog")))
	assert.True(t, helpers.PathIsLauncher(cfg, platform, windows, identity(t, "windows", "Game.amazon")))
	assert.True(t, helpers.PathIsLauncher(cfg, platform, windows, identity(t, "windows", "Game.pcgame")))
	assert.False(t, helpers.PathIsLauncher(cfg, platform, windows, identity(t, "windows", "Game.steam")))
}

// The store comes from the file's own extension, and the app ID from its
// content; the display name is the file's own name, nothing custom.
func TestGameNativeDispatchReadsStoreFromExtensionAndAppIDFromContent(t *testing.T) {
	t.Parallel()
	host := &fakeHost{files: map[string][]byte{
		"steam/Half-Life.steam": []byte("220\n"),
		"windows/Game.epic":     []byte(" 12345 "),
	}}
	platform := startedPlatform(t.Context(), t, host)
	steamEntry := &gameNativeEntries[0]
	windowsEntry := &gameNativeEntries[1]

	require.NoError(t, platform.dispatchGameNative(steamEntry, identity(t, "steam", "Half-Life.steam")))
	require.NoError(t, platform.dispatchGameNative(windowsEntry, identity(t, "windows", "Game.epic")))

	require.Len(t, host.dispatched, 2)
	steamSent := host.dispatched[0].definition
	assert.Equal(t, gameNativePackage, steamSent.Package)
	assert.Equal(t, gameNativeAction, steamSent.Action)
	require.Len(t, steamSent.Extras, 2)
	assert.Equal(t,
		LaunchExtra{Name: "app_id", Type: "int", Source: extraSourceLiteral, Value: "220"}, steamSent.Extras[0])
	assert.Equal(t,
		LaunchExtra{Name: "game_source", Type: "string", Source: extraSourceLiteral, Value: "STEAM"},
		steamSent.Extras[1])

	windowsSent := host.dispatched[1].definition
	assert.Equal(t, "12345", windowsSent.Extras[0].Value)
	assert.Equal(t, "EPIC", windowsSent.Extras[1].Value)
}

func TestGameNativeAppIDBounds(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"empty":            "",
		"zero":             "0",
		"negative":         "-5",
		"not a number":     "abc",
		"too many digits":  "99999999999",
		"overflows an int": "9999999999",
		"decimal":          "1.5",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := canonicalAppID([]byte(content))
			assert.False(t, ok, content)
		})
	}
	id, ok := canonicalAppID([]byte(" 220 \n"))
	require.True(t, ok)
	assert.Equal(t, "220", id)

	// A leading zero is accepted and normalized away, not rejected.
	id, ok = canonicalAppID([]byte("0220"))
	require.True(t, ok)
	assert.Equal(t, "220", id)
}

func TestGameNativeDispatchRefusesAnInvalidExport(t *testing.T) {
	t.Parallel()
	host := &fakeHost{files: map[string][]byte{"steam/Bad.steam": []byte("not-a-number")}}
	platform := startedPlatform(t.Context(), t, host)

	err := platform.dispatchGameNative(&gameNativeEntries[0], identity(t, "steam", "Bad.steam"))

	require.EqualError(t, err, msgWrongMedia)
	assert.Empty(t, host.dispatched)
}

func TestGameNativeDispatchReportsAnUnreadableFile(t *testing.T) {
	t.Parallel()
	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)

	err := platform.dispatchGameNative(&gameNativeEntries[0], identity(t, "steam", "Gone.steam"))

	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, err, &repair)
	assert.Equal(t, repairMessage(FailureSourceUnavailable, ""), repair.Error())
}
