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
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func startMediaAssetWSServer(
	t *testing.T,
	root string,
	mediaDB *helpers.MockMediaDBI,
	encrypted bool,
	gateway *apimiddleware.EncryptionGateway,
) string {
	t.Helper()

	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test")
	pl.On("Settings").Return(platforms.Settings{})
	pl.On("RootDirs", mock.Anything).Return([]string{root})
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetEncryptionEnabled(encrypted)
	st, _ := state.NewState(pl, "media-asset-ws-test")

	m := newWebSocketSession()
	m.HandleDisconnect(func(session *melody.Session) {
		closeWSDispatcher(session)
	})
	m.HandleMessage(handleWSMessage(
		NewMethodMap(), pl, cfg, st, nil, nil,
		&database.Database{MediaDB: mediaDB}, nil, nil, nil, nil, nil,
		nil, nil, gateway, nil, nil,
	))

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		_ = m.HandleRequest(w, r)
	})
	srv := httptest.NewUnstartedServer(mux)
	if encrypted {
		var lc net.ListenConfig
		ln, listenErr := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, listenErr)
		srv.Listener = &fakeRemoteListener{
			Listener: ln,
			fakeAddr: &net.TCPAddr{IP: net.ParseIP(testEncryptionSourceIP), Port: 12345},
		}
	}
	srv.Start()
	t.Cleanup(func() {
		st.StopService()
		_ = m.Close()
		srv.Close()
	})
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return "ws://" + u.Host + "/api"
}

func expectMediaAssetWSRequest(
	mediaDB *helpers.MockMediaDBI,
	manualPath string,
) {
	row := database.MediaFullRow{
		Media:  database.Media{DBID: 1, Path: filepath.Join("games", "game.rom")},
		Title:  database.MediaTitle{DBID: 2, Name: "Game"},
		System: database.System{DBID: 3, SystemID: "NES", Name: "NES"},
	}
	mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{int64(1)}).
		Return(map[int64]database.MediaFullRow{1: row}, nil).Once()
	mediaDB.On("GetMediaPropertyMetadata", mock.Anything, int64(1)).
		Return([]database.MediaProperty{{TypeTag: "property:manual", Text: manualPath}}, nil).Once()
}

func assertMediaAssetWireResponse(t *testing.T, wire, expected []byte) {
	t.Helper()
	var response struct {
		Error  *models.ErrorObject       `json:"error"`
		Result models.MediaAssetResponse `json:"result"`
	}
	require.NoError(t, json.Unmarshal(wire, &response))
	require.Nil(t, response.Error)
	decoded, err := base64.StdEncoding.DecodeString(response.Result.Data)
	require.NoError(t, err)
	require.Equal(t, expected, decoded)
}

func TestMediaAssetPlaintextWebSocket(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manual := []byte("%PDF-1.7\nplain websocket")
	manualPath := filepath.Join(root, "manual.pdf")
	require.NoError(t, os.WriteFile(manualPath, manual, 0o600))
	mediaDB := helpers.NewMockMediaDBI()
	expectMediaAssetWSRequest(mediaDB, manualPath)
	conn := dialWS(t, startMediaAssetWSServer(t, root, mediaDB, false, nil))
	defer func() { _ = conn.Close() }()

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(
		`{"jsonrpc":"2.0","method":"media.asset","params":{"mediaId":1,"assetType":"manual"},"id":1}`,
	)))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, wire, err := conn.ReadMessage()
	require.NoError(t, err)
	assertMediaAssetWireResponse(t, wire, manual)
	mediaDB.AssertExpectations(t)
}

func TestMediaAssetEncryptedWebSocket(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manual := []byte("%PDF-1.7\nencrypted websocket")
	manualPath := filepath.Join(root, "manual.pdf")
	require.NoError(t, os.WriteFile(manualPath, manual, 0o600))
	mediaDB := helpers.NewMockMediaDBI()
	expectMediaAssetWSRequest(mediaDB, manualPath)
	request := []byte(
		`{"jsonrpc":"2.0","method":"media.asset","params":{"mediaId":1,"assetType":"manual"},"id":1}`,
	)
	first := newTestEncryptionFirstFrameForRequest(t, request)
	conn := dialWS(t, startMediaAssetWSServer(t, root, mediaDB, true, first.gateway))
	defer func() { _ = conn.Close() }()

	firstWire, err := json.Marshal(first.frame) //nolint:gosec // Deliberate encrypted test frame.
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, firstWire))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, encryptedWire, err := conn.ReadMessage()
	require.NoError(t, err)
	var frame apimiddleware.EncryptedFrame
	require.NoError(t, json.Unmarshal(encryptedWire, &frame))
	ciphertext, err := base64.StdEncoding.DecodeString(frame.Ciphertext)
	require.NoError(t, err)
	wire, err := crypto.Decrypt(
		first.secrets.s2cGCM,
		first.secrets.s2cNonce,
		0,
		ciphertext,
		first.secrets.aad,
	)
	require.NoError(t, err)
	assertMediaAssetWireResponse(t, wire, manual)

	// A subsequent encrypted request proves chunk delivery did not desynchronize
	// either direction's AEAD counter.
	expectMediaAssetWSRequest(mediaDB, manualPath)
	ciphertext, err = crypto.Encrypt(
		first.secrets.c2sGCM,
		first.secrets.c2sNonce,
		1,
		request,
		first.secrets.aad,
	)
	require.NoError(t, err)
	nextWire, err := json.Marshal(apimiddleware.EncryptedFrame{
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, nextWire))
	_, encryptedWire, err = conn.ReadMessage()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encryptedWire, &frame))
	ciphertext, err = base64.StdEncoding.DecodeString(frame.Ciphertext)
	require.NoError(t, err)
	wire, err = crypto.Decrypt(
		first.secrets.s2cGCM,
		first.secrets.s2cNonce,
		1,
		ciphertext,
		first.secrets.aad,
	)
	require.NoError(t, err)
	assertMediaAssetWireResponse(t, wire, manual)
	mediaDB.AssertExpectations(t)
}
