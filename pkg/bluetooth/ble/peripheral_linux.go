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

package ble

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"github.com/rs/zerolog/log"
)

const (
	// exportRoot is where this process publishes its GATT objects. Each
	// Serve call gets its own numbered subtree so a restart never reuses a
	// path bluetoothd may still be tearing down.
	exportRoot = "/org/zaparoo/ble"

	// registerTimeout bounds RegisterApplication and RegisterAdvertisement,
	// which make bluetoothd walk our object tree before replying.
	registerTimeout = 10 * time.Second

	bluezErrFailed = "org.bluez.Error.Failed"

	// advRestartDelay is how long after the last client arrived or left
	// the advertisement is restarted. Clients come and go in bursts, and
	// each restart is a round of commands to the controller, so a burst
	// gets one restart.
	advRestartDelay = time.Second
)

var errAlreadyServing = errors.New("bluetooth: peripheral is already serving")

// serveCounter numbers Serve calls for unique object paths.
var serveCounter atomic.Uint64

// peripheral is the Linux Peripheral.
type peripheral struct {
	a       *adapter
	handler PeripheralHandler
	chars   map[string]*gattChar
	// clients holds the devices that have read or written one of the
	// served characteristics during this Serve. Only they are Core's
	// clients; every other device on the adapter, a gamepad above all, is
	// none of Core's business.
	clients map[dbus.ObjectPath]bool
	// newClient carries a device's path the first time it uses the GATT
	// service, for watchPeers.
	newClient chan dbus.ObjectPath
	// useWaiters are closed the next time their device uses the GATT
	// service.
	useWaiters map[dbus.ObjectPath][]chan struct{}
	// advChanged wakes Serve after SetAdvertising.
	advChanged chan struct{}
	// openBondEvents opens the management socket a bond window reads new
	// keys from.
	openBondEvents func() (mgmtConn, error)
	// linkRole reports whether a device is connected to the adapter as a
	// central, the adapter being the peripheral.
	linkRole func(index uint16, addr [6]byte) (bool, error)
	mu       syncutil.Mutex
	// bondStallTimeout is how long a device that just bonded has to use
	// the GATT service before it is disconnected.
	bondStallTimeout time.Duration
	serving          bool
	// advOff is set while advertising is switched off.
	advOff bool
}

func newPeripheral(a *adapter) *peripheral {
	return &peripheral{
		a:          a,
		chars:      make(map[string]*gattChar),
		clients:    make(map[dbus.ObjectPath]bool),
		newClient:  make(chan dbus.ObjectPath, 16),
		advChanged: make(chan struct{}, 1),
		useWaiters: make(map[dbus.ObjectPath][]chan struct{}),

		bondStallTimeout: bondStallTimeout,
		openBondEvents:   openMgmt,
		linkRole:         connectedAsPeripheral,
	}
}

// SetAdvertising implements Peripheral.
func (p *peripheral) SetAdvertising(on bool) {
	p.mu.Lock()
	changed := p.advOff == on
	p.advOff = !on
	p.mu.Unlock()
	if !changed {
		return
	}
	select {
	case p.advChanged <- struct{}{}:
	default:
	}
}

func (p *peripheral) advertisingOn() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.advOff
}

// noteClient records that the device at path used the GATT service.
func (p *peripheral) noteClient(path dbus.ObjectPath) {
	if path == "" {
		return
	}
	p.mu.Lock()
	for _, waiter := range p.useWaiters[path] {
		close(waiter)
	}
	delete(p.useWaiters, path)
	known := !p.serving || p.clients[path]
	if !known {
		p.clients[path] = true
	}
	p.mu.Unlock()
	if known {
		return
	}
	select {
	case p.newClient <- path:
	default:
		// watchPeers is behind; it restarts advertising for the clients
		// already queued, which covers this one too.
	}
}

// awaitUse returns a channel closed the next time the device at path uses
// the GATT service.
func (p *peripheral) awaitUse(path dbus.ObjectPath) <-chan struct{} {
	used := make(chan struct{})
	p.mu.Lock()
	p.useWaiters[path] = append(p.useWaiters[path], used)
	p.mu.Unlock()
	return used
}

