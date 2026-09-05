//go:build linux

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

package bluez

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
)

const (
	bluezService = "org.bluez"

	adapterIface       = "org.bluez.Adapter1"
	deviceIface        = "org.bluez.Device1"
	gattServiceIface   = "org.bluez.GattService1"
	gattCharIface      = "org.bluez.GattCharacteristic1"
	gattManagerIface   = "org.bluez.GattManager1"
	advManagerIface    = "org.bluez.LEAdvertisingManager1"
	advIface           = "org.bluez.LEAdvertisement1"
	objectManagerIface = "org.freedesktop.DBus.ObjectManager"
	propertiesIface    = "org.freedesktop.DBus.Properties"

	signalPropertiesChanged = propertiesIface + ".PropertiesChanged"
	signalInterfacesAdded   = objectManagerIface + ".InterfacesAdded"
	signalInterfacesRemoved = objectManagerIface + ".InterfacesRemoved"

	bluezRootPath   = dbus.ObjectPath("/")
	bluezPathPrefix = dbus.ObjectPath("/org/bluez")

	// signalBuffer is how many signals one subscriber may fall behind before
	// the router starts dropping for it.
	signalBuffer = 256
)

// managedObjects is the shape of ObjectManager.GetManagedObjects.
type managedObjects = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// adapter is the Linux Adapter: one private system-bus connection and the
// Adapter1 object it drives.
type adapter struct {
	conn        *dbus.Conn
	obj         dbus.BusObject
	signals     *signalRouter
	gone        chan struct{}
	path        dbus.ObjectPath
	address     string
	roles       []Role
	callTimeout time.Duration
	goneOnce    sync.Once
	closeOnce   sync.Once
}

