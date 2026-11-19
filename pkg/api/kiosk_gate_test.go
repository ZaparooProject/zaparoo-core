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
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kioskDeniedMethods is every registered method a capped caller is refused.
// Together with kioskAllowedMethods it has to cover the whole registry, so
// adding a method to the API means deciding here which side it falls on.
//
//nolint:gochecknoglobals // test table
var kioskDeniedMethods = map[string]bool{
	models.MethodMediaGenerate: true, models.MethodMediaGenerateCancel: true,
	models.MethodMediaGenerateResume: true, models.MethodMediaIndex: true,
	models.MethodMediaTagsUpdate: true, models.MethodMediaMetaUpdate: true,
	models.MethodMediaScrape: true, models.MethodMediaScrapeCancel: true,
	models.MethodMediaScrapeResume: true, models.MethodMediaActiveUpdate: true,
	models.MethodMediaCleanOrphans: true,
	// Uploads only feed media.meta.update.
	models.MethodUploadsBegin: true, models.MethodUploadsChunk: true,
	models.MethodUploadsStatus: true, models.MethodUploadsFinish: true,
	models.MethodUploadsCancel: true,
	// Host file browsing only feeds media.meta.update.
	models.MethodFilesBrowse: true, models.MethodFilesImage: true,
	models.MethodSettingsUpdate: true, models.MethodSettingsZapScriptHold: true,
	models.MethodSettingsReload: true, models.MethodSettingsLogsDownload: true,
	models.MethodSettingsBackup: true, models.MethodSettingsBackupList: true,
	models.MethodSettingsBackupInspect: true, models.MethodSettingsBackupDelete: true,
	models.MethodSettingsBackupRestore: true, models.MethodSettingsBackupRemoteRun: true,
	models.MethodSettingsBackupRemoteList: true, models.MethodSettingsBackupRemoteRestore: true,
	models.MethodPlaytimeLimitsUpdate: true, models.MethodPlaytimeExtend: true,
	models.MethodClients: true, models.MethodClientsDelete: true,
	models.MethodProfilesNew: true, models.MethodProfilesUpdate: true,
	models.MethodProfilesDelete: true, models.MethodProfilesVerify: true,
	models.MethodProfilesContent: true, models.MethodProfilesContentUpdate: true,
	models.MethodDecksNew: true, models.MethodDecksUpdate: true, models.MethodDecksDelete: true,
	models.MethodLaunchersRefresh: true,
	models.MethodMappings:         true, models.MethodMappingsNew: true, models.MethodMappingsDelete: true,
	models.MethodMappingsUpdate: true, models.MethodMappingsReload: true,
	models.MethodReadersWrite: true, models.MethodReadersWriteCancel: true,
	models.MethodInputKeyboard: true, models.MethodInputGamepad: true, models.MethodScreenshot: true,
	models.MethodInboxDelete: true, models.MethodInboxClear: true,
	models.MethodSettingsAuthClaim: true, models.MethodSettingsAuthUnlink: true,
	models.MethodSettingsAuthLink: true, models.MethodSettingsAuthLinkStatus: true,
	models.MethodSettingsAuthLinkCancel: true,
	models.MethodUpdateCheck:            true, models.MethodUpdateApply: true,
	models.MethodRemoteActivity:    true,
	models.MethodDevicePowerReboot: true, models.MethodDevicePowerShutdown: true,
	models.MethodDevicePowerSuspend:     true,
	models.MethodDeviceBluetoothDevices: true, models.MethodDeviceBluetoothScanStart: true,
	models.MethodDeviceBluetoothScanStop: true, models.MethodDeviceBluetoothPair: true,
	models.MethodDeviceBluetoothForget: true,
	models.MethodKioskEnable:           true, models.MethodKioskDisable: true,
	models.MethodKioskUpdate: true, models.MethodKioskAllow: true,
	// Registered after construction, by the server itself.
	models.MethodClientsPairStart: true, models.MethodClientsPairCancel: true,
}

// newKioskState returns device state with a kiosk lock over a real user
// database holding one admin profile.
func newKioskState(t *testing.T) (*state.State, *kiosk.Service) {
	t.Helper()
	userDB, cleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, userDB.CreateProfile(&database.Profile{
		ProfileID: "admin-1", Name: "Parent", Role: "admin", SwitchID: "a-b-c", PINHash: "x",
	}))
	st, ns := state.NewState(nil, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	lock := kiosk.NewService(&database.Database{UserDB: userDB}, clockwork.NewFakeClock())
	st.SetKiosk(lock)
	return st, lock
}

func lockDevice(t *testing.T, lock *kiosk.Service) {
	t.Helper()
	settings, ok := kiosk.PresetSettings(kiosk.PresetFamily)
	require.True(t, ok)
	require.NoError(t, lock.Enable(&settings, kiosk.EnableOptions{ConfirmEmpty: true}))
}