// stopAwaitingUse drops a channel awaitUse returned.
func (p *peripheral) stopAwaitingUse(path dbus.ObjectPath, used <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	waiters := p.useWaiters[path]
	for i, waiter := range waiters {
		if waiter == used {
			waiters = slices.Delete(waiters, i, i+1)
			break
		}
	}
	if len(waiters) == 0 {
		delete(p.useWaiters, path)
	} else {
		p.useWaiters[path] = waiters
	}
}

// forgetClient reports whether the device at path was a client, and stops
// treating it as one.
func (p *peripheral) forgetClient(path dbus.ObjectPath) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	was := p.clients[path]
	delete(p.clients, path)
	return was
}

// objectManager implements org.freedesktop.DBus.ObjectManager for the
// application root, which is how bluetoothd discovers our services.
type objectManager struct {
	objects managedObjects
}

func (om *objectManager) GetManagedObjects() (managedObjects, *dbus.Error) {
	return om.objects, nil
}

// gattChar implements org.bluez.GattCharacteristic1 for one local
// characteristic. Only D-Bus methods may be exported on this type.
type gattChar struct {
	p     *peripheral
	props *prop.Properties
	uuid  string
}

func (c *gattChar) ReadValue(options map[string]dbus.Variant) ([]byte, *dbus.Error) {
	c.p.noteClient(devicePathFromOptions(options))
	value, err := c.p.currentHandler().OnRead(peerFromOptions(options), c.uuid)
	if err != nil {
		return nil, dbus.NewError(bluezErrFailed, []any{err.Error()})
	}
	return value, nil
}

func (c *gattChar) WriteValue(value []byte, options map[string]dbus.Variant) *dbus.Error {
	mtu := 0
	if v, ok := options["mtu"]; ok {
		if m, ok := v.Value().(uint16); ok {
			mtu = int(m)
		}
	}
	c.p.noteClient(devicePathFromOptions(options))
	c.p.currentHandler().OnWrite(peerFromOptions(options), c.uuid, append([]byte(nil), value...), mtu)
	return nil
}

func (c *gattChar) StartNotify() *dbus.Error {
	c.p.currentHandler().OnSubscribe(Peer{}, c.uuid, true)
	return nil
}

func (c *gattChar) StopNotify() *dbus.Error {
	c.p.currentHandler().OnSubscribe(Peer{}, c.uuid, false)
	return nil
}

// advertisement implements org.bluez.LEAdvertisement1.
type advertisement struct {
	a    *adapter
	path dbus.ObjectPath
}

// Release is called by bluetoothd when it drops the advertisement on its
// own, for example after the controller was reset. Nobody can find the
// device any more, so the adapter is given up for its owner to reopen.
func (adv *advertisement) Release() *dbus.Error {
	log.Warn().Str("path", string(adv.path)).Msg("bluetooth advertisement released by bluetoothd")
	adv.a.markGone()
	return nil
}

// peerFromOptions reads the writing or reading device from the options
// bluetoothd passes with every server-side ReadValue and WriteValue.
func peerFromOptions(options map[string]dbus.Variant) Peer {
	path := devicePathFromOptions(options)
	if path == "" {
		return Peer{}
	}
	return peerFromPath(path)
}

// devicePathFromOptions reads the device object path from those options, or
// "" when there is none.
func devicePathFromOptions(options map[string]dbus.Variant) dbus.ObjectPath {
	v, ok := options["device"]
	if !ok {
		return ""
	}
	path, ok := v.Value().(dbus.ObjectPath)
	if !ok {
		return ""
	}
	return path
}

// noopHandler stands in between Serve calls so a late callback from
// bluetoothd never hits a nil handler.
type noopHandler struct{}

func (noopHandler) OnWrite(Peer, string, []byte, int)   {}
func (noopHandler) OnRead(Peer, string) ([]byte, error) { return nil, ErrNotFound }
func (noopHandler) OnSubscribe(Peer, string, bool)      {}
func (noopHandler) OnDisconnect(Peer)                   {}

