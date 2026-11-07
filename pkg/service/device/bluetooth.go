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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// BluetoothScanDefault and BluetoothScanMax bound a scan, so the radio
	// never stays searching because a client forgot about it.
	BluetoothScanDefault = 60 * time.Second
	BluetoothScanMax     = 120 * time.Second
	// bluetoothPairTimeout bounds a whole attempt: pairing, trusting and
	// connecting.
	bluetoothPairTimeout = 30 * time.Second
	// bluetoothDebounce gathers a discovery burst into one notification.
	bluetoothDebounce = 250 * time.Millisecond
	// bluetoothCleanupTimeout bounds the stack calls Core makes on its own
	// behalf, with no request waiting on them.
	bluetoothCleanupTimeout = 5 * time.Second
	// bluetoothStopRetry is how long Core waits to stop a scan again after
	// the stack refused.
	bluetoothStopRetry = 30 * time.Second
)

// ErrBluetoothBusy means a pairing attempt is already running.
var ErrBluetoothBusy = errors.New("a bluetooth pairing attempt is already running")

var errPairTimeout = errors.New("bluetooth pairing timed out")

// BluetoothOptions configures a Bluetooth manager.
type BluetoothOptions struct {
	Control hoststatus.BluetoothController
	Clock   clockwork.Clock
	// Publish delivers a notification to every connected client.
	Publish func(models.Notification)
	// Controllers returns the connected controllers last read, to match
	// Bluetooth devices against.
	Controllers func() []hoststatus.Controller
	// PairTimeout and Debounce are zero for the defaults.
	PairTimeout time.Duration
	Debounce    time.Duration
}

// Bluetooth scans for, pairs and forgets Bluetooth devices, and tells clients
// what changed while any are connected. Scans end by themselves, and when the
// last client goes.
type Bluetooth struct {
	ctl           hoststatus.BluetoothController
	clock         clockwork.Clock
	publish       func(models.Notification)
	controllers   func() []hoststatus.Controller
	base          context.Context
	stop          context.CancelFunc
	discovered    map[string]bool
	pairing       *models.DeviceBluetoothPairing
	scanTimer     clockwork.Timer
	flushTimer    clockwork.Timer
	watchCancel   context.CancelFunc
	lastPublished []byte
	wg            sync.WaitGroup
	pairTimeout   time.Duration
	debounce      time.Duration
	scanGen       uint64
	clients       int
	// opMu serialises changes to discovery and pairing so what the stack is
	// doing and what Core reports agree. It is always taken before mu, and
	// never by anything the stack's change signals run.
	opMu syncutil.Mutex
	// flushMu keeps notifications in the order their states were read.
	flushMu    syncutil.Mutex
	mu         syncutil.Mutex
	scanning   bool
	scanAll    bool
	pairActive bool
	closed     bool
	// attempts counts pair requests accepted since Core started.
	attempts int
}

// NewBluetooth builds a manager. It does nothing until it is asked to or a
// client connects.
func NewBluetooth(opts *BluetoothOptions) *Bluetooth {
	base, stop := context.WithCancel(context.Background())
	manager := &Bluetooth{
		ctl:         opts.Control,
		clock:       opts.Clock,
		publish:     opts.Publish,
		controllers: opts.Controllers,
		base:        base,
		stop:        stop,
		discovered:  map[string]bool{},
		pairTimeout: opts.PairTimeout,
		debounce:    opts.Debounce,
	}
	if manager.clock == nil {
		manager.clock = clockwork.NewRealClock()
	}
	if manager.pairTimeout <= 0 {
		manager.pairTimeout = bluetoothPairTimeout
	}
	if manager.debounce <= 0 {
		manager.debounce = bluetoothDebounce
	}
	return manager
}

// Available reports whether the stack is running and has an adapter, powered
// or not.
func (b *Bluetooth) Available(ctx context.Context) bool {
	_, err := b.ctl.Read(ctx)
	return err == nil
}

// ClientConnected records that a client is listening for notifications.
func (b *Bluetooth) ClientConnected() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clients++
	if b.clients != 1 || b.closed {
		return
	}
	ctx, cancel := context.WithCancel(b.base)
	b.watchCancel = cancel
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.ctl.Watch(ctx, b.changed)
	}()
}