// Open connects to bluetoothd on a private bus connection and selects an
// adapter: the lowest-numbered powered one, or the lowest-numbered one at
// all when none is powered. With WithPowerOn a powered-off choice is
// switched on.
func Open(ctx context.Context, opts ...Option) (Adapter, error) {
	o := applyOptions(opts)

	conn, err := connect(o)
	if err != nil {
		return nil, err
	}

	a := &adapter{
		conn:        conn,
		gone:        make(chan struct{}),
		callTimeout: o.callTimeout,
	}
	if err := a.selectAdapter(ctx, o.powerOn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := a.watchSignals(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return a, nil
}

// connect opens and authenticates a private bus connection with sequential
// signal delivery, so property changes arrive in the order BlueZ sent them.
func connect(o options) (*dbus.Conn, error) {
	handler := dbus.WithSignalHandler(dbus.NewSequentialSignalHandler())
	var (
		conn *dbus.Conn
		err  error
	)
	if o.busAddress != "" {
		conn, err = dbus.Dial(o.busAddress, handler)
	} else {
		conn, err = dbus.SystemBusPrivate(handler)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: auth: %w", ErrUnavailable, err)
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: hello: %w", ErrUnavailable, err)
	}
	return conn, nil
}

// selectAdapter finds the adapter to use and reads its properties.
func (a *adapter) selectAdapter(ctx context.Context, powerOn bool) error {
	objs, err := a.managedObjects(ctx)
	if err != nil {
		return err
	}

	paths := make([]string, 0, len(objs))
	for path, ifaces := range objs {
		if _, ok := ifaces[adapterIface]; ok {
			paths = append(paths, string(path))
		}
	}
	if len(paths) == 0 {
		return ErrNoAdapter
	}
	sort.Strings(paths)
	chosen := paths[0]
	for _, path := range paths {
		if powered, ok := objs[dbus.ObjectPath(path)][adapterIface]["Powered"].Value().(bool); ok && powered {
			chosen = path
			break
		}
	}
	a.path = dbus.ObjectPath(chosen)
	a.obj = a.conn.Object(bluezService, a.path)

	props := objs[a.path][adapterIface]
	a.address = stringProp(props, "Address")
	if roles, ok := props["Roles"].Value().([]string); ok {
		for _, r := range roles {
			a.roles = append(a.roles, Role(r))
		}
	}

	if powered, ok := props["Powered"].Value().(bool); ok && !powered && powerOn {
		log.Info().Str("adapter", string(a.path)).Msg("bluetooth adapter is powered off, powering on")
		if err := a.setProperty(ctx, a.obj, adapterIface, "Powered", true); err != nil {
			log.Warn().Err(err).Msg("failed to power on bluetooth adapter")
		}
	}
	return nil
}

// watchSignals subscribes to everything the roles need and starts the router.
// The adapter is marked gone when its object is removed or the bus drops.
func (a *adapter) watchSignals(ctx context.Context) error {
	matches := [][]dbus.MatchOption{
		{
			dbus.WithMatchInterface(propertiesIface),
			dbus.WithMatchMember("PropertiesChanged"),
			dbus.WithMatchPathNamespace(bluezPathPrefix),
		},
		{
			dbus.WithMatchInterface(objectManagerIface),
			dbus.WithMatchMember("InterfacesAdded"),
			dbus.WithMatchObjectPath(bluezRootPath),
		},
		{
			dbus.WithMatchInterface(objectManagerIface),
			dbus.WithMatchMember("InterfacesRemoved"),
			dbus.WithMatchObjectPath(bluezRootPath),
		},
	}
	for _, m := range matches {
		if err := a.conn.AddMatchSignalContext(ctx, m...); err != nil {
			return fmt.Errorf("add signal match: %w", err)
		}
	}

	in := make(chan *dbus.Signal, signalBuffer)
	a.conn.Signal(in)
	a.signals = newSignalRouter()
	go a.signals.run(in)

	removed, unsubscribe := a.signals.subscribe(func(sig *dbus.Signal) bool {
		path, _, ok := interfacesRemoved(sig)
		return ok && path == a.path
	})
	go func() {
		defer unsubscribe()
		select {
		case <-removed:
			log.Warn().Str("adapter", string(a.path)).Msg("bluetooth adapter removed")
		case <-a.conn.Context().Done():
			log.Warn().Msg("bluetooth bus connection closed")
		case <-a.gone:
			return
		}
		a.markGone()
	}()
	return nil
}

func (a *adapter) markGone() {
	a.goneOnce.Do(func() { close(a.gone) })
}

func (a *adapter) Address() string { return a.address }

func (a *adapter) Roles() []Role { return append([]Role(nil), a.roles...) }

func (a *adapter) Gone() <-chan struct{} { return a.gone }

func (a *adapter) Peripheral() (Peripheral, error) {
	if !SupportsRole(a.roles, RolePeripheral) {
		return nil, fmt.Errorf("%w: %s", ErrRoleUnsupported, RolePeripheral)
	}
	return newPeripheral(a), nil
}

func (a *adapter) Central() (Central, error) {
	if !SupportsRole(a.roles, RoleCentral) {
		return nil, fmt.Errorf("%w: %s", ErrRoleUnsupported, RoleCentral)
	}
	return &central{a: a}, nil
}

func (a *adapter) Close() error {
	var err error
	a.closeOnce.Do(func() {
		a.markGone()
		a.signals.close()
		if closeErr := a.conn.Close(); closeErr != nil {
			err = fmt.Errorf("close bus connection: %w", closeErr)
		}
	})
	return err
}

// callCtx bounds one D-Bus round trip by the caller's context and the call
// timeout, whichever ends first.
func (a *adapter) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, a.callTimeout)
}

// managedObjects fetches everything bluetoothd exports.
func (a *adapter) managedObjects(ctx context.Context) (managedObjects, error) {
	cctx, cancel := a.callCtx(ctx)
	defer cancel()
	var objs managedObjects
	call := a.conn.Object(bluezService, bluezRootPath).
		CallWithContext(cctx, objectManagerIface+".GetManagedObjects", 0)
	if call.Err != nil {
		return nil, mapBusError("get managed objects", call.Err)
	}
	if err := call.Store(&objs); err != nil {
		return nil, fmt.Errorf("decode managed objects: %w", err)
	}
	return objs, nil
}

// getProperty reads one property of a BlueZ object.
func (a *adapter) getProperty(ctx context.Context, obj dbus.BusObject, iface, name string) (dbus.Variant, error) {
	cctx, cancel := a.callCtx(ctx)
	defer cancel()
	var v dbus.Variant
	call := obj.CallWithContext(cctx, propertiesIface+".Get", 0, iface, name)
	if call.Err != nil {
		return dbus.Variant{}, mapBusError("get "+iface+"."+name, call.Err)
	}
	if err := call.Store(&v); err != nil {
		return dbus.Variant{}, fmt.Errorf("decode %s.%s: %w", iface, name, err)
	}
	return v, nil
}

