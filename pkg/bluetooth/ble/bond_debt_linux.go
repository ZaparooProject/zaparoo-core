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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
)

// Closing a bonding window can fail part way: bluetoothd answers too slowly,
// or the adapter is lost while the window is open and its bus connection is
// closed under it. What the window still had to undo is then owed: switching
// Pairable back off, and removing the bonds it accepted that never completed
// Core's pairing. A lost adapter cannot be reached through the window again,
// so what is owed is kept apart from the window and settled the next time
// Core serves on that adapter or opens a window on it.

// bondSettleTimeout bounds one attempt at settling what is owed, which takes
// a bluetoothd call per item.
const bondSettleTimeout = 10 * time.Second

// errAdapterGone is why nothing can be settled through a lost adapter.
var errAdapterGone = errors.New("adapter is gone")

// bondDebtKey names the adapter a debt belongs to. The address is part of it
// because another controller can take over a lost one's path.
type bondDebtKey struct {
	path    dbus.ObjectPath
	address string
}

func (a *adapter) bondDebtKey() bondDebtKey {
	return bondDebtKey{path: a.path, address: a.address}
}

// bondDebt is what closed windows still owe one adapter.
type bondDebt struct {
	// devices are the bonds a window accepted that never completed Core's
	// pairing and are still to be removed.
	devices map[dbus.ObjectPath]bool
	// pairableOff is set when a window switched Pairable on and could not
	// switch it back off.
	pairableOff bool
}

// bondDebts holds what is owed to every adapter. It is kept in memory only,
// so a debt does not outlive Core: after a restart a bond left behind by a
// failed close stays on the adapter until it is removed by hand, and an
// adapter left pairable stays so until something switches it off.
type bondDebts struct {
	owed map[bondDebtKey]*bondDebt
	mu   syncutil.Mutex
	// settling is held across the bluetoothd calls of one settle and while
	// a window decides whether Pairable is its to restore, so a settle
	// cannot switch Pairable off under a window that is opening.
	settling syncutil.Mutex
}

func newBondDebts() *bondDebts {
	return &bondDebts{owed: make(map[bondDebtKey]*bondDebt)}
}

// processBondDebts is what every peripheral of this process owes through,
// so a debt outlives the adapter connection it was made on.
//
//nolint:gochecknoglobals // a debt has to outlive every adapter and peripheral object
var processBondDebts = newBondDebts()

// add records what a window could not undo.
func (d *bondDebts) add(key bondDebtKey, pairableOff bool, devices []dbus.ObjectPath) {
	if !pairableOff && len(devices) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	debt := d.owed[key]
	if debt == nil {
		debt = &bondDebt{devices: make(map[dbus.ObjectPath]bool)}
		d.owed[key] = debt
	}
	debt.pairableOff = debt.pairableOff || pairableOff
	for _, device := range devices {
		debt.devices[device] = true
	}
}

// snapshot copies what is owed to an adapter.
func (d *bondDebts) snapshot(key bondDebtKey) (pairableOff bool, devices []dbus.ObjectPath) {
	d.mu.Lock()
	defer d.mu.Unlock()
	debt := d.owed[key]
	if debt == nil {
		return false, nil
	}
	for device := range debt.devices {
		devices = append(devices, device)
	}
	slices.Sort(devices)
	return debt.pairableOff, devices
}

// owes reports whether a device's bond is still to be removed.
func (d *bondDebts) owes(key bondDebtKey, device dbus.ObjectPath) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	debt := d.owed[key]
	return debt != nil && debt.devices[device]
}

// forget drops a device from what is owed: its bond is gone, or the device
// proved itself and keeps it.
func (d *bondDebts) forget(key bondDebtKey, device dbus.ObjectPath) {
	d.mu.Lock()
	defer d.mu.Unlock()
	debt := d.owed[key]
	if debt == nil {
		return
	}
	delete(debt.devices, device)
	d.dropIfEmpty(key, debt)
}

// takePairable hands the Pairable restore over to the caller and reports
// whether one was owed.
func (d *bondDebts) takePairable(key bondDebtKey) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	debt := d.owed[key]
	if debt == nil || !debt.pairableOff {
		return false
	}
	debt.pairableOff = false
	d.dropIfEmpty(key, debt)
	return true
}

