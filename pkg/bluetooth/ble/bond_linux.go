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
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog/log"
)

// A phone bonds with Core when bluetoothd, as a GATT client of the phone,
// reads something that needs encryption. The pairing itself is Just Works:
// Core has no display to show a code on and the phone's code would prove
// nothing, since Core's own PIN is what authenticates the app. bluetoothd
// asks the default agent to authorize such a pairing, so while Core's pairing
// is open an agent accepts it. Outside that window the adapter is left as it
// was: not pairable, unless something else made it so.
//
// The agent cannot ask the device to prove anything first. bluetoothd often
// starts the pairing itself within a second of the connection, and iOS holds
// the app's requests until the pairing ends, so the app cannot use Core's
// service before it is asked about. Any device in range may bond while the
// window is open. A bonded device can use any profile bluetoothd offers, a
// HID keyboard included, and HOG never asks the agent. So a bond Core
// accepted is removed when the window closes unless the device went on to
// prove it is a Core client with Core's PIN or an existing credential.
//
// bluetoothd can leave a request from the phone unanswered when it arrives
// while bluetoothd is raising the link's security: an iOS phone's own
// discovery request did so, and the phone waited out its 30 second ATT
// timeout before the link, and the app on it, could do anything. So a device
// that bonds and then does not use Core's service soon after is disconnected,
// and reconnects already bonded.

const (
	agentManagerIface = "org.bluez.AgentManager1"
	agentIface        = "org.bluez.Agent1"
	agentCapability   = "NoInputNoOutput"
	bluezErrRejected  = "org.bluez.Error.Rejected"
	bluezErrNotExist  = "org.bluez.Error.DoesNotExist"
)

var bondAgentCounter atomic.Uint64

// bondStallTimeout is how long after its bond is accepted a device has to
// use Core's service before its link is taken as stalled. Encryption starts
// within a second of the bond being accepted and the app reaches Core's
// service within a second of that. An app holding the link while its user
// types the PIN keeps reading Core's info.
const bondStallTimeout = 3 * time.Second

// bondDisconnectTimeout bounds dropping a stalled link.
const bondDisconnectTimeout = 5 * time.Second

// bondAgent answers bluetoothd while the window is open. Only D-Bus methods
// may be exported on this type.
type bondAgent struct {
	w *bondWindow
}

func rejected() *dbus.Error { return dbus.NewError(bluezErrRejected, nil) }

func (*bondAgent) Release() *dbus.Error { return nil }

func (*bondAgent) Cancel() *dbus.Error { return nil }

func (*bondAgent) RequestPinCode(dbus.ObjectPath) (string, *dbus.Error) { return "", rejected() }

func (*bondAgent) DisplayPinCode(dbus.ObjectPath, string) *dbus.Error { return rejected() }

func (*bondAgent) RequestPasskey(dbus.ObjectPath) (uint32, *dbus.Error) { return 0, rejected() }

func (*bondAgent) DisplayPasskey(dbus.ObjectPath, uint32, uint16) *dbus.Error { return nil }

func (*bondAgent) RequestConfirmation(dbus.ObjectPath, uint32) *dbus.Error { return rejected() }

func (*bondAgent) AuthorizeService(dbus.ObjectPath, string) *dbus.Error { return rejected() }

// RequestAuthorization is how bluetoothd asks about an incoming Just Works
// pairing.
func (g *bondAgent) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	if g.w.authorize(device) {
		return nil
	}
	return rejected()
}

// bondWindow is the Linux BondWindow.
type bondWindow struct {
	p        *peripheral
	closed   chan struct{}
	accepted map[dbus.ObjectPath]bool
	kept     map[dbus.ObjectPath]bool
	// events is the management socket new bonds are read from, nil when
	// it could not be opened.
	events mgmtConn
	path   dbus.ObjectPath
	mu     syncutil.Mutex
	once   sync.Once
	// restorePairable is set when the window switched Pairable on.
	restorePairable bool
}

// AllowBonding implements Bonder.
func (p *peripheral) AllowBonding(ctx context.Context) (BondWindow, error) {
	a := p.a
	w := &bondWindow{
		p:        p,
		closed:   make(chan struct{}),
		accepted: make(map[dbus.ObjectPath]bool),
		kept:     make(map[dbus.ObjectPath]bool),
		path: dbus.ObjectPath("/org/zaparoo/ble/agent" +
			strconv.FormatUint(bondAgentCounter.Add(1), 10)),
	}

	if err := a.export(&bondAgent{w: w}, w.path, agentIface); err != nil {
		return nil, fmt.Errorf("export bonding agent: %w", err)
	}
	manager := a.conn.Object(bluezService, bluezPathPrefix)
	if err := a.call(ctx, manager, agentManagerIface+".RegisterAgent", w.path, agentCapability); err != nil {
		_ = a.export(nil, w.path, agentIface)
		return nil, fmt.Errorf("register bonding agent: %w", err)
	}
	if err := a.call(ctx, manager, agentManagerIface+".RequestDefaultAgent", w.path); err != nil {
		w.unregister(ctx)
		return nil, fmt.Errorf("make bonding agent the default: %w", err)
	}

	v, err := a.getProperty(ctx, a.obj, adapterIface, "Pairable")
	if err != nil {
		w.unregister(ctx)
		return nil, err
	}
	if pairable, ok := v.Value().(bool); !ok || !pairable {
		if err := a.setProperty(ctx, a.obj, adapterIface, "Pairable", true); err != nil {
			w.unregister(ctx)
			return nil, err
		}
		w.restorePairable = true
	}
	w.watchBonds()
	log.Info().Msg("bluetooth bonding open for Core's pairing")
	return w, nil
}

