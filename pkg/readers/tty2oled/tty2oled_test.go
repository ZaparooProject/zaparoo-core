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

package tty2oled

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: t.TempDir(),
	})

	reader := NewReader(cfg, mockPlatform)
	defer func() { _ = reader.Close() }()

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
	assert.NotNil(t, reader.stateManager)
	assert.NotNil(t, reader.pictureManager)
	assert.NotNil(t, reader.operationQueue)
	assert.Equal(t, StateDisconnected, reader.stateManager.GetState())
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	metadata := reader.Metadata()

	assert.Equal(t, "tty2oled", metadata.ID)
	assert.Equal(t, "TTY2OLED serial display device", metadata.Description)
	assert.False(t, metadata.DefaultEnabled)
	assert.True(t, metadata.DefaultAutoDetect)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	ids := reader.IDs()

	require.Len(t, ids, 1)
	assert.Equal(t, "tty2oled", ids[0])
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	capabilities := reader.Capabilities()

	require.Len(t, capabilities, 1)
	assert.Equal(t, readers.CapabilityDisplay, capabilities[0])
}

func TestWrite_NotSupported(t *testing.T) {
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

func TestDevice(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		path: "/dev/ttyUSB0",
	}

	assert.Equal(t, "/dev/ttyUSB0", reader.Device())
}

func TestInfo_NotConnected(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		connected: false,
	}

	assert.Equal(t, "tty2oled (disconnected)", reader.Info())
}

func TestInfo_Connected(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		path:      "/dev/ttyUSB0",
		connected: true,
	}

	assert.Equal(t, "tty2oled (/dev/ttyUSB0)", reader.Info())
}

func TestConnected_ReturnsFalseWhenDisconnected(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		stateManager: NewStateManager(),
	}

	assert.False(t, reader.Connected())
}

func TestConnected_ReturnsTrueWhenConnected(t *testing.T) {
	t.Parallel()

	sm := NewStateManager()
	// Force state to Connected without going through full connection process
	sm.ForceState(StateConnected)

	reader := &Reader{
		stateManager: sm,
	}

	assert.True(t, reader.Connected())
}

func TestStateManager_InitialState(t *testing.T) {
	t.Parallel()

	sm := NewStateManager()
	assert.Equal(t, StateDisconnected, sm.GetState())
}

func TestStateManager_SetState_ValidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		from     ConnectionState
		to       ConnectionState
		expected bool
	}{
		// From Disconnected
		{"Disconnected -> Detecting", StateDisconnected, StateDetecting, true},
		{"Disconnected -> Connecting", StateDisconnected, StateConnecting, true},
		{"Disconnected -> Invalid", StateDisconnected, StateHandshaking, false},

		// From Detecting
		{"Detecting -> Connecting", StateDetecting, StateConnecting, true},
		{"Detecting -> Disconnected", StateDetecting, StateDisconnected, true},
		{"Detecting -> Invalid", StateDetecting, StateInitializing, false},

		// From Connecting
		{"Connecting -> Handshaking", StateConnecting, StateHandshaking, true},
		{"Connecting -> Disconnected", StateConnecting, StateDisconnected, true},
		{"Connecting -> Invalid", StateConnecting, StateConnected, false},

		// From Handshaking
		{"Handshaking -> Initializing", StateHandshaking, StateInitializing, true},
		{"Handshaking -> Disconnected", StateHandshaking, StateDisconnected, true},
		{"Handshaking -> Invalid", StateHandshaking, StateConnected, false},

		// From Initializing
		{"Initializing -> Connected", StateInitializing, StateConnected, true},
		{"Initializing -> Disconnected", StateInitializing, StateDisconnected, true},
		{"Initializing -> Invalid", StateInitializing, StateHandshaking, false},

		// From Connected
		{"Connected -> Disconnected", StateConnected, StateDisconnected, true},
		{"Connected -> Detecting", StateConnected, StateDetecting, true},
		{"Connected -> Invalid", StateConnected, StateConnecting, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sm := NewStateManager()
			sm.ForceState(tt.from)

			result := sm.SetState(tt.to)
			assert.Equal(t, tt.expected, result, "SetState(%s -> %s) should return %v",
				tt.from.String(), tt.to.String(), tt.expected)

			if result {
				assert.Equal(t, tt.to, sm.GetState())
			} else {
				assert.Equal(t, tt.from, sm.GetState())
			}
		})
	}
}

