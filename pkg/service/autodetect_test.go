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

package service

import (
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewAutoDetector(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	ad := NewAutoDetector(cfg)

	require.NotNil(t, ad)
	assert.NotNil(t, ad.connected, "connected map should be initialized")
	assert.NotNil(t, ad.failed, "failed map should be initialized")
	assert.Empty(t, ad.connected)
	assert.Empty(t, ad.failed)
}

func TestAutoDetector_ConnectedMapOperations(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	// Initially empty
	assert.False(t, ad.isConnected("/dev/ttyUSB0"))

	// Set connected
	ad.setConnected("/dev/ttyUSB0")
	assert.True(t, ad.isConnected("/dev/ttyUSB0"))

	// Another path should not be connected
	assert.False(t, ad.isConnected("/dev/ttyUSB1"))

	// Clear path
	ad.ClearPath("/dev/ttyUSB0")
	assert.False(t, ad.isConnected("/dev/ttyUSB0"))
}

func TestAutoDetector_FailedPathOperations(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	// Set failed paths
	ad.setFailed("/dev/ttyUSB0")
	ad.setFailed("/dev/ttyUSB1")

	// Get all failed paths
	failed := ad.getFailedPaths()
	assert.Len(t, failed, 2)
	assert.Contains(t, failed, "/dev/ttyUSB0")
	assert.Contains(t, failed, "/dev/ttyUSB1")

	// Clear failed path
	ad.ClearFailedPath("/dev/ttyUSB0")
	failed = ad.getFailedPaths()
	assert.Len(t, failed, 1)
	assert.Contains(t, failed, "/dev/ttyUSB1")
}

func TestAutoDetector_FailedPathsAreSharedAcrossDrivers(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	// A failed path is shared across all drivers - if a path fails,
	// no driver should retry it until it's cleared
	ad.setFailed("/dev/ttyUSB0")

	failed := ad.getFailedPaths()
	assert.Len(t, failed, 1)
	assert.Contains(t, failed, "/dev/ttyUSB0")

	// Clear and verify
	ad.ClearFailedPath("/dev/ttyUSB0")
	failed = ad.getFailedPaths()
	assert.Empty(t, failed)
}

func TestAutoDetector_UpdateConnectedFromReaders(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	// Pre-set some connections
	ad.setConnected("/dev/old")

	// Create mock readers
	mockReader1 := mocks.NewMockReader()
	mockReader1.On("Path").Return("/dev/ttyUSB0")

	mockReader2 := mocks.NewMockReader()
	mockReader2.On("Path").Return("/dev/ttyUSB1")

	// Update from readers - should replace old connections
	ad.updateConnectedFromReaders([]readers.Reader{mockReader1, mockReader2})

	// New paths should be connected
	assert.True(t, ad.isConnected("/dev/ttyUSB0"))
	assert.True(t, ad.isConnected("/dev/ttyUSB1"))

	// Old path should be gone
	assert.False(t, ad.isConnected("/dev/old"))
}

func TestAutoDetector_UpdateConnectedFromReaders_HandlesNil(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	mockReader := mocks.NewMockReader()
	mockReader.On("Path").Return("/dev/ttyUSB0")

	// Include a nil in the slice
	ad.updateConnectedFromReaders([]readers.Reader{mockReader, nil})

	assert.True(t, ad.isConnected("/dev/ttyUSB0"))
}

func TestAutoDetector_DetectReaders_NoSupportedReaders(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{})

	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	mockPlatform.AssertExpectations(t)
}

func TestAutoDetector_DetectReaders_DriverNotEnabled(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}

	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "test-driver",
		DefaultEnabled:    false, // Not enabled by default
		DefaultAutoDetect: true,
	})

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	// Detect should not be called since driver is not enabled
	mockReader.AssertNotCalled(t, "Detect", mock.Anything)
}

func TestAutoDetector_DetectReaders_AutoDetectDisabled(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(false) // Disable auto-detect globally

	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "test-driver",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"test-driver"}).Maybe()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	// Detect should not be called since auto-detect is disabled
	mockReader.AssertNotCalled(t, "Detect", mock.Anything)
}

func TestAutoDetector_DetectReaders_NoDeviceDetected(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"simpleserial"}).Maybe()
	mockReader.On("Detect", mock.Anything).Return("") // No device found

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	mockReader.AssertExpectations(t)
}

func TestAutoDetector_DetectReaders_InvalidDetectString(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"simpleserial"}).Maybe()
	mockReader.On("Detect", mock.Anything).Return("invalid-string-no-colon")

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	mockReader.AssertExpectations(t)
}

