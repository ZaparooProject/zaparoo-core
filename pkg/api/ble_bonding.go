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

package api

import (
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/rs/zerolog/log"
)

// bondCloseTimeout bounds closing a bonding window, which takes a few
// bluetoothd calls.
const bondCloseTimeout = 10 * time.Second

// A phone has to bond with the adapter to reconnect later, and the adapter
// only lets it while Core's pairing PIN is shown. The bonding loop keeps a
// window open exactly then, on the peripheral being served, and holds it
// open past the PIN while a pair.finish is still being answered so the
// phone that just paired is kept.

// kickBonding wakes the bonding loop.
func (t *bleTransport) kickBonding() {
	select {
	case t.bondKick <- struct{}{}:
	default:
	}
}

// holdBonding keeps an open window open until the returned function runs.
func (t *bleTransport) holdBonding() (release func()) {
	t.mu.Lock()
	t.bondHolds++
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		t.bondHolds--
		t.mu.Unlock()
		t.kickBonding()
	}
}

// keepBond marks a peer that proved it is Core's client, so the bond it
// made during the window stays.
func (t *bleTransport) keepBond(peer ble.Peer) {
	t.mu.Lock()
	w := t.bondWindow
	t.mu.Unlock()
	if w != nil {
		w.Keep(peer)
	}
}

// bondingLoop opens and closes the bonding window until the transport stops.
func (t *bleTransport) bondingLoop(pinChanged <-chan struct{}) {
	var (
		window ble.BondWindow
		owner  ble.Peripheral
	)
	closeWindow := func() {
		t.mu.Lock()
		t.bondWindow = nil
		t.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), bondCloseTimeout)
		if err := window.Close(ctx); err != nil {
			log.Warn().Err(err).Msg("closing bluetooth bonding")
		}
		cancel()
		window, owner = nil, nil
	}
	defer func() {
		if window != nil {
			closeWindow()
		}
	}()

	for {
		pin, expiresAt := t.pairing.PendingPIN()
		t.mu.Lock()
		p := t.serving
		holds := t.bondHolds
		t.mu.Unlock()
		bonder, canBond := p.(ble.Bonder)
		want := pin != "" && canBond

		if window != nil && (owner != p || (!want && holds == 0)) {
			closeWindow()
		}
		if window == nil && want {
			w, err := bonder.AllowBonding(t.ctx)
			if err != nil {
				log.Warn().Err(err).Msg("bluetooth bonding unavailable; a phone paired now cannot reconnect later")
			} else {
				window, owner = w, p
				t.mu.Lock()
				t.bondWindow = w
				t.mu.Unlock()
			}
		}

		// The PIN's expiry is only noticed by the pairing cleanup pass,
		// so the loop also wakes itself for it.
		var expired <-chan time.Time
		var timer *time.Timer
		if pin != "" {
			timer = time.NewTimer(time.Until(expiresAt))
			expired = timer.C
		}
		select {
		case <-t.ctx.Done():
		case <-pinChanged:
		case <-t.bondKick:
		case <-expired:
		}
		if timer != nil {
			timer.Stop()
		}
		if t.ctx.Err() != nil {
			return
		}
	}
}
