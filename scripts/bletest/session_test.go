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
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialsRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "creds.json")
	want := &credentials{DeviceID: "dev", AuthToken: "tok", PairingKey: "a2V5"}
	require.NoError(t, want.save(path))

	// Windows has no permission bits to check: it reports every writable
	// file as 0666 whatever mode it was created with.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "credentials are private to the operator")
	}

	got, err := loadCredentials(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestCredentialsSaveTightensAnExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))
	//nolint:gosec // the loose mode is what the save has to correct
	require.NoError(t, os.Chmod(path, 0o644))

	want := &credentials{DeviceID: "dev", AuthToken: "tok", PairingKey: "a2V5"}
	require.NoError(t, want.save(path))

	got, err := loadCredentials(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left behind")

	if runtime.GOOS == "windows" {
		t.Skip("Windows does not have POSIX file modes")
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "a file that was readable by others no longer is")
}

func TestCredentialsErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := loadCredentials(filepath.Join(dir, "missing.json"))
	require.ErrorContains(t, err, "read credentials")

	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{"), 0o600))
	_, err = loadCredentials(bad)
	require.ErrorContains(t, err, "decode credentials")

	err = (&credentials{}).save(filepath.Join(dir, "no", "such", "dir", "creds.json"))
	require.ErrorContains(t, err, "write credentials")
}

func TestRPCErrorAndRequestEncoding(t *testing.T) {
	t.Parallel()
	err := &rpcError{Code: -32601, Message: "method not found", Data: json.RawMessage(`"x"`)}
	assert.Equal(t, `rpc error -32601: method not found "x"`, err.Error())

	req, encErr := rpcRequest(7, "media.search", map[string]string{"query": "mario"})
	require.NoError(t, encErr)
	assert.JSONEq(t, `{"jsonrpc":"2.0","id":7,"method":"media.search","params":{"query":"mario"}}`, string(req))

	req, encErr = rpcRequest(8, "version", nil)
	require.NoError(t, encErr)
	assert.JSONEq(t, `{"jsonrpc":"2.0","id":8,"method":"version"}`, string(req))

	_, encErr = rpcRequest(9, "bad", make(chan int))
	require.ErrorContains(t, encErr, "encode bad")
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "short", truncate([]byte("short")))
	long := truncate([]byte(strings.Repeat("a", 250)))
	assert.True(t, strings.HasPrefix(long, strings.Repeat("a", 200)+"…"))
	assert.True(t, strings.HasSuffix(long, "(250 bytes)"))
}

func TestPairAgainstCorePairingManager(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	pin, _, err := core.pairing.StartPairing("")
	require.NoError(t, err)
	l := core.dial(t)

	creds, err := pair(t.Context(), l, pin, "bletest", false)
	require.NoError(t, err)
	assert.Equal(t, fakeCoreDeviceID, creds.DeviceID)

	stored := core.client(creds.AuthToken)
	require.NotNil(t, stored, "core stored the client under the token it returned")
	key, err := base64.StdEncoding.DecodeString(creds.PairingKey)
	require.NoError(t, err)
	assert.Equal(t, stored.PairingKey, key, "both sides derived the same pairing key")
	assert.Equal(t, "bletest", stored.ClientName)
}

func TestPairWithACorruptedConfirmationIsRefused(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	pin, _, err := core.pairing.StartPairing("")
	require.NoError(t, err)
	l := core.dial(t)

	_, err = pair(t.Context(), l, pin, "bletest", true)
	require.ErrorContains(t, err, "pair.finish: rpc error -32000: wrong PIN")
}

func TestPairWithoutAPendingPIN(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	l := core.dial(t)

	_, err := pair(t.Context(), l, "123456", "bletest", false)
	require.ErrorContains(t, err, "pair.start: rpc error -32000: no pairing in progress")
}

func TestPairRejectsBadServerMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		result string
		want   string
	}{
		{
			name: "pake not base64", method: "pair.start", result: `{"session":"s","pake":"!!"}`,
			want: "decode server pake message",
		},
		{
			name: "pake malformed", method: "pair.start", result: `{"session":"s","pake":"AAAA"}`,
			want: "parse server pake message",
		},
		{
			name: "start result wrong shape", method: "pair.start", result: `"text"`,
			want: "decode pair.start result",
		},
		{
			name: "confirm not base64", method: "pair.finish", result: `{"authToken":"t","confirm":"!!"}`,
			want: "decode server confirmation",
		},
		{
			name: "confirm wrong", method: "pair.finish", result: `{"authToken":"t","confirm":"AAAA"}`,
			want: "server confirmation does not match",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			core := newFakeCore(t, &fakeCoreOptions{
				plain: func(method string) (json.RawMessage, *rpcError, bool) {
					return json.RawMessage(tt.result), nil, method == tt.method
				},
			})
			pin, _, err := core.pairing.StartPairing("")
			require.NoError(t, err)
			l := core.dial(t)

			_, err = pair(t.Context(), l, pin, "bletest", false)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestPlainCallRejectsANonJSONReply(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{onPlaintext: plainIgnore})
	l := core.dial(t)

	core.tag.Store(uint32(l.tag))
	core.send([]byte("not json"))
	var out struct{}
	err := plainCall(t.Context(), l, 1, "hello", nil, &out)
	require.ErrorContains(t, err, "decode hello response")
}

func TestPlainCallReportsSendFailure(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{rxWrite: func([]byte) error { return os.ErrClosed }})
	l := core.dial(t)

	var out struct{}
	err := plainCall(t.Context(), l, 1, "pair.start", nil, &out)
	require.ErrorIs(t, err, os.ErrClosed)
	_, err = pair(t.Context(), l, "123456", "bletest", false)
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestSessionEncryptsForCore(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	creds, err := loadCredentials(core.seedCredentials(t))
	require.NoError(t, err)
	l := core.dial(t)

	s, err := newSession(l, creds, transportLabel)
	require.NoError(t, err)
	c := &caller{s: s, timeout: testCallTimeout}
	for range 3 {
		result, callErr := c.call(t.Context(), "version", nil)
		require.NoError(t, callErr)
		assert.JSONEq(t, `{"version":"fake"}`, string(result))
	}
	assert.Equal(t, uint64(3), s.sent)
	assert.Equal(t, uint64(3), s.received)
}

func TestSessionRefusedForAnotherTransport(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	creds, err := loadCredentials(core.seedCredentials(t))
	require.NoError(t, err)
	l := core.dial(t)

	s, err := newSession(l, creds, "ws")
	require.NoError(t, err)
	require.NoError(t, s.send(t.Context(), []byte(`{"jsonrpc":"2.0","id":1,"method":"version"}`)))
	_, err = s.recv(t.Context())
	var rpcErr *rpcError
	require.ErrorAs(t, err, &rpcErr, "core's plaintext refusal surfaces as its error")
	assert.Equal(t, -32001, rpcErr.Code)
}

func TestSessionRecvRejectsUndecryptableFrames(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	creds, err := loadCredentials(core.seedCredentials(t))
	require.NoError(t, err)
	l := core.dial(t)
	s, err := newSession(l, creds, transportLabel)
	require.NoError(t, err)

	core.tag.Store(uint32(l.tag))
	core.send([]byte("random bytes"))
	_, err = s.recv(t.Context())
	require.ErrorContains(t, err, "decrypt frame 0 (random bytes)")

	l.close()
	_, err = s.recv(t.Context())
	require.Error(t, err)
}

func TestNewSessionAndSealErrors(t *testing.T) {
	t.Parallel()
	l := &link{}

	_, err := newSession(l, &credentials{PairingKey: "!!"}, transportLabel)
	require.ErrorContains(t, err, "decode pairing key")

	_, err = newSession(l, &credentials{PairingKey: base64.StdEncoding.EncodeToString([]byte("short"))},
		transportLabel)
	require.ErrorIs(t, err, crypto.ErrInvalidPairingKey)

	key := base64.StdEncoding.EncodeToString(make([]byte, crypto.PairingKeySize))
	for _, token := range []string{"", strings.Repeat("t", 256)} {
		s, sessErr := newSession(l, &credentials{PairingKey: key, AuthToken: token}, transportLabel)
		require.NoError(t, sessErr)
		require.ErrorContains(t, s.send(t.Context(), []byte("x")), "want 1-255")
	}
}

func TestSealFrameLayout(t *testing.T) {
	t.Parallel()
	key := base64.StdEncoding.EncodeToString(make([]byte, crypto.PairingKeySize))
	s, err := newSession(&link{}, &credentials{PairingKey: key, AuthToken: "tok"}, transportLabel)
	require.NoError(t, err)

	first, err := s.seal([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, byte(encryptionVersion), first[0])
	assert.Equal(t, s.salt, first[1:1+crypto.SessionSaltSize])
	tokenAt := 1 + crypto.SessionSaltSize
	assert.Equal(t, byte(3), first[tokenAt])
	assert.Equal(t, "tok", string(first[tokenAt+1:tokenAt+4]))

	second, err := s.seal([]byte("hello"))
	require.NoError(t, err)
	assert.Len(t, second, len(first)-tokenAt-4, "later frames are the ciphertext alone")
}
