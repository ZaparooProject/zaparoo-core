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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/apigatt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

const (
	// bleOutboundLimit caps the bytes queued for one session. A link that
	// cannot drain this much is dead or hopelessly slow, and an encrypted
	// frame can never be dropped without desyncing the counters, so the
	// session is closed instead.
	bleOutboundLimit = 1 << 20
	// bleOutboundMessages caps the number of queued messages.
	bleOutboundMessages = 64
	// bleInboundMessages caps reassembled messages waiting to be handled.
	// Chunks arrive on BlueZ's goroutines; handling runs on one goroutine
	// per session so encrypted frames are decrypted in the order they
	// completed, which the AEAD counters require.
	bleInboundMessages = 32

	// Pre-auth pairing methods, handled by the transport and never exposed
	// on the method map.
	blePairStartMethod  = "pair.start"
	blePairFinishMethod = "pair.finish"

	// Pairing over BLE is limited per connection to one request per second
	// with room for the start and finish pair to arrive back to back, and
	// across all connections by the transport's own limiter, because a peer
	// can reset the per-connection one by reconnecting under a new private
	// address. The PIN attempt counter is what actually bounds guessing.
	blePairingRate           = rate.Limit(1)
	blePairingBurst          = 2
	bleTransportPairingRate  = rate.Limit(2)
	bleTransportPairingBurst = 4

	// Pairing requests are capped at the sizes the HTTP endpoints accept.
	blePairStartMaxParams  = 16 * 1024
	blePairFinishMaxParams = 4 * 1024

	// blePendingIdleTimeout is how long an unauthenticated session may sit
	// silent. It covers a user reading the PIN off the screen and typing it
	// before the app sends anything, and matches the pairing session TTL.
	blePendingIdleTimeout = 2 * time.Minute
	// bleAuthenticatedIdleTimeout ends an authenticated session that sends
	// nothing at all. The link layer normally reports a lost peer, but the
	// report travels over a signal path that can drop under load, and the
	// heartbeat is cheap on a radio link.
	bleAuthenticatedIdleTimeout = 5 * time.Minute

	// bleEnvelopeOverhead is the fixed cost of the encrypted frame around a
	// plaintext: the AEAD tag, the JSON envelope, and base64 rounding.
	bleEnvelopeOverhead = 64
)

var (
	errBLESessionClosed = errors.New("bluetooth session closed")
	errBLEOutboundFull  = errors.New("bluetooth session outbound queue full")
)

// bleDroppableNotifications are the chatty progress notifications a
// backed-up link can skip without the app losing state.
var bleDroppableNotifications = map[string]bool{
	models.NotificationMediaIndexing: true,
	models.NotificationMediaScraping: true,
}

type bleAuthState uint8

const (
	// bleAuthPending is the initial state: only pairing requests and an
	// encrypted first frame are accepted.
	bleAuthPending bleAuthState = iota
	bleAuthEncrypted
)

// blePlaintextLimit is the largest plaintext whose encrypted frame still
// fits a wire message of the given size. Base64 grows the ciphertext by a
// third.
func blePlaintextLimit(wire int) int {
	return wire*3/4 - bleEnvelopeOverhead
}

// bleSession is one central's connection: it reassembles chunks, runs the
// pre-auth pairing methods, establishes the encrypted session, feeds the
// shared dispatcher, and chunks everything going back out.
type bleSession struct {
	t             *bleTransport
	p             bluez.Peripheral
	reasm         *apigatt.Reassembler
	dispatcher    *wsSessionDispatcher
	cs            *apimiddleware.ClientSession
	idleTimer     clockwork.Timer
	pairLimiter   *rate.Limiter
	inbound       chan []byte
	outbound      chan []byte
	ctx           context.Context
	cancel        context.CancelFunc
	peer          bluez.Peer
	maxPlaintext  int
	outboundBytes int
	mtu           int
	mu            syncutil.Mutex
	closeOnce     sync.Once
	tag           uint16
	tagSet        bool
	state         bleAuthState
	closed        bool
}

func newBLESession(t *bleTransport, peer bluez.Peer, p bluez.Peripheral) *bleSession {
	ctx, cancel := context.WithCancel(t.ctx)
	s := &bleSession{
		t:            t,
		p:            p,
		reasm:        apigatt.NewReassembler(t.clock, apigatt.MaxMessageSize),
		pairLimiter:  rate.NewLimiter(blePairingRate, blePairingBurst),
		inbound:      make(chan []byte, bleInboundMessages),
		outbound:     make(chan []byte, bleOutboundMessages),
		ctx:          ctx,
		cancel:       cancel,
		peer:         peer,
		maxPlaintext: blePlaintextLimit(apigatt.MaxMessageSize),
		mtu:          apigatt.DefaultMTU,
	}
	s.dispatcher = newSessionDispatcher(ctx, s, t.core.platform)
	s.dispatcher.maxResponseSize = s.maxPlaintext
	s.idleTimer = t.clock.AfterFunc(blePendingIdleTimeout, func() {
		s.shutdown("idle timeout")
	})
	go s.reader()
	go s.writer()
	return s
}

