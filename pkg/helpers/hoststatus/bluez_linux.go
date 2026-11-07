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
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
)

const (
	bluezRoot         = dbus.ObjectPath("/org/bluez")
	bluezDevice       = "org.bluez.Device1"
	bluezBattery      = "org.bluez.Battery1"
	bluezAgentManager = "org.bluez.AgentManager1"
	bluezAgent        = "org.bluez.Agent1"
	dbusObjectManager = "org.freedesktop.DBus.ObjectManager"
	dbusProperties    = "org.freedesktop.DBus.Properties"

	// bluezAgentPath is where the pairing agent is exported while an attempt
	// runs. Only one attempt runs at a time.
	bluezAgentPath = dbus.ObjectPath("/org/zaparoo/core/bluetooth/agent")
	// bluezCapability tells the stack Core can neither show nor enter a code,
	// so pairing falls back to "Just Works".
	bluezCapability = "NoInputNoOutput"

	bluezCallLimit = 2 * time.Second
	// bluezRadioLimit bounds the calls BlueZ answers only after the adapter
	// does: starting and stopping discovery, and removing a device, which
	// disconnects it and clears it from the controller first. A slow adapter
	// takes seconds over these, and giving up early reports a failure for a
	// change that still lands.
	bluezRadioLimit = 10 * time.Second
	bluezWatchRetry = 5 * time.Second
)

// BlueZ errors Core tells apart.
const (
	bluezErrAlreadyExists    = "org.bluez.Error.AlreadyExists"
	bluezErrAlreadyConnected = "org.bluez.Error.AlreadyConnected"
	bluezErrInProgress       = "org.bluez.Error.InProgress"
	bluezErrNotReady         = "org.bluez.Error.NotReady"
	bluezErrDoesNotExist     = "org.bluez.Error.DoesNotExist"
	bluezErrRejected         = "org.bluez.Error.Rejected"
	bluezErrAuthRejected     = "org.bluez.Error.AuthenticationRejected"
	bluezErrAuthCanceled     = "org.bluez.Error.AuthenticationCanceled"
	bluezErrAuthTimeout      = "org.bluez.Error.AuthenticationTimeout"
	bluezErrConnectionFailed = "org.bluez.Error.ConnectionAttemptFailed"
	dbusErrServiceUnknown    = "org.freedesktop.DBus.Error.ServiceUnknown"
	dbusErrUnknownObject     = "org.freedesktop.DBus.Error.UnknownObject"
	dbusErrNoReply           = "org.freedesktop.DBus.Error.NoReply"
	dbusErrNameHasNoOwner    = "org.freedesktop.DBus.Error.NameHasNoOwner"
	signalInterfacesAdded    = dbusObjectManager + ".InterfacesAdded"
	signalInterfacesRemoved  = dbusObjectManager + ".InterfacesRemoved"
	signalPropertiesChanged  = dbusProperties + ".PropertiesChanged"
	bluezGetManagedObjects   = dbusObjectManager + ".GetManagedObjects"
	bluezPropertiesSet       = dbusProperties + ".Set"
	bluezSetDiscoveryFilter  = bluezAdapter + ".SetDiscoveryFilter"
	bluezStartDiscovery      = bluezAdapter + ".StartDiscovery"
	bluezStopDiscovery       = bluezAdapter + ".StopDiscovery"
	bluezRemoveDevice        = bluezAdapter + ".RemoveDevice"
	bluezPair                = bluezDevice + ".Pair"
	bluezCancelPairing       = bluezDevice + ".CancelPairing"
	bluezConnect             = bluezDevice + ".Connect"
	bluezRegisterAgent       = bluezAgentManager + ".RegisterAgent"
	bluezRequestDefaultAgent = bluezAgentManager + ".RequestDefaultAgent"
	bluezUnregisterAgent     = bluezAgentManager + ".UnregisterAgent"
)

type bluezObjects = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// bluezBus is the part of a D-Bus connection the BlueZ controller uses. It is
// a connection of Core's own, not the shared one: BlueZ ties discovery
// sessions and agents to the connection that asked for them and ends both when
// it closes, so nothing else may close it underneath.
type bluezBus interface {
	call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error)
	export(v any, path dbus.ObjectPath, iface string) error
	unexport(path dbus.ObjectPath, iface string)
	// subscribe delivers BlueZ's object and property signals until stop is
	// called. The channel closes when the connection is lost.
	subscribe() (signals <-chan *dbus.Signal, stop func(), err error)
}

