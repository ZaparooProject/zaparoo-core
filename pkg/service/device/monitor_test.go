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
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testWait = 5 * time.Second

// fakeDevice is a device whose state tests change at will. Every read is
// announced on reads so a test can wait for the monitor to have looked.
type fakeDevice struct {
	networkErr  error
	displayHang chan struct{}
	reads       chan string
	controllers []hoststatus.Controller
	network     hoststatus.Network
	power       power.Detail
	mu          syncutil.Mutex
	readCount   atomic.Int32
}

func newFakeDevice() *fakeDevice {
	percent := 80
	return &fakeDevice{
		reads: make(chan string, 256),
		power: power.Detail{
			Present: true, Percent: &percent, Source: power.SourceBattery, State: power.ChargeDischarging,
			Batteries: []power.Battery{{ID: "BAT0", Percent: &percent, State: power.ChargeDischarging}},
		},
		network: hoststatus.Network{
			Type: hoststatus.LinkWifi, Interface: "wlan0",
			Internet: hoststatus.InternetFull, InternetAuthoritative: true,
			Interfaces: []hoststatus.Interface{{
				Name: "wlan0", Type: hoststatus.LinkWifi, Up: true, Addresses: []string{"192.168.1.20"},
			}},
		},
		controllers: []hoststatus.Controller{},
	}
}

func (d *fakeDevice) note(section string) {
	d.readCount.Add(1)
	select {
	case d.reads <- section:
	default:
	}
}

func (d *fakeDevice) readers() hoststatus.Readers {
	return hoststatus.Readers{
		Power: func() (power.Detail, error) {
			d.note("power")
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.power, nil
		},
		Network: func() (hoststatus.Network, error) {
			d.note("network")
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.network, d.networkErr
		},
		Bluetooth: func() (hoststatus.Bluetooth, error) {
			d.note("bluetooth")
			return hoststatus.Bluetooth{Present: true}, nil
		},
		Storage: func(roots []hoststatus.StorageRoot) ([]hoststatus.Volume, error) {
			d.note("storage")
			volumes := make([]hoststatus.Volume, 0, len(roots))
			for _, root := range roots {
				volumes = append(volumes, hoststatus.Volume{
					Path: root.Path, Roles: []string{root.Role}, Total: 100, Free: 40, Used: 60,
				})
			}
			return volumes, nil
		},
		Display: func() (hoststatus.Display, error) {
			d.note("display")
			if d.displayHang != nil {
				<-d.displayHang
			}
			return hoststatus.Display{}, hoststatus.ErrUnsupported
		},
		Controllers: func() ([]hoststatus.Controller, error) {
			d.note("controllers")
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.controllers, nil
		},
		System: func() (hoststatus.System, error) {
			d.note("system")
			return hoststatus.System{Hostname: "testhost", Model: "Test Model"}, nil
		},
	}
}

func (d *fakeDevice) setControllers(controllers []hoststatus.Controller) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.controllers = controllers
}

func (d *fakeDevice) setNetwork(network *hoststatus.Network, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.network = *network
	d.networkErr = err
}

// awaitRead waits until the monitor has read the named section.
func (d *fakeDevice) awaitRead(t *testing.T, section string) {
	t.Helper()
	deadline := time.After(testWait)
	for {
		select {
		case got := <-d.reads:
			if got == section {
				return
			}
		case <-deadline:
			t.Fatalf("monitor never read %s", section)
		}
	}
}

// fakeProber answers probes from a queue and announces each one.
type fakeProber struct {
	answers chan hoststatus.InternetState
	calls   chan struct{}
}

func newFakeProber() *fakeProber {
	return &fakeProber{answers: make(chan hoststatus.InternetState, 16), calls: make(chan struct{}, 16)}
}

func (p *fakeProber) Probe(ctx context.Context) hoststatus.InternetState {
	p.calls <- struct{}{}
	select {
	case answer := <-p.answers:
		return answer
	case <-ctx.Done():
		return ""
	}
}

type harness struct {
	monitor   *Monitor
	clock     *clockwork.FakeClock
	device    *fakeDevice
	published chan models.DeviceChangedNotification
}

func testPlatform(settings platforms.Settings) *mocks.MockPlatform {
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test")
	pl.On("Settings").Return(settings)
	pl.On("RootDirs", mock.Anything).Return([]string{"/media/games"})
	return pl
}

