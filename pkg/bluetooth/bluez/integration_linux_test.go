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
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests run the real BlueZ layer against a fake bluetoothd exported on
// a private session bus, so the D-Bus mechanics (object export, property
// emission, argument encoding, signal routing) are exercised end to end
// without hardware or a system bluetoothd.

const (
	fakeAdapterPath = dbus.ObjectPath("/org/bluez/hci0")
	fakeDevicePath  = dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_01")
	fakeDeviceAddr  = "AA:BB:CC:DD:EE:01"
	fakeServicePath = fakeDevicePath + "/service0001"
	fakeCharPath    = fakeServicePath + "/char0002"
	fakeServiceUUID = "6e400001-b5a3-f393-e0a9-e50e24dcca9e"
	fakeCharUUID    = "6e400003-b5a3-f393-e0a9-e50e24dcca9e"
	integrationWait = 5 * time.Second
)

// startSessionBus launches a private dbus-daemon and returns its address.
func startSessionBus(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping D-Bus integration test in short mode")
	}
	// On CI the daemon is a declared dependency of the workflow, so its
	// absence is a broken pipeline, not a reason to skip.
	onCI := os.Getenv("CI") != ""
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		if onCI {
			t.Fatal("dbus-daemon is not installed on this CI runner")
		}
		t.Skip("dbus-daemon not installed")
	}

	cmd := exec.CommandContext(t.Context(), "dbus-daemon", "--session", "--print-address", "--nofork", "--nopidfile")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	addrCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			addrCh <- strings.TrimSpace(scanner.Text())
		}
		close(addrCh)
	}()
	select {
	case addr, ok := <-addrCh:
		if !ok || addr == "" {
			reason := "dbus-daemon exited before printing an address: " + strings.TrimSpace(stderr.String())
			if onCI {
				t.Fatal(reason)
			}
			t.Skip(reason)
		}
		return addr
	case <-time.After(integrationWait):
		t.Fatal("dbus-daemon did not print its address")
		return ""
	}
}

// fakeBluez is enough of bluetoothd for the layer to talk to.
type fakeBluez struct {
	conn        *dbus.Conn
	objects     managedObjects
	adapterProp *prop.Properties
	deviceProp  *prop.Properties
	charProp    *prop.Properties
	calls       chan string
	appRoot     chan appRegistration
	advPaths    chan appRegistration
	mu          syncutil.Mutex
	// refuseAdvert makes RegisterAdvertisement fail the way bluetoothd does
	// when the controller cannot advertise.
	refuseAdvert bool
	// outOfRange makes discovery find nothing.
	outOfRange bool
}

type appRegistration struct {
	sender dbus.Sender
	path   dbus.ObjectPath
}

func (f *fakeBluez) GetManagedObjects() (managedObjects, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(managedObjects, len(f.objects))
	for path, ifaces := range f.objects {
		out[path] = ifaces
	}
	return out, nil
}

func (f *fakeBluez) record(call string) {
	select {
	case f.calls <- call:
	default:
	}
}

// fakeAdapter implements the adapter-side interfaces.
type fakeAdapter struct{ f *fakeBluez }

func (a *fakeAdapter) SetDiscoveryFilter(filter map[string]dbus.Variant) *dbus.Error {
	a.f.record("SetDiscoveryFilter:" + stringProp(filter, "Transport"))
	return nil
}

