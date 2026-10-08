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

package methods

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupcoordinator "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup/coordinator"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/device"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakePowerControl is a device that can reboot, may not shut down, and cannot
// suspend.
type fakePowerControl struct {
	committed chan hoststatus.PowerAction
	commitErr error
}

func newFakePowerControl() *fakePowerControl {
	return &fakePowerControl{committed: make(chan hoststatus.PowerAction, 4)}
}

func (*fakePowerControl) PowerActions(context.Context) map[hoststatus.PowerAction]hoststatus.Availability {
	return map[hoststatus.PowerAction]hoststatus.Availability{
		hoststatus.PowerReboot:   hoststatus.Supported,
		hoststatus.PowerShutdown: hoststatus.NotPermitted,
	}
}

func (c *fakePowerControl) PreparePowerAction(_ context.Context, action hoststatus.PowerAction) (func() error, error) {
	switch action {
	case hoststatus.PowerReboot:
		return func() error {
			c.committed <- action
			return c.commitErr
		}, nil
	case hoststatus.PowerShutdown:
		return nil, hoststatus.ErrNotPermitted
	default:
		return nil, hoststatus.ErrUnsupported
	}
}

func deviceTestEnv(t *testing.T, control hoststatus.PowerController) requests.RequestEnv {
	t.Helper()

	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test")
	pl.On("Settings").Return(platforms.Settings{DataDir: "/data/zaparoo"})
	pl.On("RootDirs", mock.Anything).Return([]string{"/media/games"})

	percent := 55
	monitor := device.NewMonitor(&device.Options{
		Platform: pl,
		Clock:    clockwork.NewFakeClockAt(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)),
		Readers: hoststatus.Readers{
			Power: func() (power.Detail, error) {
				return power.Detail{
					Present: true, Percent: &percent, Source: power.SourceExternal, State: power.ChargeCharging,
					Batteries: []power.Battery{{ID: "BAT0", Percent: &percent, State: power.ChargeCharging}},
				}, nil
			},
			Network: func() (hoststatus.Network, error) {
				return hoststatus.Network{
					Type: hoststatus.LinkWired, Interface: "eth0",
					Internet: hoststatus.InternetFull, InternetAuthoritative: true,
					Interfaces: []hoststatus.Interface{{
						Name: "eth0", Type: hoststatus.LinkWired, Up: true, Addresses: []string{"10.0.0.5"},
					}},
				}, nil
			},
			System:       func() (hoststatus.System, error) { return hoststatus.System{Hostname: "box"}, nil },
			PowerControl: control,
		},
	})

	st, _ := state.NewState(pl, "boot")
	t.Cleanup(st.StopService)
	st.SetDeviceMonitor(monitor)

	return requests.RequestEnv{
		Context:  t.Context(),
		Platform: pl,
		State:    st,
		IsLocal:  true,
	}
}

func TestHandleDeviceStatus(t *testing.T) {
	t.Parallel()

	env := deviceTestEnv(t, newFakePowerControl())
	result, err := HandleDeviceStatus(env)
	require.NoError(t, err)

	response, ok := result.(models.DeviceStatusResponse)
	require.True(t, ok)

	require.NotNil(t, response.Power)
	assert.Equal(t, 55, *response.Power.Percent, "the charge is reported while charging")
	assert.Equal(t, "external", *response.Power.Source)
	assert.Equal(t, "charging", *response.Power.ChargeState)

	require.NotNil(t, response.Network)
	assert.Equal(t, "wired", response.Network.Type)
	assert.Equal(t, "full", *response.Network.Internet)
	assert.Equal(t, []string{"10.0.0.5"}, response.Network.Interfaces[0].Addresses)

	assert.Equal(t, "box", response.System.Hostname)
	assert.Equal(t, "test", response.System.Platform)
	assert.True(t, response.Time.ClockReliable)

	assert.Nil(t, response.Bluetooth)
	assert.Nil(t, response.Display)
	assert.Nil(t, response.Controllers)
	assert.Equal(t, models.DeviceUnsupported, response.Capabilities.Sections[models.DeviceSectionBluetooth])
	assert.Equal(t, models.DeviceSupported, response.Capabilities.Sections[models.DeviceSectionPower])

	assert.Equal(t, map[string]string{
		models.MethodDevicePowerReboot:   models.DeviceSupported,
		models.MethodDevicePowerShutdown: models.DeviceNotPermitted,
	}, response.Capabilities.Actions, "an action the device cannot do is left out")
}

func TestHandleDeviceStatus_Access(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clientRole  string
		wantReboot  string
		isLocal     bool
		wantAllowed bool
	}{
		{name: "local", isLocal: true, wantAllowed: true, wantReboot: models.DeviceSupported},
		{
			name: "paired admin", clientRole: string(permissions.RoleAdmin),
			wantAllowed: true, wantReboot: models.DeviceSupported,
		},
		{
			name: "paired member may look but not touch", clientRole: string(permissions.RoleMember),
			wantAllowed: true, wantReboot: models.DeviceNotPermitted,
		},
		{name: "unpaired remote is refused"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := deviceTestEnv(t, newFakePowerControl())
			env.IsLocal = tt.isLocal
			env.ClientRole = tt.clientRole

			result, err := HandleDeviceStatus(env)
			if !tt.wantAllowed {
				require.ErrorIs(t, err, ErrForbidden)
				return
			}
			require.NoError(t, err)
			response, ok := result.(models.DeviceStatusResponse)
			require.True(t, ok)
			assert.Equal(t, tt.wantReboot, response.Capabilities.Actions[models.MethodDevicePowerReboot])
		})
	}
}

