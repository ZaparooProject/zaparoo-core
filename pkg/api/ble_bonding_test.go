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
	"errors"
	"slices"
	"testing"
	"time"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBonder is a fake peripheral that can open bonding windows. It logs
// what happens to them in order: "open", "keep:<path>", "close" and, for a
// close that fails, "close-failed".
type fakeBonder struct {
	*mocks.FakePeripheral
	err    error
	events []string
	opens  int
	// closeFails is how many closes still fail.
	closeFails int
	mu         syncutil.Mutex
}

type fakeBondWindow struct{ b *fakeBonder }

func (b *fakeBonder) AllowBonding(context.Context) (ble.BondWindow, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.opens++
	if b.err != nil {
		return nil, b.err
	}
	b.events = append(b.events, "open")
	return &fakeBondWindow{b: b}, nil
}

func (b *fakeBonder) log() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.events)
}

func (b *fakeBonder) openCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.opens
}

func (w *fakeBondWindow) Keep(peer ble.Peer) {
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	w.b.events = append(w.b.events, "keep:"+peer.Path)
}

func (w *fakeBondWindow) Close(context.Context) error {
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	if w.b.closeFails > 0 {
		w.b.closeFails--
		w.b.events = append(w.b.events, "close-failed")
		return errors.New("bluetoothd did not answer")
	}
	w.b.events = append(w.b.events, "close")
	return nil
}

func (b *fakeBonder) failCloses(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeFails = n
}

func (b *fakeBonder) waitFor(t *testing.T, want ...string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, want, b.log())
	}, bleTestTimeout, 5*time.Millisecond)
}

func TestBLEBonding_OpensWhilePairingAndClosesOnCancel(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	bonder := &fakeBonder{}
	newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr, bonder: bonder})

	// No PIN, no window.
	assert.Never(t, func() bool { return bonder.openCount() > 0 }, 50*time.Millisecond, 5*time.Millisecond)

	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	bonder.waitFor(t, "open")

	harness.mgr.CancelPairing()
	bonder.waitFor(t, "open", "close")
}

func TestBLEBonding_OpensForPINShownBeforeServing(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)

	bonder := &fakeBonder{}
	newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr, bonder: bonder})
	bonder.waitFor(t, "open")
}

func TestBLEBonding_ClosesWhenPINExpires(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t, WithPairingPINTTL(150*time.Millisecond))
	bonder := &fakeBonder{}
	newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr, bonder: bonder})

	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	bonder.waitFor(t, "open")
	// Nothing touches the pairing manager: the loop notices expiry itself.
	bonder.waitFor(t, "open", "close")
}

func TestBLEBonding_StopClosesOpenWindow(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	bonder := &fakeBonder{}
	rig := newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr, bonder: bonder})

	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	bonder.waitFor(t, "open")

	rig.transport.stop()
	assert.Equal(t, []string{"open", "close"}, bonder.log())
}

func TestBLEBonding_HoldKeepsWindowPastPIN(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	bonder := &fakeBonder{}
	rig := newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr, bonder: bonder})

	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	bonder.waitFor(t, "open")

	release := rig.transport.holdBonding()
	harness.mgr.CancelPairing()
	assert.Never(t, func() bool { return len(bonder.log()) > 1 }, 100*time.Millisecond, 5*time.Millisecond)

	peer := ble.Peer{Path: "/org/bluez/hci0/dev_11_22_33_44_55_66"}
	rig.transport.keepBond(peer)
	release()
	bonder.waitFor(t, "open", "keep:"+peer.Path, "close")
}

func TestBLEBonding_FailedOpenLeavesNoWindow(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	bonder := &fakeBonder{err: errors.New("bluetoothd said no")}
	rig := newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr, bonder: bonder})

	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return bonder.openCount() == 1 }, bleTestTimeout, 5*time.Millisecond)

	// keepBond without a window is a no-op.
	rig.transport.keepBond(ble.Peer{Path: "/org/bluez/hci0/dev_11_22_33_44_55_66"})
	harness.mgr.CancelPairing()
	rig.transport.stop()
	assert.Empty(t, bonder.log())
}

func TestBLEBonding_PeripheralWithoutBonderNeverOpens(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	rig := newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr})

	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	assert.Never(t, func() bool {
		rig.transport.mu.Lock()
		defer rig.transport.mu.Unlock()
		return rig.transport.bondWindow != nil
	}, 50*time.Millisecond, 5*time.Millisecond)
}