func (a *fakeAdapter) StartDiscovery() *dbus.Error {
	a.f.record("StartDiscovery")
	a.f.mu.Lock()
	if a.f.outOfRange {
		a.f.mu.Unlock()
		return nil
	}
	// Discovery "finds" the device: publish it and announce it.
	a.f.objects[fakeDevicePath] = map[string]map[string]dbus.Variant{
		deviceIface: {
			"Address":          dbus.MakeVariant(fakeDeviceAddr),
			"Name":             dbus.MakeVariant("Fake Reader"),
			"UUIDs":            dbus.MakeVariant([]string{fakeServiceUUID}),
			"RSSI":             dbus.MakeVariant(int16(-61)),
			"ManufacturerData": dbus.MakeVariant(map[uint16]dbus.Variant{0x0059: dbus.MakeVariant([]byte{1, 2})}),
			"ServiceData":      dbus.MakeVariant(map[string]dbus.Variant{fakeServiceUUID: dbus.MakeVariant([]byte{9})}),
			"Connected":        dbus.MakeVariant(false),
			"ServicesResolved": dbus.MakeVariant(false),
		},
	}
	a.f.mu.Unlock()
	_ = a.f.conn.Emit(bluezRootPath, signalInterfacesAdded, fakeDevicePath, a.f.objects[fakeDevicePath])
	return nil
}

func (a *fakeAdapter) StopDiscovery() *dbus.Error {
	a.f.record("StopDiscovery")
	return nil
}

func (a *fakeAdapter) RegisterApplication(
	sender dbus.Sender, path dbus.ObjectPath, _ map[string]dbus.Variant,
) *dbus.Error {
	a.f.record("RegisterApplication")
	a.f.appRoot <- appRegistration{sender: sender, path: path}
	return nil
}

func (a *fakeAdapter) UnregisterApplication(_ dbus.ObjectPath) *dbus.Error {
	a.f.record("UnregisterApplication")
	return nil
}

func (a *fakeAdapter) RegisterAdvertisement(
	sender dbus.Sender, path dbus.ObjectPath, _ map[string]dbus.Variant,
) *dbus.Error {
	a.f.record("RegisterAdvertisement")
	a.f.mu.Lock()
	refuse := a.f.refuseAdvert
	a.f.mu.Unlock()
	if refuse {
		return dbus.NewError("org.bluez.Error.NotPermitted", []any{"Maximum advertisements reached"})
	}
	select {
	case a.f.advPaths <- appRegistration{sender: sender, path: path}:
	default:
	}
	return nil
}

func (a *fakeAdapter) UnregisterAdvertisement(_ dbus.ObjectPath) *dbus.Error {
	a.f.record("UnregisterAdvertisement")
	return nil
}

// fakeDevice implements org.bluez.Device1 for the discovered device.
type fakeDevice struct{ f *fakeBluez }

func (d *fakeDevice) Connect() *dbus.Error {
	d.f.record("Connect")
	d.f.mu.Lock()
	d.f.objects[fakeServicePath] = map[string]map[string]dbus.Variant{
		gattServiceIface: {"UUID": dbus.MakeVariant(fakeServiceUUID), "Primary": dbus.MakeVariant(true)},
	}
	d.f.objects[fakeCharPath] = map[string]map[string]dbus.Variant{
		gattCharIface: {"UUID": dbus.MakeVariant(fakeCharUUID), "Service": dbus.MakeVariant(fakeServicePath)},
	}
	d.f.mu.Unlock()
	d.f.deviceProp.SetMust(deviceIface, "Connected", true)
	d.f.deviceProp.SetMust(deviceIface, "ServicesResolved", true)
	return nil
}

func (d *fakeDevice) Disconnect() *dbus.Error {
	d.f.record("Disconnect")
	d.f.deviceProp.SetMust(deviceIface, "Connected", false)
	return nil
}

// fakeChar implements org.bluez.GattCharacteristic1 for the device's TX.
type fakeChar struct{ f *fakeBluez }

func (c *fakeChar) StartNotify() *dbus.Error {
	c.f.record("StartNotify")
	return nil
}

func (c *fakeChar) StopNotify() *dbus.Error {
	c.f.record("StopNotify")
	return nil
}

func (c *fakeChar) ReadValue(_ map[string]dbus.Variant) ([]byte, *dbus.Error) {
	c.f.record("ReadValue")
	return []byte("read me"), nil
}

func (c *fakeChar) WriteValue(value []byte, options map[string]dbus.Variant) *dbus.Error {
	c.f.record("WriteValue:" + string(value) + ":" + stringProp(options, "type"))
	return nil
}

