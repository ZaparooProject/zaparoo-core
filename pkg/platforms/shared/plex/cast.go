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

package plex

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	castPlexAppID   = "9AC194DC"
	nsConnection    = "urn:x-cast:com.google.cast.tp.connection"
	nsHeartbeat     = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsReceiver      = "urn:x-cast:com.google.cast.receiver"
	nsMedia         = "urn:x-cast:com.google.cast.media"
	nsPlex          = "urn:x-cast:plex"
	castSenderID    = "sender-0"
	castReceiverID  = "receiver-0"
	castDefaultPort = 8009
	// maxCastFrame bounds a frame from the network. Receiver and media
	// status messages are a few KiB.
	maxCastFrame = 64 << 10
)

// castLoadAckWait is how long a LOAD waits for the receiver to answer. A
// receiver that stays silent may still play, which readiness then confirms.
var castLoadAckWait = 10 * time.Second

// Cast message field numbers and enum values from the CastV2 CastMessage
// protobuf.
const (
	castFieldProtocolVersion = 1
	castFieldSourceID        = 2
	castFieldDestinationID   = 3
	castFieldNamespace       = 4
	castFieldPayloadType     = 5
	castFieldPayloadUTF8     = 6
	castFieldPayloadBinary   = 7
	castPayloadTypeString    = 0
)

// castMessage is a CastV2 CastMessage with a string payload.
type castMessage struct {
	SourceID      string
	DestinationID string
	Namespace     string
	PayloadUTF8   string
	PayloadBinary []byte
	PayloadType   uint64
}

func encodeCastMessage(m *castMessage) []byte {
	var b []byte
	b = protowire.AppendTag(b, castFieldProtocolVersion, protowire.VarintType)
	b = protowire.AppendVarint(b, 0)
	b = protowire.AppendTag(b, castFieldSourceID, protowire.BytesType)
	b = protowire.AppendString(b, m.SourceID)
	b = protowire.AppendTag(b, castFieldDestinationID, protowire.BytesType)
	b = protowire.AppendString(b, m.DestinationID)
	b = protowire.AppendTag(b, castFieldNamespace, protowire.BytesType)
	b = protowire.AppendString(b, m.Namespace)
	b = protowire.AppendTag(b, castFieldPayloadType, protowire.VarintType)
	b = protowire.AppendVarint(b, castPayloadTypeString)
	b = protowire.AppendTag(b, castFieldPayloadUTF8, protowire.BytesType)
	b = protowire.AppendString(b, m.PayloadUTF8)
	return b
}

func decodeCastMessage(b []byte) (*castMessage, error) {
	m := &castMessage{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, fmt.Errorf("cast message tag: %w", protowire.ParseError(n))
		}
		b = b[n:]
		switch {
		case typ == protowire.BytesType && num >= castFieldSourceID && num <= castFieldPayloadBinary &&
			num != castFieldPayloadType:
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, fmt.Errorf("cast message field %d: %w", num, protowire.ParseError(n))
			}
			b = b[n:]
			switch num { //nolint:exhaustive // the guard above admits only these fields
			case castFieldSourceID:
				m.SourceID = string(v)
			case castFieldDestinationID:
				m.DestinationID = string(v)
			case castFieldNamespace:
				m.Namespace = string(v)
			case castFieldPayloadUTF8:
				m.PayloadUTF8 = string(v)
			case castFieldPayloadBinary:
				m.PayloadBinary = append([]byte(nil), v...)
			}
		case typ == protowire.VarintType && num == castFieldPayloadType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return nil, fmt.Errorf("cast message payload type: %w", protowire.ParseError(n))
			}
			b = b[n:]
			m.PayloadType = v
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return nil, fmt.Errorf("cast message field %d: %w", num, protowire.ParseError(n))
			}
			b = b[n:]
		}
	}
	return m, nil
}

func writeCastFrame(w io.Writer, m *castMessage) error {
	body := encodeCastMessage(m)
	if len(body) > maxCastFrame {
		return fmt.Errorf("cast message of %d bytes is too large", len(body))
	}
	frame := make([]byte, 4, 4+len(body))
	binary.BigEndian.PutUint32(frame, uint32(len(body))) //nolint:gosec // G115: bounded by maxCastFrame above
	frame = append(frame, body...)
	if _, err := w.Write(frame); err != nil {
		return fmt.Errorf("failed to write cast message: %w", err)
	}
	return nil
}

func readCastFrame(r io.Reader) (*castMessage, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("failed to read cast frame: %w", err)
	}
	size := binary.BigEndian.Uint32(header[:])
	if size > maxCastFrame {
		return nil, fmt.Errorf("cast frame of %d bytes exceeds %d", size, maxCastFrame)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("failed to read cast frame: %w", err)
	}
	return decodeCastMessage(body)
}

// castDialer opens a TLS connection to a Cast device.
type castDialer func(ctx context.Context, addr string) (net.Conn, error)

