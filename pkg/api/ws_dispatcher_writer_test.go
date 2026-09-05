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
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSessionWriter stands in for a non-WebSocket transport: it records what
// the dispatcher writes and whether the dispatcher asked to close it.
type fakeSessionWriter struct {
	writeErr  error
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeSessionWriter() *fakeSessionWriter {
	return &fakeSessionWriter{
		writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (f *fakeSessionWriter) Write(p []byte) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes <- append([]byte(nil), p...)
	return nil
}

func (f *fakeSessionWriter) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *fakeSessionWriter) waitWrite(t *testing.T) []byte {
	t.Helper()
	select {
	case data := <-f.writes:
		return data
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not write a response")
		return nil
	}
}

func (f *fakeSessionWriter) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-f.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not close the session")
	}
}

func TestSessionDispatcherWritesThroughSessionWriter(t *testing.T) {
	t.Parallel()

	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod("test.echo", func(requests.RequestEnv) (any, error) {
		return map[string]string{"kind": "echo"}, nil
	}, true))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := newFakeSessionWriter()
	d := newSessionDispatcher(ctx, writer, nil)
	defer d.close()

	env := &requests.RequestEnv{Context: ctx, IsLocal: true}
	require.NoError(t, enqueueWSRequest(
		d, &methodMap, env,
		[]byte(`{"jsonrpc":"2.0","method":"test.echo","id":"writer-id"}`),
		nil, nil,
	))

	var resp models.ResponseObject
	require.NoError(t, json.Unmarshal(writer.waitWrite(t), &resp))
	assert.Equal(t, models.NewStringID("writer-id"), resp.ID)
	assert.Equal(t, map[string]any{"kind": "echo"}, resp.Result)
	assert.Nil(t, resp.Error)

	select {
	case <-writer.closed:
		t.Fatal("successful write must not close the session")
	default:
	}
}

func TestSessionDispatcherClosesSessionOnWriteError(t *testing.T) {
	t.Parallel()

	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod("test.echo", func(requests.RequestEnv) (any, error) {
		return map[string]string{"kind": "echo"}, nil
	}, true))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := newFakeSessionWriter()
	writer.writeErr = errors.New("transport gone")
	d := newSessionDispatcher(ctx, writer, nil)
	defer d.close()

	env := &requests.RequestEnv{Context: ctx, IsLocal: true}
	require.NoError(t, enqueueWSRequest(
		d, &methodMap, env,
		[]byte(`{"jsonrpc":"2.0","method":"test.echo","id":"writer-id"}`),
		nil, nil,
	))

	writer.waitClosed(t)
}

func TestSessionDispatcherEncryptsResponseForSessionWriter(t *testing.T) {
	t.Parallel()

	cs, clientSecrets := establishTestEncryptionSession(t)

	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod("test.echo", func(requests.RequestEnv) (any, error) {
		return map[string]string{"kind": "echo"}, nil
	}, true))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := newFakeSessionWriter()
	d := newSessionDispatcher(ctx, writer, nil)
	defer d.close()

	env := &requests.RequestEnv{Context: ctx, IsLocal: true}
	require.NoError(t, enqueueWSRequest(
		d, &methodMap, env,
		[]byte(`{"jsonrpc":"2.0","method":"test.echo","id":"writer-id"}`),
		cs, nil,
	))

	var frame struct {
		Ciphertext string `json:"e"`
	}
	require.NoError(t, json.Unmarshal(writer.waitWrite(t), &frame))
	ciphertext, err := base64.StdEncoding.DecodeString(frame.Ciphertext)
	require.NoError(t, err)
	plaintext, err := crypto.Decrypt(
		clientSecrets.s2cGCM, clientSecrets.s2cNonce, 0, ciphertext, clientSecrets.aad,
	)
	require.NoError(t, err)

	var resp models.ResponseObject
	require.NoError(t, json.Unmarshal(plaintext, &resp))
	assert.Equal(t, models.NewStringID("writer-id"), resp.ID)
	assert.Equal(t, map[string]any{"kind": "echo"}, resp.Result)
}