// newFakeBluez exports the fake on a fresh connection to the bus.
func newFakeBluez(t *testing.T, addr string) *fakeBluez {
	t.Helper()
	conn, err := dbus.Connect(addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	reply, err := conn.RequestName(bluezService, dbus.NameFlagDoNotQueue)
	require.NoError(t, err)
	require.Equal(t, dbus.RequestNameReplyPrimaryOwner, reply)

	f := &fakeBluez{
		conn:     conn,
		calls:    make(chan string, 64),
		appRoot:  make(chan appRegistration, 1),
		advPaths: make(chan appRegistration, 1),
	}
	adapterSpec := prop.Map{adapterIface: {
		"Address": {Value: "00:11:22:33:44:55", Emit: prop.EmitConst},
		"Powered": {Value: true, Writable: true, Emit: prop.EmitTrue},
		"Roles":   {Value: []string{"central", "peripheral", "central-peripheral"}, Emit: prop.EmitConst},
	}}
	f.objects = managedObjects{
		fakeAdapterPath: {
			adapterIface:     variantsOf(adapterSpec)[adapterIface],
			gattManagerIface: {},
			advManagerIface:  {},
		},
	}
	f.adapterProp, err = prop.Export(conn, fakeAdapterPath, adapterSpec)
	require.NoError(t, err)
	require.NoError(t, conn.Export(f, bluezRootPath, objectManagerIface))
	adapter := &fakeAdapter{f: f}
	require.NoError(t, conn.Export(adapter, fakeAdapterPath, adapterIface))
	require.NoError(t, conn.Export(adapter, fakeAdapterPath, gattManagerIface))
	require.NoError(t, conn.Export(adapter, fakeAdapterPath, advManagerIface))

	f.deviceProp, err = prop.Export(conn, fakeDevicePath, prop.Map{deviceIface: {
		"Address":          {Value: fakeDeviceAddr, Emit: prop.EmitConst},
		"RSSI":             {Value: int16(-61), Writable: true, Emit: prop.EmitTrue},
		"Connected":        {Value: false, Writable: true, Emit: prop.EmitTrue},
		"ServicesResolved": {Value: false, Writable: true, Emit: prop.EmitTrue},
	}})
	require.NoError(t, err)
	require.NoError(t, conn.Export(&fakeDevice{f: f}, fakeDevicePath, deviceIface))

	f.charProp, err = prop.Export(conn, fakeCharPath, prop.Map{gattCharIface: {
		"UUID":  {Value: fakeCharUUID, Emit: prop.EmitConst},
		"Value": {Value: []byte{}, Writable: true, Emit: prop.EmitTrue},
		"MTU":   {Value: uint16(247), Emit: prop.EmitConst},
	}})
	require.NoError(t, err)
	require.NoError(t, conn.Export(&fakeChar{f: f}, fakeCharPath, gattCharIface))
	return f
}

func (f *fakeBluez) expectCall(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(integrationWait)
	for {
		select {
		case got := <-f.calls:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("fake bluetoothd never saw %q", want)
		}
	}
}

func TestIntegration_OpenReadsAdapter(t *testing.T) {
	addr := startSessionBus(t)
	_ = newFakeBluez(t, addr)

	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }()

	assert.Equal(t, "00:11:22:33:44:55", adapter.Address())
	assert.ElementsMatch(t, []Role{"central", "peripheral", "central-peripheral"}, adapter.Roles())
	_, err = adapter.Peripheral()
	require.NoError(t, err)
	_, err = adapter.Central()
	require.NoError(t, err)

	require.NoError(t, adapter.Close())
	select {
	case <-adapter.Gone():
	case <-time.After(integrationWait):
		t.Fatal("Close did not mark the adapter gone")
	}
}

func TestIntegration_OpenWithoutBluetoothd(t *testing.T) {
	addr := startSessionBus(t)

	_, err := Open(t.Context(), WithBusAddress(addr), WithCallTimeout(time.Second))
	require.ErrorIs(t, err, ErrUnavailable)
}