func TestHandleDeviceStatus_NoMonitor(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	st, _ := state.NewState(pl, "boot")
	t.Cleanup(st.StopService)

	_, err := HandleDeviceStatus(requests.RequestEnv{Context: t.Context(), Platform: pl, State: st, IsLocal: true})
	var catErr *models.CategorizedError
	require.ErrorAs(t, err, &catErr)
	assert.Equal(t, models.ErrorCategoryUnavailable, catErr.Category)
}

// Rebooting stops the device for everyone using it, so it needs the capability
// and a request from the device itself or from a paired admin. An unpaired
// remote request must be refused in its own right.
func TestHandleDevicePower_Authorization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clientRole  string
		isLocal     bool
		wantAllowed bool
	}{
		{name: "paired member is refused", clientRole: string(permissions.RoleMember)},
		{name: "unknown role degrades to member and is refused", clientRole: "superuser"},
		{name: "unpaired remote is refused"},
		{name: "online remote operation is refused", clientRole: string(permissions.RoleRemote)},
		{name: "paired admin is allowed", clientRole: string(permissions.RoleAdmin), wantAllowed: true},
		{name: "local is allowed", isLocal: true, wantAllowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			control := newFakePowerControl()
			env := deviceTestEnv(t, control)
			env.IsLocal = tt.isLocal
			env.ClientRole = tt.clientRole

			result, err := HandleDevicePower(hoststatus.PowerReboot)(env)
			if !tt.wantAllowed {
				require.ErrorIs(t, err, ErrForbidden)
				assert.Nil(t, result)
				assert.Empty(t, control.committed)
				return
			}
			require.NoError(t, err)
			response, ok := result.(models.ResponseWithCallback)
			require.True(t, ok)
			response.AfterWrite()
			assert.Equal(t, hoststatus.PowerReboot, awaitCommit(t, control))
		})
	}
}

func awaitCommit(t *testing.T, control *fakePowerControl) hoststatus.PowerAction {
	t.Helper()
	select {
	case action := <-control.committed:
		return action
	case <-time.After(5 * time.Second):
		t.Fatal("the power action was never carried out")
		return ""
	}
}

func TestHandleDevicePower_WaitsForTheResponse(t *testing.T) {
	t.Parallel()

	control := newFakePowerControl()
	control.commitErr = errors.New("logged, not returned")
	env := deviceTestEnv(t, control)

	result, err := HandleDevicePower(hoststatus.PowerReboot)(env)
	require.NoError(t, err)
	response, ok := result.(models.ResponseWithCallback)
	require.True(t, ok)
	assert.Equal(t, NoContent{}, response.Result)
	assert.Empty(t, control.committed, "nothing happens before the response is written")

	response.AfterWrite()
	assert.Equal(t, hoststatus.PowerReboot, awaitCommit(t, control))

	// A second callback, or the fallback timer, must not repeat the action.
	response.AfterWrite()
	select {
	case <-control.committed:
		t.Fatal("the power action ran twice")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHandleDevicePower_Refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		action   hoststatus.PowerAction
		category string
	}{
		{
			name: "operating system refuses", action: hoststatus.PowerShutdown,
			category: models.ErrorCategoryNotPermitted,
		},
		{name: "device cannot", action: hoststatus.PowerSuspend, category: models.ErrorCategoryUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			control := newFakePowerControl()
			result, err := HandleDevicePower(tt.action)(deviceTestEnv(t, control))
			assert.Nil(t, result)
			var catErr *models.CategorizedError
			require.ErrorAs(t, err, &catErr)
			assert.Equal(t, tt.category, catErr.Category)
			assert.Empty(t, control.committed)
		})
	}
}

func TestHandleDevicePower_BusyDuringBackup(t *testing.T) {
	t.Parallel()

	control := newFakePowerControl()
	env := deviceTestEnv(t, control)
	lease, err := env.State.BackupCoordinator().Begin(
		t.Context(), backupcoordinator.OperationLocalRestore, backupcoordinator.OperationRead,
	)
	require.NoError(t, err)

	result, err := HandleDevicePower(hoststatus.PowerReboot)(env)
	assert.Nil(t, result)
	var catErr *models.CategorizedError
	require.ErrorAs(t, err, &catErr)
	assert.Equal(t, models.ErrorCategoryBusy, catErr.Category)

	lease.Release()
	_, err = HandleDevicePower(hoststatus.PowerReboot)(env)
	require.NoError(t, err)
}

func TestHandleDevicePower_NoMonitor(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	st, _ := state.NewState(pl, "boot")
	t.Cleanup(st.StopService)

	env := requests.RequestEnv{Context: t.Context(), Platform: pl, State: st, IsLocal: true}
	_, err := HandleDevicePower(hoststatus.PowerReboot)(env)
	var catErr *models.CategorizedError
	require.ErrorAs(t, err, &catErr)
	assert.Equal(t, models.ErrorCategoryUnavailable, catErr.Category)
}