func TestAutoDetector_DetectReaders_AlreadyConnected(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	// Create an existing connected reader that the state knows about
	existingReader := mocks.NewMockReader()
	existingReader.On("Path").Return("/dev/ttyUSB0")
	existingReader.On("Metadata").Return(readers.DriverMetadata{ID: "simpleserial"})
	existingReader.On("ReaderID").Return("simpleserial-existing")

	// This mock reader will try to detect the same path
	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"simpleserial"}).Maybe()
	mockReader.On("Detect", mock.Anything).Return("simpleserial:/dev/ttyUSB0")
	mockReader.On("Close").Return(nil)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, notifCh := state.NewState(mockPlatform, "test-uuid")
	// Set the existing reader in state so the path is marked as connected
	st.SetReader(existingReader)
	scanChan := make(chan readers.Scan, 10)

	// Drain notifications
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range notifCh { //nolint:revive // drain channel
		}
	}()

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	// Close should be called for the unused reader (the one that detected same path)
	mockReader.AssertCalled(t, "Close")

	st.StopService()
	close(st.Notifications)
	<-done
}

func TestAutoDetector_DetectReaders_SuccessfulConnection(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"simpleserial"}).Maybe()
	mockReader.On("Detect", mock.Anything).Return("simpleserial:/dev/ttyUSB0")
	mockReader.On("Open", mock.Anything, mock.Anything).Return(nil)
	mockReader.On("Connected").Return(true)
	mockReader.On("Path").Return("/dev/ttyUSB0")
	mockReader.On("ReaderID").Return("simpleserial-abc123")

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, notifCh := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	// Drain notification channel in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range notifCh { //nolint:revive // drain channel
		}
	}()

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	assert.True(t, ad.isConnected("/dev/ttyUSB0"))

	// Verify reader was set in state
	rs := st.ListReaders()
	assert.Len(t, rs, 1)

	mockReader.AssertExpectations(t)

	// Cleanup
	st.StopService()
	close(st.Notifications)
	<-done
}

func TestAutoDetector_DetectReaders_OpenError(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"simpleserial"}).Maybe()
	mockReader.On("Detect", mock.Anything).Return("simpleserial:/dev/ttyUSB0")
	mockReader.On("Open", mock.Anything, mock.Anything).Return(errors.New("open failed"))

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err) // DetectReaders doesn't return errors for individual failures
	// Path should be marked as failed
	failed := ad.getFailedPaths()
	assert.Contains(t, failed, "/dev/ttyUSB0")
}

func TestAutoDetector_DetectReaders_ConnectedReturnsFalse(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"simpleserial"}).Maybe()
	mockReader.On("Detect", mock.Anything).Return("simpleserial:/dev/ttyUSB0")
	mockReader.On("Open", mock.Anything, mock.Anything).Return(nil)
	mockReader.On("Connected").Return(false) // Connection failed
	mockReader.On("Close").Return(nil)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	// Path should be marked as failed
	failed := ad.getFailedPaths()
	assert.Contains(t, failed, "/dev/ttyUSB0")
	mockReader.AssertCalled(t, "Close")
}

func TestAutoDetector_ConnectReader_Success(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	mockReader := mocks.NewMockReader()
	mockReader.On("Open", mock.Anything, mock.Anything).Return(nil)
	mockReader.On("Connected").Return(true)
	mockReader.On("Path").Return("/dev/ttyUSB0")
	mockReader.On("ReaderID").Return("simpleserial-abc123")
	mockReader.On("Metadata").Return(readers.DriverMetadata{ID: "simpleserial"})

	mockPlatform := mocks.NewMockPlatform()
	st, notifCh := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	// Drain notification channel
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range notifCh { //nolint:revive // drain channel
		}
	}()

	err := ad.connectReader(mockReader, "simpleserial", "/dev/ttyUSB0", "simpleserial:/dev/ttyUSB0", st, scanChan)

	require.NoError(t, err)
	assert.True(t, ad.isConnected("/dev/ttyUSB0"))

	st.StopService()
	close(st.Notifications)
	<-done
}