func newHarness(t *testing.T, pl platforms.Platform, prober InternetProber) *harness {
	t.Helper()
	h := &harness{
		// A date that passes the clock-reliability check.
		clock:     clockwork.NewFakeClockAt(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)),
		device:    newFakeDevice(),
		published: make(chan models.DeviceChangedNotification, 64),
	}
	h.monitor = NewMonitor(&Options{
		Platform: pl,
		Clock:    h.clock,
		Prober:   prober,
		Readers:  h.device.readers(),
		Publish: func(notification models.Notification) {
			assert.Equal(t, models.NotificationDeviceChanged, notification.Method)
			var params models.DeviceChangedNotification
			assert.NoError(t, json.Unmarshal(notification.Params, &params))
			h.published <- params
		},
		ReadTimeout: 50 * time.Millisecond,
	})
	return h
}

// run starts the monitor loop and returns a function that stops it and waits
// for it to finish.
func (h *harness) run(t *testing.T) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.monitor.Run(ctx)
	}()
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case <-done:
		case <-time.After(testWait):
			t.Error("monitor did not stop")
		}
	}
	t.Cleanup(stop)
	return stop
}

func (h *harness) awaitPublish(t *testing.T) models.DeviceChangedNotification {
	t.Helper()
	select {
	case params := <-h.published:
		return params
	case <-time.After(testWait):
		t.Fatal("no device.changed was published")
		return models.DeviceChangedNotification{}
	}
}

// awaitParked waits until the loop is asleep on its timer.
func (h *harness) awaitParked(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	require.NoError(t, h.clock.BlockUntilContext(ctx, 1), "monitor loop never went back to sleep")
}

func (h *harness) assertNothingPublished(t *testing.T) {
	t.Helper()
	select {
	case params := <-h.published:
		t.Fatalf("unexpected device.changed: %+v", params)
	default:
	}
}

func TestMonitor_IdleWithoutClients(t *testing.T) {
	t.Parallel()

	h := newHarness(t, testPlatform(platforms.Settings{}), nil)
	stop := h.run(t)
	h.clock.Advance(time.Hour)
	stop()

	assert.Zero(t, h.device.readCount.Load(), "nothing is read while nobody is connected")
	h.assertNothingPublished(t)
}

func TestMonitor_FirstClientGetsCurrentState(t *testing.T) {
	t.Parallel()

	h := newHarness(t, testPlatform(platforms.Settings{}), nil)
	h.run(t)
	h.monitor.ClientConnected()

	params := h.awaitPublish(t)
	require.NotNil(t, params.Power)
	assert.True(t, params.Power.Present)
	assert.Equal(t, 80, *params.Power.Percent)
	assert.Equal(t, "battery", *params.Power.Source)
	assert.Equal(t, "discharging", *params.Power.ChargeState)
	require.NotNil(t, params.Network)
	assert.Equal(t, "wifi", params.Network.Type)
	assert.Equal(t, "full", *params.Network.Internet)
	require.NotNil(t, params.Bluetooth)
	assert.True(t, params.Bluetooth.Present)
	assert.Nil(t, params.Display, "an unsupported section is null")
	require.NotNil(t, params.Controllers)
	assert.Zero(t, params.Controllers.Count)
	assert.True(t, params.Time.ClockReliable)
}

func TestMonitor_PublishesOnlyOnChange(t *testing.T) {
	t.Parallel()

	h := newHarness(t, testPlatform(platforms.Settings{}), nil)
	h.run(t)
	h.monitor.ClientConnected()
	h.awaitPublish(t)
	h.awaitParked(t)

	// A poll that finds nothing new says nothing.
	for len(h.device.reads) > 0 {
		<-h.device.reads
	}
	h.clock.Advance(5 * time.Second)
	h.device.awaitRead(t, "controllers")
	h.awaitParked(t)
	h.assertNothingPublished(t)

	// A controller appearing is announced on the next poll.
	percent := 60
	h.device.setControllers([]hoststatus.Controller{{
		ID: "input7", Name: "Pad", VendorID: "054c", ProductID: "0ce6",
		Connection: hoststatus.ConnectionBluetooth,
		Battery:    &hoststatus.ControllerBattery{Percent: &percent, Level: hoststatus.BatteryLevelMedium},
	}})
	h.clock.Advance(5 * time.Second)
	params := h.awaitPublish(t)
	require.NotNil(t, params.Controllers)
	require.Len(t, params.Controllers.Items, 1)
	assert.Equal(t, 1, params.Controllers.Count)
	assert.Equal(t, "Pad", *params.Controllers.Items[0].Name)
	assert.Equal(t, 60, *params.Controllers.Items[0].Battery.Percent)
}

