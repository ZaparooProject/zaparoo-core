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
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func pathFixture() LaunchDefinition {
	return LaunchDefinition{
		Version: 1, ID: "Host.Example.NES", System: "NES", Extensions: []string{".nes"},
		Package: "org.example.launcher", Activity: "org.example.GameActivity",
		Action: "android.intent.action.MAIN", Strategy: "filesystem_path",
		StorageAccess: "legacy_read", MaxTargetSDK: 28, Repair: "Open the launcher settings to repair access.",
		Extras: []LaunchExtra{
			{Name: "ROM", Type: "string", Source: "media"},
			{Name: "CORE", Type: "string", Source: "application_data", Suffix: "cores/example.so"},
		},
	}
}

func contentFixture() LaunchDefinition {
	return LaunchDefinition{
		Version: 2, ID: "Host.Content.PSP", System: "PSP", Extensions: []string{".chd", ".cso", ".iso"},
		Package: "org.example.launcher", Activity: "org.example.GameActivity",
		Action: "android.intent.action.VIEW", Strategy: "content_uri", StorageAccess: "none",
		Repair: "Install or enable the launcher.", DataSource: "media", GrantReadURI: true, ClipData: true,
	}
}

func TestDefinitionRoundTrips(t *testing.T) {
	t.Parallel()
	original := pathFixture()
	data, err := json.Marshal(original)
	require.NoError(t, err)
	definition, err := parseLaunchDefinition(data)
	require.NoError(t, err)
	require.Equal(t, original, definition)
}

func TestDefinitionRejectsUnsupportedOrAmbiguousData(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*LaunchDefinition){
		"version":         func(d *LaunchDefinition) { d.Version = 3 },
		"system":          func(d *LaunchDefinition) { d.System = "unknown" },
		"command":         func(d *LaunchDefinition) { d.Package = "am start; echo bad" },
		"activity":        func(d *LaunchDefinition) { d.Activity = "" },
		"action":          func(d *LaunchDefinition) { d.Action = "arbitrary" },
		"uri":             func(d *LaunchDefinition) { d.Strategy = "content_uri" },
		"storage":         func(d *LaunchDefinition) { d.StorageAccess = "all_files" },
		"sdk":             func(d *LaunchDefinition) { d.MaxTargetSDK = 30 },
		"extensions":      func(d *LaunchDefinition) { d.Extensions = []string{".nes", ".nes"} },
		"extra name":      func(d *LaunchDefinition) { d.Extras[1].Name = "ROM" },
		"extra type":      func(d *LaunchDefinition) { d.Extras[0].Type = "intent" },
		"extra source":    func(d *LaunchDefinition) { d.Extras[1].Source = "shell" },
		"missing media":   func(d *LaunchDefinition) { d.Extras = d.Extras[1:] },
		"duplicate media": func(d *LaunchDefinition) { d.Extras[1].Source = "media"; d.Extras[1].Suffix = "" },
		"media suffix":    func(d *LaunchDefinition) { d.Extras[0].Suffix = "elsewhere" },
		"absolute suffix": func(d *LaunchDefinition) { d.Extras[1].Suffix = "/elsewhere" },
		"traversal":       func(d *LaunchDefinition) { d.Extras[1].Suffix = "cores/../elsewhere" },
		"backslash":       func(d *LaunchDefinition) { d.Extras[1].Suffix = `cores\elsewhere` },
		"empty component": func(d *LaunchDefinition) { d.Extras[1].Suffix = "cores//elsewhere" },
		"nul":             func(d *LaunchDefinition) { d.Extras[1].Suffix = "cores/\x00" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			definition := pathFixture()
			change(&definition)
			require.ErrorIs(t, definition.Validate(), ErrLaunchDefinition)
		})
	}
	valid, err := json.Marshal(pathFixture())
	require.NoError(t, err)
	for _, data := range [][]byte{
		[]byte(`{}`), []byte(`{"version":1,"unknown":true}`),
		append(valid, []byte(`{}`)...), []byte(strings.Repeat(" ", maxLaunchDefinitionBytes+1)),
	} {
		_, err = parseLaunchDefinition(data)
		require.ErrorIs(t, err, ErrLaunchDefinition)
	}
}

