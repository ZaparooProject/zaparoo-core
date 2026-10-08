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
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceStatus_UnsupportedBeforeTheHostReports(t *testing.T) {
	t.Parallel()

	p := &Platform{}

	_, err := p.DevicePower()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = p.DeviceNetwork()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = p.DeviceBluetooth()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = p.DeviceStorage()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = p.DeviceDisplay()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = p.DeviceControllers()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
	_, err = p.DeviceSystem()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)

	status, err := p.PowerStatus()
	require.NoError(t, err)
	assert.Equal(t, power.SourceUnknown, status.Source, "the update gate must not assume a full battery")
}

func TestDeviceStatus_ReportsWhatTheHostPushed(t *testing.T) {
	t.Parallel()

	p := &Platform{}
	percent := 42
	enabled := true
	p.SetDeviceStatus(&DeviceStatus{
		Power: &power.Detail{
			Present: true, Percent: &percent, Source: power.SourceBattery, State: power.ChargeDischarging,
		},
		Network:     &hoststatus.Network{Type: hoststatus.LinkWifi, Internet: hoststatus.InternetFull},
		Bluetooth:   &hoststatus.Bluetooth{Present: true, Powered: &enabled},
		Controllers: []hoststatus.Controller{},
		Storage:     []hoststatus.Volume{{Path: "internal", Total: 10, Free: 4, Used: 6}},
		System:      &hoststatus.System{Model: "Handheld"},
	})

	detail, err := p.DevicePower()
	require.NoError(t, err)
	assert.Equal(t, 42, *detail.Percent)

	network, err := p.DeviceNetwork()
	require.NoError(t, err)
	assert.Equal(t, hoststatus.InternetFull, network.Internet)
	assert.True(t, network.InternetAuthoritative, "the host's reachability answer is never second-guessed by a probe")

	bluetooth, err := p.DeviceBluetooth()
	require.NoError(t, err)
	assert.True(t, *bluetooth.Powered)

	controllers, err := p.DeviceControllers()
	require.NoError(t, err, "an empty list is a report that none are connected")
	assert.Empty(t, controllers)

	volumes, err := p.DeviceStorage()
	require.NoError(t, err)
	assert.Len(t, volumes, 1)

	system, err := p.DeviceSystem()
	require.NoError(t, err)
	assert.Equal(t, "Handheld", system.Model)

	_, err = p.DeviceDisplay()
	require.ErrorIs(t, err, hoststatus.ErrUnsupported, "a section the host leaves out stays unsupported")
}

func TestDeviceStatus_PowerStatusForTheUpdateGate(t *testing.T) {
	t.Parallel()

	percent := 15
	tests := []struct {
		detail *power.Detail
		name   string
		want   power.Status
	}{
		{
			name:   "no battery",
			detail: &power.Detail{Source: power.SourceExternal},
			want:   power.Status{Source: power.SourceNoBattery},
		},
		{
			name:   "charging",
			detail: &power.Detail{Present: true, Percent: &percent, Source: power.SourceExternal},
			want:   power.Status{Source: power.SourceExternal},
		},
		{
			name:   "on battery",
			detail: &power.Detail{Present: true, Percent: &percent, Source: power.SourceBattery},
			want:   power.Status{Source: power.SourceBattery, Percent: 15},
		},
		{
			name:   "on battery with no reading",
			detail: &power.Detail{Present: true, Source: power.SourceBattery},
			want:   power.Status{Source: power.SourceUnknown},
		},
		{
			name:   "supply not known",
			detail: &power.Detail{Present: true, Percent: &percent},
			want:   power.Status{Source: power.SourceUnknown},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &Platform{}
			p.SetDeviceStatus(&DeviceStatus{Power: tt.detail})
			got, err := p.PowerStatus()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDeviceStatus_NotifiesOutsideTheLock(t *testing.T) {
	t.Parallel()

	p := &Platform{}
	calls := 0
	p.SetDeviceStatusChanged(func() {
		calls++
		// Reading back from inside the callback must not deadlock.
		_, _ = p.DevicePower()
	})
	p.SetDeviceStatus(&DeviceStatus{})
	p.SetDeviceStatus(&DeviceStatus{})
	assert.Equal(t, 2, calls)

	p.SetDeviceStatusChanged(nil)
	p.SetDeviceStatus(&DeviceStatus{})
	assert.Equal(t, 2, calls)
}

func TestDeviceStatus_NoPowerActions(t *testing.T) {
	t.Parallel()

	p := &Platform{}
	assert.Empty(t, p.PowerActions(context.Background()))
	_, err := p.PreparePowerAction(context.Background(), hoststatus.PowerReboot)
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
}