// stubMethodMap registers every real method name with a handler that only
// reports it was reached, keeping each method's real legacy flag, so the
// gate in front of the handlers can be tested by itself.
func stubMethodMap(t *testing.T) *MethodMap {
	t.Helper()
	registered := NewMethodMap()
	stub := &MethodMap{}
	names := registered.ListMethods()
	names = append(names, models.MethodClientsPairStart, models.MethodClientsPairCancel, models.MethodDeviceEndpoints)
	for _, name := range names {
		legacy := false
		if definition, ok := registered.getDefinition(name); ok {
			legacy = definition.legacyAllowed
		}
		require.NoError(t, stub.AddMethod(name, func(requests.RequestEnv) (any, error) {
			return "reached", nil
		}, legacy))
	}
	return stub
}

//nolint:gocritic // test helper takes the environment by value, as handleRequest does
func callMethod(
	methodMap *MethodMap, env requests.RequestEnv, method string,
) (any, *models.ErrorObject) {
	return handleRequest(methodMap, env, models.RequestObject{JSONRPC: "2.0", Method: method})
}

func TestKioskEveryMethodIsClassified(t *testing.T) {
	t.Parallel()
	for _, method := range stubMethodMap(t).ListMethods() {
		allowed := kioskAllowedMethods[method]
		denied := kioskDeniedMethods[method]
		assert.NotEqual(t, allowed, denied,
			"%s must be in exactly one of kioskAllowedMethods and the test's kioskDeniedMethods", method)
	}
	for method := range kioskAllowedMethods {
		_, ok := stubMethodMap(t).getDefinition(method)
		assert.True(t, ok, "kioskAllowedMethods lists unregistered method %s", method)
	}
}

func TestKioskGateCapsEveryCallerButAPairedAdmin(t *testing.T) {
	t.Parallel()
	st, lock := newKioskState(t)
	lockDevice(t, lock)
	methodMap := stubMethodMap(t)

	capped := map[string]requests.RequestEnv{
		"localhost":     {State: st, IsLocal: true},
		"API key":       {State: st, APIKeyAuthenticated: true, PlatformID: platformids.Linux},
		"paired member": {State: st, ClientRole: "member", PlatformID: platformids.Linux},
		"online remote": {State: st, ClientRole: "remote", PlatformID: platformids.Linux},
	}
	for name, env := range capped {
		for method := range kioskDeniedMethods {
			_, rpcErr := callMethod(methodMap, env, method)
			require.NotNil(t, rpcErr, "%s calling %s", name, method)
			assert.Equal(t, methods.ErrKioskLocked.Error(), rpcErr.Message, "%s calling %s", name, method)
		}
		for method := range kioskAllowedMethods {
			result, rpcErr := callMethod(methodMap, env, method)
			require.Nil(t, rpcErr, "%s calling %s", name, method)
			assert.Equal(t, "reached", result)
		}
	}

	admin := requests.RequestEnv{State: st, ClientRole: "admin", PlatformID: platformids.Linux}
	for method := range kioskDeniedMethods {
		result, rpcErr := callMethod(methodMap, admin, method)
		require.Nil(t, rpcErr, "paired admin calling %s", method)
		assert.Equal(t, "reached", result)
	}
}

func TestKioskGateLiftsWithTheLock(t *testing.T) {
	t.Parallel()
	st, lock := newKioskState(t)
	methodMap := stubMethodMap(t)
	local := requests.RequestEnv{State: st, IsLocal: true}

	_, rpcErr := callMethod(methodMap, local, models.MethodSettingsUpdate)
	require.Nil(t, rpcErr, "kiosk off")

	lockDevice(t, lock)
	_, rpcErr = callMethod(methodMap, local, models.MethodSettingsUpdate)
	require.NotNil(t, rpcErr, "locked")

	require.NoError(t, lock.Unlock(time.Minute))
	_, rpcErr = callMethod(methodMap, local, models.MethodSettingsUpdate)
	require.Nil(t, rpcErr, "temporarily unlocked")

	require.NoError(t, lock.Lock())
	_, rpcErr = callMethod(methodMap, local, models.MethodSettingsUpdate)
	require.NotNil(t, rpcErr, "locked again")

	require.NoError(t, lock.Disable())
	_, rpcErr = callMethod(methodMap, local, models.MethodSettingsUpdate)
	require.Nil(t, rpcErr, "kiosk off again")
}

