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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scan(t *testing.T, launcher *platforms.Launcher) []platforms.ScanResult {
	t.Helper()
	require.NotNil(t, launcher.Scanner, launcher.ID)
	results, err := launcher.Scanner(t.Context(), nil, "", nil)
	require.NoError(t, err)
	return results
}

// A profiled variant is offered as its own media entry, identified by the
// package and the profile's variant key, never by the intent that starts it.
func TestAppVariantIsScannedAsItsOwnIdentity(t *testing.T) {
	t.Parallel()

	platform := startedPlatform(t.Context(), t, &fakeHost{})
	red := launcherByID(t, platform.Launchers(nil), "Pokeport.PokemonRed")

	assert.Equal(t, []string{"android"}, red.Schemes)
	assert.Equal(t, "Android", red.SystemID)
	assert.Empty(t, red.Extensions, "an app never matches a file")

	results := scan(t, red)
	require.Len(t, results, 1)
	assert.Equal(t, "android://com.theboisclub.pokemonred:red/Pokemon%20Red", results[0].Path)
	assert.Equal(t, "Pokemon Red", results[0].Name)
	assert.True(t, results[0].NoExt)

	assert.True(t, red.Test(nil, results[0].Path))
	assert.False(t, red.Test(nil, "android://com.theboisclub.pokemonred:blue/Pokemon%20Blue"))
	assert.False(t, red.Test(nil, "android://com.theboisclub.pokemonred/Pokeport"))
}

// The literal extra reaches the host, and no media ever does.
func TestAppDispatchSendsTheProfileLiteralAndNoMedia(t *testing.T) {
	t.Parallel()

	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)
	crystal := launcherByID(t, platform.Launchers(nil), "Pokeport.PokemonCrystal")

	_, err := crystal.Launch(nil, "android://com.theboisclub.pokemonred:crystal/Pokemon%20Crystal", nil)
	require.NoError(t, err)

	require.Len(t, host.dispatched, 1)
	sent := host.dispatched[0]
	assert.Empty(t, sent.reference, "an app launch carries no media")
	assert.Empty(t, sent.segments, "an app launch carries no media")
	assert.Equal(t, StrategyApp, sent.definition.Strategy)
	assert.Equal(t, "com.theboisclub.pokemonred", sent.definition.Package)
	require.Len(t, sent.definition.Extras, 1)
	assert.Equal(t, "game", sent.definition.Extras[0].Name)
	assert.Equal(t, "crystal", sent.definition.Extras[0].Value)
	assert.Equal(t, extraSourceLiteral, sent.definition.Extras[0].Source)
}

// An app launch must refuse rather than dispatch with no way to cancel it,
// whether the host itself is missing or the launcher context simply has not
// been supplied yet (before StartPost, or after Stop on a reused Platform).
func TestAppDispatchRefusesBeforeTheHostIsReady(t *testing.T) {
	t.Parallel()

	noHost, err := New(platforms.Settings{DataDir: "/data"}, nil)
	require.NoError(t, err)
	entry, ok := noHost.entryByID["Pokeport.PokemonCrystal"]
	require.True(t, ok)
	require.Error(t, noHost.dispatchApp(entry, "android://com.theboisclub.pokemonred:crystal/Pokemon%20Crystal"),
		"no host at all must refuse")

	notStarted, err := New(platforms.Settings{DataDir: "/data"}, &fakeHost{})
	require.NoError(t, err)
	entry, ok = notStarted.entryByID["Pokeport.PokemonCrystal"]
	require.True(t, ok)
	require.Error(t, notStarted.dispatchApp(entry, "android://com.theboisclub.pokemonred:crystal/Pokemon%20Crystal"),
		"a host with no launcher context yet must also refuse, not fall back to a background context")
}

