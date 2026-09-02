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
	"encoding/json"
	"fmt"
	"time"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
)

const (
	// melodySessionEncryptionKey attaches ClientSession to melody session storage.
	melodySessionEncryptionKey   = "encryption_session"
	melodySessionAuthStateKey    = "authentication_state"
	melodySessionAuthDeadlineKey = "authentication_deadline"
)

type webSocketAuthState string

const (
	// webSocketAuthUnsettled is the initial state when encryption is optional:
	// the client may still negotiate an encrypted session on its first frame,
	// so the transport mode is not known yet. Unlike webSocketAuthPending it
	// does not arm the authentication deadline, because staying quiet is
	// legitimate for a client that is never required to authenticate.
	webSocketAuthUnsettled webSocketAuthState = "unsettled"
	webSocketAuthPending   webSocketAuthState = "pending"
	webSocketAuthPlaintext webSocketAuthState = "plaintext"
	webSocketAuthEncrypted webSocketAuthState = "encrypted"
)

func getWebSocketAuthState(session *melody.Session) webSocketAuthState {
	value, ok := session.Get(melodySessionAuthStateKey)
	if !ok {
		return webSocketAuthPending
	}
	state, ok := value.(webSocketAuthState)
	if !ok {
		return webSocketAuthPending
	}
	return state
}

func setWebSocketAuthState(session *melody.Session, state webSocketAuthState) {
	session.Set(melodySessionAuthStateKey, state)
	if state != webSocketAuthPending {
		stopWebSocketAuthDeadline(session)
	}
}

func startWebSocketAuthDeadline(session *melody.Session, timeout time.Duration) {
	if getWebSocketAuthState(session) != webSocketAuthPending {
		return
	}
	timer := time.AfterFunc(timeout, func() {
		if getWebSocketAuthState(session) == webSocketAuthPending {
			closeMelodySession(session)
		}
	})
	session.Set(melodySessionAuthDeadlineKey, timer)
}

func stopWebSocketAuthDeadline(session *melody.Session) {
	value, ok := session.Get(melodySessionAuthDeadlineKey)
	if !ok {
		return
	}
	timer, ok := value.(*time.Timer)
	if ok {
		timer.Stop()
	}
}

// getClientSession returns the encryption session (or nil for plaintext).
func getClientSession(session *melody.Session) *apimiddleware.ClientSession {
	v, ok := session.Get(melodySessionEncryptionKey)
	if !ok {
		return nil
	}
	cs, ok := v.(*apimiddleware.ClientSession)
	if !ok {
		return nil
	}
	return cs
}

// setClientSession attaches an encryption session to a melody session.
func setClientSession(session *melody.Session, cs *apimiddleware.ClientSession) {
	session.Set(melodySessionEncryptionKey, cs)
}

// encryptionRequiredErrorResponse returns a JSON-RPC error for plaintext
// frames when encryption is required.
func encryptionRequiredErrorResponse() ([]byte, error) {
	data, err := json.Marshal(models.ResponseErrorObject{
		JSONRPC: "2.0",
		ID:      models.NullRPCID,
		Error: &models.ErrorObject{
			Code:    -32002,
			Message: "encryption required for remote connections",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal encryption-required error: %w", err)
	}
	return data, nil
}

// unsupportedEncryptionVersionResponse returns a version mismatch error
// (per JSON-RPC 2.0 §5.1).
func unsupportedEncryptionVersionResponse() ([]byte, error) {
	data, err := json.Marshal(models.ResponseErrorObject{
		JSONRPC: "2.0",
		ID:      models.NullRPCID,
		Error: &models.ErrorObject{
			Code:    -32001,
			Message: "unsupported encryption version",
			Data: map[string]any{
				"supported": []int{apimiddleware.EncryptionProtoVersion},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal unsupported-version error: %w", err)
	}
	return data, nil
}

// sendWSPlaintext sends plaintext before encryption handshake completes.
func sendWSPlaintext(session *melody.Session, data []byte) {
	if err := session.Write(data); err != nil {
		log.Debug().Err(err).Msg("failed to write plaintext WS message")
	}
}