// dialCastTLS connects to a Cast device. Cast devices present self-signed
// certificates that cannot be verified, and the connection only carries
// commands on the local network, so the certificate is not checked.
func dialCastTLS(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &tls.Dialer{Config: &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // G402: Cast devices use self-signed certificates; LAN only
		MinVersion:         tls.VersionTLS12,
	}}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to cast device: %w", err)
	}
	return conn, nil
}

// castPayload is the part of every Cast JSON payload the session reads.
type castPayload struct {
	Type      string          `json:"type"`
	Reason    string          `json:"reason"`
	Status    json.RawMessage `json:"status"`
	RequestID int             `json:"requestId"`
}

type castApplication struct {
	AppID       string `json:"appId"`
	TransportID string `json:"transportId"`
}

type castReceiverStatus struct {
	Applications []castApplication `json:"applications"`
}

type castMedia struct {
	ContentID string `json:"contentId"`
}

type castMediaStatus struct {
	Media          *castMedia `json:"media"`
	PlayerState    string     `json:"playerState"`
	MediaSessionID int        `json:"mediaSessionId"`
}

// castSession is one connection to a Cast device. It is used by one
// goroutine at a time.
type castSession struct {
	conn      net.Conn
	stop      func() bool
	requestID int
}

func openCastSession(ctx context.Context, dial castDialer, addr string) (*castSession, error) {
	if dial == nil {
		dial = dialCastTLS
	}
	conn, err := dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	s := &castSession{conn: conn, stop: stop}
	if err := s.connect(castReceiverID); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *castSession) close() {
	s.stop()
	_ = s.conn.Close()
}

func (s *castSession) send(dst, namespace string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode cast message: %w", err)
	}
	return writeCastFrame(s.conn, &castMessage{
		SourceID:      castSenderID,
		DestinationID: dst,
		Namespace:     namespace,
		PayloadUTF8:   string(data),
	})
}

// request sends a payload with the next request ID and returns the ID.
func (s *castSession) request(dst, namespace string, payload map[string]any) (int, error) {
	s.requestID++
	payload["requestId"] = s.requestID
	return s.requestID, s.send(dst, namespace, payload)
}

func (s *castSession) connect(dst string) error {
	return s.send(dst, nsConnection, map[string]any{
		"type":      "CONNECT",
		"origin":    map[string]any{},
		"userAgent": product,
	})
}

// await reads messages until match accepts one, answering heartbeats on the
// way.
func (s *castSession) await(match func(m *castMessage, p *castPayload) (bool, error)) error {
	for {
		m, readErr := readCastFrame(s.conn)
		if readErr != nil {
			return readErr
		}
		var p castPayload
		if m.PayloadUTF8 != "" {
			if jsonErr := json.Unmarshal([]byte(m.PayloadUTF8), &p); jsonErr != nil {
				log.Debug().Err(jsonErr).Str("namespace", m.Namespace).Msg("ignoring unreadable cast payload")
				continue
			}
		}
		switch {
		case m.Namespace == nsHeartbeat && p.Type == "PING":
			if sendErr := s.send(m.SourceID, nsHeartbeat, map[string]any{"type": "PONG"}); sendErr != nil {
				return sendErr
			}
			continue
		case m.Namespace == nsConnection && p.Type == "CLOSE":
			return fmt.Errorf("cast device closed the connection from %s", m.SourceID)
		}
		done, err := match(m, &p)
		if err != nil || done {
			return err
		}
	}
}

// plexTransport returns the running Plex app's transport ID, launching the
// app first when launch is set.
func (s *castSession) plexTransport(launch bool) (string, error) {
	var payload map[string]any
	if launch {
		payload = map[string]any{"type": "LAUNCH", "appId": castPlexAppID}
	} else {
		payload = map[string]any{"type": "GET_STATUS"}
	}
	if _, err := s.request(castReceiverID, nsReceiver, payload); err != nil {
		return "", err
	}
	var transport string
	err := s.await(func(m *castMessage, p *castPayload) (bool, error) {
		if m.Namespace != nsReceiver {
			return false, nil
		}
		switch p.Type {
		case "LAUNCH_ERROR", "INVALID_REQUEST":
			return false, fmt.Errorf("cast device refused to start Plex: %s", p.Reason)
		case "RECEIVER_STATUS":
		default:
			return false, nil
		}
		var status castReceiverStatus
		if err := json.Unmarshal(p.Status, &status); err != nil {
			return false, nil //nolint:nilerr // a status without applications is not the one awaited
		}
		for _, app := range status.Applications {
			if app.AppID == castPlexAppID && app.TransportID != "" {
				transport = app.TransportID
				return true, nil
			}
		}
		if !launch {
			return false, errors.New("plex is not running on the cast device")
		}
		return false, nil
	})
	if err != nil {
		return "", err
	}
	if err := s.connect(transport); err != nil {
		return "", err
	}
	return transport, nil
}

// castPlayer is a Cast device that plays Plex through the Plex receiver app.
type castPlayer struct {
	dial     castDialer
	name     string
	addr     string
	clientID string
}