func TestStateManager_ForceState(t *testing.T) {
	t.Parallel()

	sm := NewStateManager()

	// Force directly to Connected (normally invalid transition from Disconnected)
	sm.ForceState(StateConnected)
	assert.Equal(t, StateConnected, sm.GetState())

	// Force back to Disconnected
	sm.ForceState(StateDisconnected)
	assert.Equal(t, StateDisconnected, sm.GetState())
}

func TestConnectionState_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expected string
		state    ConnectionState
	}{
		{state: StateDisconnected, expected: "Disconnected"},
		{state: StateDetecting, expected: "Detecting"},
		{state: StateConnecting, expected: "Connecting"},
		{state: StateHandshaking, expected: "Handshaking"},
		{state: StateInitializing, expected: "Initializing"},
		{state: StateConnected, expected: "Connected"},
		{state: ConnectionState(999), expected: "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestValidateStateForOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation string
		state     ConnectionState
		wantError bool
	}{
		{name: "Connected allows operation", operation: "test", state: StateConnected, wantError: false},
		{name: "Disconnected rejects operation", operation: "test", state: StateDisconnected, wantError: true},
		{name: "Connecting rejects operation", operation: "test", state: StateConnecting, wantError: true},
		{name: "Handshaking rejects operation", operation: "test", state: StateHandshaking, wantError: true},
		{name: "Initializing rejects operation", operation: "test", state: StateInitializing, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sm := NewStateManager()
			sm.ForceState(tt.state)

			reader := &Reader{
				stateManager: sm,
			}

			err := reader.validateStateForOperation(tt.operation)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not allowed")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOnMediaChange_InvalidState(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		stateManager: NewStateManager(), // Starts in Disconnected state
	}

	err := reader.OnMediaChange(&models.ActiveMedia{
		SystemID: "Genesis",
		Name:     "Sonic",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

func TestOnMediaChange_DuplicateMedia(t *testing.T) {
	t.Parallel()

	sm := NewStateManager()
	sm.ForceState(StateConnected)

	reader := &Reader{
		stateManager: sm,
		currentMedia: &models.ActiveMedia{
			SystemID: "Genesis",
			Name:     "Sonic",
		},
	}

	// Try to set same media again
	err := reader.OnMediaChange(&models.ActiveMedia{
		SystemID: "Genesis",
		Name:     "Sonic",
	})

	assert.NoError(t, err, "duplicate media should not error, just skip")
}

func TestOnMediaChange_NilMedia(t *testing.T) {
	t.Parallel()

	sm := NewStateManager()
	sm.ForceState(StateConnected)

	reader := &Reader{
		stateManager:   sm,
		operationQueue: make(chan MediaOperation, 10),
	}

	err := reader.OnMediaChange(nil)
	require.NoError(t, err)

	// Should have queued a clear operation (with nil media)
	select {
	case op := <-reader.operationQueue:
		assert.Nil(t, op.media)
	default:
		t.Error("expected operation to be queued")
	}
}

func TestOnMediaChange_QueuesOperation(t *testing.T) {
	t.Parallel()

	sm := NewStateManager()
	sm.ForceState(StateConnected)

	reader := &Reader{
		stateManager:   sm,
		operationQueue: make(chan MediaOperation, 10),
	}

	media := &models.ActiveMedia{
		SystemID: "Genesis",
		Name:     "Sonic",
	}

	err := reader.OnMediaChange(media)
	require.NoError(t, err)

	// Should have queued the media operation
	select {
	case op := <-reader.operationQueue:
		assert.Equal(t, media, op.media)
	default:
		t.Error("expected operation to be queued")
	}
}

func TestQueueOperation_DrainsOldOperations(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		operationQueue: make(chan MediaOperation, 10),
	}

	// Queue multiple operations
	media1 := &models.ActiveMedia{SystemID: "Genesis", Name: "Sonic"}
	media2 := &models.ActiveMedia{SystemID: "SNES", Name: "Mario"}
	media3 := &models.ActiveMedia{SystemID: "PSX", Name: "Crash"}

	reader.queueOperation(media1)
	reader.queueOperation(media2)
	reader.queueOperation(media3) // This should drain media1 and media2

	// Only media3 should be in the queue
	assert.Len(t, reader.operationQueue, 1)

	op := <-reader.operationQueue
	assert.Equal(t, media3, op.media)
}

func TestExtractHexFromLine(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	tests := []struct {
		name     string
		line     string
		expected []string
	}{
		{
			name:     "C array format",
			line:     "0xFF,0x00,0xAA,0xBB,",
			expected: []string{"FF", "00", "AA", "BB"},
		},
		{
			name:     "lowercase hex",
			line:     "0xff,0x00,0xaa,0xbb,",
			expected: []string{"FF", "00", "AA", "BB"},
		},
		{
			name:     "single digit hex (padded)",
			line:     "0xF,0x0,0xA,",
			expected: []string{"0F", "00", "0A"},
		},
		{
			name:     "mixed format",
			line:     "  0xFF, 0x00 ; 0xAA",
			expected: []string{"FF", "00", "AA"},
		},
		{
			name:     "empty line",
			line:     "",
			expected: nil,
		},
		{
			name:     "no hex values",
			line:     "some text without hex",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := reader.extractHexFromLine(tt.line)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHexToBinary(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	tests := []struct {
		name      string
		hexValues []string
		expected  []byte
		wantError bool
	}{
		{
			name:      "valid hex",
			hexValues: []string{"FF", "00", "AA", "BB"},
			expected:  []byte{0xFF, 0x00, 0xAA, 0xBB},
			wantError: false,
		},
		{
			name:      "invalid hex - too long",
			hexValues: []string{"FFF"},
			expected:  nil,
			wantError: true,
		},
		{
			name:      "invalid hex - not hex",
			hexValues: []string{"ZZ"},
			expected:  nil,
			wantError: true,
		},
		{
			name:      "empty input",
			hexValues: []string{},
			expected:  []byte{},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := reader.hexToBinary(tt.hexValues)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestReadPictureFile(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	// Create a test picture file
	tmpDir := t.TempDir()
	picturePath := filepath.Join(tmpDir, "test.xbm")

	// XBM format: 3 header lines + hex data
	content := `// Comment line 1
// Comment line 2
const unsigned char test[] = {
0xFF,0x00,0xAA,
0xBB,0xCC,0xDD
};`

	err := os.WriteFile(picturePath, []byte(content), 0o600)
	require.NoError(t, err)

	hexValues, err := reader.readPictureFile(picturePath)
	require.NoError(t, err)

	expected := []string{"FF", "00", "AA", "BB", "CC", "DD"}
	assert.Equal(t, expected, hexValues)
}

func TestReadPictureFile_TooShort(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	tmpDir := t.TempDir()
	picturePath := filepath.Join(tmpDir, "test.xbm")

	// Only 2 lines (need at least 4)
	content := `line1
line2`

	err := os.WriteFile(picturePath, []byte(content), 0o600)
	require.NoError(t, err)

	_, err = reader.readPictureFile(picturePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestReadPictureFile_NoHexData(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	tmpDir := t.TempDir()
	picturePath := filepath.Join(tmpDir, "test.xbm")

	// Header lines but no hex data
	content := `line1
line2
line3
line4`

	err := os.WriteFile(picturePath, []byte(content), 0o600)
	require.NoError(t, err)

	_, err = reader.readPictureFile(picturePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no hex data")
}

func TestIsDisconnectionError(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "device not configured string",
			err:      &customError{msg: "device not configured"},
			expected: true,
		},
		{
			name:     "input/output error string",
			err:      &customError{msg: "input/output error"},
			expected: true,
		},
		{
			name:     "no such device string",
			err:      &customError{msg: "no such device"},
			expected: true,
		},
		{
			name:     "device not found string",
			err:      &customError{msg: "device not found"},
			expected: true,
		},
		{
			name:     "broken pipe string",
			err:      &customError{msg: "broken pipe"},
			expected: true,
		},
		{
			name:     "device disconnected string",
			err:      &customError{msg: "device disconnected"},
			expected: true,
		},
		{
			name:     "other error (not disconnection)",
			err:      &customError{msg: "some other error"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := reader.isDisconnectionError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// customError is a helper for testing error string matching
type customError struct {
	msg string
}

func (e *customError) Error() string {
	return e.msg
}

func TestGetDevicePath(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		path: "/dev/ttyUSB0",
	}

	assert.Equal(t, "/dev/ttyUSB0", reader.getDevicePath())
}

func TestGetState(t *testing.T) {
	t.Parallel()

	sm := NewStateManager()
	sm.ForceState(StateConnected)

	reader := &Reader{
		stateManager: sm,
	}

	assert.Equal(t, StateConnected, reader.getState())
}

func TestSetState_Success(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		stateManager: NewStateManager(),
	}

	// Valid transition: Disconnected -> Connecting
	result := reader.setState(StateConnecting)
	assert.True(t, result)
	assert.Equal(t, StateConnecting, reader.getState())
}

func TestSetState_Failure(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		stateManager: NewStateManager(),
	}

	// Invalid transition: Disconnected -> Handshaking (must go through Connecting first)
	result := reader.setState(StateHandshaking)
	assert.False(t, result)
	assert.Equal(t, StateDisconnected, reader.getState())
}

// TODO: Add comprehensive mock-based tests for picture manager:
// - Mock HTTP client for picture downloads
// - Test picture format selection
// - Test caching behavior
// - Test error handling for download failures
// - Test picture file validation

// TODO: Add comprehensive mock-based tests for serial operations:
// - Mock serial.Port for Open/Close operations
// - Test handshake sequence
// - Test initialization sequence
// - Test command sending
// - Test picture data transmission
// - Test device detection
// - Test health check (when re-enabled)
// - Test operation worker goroutine
// - Test panic recovery in operation worker
// - Test queue overflow handling
// - Test disconnection detection during operations
//
// These require creating mocks for:
// - serial.Port interface
// - http.Client for picture downloads
// - Context cancellation behavior
//
// Consider using dependency injection pattern (like MQTT reader) to allow
// factory methods for creating serial ports and HTTP clients in tests.

func TestProtocolCommands_Defined(t *testing.T) {
	t.Parallel()

	// Verify all protocol commands are defined
	commands := []string{
		CmdHandshake,
		CmdCore,
		CmdText,
		CmdShowName,
		CmdClearShow,
		CmdClearNoUpd,
		CmdContrast,
		CmdRotate,
		CmdOrgLogo,
		CmdClear,
		CmdScreensaver,
		CmdSetTime,
		CmdHardwareInfo,
	}

	for _, cmd := range commands {
		assert.NotEmpty(t, cmd, "command should not be empty")
		assert.True(t, strings.HasPrefix(cmd, "CMD") || cmd == "QWERTZ",
			"command %s should have CMD prefix or be QWERTZ", cmd)
	}
}

func TestProtocolConstants(t *testing.T) {
	t.Parallel()

	// Verify protocol constants
	assert.Equal(t, "\n", CommandTerminator)
	assert.Equal(t, "-1", TransitionAuto)
	assert.Equal(t, 0, ContrastMin)
	assert.Equal(t, 255, ContrastMax)
	assert.Equal(t, 128, ContrastDefault)
}

func TestPictureFormats(t *testing.T) {
	t.Parallel()

	// Verify picture formats are defined in priority order
	assert.Len(t, PictureFormats, 5)
	assert.Equal(t, "GSC_US", PictureFormats[0], "GSC_US should be highest priority")
	assert.Equal(t, "XBM_US", PictureFormats[1])
	assert.Equal(t, "GSC", PictureFormats[2])
	assert.Equal(t, "XBM", PictureFormats[3])
	assert.Equal(t, "XBM_TEXT", PictureFormats[4])
}