// Every launchable app the host reports is offered, except one a profile
// already describes, so an app is never listed twice.
func TestInstalledAppsLauncherSkipsProfiledPackages(t *testing.T) {
	t.Parallel()

	host := &fakeHost{
		appsScanned: true,
		apps: []AppInfo{
			{Package: "com.example.notes", Activity: "com.example.notes.Main", Label: "Notes"},
			{Package: "com.theboisclub.pokemonred", Activity: "org.love2d.android.GameActivity", Label: "Pokeport"},
		},
	}
	platform := startedPlatform(t.Context(), t, host)
	apps := launcherByID(t, platform.Launchers(nil), installedAppsID)

	results := scan(t, apps)
	require.Len(t, results, 1, "the profiled package is left to its own launcher")
	assert.Equal(t, "android://com.example.notes/Notes", results[0].Path)

	assert.True(t, apps.Test(nil, "android://com.example.notes/Notes"))
	assert.False(t, apps.Test(nil, "android://com.theboisclub.pokemonred/Pokeport"))
	assert.False(t, apps.Test(nil, "android://com.example.notes:red/Notes"))
}

// A listing the host never made is not evidence that no app is installed.
func TestInstalledAppsLauncherTrustsOnlyAScannedListing(t *testing.T) {
	t.Parallel()

	host := &fakeHost{apps: []AppInfo{
		{Package: "com.example.notes", Activity: "com.example.notes.Main", Label: "Notes"},
	}}
	platform := startedPlatform(t.Context(), t, host)
	apps := launcherByID(t, platform.Launchers(nil), installedAppsID)
	assert.Empty(t, scan(t, apps))
}

func TestInstalledAppDispatchNamesTheHostReportedActivity(t *testing.T) {
	t.Parallel()

	host := &fakeHost{appsScanned: true, apps: []AppInfo{
		{Package: "com.example.notes", Activity: "com.example.notes.Main", Label: "Notes"},
	}}
	platform := startedPlatform(t.Context(), t, host)
	apps := launcherByID(t, platform.Launchers(nil), installedAppsID)

	_, err := apps.Launch(nil, "android://com.example.notes/Notes", nil)
	require.NoError(t, err)
	require.Len(t, host.dispatched, 1)
	assert.Equal(t, "com.example.notes.Main", host.dispatched[0].definition.Activity)
	assert.Equal(t, actionMain, host.dispatched[0].definition.Action)

	_, err = apps.Launch(nil, "android://com.example.absent/Gone", nil)
	require.Error(t, err, "an app the host never reported cannot be started")
}

// LaunchMedia rebuilds the launcher itself before reaching the host. The
// generic apps launcher is not a catalog entry, so it has to be rebuilt from
// what the host reports instead of being rejected as unregistered.
func TestLaunchMediaStartsAnInstalledApp(t *testing.T) {
	t.Parallel()

	host := &fakeHost{appsScanned: true, apps: []AppInfo{
		{Package: "com.example.notes", Activity: "com.example.notes.Main", Label: "Notes"},
	}}
	platform := startedPlatform(t.Context(), t, host)
	apps := launcherByID(t, platform.Launchers(nil), installedAppsID)

	require.NoError(t, platform.LaunchMedia(nil, "android://com.example.notes/Notes", apps, nil, nil))
	require.Len(t, host.dispatched, 1)
	assert.Equal(t, "com.example.notes", host.dispatched[0].definition.Package)
	assert.Equal(t, "com.example.notes.Main", host.dispatched[0].definition.Activity)
}

// A profiled variant still goes through the catalog path.
func TestLaunchMediaStartsAProfiledVariant(t *testing.T) {
	t.Parallel()

	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)
	red := launcherByID(t, platform.Launchers(nil), "Pokeport.PokemonRed")

	require.NoError(t, platform.LaunchMedia(
		nil, "android://com.theboisclub.pokemonred:red/Pokemon%20Red", red, nil, nil))
	require.Len(t, host.dispatched, 1)
	require.Len(t, host.dispatched[0].definition.Extras, 1)
	assert.Equal(t, "red", host.dispatched[0].definition.Extras[0].Value)
}