// recordingHandler collects peripheral events.
type recordingHandler struct {
	writes      chan string
	disconnects chan Peer
	subscribes  chan bool
	info        []byte
}

func (h *recordingHandler) OnWrite(peer Peer, charUUID string, value []byte, mtu int) {
	h.writes <- peer.Address + "|" + charUUID + "|" + string(value) + "|" + strconv.Itoa(mtu)
}

func (h *recordingHandler) OnRead(Peer, string) ([]byte, error) { return h.info, nil }

func (h *recordingHandler) OnSubscribe(_ Peer, _ string, subscribed bool) { h.subscribes <- subscribed }

func (h *recordingHandler) OnDisconnect(peer Peer) { h.disconnects <- peer }

func TestIntegration_PeripheralServesApplication(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)

	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }()
	peripheral, err := adapter.Peripheral()
	require.NoError(t, err)

	handler := &recordingHandler{
		writes:      make(chan string, 8),
		disconnects: make(chan Peer, 8),
		subscribes:  make(chan bool, 8),
		info:        []byte(`{"v":1}`),
	}
	app := Application{Services: []Service{{
		UUID:    "0da70001-b359-443b-836f-477d34b6a638",
		Primary: true,
		Characteristics: []Characteristic{
			{UUID: "0da70002-b359-443b-836f-477d34b6a638", Flags: []string{FlagWrite, FlagWriteWithoutResponse}},
			{UUID: "0da70003-b359-443b-836f-477d34b6a638", Flags: []string{FlagNotify}},
			{UUID: "0da70004-b359-443b-836f-477d34b6a638", Flags: []string{FlagRead}},
		},
	}}}
	adv := Advertisement{LocalName: "Test Zaparoo", ServiceUUIDs: []string{app.Services[0].UUID}}

	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Add(1)
	serveErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		serveErr <- peripheral.Serve(ctx, app, adv, handler)
	}()

	// bluetoothd reads the application tree on registration.
	var reg appRegistration
	select {
	case reg = <-fake.appRoot:
	case <-time.After(integrationWait):
		t.Fatal("application was never registered")
	}
	var advReg appRegistration
	select {
	case advReg = <-fake.advPaths:
	case <-time.After(integrationWait):
		t.Fatal("advertisement was never registered")
	}

	var tree managedObjects
	require.NoError(t, fake.conn.Object(string(reg.sender), reg.path).
		Call(objectManagerIface+".GetManagedObjects", 0).Store(&tree))
	servicePath := reg.path + "/service0"
	require.Contains(t, tree, servicePath)
	assert.Equal(t, app.Services[0].UUID, stringProp(tree[servicePath][gattServiceIface], "UUID"))
	rxPath, txPath, infoPath := servicePath+"/char0", servicePath+"/char1", servicePath+"/char2"
	for _, p := range []dbus.ObjectPath{rxPath, txPath, infoPath} {
		require.Contains(t, tree, p)
		assert.Equal(t, servicePath, tree[p][gattCharIface]["Service"].Value())
	}
	flags, ok := tree[txPath][gattCharIface]["Flags"].Value().([]string)
	require.True(t, ok)
	assert.Equal(t, []string{FlagNotify}, flags)

	// The advertisement is readable the way bluetoothd reads it.
	var advType dbus.Variant
	require.NoError(t, fake.conn.Object(string(advReg.sender), advReg.path).
		Call(propertiesIface+".Get", 0, advIface, "Type").Store(&advType))
	assert.Equal(t, "peripheral", advType.Value())

	appObj := func(p dbus.ObjectPath) dbus.BusObject { return fake.conn.Object(string(reg.sender), p) }

	// A write from a peer arrives with its device path and MTU.
	writeOpts := map[string]dbus.Variant{
		"device": dbus.MakeVariant(fakeDevicePath),
		"mtu":    dbus.MakeVariant(uint16(185)),
	}
	require.NoError(t, appObj(rxPath).Call(gattCharIface+".WriteValue", 0, []byte("chunk"), writeOpts).Err)
	select {
	case got := <-handler.writes:
		assert.Equal(t, fakeDeviceAddr+"|0da70002-b359-443b-836f-477d34b6a638|chunk|185", got)
	case <-time.After(integrationWait):
		t.Fatal("write never reached the handler")
	}

	// Reads are answered by the handler.
	var value []byte
	require.NoError(t, appObj(infoPath).Call(gattCharIface+".ReadValue", 0, map[string]dbus.Variant{}).Store(&value))
	assert.Equal(t, `{"v":1}`, string(value))

	// Subscriptions are reported.
	require.NoError(t, appObj(txPath).Call(gattCharIface+".StartNotify", 0).Err)
	select {
	case subscribed := <-handler.subscribes:
		assert.True(t, subscribed)
	case <-time.After(integrationWait):
		t.Fatal("subscribe never reached the handler")
	}

	// Notify emits a Value change bluetoothd would forward to subscribers.
	changes := make(chan *dbus.Signal, 8)
	fake.conn.Signal(changes)
	require.NoError(t, fake.conn.AddMatchSignal(
		dbus.WithMatchInterface(propertiesIface), dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchObjectPath(txPath),
	))
	require.NoError(t, peripheral.Notify(app.Services[0].Characteristics[1].UUID, []byte("reply")))
	select {
	case sig := <-changes:
		iface, changed, ok := propertiesChanged(sig)
		require.True(t, ok)
		assert.Equal(t, gattCharIface, iface)
		got, _ := changedBytes(changed, "Value")
		assert.Equal(t, []byte("reply"), got)
	case <-time.After(integrationWait):
		t.Fatal("notify emitted no property change")
	}
	require.ErrorIs(t, peripheral.Notify("00000000-0000-0000-0000-000000000000", []byte("x")), ErrNotFound)

	// A peer dropping its link is reported.
	fake.deviceProp.SetMust(deviceIface, "Connected", true)
	fake.deviceProp.SetMust(deviceIface, "Connected", false)
	select {
	case peer := <-handler.disconnects:
		assert.Equal(t, string(fakeDevicePath), peer.Path)
		assert.Equal(t, fakeDeviceAddr, peer.Address)
	case <-time.After(integrationWait):
		t.Fatal("disconnect never reached the handler")
	}

	// So is bluetoothd forgetting the device altogether, which it announces
	// from the root object with the device named in the signal body.
	require.NoError(t, fake.conn.Emit(bluezRootPath, signalInterfacesRemoved, fakeDevicePath, []string{deviceIface}))
	select {
	case peer := <-handler.disconnects:
		assert.Equal(t, string(fakeDevicePath), peer.Path)
	case <-time.After(integrationWait):
		t.Fatal("device removal never reached the handler")
	}

	// A controller stops advertising while a central is connected and is
	// not reliably restarted when it leaves, so the advertisement is
	// registered afresh: once for the two departures above, not per signal.
	fake.expectCall(t, "UnregisterAdvertisement")
	fake.expectCall(t, "RegisterAdvertisement")
	select {
	case call := <-fake.calls:
		t.Fatalf("a burst of departures restarted advertising more than once: %s", call)
	case <-time.After(advRestartDelay + 500*time.Millisecond):
	}

	// Disconnect asks bluetoothd to drop the peer.
	require.NoError(t, peripheral.Disconnect(t.Context(), Peer{Path: string(fakeDevicePath)}))
	fake.expectCall(t, "Disconnect")

	cancel()
	wg.Wait()
	require.NoError(t, <-serveErr)
	fake.expectCall(t, "UnregisterAdvertisement")
	fake.expectCall(t, "UnregisterApplication")
}