// A phone that completes pair.finish over BLE keeps the bond it made, and
// the window only closes once the reply has gone out.
func TestBLEBonding_PairFinishKeepsPeerBond(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	pin, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)

	gateway := apimiddleware.NewEncryptionGateway(helpers.NewMockUserDBI())
	bonder := &fakeBonder{}
	rig := newBLETestRig(t, bleTestRigOptions{
		methodMap: versionMethodMap(t), encGateway: gateway, pairing: harness.mgr, bonder: bonder,
	})
	bonder.waitFor(t, "open")

	client := rig.client("11:22:33:44:55:66", 0x4242, 185)
	phone := newBLEPairingClient(t, pin, "Phone")
	client.send(t, phone.startRequest(t, 1))
	startResp, _ := unmarshalResult[pairStartResponse](t, client.recv(t))
	finishReq, _, _ := phone.finishRequest(t, 2, startResp, false)
	client.send(t, finishReq)
	finishResp, _ := unmarshalResult[pairFinishResponse](t, client.recv(t))
	require.NotEmpty(t, finishResp.AuthToken)

	bonder.waitFor(t, "open", "keep:"+client.peer.Path, "close")
}

// A peer that only fails pairing is never kept.
func TestBLEBonding_FailedPairFinishDoesNotKeep(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	pin, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	wrong := []byte(pin)
	wrong[0] = '0' + (wrong[0]-'0'+1)%10

	gateway := apimiddleware.NewEncryptionGateway(helpers.NewMockUserDBI())
	bonder := &fakeBonder{}
	rig := newBLETestRig(t, bleTestRigOptions{
		methodMap: versionMethodMap(t), encGateway: gateway, pairing: harness.mgr, bonder: bonder,
	})
	bonder.waitFor(t, "open")

	client := rig.client("11:22:33:44:55:66", 0x4242, 185)
	phone := newBLEPairingClient(t, string(wrong), "Phone")
	client.send(t, phone.startRequest(t, 1))
	startResp, _ := unmarshalResult[pairStartResponse](t, client.recv(t))
	finishReq, _, _ := phone.finishRequest(t, 2, startResp, false)
	client.send(t, finishReq)
	_ = client.recv(t)

	harness.mgr.CancelPairing()
	bonder.waitFor(t, "open", "close")
}

// A window whose close failed left cleanup undone, so it is closed again
// the next time the loop wakes, and once it has closed it is left alone.
func TestBLEBonding_FailedCloseIsRetried(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	bonder := &fakeBonder{}
	rig := newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr, bonder: bonder})

	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	bonder.waitFor(t, "open")

	bonder.failCloses(2)
	harness.mgr.CancelPairing()
	bonder.waitFor(t, "open", "close-failed")

	rig.transport.kickBonding()
	bonder.waitFor(t, "open", "close-failed", "close-failed")

	// A peer that proves itself meanwhile is kept by the window still owed.
	peer := ble.Peer{Path: "/org/bluez/hci0/dev_11_22_33_44_55_66"}
	rig.transport.keepBond(peer)
	rig.transport.kickBonding()
	bonder.waitFor(t, "open", "close-failed", "close-failed", "keep:"+peer.Path, "close")

	rig.transport.stop()
	assert.Equal(t, []string{"open", "close-failed", "close-failed", "keep:" + peer.Path, "close"}, bonder.log())
}

// The cleanup a failed close left is finished before the next window opens.
func TestBLEBonding_FailedCloseIsRetriedBeforeNextWindow(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	bonder := &fakeBonder{}
	newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr, bonder: bonder})

	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	bonder.waitFor(t, "open")

	bonder.failCloses(1)
	harness.mgr.CancelPairing()
	bonder.waitFor(t, "open", "close-failed")

	_, _, err = harness.mgr.StartPairing("member")
	require.NoError(t, err)
	bonder.waitFor(t, "open", "close-failed", "close", "open")
}

// Stopping gives a failed cleanup one more try.
func TestBLEBonding_StopRetriesFailedClose(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	bonder := &fakeBonder{}
	rig := newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr, bonder: bonder})

	_, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	bonder.waitFor(t, "open")

	bonder.failCloses(1)
	harness.mgr.CancelPairing()
	bonder.waitFor(t, "open", "close-failed")

	rig.transport.stop()
	assert.Equal(t, []string{"open", "close-failed", "close"}, bonder.log())
}