type privateBus struct {
	conn *dbus.Conn
	mu   syncutil.Mutex
}

func (b *privateBus) connection() (*dbus.Conn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil && b.conn.Connected() {
		return b.conn, nil
	}
	b.conn = nil
	if os.Getenv("DBUS_SYSTEM_BUS_ADDRESS") == "" {
		if _, err := os.Stat(systemBusSocket); err != nil {
			return nil, errNoSystemBus
		}
	}
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNoSystemBus, err)
	}
	b.conn = conn
	return conn, nil
}

func (b *privateBus) call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	conn, err := b.connection()
	if err != nil {
		return nil, err
	}
	reply := conn.Object(bluezService, path).CallWithContext(ctx, method, dbus.FlagNoAutoStart, args...)
	if reply.Err != nil {
		return nil, fmt.Errorf("calling %s: %w", method, reply.Err)
	}
	return reply.Body, nil
}

func (b *privateBus) export(v any, path dbus.ObjectPath, iface string) error {
	conn, err := b.connection()
	if err != nil {
		return err
	}
	if err := conn.Export(v, path, iface); err != nil {
		return fmt.Errorf("exporting %s: %w", iface, err)
	}
	return nil
}

func (b *privateBus) unexport(path dbus.ObjectPath, iface string) {
	conn, err := b.connection()
	if err != nil {
		return
	}
	_ = conn.Export(nil, path, iface)
}

func bluezMatches() [][]dbus.MatchOption {
	return [][]dbus.MatchOption{
		{
			dbus.WithMatchSender(bluezService),
			dbus.WithMatchInterface(dbusObjectManager),
			dbus.WithMatchMember("InterfacesAdded"),
		},
		{
			dbus.WithMatchSender(bluezService),
			dbus.WithMatchInterface(dbusObjectManager),
			dbus.WithMatchMember("InterfacesRemoved"),
		},
		{
			dbus.WithMatchSender(bluezService),
			dbus.WithMatchInterface(dbusProperties),
			dbus.WithMatchMember("PropertiesChanged"),
			dbus.WithMatchPathNamespace(bluezRoot),
		},
	}
}

func (b *privateBus) subscribe() (signals <-chan *dbus.Signal, stop func(), err error) {
	conn, err := b.connection()
	if err != nil {
		return nil, nil, err
	}
	matches := bluezMatches()
	removeMatches := func(added [][]dbus.MatchOption) {
		for _, match := range added {
			_ = conn.RemoveMatchSignal(match...)
		}
	}
	for i, match := range matches {
		if err := conn.AddMatchSignal(match...); err != nil {
			removeMatches(matches[:i])
			return nil, nil, fmt.Errorf("subscribing to bluetooth signals: %w", err)
		}
	}
	ch := make(chan *dbus.Signal, 32)
	conn.Signal(ch)
	return ch, func() {
		conn.RemoveSignal(ch)
		removeMatches(matches)
	}, nil
}

// BlueZ manages Bluetooth devices through bluetoothd.
type BlueZ struct {
	bus bluezBus
	// pairMu keeps to one agent, and one attempt, at a time.
	pairMu     syncutil.Mutex
	watchRetry time.Duration
}

// NewBlueZ returns a controller that connects to the system bus the first
// time it is used.
func NewBlueZ() *BlueZ {
	return &BlueZ{bus: &privateBus{}, watchRetry: bluezWatchRetry}
}

// dbusErrorName returns the D-Bus error name inside err, or "".
func dbusErrorName(err error) string {
	var value dbus.Error
	if errors.As(err, &value) {
		return value.Name
	}
	var pointer *dbus.Error
	if errors.As(err, &pointer) && pointer != nil {
		return pointer.Name
	}
	return ""
}