func (p *peripheral) currentHandler() PeripheralHandler {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handler == nil {
		return noopHandler{}
	}
	return p.handler
}

// exportedObject is one (path, interface) pair Serve put on the bus.
type exportedObject struct {
	path  dbus.ObjectPath
	iface string
}

// exported tracks what Serve put on the bus so it can take it all down.
type exported struct {
	a     *adapter
	paths []exportedObject
}

func (e *exported) add(path dbus.ObjectPath, iface string) {
	e.paths = append(e.paths, exportedObject{path: path, iface: iface})
}

func (e *exported) export(v any, path dbus.ObjectPath, iface string) error {
	if err := e.a.export(v, path, iface); err != nil {
		return fmt.Errorf("export %s at %s: %w", iface, path, err)
	}
	e.add(path, iface)
	return nil
}

func (e *exported) exportProps(path dbus.ObjectPath, spec prop.Map) (*prop.Properties, error) {
	e.a.exportMu.Lock()
	props, err := prop.Export(e.a.conn, path, spec)
	e.a.exportMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("export properties at %s: %w", path, err)
	}
	e.add(path, propertiesIface)
	return props, nil
}

func (e *exported) unexportAll() {
	for i := len(e.paths) - 1; i >= 0; i-- {
		_ = e.a.export(nil, e.paths[i].path, e.paths[i].iface)
	}
	e.paths = nil
}

