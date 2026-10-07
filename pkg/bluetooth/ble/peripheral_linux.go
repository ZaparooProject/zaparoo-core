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

	// advRestartDelay is how long after the last device connected or left
	// the advertisement is restarted. Devices come and go in bursts, most of them not
	// ours (every remembered controller is reported when an adapter comes
	// up), and each restart is a round of commands to the controller, so
	// a burst gets one restart.
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
	mu      syncutil.Mutex
	serving bool
}

func newPeripheral(a *adapter) *peripheral {
	return &peripheral{a: a, chars: make(map[string]*gattChar)}
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
	v, ok := options["device"]
	if !ok {
		return Peer{}
	}
	path, ok := v.Value().(dbus.ObjectPath)
	if !ok {
		return Peer{}
	}
	return peerFromPath(path)
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
	conn  *dbus.Conn
	paths []exportedObject
}

func (e *exported) add(path dbus.ObjectPath, iface string) {
	e.paths = append(e.paths, exportedObject{path: path, iface: iface})
}

func (e *exported) export(v any, path dbus.ObjectPath, iface string) error {
	if err := e.conn.Export(v, path, iface); err != nil {
		return fmt.Errorf("export %s at %s: %w", iface, path, err)
	}
	e.add(path, iface)
	return nil
}

func (e *exported) exportProps(path dbus.ObjectPath, spec prop.Map) (*prop.Properties, error) {
	props, err := prop.Export(e.conn, path, spec)
	if err != nil {
		return nil, fmt.Errorf("export properties at %s: %w", path, err)
	}
	e.add(path, propertiesIface)
	return props, nil
}

func (e *exported) unexportAll() {
	for i := len(e.paths) - 1; i >= 0; i-- {
		_ = e.conn.Export(nil, e.paths[i].path, e.paths[i].iface)
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
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.serving = false
		p.handler = nil
		p.chars = make(map[string]*gattChar)
		p.mu.Unlock()
	}()

	n := serveCounter.Add(1)
	root := dbus.ObjectPath(fmt.Sprintf("%s/app%d", exportRoot, n))
	advPath := dbus.ObjectPath(fmt.Sprintf("%s/adv%d", exportRoot, n))

	exp := &exported{conn: p.a.conn}
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
	if err := p.register(ctx, advManagerIface+".RegisterAdvertisement", advPath); err != nil {
		return p.registrationFailed(ctx, err)
	}
	defer p.unregister(advManagerIface+".UnregisterAdvertisement", advPath)

	log.Info().
		Str("name", adv.LocalName).
		Strs("services", adv.ServiceUUIDs).
		Msg("bluetooth peripheral advertising")

	p.watchPeers(ctx, func(peerLeft bool) {
		// A controller stops advertising when a central connects.
		// Registering the advertisement afresh puts it back on the air:
		// straight away, if the controller can hold a connection and
		// advertise at once, so a second client can find the device; and
		// once that central leaves, when bluetoothd would otherwise keep
		// the advertisement registered with nothing being sent.
		p.unregister(advManagerIface+".UnregisterAdvertisement", advPath)
		err := p.register(ctx, advManagerIface+".RegisterAdvertisement", advPath)
		switch {
		case err == nil:
			log.Debug().Bool("peerLeft", peerLeft).Msg("bluetooth advertisement restarted")
		case ctx.Err() != nil:
		case peerLeft:
			log.Warn().Err(err).Msg("bluetooth advertisement could not be restarted")
			p.a.markGone()
		default:
			// A controller that cannot advertise while connected says no
			// here. That is not a fault; advertising resumes when the
			// peer leaves.
			log.Debug().Err(err).Msg("bluetooth advertisement not restarted while a peer is connected")
		}
	})
	return nil
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

// watchPeers reports peer disconnections until ctx ends or the adapter is
// gone, and calls peersChanged once things have been quiet for
// advRestartDelay after one or more devices connected or left, saying
// whether any of them left.
//
// Only a device that was connected counts. A scan fills bluetoothd with
// device objects for everything in range and drops each one half a minute
// later; treating those as departures restarted advertising continuously
// while a reader was being scanned for, which is what the controller could
// not take.
func (p *peripheral) watchPeers(ctx context.Context, peersChanged func(peerLeft bool)) {
	prefix := p.a.devicePathPrefix()
	events, unsubscribe := p.a.signals.subscribe(func(sig *dbus.Signal) bool {
		if !strings.HasPrefix(string(signalObject(sig)), prefix) {
			return false
		}
		switch sig.Name {
		case signalPropertiesChanged, signalInterfacesAdded, signalInterfacesRemoved:
			return true
		default:
			return false
		}
	})
	defer unsubscribe()

	// Stopped until a peer comes or goes.
	restart := time.NewTimer(advRestartDelay)
	restart.Stop()
	defer restart.Stop()

	connected := make(map[dbus.ObjectPath]bool)
	anyLeft := false
	arrived := func(path dbus.ObjectPath) {
		if !connected[path] {
			connected[path] = true
			restart.Reset(advRestartDelay)
		}
	}
	left := func(path dbus.ObjectPath) {
		p.currentHandler().OnDisconnect(peerFromPath(path))
		if connected[path] {
			delete(connected, path)
			anyLeft = true
			restart.Reset(advRestartDelay)
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
		case sig, ok := <-events:
			if !ok {
				return
			}
			if path, ifaces, added := interfacesAdded(sig); added {
				// A central that connects to us arrives as a new device
				// that is already connected.
				if isConnected, present := changedBool(ifaces[deviceIface], "Connected"); present && isConnected {
					arrived(path)
				}
				continue
			}
			if iface, changed, changedOK := propertiesChanged(sig); changedOK {
				if iface != deviceIface {
					continue
				}
				if isConnected, present := changedBool(changed, "Connected"); present {
					if isConnected {
						arrived(sig.Path)
					} else {
						left(sig.Path)
					}
				}
				continue
			}
			if path, ifaces, removed := interfacesRemoved(sig); removed && slices.Contains(ifaces, deviceIface) {
				left(path)
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