func TestMonitor_ConstrainedHardwarePollsLessOften(t *testing.T) {
	t.Parallel()

	h := newHarness(t, testPlatform(platforms.Settings{ResourceConstrained: true}), nil)
	h.run(t)
	h.monitor.ClientConnected()
	h.awaitPublish(t)
	h.awaitParked(t)

	before := h.device.readCount.Load()
	h.clock.Advance(5 * time.Second)
	h.awaitParked(t)
	assert.Equal(t, before, h.device.readCount.Load(), "nothing is due after five seconds")

	for len(h.device.reads) > 0 {
		<-h.device.reads
	}
	h.clock.Advance(10 * time.Second)
	h.device.awaitRead(t, "controllers")
}

func TestMonitor_StopsReadingWhenLastClientLeaves(t *testing.T) {
	t.Parallel()

	h := newHarness(t, testPlatform(platforms.Settings{}), nil)
	stop := h.run(t)
	h.monitor.ClientConnected()
	h.monitor.ClientConnected()
	h.awaitPublish(t)
	h.awaitParked(t)

	h.monitor.ClientDisconnected()
	h.monitor.ClientDisconnected()
	// One more than there were is ignored rather than going negative.
	h.monitor.ClientDisconnected()
	assert.Zero(t, h.monitor.clientCount())

	// The loop drops its timer once it notices nobody is left.
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		return h.clock.BlockUntilContext(ctx, 1) != nil
	}, testWait, 5*time.Millisecond)

	before := h.device.readCount.Load()
	h.clock.Advance(time.Hour)
	stop()
	assert.Equal(t, before, h.device.readCount.Load())
}

func TestMonitor_HungReaderDoesNotBlockOtherSections(t *testing.T) {
	t.Parallel()

	h := newHarness(t, testPlatform(platforms.Settings{}), nil)
	h.device.displayHang = make(chan struct{})
	t.Cleanup(func() { close(h.device.displayHang) })
	h.run(t)
	h.monitor.ClientConnected()

	params := h.awaitPublish(t)
	assert.NotNil(t, params.Power)
	assert.NotNil(t, params.Controllers)
	assert.Nil(t, params.Display)
}

func TestMonitor_OneFailedReadKeepsTheLastReading(t *testing.T) {
	t.Parallel()

	h := newHarness(t, testPlatform(platforms.Settings{}), nil)
	h.monitor.collect()
	require.NotNil(t, h.monitor.current().Network)

	h.device.setNetwork(&hoststatus.Network{}, errors.New("netlink hiccup"))
	h.clock.Advance(5 * time.Second)
	h.monitor.collect()
	require.NotNil(t, h.monitor.current().Network, "one bad read does not blank the section")

	h.clock.Advance(5 * time.Second)
	h.monitor.collect()
	snap := h.monitor.current()
	assert.Nil(t, snap.Network, "a section that keeps failing has no reading")
	assert.Equal(t, hoststatus.Supported, snap.Sections[models.DeviceSectionNetwork])
}

func TestMonitor_UnsupportedSectionIsReportedAndNotPolled(t *testing.T) {
	t.Parallel()

	h := newHarness(t, testPlatform(platforms.Settings{}), nil)
	h.monitor.collect()
	snap := h.monitor.current()
	assert.Equal(t, hoststatus.Unsupported, snap.Sections[models.DeviceSectionDisplay])
	assert.Equal(t, hoststatus.Supported, snap.Sections[models.DeviceSectionPower])
	assert.Equal(t, hoststatus.Supported, snap.Sections[models.DeviceSectionTime])

	for len(h.device.reads) > 0 {
		<-h.device.reads
	}
	h.clock.Advance(time.Minute)
	h.monitor.collect()
	close(h.device.reads)
	for section := range h.device.reads {
		assert.NotEqual(t, "display", section)
	}
}

func TestMonitor_MissingReaderIsUnsupported(t *testing.T) {
	t.Parallel()

	monitor := NewMonitor(&Options{
		Platform: testPlatform(platforms.Settings{}),
		Clock:    clockwork.NewFakeClockAt(time.Unix(0, 0)),
	})
	snap := monitor.Snapshot(context.Background())
	for _, section := range []string{
		models.DeviceSectionPower, models.DeviceSectionNetwork, models.DeviceSectionBluetooth,
		models.DeviceSectionStorage, models.DeviceSectionDisplay, models.DeviceSectionControllers,
		models.DeviceSectionSystem,
	} {
		assert.Equal(t, hoststatus.Unsupported, snap.Sections[section], section)
	}
	assert.False(t, snap.ClockReliable, "a clock at the epoch is not to be trusted")
	_, err := monitor.PowerControl().PreparePowerAction(context.Background(), hoststatus.PowerReboot)
	require.ErrorIs(t, err, hoststatus.ErrUnsupported)
}