// Serve publishes the application and advertisement and blocks until ctx
// ends or the adapter is gone.
func (p *peripheral) Serve(ctx context.Context, app Application, adv Advertisement, h PeripheralHandler) error {
	p.mu.Lock()
	if p.serving {
		p.mu.Unlock()
		return errAlreadyServing
	}
	p.serving = true
	p.handler = h
	p.chars = make(map[string]*gattChar)
	p.clients = make(map[dbus.ObjectPath]bool)
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.serving = false
		p.handler = nil
		p.chars = make(map[string]*gattChar)
		p.clients = make(map[dbus.ObjectPath]bool)
		p.mu.Unlock()
	}()

	n := serveCounter.Add(1)
	root := dbus.ObjectPath(fmt.Sprintf("%s/app%d", exportRoot, n))
	advPath := dbus.ObjectPath(fmt.Sprintf("%s/adv%d", exportRoot, n))

	exp := &exported{a: p.a}
	defer exp.unexportAll()

	if err := p.exportApplication(exp, root, app); err != nil {
		return err
	}
	if err := p.register(ctx, gattManagerIface+".RegisterApplication", root); err != nil {
		return p.registrationFailed(ctx, err)
	}
	defer p.unregister(gattManagerIface+".UnregisterApplication", root)

	if err := exportAdvertisement(exp, p.a, advPath, adv); err != nil {
		return err
	}
	// Watching starts before advertising does, so a central that connects
	// the moment the device becomes visible is not missed.
	events, unsubscribe := p.subscribePeers()
	defer unsubscribe()

	registered := false
	startAdvertising := func() error {
		if err := p.register(ctx, advManagerIface+".RegisterAdvertisement", advPath); err != nil {
			return err
		}
		registered = true
		return nil
	}
	stopAdvertising := func() {
		if registered {
			p.unregister(advManagerIface+".UnregisterAdvertisement", advPath)
			registered = false
		}
	}
	defer stopAdvertising()

	// waiting is set while the controller refuses to advertise because it
	// holds a connection, which many cannot do at the same time. A gamepad
	// on the adapter is enough. That is not a fault: advertising is tried
	// again once nothing is connected, so no command reaches the
	// controller while a gamepad is using it.
	waiting := false
	advertiseOrWait := func() error {
		err := startAdvertising()
		if err == nil {
			waiting = false
			return nil
		}
		if ctx.Err() != nil || !p.anyConnected(ctx) {
			return err
		}
		if !waiting {
			log.Info().Err(err).Msg("bluetooth advertising waits until nothing is connected to the adapter")
		}
		waiting = true
		return nil
	}
	logAdvertising := func() {
		log.Info().
			Str("name", adv.LocalName).
			Strs("services", adv.ServiceUUIDs).
			Msg("bluetooth peripheral advertising")
	}

	if p.advertisingOn() {
		if err := advertiseOrWait(); err != nil {
			return p.registrationFailed(ctx, err)
		}
		if registered {
			logAdvertising()
		}
	} else {
		log.Info().Msg("bluetooth peripheral serving with advertising paused")
	}

	// restart registers the advertisement afresh. bluetoothd keeps an
	// advertisement registered after the controller stopped sending it,
	// which happens whenever a connection takes it, so this is what puts
	// the device back on the air.
	restart := func(why string) {
		stopAdvertising()
		err := advertiseOrWait()
		switch {
		case err == nil:
			if registered {
				log.Debug().Str("why", why).Msg("bluetooth advertisement restarted")
			}
		case ctx.Err() != nil:
		default:
			// Nothing is connected and the controller still says no, so
			// nobody can find the device.
			log.Warn().Err(err).Msg("bluetooth advertisement could not be restarted")
			p.a.markGone()
		}
	}

	p.watchPeers(ctx, events, func(peerLeft bool) {
		// A controller stops advertising when a central connects. Restarting
		// straight away lets a second client find the device on a controller
		// that can hold a connection and advertise at once.
		if !p.advertisingOn() {
			return
		}
		why := "client arrived"
		if peerLeft {
			why = "client left"
		}
		restart(why)
	}, func() {
		on := p.advertisingOn()
		if on == registered {
			return
		}
		if !on {
			stopAdvertising()
			waiting = false
			log.Info().Msg("bluetooth advertising paused")
			return
		}
		if err := advertiseOrWait(); err != nil {
			if ctx.Err() == nil {
				log.Warn().Err(err).Msg("bluetooth advertising could not be resumed")
				p.a.markGone()
			}
			return
		}
		if registered {
			log.Info().Msg("bluetooth advertising resumed")
		}
	}, func() {
		// A connection ended that was not one of Core's clients. It may
		// have taken the advertisement: any central can connect, and one
		// that fails to finish connecting leaves some kernels unable to
		// switch advertising back on. While advertising waits for the
		// adapter to be free, this may also be the moment it can start;
		// a controller that still refuses keeps it waiting.
		if !p.advertisingOn() {
			return
		}
		if waiting && !registered {
			if err := advertiseOrWait(); err != nil {
				if ctx.Err() == nil {
					log.Warn().Err(err).Msg("bluetooth advertising could not be started")
					p.a.markGone()
				}
				return
			}
			if registered {
				logAdvertising()
			}
			return
		}
		if registered {
			restart("device disconnected")
		}
	})
	return nil
}

// anyConnected reports whether bluetoothd says any device of this adapter
// is connected. It only reads bluetoothd's state; nothing is sent to the
// controller. A failed read counts as nothing connected.
func (p *peripheral) anyConnected(ctx context.Context) bool {
	objs, err := p.a.managedObjects(ctx)
	if err != nil {
		return false
	}
	prefix := p.a.devicePathPrefix()
	for path, ifaces := range objs {
		if !strings.HasPrefix(string(path), prefix) {
			continue
		}
		if connected, ok := ifaces[deviceIface]["Connected"].Value().(bool); ok && connected {
			return true
		}
	}
	return false
}

// registrationFailed gives the adapter up when bluetoothd refused a
// registration, so the owner reopens it and tries again instead of sitting
// on an adapter nobody can reach. A cancelled Serve is not a refusal.
func (p *peripheral) registrationFailed(ctx context.Context, err error) error {
	if ctx.Err() == nil {
		p.a.markGone()
	}
	return err
}

