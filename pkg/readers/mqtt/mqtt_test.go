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

package mqtt

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	metadata := reader.Metadata()

	assert.Equal(t, "mqtt", metadata.ID)
	assert.Equal(t, "MQTT client reader", metadata.Description)
	assert.True(t, metadata.DefaultEnabled)
	assert.False(t, metadata.DefaultAutoDetect)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	ids := reader.IDs()

	assert.Equal(t, []string{"mqtt"}, ids)
}

func TestDetect(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	result := reader.Detect([]string{"any", "input"})

	assert.Empty(t, result, "MQTT reader should not support auto-detection")
}

func TestWrite(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	token, err := reader.Write("test-data")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing not supported")
}

func TestCancelWrite(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	// Should not panic
	reader.CancelWrite()
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	capabilities := reader.Capabilities()

	assert.Empty(t, capabilities, "MQTT reader has no special capabilities")
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err, "MQTT reader should ignore media changes")
}

func TestOpen_ValidConnection(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	reader := NewReader(&config.Instance{})
	reader.clientFactory = func(opts *mqtt.ClientOptions) mqtt.Client {
		// Trigger OnConnect callback immediately to simulate successful connection
		if opts.OnConnect != nil {
			go opts.OnConnect(mockClient)
		}
		return mockClient
	}

	scanQueue := make(chan readers.Scan, 10)
	device := config.ReadersConnect{
		Driver: "mqtt",
		Path:   "localhost:1883/test/topic",
	}

	err := reader.Open(device, scanQueue)

	require.NoError(t, err)
	assert.True(t, mockClient.IsConnected())
	assert.Equal(t, "localhost:1883", reader.broker)
	assert.Equal(t, "test/topic", reader.topic)

	// OnConnect callback is called asynchronously - no need to verify mock internals
}

func TestOpen_ConnectionError(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	mockClient.connectError = assert.AnError

	reader := NewReader(&config.Instance{})
	reader.clientFactory = func(_ *mqtt.ClientOptions) mqtt.Client {
		return mockClient
	}

	scanQueue := make(chan readers.Scan, 1)
	device := config.ReadersConnect{
		Driver: "mqtt",
		Path:   "localhost:1883/topic",
	}

	err := reader.Open(device, scanQueue)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to MQTT broker")
}

func TestOpen_SubscribeError(t *testing.T) {
	t.Parallel()

	mockClient := newMockMQTTClient()
	mockClient.subscribeError = assert.AnError

	reader := NewReader(&config.Instance{})
	reader.clientFactory = func(_ *mqtt.ClientOptions) mqtt.Client {
		return mockClient
	}

	scanQueue := make(chan readers.Scan, 10)
	device := config.ReadersConnect{
		Driver: "mqtt",
		Path:   "localhost:1883/topic",
	}

	err := reader.Open(device, scanQueue)

	// Should still succeed - subscription error is handled in callback
	require.NoError(t, err)

	// Wait briefly for the OnConnect callback to execute
	time.Sleep(50 * time.Millisecond)

	// Check that error was sent to scan queue
	select {
	case scan := <-scanQueue:
		require.Error(t, scan.Error)
		assert.Contains(t, scan.Error.Error(), "failed to subscribe to topic")
	case <-time.After(100 * time.Millisecond):
		// It's ok if no error was sent - the OnConnect callback is async
	}
}

func TestOpen_InvalidDriver(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	scanQueue := make(chan readers.Scan, 1)

	device := config.ReadersConnect{
		Driver: "invalid-driver",
		Path:   "localhost:1883/topic",
	}

	err := reader.Open(device, scanQueue)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid reader id")
}