// clientID names the session the way RemoteAddr names a WebSocket client.
func (s *bleSession) clientID() string {
	return "ble:" + s.peer.Address
}

// handleChunk consumes one write to the RX characteristic.
func (s *bleSession) handleChunk(chunk []byte, mtu int) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if mtu > 0 {
		s.mtu = mtu
	}
	if !s.tagSet {
		if h, _, err := apigatt.ParseChunk(chunk); err == nil {
			s.tag = h.Tag
			s.tagSet = true
		}
	}
	msg, err := s.reasm.Push(chunk)
	queued := true
	if err == nil && msg != nil {
		// Queued under the lock so messages are handled in the order they
		// completed, whatever order BlueZ's goroutines run in.
		select {
		case s.inbound <- msg:
		default:
			queued = false
		}
	}
	s.mu.Unlock()

	if err != nil {
		log.Warn().Err(err).Str("peer", s.peer.Address).Msg("bluetooth framing error")
		s.shutdown("framing error")
		return
	}
	if !queued {
		s.shutdown("inbound queue overflow")
		return
	}
	s.touchIdle()
}

// reader handles reassembled messages one at a time.
func (s *bleSession) reader() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg := <-s.inbound:
			s.handleMessage(msg)
		}
	}
}

// touchIdle restarts the idle timer for the session's current state.
func (s *bleSession) touchIdle() {
	s.mu.Lock()
	timeout := blePendingIdleTimeout
	if s.state == bleAuthEncrypted {
		timeout = bleAuthenticatedIdleTimeout
	}
	timer := s.idleTimer
	closed := s.closed
	s.mu.Unlock()
	if !closed && timer != nil {
		timer.Reset(timeout)
	}
}

// handleMessage dispatches one reassembled message the same way
// handleWSMessage dispatches one WebSocket frame.
func (s *bleSession) handleMessage(msg []byte) {
	tracker := s.t.tracker
	trackerActive := false
	defer func() {
		if r := recover(); r != nil {
			if trackerActive && tracker != nil {
				tracker.RequestEnded()
			}
			log.Error().Interface("panic", r).Str("peer", s.peer.Address).Msg("panic in bluetooth message handler")
			s.shutdown("internal error")
		}
	}()
	if tracker != nil {
		tracker.RequestStarted()
		trackerActive = true
	}
	endTrackedRequest := func() {
		if trackerActive && tracker != nil {
			tracker.RequestEnded()
		}
		trackerActive = false
	}
	handoffTrackedRequest := func() {
		trackerActive = false
	}

	s.mu.Lock()
	cs, state := s.cs, s.state
	s.mu.Unlock()

	if state == bleAuthPending && s.handlePairing(msg) {
		endTrackedRequest()
		return
	}

	frame, err := decryptFrame(cs, msg, s.t.encGateway, s.clientID(), apimiddleware.TransportBLE)
	switch frame.outcome {
	case frameDecrypted:
		if err != nil {
			log.Warn().Err(err).Str("peer", s.peer.Address).Msg("ble: decryption failed on established session")
			endTrackedRequest()
			s.shutdown("decryption failed")
			return
		}
	case frameEstablished:
		if err != nil {
			log.Warn().Err(err).Str("peer", s.peer.Address).Msg("ble: failed to establish encrypted session")
			endTrackedRequest()
			s.shutdown("failed to establish encrypted session")
			return
		}
		s.mu.Lock()
		s.cs = frame.session
		s.state = bleAuthEncrypted
		timer := s.idleTimer
		s.mu.Unlock()
		if timer != nil {
			timer.Reset(bleAuthenticatedIdleTimeout)
		}
		cs = frame.session
		log.Info().Str("peer", s.peer.Address).Msg("bluetooth client authenticated")
	case frameUnsupportedVersion:
		// Sent on the spot: the queue would be cancelled by the shutdown
		// before the writer got to it.
		if data, marshalErr := unsupportedEncryptionVersionResponse(); marshalErr == nil {
			s.writeNow(data)
		}
		endTrackedRequest()
		s.shutdown("unsupported encryption version")
		return
	case framePlaintext:
		// Plaintext is never acceptable over BLE: there is no loopback and
		// no legacy role, only paired clients.
		endTrackedRequest()
		s.shutdown("plaintext is not accepted over bluetooth")
		return
	default:
		log.Error().Uint8("outcome", uint8(frame.outcome)).Msg("ble: unhandled frame outcome")
		endTrackedRequest()
		s.shutdown("internal error")
		return
	}
	plaintext := frame.plaintext

	if s.t.lastSeen != nil {
		s.t.lastSeen.Touch(cs.AuthToken(), time.Now().Unix())
	}

	if bytes.Equal(plaintext, []byte("ping")) {
		if err := s.dispatcher.enqueuePong(cs, tracker); err != nil {
			log.Warn().Err(err).Msg("ble: queueing pong")
			endTrackedRequest()
			s.shutdown("pong queue failed")
			return
		}
		handoffTrackedRequest()
		return
	}

	platformID := ""
	if s.t.core.platform != nil {
		platformID = s.t.core.platform.ID()
	}
	env := s.t.core.newRequestEnv(
		s.t.core.st.GetContext(), s.dispatcher.inputSession, s.clientID(), platformID, false,
	)
	env.ClientRole = cs.ClientRole()

	if err := enqueueWSRequest(s.dispatcher, s.t.methodMap, &env, plaintext, cs, tracker); err != nil {
		var queueFullErr *wsRequestQueueFullError
		if errors.As(err, &queueFullErr) {
			log.Warn().
				Str("method", queueFullErr.method).
				Str("requestId", requestIDForLog(queueFullErr.requestID)).
				Str("priority", queueFullErr.priority.String()).
				Msg("bluetooth request rejected because queue is full")
			if queueFullErr.requestID.IsAbsent() {
				endTrackedRequest()
				return
			}
			s.dispatcher.enqueueResponse(&wsResponseJob{
				result: requestResult{
					ID:          queueFullErr.requestID,
					Error:       &JSONRPCErrorServerBusy,
					ShouldReply: true,
				},
				cs:      cs,
				tracker: tracker,
				method:  queueFullErr.method,
			})
			handoffTrackedRequest()
			return
		}

		log.Warn().Err(err).Msg("failed to queue bluetooth request")
		endTrackedRequest()
		if sendErr := sendWSEncryptedError(s, cs, models.NullRPCID, JSONRPCErrorInternalError); sendErr != nil {
			s.shutdown("error response failed")
		}
		return
	}
	handoffTrackedRequest()
}