func (p *castPlayer) Name() string { return p.name }
func (*castPlayer) Kind() string   { return playerKindCast }

// castLoadPayload builds a LOAD message in the shape pychromecast's Plex
// controller sends, which is what the Plex receiver expects.
func castLoadPayload(req *playRequest) map[string]any {
	return map[string]any{
		"type": "LOAD",
		"media": map[string]any{
			"contentId":   "/library/metadata/" + req.First.RatingKey,
			"streamType":  "BUFFERED",
			"contentType": req.Kind.queueType,
			"customData": map[string]any{
				"offset":       0,
				"directPlay":   true,
				"directStream": true,
				"subtitleSize": 100,
				"audioBoost":   100,
				"server": map[string]any{
					"machineIdentifier":        req.Server.MachineID,
					"transcoderVideo":          true,
					"transcoderVideoRemuxOnly": false,
					"transcoderAudio":          true,
					"version":                  req.Server.Version,
					"myPlexSubscription":       true,
					"isVerifiedHostname":       true,
					"protocol":                 req.Server.Protocol,
					"address":                  req.Server.Address,
					"port":                     req.Server.Port,
					"accessToken":              req.Server.Token,
					"user":                     map[string]any{"username": ""},
				},
				"containerKey": fmt.Sprintf("/playQueues/%d?own=1&window=200", req.Queue.ID),
			},
			"autoplay":       true,
			"currentTime":    0,
			"activeTrackIds": nil,
		},
	}
}

func (p *castPlayer) Play(ctx context.Context, req *playRequest) error {
	s, err := openCastSession(ctx, p.dial, p.addr)
	if err != nil {
		return err
	}
	defer s.close()
	transport, err := s.plexTransport(true)
	if err != nil {
		return err
	}
	if _, err = s.request(transport, nsMedia, castLoadPayload(req)); err != nil {
		return err
	}

	ackDeadline := time.Now().Add(castLoadAckWait)
	if d, ok := ctx.Deadline(); ok && d.Before(ackDeadline) {
		ackDeadline = d
	}
	_ = s.conn.SetDeadline(ackDeadline)
	err = s.await(func(m *castMessage, p *castPayload) (bool, error) {
		if m.Namespace != nsMedia {
			return false, nil
		}
		switch p.Type {
		case "MEDIA_STATUS":
			return true, nil
		case "LOAD_FAILED", "LOAD_CANCELLED", "INVALID_REQUEST":
			reason := p.Reason
			if reason == "" {
				reason = strings.ToLower(p.Type)
			}
			return false, fmt.Errorf("plex on the cast device refused the media: %s", reason)
		}
		return false, nil
	})
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() && ctx.Err() == nil {
		log.Debug().Str("player", p.name).Msg("cast device did not acknowledge the plex load")
		return nil
	}
	return err
}

func castCommandType(cmd string) string {
	switch cmd {
	case cmdPlay:
		return "PLAY"
	case cmdPause:
		return "PAUSE"
	case cmdStop:
		return "STOP"
	case cmdNext:
		return "NEXT"
	case cmdPrevious:
		return "PREVIOUS"
	case cmdStepForward:
		return "STEPFORWARD"
	case cmdStepBack:
		return "STEPBACK"
	default:
		return ""
	}
}

// Command sends a transport command on the Plex namespace, as pychromecast
// does, so no media session ID is needed.
func (p *castPlayer) Command(ctx context.Context, cmd string, _ *mediaKind) error {
	typ := castCommandType(cmd)
	if typ == "" {
		return fmt.Errorf("unsupported cast command %q", cmd)
	}
	s, err := openCastSession(ctx, p.dial, p.addr)
	if err != nil {
		return err
	}
	defer s.close()
	transport, err := s.plexTransport(false)
	if err != nil {
		return err
	}
	_, err = s.request(transport, nsPlex, map[string]any{"type": typ})
	return err
}

func (p *castPlayer) State(ctx context.Context) (*playerState, error) {
	s, err := openCastSession(ctx, p.dial, p.addr)
	if err != nil {
		return nil, err
	}
	defer s.close()
	transport, err := s.plexTransport(false)
	if err != nil {
		return nil, err
	}
	if _, err = s.request(transport, nsMedia, map[string]any{"type": "GET_STATUS"}); err != nil {
		return nil, err
	}
	state := &playerState{}
	err = s.await(func(m *castMessage, p *castPayload) (bool, error) {
		if m.Namespace != nsMedia || p.Type != "MEDIA_STATUS" {
			return false, nil
		}
		var statuses []castMediaStatus
		if jsonErr := json.Unmarshal(p.Status, &statuses); jsonErr != nil || len(statuses) == 0 {
			return true, nil //nolint:nilerr // no media session means nothing is playing
		}
		st := statuses[0]
		if st.Media != nil {
			state.RatingKey = keyFromPath(st.Media.ContentID)
		}
		switch st.PlayerState {
		case "PLAYING", "BUFFERING":
			state.Playing = true
		case "PAUSED":
			state.Paused = true
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return state, nil
}