func probedNetwork() *hoststatus.Network {
	return &hoststatus.Network{Type: hoststatus.LinkWifi, Interface: "wlan0"}
}

// awaitProbe waits for a probe to start, answers it, and applies the result
// the way the loop would.
func awaitProbe(t *testing.T, m *Monitor, prober *fakeProber, answer hoststatus.InternetState) {
	t.Helper()
	select {
	case <-prober.calls:
	case <-time.After(testWait):
		t.Fatal("no probe was started")
	}
	prober.answers <- answer
	select {
	case result := <-m.probeDone:
		m.finishProbe(result)
	case <-time.After(testWait):
		t.Fatal("probe never finished")
	}
}

func assertNoProbe(t *testing.T, m *Monitor, prober *fakeProber) {
	t.Helper()
	m.startProbeIfDue(context.Background())
	select {
	case <-prober.calls:
		t.Fatal("a probe started before it was due")
	default:
	}
}

func TestMonitor_ProbeBacksOffUntilOnline(t *testing.T) {
	t.Parallel()

	prober := newFakeProber()
	h := newHarness(t, testPlatform(platforms.Settings{}), prober)
	h.device.setNetwork(probedNetwork(), nil)
	m := h.monitor
	ctx := context.Background()

	m.collect()
	assert.Nil(t, StatusResponse(ptr(m.current()), nil).Network.Internet, "not known until a probe has answered")

	// The first probe goes out at once; each failure doubles the wait.
	for _, wait := range []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second} {
		if wait > 0 {
			h.clock.Advance(wait - time.Millisecond)
			assertNoProbe(t, m, prober)
			h.clock.Advance(time.Millisecond)
		}
		m.startProbeIfDue(ctx)
		awaitProbe(t, m, prober, hoststatus.InternetNone)
		assert.Equal(t, "none", *StatusResponse(ptr(m.current()), nil).Network.Internet)
	}

	// Once online it settles to the steady interval.
	h.clock.Advance(8 * time.Second)
	m.startProbeIfDue(ctx)
	awaitProbe(t, m, prober, hoststatus.InternetFull)
	assert.Equal(t, "full", *StatusResponse(ptr(m.current()), nil).Network.Internet)

	h.clock.Advance(probeInterval - time.Second)
	assertNoProbe(t, m, prober)
	h.clock.Advance(time.Second)
	m.startProbeIfDue(ctx)
	awaitProbe(t, m, prober, hoststatus.InternetPortal)
	assert.Equal(t, "portal", *StatusResponse(ptr(m.current()), nil).Network.Internet)

	// Losing it starts the backoff from the beginning.
	h.clock.Advance(time.Second)
	m.startProbeIfDue(ctx)
	awaitProbe(t, m, prober, hoststatus.InternetFull)
}

func TestMonitor_LinkChangeForgetsTheOldAnswer(t *testing.T) {
	t.Parallel()

	prober := newFakeProber()
	h := newHarness(t, testPlatform(platforms.Settings{}), prober)
	h.device.setNetwork(probedNetwork(), nil)
	m := h.monitor

	m.collect()
	m.startProbeIfDue(context.Background())
	awaitProbe(t, m, prober, hoststatus.InternetFull)

	h.device.setNetwork(&hoststatus.Network{Type: hoststatus.LinkWired, Interface: "eth0"}, nil)
	h.clock.Advance(5 * time.Second)
	m.collect()
	assert.Nil(t, StatusResponse(ptr(m.current()), nil).Network.Internet)
	m.startProbeIfDue(context.Background())
	awaitProbe(t, m, prober, hoststatus.InternetNone)
}

func TestMonitor_StaleProbeResultIsDiscarded(t *testing.T) {
	t.Parallel()

	prober := newFakeProber()
	h := newHarness(t, testPlatform(platforms.Settings{}), prober)
	h.device.setNetwork(probedNetwork(), nil)
	m := h.monitor

	m.collect()
	m.finishProbe(probeResult{routeKey: "wired|eth0", state: hoststatus.InternetFull})
	assert.Nil(t, StatusResponse(ptr(m.current()), nil).Network.Internet)
	m.finishProbe(probeResult{routeKey: m.routeKey, state: ""})
	assert.Nil(t, StatusResponse(ptr(m.current()), nil).Network.Internet)
}

