// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// mockMQTTClient implements mqtt.Client for testing
type mockMQTTClient struct {
	connectError    error
	subscribeError  error
	publishError    error
	messageHandler  mqtt.MessageHandler
	disconnectCalls int
	connected       bool
}

func newMockMQTTClient() *mockMQTTClient {
	return &mockMQTTClient{
		connected: false,
	}
}

func (m *mockMQTTClient) IsConnected() bool {
	return m.connected
}

func (m *mockMQTTClient) IsConnectionOpen() bool {
	return m.connected
}

func (m *mockMQTTClient) Connect() mqtt.Token {
	if m.connectError != nil {
		return &mockToken{err: m.connectError}
	}
	m.connected = true
	return &mockToken{complete: true}
}

func (m *mockMQTTClient) Disconnect(_ uint) {
	m.connected = false
	m.disconnectCalls++
}

func (m *mockMQTTClient) Publish(_ string, _ byte, _ bool, _ any) mqtt.Token {
	if m.publishError != nil {
		return &mockToken{err: m.publishError}
	}
	return &mockToken{complete: true}
}

func (m *mockMQTTClient) Subscribe(_ string, _ byte, callback mqtt.MessageHandler) mqtt.Token {
	if m.subscribeError != nil {
		return &mockToken{err: m.subscribeError}
	}
	m.messageHandler = callback
	return &mockToken{complete: true}
}

func (*mockMQTTClient) SubscribeMultiple(_ map[string]byte, _ mqtt.MessageHandler) mqtt.Token {
	return &mockToken{complete: true}
}

func (*mockMQTTClient) Unsubscribe(_ ...string) mqtt.Token {
	return &mockToken{complete: true}
}

func (m *mockMQTTClient) AddRoute(_ string, callback mqtt.MessageHandler) {
	m.messageHandler = callback
}

func (*mockMQTTClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.ClientOptionsReader{}
}

// mockToken implements mqtt.Token for testing
type mockToken struct {
	err      error
	complete bool
}

func (*mockToken) Wait() bool {
	return true
}

func (t *mockToken) WaitTimeout(_ time.Duration) bool {
	return t.complete
}

func (*mockToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (t *mockToken) Error() error {
	return t.err
}
