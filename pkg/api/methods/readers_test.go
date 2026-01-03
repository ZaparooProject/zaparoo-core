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

package methods_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createWriteCapableReader(readerID string) *mocks.MockReader {
	m := mocks.NewMockReader()
	m.On("ReaderID").Maybe().Return(readerID)
	m.On("Capabilities").Return([]readers.Capability{readers.CapabilityWrite})
	m.On("Connected").Maybe().Return(true)
	return m
}

func TestHandleReaderWrite(t *testing.T) {
	t.Parallel()

	t.Run("invalid JSON params", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{invalid json}`)

		_, err := methods.HandleReaderWrite(params, nil, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid params")
	})

	t.Run("missing required text field", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{}`)

		_, err := methods.HandleReaderWrite(params, nil, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid params")
	})

	t.Run("no readers available", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test"}`)

		_, err := methods.HandleReaderWrite(params, []readers.Reader{}, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to select writer")
	})

	t.Run("strict mode - reader not found", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test", "readerId": "nonexistent"}`)
		m := createWriteCapableReader("reader-1")
		rs := []readers.Reader{m}

		_, err := methods.HandleReaderWrite(params, rs, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to select writer")
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("strict mode - reader not connected", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test", "readerId": "reader-1"}`)
		m := mocks.NewMockReader()
		m.On("ReaderID").Return("reader-1")
		m.On("Connected").Return(false)
		rs := []readers.Reader{m}

		_, err := methods.HandleReaderWrite(params, rs, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to select writer")
		assert.Contains(t, err.Error(), "not connected")
	})

	t.Run("strict mode - reader lacks write capability", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test", "readerId": "reader-1"}`)
		m := mocks.NewMockReader()
		m.On("ReaderID").Return("reader-1")
		m.On("Connected").Return(true)
		m.On("Capabilities").Return([]readers.Capability{readers.CapabilityDisplay})
		rs := []readers.Reader{m}

		_, err := methods.HandleReaderWrite(params, rs, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to select writer")
		assert.Contains(t, err.Error(), "write capability")
	})

	t.Run("strict mode - success", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test data", "readerId": "reader-1"}`)
		m := createWriteCapableReader("reader-1")
		returnedToken := &tokens.Token{Data: "test data"}
		m.On("Write", "test data").Return(returnedToken, nil)
		rs := []readers.Reader{m}

		var wroteToken *tokens.Token
		setWroteToken := func(t *tokens.Token) { wroteToken = t }

		result, err := methods.HandleReaderWrite(params, rs, nil, setWroteToken)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
		assert.Equal(t, returnedToken, wroteToken)
		m.AssertExpectations(t)
	})

	t.Run("preferred mode - uses last scanned reader", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test data"}`)
		m1 := createWriteCapableReader("reader-1")
		m2 := createWriteCapableReader("reader-2")
		returnedToken := &tokens.Token{Data: "test data"}
		m2.On("Write", "test data").Return(returnedToken, nil)
		rs := []readers.Reader{m1, m2}

		lastScanned := &tokens.Token{
			ReaderID: "reader-2",
			ScanTime: time.Now(),
		}

		var wroteToken *tokens.Token
		setWroteToken := func(t *tokens.Token) { wroteToken = t }

		result, err := methods.HandleReaderWrite(params, rs, lastScanned, setWroteToken)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
		assert.Equal(t, returnedToken, wroteToken)
		m2.AssertExpectations(t)
	})

	t.Run("preferred mode - falls back to first available", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test data"}`)
		m1 := createWriteCapableReader("reader-1")
		m2 := createWriteCapableReader("reader-2")
		returnedToken := &tokens.Token{Data: "test data"}
		m1.On("Write", "test data").Return(returnedToken, nil)
		rs := []readers.Reader{m1, m2}

		var wroteToken *tokens.Token
		setWroteToken := func(t *tokens.Token) { wroteToken = t }

		result, err := methods.HandleReaderWrite(params, rs, nil, setWroteToken)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
		assert.Equal(t, returnedToken, wroteToken)
		m1.AssertExpectations(t)
	})

	t.Run("preferred mode - ignores last scanned with zero time", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test data"}`)
		m1 := createWriteCapableReader("reader-1")
		m2 := createWriteCapableReader("reader-2")
		returnedToken := &tokens.Token{Data: "test data"}
		m1.On("Write", "test data").Return(returnedToken, nil)
		rs := []readers.Reader{m1, m2}

		lastScanned := &tokens.Token{
			ReaderID: "reader-2",
			// Zero ScanTime - should be ignored
		}

		var wroteToken *tokens.Token
		setWroteToken := func(t *tokens.Token) { wroteToken = t }

		result, err := methods.HandleReaderWrite(params, rs, lastScanned, setWroteToken)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
		assert.Equal(t, returnedToken, wroteToken)
		m1.AssertExpectations(t)
	})

	t.Run("preferred mode - ignores last scanned with empty reader ID", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test data"}`)
		m1 := createWriteCapableReader("reader-1")
		returnedToken := &tokens.Token{Data: "test data"}
		m1.On("Write", "test data").Return(returnedToken, nil)
		rs := []readers.Reader{m1}

		lastScanned := &tokens.Token{
			ReaderID: "",
			ScanTime: time.Now(),
		}

		var wroteToken *tokens.Token
		setWroteToken := func(t *tokens.Token) { wroteToken = t }

		result, err := methods.HandleReaderWrite(params, rs, lastScanned, setWroteToken)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
		assert.Equal(t, returnedToken, wroteToken)
		m1.AssertExpectations(t)
	})

	t.Run("write error from reader", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test data"}`)
		m := createWriteCapableReader("reader-1")
		m.On("Write", "test data").Return(nil, errors.New("hardware failure"))
		rs := []readers.Reader{m}

		_, err := methods.HandleReaderWrite(params, rs, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "error writing to reader")
		m.AssertExpectations(t)
	})

	t.Run("write cancelled - logs at debug level", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test data"}`)
		m := createWriteCapableReader("reader-1")
		m.On("Write", "test data").Return(nil, context.Canceled)
		rs := []readers.Reader{m}

		_, err := methods.HandleReaderWrite(params, rs, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "error writing to reader")
		m.AssertExpectations(t)
	})

	t.Run("write returns nil token - does not call setWroteToken", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"text": "test data"}`)
		m := createWriteCapableReader("reader-1")
		m.On("Write", "test data").Return(nil, nil)
		rs := []readers.Reader{m}

		setCalled := false
		setWroteToken := func(_ *tokens.Token) { setCalled = true }

		result, err := methods.HandleReaderWrite(params, rs, nil, setWroteToken)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
		assert.False(t, setCalled, "setWroteToken should not be called when token is nil")
		m.AssertExpectations(t)
	})
}

func TestHandleReaderWriteCancel(t *testing.T) {
	t.Parallel()

	t.Run("invalid JSON params", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{invalid json}`)

		_, err := methods.HandleReaderWriteCancel(params, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid params")
	})

	t.Run("strict mode - reader not found", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"readerId": "nonexistent"}`)
		m := createWriteCapableReader("reader-1")
		rs := []readers.Reader{m}

		_, err := methods.HandleReaderWriteCancel(params, rs)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to select reader")
	})

	t.Run("strict mode - reader not connected", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"readerId": "reader-1"}`)
		m := mocks.NewMockReader()
		m.On("ReaderID").Return("reader-1")
		m.On("Connected").Return(false)
		rs := []readers.Reader{m}

		_, err := methods.HandleReaderWriteCancel(params, rs)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not connected")
	})

	t.Run("strict mode - reader lacks write capability", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"readerId": "reader-1"}`)
		m := mocks.NewMockReader()
		m.On("ReaderID").Return("reader-1")
		m.On("Connected").Return(true)
		m.On("Capabilities").Return([]readers.Capability{readers.CapabilityDisplay})
		rs := []readers.Reader{m}

		_, err := methods.HandleReaderWriteCancel(params, rs)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "write capability")
	})

	t.Run("strict mode - cancels specific reader", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"readerId": "reader-1"}`)
		m := createWriteCapableReader("reader-1")
		m.On("CancelWrite").Return()
		rs := []readers.Reader{m}

		result, err := methods.HandleReaderWriteCancel(params, rs)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
		m.AssertCalled(t, "CancelWrite")
	})

	t.Run("broadcast mode - cancels all write-capable readers", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{}`)
		m1 := createWriteCapableReader("reader-1")
		m1.On("CancelWrite").Return()
		m2 := createWriteCapableReader("reader-2")
		m2.On("CancelWrite").Return()

		// Display-only reader should not have CancelWrite called
		m3 := mocks.NewMockReader()
		m3.On("Capabilities").Return([]readers.Capability{readers.CapabilityDisplay})

		rs := []readers.Reader{m1, m2, m3}

		result, err := methods.HandleReaderWriteCancel(params, rs)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
		m1.AssertCalled(t, "CancelWrite")
		m2.AssertCalled(t, "CancelWrite")
		m3.AssertNotCalled(t, "CancelWrite")
	})

	t.Run("broadcast mode - no readers is not an error", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{}`)

		result, err := methods.HandleReaderWriteCancel(params, []readers.Reader{})

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
	})

	t.Run("broadcast mode - no write-capable readers is not an error", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{}`)
		m := mocks.NewMockReader()
		m.On("Capabilities").Return([]readers.Capability{readers.CapabilityDisplay})
		rs := []readers.Reader{m}

		result, err := methods.HandleReaderWriteCancel(params, rs)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
	})

	t.Run("broadcast mode - empty readerId treated as broadcast", func(t *testing.T) {
		t.Parallel()

		params := json.RawMessage(`{"readerId": ""}`)
		m := createWriteCapableReader("reader-1")
		m.On("CancelWrite").Return()
		rs := []readers.Reader{m}

		result, err := methods.HandleReaderWriteCancel(params, rs)

		require.NoError(t, err)
		assert.Equal(t, methods.NoContent{}, result)
		m.AssertCalled(t, "CancelWrite")
	})
}

func TestHandleReaders(t *testing.T) {
	t.Parallel()

	t.Run("empty readers list", func(t *testing.T) {
		t.Parallel()

		result, err := methods.HandleReaders([]readers.Reader{})

		require.NoError(t, err)
		resp, ok := result.(models.ReadersResponse)
		require.True(t, ok)
		assert.Empty(t, resp.Readers)
	})

	t.Run("single reader", func(t *testing.T) {
		t.Parallel()

		m := mocks.NewMockReader()
		m.On("Path").Return("/dev/ttyUSB0")
		m.On("ReaderID").Return("pn532-a1b2c3d4e5f67890")
		m.On("Metadata").Return(readers.DriverMetadata{ID: "pn532"})
		m.On("Info").Return("PN532 on /dev/ttyUSB0")
		m.On("Connected").Return(true)
		m.On("Capabilities").Return([]readers.Capability{
			readers.CapabilityWrite,
			readers.CapabilityDisplay,
		})
		rs := []readers.Reader{m}

		result, err := methods.HandleReaders(rs)

		require.NoError(t, err)
		resp, ok := result.(models.ReadersResponse)
		require.True(t, ok)
		require.Len(t, resp.Readers, 1)

		info := resp.Readers[0]
		assert.Equal(t, "/dev/ttyUSB0", info.ID)
		assert.Equal(t, "pn532-a1b2c3d4e5f67890", info.ReaderID)
		assert.Equal(t, "pn532", info.Driver)
		assert.Equal(t, "PN532 on /dev/ttyUSB0", info.Info)
		assert.True(t, info.Connected)
		assert.ElementsMatch(t, []string{"write", "display"}, info.Capabilities)
	})

	t.Run("multiple readers", func(t *testing.T) {
		t.Parallel()

		m1 := mocks.NewMockReader()
		m1.On("Path").Return("/dev/ttyUSB0")
		m1.On("ReaderID").Return("pn532-a1b2c3d4e5f67890")
		m1.On("Metadata").Return(readers.DriverMetadata{ID: "pn532"})
		m1.On("Info").Return("PN532 on /dev/ttyUSB0")
		m1.On("Connected").Return(true)
		m1.On("Capabilities").Return([]readers.Capability{readers.CapabilityWrite})

		m2 := mocks.NewMockReader()
		m2.On("Path").Return("localhost:1883/zaparoo/tokens")
		m2.On("ReaderID").Return("mqtt-f8e7d6c5b4a39281")
		m2.On("Metadata").Return(readers.DriverMetadata{ID: "mqtt"})
		m2.On("Info").Return("MQTT localhost:1883/zaparoo/tokens")
		m2.On("Connected").Return(false)
		m2.On("Capabilities").Return([]readers.Capability{})

		rs := []readers.Reader{m1, m2}

		result, err := methods.HandleReaders(rs)

		require.NoError(t, err)
		resp, ok := result.(models.ReadersResponse)
		require.True(t, ok)
		require.Len(t, resp.Readers, 2)

		assert.Equal(t, "pn532-a1b2c3d4e5f67890", resp.Readers[0].ReaderID)
		assert.Equal(t, "mqtt-f8e7d6c5b4a39281", resp.Readers[1].ReaderID)
	})

	t.Run("skips nil readers", func(t *testing.T) {
		t.Parallel()

		m := mocks.NewMockReader()
		m.On("Path").Return("/dev/ttyUSB0")
		m.On("ReaderID").Return("pn532-a1b2c3d4e5f67890")
		m.On("Metadata").Return(readers.DriverMetadata{ID: "pn532"})
		m.On("Info").Return("PN532 on /dev/ttyUSB0")
		m.On("Connected").Return(true)
		m.On("Capabilities").Return([]readers.Capability{})

		rs := []readers.Reader{nil, m, nil}

		result, err := methods.HandleReaders(rs)

		require.NoError(t, err)
		resp, ok := result.(models.ReadersResponse)
		require.True(t, ok)
		require.Len(t, resp.Readers, 1)
		assert.Equal(t, "pn532-a1b2c3d4e5f67890", resp.Readers[0].ReaderID)
	})

	t.Run("reader with no capabilities", func(t *testing.T) {
		t.Parallel()

		m := mocks.NewMockReader()
		m.On("Path").Return("/tmp/zaparoo/tokens.txt")
		m.On("ReaderID").Return("file-1029384756abcdef")
		m.On("Metadata").Return(readers.DriverMetadata{ID: "file"})
		m.On("Info").Return("/tmp/zaparoo/tokens.txt")
		m.On("Connected").Return(true)
		m.On("Capabilities").Return([]readers.Capability{})

		rs := []readers.Reader{m}

		result, err := methods.HandleReaders(rs)

		require.NoError(t, err)
		resp, ok := result.(models.ReadersResponse)
		require.True(t, ok)
		require.Len(t, resp.Readers, 1)
		assert.Empty(t, resp.Readers[0].Capabilities)
	})
}
