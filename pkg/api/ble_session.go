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
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/apigatt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

const (
	// bleOutboundMessages caps the number of messages queued for one
	// session. An encrypted frame can never be dropped without desyncing
	// the counters, so a session that overflows is closed instead.
	bleOutboundMessages = 256
	// bleOutboundHighWater is how many queued bytes make responses wait
	// and droppable notifications get skipped. A single message may be far
	// larger; it is the backlog behind it that is held back.
	bleOutboundHighWater = 1 << 20
	// bleGapTimeout is how long a chunk may be missing while later ones
	// wait for it. Reordering closes a gap in milliseconds; one that stays
	// open is a write the Bluetooth stack dropped, and nothing more can
	// be read from the connection.
	bleGapTimeout = 10 * time.Second
	// bleRefuseFor is how long a peer whose session Core ended is ignored
	// if it is never seen to disconnect.
	bleRefuseFor = 30 * time.Second
	// bleAckTimeout is how long the writer waits, with the send window
	// full, for the client to acknowledge anything at all.
	bleAckTimeout = 30 * time.Second
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
)

var (
	errBLESessionClosed = errors.New("bluetooth session closed")
	errBLEOutboundFull  = errors.New("bluetooth session outbound queue full")
	errBLEAckTimeout    = errors.New("bluetooth client stopped acknowledging")
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

// bleSession is one central's connection: it reassembles chunks, runs the
// pre-auth pairing methods, establishes the encrypted session, feeds the
// shared dispatcher, and chunks everything going back out.
type bleSession struct {
	idleTimer     clockwork.Timer
	gapTimer      clockwork.Timer
	p             ble.Peripheral
	ctx           context.Context
	cancel        context.CancelFunc
	room          chan struct{}
	dispatcher    *wsSessionDispatcher
	pairLimiter   *rate.Limiter
	inbound       chan []byte
	outbound      chan []byte
	acks          chan struct{}
	cs            *apimiddleware.ClientSession
	reasm         *apigatt.Reassembler
	t             *bleTransport
	peer          ble.Peer
	farewell      []byte
	outboundBytes int
	mtu           int
	closeOnce     sync.Once
	mu            syncutil.Mutex
	sendMu        syncutil.Mutex
	chunker       apigatt.Chunker
	tag           uint16
	sent          uint16
	acked         uint16
	// gapAt is the sequence number gapTimer is waiting on.
	gapAt  uint16
	tagSet bool
	state  bleAuthState
	closed bool
}

func newBLESession(t *bleTransport, peer ble.Peer, p ble.Peripheral) *bleSession {
	ctx, cancel := context.WithCancel(t.ctx)
	s := &bleSession{
		t:           t,
		p:           p,
		reasm:       apigatt.NewReassembler(apigatt.MaxUnauthenticatedMessageSize),
		pairLimiter: rate.NewLimiter(blePairingRate, blePairingBurst),
		inbound:     make(chan []byte, bleInboundMessages),
		outbound:    make(chan []byte, bleOutboundMessages),
		acks:        make(chan struct{}, 1),
		room:        make(chan struct{}, 1),
		ctx:         ctx,
		cancel:      cancel,
		peer:        peer,
		mtu:         apigatt.DefaultMTU,
	}
	s.dispatcher = newSessionDispatcher(ctx, s, t.core.platform)
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

// handleChunk consumes one write to the RX characteristic. BlueZ delivers
// each write on its own goroutine, so calls arrive in any order; the
// sequence number in the chunk is what orders them.
func (s *bleSession) handleChunk(chunk []byte, mtu int) {
	h, payload, err := apigatt.ParseChunk(chunk)
	if err != nil {
		s.framingError(err)
		return
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if mtu > 0 {
		s.mtu = mtu
	}
	if !s.tagSet {
		s.tag = h.Tag
		s.tagSet = true
	}
	if h.Tag != s.tag {
		s.mu.Unlock()
		s.framingError(fmt.Errorf("%w: session tag changed", apigatt.ErrMalformedChunk))
		return
	}
	if h.Ack {
		// An acknowledgement may only move forward, and only over chunks
		// that were actually sent.
		valid := h.Seq-s.acked <= s.sent-s.acked
		if valid {
			s.acked = h.Seq
		}
		s.mu.Unlock()
		if !valid {
			s.framingError(fmt.Errorf("%w: acknowledgement of chunk %d never sent", apigatt.ErrSequence, h.Seq))
			return
		}
		signal(s.acks)
		s.touchIdle()
		return
	}
	msgs, err := s.reasm.Push(h, payload)
	s.watchGapLocked()
	queued := true
	// Queued under the lock so messages are handled in the order the
	// reassembler released them.
	for _, msg := range msgs {
		select {
		case s.inbound <- msg:
		default:
			queued = false
		}
	}
	s.mu.Unlock()

	if err != nil {
		s.framingError(err)
		return
	}
	if !queued {
		s.shutdown("inbound queue overflow")
		return
	}
	s.touchIdle()
}

// watchGapLocked starts, restarts or stops the timer that gives up on a
// chunk that never arrives. The caller holds s.mu.
func (s *bleSession) watchGapLocked() {
	if s.reasm.Held() == 0 {
		if s.gapTimer != nil {
			s.gapTimer.Stop()
			s.gapTimer = nil
		}
		return
	}
	next := s.reasm.Next()
	if s.gapTimer != nil && s.gapAt == next {
		return
	}
	if s.gapTimer != nil {
		s.gapTimer.Stop()
	}
	s.gapAt = next
	s.gapTimer = s.t.clock.AfterFunc(bleGapTimeout, func() {
		log.Warn().Str("peer", s.peer.Address).Uint16("missing", next).Msg("bluetooth chunk never arrived")
		s.shutdown("a chunk was lost in transit")
	})
}

func (s *bleSession) framingError(err error) {
	log.Warn().Err(err).Str("peer", s.peer.Address).Msg("bluetooth framing error")
	s.shutdown("framing error")
}

// signal wakes whoever waits on ch without blocking when nobody does.
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
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

	// Encrypted frames are binary on this transport. Anything that opens
	// like JSON before a session exists is a plaintext request, and
	// plaintext is never acceptable over BLE: there is no loopback and no
	// legacy role, only paired clients.
	if cs == nil && bytes.HasPrefix(msg, []byte("{")) {
		endTrackedRequest()
		s.shutdown("plaintext is not accepted over bluetooth")
		return
	}

	frame, err := decryptFrame(cs, msg, s.t.encGateway, s.clientID(), apimiddleware.TransportBLE, true)
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
		s.reasm.SetMaxMessage(apigatt.MaxMessageSize)
		timer := s.idleTimer
		s.mu.Unlock()
		if timer != nil {
			timer.Reset(bleAuthenticatedIdleTimeout)
		}
		cs = frame.session
		log.Info().Str("peer", s.peer.Address).Msg("bluetooth client authenticated")
	case frameUnsupportedVersion:
		// The usual answer to this takes the place of the closing notice.
		if data, marshalErr := unsupportedEncryptionVersionResponse(); marshalErr == nil {
			s.mu.Lock()
			s.farewell = data
			s.mu.Unlock()
		}
		endTrackedRequest()
		s.shutdown("unsupported encryption version")
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
	env.ZapScriptHold = func() bool {
		return s.dispatcher.holdZapScript(s.t.core.st.AcquireZapScriptHold)
	}
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
		if sendErr := sendWSEncryptedError(
			env.Context, s, cs, models.NullRPCID, JSONRPCErrorInternalError,
		); sendErr != nil {
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
	backedUp := queued > bleOutboundHighWater/2 || len(s.outbound) > bleOutboundMessages/2
	if backedUp && bleDroppableNotifications[method] {
		log.Debug().Str("method", method).Int("queued", queued).Msg("ble: link backed up, dropping notification")
		return
	}
	if err := cs.SendEncryptedFrame(data, s.Write); err != nil {
		log.Warn().Err(err).Str("peer", s.peer.Address).Msg("ble: sending notification")
		s.shutdown("notification write failed")
	}
}

// Write implements sessionWriter: it queues one complete wire message and
// never blocks, because callers hold the encryption lock. Encryption has
// already happened, so a message that cannot be queued ends the session
// rather than desyncing the counters.
func (s *bleSession) Write(msg []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errBLESessionClosed
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

// WaitWritable implements writableWaiter: responses wait here while the
// link works through what is already queued.
func (s *bleSession) WaitWritable(ctx context.Context) error {
	for {
		s.mu.Lock()
		closed, queued := s.closed, s.outboundBytes
		s.mu.Unlock()
		if closed {
			return errBLESessionClosed
		}
		if queued < bleOutboundHighWater {
			return nil
		}
		select {
		case <-s.room:
		case <-s.ctx.Done():
			return errBLESessionClosed
		case <-ctx.Done():
			return fmt.Errorf("waiting for bluetooth link: %w", ctx.Err())
		}
	}
}

// Close implements sessionWriter.
func (s *bleSession) Close() error {
	s.shutdown("closed by dispatcher")
	return nil
}

// send chunks one message onto the TX characteristic. With paced set it
// stays inside the send window, waiting for the client's acknowledgements.
func (s *bleSession) send(msg []byte, paced bool) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	s.mu.Lock()
	s.chunker.Tag = s.tag
	mtu := s.mtu
	s.mu.Unlock()

	//nolint:wrapcheck // the emit errors are already this package's own
	return s.chunker.Split(msg, mtu, func(chunk []byte) error {
		if paced {
			if err := s.waitWindow(); err != nil {
				return err
			}
		}
		// Counted as sent before it reaches the radio, so the client's
		// acknowledgement can never arrive ahead of the count.
		s.mu.Lock()
		s.sent = s.chunker.Next()
		s.mu.Unlock()
		if err := s.p.Notify(s.peer, apigatt.TXCharUUID, chunk); err != nil {
			return fmt.Errorf("notify: %w", err)
		}
		return nil
	})
}

// waitWindow blocks while SendWindow chunks are unacknowledged.
func (s *bleSession) waitWindow() error {
	for {
		s.mu.Lock()
		outstanding := s.sent - s.acked
		s.mu.Unlock()
		if outstanding < apigatt.SendWindow {
			return nil
		}
		select {
		case <-s.acks:
		case <-s.ctx.Done():
			return errBLESessionClosed
		case <-s.t.clock.After(bleAckTimeout):
			s.mu.Lock()
			sent, acked := s.sent, s.acked
			s.mu.Unlock()
			return fmt.Errorf("%w: sent through chunk %d, acknowledged through %d", errBLEAckTimeout, sent, acked)
		}
	}
}

// writeNow sends a message straight away, bypassing the queue and the send
// window, for the last words of a session that is about to end.
func (s *bleSession) writeNow(msg []byte) {
	s.mu.Lock()
	spoken := s.tagSet
	s.mu.Unlock()
	if len(msg) == 0 || !spoken {
		return
	}
	if err := s.send(msg, false); err != nil {
		log.Debug().Err(err).Str("peer", s.peer.Address).Msg("ble: final write failed")
	}
}

// writer sends queued messages in order.
func (s *bleSession) writer() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg := <-s.outbound:
			started := s.t.clock.Now()
			err := s.send(msg, true)
			if took := s.t.clock.Since(started); took > time.Second {
				log.Debug().Int("bytes", len(msg)).Dur("took", took).Err(err).
					Str("peer", s.peer.Address).Msg("ble: sent large message")
			}
			s.mu.Lock()
			s.outboundBytes -= len(msg)
			s.mu.Unlock()
			signal(s.room)
			if err != nil {
				if !errors.Is(err, errBLESessionClosed) {
					log.Warn().Err(err).Str("peer", s.peer.Address).Msg("ble: sending message")
				}
				s.shutdown("send failed")
				return
			}
		}
	}
}