func TestIntegration_CentralFindsConnectsAndSubscribes(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)

	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }()
	central, err := adapter.Central()
	require.NoError(t, err)

	findCtx, cancelFind := context.WithTimeout(t.Context(), integrationWait)
	defer cancelFind()
	dev, err := central.Find(findCtx, strings.ToLower(fakeDeviceAddr))
	require.NoError(t, err)
	assert.Equal(t, fakeDeviceAddr, dev.Address())
	fake.expectCall(t, "SetDiscoveryFilter:le")
	fake.expectCall(t, "StartDiscovery")
	fake.expectCall(t, "StopDiscovery")

	require.NoError(t, dev.Connect(t.Context()))
	fake.expectCall(t, "Connect")

	_, err = dev.Characteristic(fakeServiceUUID, "00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, ErrNotFound)
	tx, err := dev.Characteristic(strings.ToUpper(fakeServiceUUID), fakeCharUUID)
	require.NoError(t, err)

	subCtx, cancelSub := context.WithCancel(t.Context())
	defer cancelSub()
	values, err := tx.Subscribe(subCtx)
	require.NoError(t, err)
	fake.expectCall(t, "StartNotify")

	fake.charProp.SetMust(gattCharIface, "Value", []byte("SCAN\tuid=1\n"))
	select {
	case v := <-values:
		assert.Equal(t, "SCAN\tuid=1\n", string(v))
	case <-time.After(integrationWait):
		t.Fatal("notification never arrived")
	}

	require.NoError(t, tx.Write(t.Context(), []byte("hello"), false))
	fake.expectCall(t, "WriteValue:hello:command")
	require.NoError(t, tx.Write(t.Context(), []byte("ack"), true))
	fake.expectCall(t, "WriteValue:ack:request")

	// A device that rebuilds its services keeps the link but drops the
	// characteristic, which ends that subscription.
	rebuiltCtx, cancelRebuilt := context.WithCancel(t.Context())
	defer cancelRebuilt()
	rebuilt, err := tx.Subscribe(rebuiltCtx)
	require.NoError(t, err)
	require.NoError(t, fake.conn.Emit(bluezRootPath, signalInterfacesRemoved, fakeCharPath, []string{gattCharIface}))
	select {
	case _, open := <-rebuilt:
		assert.False(t, open, "stream closes when the characteristic is removed")
	case <-time.After(integrationWait):
		t.Fatal("stream outlived its characteristic")
	}
	select {
	case v, open := <-values:
		assert.False(t, open, "the other subscription to it closes too, got %q", v)
	case <-time.After(integrationWait):
		t.Fatal("second stream outlived its characteristic")
	}

	// The link dropping is visible on Disconnected.
	fake.deviceProp.SetMust(deviceIface, "Connected", false)
	select {
	case <-dev.Disconnected():
	case <-time.After(integrationWait):
		t.Fatal("disconnect was not noticed")
	}
	linkCtx, cancelLink := context.WithCancel(t.Context())
	defer cancelLink()
	if afterDrop, subErr := tx.Subscribe(linkCtx); subErr == nil {
		select {
		case _, open := <-afterDrop:
			assert.False(t, open, "stream closes after disconnect")
		case <-time.After(integrationWait):
			t.Fatal("stream did not close")
		}
	}

	// A device that never shows up times out cleanly.
	missingCtx, cancelMissing := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancelMissing()
	_, err = central.Find(missingCtx, "AA:BB:CC:DD:EE:02")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestIntegration_CentralScansReadsAndReportsMTU(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)

	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }()
	central, err := adapter.Central()
	require.NoError(t, err)

	scanCtx, cancelScan := context.WithCancel(t.Context())
	defer cancelScan()
	results, err := central.Scan(scanCtx, ScanFilter{ServiceUUIDs: []string{strings.ToUpper(fakeServiceUUID)}})
	require.NoError(t, err)
	fake.expectCall(t, "SetDiscoveryFilter:le")
	fake.expectCall(t, "StartDiscovery")

	select {
	case r := <-results:
		assert.Equal(t, fakeDeviceAddr, r.Address)
		assert.Equal(t, "Fake Reader", r.Name)
		assert.Equal(t, []string{fakeServiceUUID}, r.ServiceUUIDs)
		assert.True(t, r.HasRSSI)
		assert.Equal(t, int16(-61), r.RSSI)
		assert.Equal(t, map[uint16][]byte{0x0059: {1, 2}}, r.ManufacturerData)
		assert.Equal(t, map[string][]byte{fakeServiceUUID: {9}}, r.ServiceData)
	case <-time.After(integrationWait):
		t.Fatal("scan reported nothing")
	}

	// A later sighting updates the same device. The first sighting may be
	// reported more than once, so read until the update shows.
	fake.deviceProp.SetMust(deviceIface, "RSSI", int16(-40))
	deadline := time.After(integrationWait)
	for updated := false; !updated; {
		select {
		case r := <-results:
			assert.Equal(t, "Fake Reader", r.Name, "earlier details are kept")
			updated = r.RSSI == -40
		case <-deadline:
			t.Fatal("scan did not report the update")
		}
	}

	cancelScan()
	for open := true; open; {
		select {
		case _, open = <-results:
		case <-deadline:
			t.Fatal("scan did not stop")
		}
	}
	fake.expectCall(t, "StopDiscovery")

	// A scan for a service nothing advertises reports nothing.
	otherCtx, cancelOther := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancelOther()
	none, err := central.Scan(otherCtx, ScanFilter{ServiceUUIDs: []string{"0000180f-0000-1000-8000-00805f9b34fb"}})
	require.NoError(t, err)
	_, open := <-none
	assert.False(t, open)

	dev, err := central.Find(t.Context(), fakeDeviceAddr)
	require.NoError(t, err)
	require.NoError(t, dev.Connect(t.Context()))
	ch, err := dev.Characteristic(fakeServiceUUID, fakeCharUUID)
	require.NoError(t, err)

	value, err := ch.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "read me", string(value))
	mtu, err := ch.MTU(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 247, mtu)

	// bluetoothd forgetting the device ends the link.
	require.NoError(t, fake.conn.Emit(bluezRootPath, signalInterfacesRemoved, fakeDevicePath, []string{deviceIface}))
	select {
	case <-dev.Disconnected():
	case <-time.After(integrationWait):
		t.Fatal("device removal was not noticed")
	}
}

