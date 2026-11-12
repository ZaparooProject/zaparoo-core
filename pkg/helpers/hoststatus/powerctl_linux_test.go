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

package hoststatus

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func logindKey(method string) string {
	return logindService + " " + logindManager + "." + method
}

func newLinuxPower(bus busCaller, executor *recordingExecutor, euid int, programs ...string) *LinuxPowerControl {
	lookPath := func(name string) (string, error) {
		for _, program := range programs {
			if program == name {
				return "/sbin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	control := NewLinuxPowerControl(executor, lookPath, func() int { return euid })
	control.bus = bus
	return control
}

func TestLinuxPowerControl_Logind(t *testing.T) {
	t.Parallel()

	bus := &fakeBus{replies: map[string][]any{
		logindKey("CanReboot"):   {"yes"},
		logindKey("CanPowerOff"): {"challenge"},
		logindKey("CanSuspend"):  {"na"},
		logindKey("Reboot"):      {},
	}}
	executor := &recordingExecutor{}
	control := newLinuxPower(bus, executor, 1000, "reboot", "poweroff")

	assert.Equal(t, map[PowerAction]Availability{
		PowerReboot:   Supported,
		PowerShutdown: NotPermitted,
	}, control.PowerActions(context.Background()))

	_, err := control.PreparePowerAction(context.Background(), PowerShutdown)
	require.ErrorIs(t, err, ErrNotPermitted)
	_, err = control.PreparePowerAction(context.Background(), PowerSuspend)
	require.ErrorIs(t, err, ErrUnsupported)

	commit, err := control.PreparePowerAction(context.Background(), PowerReboot)
	require.NoError(t, err)
	assert.NotContains(t, bus.calls, logindKey("Reboot"), "nothing happens until commit")
	require.NoError(t, commit())
	assert.Contains(t, bus.calls, logindKey("Reboot"))
	assert.Empty(t, executor.ran, "the login manager is used instead of running commands")
}

func TestLinuxPowerControl_FallbackAsRoot(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	control := newLinuxPower(&fakeBus{}, executor, 0, "reboot", "poweroff")

	assert.Equal(t, map[PowerAction]Availability{
		PowerReboot:   Supported,
		PowerShutdown: Supported,
	}, control.PowerActions(context.Background()))

	commit, err := control.PreparePowerAction(context.Background(), PowerShutdown)
	require.NoError(t, err)
	require.NoError(t, commit())
	assert.Equal(t, [][]string{{"/sbin/poweroff"}}, executor.ran)

	_, err = control.PreparePowerAction(context.Background(), PowerSuspend)
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestLinuxPowerControl_FallbackUnprivileged(t *testing.T) {
	t.Parallel()

	control := newLinuxPower(&fakeBus{}, &recordingExecutor{}, 1000, "reboot")
	assert.Equal(t, map[PowerAction]Availability{PowerReboot: NotPermitted}, control.PowerActions(context.Background()))
	_, err := control.PreparePowerAction(context.Background(), PowerReboot)
	require.ErrorIs(t, err, ErrNotPermitted)
}

func TestLinuxPowerControl_UnknownAction(t *testing.T) {
	t.Parallel()

	control := newLinuxPower(&fakeBus{}, &recordingExecutor{}, 0, "reboot")
	_, err := control.PreparePowerAction(context.Background(), PowerAction("hibernate"))
	require.ErrorIs(t, err, ErrUnsupported)
}

// recordingObject is a bus object that records the flags of every call made
// on it.
type recordingObject struct {
	dbus.BusObject
	flags   *[]dbus.Flags
	methods *[]string
	service string
	owned   bool
}

func (o *recordingObject) CallWithContext(_ context.Context, method string, flags dbus.Flags, _ ...any) *dbus.Call {
	*o.flags = append(*o.flags, flags)
	*o.methods = append(*o.methods, o.service+" "+method)
	if method == dbusNameHasOwner {
		return &dbus.Call{Body: []any{o.owned}}
	}
	return &dbus.Call{Body: []any{dbus.MakeVariant(true)}}
}

func newRecordingBus(owned bool) (bus *systemBus, flags *[]dbus.Flags, methods *[]string) {
	flags = &[]dbus.Flags{}
	methods = &[]string{}
	bus = &systemBus{}
	bus.objects = func(service string, _ dbus.ObjectPath) (dbus.BusObject, error) {
		return &recordingObject{owned: owned, service: service, flags: flags, methods: methods}, nil
	}
	return bus, flags, methods
}

// Asking a service that is not running makes the bus start it, and a freshly
// started bluetoothd powers its adapter on. Core must never be the cause.
func TestSystemBus_NeverStartsAService(t *testing.T) {
	t.Parallel()

	t.Run("a service with no owner is never called", func(t *testing.T) {
		t.Parallel()
		bus, flags, methods := newRecordingBus(false)
		_, err := busProperty[bool](context.Background(), bus, bluezService, "/org/bluez/hci0", bluezAdapter, "Powered")
		require.ErrorIs(t, err, errNoService)
		assert.Equal(t, []string{dbusService + " " + dbusNameHasOwner}, *methods)
		for _, flag := range *flags {
			assert.NotZero(t, flag&dbus.FlagNoAutoStart)
		}
	})

	t.Run("every call forbids activation", func(t *testing.T) {
		t.Parallel()
		bus, flags, methods := newRecordingBus(true)
		powered, err := busProperty[bool](
			context.Background(), bus, bluezService, "/org/bluez/hci0", bluezAdapter, "Powered")
		require.NoError(t, err)
		assert.True(t, powered)
		assert.Equal(t, []string{
			dbusService + " " + dbusNameHasOwner,
			bluezService + " " + dbusPropertiesGet,
		}, *methods)
		require.Len(t, *flags, 2)
		for _, flag := range *flags {
			assert.NotZero(t, flag&dbus.FlagNoAutoStart)
		}
	})
}

func TestBusProperty_RejectsUnexpectedReplies(t *testing.T) {
	t.Parallel()

	bus := &fakeBus{replies: map[string][]any{
		"svc " + dbusPropertiesGet + " iface Empty":   {},
		"svc " + dbusPropertiesGet + " iface Plain":   {"not a variant"},
		"svc " + dbusPropertiesGet + " iface Number":  variant(uint32(4)),
		"svc " + dbusPropertiesGet + " iface Boolean": variant(true),
	}}
	ctx := context.Background()

	_, err := busProperty[bool](ctx, bus, "svc", "/", "iface", "Empty")
	require.Error(t, err)
	_, err = busProperty[bool](ctx, bus, "svc", "/", "iface", "Plain")
	require.Error(t, err)
	_, err = busProperty[bool](ctx, bus, "svc", "/", "iface", "Number")
	require.Error(t, err)
	value, err := busProperty[bool](ctx, bus, "svc", "/", "iface", "Boolean")
	require.NoError(t, err)
	assert.True(t, value)
}