func TestAutoDetector_ConnectReader_OpenError(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	mockReader := mocks.NewMockReader()
	mockReader.On("Open", mock.Anything, mock.Anything).Return(errors.New("permission denied"))

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.connectReader(mockReader, "simpleserial", "/dev/ttyUSB0", "simpleserial:/dev/ttyUSB0", st, scanChan)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestAutoDetector_ConnectReader_NotConnected(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	mockReader := mocks.NewMockReader()
	mockReader.On("Open", mock.Anything, mock.Anything).Return(nil)
	mockReader.On("Connected").Return(false)
	mockReader.On("Close").Return(nil)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.connectReader(mockReader, "simpleserial", "/dev/ttyUSB0", "simpleserial:/dev/ttyUSB0", st, scanChan)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reader failed to connect")
	mockReader.AssertCalled(t, "Close")
}

func TestAutoDetector_ConnectReader_CloseError(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	mockReader := mocks.NewMockReader()
	mockReader.On("Open", mock.Anything, mock.Anything).Return(nil)
	mockReader.On("Connected").Return(false)
	mockReader.On("Close").Return(errors.New("close failed"))

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.connectReader(mockReader, "simpleserial", "/dev/ttyUSB0", "simpleserial:/dev/ttyUSB0", st, scanChan)

	// Should still return the main error, close error is just logged
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reader failed to connect")
}

func TestAutoDetector_LogDetectionResults_StateChange(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	// First call with detected devices
	ad.logDetectionResults([]string{"simpleserial:/dev/ttyUSB0"}, nil)

	// Should have logged and updated state
	assert.False(t, ad.lastLogTime.IsZero())
	assert.NotEmpty(t, ad.lastDetectionSummary)

	firstLogTime := ad.lastLogTime
	firstSummary := ad.lastDetectionSummary

	// Same state - shouldn't update (unless heartbeat)
	time.Sleep(10 * time.Millisecond)
	ad.logDetectionResults([]string{"simpleserial:/dev/ttyUSB0"}, nil)

	// Log time should be same since state didn't change and not enough time for heartbeat
	assert.Equal(t, firstLogTime, ad.lastLogTime)
	assert.Equal(t, firstSummary, ad.lastDetectionSummary)
}

func TestAutoDetector_LogDetectionResults_NoDevices(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	// First call with no detected devices (triggers heartbeat on first call)
	ad.logDetectionResults(nil, nil)

	// Should have logged
	assert.False(t, ad.lastLogTime.IsZero())
}

func TestAutoDetector_LogDetectionResults_WithFailedAttempts(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	// Add some failed attempts
	ad.setFailed("/dev/ttyUSB0")
	ad.setFailed("/dev/ttyUSB1")

	// Log with no new devices
	ad.logDetectionResults(nil, nil)

	// Should have logged (heartbeat triggered on first call)
	assert.False(t, ad.lastLogTime.IsZero())

	// Summary should include failed count
	assert.Contains(t, ad.lastDetectionSummary, "total_failed:2")
}

func TestAutoDetector_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	done := make(chan struct{})

	// Concurrent writers
	go func() {
		for i := range 100 {
			ad.setConnected("/dev/ttyUSB" + string(rune('0'+i%10)))
		}
		done <- struct{}{}
	}()

	go func() {
		for i := range 100 {
			ad.setFailed("/dev/ttyUSB" + string(rune('0'+i%10)))
		}
		done <- struct{}{}
	}()

	// Concurrent readers
	go func() {
		for range 100 {
			_ = ad.isConnected("/dev/ttyUSB0")
			_ = ad.getFailedPaths()
		}
		done <- struct{}{}
	}()

	// Wait for all goroutines
	for range 3 {
		<-done
	}

	// No race conditions should have occurred
}

func TestAutoDetector_DetectReaders_ExcludesConnectedReaderPaths(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	// Create an existing connected reader
	existingReader := mocks.NewMockReader()
	existingReader.On("Path").Return("/dev/ttyUSB0")
	existingReader.On("Metadata").Return(readers.DriverMetadata{ID: "simpleserial"})
	existingReader.On("ReaderID").Return("simpleserial-existing")

	// Create a new reader for detection
	newReader := mocks.NewMockReader()
	newReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	newReader.On("IDs").Return([]string{"simpleserial"}).Maybe()
	// Detect is called with exclude list containing existing reader's connection string.
	// IMPORTANT: The format MUST be "driver:path" because Detect() implementations parse
	// this format to extract the path. Using just the path would break detection.
	newReader.On("Detect", mock.MatchedBy(func(exclude []string) bool {
		for _, e := range exclude {
			if e == "simpleserial:/dev/ttyUSB0" {
				return true
			}
		}
		return false
	})).Return("")

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{newReader})

	st, notifCh := state.NewState(mockPlatform, "test-uuid")
	st.SetReader(existingReader)
	scanChan := make(chan readers.Scan, 10)

	// Drain notifications
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range notifCh { //nolint:revive // drain channel
		}
	}()

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	newReader.AssertExpectations(t)

	st.StopService()
	close(st.Notifications)
	<-done
}