// setProperty writes one property of a BlueZ object.
func (a *adapter) setProperty(ctx context.Context, obj dbus.BusObject, iface, name string, value any) error {
	cctx, cancel := a.callCtx(ctx)
	defer cancel()
	call := obj.CallWithContext(cctx, propertiesIface+".Set", 0, iface, name, dbus.MakeVariant(value))
	if call.Err != nil {
		return mapBusError("set "+iface+"."+name, call.Err)
	}
	return nil
}

// call invokes a method on a BlueZ object with the standard timeout.
func (a *adapter) call(ctx context.Context, obj dbus.BusObject, method string, args ...any) error {
	cctx, cancel := a.callCtx(ctx)
	defer cancel()
	if call := obj.CallWithContext(cctx, method, 0, args...); call.Err != nil {
		return mapBusError(method, call.Err)
	}
	return nil
}

// mapBusError turns the D-Bus errors that mean "bluetoothd is not there"
// into ErrUnavailable so callers can back off instead of retrying hot.
func mapBusError(op string, err error) error {
	var dbusErr dbus.Error
	if errors.As(err, &dbusErr) {
		switch dbusErr.Name {
		case "org.freedesktop.DBus.Error.ServiceUnknown",
			"org.freedesktop.DBus.Error.NameHasNoOwner",
			"org.freedesktop.DBus.Error.NoReply":
			return fmt.Errorf("%w: %s: %w", ErrUnavailable, op, err)
		case "org.freedesktop.DBus.Error.UnknownObject":
			return fmt.Errorf("%w: %s: %w", ErrNotFound, op, err)
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}

// signalRouter fans one bus signal stream out to interested subscribers.
type signalRouter struct {
	subs   map[int]*signalSub
	mu     syncutil.Mutex
	next   int
	closed bool
}

type signalSub struct {
	ch    chan *dbus.Signal
	match func(*dbus.Signal) bool
}

func newSignalRouter() *signalRouter {
	return &signalRouter{subs: make(map[int]*signalSub)}
}

// subscribe returns a channel receiving every signal match accepts, and a
// function that ends the subscription and closes the channel.
func (r *signalRouter) subscribe(match func(*dbus.Signal) bool) (events <-chan *dbus.Signal, cancel func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.next
	r.next++
	sub := &signalSub{ch: make(chan *dbus.Signal, signalBuffer), match: match}
	if r.closed {
		close(sub.ch)
		return sub.ch, func() {}
	}
	r.subs[id] = sub
	var once sync.Once
	return sub.ch, func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if _, ok := r.subs[id]; ok {
				delete(r.subs, id)
				close(sub.ch)
			}
		})
	}
}

// run delivers signals until the input channel closes. A subscriber that
// has fallen signalBuffer signals behind loses the newest one rather than
// stalling everyone else.
func (r *signalRouter) run(in <-chan *dbus.Signal) {
	for sig := range in {
		r.mu.Lock()
		for _, sub := range r.subs {
			if !sub.match(sig) {
				continue
			}
			select {
			case sub.ch <- sig:
			default:
				log.Warn().Str("signal", sig.Name).Msg("bluetooth signal subscriber is behind, dropping signal")
			}
		}
		r.mu.Unlock()
	}
	r.close()
}

// close ends every subscription.
func (r *signalRouter) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for id, sub := range r.subs {
		delete(r.subs, id)
		close(sub.ch)
	}
}

// devicePathPrefix is where BlueZ puts remote devices of this adapter.
func (a *adapter) devicePathPrefix() string {
	return string(a.path) + "/dev_"
}

// addressFromPath recovers the Bluetooth address from a Device1 object path.
func addressFromPath(path dbus.ObjectPath) string {
	s := string(path)
	idx := strings.LastIndex(s, "/dev_")
	if idx < 0 {
		return ""
	}
	return strings.ReplaceAll(s[idx+len("/dev_"):], "_", ":")
}

// stringProp reads a string property from a property map, or "" when it is
// absent or not a string.
func stringProp(props map[string]dbus.Variant, name string) string {
	v, ok := props[name]
	if !ok {
		return ""
	}
	s, ok := v.Value().(string)
	if !ok {
		return ""
	}
	return s
}

// peerFromPath builds the Peer for a Device1 object path.
func peerFromPath(path dbus.ObjectPath) Peer {
	return Peer{Path: string(path), Address: addressFromPath(path)}
}