// watchBonds starts reading the bonds bluetoothd makes without the agent.
// Without the management socket such a bond is not checked for a stall,
// which leaves a re-paired phone waiting out its own timeout.
func (w *bondWindow) watchBonds() {
	index, err := controllerIndex(string(w.p.a.path))
	if err != nil {
		log.Info().Err(err).Msg("bluetooth bonds bluetoothd renews go unwatched")
		return
	}
	conn, err := w.p.openBondEvents()
	if err != nil {
		log.Info().Err(err).Msg("bluetooth bonds bluetoothd renews go unwatched")
		return
	}
	w.mu.Lock()
	w.events = conn
	w.mu.Unlock()
	go w.watchRenewedBonds(conn, index)
}

// authorize accepts a pairing while the window is open and remembers the
// device, so its bond goes when the window closes unless it is kept.
func (w *bondWindow) authorize(device dbus.ObjectPath) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.closed:
		log.Debug().Str("device", string(device)).Msg("bluetooth bond refused: window closed")
		return false
	default:
	}
	w.accepted[device] = true
	log.Info().Str("peer", addressFromPath(device)).Msg("bluetooth bond accepted for Core's pairing")
	go w.p.dropIfStalled(device, w.p.awaitUse(device), w.p.bondStallTimeout)
	return true
}

// dropIfStalled disconnects a device that just bonded unless it uses Core's
// service within timeout.
func (p *peripheral) dropIfStalled(device dbus.ObjectPath, used <-chan struct{}, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-used:
		return
	case <-p.a.gone:
		p.stopAwaitingUse(device, used)
		return
	case <-timer.C:
		p.stopAwaitingUse(device, used)
	}
	ctx, cancel := context.WithTimeout(context.Background(), bondDisconnectTimeout)
	defer cancel()
	if err := p.Disconnect(ctx, Peer{Path: string(device)}); err != nil {
		log.Debug().Err(err).Str("peer", addressFromPath(device)).
			Msg("disconnect a bluetooth peer that stalled after bonding")
		return
	}
	log.Info().Str("peer", addressFromPath(device)).
		Msg("disconnected a bluetooth peer that bonded but did not reach Core's service; it reconnects bonded")
}

// Keep implements BondWindow.
func (w *bondWindow) Keep(peer Peer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.kept[dbus.ObjectPath(peer.Path)] = true
}

// unregister removes the agent from bluetoothd and the bus.
func (w *bondWindow) unregister(ctx context.Context) {
	a := w.p.a
	manager := a.conn.Object(bluezService, bluezPathPrefix)
	if err := a.call(ctx, manager, agentManagerIface+".UnregisterAgent", w.path); err != nil {
		log.Debug().Err(err).Msg("unregister bonding agent")
	}
	_ = a.export(nil, w.path, agentIface)
}

// Close implements BondWindow.
func (w *bondWindow) Close(ctx context.Context) error {
	var errs []error
	w.once.Do(func() {
		close(w.closed)
		a := w.p.a
		w.mu.Lock()
		events := w.events
		w.events = nil
		w.mu.Unlock()
		if events != nil {
			_ = events.Close()
		}
		w.unregister(ctx)
		if w.restorePairable {
			if err := a.setProperty(ctx, a.obj, adapterIface, "Pairable", false); err != nil {
				errs = append(errs, err)
			}
		}

		w.mu.Lock()
		var drop []dbus.ObjectPath
		for path := range w.accepted {
			if !w.kept[path] {
				drop = append(drop, path)
			}
		}
		w.mu.Unlock()

		// Every accepted device that did not prove itself goes, paired or
		// not yet: one still pairing would otherwise finish with a bond
		// nobody checks.
		for _, path := range drop {
			err := a.call(ctx, a.obj, adapterIface+".RemoveDevice", path)
			var dbusErr dbus.Error
			if errors.As(err, &dbusErr) && dbusErr.Name == bluezErrNotExist {
				err = nil
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("remove unproven bond: %w", err))
				continue
			}
			log.Info().Str("peer", addressFromPath(path)).
				Msg("removed a bluetooth bond that never completed Core's pairing")
		}
		log.Info().Msg("bluetooth bonding closed")
	})
	return errors.Join(errs...)
}