func TestAutoDetector_DetectReaders_ClearsFailedOnSuccess(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	// Pre-mark as failed
	ad.setFailed("/dev/ttyUSB0")
	require.Len(t, ad.getFailedPaths(), 1)

	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"simpleserial"}).Maybe()
	mockReader.On("Detect", mock.Anything).Return("simpleserial:/dev/ttyUSB0")
	mockReader.On("Open", mock.Anything, mock.Anything).Return(nil)
	mockReader.On("Connected").Return(true)
	mockReader.On("Path").Return("/dev/ttyUSB0")
	mockReader.On("ReaderID").Return("simpleserial-abc123")

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, notifCh := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	// Drain notifications
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range notifCh { //nolint:revive // drain channel
		}
	}()

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)
	// Failed should be cleared after successful connection
	assert.Empty(t, ad.getFailedPaths())

	st.StopService()
	close(st.Notifications)
	<-done
}

func TestAutoDetector_GetFailedPaths_AnyPath(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	// Any string can be stored as a path
	ad.setFailed("some-path")
	ad.setFailed("/dev/ttyUSB0")

	failed := ad.getFailedPaths()
	assert.Len(t, failed, 2)
	assert.Contains(t, failed, "some-path")
	assert.Contains(t, failed, "/dev/ttyUSB0")
}

func TestAutoDetector_DetectReaders_AlreadyConnected_CloseError(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	// Create an existing connected reader that the state knows about
	existingReader := mocks.NewMockReader()
	existingReader.On("Path").Return("/dev/ttyUSB0")
	existingReader.On("Metadata").Return(readers.DriverMetadata{ID: "simpleserial"})
	existingReader.On("ReaderID").Return("simpleserial-existing")

	// This mock reader will try to detect the same path and fail to close
	// Don't use NewMockReader to avoid the default Close().Maybe() setup
	mockReader := &mocks.MockReader{}
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "simpleserial",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"simpleserial"}).Maybe()
	mockReader.On("Detect", mock.Anything).Return("simpleserial:/dev/ttyUSB0")
	mockReader.On("Close").Return(errors.New("close failed"))

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, notifCh := state.NewState(mockPlatform, "test-uuid")
	st.SetReader(existingReader)
	scanChan := make(chan readers.Scan, 10)

	// Drain notifications
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range notifCh { //nolint:revive // drain channel
		}
	}()

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	// Should not fail even though Close returned error
	require.NoError(t, err)
	mockReader.AssertCalled(t, "Close")

	st.StopService()
	close(st.Notifications)
	<-done
}

func TestAutoDetector_ConnectReader_CloseError_AfterFailedConnect(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)

	// Don't use NewMockReader to avoid the default Close().Maybe() setup
	mockReader := &mocks.MockReader{}
	mockReader.On("Open", mock.Anything, mock.Anything).Return(nil)
	mockReader.On("Connected").Return(false) // Connection failed
	mockReader.On("Close").Return(errors.New("close failed"))

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	err := ad.connectReader(mockReader, "simpleserial", "/dev/ttyUSB0", "simpleserial:/dev/ttyUSB0", st, scanChan)

	// Should still return the connection error, close error is just logged
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reader failed to connect")
	mockReader.AssertCalled(t, "Close")
}

func TestAutoDetector_DetectReaders_EmptyPath(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "mqtt",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	mockReader.On("IDs").Return([]string{"mqtt"}).Maybe()
	// MQTT returns driver with empty path
	mockReader.On("Detect", mock.Anything).Return("mqtt:")
	mockReader.On("Open", mock.Anything, mock.Anything).Return(nil)
	mockReader.On("Connected").Return(true)
	mockReader.On("Path").Return("")
	mockReader.On("ReaderID").Return("mqtt-broker")

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{mockReader})

	st, notifCh := state.NewState(mockPlatform, "test-uuid")
	scanChan := make(chan readers.Scan, 10)

	// Drain notifications
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range notifCh { //nolint:revive // drain channel
		}
	}()

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)

	require.NoError(t, err)

	st.StopService()
	close(st.Notifications)
	<-done
}