// bluezError turns a failed call into one of the errors callers act on.
func bluezError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errNoSystemBus) {
		return fmt.Errorf("%w: %w", ErrUnsupported, err)
	}
	switch dbusErrorName(err) {
	case dbusErrServiceUnknown, dbusErrNameHasNoOwner:
		return fmt.Errorf("%w: %w", ErrUnsupported, err)
	case bluezErrNotReady:
		return fmt.Errorf("%w: %w", ErrBluetoothNotPowered, err)
	case bluezErrDoesNotExist, dbusErrUnknownObject:
		return fmt.Errorf("%w: %w", ErrBluetoothNotFound, err)
	case bluezErrAuthRejected, bluezErrAuthCanceled, bluezErrRejected:
		return fmt.Errorf("%w: %w", ErrBluetoothRejected, err)
	case bluezErrAuthTimeout, bluezErrConnectionFailed, dbusErrNoReply:
		return fmt.Errorf("%w: %w", ErrBluetoothTimeout, err)
	default:
		return err
	}
}

func (b *BlueZ) quickCall(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	ctx, cancel := context.WithTimeout(ctx, bluezCallLimit)
	defer cancel()
	return b.bus.call(ctx, path, method, args...)
}

func (b *BlueZ) radioCall(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	ctx, cancel := context.WithTimeout(ctx, bluezRadioLimit)
	defer cancel()
	return b.bus.call(ctx, path, method, args...)
}

func (b *BlueZ) objects(ctx context.Context) (bluezObjects, error) {
	body, err := b.quickCall(ctx, "/", bluezGetManagedObjects)
	if err != nil {
		return nil, bluezError(err)
	}
	if len(body) != 1 {
		return nil, errors.New("bluetooth objects: unexpected reply")
	}
	objects, ok := body[0].(bluezObjects)
	if !ok {
		return nil, fmt.Errorf("bluetooth objects: unexpected reply type %T", body[0])
	}
	return objects, nil
}

func variantValue[T any](props map[string]dbus.Variant, name string) (T, bool) {
	var zero T
	variant, ok := props[name]
	if !ok {
		return zero, false
	}
	value, ok := variant.Value().(T)
	return value, ok
}

// adapterPath picks the adapter Core uses: the first one, which on a machine
// with one adapter is the only one.
func adapterPath(objects bluezObjects) (dbus.ObjectPath, map[string]dbus.Variant, error) {
	var paths []string
	for path, ifaces := range objects {
		if _, ok := ifaces[bluezAdapter]; ok {
			paths = append(paths, string(path))
		}
	}
	if len(paths) == 0 {
		return "", nil, ErrBluetoothNoAdapter
	}
	sort.Strings(paths)
	path := dbus.ObjectPath(paths[0])
	return path, objects[path][bluezAdapter], nil
}

func parseBluezDevice(props map[string]dbus.Variant) (BluetoothDevice, bool) {
	address, ok := variantValue[string](props, "Address")
	if !ok {
		return BluetoothDevice{}, false
	}
	address = strings.ToUpper(address)
	device := BluetoothDevice{Address: address}
	// BlueZ makes up an alias from the address for a device with no name,
	// which is not a name.
	if alias, _ := variantValue[string](props, "Alias"); alias != "" && alias != strings.ReplaceAll(address, ":", "-") {
		device.Name = alias
	} else if name, _ := variantValue[string](props, "Name"); name != "" {
		device.Name = name
	}
	modalias, _ := variantValue[string](props, "Modalias")
	device.VendorID, device.ProductID = ParseModalias(modalias)
	device.Icon, _ = variantValue[string](props, "Icon")
	device.UUIDs, _ = variantValue[[]string](props, "UUIDs")
	device.Class, _ = variantValue[uint32](props, "Class")
	device.Appearance, _ = variantValue[uint16](props, "Appearance")
	device.Paired, _ = variantValue[bool](props, "Paired")
	device.Connected, _ = variantValue[bool](props, "Connected")
	device.Trusted, _ = variantValue[bool](props, "Trusted")
	// BlueZ only holds a signal strength for a device the running discovery
	// has heard, and drops it when discovery ends.
	_, device.InRange = props["RSSI"]
	return device, true
}

