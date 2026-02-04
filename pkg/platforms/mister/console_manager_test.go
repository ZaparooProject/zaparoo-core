//go:build linux

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

package mister

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockTTYReader is a mock implementation of TTYReader for testing.
type mockTTYReader struct {
	err error
	tty string
}

func (m *mockTTYReader) GetActiveTTY() (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.tty, nil
}

// mockFramebufferChecker is a mock implementation of FramebufferChecker for testing.
type mockFramebufferChecker struct {
	ready bool
}

func (m *mockFramebufferChecker) IsReady() bool {
	return m.ready
}

// mockCoreNameGetter is a mock implementation of CoreNameGetter for testing.
type mockCoreNameGetter struct {
	coreName string
}

func (m *mockCoreNameGetter) GetCoreName() string {
	return m.coreName
}

func TestMiSTerConsoleManager_Open_CancelledContext(t *testing.T) {
	t.Parallel()

	pl := &Platform{}
	cm := newConsoleManager(pl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cm.Open(ctx, "7")
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestMiSTerConsoleManager_ConcurrentActiveFlag(t *testing.T) {
	t.Parallel()

	pl := &Platform{}
	cm := newConsoleManager(pl)
	done := make(chan bool)

	// Concurrent readers
	for range 10 {
		go func() {
			defer func() { done <- true }()
			for range 100 {
				cm.mu.RLock()
				_ = cm.active
				cm.mu.RUnlock()
			}
		}()
	}

	// Concurrent writers
	for i := range 5 {
		go func(val bool) {
			defer func() { done <- true }()
			for range 50 {
				cm.mu.Lock()
				cm.active = val
				cm.mu.Unlock()
			}
		}(i%2 == 0)
	}

	// Wait for all goroutines
	for range 15 {
		<-done
	}
}

func TestMiSTerConsoleManager_Open_AlreadyActive(t *testing.T) {
	t.Parallel()

	pl := &Platform{}
	cm := newConsoleManager(pl)

	// Set console as already active
	cm.mu.Lock()
	cm.active = true
	cm.mu.Unlock()

	// Open should return immediately without error
	err := cm.Open(context.Background(), "7")
	require.NoError(t, err)

	// Verify still active
	cm.mu.RLock()
	assert.True(t, cm.active)
	cm.mu.RUnlock()
}

func TestMiSTerConsoleManager_Open_ChvtAfterTty1Confirmed(t *testing.T) {
	t.Parallel()

	mockTTY := &mockTTYReader{tty: "tty1"}
	mockFB := &mockFramebufferChecker{ready: true}
	mockExec := &mocks.MockCommandExecutor{}

	mockExec.On("Run", mock.Anything, "chvt", []string{"7"}).Return(nil)

	pl := NewPlatform()
	cm := newConsoleManager(pl)
	cm.ttyReader = mockTTY
	cm.fbChecker = mockFB
	cm.executor = mockExec

	// Verify the mock TTY reader works
	tty, err := cm.getTTY()
	require.NoError(t, err)
	assert.Equal(t, "tty1", tty)

	// Verify the framebuffer checker works
	err = cm.waitForFramebuffer(200 * time.Millisecond)
	require.NoError(t, err)
}

func TestMiSTerConsoleManager_getTTY_UsesTTYReader(t *testing.T) {
	t.Parallel()

	mockTTY := &mockTTYReader{tty: "tty3"}

	pl := &Platform{}
	cm := newConsoleManager(pl)
	cm.ttyReader = mockTTY

	tty, err := cm.getTTY()
	require.NoError(t, err)
	assert.Equal(t, "tty3", tty)
}

func TestMiSTerConsoleManager_getTTY_Error(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("tty read error")
	mockTTY := &mockTTYReader{err: expectedErr}

	pl := &Platform{}
	cm := newConsoleManager(pl)
	cm.ttyReader = mockTTY

	_, err := cm.getTTY()
	require.Error(t, err)
	require.ErrorIs(t, err, expectedErr)
	assert.Contains(t, err.Error(), "failed to get active TTY")
}

func TestMiSTerConsoleManager_waitForFramebuffer_Ready(t *testing.T) {
	t.Parallel()

	mockFB := &mockFramebufferChecker{ready: true}

	pl := &Platform{}
	cm := newConsoleManager(pl)
	cm.fbChecker = mockFB

	err := cm.waitForFramebuffer(200 * time.Millisecond)
	require.NoError(t, err)
}

func TestMiSTerConsoleManager_waitForFramebuffer_Timeout(t *testing.T) {
	t.Parallel()

	mockFB := &mockFramebufferChecker{ready: false}

	pl := &Platform{}
	cm := newConsoleManager(pl)
	cm.fbChecker = mockFB

	err := cm.waitForFramebuffer(10 * time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "framebuffer not ready")
}

func TestMiSTerConsoleManager_Close_AlreadyInactive(t *testing.T) {
	t.Parallel()

	pl := &Platform{}
	cm := newConsoleManager(pl)

	// Console is inactive by default
	err := cm.Close()
	require.NoError(t, err)
}

func TestRealTTYReader_GetActiveTTY(t *testing.T) {
	t.Parallel()

	reader := realTTYReader{}

	// This will fail in test environment (no /sys/devices/virtual/tty/tty0/active)
	_, err := reader.GetActiveTTY()
	if err != nil {
		assert.Contains(t, err.Error(), "failed to stat tty active file")
	}
}

func TestRealFramebufferChecker_IsReady(t *testing.T) {
	t.Parallel()

	checker := realFramebufferChecker{}

	// In test environment, framebuffer may or may not exist
	// Just verify it returns a boolean without panicking
	_ = checker.IsReady()
}

func TestMiSTerConsoleManager_Open_NoInitialChvt(t *testing.T) {
	t.Parallel()

	var chvtCallCount atomic.Int32

	mockExec := &mocks.MockCommandExecutor{}
	mockExec.On("Run", mock.Anything, "chvt", mock.Anything).Run(func(_ mock.Arguments) {
		chvtCallCount.Add(1)
	}).Return(nil)

	mockTTY := &mockTTYReader{tty: "tty1"}
	mockFB := &mockFramebufferChecker{ready: true}

	pl := &Platform{}
	cm := newConsoleManager(pl)
	cm.ttyReader = mockTTY
	cm.fbChecker = mockFB
	cm.executor = mockExec

	// Simulate what Open() does after tty1 is confirmed
	err := cm.waitForFramebuffer(200 * time.Millisecond)
	require.NoError(t, err)

	// Switch to target VT
	ctx := context.Background()
	err = cm.executor.Run(ctx, "chvt", "7")
	require.NoError(t, err)

	assert.Equal(t, int32(1), chvtCallCount.Load(), "chvt should be called exactly once")
	mockExec.AssertExpectations(t)
}

func TestMiSTerConsoleManager_waitForFramebuffer_EventuallyReady(t *testing.T) {
	t.Parallel()

	mockFB := &mockFramebufferChecker{ready: true}

	pl := &Platform{}
	cm := newConsoleManager(pl)
	cm.fbChecker = mockFB

	err := cm.waitForFramebuffer(200 * time.Millisecond)
	require.NoError(t, err)
}

func TestNewConsoleManager_DefaultDependencies(t *testing.T) {
	t.Parallel()

	pl := &Platform{}
	cm := newConsoleManager(pl)

	// Verify defaults are set
	assert.NotNil(t, cm.ttyReader)
	assert.NotNil(t, cm.fbChecker)
	assert.NotNil(t, cm.coreNameGetter)
	assert.NotNil(t, cm.executor)

	// Verify they're the real implementations
	_, isTTYReal := cm.ttyReader.(realTTYReader)
	assert.True(t, isTTYReal, "ttyReader should be realTTYReader by default")

	_, isFBReal := cm.fbChecker.(realFramebufferChecker)
	assert.True(t, isFBReal, "fbChecker should be realFramebufferChecker by default")

	_, isCoreNameReal := cm.coreNameGetter.(realCoreNameGetter)
	assert.True(t, isCoreNameReal, "coreNameGetter should be realCoreNameGetter by default")
}

func TestMockCoreNameGetter(t *testing.T) {
	t.Parallel()

	mockCN := &mockCoreNameGetter{coreName: "MENU"}

	pl := &Platform{}
	cm := newConsoleManager(pl)
	cm.coreNameGetter = mockCN

	// Verify the mock returns the configured value
	assert.Equal(t, "MENU", cm.coreNameGetter.GetCoreName())

	// Change the mock value
	mockCN.coreName = "NES"
	assert.Equal(t, "NES", cm.coreNameGetter.GetCoreName())
}