// farewellMessage is the plaintext notice sent as a session Core is ending
// closes.
func (s *bleSession) farewellMessage(reason string) []byte {
	s.mu.Lock()
	farewell := s.farewell
	s.mu.Unlock()
	if farewell != nil {
		return farewell
	}
	errObj := JSONRPCErrorSessionClosed
	errObj.Data = map[string]any{"reason": reason}
	data, err := json.Marshal(models.ResponseErrorObject{JSONRPC: "2.0", ID: models.NullRPCID, Error: &errObj})
	if err != nil {
		return nil
	}
	return data
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
		if s.gapTimer != nil {
			s.gapTimer.Stop()
			s.gapTimer = nil
		}
		s.mu.Unlock()

		log.Info().Str("peer", s.peer.Address).Str("reason", reason).Msg("bluetooth client session closed")
		s.cancel()
		if timer != nil {
			timer.Stop()
		}
		s.t.forget(s)
		if disconnect {
			s.t.refuse(s.peer)
		}
		s.t.wg.Add(1)
		go func() {
			defer s.t.wg.Done()
			if disconnect {
				// Sent before the dispatcher is torn down and the link
				// dropped. On a stack that cannot drop the link it is the
				// only notice the client gets.
				s.writeNow(s.farewellMessage(reason))
			}
			s.dispatcher.close()
			if disconnect {
				disconnectPeer(s.p, s.peer)
			}
		}()
	})
}
