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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
)

const (
	// melodySessionEncryptionKey attaches ClientSession to melody session storage.
	melodySessionEncryptionKey   = "encryption_session"
	melodySessionAuthStateKey    = "authentication_state"
	melodySessionAuthDeadlineKey = "authentication_deadline"
	melodySessionNotifQueueKey   = "notification_queue"
	melodySessionSettleTimerKey  = "settle_timer"
)

const (
	// webSocketSettleGrace bounds how long an optional-encryption session's
	// transport mode may stay unknown. A client that negotiates encryption
	// sends its first frame straight after the upgrade, so this only has to
	// cover a round trip. A client that never speaks is a notification
	// listener (the CLI's -read and -pair waits, the TUI's listeners) and is
	// settled as plaintext when the grace expires, because starving it of
	// notifications forever is worse than the leak this window closes.
	webSocketSettleGrace = 2 * time.Second
	// webSocketPendingNotifLimit caps what one unsettled session may hold, so
	// a burst during the grace window cannot grow without bound. Overflow
	// drops the newest notification, matching the broker's own best-effort
	// delivery.
	webSocketPendingNotifLimit = 64
)

type webSocketAuthState string

const (
	// webSocketAuthUnsettled is the initial state when encryption is optional:
	// the client may still negotiate an encrypted session on its first frame,
	// so the transport mode is not known yet. Unlike webSocketAuthPending it
	// does not arm the authentication deadline, because staying quiet is
	// legitimate for a client that is never required to authenticate; the
	// state instead resolves to plaintext after webSocketSettleGrace.
	// Notifications broadcast while unsettled are queued, not written.
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

// wsNotificationQueue holds notifications broadcast to a session whose
// transport mode is not settled yet. Writing them in the clear would desync a
// client that has already committed to an encrypted session, and dropping them
// would starve a listener that never sends a frame, so they are held until the
// mode is known and then either flushed in order or discarded.
//
// The flush happens under the lock so a broadcast arriving mid-flush cannot
// overtake the notifications it was queued behind.
type wsNotificationQueue struct {
	pending [][]byte
	mu      syncutil.Mutex
	settled bool
}

// enqueue holds a notification while the transport mode is unknown. It reports
// false once the mode is settled, which is the caller's signal to write
// normally.
func (q *wsNotificationQueue) enqueue(plaintext []byte) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.settled {
		return false
	}
	if len(q.pending) >= webSocketPendingNotifLimit {
		log.Warn().Msg("ws: dropping notification, unsettled session queue is full")
		return true
	}
	q.pending = append(q.pending, append([]byte(nil), plaintext...))
	return true
}

func getNotificationQueue(session *melody.Session) *wsNotificationQueue {
	value, ok := session.Get(melodySessionNotifQueueKey)
	if !ok {
		return nil
	}
	queue, ok := value.(*wsNotificationQueue)
	if !ok {
		return nil
	}
	return queue
}

// settleWebSocketTransport records the session's transport mode and releases
// the notifications queued while it was unknown: flushed in order for a
// plaintext session, discarded for one that turned out to be encrypted.
//
// The whole transition happens under the queue's lock so the settle grace
// expiring cannot race an encrypted first frame into publishing the queue in
// the clear, and so a broadcast arriving mid-flush cannot overtake the
// notifications it was queued behind. onlyIfUnsettled is how the grace timer
// stands down when the client has spoken in the meantime.
func settleWebSocketTransport(session *melody.Session, state webSocketAuthState, onlyIfUnsettled bool) {
	queue := getNotificationQueue(session)
	if queue == nil {
		if onlyIfUnsettled && getWebSocketAuthState(session) != webSocketAuthUnsettled {
			return
		}
		setWebSocketAuthState(session, state)
		stopWebSocketSettleGrace(session)
		return
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	if onlyIfUnsettled && queue.settled {
		return
	}
	pending := queue.pending
	queue.pending = nil
	queue.settled = true
	setWebSocketAuthState(session, state)
	stopWebSocketSettleGrace(session)
	if state != webSocketAuthPlaintext {
		return
	}
	for _, plaintext := range pending {
		if err := session.Write(plaintext); err != nil {
			logWSWriteError(err, "flushing queued notifications")
			return
		}
	}
}

// startWebSocketSettleGrace settles a silent session as plaintext once the
// grace expires, so a client that only ever listens still gets notifications.
func startWebSocketSettleGrace(session *melody.Session, grace time.Duration) {
	if getWebSocketAuthState(session) != webSocketAuthUnsettled {
		return
	}
	timer := time.AfterFunc(grace, func() {
		if getWebSocketAuthState(session) != webSocketAuthUnsettled {
			return
		}
		log.Debug().Msg("ws: no client frame within settle grace, treating session as plaintext")
		settleWebSocketTransport(session, webSocketAuthPlaintext, true)
	})
	session.Set(melodySessionSettleTimerKey, timer)
}

func stopWebSocketSettleGrace(session *melody.Session) {
	value, ok := session.Get(melodySessionSettleTimerKey)
	if !ok {
		return
	}
	if timer, ok := value.(*time.Timer); ok {
		timer.Stop()
	}
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