// ClientDisconnected records that a client has gone. With nobody left to see
// it, a running scan is stopped.
func (b *Bluetooth) ClientDisconnected() {
	b.mu.Lock()
	if b.clients > 0 {
		b.clients--
	}
	last := b.clients == 0
	if last && b.watchCancel != nil {
		b.watchCancel()
		b.watchCancel = nil
	}
	if last && b.flushTimer != nil {
		b.flushTimer.Stop()
		b.flushTimer = nil
	}
	scanning := b.scanning && !b.closed
	generation := b.scanGen
	if last && scanning {
		b.wg.Add(1)
	}
	b.mu.Unlock()

	if last && scanning {
		// The disconnect path must not wait on the stack.
		go func() {
			defer b.wg.Done()
			ctx, cancel := context.WithTimeout(b.base, bluetoothCleanupTimeout)
			defer cancel()
			b.opMu.Lock()
			defer b.opMu.Unlock()
			// A client may have connected and started a scan of its own
			// before this ran. Only the scan nobody is left to see is stopped.
			b.mu.Lock()
			stale := b.clients == 0 && b.scanGen == generation
			b.mu.Unlock()
			if stale {
				logStopErr(b.stopScanLocked(ctx))
			}
		}()
	}
}

// Close stops any scan and waits for background work to finish.
func (b *Bluetooth) Close() {
	b.mu.Lock()
	b.closed = true
	if b.scanTimer != nil {
		b.scanTimer.Stop()
	}
	if b.flushTimer != nil {
		b.flushTimer.Stop()
		b.flushTimer = nil
	}
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), bluetoothCleanupTimeout)
	b.opMu.Lock()
	logStopErr(b.stopScanLocked(ctx))
	b.opMu.Unlock()
	cancel()

	b.stop()
	b.wg.Wait()
}

// changed schedules a notification once a burst of changes has settled.
func (b *Bluetooth) changed() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.clients == 0 || b.closed || b.flushTimer != nil {
		return
	}
	b.flushTimer = b.clock.AfterFunc(b.debounce, b.flush)
}

// flush publishes the current state when it differs from what clients were
// last told.
func (b *Bluetooth) flush() {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()

	b.mu.Lock()
	b.flushTimer = nil
	listening := b.clients > 0 && !b.closed
	b.mu.Unlock()
	if !listening {
		return
	}

	ctx, cancel := context.WithTimeout(b.base, bluetoothCleanupTimeout)
	defer cancel()
	state, err := b.State(ctx)
	switch {
	case err == nil:
	case errors.Is(err, hoststatus.ErrUnsupported) || errors.Is(err, hoststatus.ErrBluetoothNoAdapter):
		// The adapter or the stack went away. Clients hear that it is off and
		// has nothing on it.
		state = b.emptyState()
	default:
		log.Debug().Err(err).Msg("could not read bluetooth state")
		return
	}
	payload, err := json.Marshal(state)
	if err != nil {
		log.Error().Err(err).Msg("could not encode bluetooth state")
		return
	}

	b.mu.Lock()
	changed := !bytes.Equal(payload, b.lastPublished)
	if changed {
		b.lastPublished = payload
	}
	listening = b.clients > 0 && !b.closed
	b.mu.Unlock()
	if changed && listening && b.publish != nil {
		b.publish(models.Notification{Method: models.NotificationDeviceBluetooth, Params: payload})
	}
}

func (b *Bluetooth) emptyState() models.DeviceBluetoothState {
	b.mu.Lock()
	defer b.mu.Unlock()
	// No stack or no adapter means no scan.
	b.scanEndedLocked()
	return models.DeviceBluetoothState{
		Devices:  []models.DeviceBluetoothDevice{},
		Scanning: b.scanning,
		Pairing:  b.pairingCopyLocked(),
	}
}

func (b *Bluetooth) pairingCopyLocked() *models.DeviceBluetoothPairing {
	if b.pairing == nil {
		return nil
	}
	pairing := *b.pairing
	return &pairing
}