func TestMonitor_NoProbeWithoutALinkOrWhenTheSystemAnswers(t *testing.T) {
	t.Parallel()

	prober := newFakeProber()
	h := newHarness(t, testPlatform(platforms.Settings{}), prober)
	m := h.monitor

	// The default fake network is authoritative.
	m.collect()
	assertNoProbe(t, m, prober)
	assert.Equal(t, "full", *StatusResponse(ptr(m.current()), nil).Network.Internet)

	h.device.setNetwork(&hoststatus.Network{Type: hoststatus.LinkNone}, nil)
	h.clock.Advance(5 * time.Second)
	m.collect()
	assertNoProbe(t, m, prober)
}

func TestMonitor_NoProbeWhenTurnedOffInConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML("[service]\ninternet_check = false\n"))

	prober := newFakeProber()
	device := newFakeDevice()
	device.setNetwork(probedNetwork(), nil)
	monitor := NewMonitor(&Options{
		Platform: testPlatform(platforms.Settings{}),
		Config:   cfg,
		Clock:    clockwork.NewFakeClockAt(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)),
		Prober:   prober,
		Readers:  device.readers(),
	})

	snap := monitor.Snapshot(context.Background())
	assertNoProbe(t, monitor, prober)
	response := StatusResponse(&snap, nil)
	assert.Equal(t, "wifi", response.Network.Type)
	assert.Nil(t, response.Network.Internet, "reachability is not known when Core may not ask")
	_, scheduled := monitor.nextDelay()
	assert.True(t, scheduled, "the other sections are still polled")
}

func TestMonitor_SnapshotProbesForACallerWithNoConnection(t *testing.T) {
	t.Parallel()

	prober := newFakeProber()
	prober.answers <- hoststatus.InternetFull
	h := newHarness(t, testPlatform(platforms.Settings{DataDir: "/data/zaparoo"}), prober)
	h.device.setNetwork(probedNetwork(), nil)

	snap := h.monitor.Snapshot(context.Background())
	response := StatusResponse(&snap, nil)
	assert.Equal(t, "full", *response.Network.Internet)
	assert.Equal(t, "testhost", response.System.Hostname)
	require.Len(t, response.Storage.Volumes, 2)
	assert.Equal(t, "/media/games", response.Storage.Volumes[0].Path)
	assert.Equal(t, []string{hoststatus.RoleMedia}, response.Storage.Volumes[0].Roles)
	assert.Equal(t, []string{hoststatus.RoleData}, response.Storage.Volumes[1].Roles)

	// A second request inside the interval reads nothing again.
	before := h.device.readCount.Load()
	h.monitor.Snapshot(context.Background())
	assert.Equal(t, before, h.device.readCount.Load())
}

func TestMonitor_ProbeStopsWhenClientsLeave(t *testing.T) {
	t.Parallel()

	prober := newFakeProber()
	h := newHarness(t, testPlatform(platforms.Settings{}), prober)
	h.device.setNetwork(probedNetwork(), nil)
	stop := h.run(t)
	h.monitor.ClientConnected()

	select {
	case <-prober.calls:
	case <-time.After(testWait):
		t.Fatal("no probe was started")
	}
	// The probe is still out when the last client leaves; stopping must not
	// wait on an answer that never comes.
	h.monitor.ClientDisconnected()
	stop()
}

// pushingPlatform announces its own changes.
type pushingPlatform struct {
	*mocks.MockPlatform
	onChange func()
	mu       syncutil.Mutex
}

func (p *pushingPlatform) SetDeviceStatusChanged(onChange func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onChange = onChange
}

func (p *pushingPlatform) push() bool {
	p.mu.Lock()
	onChange := p.onChange
	p.mu.Unlock()
	if onChange == nil {
		return false
	}
	onChange()
	return true
}

func TestMonitor_PushedPlatformIsReadOnlyWhenItSaysSo(t *testing.T) {
	t.Parallel()

	pl := &pushingPlatform{MockPlatform: testPlatform(platforms.Settings{})}
	h := newHarness(t, pl, nil)
	stop := h.run(t)
	h.monitor.ClientConnected()
	h.awaitPublish(t)

	before := h.device.readCount.Load()
	h.clock.Advance(time.Hour)
	assert.Equal(t, before, h.device.readCount.Load(), "a pushed platform is never polled")

	h.device.setControllers([]hoststatus.Controller{{ID: "pad", Name: "Pad"}})
	require.True(t, pl.push())
	params := h.awaitPublish(t)
	assert.Equal(t, 1, params.Controllers.Count)

	stop()
	assert.False(t, pl.push(), "the callback is removed when the monitor stops")
}

func ptr[T any](value T) *T { return &value }