// waitGone fails unless the adapter is given up.
func waitGone(t *testing.T, adapter Adapter, why string) {
	t.Helper()
	select {
	case <-adapter.Gone():
	case <-time.After(integrationWait):
		t.Fatalf("adapter not marked gone after %s", why)
	}
}

// Everything registered with bluetoothd dies with it, and a restart sends
// no removal signals: losing the bus name is the only notice.
func TestIntegration_AdapterGoneWhenBluetoothdLeavesTheBus(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)

	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }()

	_, err = fake.conn.ReleaseName(bluezService)
	require.NoError(t, err)
	waitGone(t, adapter, "bluetoothd left the bus")
}

func TestIntegration_AdapterGoneWhenPoweredOff(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)

	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }()

	// Unrelated adapter changes are not a loss.
	fake.adapterProp.SetMust(adapterIface, "Powered", true)
	select {
	case <-adapter.Gone():
		t.Fatal("adapter given up while still powered")
	case <-time.After(200 * time.Millisecond):
	}

	fake.adapterProp.SetMust(adapterIface, "Powered", false)
	waitGone(t, adapter, "power off")
}

func serveTestApplication(t *testing.T, peripheral Peripheral) <-chan error {
	t.Helper()
	handler := &recordingHandler{
		writes:      make(chan string, 8),
		disconnects: make(chan Peer, 8),
		subscribes:  make(chan bool, 8),
	}
	app := Application{Services: []Service{{
		UUID:            "0da70001-b359-443b-836f-477d34b6a638",
		Primary:         true,
		Characteristics: []Characteristic{{UUID: "0da70003-b359-443b-836f-477d34b6a638", Flags: []string{FlagNotify}}},
	}}}
	adv := Advertisement{LocalName: "Test Zaparoo", ServiceUUIDs: []string{app.Services[0].UUID}}
	serveErr := make(chan error, 1)
	go func() { serveErr <- peripheral.Serve(t.Context(), app, adv, handler) }()
	return serveErr
}