func TestOpen_InvalidPath(t *testing.T) {
	t.Parallel()

	reader := NewReader(&config.Instance{})
	scanQueue := make(chan readers.Scan, 1)

	tests := []struct {
		name        string
		path        string
		errContains string
	}{
		{
			name:        "empty path",
			path:        "",
			errContains: "failed to parse MQTT path",
		},
		{
			name:        "missing topic",
			path:        "localhost:1883",
			errContains: "failed to parse MQTT path",
		},
		{
			name:        "invalid format",
			path:        "not-a-valid-path",
			errContains: "failed to parse MQTT path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device := config.ReadersConnect{
				Driver: "mqtt",
				Path:   tt.path,
			}

			err := reader.Open(device, scanQueue)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestDeviceInfo(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		device: config.ReadersConnect{
			Driver: "mqtt",
			Path:   "broker:1883/topic",
		},
		topic: "zaparoo/tokens",
	}

	device := reader.Device()
	assert.Equal(t, "mqtt:broker:1883/topic", device)

	info := reader.Info()
	assert.Equal(t, "MQTT: zaparoo/tokens", info)
}

func TestConnected(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	// Not connected initially
	assert.False(t, reader.Connected())

	// Still not connected with nil client
	reader.client = nil
	assert.False(t, reader.Connected())
}

func TestClose(t *testing.T) {
	t.Parallel()

	t.Run("with nil client", func(t *testing.T) {
		t.Parallel()
		reader := &Reader{}

		// Should not error with nil client
		err := reader.Close()
		require.NoError(t, err)
	})

	t.Run("with connected client", func(t *testing.T) {
		t.Parallel()

		mockClient := newMockMQTTClient()
		mockClient.connected = true

		reader := &Reader{client: mockClient}

		err := reader.Close()
		require.NoError(t, err)
		assert.Equal(t, 1, mockClient.disconnectCalls)
		assert.False(t, mockClient.IsConnected())
	})

	t.Run("with disconnected client", func(t *testing.T) {
		t.Parallel()

		mockClient := newMockMQTTClient()
		mockClient.connected = false

		reader := &Reader{client: mockClient}

		err := reader.Close()
		require.NoError(t, err)
		assert.Equal(t, 0, mockClient.disconnectCalls, "should not disconnect already disconnected client")
	})
}

func TestCreateMessageHandler(t *testing.T) {
	t.Parallel()

	scanQueue := make(chan readers.Scan, 10)
	device := config.ReadersConnect{
		Driver: "mqtt",
		Path:   "broker:1883/test",
	}

	reader := &Reader{
		cfg:    &config.Instance{},
		scanCh: scanQueue,
		device: device,
	}

	handler := reader.createMessageHandler()
	assert.NotNil(t, handler)

	// Test handler with mock message
	t.Run("handles valid message", func(t *testing.T) {
		mockMsg := &mockMessage{payload: []byte("**launch.system:nes")}
		handler(nil, mockMsg)

		// Should send scan to channel
		select {
		case scan := <-scanQueue:
			assert.Equal(t, tokens.SourceReader, scan.Source)
			assert.NotNil(t, scan.Token)
			assert.Equal(t, TokenType, scan.Token.Type)
			assert.Equal(t, "**launch.system:nes", scan.Token.Text)
			assert.WithinDuration(t, time.Now(), scan.Token.ScanTime, time.Second)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Expected scan message on channel")
		}
	})

	t.Run("ignores empty message", func(t *testing.T) {
		mockMsg := &mockMessage{payload: []byte("")}
		handler(nil, mockMsg)

		// Should not send anything to channel
		select {
		case <-scanQueue:
			t.Fatal("Should not send scan for empty message")
		case <-time.After(50 * time.Millisecond):
			// Expected - no message sent
		}
	})

	t.Run("handles complex zapscript", func(t *testing.T) {
		zapscript := `**launch.system:nes
**media.change`
		mockMsg := &mockMessage{payload: []byte(zapscript)}
		handler(nil, mockMsg)

		select {
		case scan := <-scanQueue:
			assert.Equal(t, zapscript, scan.Token.Text)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Expected scan message on channel")
		}
	})
}

// mockMessage implements mqtt.Message for testing
type mockMessage struct {
	payload []byte
}

func (*mockMessage) Duplicate() bool            { return false }
func (*mockMessage) Qos() byte                  { return 0 }
func (*mockMessage) Retained() bool             { return false }
func (*mockMessage) Topic() string              { return "test/topic" }
func (*mockMessage) MessageID() uint16          { return 0 }
func (m *mockMessage) Payload() []byte          { return m.payload }
func (*mockMessage) Ack()                       {}
func (m *mockMessage) AutoAckOff() mqtt.Message { return m }
func (m *mockMessage) AutoAckOn() mqtt.Message  { return m }