// State reads the stack and returns what clients are shown: paired devices,
// devices heard by the current or last scan, and the latest pairing attempt.
func (b *Bluetooth) State(ctx context.Context) (models.DeviceBluetoothState, error) {
	// A scan started while the read is in flight must not be ended by a
	// reading taken before it began.
	b.mu.Lock()
	generation := b.scanGen
	b.mu.Unlock()
	reading, err := b.ctl.Read(ctx)
	if err != nil {
		return models.DeviceBluetoothState{}, err //nolint:wrapcheck // callers branch on hoststatus errors
	}
	controllerIDs := map[string]string{}
	if b.controllers != nil {
		for _, controller := range b.controllers() {
			if controller.Address != "" {
				controllerIDs[strings.ToUpper(controller.Address)] = controller.ID
			}
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if !reading.Discovering && b.scanGen == generation {
		// The stack ended the scan itself: a fault, a reset, or a restart
		// of the stack that took Core's discovery session with it.
		b.scanEndedLocked()
	}
	pairingID := ""
	if b.pairing != nil {
		pairingID = b.pairing.ID
	}
	devices := make([]models.DeviceBluetoothDevice, 0, len(reading.Devices))
	for i := range reading.Devices {
		device := &reading.Devices[i]
		if b.scanning && device.InRange {
			b.discovered[device.Address] = true
		}
		gamepad := hoststatus.IsBluetoothGamepad(device)
		listed := device.Paired || device.Address == pairingID ||
			(b.discovered[device.Address] && (gamepad || b.scanAll))
		if !listed {
			continue
		}
		item := models.DeviceBluetoothDevice{
			ID:          device.Address,
			Name:        optional(device.Name),
			DisplayName: optional(hoststatus.ControllerDisplayName(device.VendorID, device.ProductID)),
			VendorID:    optional(device.VendorID),
			ProductID:   optional(device.ProductID),
			Paired:      device.Paired,
			Connected:   device.Connected,
			Trusted:     device.Trusted,
			Gamepad:     gamepad,
		}
		if id, ok := controllerIDs[device.Address]; ok && device.Connected {
			item.ControllerID = &id
		}
		devices = append(devices, item)
	}
	// Paired devices first, then by address, so a list does not reorder
	// itself as names arrive.
	sort.SliceStable(devices, func(i, j int) bool {
		if devices[i].Paired != devices[j].Paired {
			return devices[i].Paired
		}
		return devices[i].ID < devices[j].ID
	})
	return models.DeviceBluetoothState{
		Powered:  reading.Powered,
		Scanning: b.scanning,
		Devices:  devices,
		Pairing:  b.pairingCopyLocked(),
	}, nil
}

// StartScan starts discovery, or extends the one running, for timeout from
// now. Discovery stops by itself when timeout passes. Extending keeps what the
// running scan has found; only a scan started from idle begins empty.
func (b *Bluetooth) StartScan(
	ctx context.Context,
	all bool,
	timeout time.Duration,
) (models.DeviceBluetoothState, error) {
	if timeout <= 0 {
		timeout = BluetoothScanDefault
	}
	if timeout > BluetoothScanMax {
		timeout = BluetoothScanMax
	}

	b.opMu.Lock()
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		b.opMu.Unlock()
		return models.DeviceBluetoothState{}, hoststatus.ErrUnsupported
	}
	err := b.ctl.StartDiscovery(ctx)
	if err == nil {
		b.mu.Lock()
		if !b.scanning {
			// A new scan replaces what the last one found.
			b.discovered = map[string]bool{}
		}
		b.scanning = true
		b.scanAll = all
		b.scanGen++
		generation := b.scanGen
		if b.scanTimer != nil {
			b.scanTimer.Stop()
		}
		b.scanTimer = b.clock.AfterFunc(timeout, func() { b.scanExpired(generation) })
		b.mu.Unlock()
	}
	b.opMu.Unlock()
	if err != nil {
		return models.DeviceBluetoothState{}, err //nolint:wrapcheck // callers branch on hoststatus errors
	}
	b.changed()
	return b.State(ctx)
}

// StopScan stops discovery. When the stack refuses, the scan keeps running,
// Core tries again later, and the error is returned.
func (b *Bluetooth) StopScan(ctx context.Context) (models.DeviceBluetoothState, error) {
	b.opMu.Lock()
	err := b.stopScanLocked(ctx)
	b.opMu.Unlock()
	if err != nil {
		return models.DeviceBluetoothState{}, err
	}
	return b.State(ctx)
}

func (b *Bluetooth) scanExpired(generation uint64) {
	ctx, cancel := context.WithTimeout(b.base, bluetoothCleanupTimeout)
	defer cancel()
	b.opMu.Lock()
	defer b.opMu.Unlock()
	b.mu.Lock()
	current := b.scanGen == generation
	b.mu.Unlock()
	if current {
		logStopErr(b.stopScanLocked(ctx))
	}
}

func logStopErr(err error) {
	if err != nil {
		log.Warn().Err(err).Msg("could not stop bluetooth discovery, trying again later")
	}
}

// scanEndedLocked records that Core's scan is over without asking the stack
// to stop it. The caller holds mu.
func (b *Bluetooth) scanEndedLocked() {
	if !b.scanning {
		return
	}
	b.scanning = false
	b.scanGen++
	if b.scanTimer != nil {
		b.scanTimer.Stop()
		b.scanTimer = nil
	}
}

// stopScanLocked ends Core's scan. The caller holds opMu. When the stack
// refuses to stop, Core keeps the scan as its own and tries again after
// bluetoothStopRetry, so discovery is never left running unowned. A stack or
// adapter that has gone away has no scan to stop.
func (b *Bluetooth) stopScanLocked(ctx context.Context) error {
	b.mu.Lock()
	wasScanning := b.scanning
	b.mu.Unlock()
	if !wasScanning {
		return nil
	}

	// Remember what is in range now: the scan's finds stay listed after it
	// ends, and devices heard since the last state read would be lost.
	if reading, err := b.ctl.Read(ctx); err == nil {
		b.mu.Lock()
		for i := range reading.Devices {
			if reading.Devices[i].InRange {
				b.discovered[reading.Devices[i].Address] = true
			}
		}
		b.mu.Unlock()
	}

	err := b.ctl.StopDiscovery(ctx)
	if errors.Is(err, hoststatus.ErrUnsupported) || errors.Is(err, hoststatus.ErrBluetoothNoAdapter) {
		err = nil
	}

	b.mu.Lock()
	b.scanGen++
	if b.scanTimer != nil {
		b.scanTimer.Stop()
		b.scanTimer = nil
	}
	if err != nil && !b.closed {
		generation := b.scanGen
		b.scanTimer = b.clock.AfterFunc(bluetoothStopRetry, func() { b.scanExpired(generation) })
	} else {
		b.scanning = false
	}
	b.mu.Unlock()
	b.changed()
	return err //nolint:wrapcheck // callers branch on hoststatus errors
}

// Pair starts pairing with a device and returns at once. The outcome arrives
// through device.bluetooth.changed. A running scan is stopped first: on many
// adapters an inquiry starves the connection pairing needs.
func (b *Bluetooth) Pair(ctx context.Context, address string) (models.DeviceBluetoothState, error) {
	b.opMu.Lock()
	b.mu.Lock()
	busy, closed := b.pairActive, b.closed
	b.mu.Unlock()
	if closed {
		b.opMu.Unlock()
		return models.DeviceBluetoothState{}, hoststatus.ErrUnsupported
	}
	if busy {
		b.opMu.Unlock()
		return models.DeviceBluetoothState{}, ErrBluetoothBusy
	}
	reading, err := b.ctl.Read(ctx)
	if err == nil && !reading.Powered {
		err = hoststatus.ErrBluetoothNotPowered
	}
	if err != nil {
		b.opMu.Unlock()
		return models.DeviceBluetoothState{}, err //nolint:wrapcheck // callers branch on hoststatus errors
	}
	// Pairing goes ahead even if the scan will not stop; the retry ends it.
	logStopErr(b.stopScanLocked(ctx))

	attempt, cancel := context.WithCancelCause(b.base)
	b.mu.Lock()
	b.pairActive = true
	b.attempts++
	number := b.attempts
	b.pairing = &models.DeviceBluetoothPairing{
		ID: address, State: models.BluetoothPairingPairing, Attempt: number,
	}
	timer := b.clock.AfterFunc(b.pairTimeout, func() { cancel(errPairTimeout) })
	b.wg.Add(1)
	b.mu.Unlock()
	b.opMu.Unlock()

	// Snapshot before the attempt starts so the reply always shows it in
	// progress; a fast outcome would otherwise race this read.
	state, stateErr := b.State(ctx)
	go func() {
		defer b.wg.Done()
		defer cancel(nil)
		defer timer.Stop()
		b.runPair(attempt, address, number)
	}()
	b.changed()
	if stateErr != nil {
		// The attempt is running, so the request succeeded. Answer with what
		// Core knows without the stack; the notification brings the rest.
		log.Debug().Err(stateErr).Msg("could not read bluetooth state after starting pairing")
		b.mu.Lock()
		state = models.DeviceBluetoothState{
			Devices:  []models.DeviceBluetoothDevice{},
			Powered:  reading.Powered,
			Scanning: b.scanning,
			Pairing:  b.pairingCopyLocked(),
		}
		b.mu.Unlock()
	}
	return state, nil
}

func (b *Bluetooth) runPair(ctx context.Context, address string, number int) {
	err := b.ctl.Pair(ctx, address)
	if err == nil {
		// Trusted lets the device reconnect by itself from now on. Connecting
		// is a courtesy: most controllers connect by themselves once paired,
		// and a pairing that went through is not undone by a slow connect.
		if trustErr := b.ctl.Trust(ctx, address); trustErr != nil {
			log.Warn().Err(trustErr).Str("device", address).Msg("could not trust bluetooth device")
		}
		if connectErr := b.ctl.Connect(ctx, address); connectErr != nil {
			log.Info().Err(connectErr).Str("device", address).Msg("paired bluetooth device did not connect")
		}
	}

	result := models.DeviceBluetoothPairing{ID: address, State: models.BluetoothPairingPaired, Attempt: number}
	if err != nil {
		code := pairErrorCode(ctx, err)
		result.State = models.BluetoothPairingFailed
		result.Error = &code
		log.Info().Err(err).Str("device", address).Str("error", code).Msg("bluetooth pairing failed")
	} else {
		log.Info().Str("device", address).Msg("bluetooth device paired")
	}

	b.mu.Lock()
	b.pairActive = false
	b.pairing = &result
	b.mu.Unlock()
	b.changed()
}

func pairErrorCode(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, hoststatus.ErrBluetoothPinRequired):
		return models.BluetoothErrorPinRequired
	case errors.Is(context.Cause(ctx), errPairTimeout), errors.Is(err, hoststatus.ErrBluetoothTimeout):
		return models.BluetoothErrorTimeout
	case errors.Is(err, hoststatus.ErrBluetoothNotPowered):
		return models.BluetoothErrorNotPowered
	case errors.Is(err, hoststatus.ErrBluetoothRejected):
		return models.BluetoothErrorRejected
	case errors.Is(err, hoststatus.ErrBluetoothNotFound):
		return models.BluetoothErrorNotFound
	default:
		return models.BluetoothErrorFailed
	}
}

// Forget unpairs a device and removes it from the stack.
func (b *Bluetooth) Forget(ctx context.Context, address string) (models.DeviceBluetoothState, error) {
	b.opMu.Lock()
	b.mu.Lock()
	pairingIt := b.pairActive && b.pairing != nil && b.pairing.ID == address
	b.mu.Unlock()
	if pairingIt {
		b.opMu.Unlock()
		return models.DeviceBluetoothState{}, ErrBluetoothBusy
	}
	err := b.ctl.Remove(ctx, address)
	b.opMu.Unlock()
	if err != nil {
		return models.DeviceBluetoothState{}, err //nolint:wrapcheck // callers branch on hoststatus errors
	}
	b.mu.Lock()
	delete(b.discovered, address)
	b.mu.Unlock()
	b.changed()
	return b.State(ctx)
}