func TestContentDefinitionValidation(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(*LaunchDefinition){
		"version":       func(d *LaunchDefinition) { d.Version = 1 },
		"action":        func(d *LaunchDefinition) { d.Action = "android.intent.action.MAIN" },
		"strategy":      func(d *LaunchDefinition) { d.Strategy = "filesystem_path" },
		"storage":       func(d *LaunchDefinition) { d.StorageAccess = "legacy_read" },
		"target sdk":    func(d *LaunchDefinition) { d.MaxTargetSDK = 28 },
		"missing grant": func(d *LaunchDefinition) { d.GrantReadURI = false },
		"missing clip":  func(d *LaunchDefinition) { d.ClipData = false },
		"missing media": func(d *LaunchDefinition) { d.DataSource = "" },
		"bad data":      func(d *LaunchDefinition) { d.DataSource = "path" },
		"duplicate media": func(d *LaunchDefinition) {
			d.Extras = []LaunchExtra{{Name: "bootPath", Type: "string", Source: "media"}}
		},
		"non-media extra": func(d *LaunchDefinition) {
			d.DataSource = ""
			d.Extras = []LaunchExtra{{Name: "path", Type: "string", Source: "application_data"}}
		},
		"media suffix": func(d *LaunchDefinition) {
			d.DataSource = ""
			d.Extras = []LaunchExtra{{Name: "bootPath", Type: "string", Source: "media", Suffix: "bad"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			definition := contentFixture()
			change(&definition)
			require.ErrorIs(t, definition.Validate(), ErrLaunchDefinition)
		})
	}

	dataDefinition := contentFixture()
	require.NoError(t, dataDefinition.Validate())
	extraDefinition := contentFixture()
	extraDefinition.DataSource = ""
	extraDefinition.Extras = []LaunchExtra{{Name: "bootPath", Type: "string", Source: "media"}}
	require.NoError(t, extraDefinition.Validate())
}

func TestDefinitionExtensionBound(t *testing.T) {
	t.Parallel()
	definition := pathFixture()
	definition.Extensions = make([]string, maxLaunchExtensions)
	for i := range definition.Extensions {
		definition.Extensions[i] = ".x" + strings.Repeat("a", i/26) + string(rune('a'+i%26))
	}
	require.NoError(t, definition.Validate())
	definition.Extensions = append(definition.Extensions, ".overflow")
	require.ErrorIs(t, definition.Validate(), ErrLaunchDefinition)
}

func FuzzDefinition(f *testing.F) {
	data, err := json.Marshal(pathFixture())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte(`{"version":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		definition, err := parseLaunchDefinition(data)
		if err == nil {
			require.NoError(t, definition.Validate())
			encoded, marshalErr := json.Marshal(definition)
			require.NoError(t, marshalErr)
			again, parseErr := parseLaunchDefinition(encoded)
			require.NoError(t, parseErr)
			require.Equal(t, definition, again)
		}
	})
}

func appFixture() LaunchDefinition {
	return LaunchDefinition{
		Version: 3, ID: "Example.App", Name: "Example", System: "Android",
		Package: "org.example.app", Activity: "org.example.app.MainActivity",
		Action: actionMain, Strategy: StrategyApp, StorageAccess: "none",
		Repair: "Install or enable the app.",
	}
}

// Data and a custom action are each reserved for the one package whose
// exported activity actually needs them; every other definition must carry
// neither, even though the schema now allows both to exist.
func TestAppDefinitionRestrictsDataAndActionByPackage(t *testing.T) {
	t.Parallel()

	scummVM := appFixture()
	scummVM.Package, scummVM.Activity = scummVMPackage, scummVMActivity
	scummVM.Data = "scummvm:sky"
	require.NoError(t, scummVM.Validate())
	other := scummVM
	other.Package, other.Activity = "org.example.app", "org.example.app.MainActivity"
	require.ErrorIs(t, other.Validate(), ErrLaunchDefinition, "Data is reserved for ScummVM's package")

	gameNative := appFixture()
	gameNative.Package, gameNative.Activity = gameNativePackage, gameNativeActivity
	gameNative.Action = gameNativeAction
	gameNative.Extras = []LaunchExtra{
		{Name: "app_id", Type: "int", Source: extraSourceLiteral, Value: "220"},
		{Name: "game_source", Type: "string", Source: extraSourceLiteral, Value: "STEAM"},
	}
	require.NoError(t, gameNative.Validate())

	otherAction := gameNative
	otherAction.Package, otherAction.Activity = "org.example.app", "org.example.app.MainActivity"
	require.ErrorIs(t, otherAction.Validate(), ErrLaunchDefinition,
		"the custom action is reserved for GameNative's package")

	otherExtra := appFixture()
	otherExtra.Extras = []LaunchExtra{{Name: "app_id", Type: "int", Source: extraSourceLiteral, Value: "220"}}
	require.ErrorIs(t, otherExtra.Validate(), ErrLaunchDefinition, "an int extra is reserved for GameNative's package")
}
