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

package device

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fullSnapshot() Snapshot {
	percent := 96
	docked := true
	powered := true
	padPercent := 80
	return Snapshot{
		Power: &power.Detail{
			Present: true, Percent: &percent, Source: power.SourceExternal, State: power.ChargeCharging,
			Batteries: []power.Battery{{ID: "BAT1", Percent: &percent, State: power.ChargeCharging}},
		},
		Network: &hoststatus.Network{
			Type: hoststatus.LinkWifi, Interface: "wlan0", Internet: hoststatus.InternetFull,
			Interfaces: []hoststatus.Interface{
				{Name: "wlan0", Type: hoststatus.LinkWifi, Up: true, Addresses: []string{"192.168.1.20"}},
				{Name: "eth0", Type: hoststatus.LinkWired},
			},
		},
		Bluetooth: &hoststatus.Bluetooth{Present: true, Powered: &powered},
		Display: &hoststatus.Display{
			InternalPanel: true, ExternalConnected: true, ExternalActive: true, Docked: &docked,
		},
		System: &hoststatus.System{Hostname: "steamdeck", Model: "Jupiter"},
		Storage: []hoststatus.Volume{{
			Path: "/home/deck", Roles: []string{"media", "data"}, Total: 100, Free: 30, Used: 70,
		}},
		StorageKnown: true,
		Controllers: []hoststatus.Controller{{
			ID: "input17", Name: "DualSense Wireless Controller", VendorID: "054c", ProductID: "0ce6",
			Connection: hoststatus.ConnectionBluetooth,
			Battery:    &hoststatus.ControllerBattery{Percent: &padPercent, Level: hoststatus.BatteryLevelFull},
		}, {ID: "xinput0"}},
		ControllersKnown: true,
		Sections:         map[string]hoststatus.Availability{models.DeviceSectionPower: hoststatus.Supported},
		Timezone:         "Australia/Perth",
		UTCOffset:        28800,
		PlatformID:       "steamos",
		ClockReliable:    true,
	}
}

func TestStatusResponse(t *testing.T) {
	t.Parallel()

	snap := fullSnapshot()
	response := StatusResponse(&snap, map[string]string{models.MethodDevicePowerReboot: models.DeviceSupported})

	assert.Equal(t, map[string]string{models.DeviceSectionPower: "supported"}, response.Capabilities.Sections)
	assert.Equal(t, map[string]string{"device.power.reboot": "supported"}, response.Capabilities.Actions)

	require.NotNil(t, response.Power)
	assert.Equal(t, 96, *response.Power.Percent)
	assert.Equal(t, "external", *response.Power.Source)
	assert.Equal(t, "charging", *response.Power.ChargeState)
	assert.Nil(t, response.Power.TimeRemaining)
	require.Len(t, response.Power.Batteries, 1)

	require.NotNil(t, response.Network)
	assert.Equal(t, "wlan0", *response.Network.Interface)
	assert.Equal(t, []string{"192.168.1.20"}, response.Network.Interfaces[0].Addresses)
	assert.Equal(t, []string{}, response.Network.Interfaces[1].Addresses, "no addresses is an empty list, not null")

	require.NotNil(t, response.Controllers)
	assert.Equal(t, 2, response.Controllers.Count)
	bare := response.Controllers.Items[1]
	assert.Nil(t, bare.Name)
	assert.Nil(t, bare.VendorID)
	assert.Nil(t, bare.Battery)
	assert.Equal(t, hoststatus.ConnectionUnknown, bare.Connection)

	assert.Equal(t, "steamos", response.System.Platform)
	assert.Equal(t, "Jupiter", *response.System.Model)
	assert.Equal(t, "Australia/Perth", *response.Time.Timezone)
	assert.Equal(t, 28800, response.Time.UTCOffset)
	assert.True(t, *response.Display.Docked)
	assert.Equal(t, uint64(30), response.Storage.Volumes[0].FreeBytes)
}

func TestStatusResponse_UnknownValuesAreNull(t *testing.T) {
	t.Parallel()

	snap := Snapshot{
		Power:   &power.Detail{Present: true, Batteries: []power.Battery{{ID: "battery"}}},
		Network: &hoststatus.Network{Type: hoststatus.LinkNone},
		System:  &hoststatus.System{Hostname: "mister"},
	}
	encoded, err := json.Marshal(StatusResponse(&snap, nil))
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	powerSection, ok := decoded["power"].(map[string]any)
	require.True(t, ok)
	assert.Nil(t, powerSection["percent"])
	assert.Nil(t, powerSection["source"])
	assert.Nil(t, powerSection["chargeState"])
	assert.NotContains(t, powerSection, "timeRemaining")

	network, ok := decoded["network"].(map[string]any)
	require.True(t, ok)
	assert.Nil(t, network["interface"])
	assert.Nil(t, network["internet"])
	assert.Equal(t, []any{}, network["interfaces"])

	system, ok := decoded["system"].(map[string]any)
	require.True(t, ok)
	assert.Nil(t, system["model"])

	// Every section key is present even when it has nothing to say.
	for _, section := range []string{"bluetooth", "storage", "display", "controllers"} {
		value, present := decoded[section]
		assert.True(t, present, section)
		assert.Nil(t, value, section)
	}
	capabilities, ok := decoded["capabilities"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{}, capabilities["actions"])
}

// The notification reaches every connected client, including ones on an
// unencrypted connection, so nothing that identifies the device on its
// network may be in it.
func TestChangedParams_CarriesNothingIdentifying(t *testing.T) {
	t.Parallel()

	snap := fullSnapshot()
	encoded, err := json.Marshal(ChangedParams(&snap))
	require.NoError(t, err)
	text := string(encoded)

	secrets := []string{
		"192.168.1.20", "wlan0", "eth0", "steamdeck", "Jupiter", "/home/deck", "Australia/Perth",
	}
	for _, secret := range secrets {
		assert.NotContains(t, text, secret)
	}

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.ElementsMatch(t, []string{"power", "network", "bluetooth", "display", "controllers", "time"}, keys(decoded))
	assert.ElementsMatch(t, []string{"type", "internet"}, keys(decoded["network"]))
	assert.ElementsMatch(t, []string{"present", "percent", "source", "chargeState"}, keys(decoded["power"]))
	assert.ElementsMatch(t, []string{"docked"}, keys(decoded["display"]))
	assert.ElementsMatch(t, []string{"clockReliable"}, keys(decoded["time"]))
}

func keys(value any) []string {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	return names
}

func TestLocalZone(t *testing.T) {
	t.Parallel()

	perth, err := time.LoadLocation("Australia/Perth")
	require.NoError(t, err)
	name, offset := localZone(time.Date(2026, 10, 8, 12, 0, 0, 0, perth))
	assert.Equal(t, "Australia/Perth", name)
	assert.Equal(t, 8*60*60, offset)

	name, offset = localZone(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	assert.Equal(t, "UTC", name)
	assert.Zero(t, offset)
}