// handlePairing answers the pre-auth pairing methods. It reports false when
// the message is not a pairing request, leaving it to the encryption path.
func (s *bleSession) handlePairing(msg []byte) bool {
	var req models.RequestObject
	if err := json.Unmarshal(msg, &req); err != nil || req.Method == "" {
		return false
	}
	method := strings.ToLower(req.Method)
	if method != blePairStartMethod && method != blePairFinishMethod {
		return false
	}
	id := req.ID
	if id.IsAbsent() {
		id = models.NullRPCID
	}

	if s.t.pairing == nil {
		s.writePairingError(id, http.StatusServiceUnavailable, "pairing unavailable")
		return true
	}
	if !s.pairLimiter.Allow() || !s.t.pairLimiter.Allow() {
		s.writePairingError(id, http.StatusTooManyRequests, "too many pairing requests")
		return true
	}

	var (
		result any
		err    error
	)
	switch method {
	case blePairStartMethod:
		var params pairStartRequest
		if len(req.Params) > blePairStartMaxParams || json.Unmarshal(req.Params, &params) != nil {
			s.writePairingError(id, http.StatusBadRequest, "invalid request body")
			return true
		}
		result, err = s.t.pairing.pairStart(params)
	default:
		var params pairFinishRequest
		if len(req.Params) > blePairFinishMaxParams || json.Unmarshal(req.Params, &params) != nil {
			s.writePairingError(id, http.StatusBadRequest, "invalid request body")
			return true
		}
		result, err = s.t.pairing.pairFinish(params, s.clientID())
	}
	if err != nil {
		status, public := pairingErrorStatus(err)
		s.writePairingError(id, status, public)
		return true
	}

	data, marshalErr := json.Marshal(models.ResponseObject{JSONRPC: "2.0", ID: id, Result: result})
	if marshalErr != nil {
		log.Error().Err(marshalErr).Msg("ble: marshalling pairing response")
		s.shutdown("pairing response failed")
		return true
	}
	if writeErr := s.Write(data); writeErr != nil {
		log.Warn().Err(writeErr).Msg("ble: writing pairing response")
	}
	return true
}

