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
package main

import (
	"context"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/schollz/pake/v3"
)

// These mirror the pairing and encryption contract in docs/api/encryption.md.
const (
	pairingCurve        = "p256"
	pairingProtoVersion = "zaparoo-v1"
	infoConfirmA        = "zaparoo-confirm-A"
	infoConfirmB        = "zaparoo-confirm-B"
	infoPairing         = "zaparoo-pairing-v1"
	encryptionVersion   = 1
	transportLabel      = "ble"
)

// credentials are what pairing leaves behind for later connections.
type credentials struct {
	DeviceID   string `json:"deviceId"`
	AuthToken  string `json:"authToken"`
	PairingKey string `json:"pairingKey"`
}

func loadCredentials(path string) (*credentials, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is the operator's own flag
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	var c credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("decode credentials: %w", err)
	}
	return &c, nil
}

func (c *credentials) save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ") //nolint:gosec // a test client's own credential file
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}

type rpcError struct {
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
	Code    int             `json:"code"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error %d: %s %s", e.Code, e.Message, e.Data)
}

type rpcMessage struct {
	Error  *rpcError       `json:"error,omitempty"`
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

func rpcRequest(id int, method string, params any) ([]byte, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", method, err)
	}
	return data, nil
}

// plainCall sends one plaintext request and waits for its response. Only
// the pairing methods are answered this way.
func plainCall(ctx context.Context, l *link, id int, method string, params, out any) error {
	req, err := rpcRequest(id, method, params)
	if err != nil {
		return err
	}
	if sendErr := l.send(ctx, req); sendErr != nil {
		return sendErr
	}
	raw, err := l.recv(ctx)
	if err != nil {
		return err
	}
	var resp rpcMessage
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode %s response %q: %w", method, raw, err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}

func writeLP(h io.Writer, b []byte) {
	var lp [4]byte
	binary.BigEndian.PutUint32(lp[:], uint32(len(b))) //nolint:gosec // short pairing fields
	_, _ = h.Write(lp[:])
	_, _ = h.Write(b)
}

func pairingHMAC(key []byte, role, name string, msgA, msgB []byte) []byte {
	h := hmac.New(sha256.New, key)
	for _, field := range [][]byte{
		[]byte(pairingProtoVersion), []byte(pairingCurve), []byte(role), []byte(name), msgA, msgB,
	} {
		writeLP(h, field)
	}
	return h.Sum(nil)
}

// pair runs the PAKE exchange over the link. corrupt sends a wrong
// confirmation, which is what a wrong PIN produces.
func pair(ctx context.Context, l *link, pin, name string, corrupt bool) (*credentials, error) {
	p, err := pake.InitCurve([]byte(pin), 0, pairingCurve)
	if err != nil {
		return nil, fmt.Errorf("init pake: %w", err)
	}
	msgA, err := crypto.EncodePakeMessage(p.Bytes())
	if err != nil {
		return nil, fmt.Errorf("encode pake message: %w", err)
	}

	var start struct {
		Session string `json:"session"`
		PAKE    string `json:"pake"`
	}
	startParams := map[string]string{"pake": base64.StdEncoding.EncodeToString(msgA), "name": name}
	if err = plainCall(ctx, l, 1, "pair.start", startParams, &start); err != nil {
		return nil, fmt.Errorf("pair.start: %w", err)
	}
	msgB, err := base64.StdEncoding.DecodeString(start.PAKE)
	if err != nil {
		return nil, fmt.Errorf("decode server pake message: %w", err)
	}
	internal, err := crypto.DecodePakeMessage(msgB)
	if err != nil {
		return nil, fmt.Errorf("parse server pake message: %w", err)
	}
	if err = p.Update(internal); err != nil {
		return nil, fmt.Errorf("pake update: %w", err)
	}
	sessionKey, err := p.SessionKey()
	if err != nil {
		return nil, fmt.Errorf("pake session key: %w", err)
	}

	prk, err := hkdf.Extract(sha256.New, sessionKey, slices.Concat(msgA, msgB))
	if err != nil {
		return nil, fmt.Errorf("hkdf extract: %w", err)
	}
	expand := func(info string, size int) []byte {
		key, expandErr := hkdf.Expand(sha256.New, prk, info, size)
		err = errors.Join(err, expandErr)
		return key
	}
	confirmA := expand(infoConfirmA, sha256.Size)
	confirmB := expand(infoConfirmB, sha256.Size)
	pairingKey := expand(infoPairing, crypto.PairingKeySize)
	if err != nil {
		return nil, fmt.Errorf("hkdf expand: %w", err)
	}

	confirm := pairingHMAC(confirmA, "client", name, msgA, msgB)
	if corrupt {
		confirm[0] ^= 0xff
	}
	var finish struct {
		AuthToken string `json:"authToken"`
		ClientID  string `json:"clientId"`
		Confirm   string `json:"confirm"`
	}
	finishParams := map[string]string{
		"session": start.Session, "confirm": base64.StdEncoding.EncodeToString(confirm),
	}
	if err = plainCall(ctx, l, 2, "pair.finish", finishParams, &finish); err != nil {
		return nil, fmt.Errorf("pair.finish: %w", err)
	}
	serverHMAC, err := base64.StdEncoding.DecodeString(finish.Confirm)
	if err != nil {
		return nil, fmt.Errorf("decode server confirmation: %w", err)
	}
	if !hmac.Equal(serverHMAC, pairingHMAC(confirmB, "server", name, msgA, msgB)) {
		return nil, errors.New("server confirmation does not match: wrong PIN or not the real device")
	}
	return &credentials{
		DeviceID:   l.info.DeviceID,
		AuthToken:  finish.AuthToken,
		PairingKey: base64.StdEncoding.EncodeToString(pairingKey),
	}, nil
}

// session is the encrypted session on top of a link.
type session struct {
	l        *link
	c2s      cipher.AEAD
	s2c      cipher.AEAD
	token    string
	c2sNonce []byte
	s2cNonce []byte
	aad      []byte
	salt     []byte
	sent     uint64
	received uint64
}

// newSession derives the session keys. label is the transport label bound
// into the AEAD; anything but "ble" must be refused by Core.
func newSession(l *link, creds *credentials, label string) (*session, error) {
	pairingKey, err := base64.StdEncoding.DecodeString(creds.PairingKey)
	if err != nil {
		return nil, fmt.Errorf("decode pairing key: %w", err)
	}
	salt := make([]byte, crypto.SessionSaltSize)
	if _, err = rand.Read(salt); err != nil {
		return nil, fmt.Errorf("session salt: %w", err)
	}
	keys, err := crypto.DeriveSessionKeys(pairingKey, salt)
	if err != nil {
		return nil, fmt.Errorf("derive session keys: %w", err)
	}
	c2s, err := crypto.NewAEAD(keys.C2SKey)
	if err != nil {
		return nil, fmt.Errorf("c2s cipher: %w", err)
	}
	s2c, err := crypto.NewAEAD(keys.S2CKey)
	if err != nil {
		return nil, fmt.Errorf("s2c cipher: %w", err)
	}
	return &session{
		l: l, c2s: c2s, s2c: s2c, c2sNonce: keys.C2SNonce, s2cNonce: keys.S2CNonce,
		aad: []byte(creds.AuthToken + ":" + label), salt: salt, token: creds.AuthToken,
	}, nil
}

// seal encrypts one plaintext into the wire frame for its position in the
// session. Frames are binary on this transport: the first is a short header
// (version, salt, token length, token) followed by the ciphertext, and every
// later one is the ciphertext alone.
func (s *session) seal(plaintext []byte) ([]byte, error) {
	ct, err := crypto.Encrypt(s.c2s, s.c2sNonce, s.sent, plaintext, s.aad)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	first := s.sent == 0
	s.sent++
	if !first {
		return ct, nil
	}
	if s.token == "" || len(s.token) > 255 {
		return nil, fmt.Errorf("auth token is %d bytes, want 1-255", len(s.token))
	}
	frame := make([]byte, 0, 2+len(s.salt)+len(s.token)+len(ct))
	frame = append(frame, encryptionVersion)
	frame = append(frame, s.salt...)
	frame = append(frame, byte(len(s.token))) //nolint:gosec // checked just above
	frame = append(frame, s.token...)
	return append(frame, ct...), nil
}

func (s *session) send(ctx context.Context, plaintext []byte) error {
	frame, err := s.seal(plaintext)
	if err != nil {
		return err
	}
	return s.l.send(ctx, frame)
}

// recv returns the next decrypted message from Core.
func (s *session) recv(ctx context.Context) ([]byte, error) {
	raw, err := s.l.recv(ctx)
	if err != nil {
		return nil, err
	}
	pt, err := crypto.Decrypt(s.s2c, s.s2cNonce, s.received, raw, s.aad)
	if err != nil {
		// Before a session exists Core can only answer in plaintext, as
		// it does for an encryption version it does not speak.
		var plain struct {
			Error *rpcError `json:"error"`
		}
		if json.Unmarshal(raw, &plain) == nil && plain.Error != nil {
			return nil, plain.Error
		}
		return nil, fmt.Errorf("decrypt frame %d (%s): %w", s.received, truncate(raw), err)
	}
	s.received++
	return pt, nil
}

func truncate(b []byte) string {
	const limit = 200
	if len(b) <= limit {
		return string(b)
	}
	return fmt.Sprintf("%s… (%d bytes)", b[:limit], len(b))
}