// A controller reset makes bluetoothd drop the advertisement and tell us so.
// Nobody can find the device after that, so the adapter is given up.
func TestIntegration_AdapterGoneWhenAdvertisementIsReleased(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)

	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }()
	peripheral, err := adapter.Peripheral()
	require.NoError(t, err)
	serveErr := serveTestApplication(t, peripheral)

	var advReg appRegistration
	select {
	case advReg = <-fake.advPaths:
	case <-time.After(integrationWait):
		t.Fatal("advertisement was never registered")
	}
	require.NoError(t, fake.conn.Object(string(advReg.sender), advReg.path).Call(advIface+".Release", 0).Err)

	waitGone(t, adapter, "advertisement release")
	select {
	case err := <-serveErr:
		require.NoError(t, err)
	case <-time.After(integrationWait):
		t.Fatal("Serve kept running on a lost adapter")
	}
}

// An adapter that refuses to advertise is given up rather than held open
// with nothing listening, so its owner tries again.
func TestIntegration_AdapterGoneWhenAdvertisingIsRefused(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	fake.mu.Lock()
	fake.refuseAdvert = true
	fake.mu.Unlock()

	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }()
	peripheral, err := adapter.Peripheral()
	require.NoError(t, err)

	select {
	case err := <-serveTestApplication(t, peripheral):
		require.ErrorContains(t, err, "Maximum advertisements reached")
	case <-time.After(integrationWait):
		t.Fatal("Serve did not report the refusal")
	}
	waitGone(t, adapter, "advertising refused")
	fake.expectCall(t, "UnregisterApplication")
}