// writePairingError sends the pairing error the HTTP endpoints would have
// answered with, carried as a JSON-RPC error.
func (s *bleSession) writePairingError(id models.RPCID, status int, message string) {
	errObj := JSONRPCErrorPairingFailed
	errObj.Data = map[string]any{"status": status, "message": message}
	data, err := json.Marshal(models.ResponseErrorObject{JSONRPC: "2.0", ID: id, Error: &errObj})
	if err != nil {
		log.Error().Err(err).Msg("ble: marshalling pairing error")
		return
	}
	if writeErr := s.Write(data); writeErr != nil {
		log.Warn().Err(writeErr).Msg("ble: writing pairing error")
	}
}

// sendNotification encrypts and queues one notification for an
// authenticated session, dropping what the link cannot afford.
func (s *bleSession) sendNotification(method string, data []byte) {
	s.mu.Lock()
	closed, state, cs, queued := s.closed, s.state, s.cs, s.outboundBytes
	s.mu.Unlock()
	if closed || state != bleAuthEncrypted || cs == nil {
		return
	}
	if len(data) > s.maxPlaintext {
		log.Debug().Str("method", method).Int("bytes", len(data)).Msg("ble: notification too large, dropped")
		return
	}
	if queued > bleOutboundLimit/2 && bleDroppableNotifications[method] {
		log.Debug().Str("method", method).Int("queued", queued).Msg("ble: link backed up, dropping notification")
		return
	}
	if err := cs.SendEncryptedFrame(data, s.Write); err != nil {
		log.Warn().Err(err).Str("peer", s.peer.Address).Msg("ble: sending notification")
		s.shutdown("notification write failed")
	}
}

// Write implements sessionWriter: it queues one complete wire message.
// Encryption has already happened, so a message that cannot be queued
// ends the session rather than desyncing the counters.
func (s *bleSession) Write(msg []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errBLESessionClosed
	}
	if s.outboundBytes+len(msg) > bleOutboundLimit {
		s.mu.Unlock()
		s.shutdown("outbound queue overflow")
		return errBLEOutboundFull
	}
	s.outboundBytes += len(msg)
	s.mu.Unlock()

	select {
	case s.outbound <- append([]byte(nil), msg...):
		return nil
	default:
		s.mu.Lock()
		s.outboundBytes -= len(msg)
		s.mu.Unlock()
		s.shutdown("outbound queue overflow")
		return errBLEOutboundFull
	}
}

// Close implements sessionWriter.
func (s *bleSession) Close() error {
	s.shutdown("closed by dispatcher")
	return nil
}

// writeNow chunks a message straight onto the characteristic, bypassing
// the queue, for the last words of a session that is about to end.
func (s *bleSession) writeNow(msg []byte) {
	s.mu.Lock()
	tag, mtu := s.tag, s.mtu
	s.mu.Unlock()
	chunks, err := apigatt.Chunker{MTU: mtu, Tag: tag}.Split(msg)
	if err != nil {
		return
	}
	for _, chunk := range chunks {
		if err := s.p.Notify(apigatt.TXCharUUID, chunk); err != nil {
			return
		}
	}
}

// writer chunks queued messages onto the TX characteristic in order.
func (s *bleSession) writer() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg := <-s.outbound:
			s.mu.Lock()
			s.outboundBytes -= len(msg)
			tag, mtu := s.tag, s.mtu
			s.mu.Unlock()

			chunks, err := apigatt.Chunker{MTU: mtu, Tag: tag}.Split(msg)
			if err != nil {
				log.Warn().Err(err).Int("bytes", len(msg)).Msg("ble: message cannot be framed")
				s.shutdown("message cannot be framed")
				return
			}
			for _, chunk := range chunks {
				if s.ctx.Err() != nil {
					return
				}
				if err := s.p.Notify(apigatt.TXCharUUID, chunk); err != nil {
					log.Warn().Err(err).Str("peer", s.peer.Address).Msg("ble: notify failed")
					s.shutdown("notify failed")
					return
				}
			}
		}
	}
}

// shutdown ends the session and drops the peer's link.
func (s *bleSession) shutdown(reason string) {
	s.shutdownWith(reason, true)
}

// shutdownWith ends the session once. The dispatcher teardown waits on
// worker goroutines, so it runs off the caller's goroutine, which may be
// one of those workers.
func (s *bleSession) shutdownWith(reason string, disconnect bool) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		timer := s.idleTimer
		s.mu.Unlock()

		log.Info().Str("peer", s.peer.Address).Str("reason", reason).Msg("bluetooth client session closed")
		s.cancel()
		if timer != nil {
			timer.Stop()
		}
		s.t.forget(s)
		s.t.wg.Add(1)
		go func() {
			defer s.t.wg.Done()
			s.dispatcher.close()
			if disconnect {
				disconnectPeer(s.p, s.peer)
			}
		}()
	})
}