// Read reports the adapter's state and every device it knows.
func (b *BlueZ) Read(ctx context.Context) (BluetoothReading, error) {
	objects, err := b.objects(ctx)
	if err != nil {
		return BluetoothReading{}, err
	}
	adapter, props, err := adapterPath(objects)
	if err != nil {
		return BluetoothReading{}, err
	}
	reading := BluetoothReading{Devices: []BluetoothDevice{}}
	reading.Powered, _ = variantValue[bool](props, "Powered")
	reading.Discovering, _ = variantValue[bool](props, "Discovering")
	for _, ifaces := range objects {
		deviceProps, ok := ifaces[bluezDevice]
		if !ok {
			continue
		}
		if owner, _ := variantValue[dbus.ObjectPath](deviceProps, "Adapter"); owner != adapter {
			continue
		}
		if device, ok := parseBluezDevice(deviceProps); ok {
			reading.Devices = append(reading.Devices, device)
		}
	}
	sort.Slice(reading.Devices, func(i, j int) bool {
		return reading.Devices[i].Address < reading.Devices[j].Address
	})
	return reading, nil
}

// Batteries reports the charge of every device that exposes BlueZ's battery
// interface, keyed by lower-case address. BlueZ has no charging state there.
func (b *BlueZ) Batteries(ctx context.Context) (map[string]int, error) {
	objects, err := b.objects(ctx)
	if err != nil {
		return nil, err
	}
	batteries := map[string]int{}
	for _, ifaces := range objects {
		batteryProps, ok := ifaces[bluezBattery]
		if !ok {
			continue
		}
		address, ok := variantValue[string](ifaces[bluezDevice], "Address")
		if !ok {
			continue
		}
		percent, ok := variantValue[byte](batteryProps, "Percentage")
		if !ok || percent > 100 {
			continue
		}
		batteries[strings.ToLower(address)] = int(percent)
	}
	return batteries, nil
}

// poweredAdapter returns the adapter when it is on.
func (b *BlueZ) poweredAdapter(ctx context.Context) (dbus.ObjectPath, bluezObjects, error) {
	objects, err := b.objects(ctx)
	if err != nil {
		return "", nil, err
	}
	adapter, props, err := adapterPath(objects)
	if err != nil {
		return "", nil, err
	}
	if powered, _ := variantValue[bool](props, "Powered"); !powered {
		return "", nil, ErrBluetoothNotPowered
	}
	return adapter, objects, nil
}

func devicePath(objects bluezObjects, adapter dbus.ObjectPath, address string) (dbus.ObjectPath, error) {
	for path, ifaces := range objects {
		props, ok := ifaces[bluezDevice]
		if !ok {
			continue
		}
		if owner, _ := variantValue[dbus.ObjectPath](props, "Adapter"); owner != adapter {
			continue
		}
		if found, _ := variantValue[string](props, "Address"); strings.EqualFold(found, address) {
			return path, nil
		}
	}
	return "", ErrBluetoothNotFound
}

// StartDiscovery starts Core's discovery session.
func (b *BlueZ) StartDiscovery(ctx context.Context) error {
	adapter, _, err := b.poweredAdapter(ctx)
	if err != nil {
		return err
	}
	// The filter belongs to this connection's session alone. Duplicate
	// reports would only wake Core for signal strength it does not show.
	filter := map[string]dbus.Variant{
		"Transport":     dbus.MakeVariant("auto"),
		"DuplicateData": dbus.MakeVariant(false),
	}
	if _, err := b.quickCall(ctx, adapter, bluezSetDiscoveryFilter, filter); err != nil {
		log.Debug().Err(err).Msg("bluetooth discovery filter refused")
	}
	if _, err := b.radioCall(ctx, adapter, bluezStartDiscovery); err != nil {
		if dbusErrorName(err) == bluezErrInProgress {
			return nil
		}
		return bluezError(err)
	}
	return nil
}

// StopDiscovery ends Core's discovery session. Ending one that is not
// running is not an error.
func (b *BlueZ) StopDiscovery(ctx context.Context) error {
	objects, err := b.objects(ctx)
	if err != nil {
		return err
	}
	adapter, _, err := adapterPath(objects)
	if err != nil {
		return err
	}
	if _, err := b.radioCall(ctx, adapter, bluezStopDiscovery); err != nil {
		switch dbusErrorName(err) {
		case bluezErrNotReady, "org.bluez.Error.Failed":
			// No session of ours, or the adapter went off and took it.
			return nil
		}
		return bluezError(err)
	}
	return nil
}