// A device bluetoothd remembers from an earlier session is not "found" until
// it is heard again: connecting to a remembered device waits on a slow
// background scan, which is what made a reader take many seconds to pick up.
func TestIntegration_FindWaitsForARememberedDeviceToBeHeard(t *testing.T) {
	addr := startSessionBus(t)
	fake := newFakeBluez(t, addr)
	fake.mu.Lock()
	fake.outOfRange = true
	fake.objects[fakeDevicePath] = map[string]map[string]dbus.Variant{
		deviceIface: {
			"Address":   dbus.MakeVariant(fakeDeviceAddr),
			"Connected": dbus.MakeVariant(false),
		},
	}
	fake.mu.Unlock()

	adapter, err := Open(t.Context(), WithBusAddress(addr))
	require.NoError(t, err)
	defer func() { _ = adapter.Close() }()
	central, err := adapter.Central()
	require.NoError(t, err)

	type found struct {
		dev Device
		err error
	}
	result := make(chan found, 1)
	go func() {
		dev, findErr := central.Find(t.Context(), fakeDeviceAddr)
		result <- found{dev: dev, err: findErr}
	}()
	fake.expectCall(t, "StartDiscovery")
	select {
	case r := <-result:
		t.Fatalf("a remembered device was returned without being heard: %v %v", r.dev, r.err)
	case <-time.After(300 * time.Millisecond):
	}

	// It starts advertising: the scan hears it and Find returns.
	fake.deviceProp.SetMust(deviceIface, "RSSI", int16(-50))
	select {
	case r := <-result:
		require.NoError(t, r.err)
		assert.Equal(t, fakeDeviceAddr, r.dev.Address())
	case <-time.After(integrationWait):
		t.Fatal("the device was heard but never found")
	}

	// One that is already connected needs no scan at all.
	fake.mu.Lock()
	fake.objects[fakeDevicePath][deviceIface]["Connected"] = dbus.MakeVariant(true)
	fake.mu.Unlock()
	fake.expectCall(t, "StopDiscovery")
	dev, err := central.Find(t.Context(), fakeDeviceAddr)
	require.NoError(t, err)
	assert.Equal(t, fakeDeviceAddr, dev.Address())
	select {
	case call := <-fake.calls:
		t.Fatalf("a connected device was scanned for: %s", call)
	default:
	}
}