// exportApplication publishes the ObjectManager root, services and
// characteristics bluetoothd will read on RegisterApplication.
func (p *peripheral) exportApplication(exp *exported, root dbus.ObjectPath, app Application) error {
	om := &objectManager{objects: managedObjects{}}
	for si, svc := range app.Services {
		svcPath := dbus.ObjectPath(fmt.Sprintf("%s/service%d", root, si))
		svcSpec := prop.Map{gattServiceIface: {
			"UUID":    {Value: svc.UUID, Emit: prop.EmitConst},
			"Primary": {Value: svc.Primary, Emit: prop.EmitConst},
		}}
		if svc.Handle != 0 {
			svcSpec[gattServiceIface]["Handle"] = &prop.Prop{Value: svc.Handle, Emit: prop.EmitConst}
		}
		if _, err := exp.exportProps(svcPath, svcSpec); err != nil {
			return err
		}
		om.objects[svcPath] = variantsOf(svcSpec)

		for ci, ch := range svc.Characteristics {
			charPath := dbus.ObjectPath(fmt.Sprintf("%s/char%d", svcPath, ci))
			charSpec := prop.Map{gattCharIface: {
				"UUID":    {Value: ch.UUID, Emit: prop.EmitConst},
				"Service": {Value: svcPath, Emit: prop.EmitConst},
				"Flags":   {Value: append([]string(nil), ch.Flags...), Emit: prop.EmitConst},
				// Writable so Notify can use the error-returning Set instead
				// of the panicking SetMust; only bluetoothd is on this bus.
				"Value": {Value: []byte{}, Writable: true, Emit: prop.EmitTrue},
			}}
			props, err := exp.exportProps(charPath, charSpec)
			if err != nil {
				return err
			}
			c := &gattChar{p: p, props: props, uuid: ch.UUID}
			if err := exp.export(c, charPath, gattCharIface); err != nil {
				return err
			}
			om.objects[charPath] = variantsOf(charSpec)
			p.mu.Lock()
			p.chars[strings.ToLower(ch.UUID)] = c
			p.mu.Unlock()
		}
	}
	return exp.export(om, root, objectManagerIface)
}

// exportAdvertisement publishes the LEAdvertisement1 object.
func exportAdvertisement(exp *exported, a *adapter, path dbus.ObjectPath, adv Advertisement) error {
	spec := prop.Map{advIface: {
		"Type":         {Value: "peripheral", Emit: prop.EmitConst},
		"ServiceUUIDs": {Value: append([]string(nil), adv.ServiceUUIDs...), Emit: prop.EmitConst},
		"LocalName":    {Value: adv.LocalName, Emit: prop.EmitConst},
		// Advertise as generally discoverable without touching the adapter's
		// global Discoverable flag, which MiSTer's controller pairing owns.
		"Discoverable": {Value: true, Emit: prop.EmitConst},
	}}
	if _, err := exp.exportProps(path, spec); err != nil {
		return err
	}
	return exp.export(&advertisement{a: a, path: path}, path, advIface)
}

// variantsOf converts a property spec into the map shape GetManagedObjects
// returns for it.
func variantsOf(spec prop.Map) map[string]map[string]dbus.Variant {
	out := make(map[string]map[string]dbus.Variant, len(spec))
	for iface, props := range spec {
		vals := make(map[string]dbus.Variant, len(props))
		for name, pr := range props {
			vals[name] = dbus.MakeVariant(pr.Value)
		}
		out[iface] = vals
	}
	return out
}

// register calls a Register* method on the adapter with the longer
// registration timeout.
func (p *peripheral) register(ctx context.Context, method string, path dbus.ObjectPath) error {
	cctx, cancel := context.WithTimeout(ctx, registerTimeout)
	defer cancel()
	if call := p.a.obj.CallWithContext(cctx, method, 0, path, map[string]dbus.Variant{}); call.Err != nil {
		return mapBusError(method, call.Err)
	}
	return nil
}

// unregister is best effort: bluetoothd also drops registrations when our
// bus connection closes, so a failure here only matters for the log.
func (p *peripheral) unregister(method string, path dbus.ObjectPath) {
	ctx, cancel := context.WithTimeout(context.Background(), p.a.callTimeout)
	defer cancel()
	if err := p.a.call(ctx, p.a.obj, method, path); err != nil && !errors.Is(err, ErrUnavailable) {
		log.Debug().Err(err).Str("method", method).Msg("bluetooth unregister failed")
	}
}