// Pair bonds with the device. The agent answering the stack's questions is
// registered for this attempt only: on systems where something else already
// runs an agent, Core must not stand in for it any longer than that.
func (b *BlueZ) Pair(ctx context.Context, address string) error {
	b.pairMu.Lock()
	defer b.pairMu.Unlock()

	adapter, objects, err := b.poweredAdapter(ctx)
	if err != nil {
		return err
	}
	device, err := devicePath(objects, adapter, address)
	if err != nil {
		return err
	}

	attempt, cancel := context.WithCancel(ctx)
	defer cancel()
	agent := &pairingAgent{device: device, onPinRequest: cancel}
	if exportErr := b.bus.export(agent, bluezAgentPath, bluezAgent); exportErr != nil {
		return bluezError(exportErr)
	}
	defer b.bus.unexport(bluezAgentPath, bluezAgent)

	// Registration is undone even when the attempt was cancelled, so it gets
	// a context of its own.
	cleanup := context.WithoutCancel(ctx)
	if _, registerErr := b.quickCall(
		cleanup, bluezRoot, bluezRegisterAgent, bluezAgentPath, bluezCapability,
	); registerErr != nil {
		return bluezError(registerErr)
	}
	defer func() {
		_, unregisterErr := b.quickCall(cleanup, bluezRoot, bluezUnregisterAgent, bluezAgentPath)
		if unregisterErr != nil {
			log.Debug().Err(unregisterErr).Msg("could not unregister bluetooth pairing agent")
		}
	}()
	if _, defaultErr := b.quickCall(cleanup, bluezRoot, bluezRequestDefaultAgent, bluezAgentPath); defaultErr != nil {
		// Pair still uses the agent of the connection that asked for it.
		log.Debug().Err(defaultErr).Msg("bluetooth pairing agent is not the default")
	}

	_, err = b.bus.call(attempt, device, bluezPair)
	if agent.pinRequested() {
		b.cancelPairing(cleanup, device)
		return ErrBluetoothPinRequired
	}
	switch {
	case err == nil:
		return nil
	case dbusErrorName(err) == bluezErrAlreadyExists:
		return nil
	case attempt.Err() != nil:
		b.cancelPairing(cleanup, device)
		return fmt.Errorf("pairing %s: %w", address, attempt.Err())
	default:
		return bluezError(err)
	}
}

func (b *BlueZ) cancelPairing(ctx context.Context, device dbus.ObjectPath) {
	if _, err := b.quickCall(ctx, device, bluezCancelPairing); err != nil {
		log.Debug().Err(err).Msg("could not cancel bluetooth pairing")
	}
}

func (b *BlueZ) device(ctx context.Context, address string) (adapter, device dbus.ObjectPath, err error) {
	objects, err := b.objects(ctx)
	if err != nil {
		return "", "", err
	}
	adapter, _, err = adapterPath(objects)
	if err != nil {
		return "", "", err
	}
	device, err = devicePath(objects, adapter, address)
	if err != nil {
		return "", "", err
	}
	return adapter, device, nil
}

// Trust lets the device reconnect by itself.
func (b *BlueZ) Trust(ctx context.Context, address string) error {
	_, device, err := b.device(ctx, address)
	if err != nil {
		return err
	}
	_, err = b.quickCall(ctx, device, bluezPropertiesSet, bluezDevice, "Trusted", dbus.MakeVariant(true))
	return bluezError(err)
}

// Connect connects the device's profiles. It waits as long as ctx allows.
func (b *BlueZ) Connect(ctx context.Context, address string) error {
	_, device, err := b.device(ctx, address)
	if err != nil {
		return err
	}
	if _, err := b.bus.call(ctx, device, bluezConnect); err != nil {
		if dbusErrorName(err) == bluezErrAlreadyConnected {
			return nil
		}
		return bluezError(err)
	}
	return nil
}

// Remove unpairs the device and forgets it.
func (b *BlueZ) Remove(ctx context.Context, address string) error {
	adapter, device, err := b.device(ctx, address)
	if err != nil {
		return err
	}
	_, err = b.radioCall(ctx, adapter, bluezRemoveDevice, device)
	return bluezError(err)
}