// dropIfEmpty must be called with mu held.
func (d *bondDebts) dropIfEmpty(key bondDebtKey, debt *bondDebt) {
	if !debt.pairableOff && len(debt.devices) == 0 {
		delete(d.owed, key)
	}
}

// describeBondDebt says what is owed in words fit for a log line. It names
// addresses only.
func describeBondDebt(pairableOff bool, devices []dbus.ObjectPath) string {
	var parts []string
	if pairableOff {
		parts = append(parts, "adapter left pairable")
	}
	if len(devices) > 0 {
		addrs := make([]string, 0, len(devices))
		for _, device := range devices {
			addrs = append(addrs, addressFromPath(device))
		}
		parts = append(parts, "unproven bonds left for "+strings.Join(addrs, ", "))
	}
	return strings.Join(parts, "; ")
}

// isPairedDevice reports whether bluetoothd still holds a bond for a device.
// A device it no longer lists, or lists as not paired, has none.
func isPairedDevice(objects managedObjects, device dbus.ObjectPath) bool {
	props, ok := objects[device][deviceIface]
	if !ok {
		return false
	}
	v, ok := props["Paired"]
	if !ok {
		return true
	}
	paired, ok := v.Value().(bool)
	return !ok || paired
}

// settle undoes what closed windows still owe a's adapter, through a. What
// succeeds is not owed any more; what fails stays owed and is in the error.
func (d *bondDebts) settle(ctx context.Context, a *adapter) error {
	key := a.bondDebtKey()
	if pairableOff, devices := d.snapshot(key); !pairableOff && len(devices) == 0 {
		return nil
	}
	select {
	case <-a.gone:
		return errAdapterGone
	default:
	}

	d.settling.Lock()
	defer d.settling.Unlock()
	pairableOff, devices := d.snapshot(key)
	if !pairableOff && len(devices) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, bondSettleTimeout)
	defer cancel()

	var errs []error
	// Pairable is only owed when a window of Core's found it off and
	// switched it on, so off is what it goes back to.
	if pairableOff {
		if err := a.setProperty(ctx, a.obj, adapterIface, "Pairable", false); err != nil {
			errs = append(errs, err)
		} else {
			d.takePairable(key)
		}
	}
	if len(devices) > 0 {
		errs = append(errs, d.removeOwed(ctx, a, key, devices)...)
	}
	if len(errs) > 0 {
		left, leftDevices := d.snapshot(key)
		return fmt.Errorf("bonding cleanup incomplete (%s): %w",
			describeBondDebt(left, leftDevices), errors.Join(errs...))
	}
	log.Info().Msg("bluetooth bonding cleanup that failed earlier is done")
	return nil
}

// removeOwed removes the owed bonds that are still unproven. Time has passed
// since their window closed, so each is checked again first: a device that
// proved itself since keeps its bond, and one bluetoothd no longer holds a
// bond for has nothing to remove.
func (d *bondDebts) removeOwed(
	ctx context.Context, a *adapter, key bondDebtKey, devices []dbus.ObjectPath,
) []error {
	objects, err := a.managedObjects(ctx)
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, device := range devices {
		if !d.owes(key, device) {
			continue
		}
		if !isPairedDevice(objects, device) {
			d.forget(key, device)
			continue
		}
		if err := a.removeDevice(ctx, device); err != nil {
			errs = append(errs, err)
			continue
		}
		d.forget(key, device)
		log.Info().Str("peer", addressFromPath(device)).
			Msg("removed a bluetooth bond that never completed Core's pairing")
	}
	return errs
}

// removeDevice removes a device and its bond from the adapter. A device
// bluetoothd already forgot is not an error.
func (a *adapter) removeDevice(ctx context.Context, device dbus.ObjectPath) error {
	err := a.call(ctx, a.obj, adapterIface+".RemoveDevice", device)
	var dbusErr dbus.Error
	if errors.As(err, &dbusErr) && dbusErr.Name == bluezErrNotExist {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove unproven bond: %w", err)
	}
	return nil
}

// settleBondDebts settles what earlier windows left owed on this adapter. A
// failure is not an error for the caller: the debt stays and is tried again.
func (p *peripheral) settleBondDebts(ctx context.Context) {
	if err := p.debts.settle(ctx, p.a); err != nil {
		log.Debug().Err(err).Msg("bluetooth bonding cleanup still owed")
	}
}
