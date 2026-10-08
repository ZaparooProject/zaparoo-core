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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
)

const (
	dbusService        = "org.freedesktop.DBus"
	dbusPath           = "/org/freedesktop/DBus"
	dbusNameHasOwner   = "org.freedesktop.DBus.NameHasOwner"
	dbusPropertiesGet  = "org.freedesktop.DBus.Properties.Get"
	systemBusCallLimit = 2 * time.Second
)

var (
	errNoSystemBus = errors.New("no system bus")
	errNoService   = errors.New("service is not running")

	systemBusSocket = filepath.Join(string(filepath.Separator), "run", "dbus", "system_bus_socket")
)

// busCaller makes method calls on the system bus.
type busCaller interface {
	// call invokes method on a service that is already running. It returns
	// errNoSystemBus or errNoService when there is nothing to ask.
	call(ctx context.Context, service string, path dbus.ObjectPath, method string, args ...any) ([]any, error)
}

// systemBus talks to services on the system bus without ever starting one.
//
// Asking a service that is not running would make the bus launch it, and a
// freshly launched bluetoothd powers its adapter on. Core is a guest on this
// machine: it checks the service already has an owner and marks every call so
// the bus will not activate anything on its behalf.
type systemBus struct {
	objects func(service string, path dbus.ObjectPath) (dbus.BusObject, error)
	conn    *dbus.Conn
	mu      syncutil.Mutex
}

func newSystemBus() *systemBus {
	bus := &systemBus{}
	bus.objects = bus.connect
	return bus
}

func (b *systemBus) connect(service string, path dbus.ObjectPath) (dbus.BusObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.conn != nil && !b.conn.Connected() {
		b.conn = nil
	}
	if b.conn == nil {
		if os.Getenv("DBUS_SYSTEM_BUS_ADDRESS") == "" {
			if _, err := os.Stat(systemBusSocket); err != nil {
				return nil, errNoSystemBus
			}
		}
		conn, err := dbus.SystemBus()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errNoSystemBus, err)
		}
		b.conn = conn
	}
	return b.conn.Object(service, path), nil
}

func (b *systemBus) call(
	ctx context.Context,
	service string,
	path dbus.ObjectPath,
	method string,
	args ...any,
) ([]any, error) {
	ctx, cancel := context.WithTimeout(ctx, systemBusCallLimit)
	defer cancel()

	daemon, err := b.objects(dbusService, dbusPath)
	if err != nil {
		return nil, err
	}
	var owned bool
	ownerCall := daemon.CallWithContext(ctx, dbusNameHasOwner, dbus.FlagNoAutoStart, service)
	if storeErr := ownerCall.Store(&owned); storeErr != nil {
		return nil, fmt.Errorf("checking for %s: %w", service, storeErr)
	}
	if !owned {
		return nil, errNoService
	}

	object, err := b.objects(service, path)
	if err != nil {
		return nil, err
	}
	reply := object.CallWithContext(ctx, method, dbus.FlagNoAutoStart, args...)
	if reply.Err != nil {
		return nil, fmt.Errorf("calling %s on %s: %w", method, service, reply.Err)
	}
	return reply.Body, nil
}

// busProperty reads one property of a running service.
func busProperty[T any](
	ctx context.Context,
	bus busCaller,
	service string,
	path dbus.ObjectPath,
	iface, name string,
) (T, error) {
	var zero T
	body, err := bus.call(ctx, service, path, dbusPropertiesGet, iface, name)
	if err != nil {
		return zero, err
	}
	if len(body) != 1 {
		return zero, fmt.Errorf("property %s.%s: unexpected reply", iface, name)
	}
	variant, ok := body[0].(dbus.Variant)
	if !ok {
		return zero, fmt.Errorf("property %s.%s: reply is not a variant", iface, name)
	}
	value, ok := variant.Value().(T)
	if !ok {
		return zero, fmt.Errorf("property %s.%s: unexpected type %T", iface, name, variant.Value())
	}
	return value, nil
}