// TestAutoDetector_ReconnectAfterUnplugReplug is a regression test for the bug where
// readers would never reconnect after being unplugged and replugged. The bug was caused
// by a key mismatch: failed connections were stored by path (e.g., "/dev/ttyUSB0") but
// the code tried to clear them using the hashed reader ID (e.g., "pn532-a1b2c3d4").
// This test ensures that after a reader disconnects, its path is properly cleared from
// the failed list, allowing it to reconnect when plugged back in.
func TestAutoDetector_ReconnectAfterUnplugReplug(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	path := "/dev/ttyUSB0"

	// Simulate a failed connection attempt (e.g., device briefly unavailable during plug-in)
	ad.setFailed(path)
	require.Contains(t, ad.getFailedPaths(), path, "path should be in failed list after failed connection")

	// Simulate reader disconnect (unplug) - this is what readers.go does when pruning
	// disconnected readers. Previously this used ClearFailedConnection(readerID) with
	// the hashed reader ID, which never matched the path-based key.
	ad.ClearPath(path)
	ad.ClearFailedPath(path)

	// Verify the path is cleared from failed list
	assert.NotContains(t, ad.getFailedPaths(), path,
		"path should be cleared from failed list after disconnect, allowing reconnection on replug")

	// Simulate replug - a new detection cycle should be able to try this path again
	// If the bug were present, the path would still be in the failed list and
	// auto-detect would skip it forever
	failedPaths := ad.getFailedPaths()
	for _, p := range failedPaths {
		if p == path {
			t.Fatal("REGRESSION: path still in failed list after disconnect - reader would never reconnect")
		}
	}
}

// TestAutoDetector_ExcludeListFormat_Regression is a regression test that verifies
// the exclude list passed to Detect() uses the "driver:path" format.
//
// CRITICAL: Detect() implementations parse this format with strings.SplitN(":", 2)
// to extract the path. If the exclude list contains just paths (e.g., "/dev/ttyUSB0"
// instead of "simpleserial:/dev/ttyUSB0"), the parsing will fail and connected
// devices will be repeatedly "detected" as new, causing reconnection loops.
func TestAutoDetector_ExcludeListFormat_Regression(t *testing.T) {
	t.Parallel()

	ad := NewAutoDetector(nil)
	cfg := &config.Instance{}
	cfg.SetAutoDetect(true)

	// Create an existing connected reader
	existingReader := mocks.NewMockReader()
	existingReader.On("Path").Return("/dev/ttyUSB0")
	existingReader.On("Metadata").Return(readers.DriverMetadata{ID: "pn532_uart"})
	existingReader.On("ReaderID").Return("pn532-abc123")

	// Create a new reader for detection that captures the exclude list
	var capturedExcludeList []string
	newReader := mocks.NewMockReader()
	newReader.On("Metadata").Return(readers.DriverMetadata{
		ID:                "pn532_uart",
		DefaultEnabled:    true,
		DefaultAutoDetect: true,
	})
	newReader.On("IDs").Return([]string{"pn532_uart"}).Maybe()
	newReader.On("Detect", mock.Anything).Run(func(args mock.Arguments) {
		if list, ok := args.Get(0).([]string); ok {
			capturedExcludeList = list
		}
	}).Return("")

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", cfg).Return([]readers.Reader{newReader})

	st, notifCh := state.NewState(mockPlatform, "test-uuid")
	st.SetReader(existingReader)
	scanChan := make(chan readers.Scan, 10)

	// Drain notifications
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range notifCh { //nolint:revive // drain channel
		}
	}()

	err := ad.DetectReaders(mockPlatform, cfg, st, scanChan)
	require.NoError(t, err)

	// CRITICAL CHECK: Verify exclude list uses "driver:path" format
	require.NotEmpty(t, capturedExcludeList, "exclude list should not be empty")

	foundCorrectFormat := false
	for _, entry := range capturedExcludeList {
		// Each entry MUST contain a colon separating driver from path
		if entry == "pn532_uart:/dev/ttyUSB0" {
			foundCorrectFormat = true
			break
		}
		// REGRESSION: If we find just the path without driver prefix, fail immediately
		if entry == "/dev/ttyUSB0" {
			t.Fatal("REGRESSION: exclude list contains path without driver prefix - " +
				"this will break Detect() implementations that parse 'driver:path' format, " +
				"causing connected devices to be repeatedly detected as new")
		}
	}

	assert.True(t, foundCorrectFormat,
		"exclude list should contain 'pn532_uart:/dev/ttyUSB0', got: %v", capturedExcludeList)

	st.StopService()
	close(st.Notifications)
	<-done
}