func TestKioskWithdrawsLegacyAccess(t *testing.T) {
	t.Parallel()
	st, lock := newKioskState(t)
	methodMap := stubMethodMap(t)
	// An unpaired client on a platform with the compatibility grant.
	legacy := requests.RequestEnv{State: st, PlatformID: platformids.Mister}

	for _, method := range []string{models.MethodRun, models.MethodSettingsUpdate, models.MethodProfiles} {
		_, rpcErr := callMethod(methodMap, legacy, method)
		require.Nil(t, rpcErr, "kiosk off leaves legacy %s as it was", method)
	}

	lockDevice(t, lock)
	for _, method := range []string{
		models.MethodRun, models.MethodSettingsUpdate, models.MethodProfiles,
		models.MethodKiosk, models.MethodKioskUnlock, models.MethodVersion,
	} {
		_, rpcErr := callMethod(methodMap, legacy, method)
		require.NotNil(t, rpcErr, "locked: legacy %s", method)
	}

	// A temporary unlock is for the person at the device. It must not also
	// reopen the device to whoever is on the network.
	require.NoError(t, lock.Unlock(time.Minute))
	_, rpcErr := callMethod(methodMap, legacy, models.MethodRun)
	require.NotNil(t, rpcErr, "temporarily unlocked: legacy stays refused")

	require.NoError(t, lock.Disable())
	_, rpcErr = callMethod(methodMap, legacy, models.MethodRun)
	require.Nil(t, rpcErr, "kiosk off restores legacy access")
}

func TestPairingRequiredWhileKioskIsOn(t *testing.T) {
	t.Parallel()
	cfg, err := helpers.NewTestConfigWithPort(helpers.NewMemoryFS(), t.TempDir(), 0)
	require.NoError(t, err)
	st, lock := newKioskState(t)

	require.False(t, cfg.EncryptionEnabled())
	assert.False(t, pairingRequired(cfg, st))
	assert.False(t, pairingRequired(cfg, nil), "no state means no kiosk")

	lockDevice(t, lock)
	assert.True(t, pairingRequired(cfg, st))
	require.NoError(t, lock.Unlock(time.Minute))
	assert.True(t, pairingRequired(cfg, st), "a temporary unlock keeps pairing required")
	assert.False(t, cfg.EncryptionEnabled(), "the stored setting is never touched")

	require.NoError(t, lock.Disable())
	assert.False(t, pairingRequired(cfg, st))
}

func TestLegacyAdmissionMiddlewareRefusesUnpairedWhileKioskIsOn(t *testing.T) {
	t.Parallel()
	st, lock := newKioskState(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	serve := func(remoteAddr string) int {
		handler := legacyAdmissionMiddleware(st, platformids.Mister, nil)(next)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", http.NoBody)
		req.RemoteAddr = remoteAddr
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}

	assert.Equal(t, http.StatusNoContent, serve("192.168.1.50:1234"), "kiosk off")

	lockDevice(t, lock)
	assert.Equal(t, http.StatusUnauthorized, serve("192.168.1.50:1234"), "locked")
	assert.Equal(t, http.StatusNoContent, serve("127.0.0.1:1234"), "the device itself is still admitted")

	require.NoError(t, lock.Unlock(time.Minute))
	assert.Equal(t, http.StatusUnauthorized, serve("192.168.1.50:1234"), "temporarily unlocked")
}

func TestSSEEndsForUnpairedClientsWhenKioskTurnsOn(t *testing.T) {
	t.Parallel()
	st, lock := newKioskState(t)
	notifications := make(chan models.Notification, 8)
	notifBroker := newTestBroker(st.GetContext(), notifications)
	t.Cleanup(st.StopService)

	handler := handleSSE(notifBroker, st, platformids.Mister)
	open := func(remoteAddr string) *http.Response {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.RemoteAddr = remoteAddr
			handler(w, r)
		}))
		t.Cleanup(server.Close)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req) //nolint:gosec // test hitting local test server
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}
	unpaired := open("192.168.1.50:1234") //nolint:bodyclose // closed by the helper's cleanup
	local := open("127.0.0.1:1234")       //nolint:bodyclose // closed by the helper's cleanup

	// Both streams are subscribed once their headers have arrived.
	lockDevice(t, lock)
	status := lock.Status()
	payload, err := json.Marshal(methods.KioskStatusResponse(nil, &status))
	require.NoError(t, err)
	notifications <- models.Notification{Method: models.NotificationKioskState, Params: payload}

	// The unpaired stream is ended without being sent anything.
	body := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(unpaired.Body)
		body <- data
	}()
	select {
	case data := <-body:
		assert.Empty(t, data)
	case <-time.After(5 * time.Second):
		t.Fatal("the unpaired stream stayed open after kiosk mode was turned on")
	}

	// The device's own stream stays open and gets the notification.
	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(local.Body)
		for scanner.Scan() {
			if text := scanner.Text(); text != "" {
				line <- text
				return
			}
		}
	}()
	select {
	case text := <-line:
		assert.Contains(t, text, models.NotificationKioskState)
	case <-time.After(5 * time.Second):
		t.Fatal("the local stream did not receive kiosk.state")
	}
}

func TestCloseUnpairedSessionsWithNothingConnected(t *testing.T) {
	t.Parallel()
	session := newWebSocketSession()
	t.Cleanup(func() { _ = session.Close() })
	// Nothing is connected, so there is nothing to close and nothing to
	// fail on.
	closeUnpairedSessions(session, platformids.Mister)
}