// relevantBluezSignal reports whether a signal can change what Read returns.
func relevantBluezSignal(signal *dbus.Signal) bool {
	switch signal.Name {
	case signalInterfacesAdded:
		if len(signal.Body) < 2 {
			return false
		}
		ifaces, ok := signal.Body[1].(map[string]map[string]dbus.Variant)
		if !ok {
			return true
		}
		_, adapter := ifaces[bluezAdapter]
		_, device := ifaces[bluezDevice]
		return adapter || device
	case signalInterfacesRemoved:
		if len(signal.Body) < 2 {
			return false
		}
		ifaces, ok := signal.Body[1].([]string)
		if !ok {
			return true
		}
		for _, iface := range ifaces {
			if iface == bluezAdapter || iface == bluezDevice {
				return true
			}
		}
		return false
	case signalPropertiesChanged:
		if !strings.HasPrefix(string(signal.Path), string(bluezRoot)) || len(signal.Body) == 0 {
			return false
		}
		iface, ok := signal.Body[0].(string)
		return ok && (iface == bluezAdapter || iface == bluezDevice)
	default:
		return false
	}
}

// Watch reports BlueZ's changes until ctx ends, resubscribing when the bus
// connection is lost.
func (b *BlueZ) Watch(ctx context.Context, onChange func()) {
	for {
		signals, stop, err := b.bus.subscribe()
		if err == nil {
			lost := b.forward(ctx, signals, onChange)
			stop()
			if !lost {
				return
			}
			// Whatever happened while the connection was down is unknown.
			onChange()
		} else {
			log.Debug().Err(err).Msg("could not watch bluetooth")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(b.watchRetry):
		}
	}
}

// forward passes signals on until ctx ends or the channel closes, and reports
// which of the two it was.
func (*BlueZ) forward(ctx context.Context, signals <-chan *dbus.Signal, onChange func()) (lost bool) {
	for {
		select {
		case <-ctx.Done():
			return false
		case signal, ok := <-signals:
			if !ok {
				return true
			}
			if signal != nil && relevantBluezSignal(signal) {
				onChange()
			}
		}
	}
}

// pairingAgent answers BlueZ during one pairing attempt. It accepts what
// needs no user input for the device being paired and refuses everything
// else. A request for a PIN or passkey ends the attempt at once instead of
// leaving it to time out.
type pairingAgent struct {
	onPinRequest func()
	device       dbus.ObjectPath
	mu           syncutil.Mutex
	pinRequest   bool
}

func (a *pairingAgent) pinRequested() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pinRequest
}

func (a *pairingAgent) needPin() *dbus.Error {
	a.mu.Lock()
	a.pinRequest = true
	a.mu.Unlock()
	if a.onPinRequest != nil {
		a.onPinRequest()
	}
	return dbus.NewError(bluezErrRejected, nil)
}

func (a *pairingAgent) allow(device dbus.ObjectPath) *dbus.Error {
	if device != a.device {
		return dbus.NewError(bluezErrRejected, nil)
	}
	return nil
}

func (*pairingAgent) Release() *dbus.Error { return nil }

func (*pairingAgent) Cancel() *dbus.Error { return nil }

func (a *pairingAgent) RequestPinCode(dbus.ObjectPath) (string, *dbus.Error) {
	return "", a.needPin()
}

func (a *pairingAgent) DisplayPinCode(dbus.ObjectPath, string) *dbus.Error {
	return a.needPin()
}

func (a *pairingAgent) RequestPasskey(dbus.ObjectPath) (uint32, *dbus.Error) {
	return 0, a.needPin()
}

func (a *pairingAgent) DisplayPasskey(dbus.ObjectPath, uint32, uint16) *dbus.Error {
	return a.needPin()
}

func (a *pairingAgent) RequestConfirmation(device dbus.ObjectPath, _ uint32) *dbus.Error {
	return a.allow(device)
}

func (a *pairingAgent) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	return a.allow(device)
}

func (a *pairingAgent) AuthorizeService(device dbus.ObjectPath, _ string) *dbus.Error {
	return a.allow(device)
}