// subscribePeers subscribes to the signals that say a remote device of this
// adapter appeared, connected, disconnected or was forgotten.
func (p *peripheral) subscribePeers() (events <-chan *dbus.Signal, unsubscribe func()) {
	prefix := p.a.devicePathPrefix()
	return p.a.signals.subscribe(func(sig *dbus.Signal) bool {
		if !strings.HasPrefix(string(signalObject(sig)), prefix) {
			return false
		}
		switch sig.Name {
		case signalPropertiesChanged, signalInterfacesRemoved:
			return true
		default:
			return false
		}
	})
}

// watchPeers reports peer disconnections until ctx ends or the adapter is
// gone, calls advertisingChanged after SetAdvertising, and calls
// peersChanged once things have been quiet for
// advRestartDelay after one or more of Core's clients arrived or left,
// saying whether any of them left. deviceLeft is called the same way after
// any other device of the adapter disconnected.
//
// A client arrives when it first uses the GATT service, not when it
// connects, because a connection alone does not say whose it is: every
// gamepad and headset on the adapter connects too. A scan also fills
// bluetoothd with device objects for everything in range and drops each
// one half a minute later, so a device that goes away without having
// disconnected does not count.
func (p *peripheral) watchPeers(
	ctx context.Context,
	events <-chan *dbus.Signal,
	peersChanged func(peerLeft bool),
	advertisingChanged func(),
	deviceLeft func(),
) {
	// Stopped until a client comes or goes.
	restart := time.NewTimer(advRestartDelay)
	restart.Stop()
	defer restart.Stop()
	// Stopped until any device leaves; a burst, such as a scan's devices
	// being dropped, gets one call.
	settled := time.NewTimer(advRestartDelay)
	settled.Stop()
	defer settled.Stop()

	anyLeft := false
	left := func(path dbus.ObjectPath, disconnected bool) {
		p.currentHandler().OnDisconnect(peerFromPath(path))
		if p.forgetClient(path) {
			anyLeft = true
			restart.Reset(advRestartDelay)
			return
		}
		if disconnected {
			settled.Reset(advRestartDelay)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.a.gone:
			return
		case <-restart.C:
			peersChanged(anyLeft)
			anyLeft = false
		case <-settled.C:
			deviceLeft()
		case <-p.newClient:
			restart.Reset(advRestartDelay)
		case <-p.advChanged:
			advertisingChanged()
		case sig, ok := <-events:
			if !ok {
				return
			}
			if iface, changed, changedOK := propertiesChanged(sig); changedOK {
				if iface != deviceIface {
					continue
				}
				if isConnected, present := changedBool(changed, "Connected"); present && !isConnected {
					left(sig.Path, true)
				}
				continue
			}
			if path, ifaces, removed := interfacesRemoved(sig); removed && slices.Contains(ifaces, deviceIface) {
				left(path, false)
			}
		}
	}
}

// Notify updates the characteristic value; bluetoothd turns the property
// change into an ATT notification for every subscribed peer, whichever one
// was asked for.
func (p *peripheral) Notify(_ Peer, charUUID string, value []byte) error {
	p.mu.Lock()
	c := p.chars[strings.ToLower(charUUID)]
	p.mu.Unlock()
	if c == nil {
		return fmt.Errorf("%w: characteristic %s", ErrNotFound, charUUID)
	}
	if dbusErr := c.props.Set(gattCharIface, "Value", dbus.MakeVariant(append([]byte(nil), value...))); dbusErr != nil {
		return fmt.Errorf("notify %s: %w", charUUID, *dbusErr)
	}
	return nil
}

// Disconnect drops the peer's link.
func (p *peripheral) Disconnect(ctx context.Context, peer Peer) error {
	if peer.Path == "" {
		return fmt.Errorf("%w: peer has no path", ErrNotFound)
	}
	obj := p.a.conn.Object(bluezService, dbus.ObjectPath(peer.Path))
	return p.a.call(ctx, obj, deviceIface+".Disconnect")
}
